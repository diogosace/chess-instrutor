package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/notnil/chess"
)

const MateScore = 100000

func winProbFromCP(cp int) float64 {
	k := 0.0026
	if cp > MateScore {
		cp = MateScore
	}
	if cp < -MateScore {
		cp = -MateScore
	}
	return 1.0 / (1.0 + math.Exp(-k*float64(cp)))
}

func scoreToCP(s UCIScore, posTurn Color, pov Color) int {
	// Convert UCI score (from side-to-move) to POV.
	sign := 1
	if posTurn != pov {
		sign = -1
	}
	if s.IsMate {
		m := s.Mate
		if m == 0 {
			return 0
		}
		if m > 0 {
			return sign * (MateScore - m)
		}
		return sign * (-MateScore - m)
	}
	return sign * s.CP
}

func classifyMove(cpLoss int, wpLoss float64) string {
	if cpLoss < 0 {
		cpLoss = 0
	}
	if wpLoss < 0 {
		wpLoss = 0
	}
	// Hybrid thresholds
	if cpLoss <= 15 || wpLoss <= 0.010 {
		return "★"
	}
	if cpLoss <= 50 || wpLoss <= 0.025 {
		return "!"
	}
	if cpLoss <= 120 || wpLoss <= 0.055 {
		return "?!"
	}
	if cpLoss <= 250 || wpLoss <= 0.120 {
		return "?"
	}
	return "??"
}

func gradeForDisplay(g string) string {
	g = strings.TrimSpace(g)
	if g == "★" {
		return ""
	}
	return g
}

func annotateMove(san string, grade string) string {
	g := strings.TrimSpace(grade)
	if g == "" || g == "★" {
		return san
	}
	if strings.HasSuffix(san, "#") {
		return san
	}
	if g == "#" {
		return san
	}
	return san + g
}

func adjustOpeningGrade(ply int, grade string, cpLoss int) string {
	if ply <= 8 && grade == "!" && cpLoss <= 35 {
		return "★"
	}
	return grade
}

func forceEarlyF3AsMistake(cfg Config, ply int, uci string, grade string) string {
	if !cfg.ForceEarlyF3AsMistake {
		return grade
	}
	if ply > 2 {
		return grade
	}
	if uci == "f2f3" || uci == "f7f6" {
		if grade == "★" || grade == "!" || grade == "?!" {
			return "?"
		}
	}
	return grade
}

type Line struct {
	SAN   string
	PV    []string // uci
	G     string
	CPPOV int
}

func analyseSorted(e *UCIEdge, fen string, movetime time.Duration, multipv int) ([]UCIAnalysisLine, error) {
	lines, err := analyseCachedMulti(e, fen, movetime, multipv, multiPVCache)
	if err != nil {
		return nil, err
	}
	return lines, nil
}

func pvNumberedAutograded(e *UCIEdge, pos *chess.Position, pvUCI []string, maxPlies int, firstGradeOverride *string, cache *AnalyseCache) string {
	temp := pos
	out := []string{}
	grades := gradesForPV(e, pos, pvUCI, maxPlies, cache)
	idx := 0
	fullmove := fenFullmove(temp.String())

	for _, u := range pvUCI {
		if idx >= maxPlies {
			break
		}
		mv, err := (chess.UCINotation{}).Decode(temp, u)
		if err != nil {
			break
		}
		san := (chess.AlgebraicNotation{}).Encode(temp, mv)
		after := temp.Update(mv)
		if !strings.Contains(san, "#") && isCheckmate(after) {
			san += "#"
		}
		g := ""
		if idx < len(grades) {
			g = grades[idx]
		}
		if idx == 0 && firstGradeOverride != nil {
			g = *firstGradeOverride
		}
		tok := annotateMove(san, g)

		turn := temp.Turn()
		if turn == chess.White {
			out = append(out, fmt.Sprintf("%d. %s", fullmove, tok))
		} else {
			if len(out) == 0 {
				out = append(out, fmt.Sprintf("%d... %s", fullmove, tok))
			} else {
				out = append(out, tok)
			}
		}

		temp = after
		if turn == chess.Black {
			fullmove++
		}
		idx++
	}
	return strings.Join(out, " ")
}

