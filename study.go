package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/notnil/chess"
)

func descQualidade(avaliacao string) string {
	switch strings.TrimSpace(avaliacao) {
	case "?!":
		return "Imprecisão"
	case "?":
		return "Erro"
	case "#":
		return "Xeque-Mate"
	case "!!":
		return "Brilhante"
	case "!":
		return "Ótimo"
	case "★":
		return "Melhor"
	case "??":
		return "Gafe"
	default:
		return "Indisponível"
	}
}

func fmtCP(cp int) string {
	return formatCP(cp)
}

func mateFromPov(score UCIScore, posTurn Color, pov Color) (int, bool) {
	if !score.IsMate {
		return 0, false
	}
	m := score.Mate
	if posTurn != pov {
		m = -m
	}
	return m, true
}

func analyseSortedPov(e *UCIEdge, fen string, t time.Duration, multipv int, pov Color) ([]UCIAnalysisLine, error) {
	lines, err := e.AnalyseFEN(fen, t, multipv)
	if err != nil {
		return nil, err
	}
	posTurn := fenTurn(fen)
	sort.Slice(lines, func(i, j int) bool {
		ai := scoreToCP(lines[i].Score, posTurn, pov)
		aj := scoreToCP(lines[j].Score, posTurn, pov)
		return ai > aj
	})
	return lines, nil
}

func fenTurn(fen string) Color {
	parts := strings.Fields(fen)
	if len(parts) >= 2 {
		if parts[1] == "b" {
			return Black
		}
	}
	return White
}

func refineBorderlineIfNeeded(e *UCIEdge, fenBefore string, fenAfter string, cfg Config, pov Color, grade string, cpLoss int, wpLoss float64, bestCP int, bestWP float64, playedCP int) (string, int, float64, int, UCIAnalysisLine) {
	ply := fenPly(fenBefore)
	if ply > 10 {
		l := UCIAnalysisLine{Score: UCIScore{CP: 0}}
		if cfg.Deterministic {
			l, _ = analyseCachedSingleDepth(e, fenAfter, cfg.DepthMove, pvCache)
		} else {
			l, _ = analyseCachedSingle(e, fenAfter, cfg.AnalysisTimeMove, pvCache)
		}
		return grade, bestCP, bestWP, playedCP, l
	}

	if grade != "?!" && grade != "?" {
		l := UCIAnalysisLine{Score: UCIScore{CP: 0}}
		if cfg.Deterministic {
			l, _ = analyseCachedSingleDepth(e, fenAfter, cfg.DepthMove, pvCache)
		} else {
			l, _ = analyseCachedSingle(e, fenAfter, cfg.AnalysisTimeMove, pvCache)
		}
		return grade, bestCP, bestWP, playedCP, l
	}
	borderlineCP := cpLoss >= 95 && cpLoss <= 170
	borderlineWP := wpLoss >= 0.040 && wpLoss <= 0.085
	if !borderlineCP && !borderlineWP {
		l := UCIAnalysisLine{Score: UCIScore{CP: 0}}
		if cfg.Deterministic {
			l, _ = analyseCachedSingleDepth(e, fenAfter, cfg.DepthMove, pvCache)
		} else {
			l, _ = analyseCachedSingle(e, fenAfter, cfg.AnalysisTimeMove, pvCache)
		}
		return grade, bestCP, bestWP, playedCP, l
	}

	// Deepen analysis on borderline cases.
	bestInfo2 := []UCIAnalysisLine(nil)
	var err error
	if cfg.Deterministic {
		bestInfo2, err = analyseCachedMultiDepth(e, fenBefore, cfg.DepthDeep, 3, multiPVCache)
	} else {
		t := maxDuration(1600*time.Millisecond, 2*cfg.AnalysisTimeMove)
		bestInfo2, err = analyseSortedPov(e, fenBefore, t, 3, pov)
	}
	if err != nil || len(bestInfo2) == 0 {
		l := UCIAnalysisLine{Score: UCIScore{CP: 0}}
		if cfg.Deterministic {
			l, _ = analyseCachedSingleDepth(e, fenAfter, cfg.DepthMove, pvCache)
		} else {
			l, _ = analyseCachedSingle(e, fenAfter, cfg.AnalysisTimeMove, pvCache)
		}
		return grade, bestCP, bestWP, playedCP, l
	}
	played2 := UCIAnalysisLine{Score: UCIScore{CP: 0}}
	if cfg.Deterministic {
		played2, err = analyseCachedSingleDepth(e, fenAfter, cfg.DepthDeep, pvCache)
	} else {
		t := maxDuration(1600*time.Millisecond, 2*cfg.AnalysisTimeMove)
		played2, err = analyseCachedSingle(e, fenAfter, t, pvCache)
	}
	if err != nil {
		l := UCIAnalysisLine{Score: UCIScore{CP: 0}}
		if cfg.Deterministic {
			l, _ = analyseCachedSingleDepth(e, fenAfter, cfg.DepthMove, pvCache)
		} else {
			l, _ = analyseCachedSingle(e, fenAfter, cfg.AnalysisTimeMove, pvCache)
		}
		return grade, bestCP, bestWP, playedCP, l
	}

	bestCP2 := scoreToCP(bestInfo2[0].Score, pov, pov)
	bestWP2 := winProbFromCP(bestCP2)
	playedCP2 := scoreToCP(played2.Score, fenTurn(fenAfter), pov)

	cpLoss2 := bestCP2 - playedCP2
	if cpLoss2 < 0 {
		cpLoss2 = 0
	}
	wpLoss2 := bestWP2 - winProbFromCP(playedCP2)
	if wpLoss2 < 0 {
		wpLoss2 = 0
	}
	grade2 := classifyMove(cpLoss2, wpLoss2)
	return grade2, bestCP2, bestWP2, playedCP2, played2
}

