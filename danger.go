package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/notnil/chess"
)

const ForcedAdvCP = 600

type DangerGroup struct {
	Main string
	Subs []string
}

func mateInOne(pos *chess.Position) *chess.Move {
	for _, m := range pos.ValidMoves() {
		after := pos.Update(m)
		if isCheckmate(after) {
			return m
		}
	}
	return nil
}

func pieceValue(t chess.PieceType) int {
	switch t {
	case chess.Pawn:
		return 1
	case chess.Knight:
		return 3
	case chess.Bishop:
		return 3
	case chess.Rook:
		return 5
	case chess.Queen:
		return 9
	default:
		return 0
	}
}

func blunderPriority(pos *chess.Position, m *chess.Move, heroColor chess.Color) (int, string) {
	b := pos.Board()
	p := b.Piece(m.S1())
	uci := (chess.UCINotation{}).Encode(pos, m)
	if p == chess.NoPiece || p.Color() != heroColor {
		return 50, uci
	}
	if p.Type() == chess.Pawn {
		file := int(m.S1().File())
		if file == 6 {
			return 0, uci // g pawn
		}
		if file == 5 {
			return 1, uci // f pawn
		}
		if file == 7 {
			return 2, uci // h pawn
		}
		if file == 3 || file == 4 {
			return 6, uci // central pawns
		}
		return 12, uci
	}
	if p.Type() == chess.King {
		return 8, uci
	}
	return 20, uci
}

func hasCheckOrCapture(pos *chess.Position) bool {
	for _, m := range pos.ValidMoves() {
		if m.HasTag(chess.Capture) || m.HasTag(chess.EnPassant) {
			return true
		}
		after := pos.Update(m)
		if inCheck(after, after.Turn()) {
			return true
		}
	}
	return false
}

func extendPVUntilMate(e *UCIEdge, pos *chess.Position, pvSeed []string, maxPlies int, t time.Duration, cache *AnalyseCache) []string {
	temp := pos
	out := []string{}
	for _, u := range pvSeed {
		mv, err := (chess.UCINotation{}).Decode(temp, u)
		if err != nil {
			break
		}
		out = append(out, u)
		temp = temp.Update(mv)
		if isCheckmate(temp) {
			return out
		}
	}

	for len(out) < maxPlies {
		if len(temp.ValidMoves()) == 0 {
			break
		}
		m1 := mateInOne(temp)
		if m1 != nil {
			u := (chess.UCINotation{}).Encode(temp, m1)
			out = append(out, u)
			temp = temp.Update(m1)
			break
		}

		info, err := analyseCachedSingle(e, temp.String(), t, cache)
		if err != nil || len(info.PV) == 0 {
			break
		}
		u0 := info.PV[0]
		mv, err := (chess.UCINotation{}).Decode(temp, u0)
		if err != nil {
			break
		}
		out = append(out, u0)
		temp = temp.Update(mv)
		if isCheckmate(temp) {
			break
		}
	}

	return out
}

func robustAdvantageAfterMove(e *UCIEdge, posBefore *chess.Position, punisherMoveUCI string, punisherColor chess.Color, minCP int, t time.Duration, defenses int, cache *AnalyseCache) bool {
	mv, err := (chess.UCINotation{}).Decode(posBefore, punisherMoveUCI)
	if err != nil {
		return false
	}
	b1 := posBefore.Update(mv)

	lines, err := e.AnalyseFEN(b1.String(), t, max(1, defenses))
	if err != nil {
		return false
	}

	worst := (*int)(nil)
	for _, l := range lines {
		if len(l.PV) == 0 {
			continue
		}
		d0 := l.PV[0]
		dm, err := (chess.UCINotation{}).Decode(b1, d0)
		if err != nil {
			continue
		}
		b2 := b1.Update(dm)
		reply, err := analyseCachedSingle(e, b2.String(), t, cache)
		if err != nil {
			continue
		}
		cp := scoreToCP(reply.Score, fromChessColor(b2.Turn()), fromChessColor(punisherColor))
		if worst == nil {
			worst = new(int)
			*worst = cp
		} else if cp < *worst {
			*worst = cp
		}
	}

	if worst == nil {
		return false
	}
	return *worst >= minCP
}