func fenFullmove(fen string) int {
	parts := strings.Fields(fen)
	if len(parts) < 6 {
		return 1
	}
	// last field is fullmove number
	n := 1
	_, _ = fmt.Sscanf(parts[5], "%d", &n)
	if n <= 0 {
		return 1
	}
	return n
}

type AnalyseCache struct {
	m map[string]UCIAnalysisLine
}

func NewAnalyseCache() *AnalyseCache { return &AnalyseCache{m: map[string]UCIAnalysisLine{}} }

func (c *AnalyseCache) key(fen string, ms int64) string { return fmt.Sprintf("%s|%d", fen, ms) }

func (c *AnalyseCache) Get(fen string, ms int64) (UCIAnalysisLine, bool) {
	l, ok := c.m[c.key(fen, ms)]
	return l, ok
}

func (c *AnalyseCache) Put(fen string, ms int64, l UCIAnalysisLine) {
	if len(c.m) > 4000 {
		c.m = map[string]UCIAnalysisLine{}
	}
	c.m[c.key(fen, ms)] = l
}

type MultiPVCache struct {
	m map[string][]UCIAnalysisLine
}

func NewMultiPVCache() *MultiPVCache { return &MultiPVCache{m: map[string][]UCIAnalysisLine{}} }

func (c *MultiPVCache) key(fen string, ms int64, multipv int) string {
	return fmt.Sprintf("%s|%d|%d", fen, ms, multipv)
}

func (c *MultiPVCache) Get(fen string, ms int64, multipv int) ([]UCIAnalysisLine, bool) {
	v, ok := c.m[c.key(fen, ms, multipv)]
	return v, ok
}

func (c *MultiPVCache) Put(fen string, ms int64, multipv int, v []UCIAnalysisLine) {
	if len(c.m) > 1200 {
		c.m = map[string][]UCIAnalysisLine{}
	}
	// store a shallow copy
	out := make([]UCIAnalysisLine, len(v))
	copy(out, v)
	c.m[c.key(fen, ms, multipv)] = out
}

var multiPVCache = NewMultiPVCache()

var engineCacheFingerprint string

func analyseCachedMulti(e *UCIEdge, fen string, movetime time.Duration, multipv int, cache *MultiPVCache) ([]UCIAnalysisLine, error) {
	ms := movetime.Milliseconds()
	if cache != nil {
		if v, ok := cache.Get(fen, ms, multipv); ok {
			return v, nil
		}
	}
	if diskCache != nil && engineCacheFingerprint != "" {
		key := cacheKey(engineCacheFingerprint, fen, "t", ms, multipv)
		if v, ok := diskCache.GetMulti(key); ok {
			if cache != nil {
				cache.Put(fen, ms, multipv, v)
			}
			return v, nil
		}
	}
	lines, err := e.AnalyseFEN(fen, movetime, multipv)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		lines = []UCIAnalysisLine{{MultiPV: 1, Score: UCIScore{CP: 0}, PV: nil}}
	}
	if cache != nil {
		cache.Put(fen, ms, multipv, lines)
	}
	if diskCache != nil && engineCacheFingerprint != "" {
		key := cacheKey(engineCacheFingerprint, fen, "t", ms, multipv)
		diskCache.PutMulti(key, lines)
	}
	return lines, nil
}

func analyseCachedMultiDepth(e *UCIEdge, fen string, depth int, multipv int, cache *MultiPVCache) ([]UCIAnalysisLine, error) {
	msKey := int64(-depth)
	if cache != nil {
		if v, ok := cache.Get(fen, msKey, multipv); ok {
			return v, nil
		}
	}
	if diskCache != nil && engineCacheFingerprint != "" {
		key := cacheKey(engineCacheFingerprint, fen, "d", msKey, multipv)
		if v, ok := diskCache.GetMulti(key); ok {
			if cache != nil {
				cache.Put(fen, msKey, multipv, v)
			}
			return v, nil
		}
	}
	lines, err := e.AnalyseFENDepth(fen, depth, multipv)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		lines = []UCIAnalysisLine{{MultiPV: 1, Score: UCIScore{CP: 0}, PV: nil}}
	}
	if cache != nil {
		cache.Put(fen, msKey, multipv, lines)
	}
	if diskCache != nil && engineCacheFingerprint != "" {
		key := cacheKey(engineCacheFingerprint, fen, "d", msKey, multipv)
		diskCache.PutMulti(key, lines)
	}
	return lines, nil
}