func fenPly(fen string) int {
	// halfmove count not present; approximate by parsing fullmove + turn.
	parts := strings.Fields(fen)
	if len(parts) < 6 {
		return 0
	}
	full := 1
	_, _ = fmt.Sscanf(parts[5], "%d", &full)
	turn := parts[1]
	ply := (full - 1) * 2
	if turn == "b" {
		ply++
	}
	if ply < 0 {
		return 0
	}
	return ply
}

func materialBalance(pos *chess.Position, pov chess.Color) int {
	vals := map[chess.PieceType]int{
		chess.Pawn:   1,
		chess.Knight: 3,
		chess.Bishop: 3,
		chess.Rook:   5,
		chess.Queen:  9,
		chess.King:   0,
	}
	us := 0
	them := 0
	b := pos.Board()
	for _, p := range b.SquareMap() {
		if p == chess.NoPiece {
			continue
		}
		v := vals[p.Type()]
		if p.Color() == pov {
			us += v
		} else {
			them += v
		}
	}
	return us - them
}

func detectBrilliantBySacrifice(posBefore *chess.Position, moveObj *chess.Move, playedInfo UCIAnalysisLine, playedCP int, grade string) bool {
	if grade != "★" && grade != "!" {
		return false
	}
	pov := posBefore.Turn()
	posAfter := posBefore.Update(moveObj)
	mate, ok := mateFromPov(playedInfo.Score, fromChessColor(posAfter.Turn()), fromChessColor(pov))
	isMateForPov := ok && mate > 0
	if !isMateForPov && playedCP < 200 {
		return false
	}
	if len(playedInfo.PV) == 0 {
		return false
	}

	temp := posAfter
	for i := 0; i < 2 && i < len(playedInfo.PV); i++ {
		mv, err := (chess.UCINotation{}).Decode(temp, playedInfo.PV[i])
		if err != nil {
			break
		}
		if !moveInValidMoves(temp, mv) {
			break
		}
		temp = temp.Update(mv)
	}

	bal0 := materialBalance(posBefore, pov)
	bal1 := materialBalance(temp, pov)
	return bal1 <= bal0-3
}