type severityKey struct {
	Kind int
	Val  int
}

func severity(mateIn *int, cp *int) severityKey {
	if mateIn != nil && *mateIn > 0 {
		return severityKey{Kind: 0, Val: *mateIn}
	}
	v := 0
	if cp != nil {
		v = *cp
	}
	return severityKey{Kind: 1, Val: -v}
}

type cand struct {
	Main          string
	Subs          []string
	AfterPunish   *chess.Position
	Severity      severityKey
	MateIn        *int
	CP            int
	PunisherFirst string
	AfterBlunder  *chess.Position
}

func dangerVariantsGrouped(
	e *UCIEdge,
	posStart *chess.Position,
	pv []string,
	victimColor chess.Color,
	startAfterPlies int,
	maxGroups int,
	maxSubs int,
	maxBlunders int,
	engineTime time.Duration,
	minCPAdvantage int,
	pvPlies int,
	excludeUCI map[string]struct{},
	cache *AnalyseCache,
) []DangerGroup {
	base := posStart
	if startAfterPlies > 0 {
		if len(pv) == 0 {
			return nil
		}
		applied := 0
		for _, u := range pv {
			if applied >= startAfterPlies {
				break
			}
			mv, err := (chess.UCINotation{}).Decode(base, u)
			if err != nil {
				break
			}
			base = base.Update(mv)
			applied++
		}
		if applied != startAfterPlies {
			return nil
		}
	}

	if base.Turn() != victimColor {
		return nil
	}

	blunders := base.ValidMoves()
	sort.Slice(blunders, func(i, j int) bool {
		pi, ui := blunderPriority(base, blunders[i], victimColor)
		pj, uj := blunderPriority(base, blunders[j], victimColor)
		if pi != pj {
			return pi < pj
		}
		return ui < uj
	})

	cands := []cand{}
	var forced *struct {
		Key severityKey
		Txt string
	}

	for idx, bm := range blunders {
		if idx >= maxBlunders {
			break
		}
		uci := (chess.UCINotation{}).Encode(base, bm)
		if excludeUCI != nil {
			if _, ok := excludeUCI[uci]; ok {
				continue
			}
		}
		afterBlunder := base.Update(bm)

		m1 := mateInOne(afterBlunder)
		if m1 != nil {
			seq := []string{uci, (chess.UCINotation{}).Encode(afterBlunder, m1)}
			txt := pvNumberedAutograded(e, base, seq, len(seq), nil, cache)
			key := severity(ptr(1), ptr(MateScore))
			if forced == nil || key.Kind < forced.Key.Kind || (key.Kind == forced.Key.Kind && key.Val < forced.Key.Val) {
				forced = &struct {
					Key severityKey
					Txt string
				}{Key: key, Txt: txt}
			}
			continue
		}

		if !hasCheckOrCapture(afterBlunder) {
			continue
		}

		info, err := analyseCachedSingle(e, afterBlunder.String(), engineTime, cache)
		if err != nil || len(info.PV) == 0 {
			continue
		}
		punisher := afterBlunder.Turn()
		cpPunisher := scoreToCP(info.Score, fromChessColor(punisher), fromChessColor(punisher))
		var mateIn *int
		if info.Score.IsMate && info.Score.Mate > 0 {
			mi := info.Score.Mate
			mateIn = &mi
		}

		punFirst := info.PV[0]
		thr := minCPAdvantage
		pm, err := (chess.UCINotation{}).Decode(afterBlunder, punFirst)
		if err == nil {
			afterPun := afterBlunder.Update(pm)
			if inCheck(afterPun, afterPun.Turn()) {
				thr = min(thr, 120)
			}
			if pm.HasTag(chess.Capture) || pm.HasTag(chess.EnPassant) {
				cap := afterBlunder.Board().Piece(pm.S2())
				if cap != chess.NoPiece && pieceValue(cap.Type()) >= 3 {
					thr = min(thr, 150)
				}
			}
		}

		if mateIn == nil && cpPunisher < thr {
			continue
		}

		follow := []string{}
		temp := afterBlunder
		for i := 0; i < len(info.PV) && i < max(1, pvPlies-1); i++ {
			u := info.PV[i]
			mv, err := (chess.UCINotation{}).Decode(temp, u)
			if err != nil {
				break
			}
			follow = append(follow, u)
			temp = temp.Update(mv)
		}
		lineMoves := append([]string{uci}, follow...)
		txtMain := pvNumberedAutograded(e, base, lineMoves, len(lineMoves), nil, cache)

		// after_punish = base + bm + punisher_first
		var afterPunish *chess.Position
		if pm != nil && err == nil {
			ap := base.Update(bm).Update(pm)
			afterPunish = ap
		}

		sev := severity(mateIn, ptr(cpPunisher))
		cands = append(cands, cand{Main: txtMain, AfterPunish: afterPunish, Severity: sev, MateIn: mateIn, CP: cpPunisher, PunisherFirst: punFirst, AfterBlunder: afterBlunder})

		// forced lines
		if mateIn != nil {
			seed := []string{}
			t3 := afterBlunder
			for _, u := range info.PV {
				mv, err := (chess.UCINotation{}).Decode(t3, u)
				if err != nil {
					break
				}
				seed = append(seed, u)
				t3 = t3.Update(mv)
				if isCheckmate(t3) {
					break
				}
			}
			ext := extendPVUntilMate(e, afterBlunder, seed, 32, maxDuration(50*time.Millisecond, engineTime), cache)
			full := append([]string{uci}, ext...)
			forcedTxt := pvEmSANNumbered(base, full, len(full))
			k := severity(mateIn, ptr(MateScore))
			if forced == nil || k.Kind < forced.Key.Kind || (k.Kind == forced.Key.Kind && k.Val < forced.Key.Val) {
				forced = &struct {
					Key severityKey
					Txt string
				}{Key: k, Txt: forcedTxt}
			}
		} else if cpPunisher >= ForcedAdvCP {
			if robustAdvantageAfterMove(e, afterBlunder, punFirst, punisher, ForcedAdvCP, maxDuration(60*time.Millisecond, engineTime), 3, cache) {
				more := []string{}
				t4 := afterBlunder
				limit := min(len(info.PV), max(pvPlies, 10))
				for i := 0; i < limit; i++ {
					u := info.PV[i]
					mv, err := (chess.UCINotation{}).Decode(t4, u)
					if err != nil {
						break
					}
					more = append(more, u)
					t4 = t4.Update(mv)
					if len(t4.ValidMoves()) == 0 {
						break
					}
				}
				full := append([]string{uci}, more...)
				forcedTxt := pvEmSANNumbered(base, full, len(full))
				k := severity(nil, ptr(cpPunisher))
				if forced == nil || k.Kind < forced.Key.Kind || (k.Kind == forced.Key.Kind && k.Val < forced.Key.Val) {
					forced = &struct {
						Key severityKey
						Txt string
					}{Key: k, Txt: forcedTxt}
				}
			}
		}
	}

	sort.Slice(cands, func(i, j int) bool {
		if cands[i].Severity.Kind != cands[j].Severity.Kind {
			return cands[i].Severity.Kind < cands[j].Severity.Kind
		}
		return cands[i].Severity.Val < cands[j].Severity.Val
	})

	chosen := cands
	if maxGroups >= 0 && len(chosen) > maxGroups {
		chosen = chosen[:maxGroups]
	}

	if maxSubs > 0 {
		for i := range chosen {
			if strings.Contains(chosen[i].Main, "#") {
				chosen[i].Subs = nil
				continue
			}
			ap := chosen[i].AfterPunish
			if ap == nil || len(ap.ValidMoves()) == 0 {
				continue
			}
			if ap.Turn() != victimColor {
				continue
			}
			subMoves := ap.ValidMoves()
			sort.Slice(subMoves, func(a, b int) bool {
				pa, ua := blunderPriority(ap, subMoves[a], victimColor)
				pb, ub := blunderPriority(ap, subMoves[b], victimColor)
				if pa != pb {
					return pa < pb
				}
				return ua < ub
			})

			subs := []string{}
			limit := max(10, maxSubs*6)
			for j, sm := range subMoves {
				if j >= limit {
					break
				}
				suci := (chess.UCINotation{}).Encode(ap, sm)
				bsub := ap.Update(sm)

				m1 := mateInOne(bsub)
				if m1 != nil {
					seq := []string{suci, (chess.UCINotation{}).Encode(bsub, m1)}
					txt := pvNumberedAutograded(e, ap, seq, len(seq), nil, cache)
					subs = append(subs, txt)
					if len(subs) >= maxSubs {
						break
					}
					continue
				}

				if !hasCheckOrCapture(bsub) {
					continue
				}

				info2, err := analyseCachedSingle(e, bsub.String(), maxDuration(60*time.Millisecond, engineTime), cache)
				if err != nil || len(info2.PV) == 0 {
					continue
				}
				cp2 := scoreToCP(info2.Score, fromChessColor(bsub.Turn()), fromChessColor(bsub.Turn()))
				mate2 := 0
				if info2.Score.IsMate {
					mate2 = info2.Score.Mate
				}
				if (mate2 <= 0) && cp2 < 220 {
					continue
				}

				follow2 := []string{}
				t2 := bsub
				for k := 0; k < len(info2.PV) && k < max(1, pvPlies-1); k++ {
					u := info2.PV[k]
					mv, err := (chess.UCINotation{}).Decode(t2, u)
					if err != nil {
						break
					}
					follow2 = append(follow2, u)
					t2 = t2.Update(mv)
				}
				seq := append([]string{suci}, follow2...)
				txt := pvNumberedAutograded(e, ap, seq, len(seq), nil, cache)
				subs = append(subs, txt)
				if len(subs) >= maxSubs {
					break
				}
			}
			chosen[i].Subs = subs
		}
	}

	out := []DangerGroup{}
	if forced != nil {
		out = append(out, DangerGroup{Main: strings.TrimSpace(forced.Txt), Subs: nil})
	}
	for _, c := range chosen {
		out = append(out, DangerGroup{Main: strings.TrimSpace(c.Main), Subs: c.Subs})
	}

	seen := map[string]struct{}{}
	final := []DangerGroup{}
	for _, g := range out {
		main := strings.TrimSpace(g.Main)
		if main == "" {
			continue
		}
		if _, ok := seen[main]; ok {
			continue
		}
		seen[main] = struct{}{}
		subs := []string{}
		for _, s := range g.Subs {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if _, ok := seen[s]; ok {
				continue
			}
			subs = append(subs, s)
			seen[s] = struct{}{}
			if len(subs) >= max(0, maxSubs) {
				break
			}
		}
		final = append(final, DangerGroup{Main: main, Subs: subs})
	}

	return final
}

func printDangersGrouped(dangers []DangerGroup, labelTxt string, globalSeen map[string]struct{}, maxDangerSubs int) {
	if len(dangers) == 0 {
		return
	}
	seen := globalSeen
	if seen == nil {
		seen = map[string]struct{}{}
	}
	firstPrinted := false
	for _, item := range dangers {
		main := strings.TrimSpace(item.Main)
		if main == "" {
			continue
		}
		if _, ok := seen[main]; ok {
			continue
		}
		prefix := "  " + label(labelTxt+":") + " "
		if firstPrinted {
			prefix = "  " + strings.Repeat(" ", len(labelTxt)+2)
		}
		fmt.Println(prefix + main)
		seen[main] = struct{}{}
		firstPrinted = true

		for i, sub := range item.Subs {
			if i >= maxDangerSubs {
				break
			}
			sub = strings.TrimSpace(sub)
			if sub == "" {
				continue
			}
			if _, ok := seen[sub]; ok {
				continue
			}
			fmt.Println("    ↳ " + sub)
			seen[sub] = struct{}{}
		}
	}
}

func ptr[T any](v T) *T { return &v }

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