func analyseCachedSingle(e *UCIEdge, fen string, movetime time.Duration, cache *AnalyseCache) (UCIAnalysisLine, error) {
	ms := movetime.Milliseconds()
	if cache != nil {
		if l, ok := cache.Get(fen, ms); ok {
			return l, nil
		}
	}
	if diskCache != nil && engineCacheFingerprint != "" {
		key := cacheKey(engineCacheFingerprint, fen, "t", ms, 1)
		if l, ok := diskCache.GetSingle(key); ok {
			if cache != nil {
				cache.Put(fen, ms, l)
			}
			return l, nil
		}
	}
	lines, err := e.AnalyseFEN(fen, movetime, 1)
	if err != nil {
		return UCIAnalysisLine{}, err
	}
	if len(lines) == 0 {
		// Engine may occasionally return no info within very small time slices.
		l := UCIAnalysisLine{MultiPV: 1, Score: UCIScore{CP: 0}, PV: nil}
		return l, nil
	}
	l := lines[0]
	if cache != nil {
		cache.Put(fen, ms, l)
	}
	if diskCache != nil && engineCacheFingerprint != "" {
		key := cacheKey(engineCacheFingerprint, fen, "t", ms, 1)
		diskCache.PutSingle(key, l)
	}
	return l, nil
}

func analyseCachedSingleDepth(e *UCIEdge, fen string, depth int, cache *AnalyseCache) (UCIAnalysisLine, error) {
	msKey := int64(-depth)
	if cache != nil {
		if l, ok := cache.Get(fen, msKey); ok {
			return l, nil
		}
	}
	if diskCache != nil && engineCacheFingerprint != "" {
		key := cacheKey(engineCacheFingerprint, fen, "d", msKey, 1)
		if l, ok := diskCache.GetSingle(key); ok {
			if cache != nil {
				cache.Put(fen, msKey, l)
			}
			return l, nil
		}
	}
	lines, err := e.AnalyseFENDepth(fen, depth, 1)
	if err != nil {
		return UCIAnalysisLine{}, err
	}
	if len(lines) == 0 {
		l := UCIAnalysisLine{MultiPV: 1, Score: UCIScore{CP: 0}, PV: nil}
		return l, nil
	}
	l := lines[0]
	if cache != nil {
		cache.Put(fen, msKey, l)
	}
	if diskCache != nil && engineCacheFingerprint != "" {
		key := cacheKey(engineCacheFingerprint, fen, "d", msKey, 1)
		diskCache.PutSingle(key, l)
	}
	return l, nil
}

func gradesForPV(e *UCIEdge, pos *chess.Position, pvUCI []string, maxPlies int, cache *AnalyseCache) []string {
	// Similar to Python: grade each ply by comparing chosen move vs best.
	temp := pos
	positions := []*chess.Position{temp}
	moves := []string{}

	for _, u := range pvUCI {
		if len(moves) >= maxPlies {
			break
		}
		mv, err := (chess.UCINotation{}).Decode(temp, u)
		if err != nil {
			break
		}
		moves = append(moves, u)
		temp = temp.Update(mv)
		positions = append(positions, temp)
	}
	if len(moves) == 0 {
		return nil
	}

	infos := make([]UCIAnalysisLine, 0, len(positions))
	for _, p := range positions {
		l, err := analyseCachedSingle(e, p.String(), 50*time.Millisecond, cache)
		if err != nil {
			infos = append(infos, UCIAnalysisLine{Score: UCIScore{CP: 0}, PV: nil})
			continue
		}
		infos = append(infos, l)
	}

	grades := make([]string, 0, len(moves))
	for i := 0; i < len(moves); i++ {
		mover := fromChessColor(positions[i].Turn())
		best := infos[i].Score
		after := infos[i+1].Score
		bestCP := scoreToCP(best, mover, mover)
		playedCP := scoreToCP(after, fromChessColor(positions[i+1].Turn()), mover)
		cpLoss := bestCP - playedCP
		if cpLoss < 0 {
			cpLoss = 0
		}
		wpLoss := winProbFromCP(bestCP) - winProbFromCP(playedCP)
		if wpLoss < 0 {
			wpLoss = 0
		}
		g := classifyMove(cpLoss, wpLoss)
		grades = append(grades, g)
	}
	return grades
}

