package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/notnil/chess"
)

func main() {
	cfg := defaultConfig()

	enginePath := cfg.EnginePath
	if v := strings.TrimSpace(os.Getenv("STOCKFISH_PATH")); v != "" {
		enginePath = v
	}

	e, err := NewUCIEdge(enginePath, cfg.EngineOptions())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Falha ao iniciar Stockfish: %v\n", err)
		os.Exit(1)
	}
	defer e.Close()

	// Persistent cache is best-effort; failures shouldn't stop the trainer.
	if cfg.CacheEnabled {
		if c, err := openDiskCache(cfg.CachePath); err == nil {
			diskCache = c
			defer diskCache.Close()
		}
	}
	engineCacheFingerprint = engineFingerprint(enginePath, cfg.EngineOptions(), cfg.Deterministic)

	in := bufio.NewReader(os.Stdin)

	hero, startFlags := chooseColor(in)
	st := NewState(hero)
	if startFlags.Gambit != "" {
		st.EngineGambit = startFlags.Gambit
	}

	hintCodeLegend()

	// Se humano joga de pretas, engine (brancas) abre.
	if st.Turn() != st.HeroColor {
		if err := playEngineMove(e, st, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Erro engine: %v\n", err)
			return
		}
	}

	for !st.GameOver() {
		// Se não for a vez do humano, engine joga.
		if st.Turn() != st.HeroColor {
			if err := playEngineMove(e, st, cfg); err != nil {
				fmt.Fprintf(os.Stderr, "Erro engine: %v\n", err)
				return
			}
			continue
		}

		// Pós-lance da engine: mostra DICA-CÓDIGO antes de pedir o lance.
		if st.MoveCount() > 0 {
			printHintCode(e, st, cfg)
		}

		side := "Brancas"
		if st.Turn() == Black {
			side = "Pretas"
		}
		raw, readErr := readLinePrompt(in, fmt.Sprintf("Seu lance (%s): ", side))
		if readErr == io.EOF {
			fmt.Println("\nEncerrado.")
			return
		}
		if readErr != nil {
			fmt.Println("\nEncerrado.")
			return
		}
		baseMove, flags := parseInputFlags(raw)
		if flags.Gambit != "" {
			st.EngineGambit = flags.Gambit
		}
		raw = normalizarLance(baseMove)
		if raw == "" {
			continue
		}

		fenBefore := st.FEN()
		fenOpt, _ := chess.FEN(fenBefore)
		posBefore := chess.NewGame(fenOpt).Position()

		mv, uci, san, err := parseMoveInput(posBefore, raw)
		if err != nil {
			fmt.Println("\nLances legais:")
			fmt.Println(legalMovesPythonList(posBefore))
			fmt.Println("❌ Lance inválido")
			continue
		}
		if !moveInValidMoves(posBefore, mv) {
			fmt.Println("❌ Lance ilegal nessa posição")
			continue
		}
		if err := st.Game.Move(mv); err != nil {
			fmt.Println("❌ Lance ilegal nessa posição")
			continue
		}

		fenAfter := st.FEN()
		fenOptAfter, _ := chess.FEN(fenAfter)
		posAfter := chess.NewGame(fenOptAfter).Position()
		if isCheckmate(posAfter) && !strings.HasSuffix(san, "#") {
			san += "#"
		}

		st.MovesSAN = append(st.MovesSAN, san)
		st.MovesUCI = append(st.MovesUCI, uci)
		st.UpdateOpeningDetection()

		cfgUse := cfg
		if flags.Review {
			if cfgUse.Deterministic {
				cfgUse.DepthMove = cfgUse.DepthDeep
				cfgUse.DepthSuggest = cfgUse.DepthDeep
				cfgUse.DepthHint = cfgUse.DepthHint
			} else {
				cfgUse.AnalysisTimeMove = maxDuration(1600*time.Millisecond, 2*cfgUse.AnalysisTimeMove)
				cfgUse.AnalysisTimeSuggest = maxDuration(1600*time.Millisecond, 2*cfgUse.AnalysisTimeSuggest)
				cfgUse.AnalysisTimeHint = maxDuration(200*time.Millisecond, cfgUse.AnalysisTimeHint)
			}
		}
		eval, _ := evalMoveLikePython(e, posBefore, posAfter, uci, cfgUse)
		st.Grades = append(st.Grades, eval.Grade)

		fmt.Printf("\n%s: %s (%s)\n", san, eval.Grade, descQualidade(eval.Grade))
		alertaTaticaPosLance(posAfter, eval.PlayedInfo, fromChessColor(posBefore.Turn()), san, cfgUse)
		analisarSeuLance(e, posBefore, san, posAfter, cfgUse)
		mostrarSugestoes(e, st, posAfter, cfgUse)

		if !st.GameOver() {
			if err := playEngineMove(e, st, cfg); err != nil {
				fmt.Fprintf(os.Stderr, "Erro engine: %v\n", err)
				return
			}
		}
	}
}
