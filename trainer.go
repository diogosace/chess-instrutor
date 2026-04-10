package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/notnil/chess"
)

var pvCache = NewAnalyseCache()

func chooseColor(in *bufio.Reader) (Color, InputFlags) {
	fmt.Print("Jogar de brancas ou pretas? [b/p] (default b): ")
	raw, _ := in.ReadString('\n')
	base, flags := parseInputFlags(strings.TrimSpace(strings.ToLower(raw)))
	if base == "" || strings.HasPrefix(base, "b") || strings.HasPrefix(base, "w") {
		return White, flags
	}
	if strings.HasPrefix(base, "p") || strings.HasPrefix(base, "n") {
		// p = pretas; n = negras
		return Black, flags
	}
	return White, flags
}

func titulo(txt string) {
	fmt.Printf("\n%s\n", txt)
}

func hintCodeLegend() {
	titulo("Código de dica (legenda)")
	fmt.Println("- C: melhor lance dá cheque (1/0)")
	fmt.Println("- X: melhor lance é captura (1/0)")
	fmt.Println("- M: mate a favor em N (ex.: M3) | m: mate contra em N (ex.: m4)")
	fmt.Println("- H: nº de peças suas penduradas (aprox)")
	fmt.Println("- Δ: diferença (melhor vs 2º melhor) em cp; alto = posição crítica/lance único")
}

func printHintCode(e *UCIEdge, st *State, cfg Config) {
	if !cfg.ShowHintCode {
		return
	}
	payload := hintCode(e, st, cfg)
	if payload == "" {
		return
	}
	// Python prints a blank line before the label.
	fmt.Printf("\n%s %s\n", label("DICA-CÓDIGO:"), payload)
}

func normalizarLance(move string) string {
	move = strings.TrimSpace(move)
	if move == "" {
		return move
	}
	if strings.ContainsRune("rnbqk", rune(move[0])) {
		move = strings.ToUpper(move[:1]) + move[1:]
	}
	return move
}

func readLinePrompt(in *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)
	raw, err := in.ReadString('\n')
	if err != nil {
		if err == io.EOF {
			return strings.TrimSpace(raw), io.EOF
		}
		return strings.TrimSpace(raw), err
	}
	return strings.TrimSpace(raw), nil
}

func playEngineMove(e *UCIEdge, st *State, cfg Config) error {
	if st.GameOver() {
		return nil
	}

	fenBefore := st.FEN()
	plyBefore := st.MoveCount()
	fenOpt, err := chess.FEN(fenBefore)
	if err != nil {
		return err
	}
	posBefore := chess.NewGame(fenOpt).Position()

	// Optional: steer engine into a chosen gambit/opening line when it fits.
	uci := ""
	if st.EngineGambit != "" {
		if !steeringStillPossible(st.MovesUCI, st.EngineGambit) {
			st.EngineGambit = ""
		} else if pref, ok := gambitPreferredMoveUCI(posBefore, st.MovesUCI, st.EngineGambit); ok {
			pm, err := (chess.UCINotation{}).Decode(posBefore, pref)
			if err == nil && moveInValidMoves(posBefore, pm) {
				uci = pref
			}
		}
	}
	if uci == "" {
		depth := 15
		if cfg.DepthSuggest > 0 {
			depth = cfg.DepthSuggest
		}
		var err error
		uci, err = e.BestMoveFENDepth(fenBefore, depth)
		if err != nil {
			return err
		}
	}
	_, sanEngine, err := st.ApplyMoveUCI(uci)
	if err != nil {
		return err
	}
	if isCheckmate(st.Position()) && !strings.HasSuffix(sanEngine, "#") {
		sanEngine += "#"
	}

	// Mate: print special and don't store grade (SAN already contains '#').
	if st.GameOver() || strings.HasSuffix(sanEngine, "#") {
		qual := "#"
		fmt.Printf("\nEngine: %s %s (%s)\n", sanEngine, qual, descQualidade(qual))
		st.MovesSAN = append(st.MovesSAN, sanEngine)
		st.MovesUCI = append(st.MovesUCI, uci)
		st.UpdateOpeningDetection()
		st.Grades = append(st.Grades, "")
		st.PrintGame()
		return nil
	}

	// Grade the engine move like Python.
	pov := fromChessColor(posBefore.Turn())
	bestInfo := []UCIAnalysisLine(nil)
	if cfg.Deterministic {
		bestInfo, err = analyseCachedMultiDepth(e, fenBefore, cfg.DepthMove, 5, multiPVCache)
	} else {
		bestInfo, err = analyseSortedPov(e, fenBefore, cfg.AnalysisTimeMove, 5, pov)
	}
	if err != nil || len(bestInfo) == 0 {
		bestInfo = []UCIAnalysisLine{{Score: UCIScore{CP: 0}}}
	}
	bestCP := scoreToCP(bestInfo[0].Score, pov, pov)
	bestWP := winProbFromCP(bestCP)

	fenAfter := st.FEN()
	played := UCIAnalysisLine{Score: UCIScore{CP: 0}}
	if cfg.Deterministic {
		played, err = analyseCachedSingleDepth(e, fenAfter, cfg.DepthMove, pvCache)
	} else {
		played, err = analyseCachedSingle(e, fenAfter, cfg.AnalysisTimeMove, pvCache)
	}
	if err != nil {
		played = UCIAnalysisLine{Score: UCIScore{CP: 0}}
	}
	playedCP := scoreToCP(played.Score, fromChessColor(st.Position().Turn()), pov)
	cpLoss := bestCP - playedCP
	if cpLoss < 0 {
		cpLoss = 0
	}
	wpLoss := bestWP - winProbFromCP(playedCP)
	if wpLoss < 0 {
		wpLoss = 0
	}
	qual := classifyMove(cpLoss, wpLoss)
	qual = adjustOpeningGrade(plyBefore, qual, cpLoss)

	q := gradeForDisplay(qual)
	qtxt := ""
	if q != "" {
		qtxt = " " + q
	}
	fmt.Printf("\nEngine: %s%s (%s)\n", sanEngine, qtxt, descQualidade(qual))

	st.MovesSAN = append(st.MovesSAN, sanEngine)
	st.MovesUCI = append(st.MovesUCI, uci)
	st.UpdateOpeningDetection()
	st.Grades = append(st.Grades, qual)
	st.PrintGame()
	return nil
}