func hintCode(e *UCIEdge, st *State, cfg Config) string {
	if !cfg.ShowHintCode {
		return ""
	}
	pos := st.Position()
	var lines []UCIAnalysisLine
	var err error
	if cfg.Deterministic {
		lines, err = analyseCachedMultiDepth(e, pos.String(), cfg.DepthHint, 2, multiPVCache)
	} else {
		lines, err = analyseSorted(e, pos.String(), cfg.AnalysisTimeHint, 2)
	}
	if err != nil || len(lines) == 0 {
		return ""
	}
	best := lines[0]
	cp1 := scoreToCP(best.Score, st.Turn(), st.Turn())
	mate := "-"
	if best.Score.IsMate {
		m := best.Score.Mate
		if m > 0 {
			mate = fmt.Sprintf("M%d", m)
		} else if m < 0 {
			mate = fmt.Sprintf("m%d", -m)
		}
	}
	cp2 := 0
	delta := "-"
	if len(lines) > 1 {
		cp2 = scoreToCP(lines[1].Score, st.Turn(), st.Turn())
		delta = fmt.Sprintf("%d", cp1-cp2)
	}
	// Determine if best move is check/capture based on the position (like python-chess).
	C := 0
	X := 0
	if len(best.PV) > 0 {
		mv, err := (chess.UCINotation{}).Decode(pos, best.PV[0])
		if err == nil {
			after := pos.Update(mv)
			if inCheck(after, after.Turn()) {
				C = 1
			}
			if isCaptureLikePython(pos, mv) {
				X = 1
			}
		}
	}
	H := hangingCountSimple(pos, pos.Turn())

	return fmt.Sprintf("C%d X%d %s H%d Δ%s", C, X, mate, H, delta)
}

func isCaptureLikePython(pos *chess.Position, mv *chess.Move) bool {
	b := pos.Board()
	occ := b.SquareMap()
	// Direct capture.
	if occ[mv.S2()] != chess.NoPiece {
		return true
	}
	// Approximate en-passant: pawn moves diagonally to an empty square.
	src := occ[mv.S1()]
	if src == chess.NoPiece {
		return false
	}
	if src.Type() != chess.Pawn {
		return false
	}
	df := int(mv.S2().File()) - int(mv.S1().File())
	dr := int(mv.S2().Rank()) - int(mv.S1().Rank())
	if df < 0 {
		df = -df
	}
	if dr < 0 {
		dr = -dr
	}
	return df == 1 && dr == 1
}

func hangingCountSimple(pos *chess.Position, color chess.Color) int {
	// Approximate: count pieces attacked more than defended.
	opp := chess.White
	if color == chess.White {
		opp = chess.Black
	}
	attByOpp := attackCounts(pos, opp)
	attByUs := attackCounts(pos, color)

	b := pos.Board()
	cnt := 0
	for sq, p := range b.SquareMap() {
		if p == chess.NoPiece {
			continue
		}
		if p.Color() != color {
			continue
		}
		if p.Type() == chess.King {
			continue
		}
		atk := attByOpp[sq]
		if atk == 0 {
			continue
		}
		dfn := attByUs[sq]
		if atk > dfn {
			cnt++
		}
	}
	return cnt
}

func formatCP(cp int) string {
	if cp > MateScore-1000 || cp < -MateScore+1000 {
		return "mate"
	}
	return strings.ReplaceAll(fmt.Sprintf("%.2f", float64(cp)/100.0), "-0.00", "0.00")
}

func sortLinesByScore(lines []UCIAnalysisLine) {
	sort.Slice(lines, func(i, j int) bool {
		// Higher CP is better for side-to-move
		ai := lines[i].Score
		aj := lines[j].Score
		ci := ai.CP
		cj := aj.CP
		if ai.IsMate && !aj.IsMate {
			return ai.Mate > 0
		}
		if !ai.IsMate && aj.IsMate {
			return !(aj.Mate > 0)
		}
		return ci > cj
	})
}