func verifyBrilliantInevitable(e *UCIEdge, posAfter *chess.Position, pov Color, cfg Config) bool {
	defesas, err := e.AnalyseFEN(posAfter.String(), maxDuration(250*time.Millisecond, cfg.AnalysisTimeMove), 3)
	if err != nil {
		return false
	}
	piores := (*int)(nil)
	for _, linha := range defesas {
		if len(linha.PV) == 0 {
			continue
		}
		def := linha.PV[0]
		dm, err := (chess.UCINotation{}).Decode(posAfter, def)
		if err != nil {
			continue
		}
		if !moveInValidMoves(posAfter, dm) {
			continue
		}
		afterDef := posAfter.Update(dm)
		resp, err := analyseCachedSingle(e, afterDef.String(), maxDuration(250*time.Millisecond, cfg.AnalysisTimeMove), pvCache)
		if err != nil {
			continue
		}
		cp := scoreToCP(resp.Score, fromChessColor(afterDef.Turn()), pov)
		if piores == nil {
			piores = new(int)
			*piores = cp
		} else if cp < *piores {
			*piores = cp
		}
	}
	if piores == nil {
		return false
	}
	return *piores >= 180
}

func moveInValidMoves(pos *chess.Position, mv *chess.Move) bool {
	for _, m := range pos.ValidMoves() {
		if m.S1() == mv.S1() && m.S2() == mv.S2() && m.Promo() == mv.Promo() {
			return true
		}
	}
	return false
}

func parseMoveInput(pos *chess.Position, raw string) (*chess.Move, string, string, error) {
	in := strings.TrimSpace(raw)
	if in == "" {
		return nil, "", "", fmt.Errorf("lance vazio")
	}
	// Try UCI first (like older Go version), then SAN.
	if len(in) == 4 || len(in) == 5 {
		mv, err := (chess.UCINotation{}).Decode(pos, in)
		if err == nil {
			san := (chess.AlgebraicNotation{}).Encode(pos, mv)
			uci := (chess.UCINotation{}).Encode(pos, mv)
			return mv, uci, san, nil
		}
	}
	mv, err := (chess.AlgebraicNotation{}).Decode(pos, in)
	if err != nil {
		return nil, "", "", err
	}
	uci := (chess.UCINotation{}).Encode(pos, mv)
	san := (chess.AlgebraicNotation{}).Encode(pos, mv)
	return mv, uci, san, nil
}

func legalMovesPythonList(pos *chess.Position) string {
	moves := pos.ValidMoves()
	not := chess.AlgebraicNotation{}
	parts := make([]string, 0, len(moves))
	for _, m := range moves {
		parts = append(parts, "'"+not.Encode(pos, m)+"'")
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

type MoveEval struct {
	Grade      string
	BestCP     int
	BestWP     float64
	PlayedCP   int
	PlayedInfo UCIAnalysisLine
	CPDiff     int
	WPDiff     float64
}

func evalMoveLikePython(e *UCIEdge, posBefore *chess.Position, posAfter *chess.Position, moveUCI string, cfg Config) (MoveEval, error) {
	pov := fromChessColor(posBefore.Turn())
	bestInfo := []UCIAnalysisLine(nil)
	var err error
	if cfg.Deterministic {
		bestInfo, err = analyseCachedMultiDepth(e, posBefore.String(), cfg.DepthMove, 5, multiPVCache)
	} else {
		bestInfo, err = analyseSortedPov(e, posBefore.String(), cfg.AnalysisTimeMove, 5, pov)
	}
	if err != nil || len(bestInfo) == 0 {
		bestInfo = []UCIAnalysisLine{{Score: UCIScore{CP: 0}}}
	}
	bestCP := scoreToCP(bestInfo[0].Score, pov, pov)
	bestWP := winProbFromCP(bestCP)

	playedInfo := UCIAnalysisLine{Score: UCIScore{CP: 0}}
	if cfg.Deterministic {
		playedInfo, err = analyseCachedSingleDepth(e, posAfter.String(), cfg.DepthMove, pvCache)
	} else {
		playedInfo, err = analyseCachedSingle(e, posAfter.String(), cfg.AnalysisTimeMove, pvCache)
	}
	if err != nil {
		playedInfo = UCIAnalysisLine{Score: UCIScore{CP: 0}}
	}
	playedCP := scoreToCP(playedInfo.Score, fromChessColor(posAfter.Turn()), pov)

	cpLoss := bestCP - playedCP
	if cpLoss < 0 {
		cpLoss = 0
	}
	wpLoss := bestWP - winProbFromCP(playedCP)
	if wpLoss < 0 {
		wpLoss = 0
	}
	grade := classificar_lance(cpLoss, wpLoss)

	grade, bestCP, bestWP, playedCP, playedInfo = refineBorderlineIfNeeded(e, posBefore.String(), posAfter.String(), cfg, pov, grade, cpLoss, wpLoss, bestCP, bestWP, playedCP)

	// Hard mate rules.
	bestMate, bestIsMate := mateFromPov(bestInfo[0].Score, pov, pov)
	playedMate, playedIsMate := mateFromPov(playedInfo.Score, fromChessColor(posAfter.Turn()), pov)

	if playedIsMate {
		if playedMate < 0 && (-playedMate) <= 6 {
			grade = "??"
		}
	}
	if bestIsMate {
		if bestMate > 0 && bestMate <= 4 {
			if !playedIsMate || playedMate <= 0 || playedMate > bestMate {
				grade = "??"
			}
		}
	}

	ply := fenPly(posBefore.String())
	grade = adjustOpeningGrade(ply, grade, cpLoss)
	grade = forceEarlyF3AsMistake(cfg, ply, moveUCI, grade)

	// Brilliant by sacrifice.
	mv, err := (chess.UCINotation{}).Decode(posBefore, moveUCI)
	if err == nil {
		if detectBrilliantBySacrifice(posBefore, mv, playedInfo, playedCP, grade) {
			if verifyBrilliantInevitable(e, posAfter, pov, cfg) {
				grade = "!!"
			}
		}
	}

	return MoveEval{Grade: grade, BestCP: bestCP, BestWP: bestWP, PlayedCP: playedCP, PlayedInfo: playedInfo, CPDiff: cpLoss, WPDiff: wpLoss}, nil
}

// Python naming parity (helper kept local).
func classificar_lance(cpLoss int, wpLoss float64) string { return classifyMove(cpLoss, wpLoss) }

func alertaTaticaPosLance(posAfter *chess.Position, infoAfter UCIAnalysisLine, pov Color, contexto string, cfg Config) {
	score := infoAfter.Score
	pv := infoAfter.PV
	if len(pv) == 0 {
		return
	}

	cp := scoreToCP(score, fromChessColor(posAfter.Turn()), pov)
	mate, isMate := mateFromPov(score, fromChessColor(posAfter.Turn()), pov)
	pvTxt := pvEmSANNumbered(posAfter, pv, min(cfg.PVPliesMain, 8))

	prefix := ""
	if strings.TrimSpace(contexto) != "" {
		prefix = strings.TrimSpace(contexto) + ": "
	}

	if isMate && mate < 0 {
		fmt.Printf("  %s %smate contra você em %d: %s\n", label("Perigo:"), prefix, -mate, pvTxt)
		return
	}
	if cp <= -250 {
		fmt.Printf("  %s %stática forte (%s): %s\n", label("Perigo:"), prefix, fmtCP(cp), pvTxt)
		return
	}
	if cp <= -120 {
		fmt.Printf("  %s %ssequência forte (%s): %s\n", label("Cuidado:"), prefix, fmtCP(cp), pvTxt)
	}
}

func analisarSeuLance(e *UCIEdge, posBefore *chess.Position, moveJogadoSAN string, posAfter *chess.Position, cfg Config) {
	infoCont, err := analyseSortedPov(e, posAfter.String(), cfg.AnalysisTimeSuggest, max(1, cfg.ShowMainLine), fromChessColor(posAfter.Turn()))
	if err != nil || len(infoCont) == 0 {
		return
	}
	bestCPTurn := scoreToCP(infoCont[0].Score, fromChessColor(posAfter.Turn()), fromChessColor(posAfter.Turn()))
	bestWPTurn := winProbFromCP(bestCPTurn)
	povMe := opposite(fromChessColor(posAfter.Turn()))

	titulo(fmt.Sprintf("Linhas principais (%d)", cfg.ShowMainLine))

	var seenMain map[string]struct{}
	if cfg.DedupDangersAcrossLines {
		seenMain = map[string]struct{}{}
	}

	maxLines := cfg.ShowMainLine
	if maxLines < 1 {
		maxLines = 1
	}
	for i, linha := range infoCont {
		if i >= maxLines {
			break
		}
		pv := linha.PV
		if len(pv) == 0 {
			continue
		}
		cp := scoreToCP(linha.Score, fromChessColor(posAfter.Turn()), povMe)
		labelTxt := "Sólida"
		if i != 0 {
			labelTxt = fmt.Sprintf("Opção %d", i+1)
		}
		lineCPTurn := scoreToCP(linha.Score, fromChessColor(posAfter.Turn()), fromChessColor(posAfter.Turn()))
		perdaCP := bestCPTurn - lineCPTurn
		if perdaCP < 0 {
			perdaCP = 0
		}
		wpLoss := bestWPTurn - winProbFromCP(lineCPTurn)
		if wpLoss < 0 {
			wpLoss = 0
		}
		qual := classificar_lance(perdaCP, wpLoss)
		pvTxt := pvNumberedAutograded(e, posAfter, pv, cfg.PVPliesMain, &qual, pvCache)
		fmt.Printf("- %s (%s): %s\n", labelTxt, fmtCP(cp), pvTxt)

		dangers := dangerVariantsGrouped(
			e,
			posAfter,
			pv,
			toChessColor(povMe),
			1,
			cfg.MaxDangers,
			cfg.MaxDangerSubs,
			18,
			80*time.Millisecond,
			240,
			cfg.PVPliesMain,
			nil,
			pvCache,
		)
		printDangersGrouped(dangers, "Perigo", seenMain, cfg.MaxDangerSubs)
	}

	pov := fromChessColor(posBefore.Turn())
	altT := cfg.AnalysisTimeSuggest
	if altT < 1500*time.Millisecond {
		altT = 1500 * time.Millisecond
	}
	infoAlt, err := analyseSortedPov(e, posBefore.String(), altT, 5, pov)
	if err != nil || len(infoAlt) == 0 {
		return
	}
	melhorCP := scoreToCP(infoAlt[0].Score, pov, pov)
	melhorWP := winProbFromCP(melhorCP)

	type altEntry struct {
		SAN   string
		PV    []string
		Qual  string
		Score UCIScore
	}
	melhores := []altEntry{}
	for _, linha := range infoAlt {
		pv := linha.PV
		if len(pv) == 0 {
			continue
		}
		mv, err := (chess.UCINotation{}).Decode(posBefore, pv[0])
		if err != nil {
			continue
		}
		san := (chess.AlgebraicNotation{}).Encode(posBefore, mv)
		if san == moveJogadoSAN {
			continue
		}
		lineCP := scoreToCP(linha.Score, pov, pov)
		perda := melhorCP - lineCP
		wpLoss := melhorWP - winProbFromCP(lineCP)
		qual := classificar_lance(perda, wpLoss)
		if qual == "!" || qual == "★" {
			melhores = append(melhores, altEntry{SAN: san, PV: pv, Qual: qual, Score: linha.Score})
		}
	}
	if len(melhores) == 0 {
		return
	}

	titulo("Outras opções")
	var seenAlt map[string]struct{}
	if cfg.DedupDangersAcrossLines {
		seenAlt = map[string]struct{}{}
	}
	for i, m := range melhores {
		if i >= cfg.ShowBetterMoves {
			break
		}
		cp := scoreToCP(m.Score, pov, pov)
		q := gradeForDisplay(m.Qual)
		qtxt := ""
		if q != "" {
			qtxt = " " + q
		}
		pvTxt := pvNumberedAutograded(e, posBefore, m.PV, cfg.PVPliesMain, &m.Qual, pvCache)
		fmt.Printf("- %s%s (%s): %s\n", m.SAN, qtxt, fmtCP(cp), pvTxt)

		dangers := dangerVariantsGrouped(
			e,
			posBefore,
			m.PV,
			toChessColor(pov),
			1,
			cfg.MaxDangers,
			cfg.MaxDangerSubs,
			18,
			80*time.Millisecond,
			240,
			cfg.PVPliesMain,
			nil,
			pvCache,
		)
		printDangersGrouped(dangers, "Perigo", seenAlt, cfg.MaxDangerSubs)
	}
}

func mostrarSugestoes(e *UCIEdge, st *State, pos *chess.Position, cfg Config) {
	pov := fromChessColor(pos.Turn())
	info, err := analyseSortedPov(e, pos.String(), cfg.AnalysisTimeSuggest, cfg.SuggestMultiPV, pov)
	if err != nil || len(info) == 0 {
		return
	}
	melhorCP := scoreToCP(info[0].Score, pov, pov)
	melhorWP := winProbFromCP(melhorCP)

	type sugEntry struct {
		SAN   string
		PV    []string
		Qual  string
		Score UCIScore
		UCI   string
	}
	byUCI := map[string]sugEntry{}
	melhores := []sugEntry{}
	for _, linha := range info {
		pv := linha.PV
		if len(pv) == 0 {
			continue
		}
		mv, err := (chess.UCINotation{}).Decode(pos, pv[0])
		if err != nil {
			continue
		}
		san := (chess.AlgebraicNotation{}).Encode(pos, mv)
		if !strings.Contains(san, "#") {
			after := pos.Update(mv)
			if isCheckmate(after) {
				san += "#"
			}
		}
		lineCP := scoreToCP(linha.Score, pov, pov)
		perda := melhorCP - lineCP
		if perda < 0 {
			perda = 0
		}
		wpLoss := melhorWP - winProbFromCP(lineCP)
		if wpLoss < 0 {
			wpLoss = 0
		}
		qual := classificar_lance(perda, wpLoss)
		entry := sugEntry{SAN: san, PV: pv, Qual: qual, Score: linha.Score, UCI: pv[0]}
		if entry.UCI != "" {
			byUCI[strings.ToLower(entry.UCI)] = entry
		}
		if qual == "!" || qual == "★" {
			melhores = append(melhores, entry)
		}
	}

	titulo("Respostas interessantes do adversário")
	var seen map[string]struct{}
	if cfg.DedupDangersAcrossLines {
		seen = map[string]struct{}{}
	}
	for i, m := range melhores {
		if i >= cfg.ShowOpponentBest {
			break
		}
		pvTxt := pvNumberedAutograded(e, pos, m.PV, cfg.PVPliesMain, &m.Qual, pvCache)
		q := gradeForDisplay(m.Qual)
		qtxt := ""
		if q != "" {
			qtxt = " " + q
		}
		note := ""
		if st != nil {
			if tags := openingTagsForNextMove(st.MovesUCI, m.UCI); len(tags) > 0 {
				note = " (Possível " + tags[0] + ")"
			}
		}
		fmt.Printf("- %s%s%s | %s\n", m.SAN, qtxt, note, pvTxt)

		dangers := dangerVariantsGrouped(
			e,
			pos,
			m.PV,
			toChessColor(pov),
			2,
			cfg.MaxDangers,
			cfg.MaxDangerSubs,
			18,
			80*time.Millisecond,
			240,
			cfg.PVPliesMain,
			nil,
			pvCache,
		)
		printDangersGrouped(dangers, "Perigo", seen, cfg.MaxDangerSubs)
	}

	exclude := map[string]struct{}{}
	for i := 0; i < len(melhores) && i < cfg.ShowOpponentBest; i++ {
		if melhores[i].UCI != "" {
			exclude[melhores[i].UCI] = struct{}{}
		}
	}

	preview := dangerVariantsGrouped(
		e,
		pos,
		nil,
		toChessColor(pov),
		0,
		cfg.OpponentMistakesPreviewMax,
		0,
		22,
		100*time.Millisecond,
		180,
		cfg.PVPliesMain,
		exclude,
		pvCache,
	)

	// Gambits the opponent may choose here (without repeating the already-listed best replies).
	if st != nil {
		excludeMoves := map[string]struct{}{}
		for i := 0; i < len(melhores) && i < cfg.ShowOpponentBest; i++ {
			if melhores[i].UCI != "" {
				excludeMoves[strings.ToLower(melhores[i].UCI)] = struct{}{}
			}
		}
		kind := KindGambit
		cands := lineNextCandidates(st.MovesUCI, pos.Turn(), &kind)
		printed := 0
		seenCand := map[string]struct{}{}
		nextPly := len(normalizeHistory(st.MovesUCI)) + 1
		for _, c := range cands {
			if tagPly(c.Def) != nextPly {
				continue
			}
			if _, ok := excludeMoves[strings.ToLower(c.NextMoveUCI)]; ok {
				continue
			}
			k := strings.ToLower(c.Def.Key + ":" + c.NextMoveUCI)
			if _, ok := seenCand[k]; ok {
				continue
			}
			seenCand[k] = struct{}{}
			mv, err := (chess.UCINotation{}).Decode(pos, c.NextMoveUCI)
			if err != nil || !moveInValidMoves(pos, mv) {
				continue
			}
			san := (chess.AlgebraicNotation{}).Encode(pos, mv)
			if printed == 0 {
				titulo("Gambitos prováveis do adversário")
			}
			printed++
			// If engine already analysed this move in MultiPV, reuse its evaluation/PV.
			q := ""
			cpTxt := ""
			pvTxt := ""
			reacaoTxt := ""
			if ent, ok := byUCI[strings.ToLower(c.NextMoveUCI)]; ok {
				q = gradeForDisplay(ent.Qual)
				if q != "" {
					q = " " + q
				}
				cp := scoreToCP(ent.Score, pov, pov)
				cpTxt = " (" + fmtCP(cp) + ")"
				pvTxt = pvEmSANNumbered(pos, ent.PV, min(cfg.PVPliesMain, 8))
				// Reaction = best reply by the other side (ply 2 of PV).
				if len(ent.PV) >= 2 {
					posAfterGambit := pos.Update(mv)
					rmv, err := (chess.UCINotation{}).Decode(posAfterGambit, ent.PV[1])
					if err == nil && moveInValidMoves(posAfterGambit, rmv) {
						reacaoTxt = (chess.AlgebraicNotation{}).Encode(posAfterGambit, rmv)
					}
				}
			}
			risk := formatRiskTags(gambitRiskTags(pos, c.NextMoveUCI))
			line := "- " + san + q + " — " + c.Def.NamePT + cpTxt
			if risk != "" {
				line += " | " + risk
			}
			fmt.Println(line)
			if strings.TrimSpace(reacaoTxt) != "" {
				fmt.Printf("  Reaja com: %s\n", reacaoTxt)
			}
			if pvTxt != "" {
				fmt.Printf("  Linha: %s\n", pvTxt)
			}
			if printed >= 6 {
				break
			}
		}
	}

	if len(preview) > 0 {
		titulo("Se o adversário errar (como punir)")
		for i, item := range preview {
			if i >= cfg.OpponentMistakesPreviewMax {
				break
			}
			fmt.Printf("- %s\n", item.Main)
		}
	}
}

func opposite(c Color) Color {
	if c == White {
		return Black
	}
	return White
}
