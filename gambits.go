package main

import (
	"regexp"
	"strings"

	"github.com/notnil/chess"
)

var reECOTag = regexp.MustCompile(`(?i)^[A-E][0-9]{2}$`)

func normGambitName(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

func steeringStillPossible(history []string, gambit string) bool {
	g := normGambitName(gambit)
	if g == "" || g == "off" || g == "none" {
		return false
	}

	// If the user selected a specific book line (imported or built-in), keep steering
	// as long as the played history is still a prefix of that line.
	if def, ok := resolveBookLineForSteering(history, gambit); ok {
		moves := normalizeHistory(def.MovesUCI)
		h := normalizeHistory(history)
		return len(h) < len(moves) && prefixEqual(h, moves)
	}

	h := normalizeHistory(history)
	// Side to move from ply parity (starting position = White to move at ply 0).
	sideToMove := chess.White
	if len(h)%2 == 1 {
		sideToMove = chess.Black
	}

	switch g {
	case "sicilian", "sicilian-defense", "siciliana":
		// Only meaningful as Black's first reply to 1.e4.
		return sideToMove == chess.Black && len(h) == 1 && h[0] == "e2e4"
	case "alapin", "alapin-sicilian", "alapine":
		// Only meaningful as White's 2nd move after 1.e4 c5.
		return sideToMove == chess.White && len(h) == 2 && h[0] == "e2e4" && h[1] == "c7c5"
	case "nimzo-indian", "nimzo", "nimzo-india", "nimzo-indian-defense":
		// Only meaningful for Black, in the early setup.
		if sideToMove != chess.Black {
			return false
		}
		// Allow at these exact decision points.
		if len(h) == 1 && h[0] == "d2d4" {
			return true // ...Nf6
		}
		if len(h) == 3 && h[0] == "d2d4" && h[1] == "g8f6" && h[2] == "c2c4" {
			return true // ...e6
		}
		if len(h) == 5 && h[0] == "d2d4" && h[1] == "g8f6" && h[2] == "c2c4" && h[3] == "e7e6" && h[4] == "b1c3" {
			return true // ...Bb4
		}
		return false
	default:
		// Unknown steering key: keep it (don't surprise-clear).
		return true
	}
}

func gambitPreferredMoveUCI(pos *chess.Position, history []string, gambit string) (string, bool) {
	g := normGambitName(gambit)
	if g == "" || g == "off" || g == "none" {
		return "", false
	}

	// Book-driven steering: if the user picked a line from the imported/built-in book,
	// follow its next move when it matches the current history and it's our turn.
	if def, ok := resolveBookLineForSteering(history, gambit); ok {
		moves := normalizeHistory(def.MovesUCI)
		h := normalizeHistory(history)
		if len(h) < len(moves) && prefixEqual(h, moves) {
			next := moves[len(h)]
			mv, err := (chess.UCINotation{}).Decode(pos, next)
			if err == nil && moveInValidMoves(pos, mv) {
				return next, true
			}
		}
	}

	// NOTE: This is intentionally small and heuristic-driven.
	// If the pattern doesn't match the current position, we don't force anything.
	turn := pos.Turn()
	b := pos.Board()
	occ := b.SquareMap()

	hasPieceAt := func(sq chess.Square, pt chess.PieceType, c chess.Color) bool {
		p := occ[sq]
		return p != chess.NoPiece && p.Type() == pt && p.Color() == c
	}

	// Basic opening steering (requested examples).
	switch g {
	case "sicilian", "sicilian-defense", "siciliana":
		// As Black vs 1.e4: prefer 1...c5
		if turn == chess.Black && hasPieceAt(chess.E4, chess.Pawn, chess.White) && hasPieceAt(chess.C7, chess.Pawn, chess.Black) {
			return "c7c5", true
		}
	case "alapin", "alapin-sicilian", "alapine":
		// As White after ...c5: prefer c3 (c2c3)
		if turn == chess.White && hasPieceAt(chess.E4, chess.Pawn, chess.White) && hasPieceAt(chess.C5, chess.Pawn, chess.Black) && hasPieceAt(chess.C2, chess.Pawn, chess.White) {
			return "c2c3", true
		}
	case "nimzo-indian", "nimzo", "nimzo-india", "nimzo-indian-defense":
		// Nimzo-Indian setup for Black: ...Nf6, ...e6, ...Bb4 (only when it fits).
		if turn != chess.Black {
			return "", false
		}
		// If we can play ...Nf6 (g8->f6) and white has played d4/c4-ish, prefer it.
		if hasPieceAt(chess.D4, chess.Pawn, chess.White) || hasPieceAt(chess.C4, chess.Pawn, chess.White) {
			if hasPieceAt(chess.G8, chess.Knight, chess.Black) {
				return "g8f6", true
			}
			// If knight already on f6, prefer ...e6.
			if hasPieceAt(chess.F6, chess.Knight, chess.Black) && hasPieceAt(chess.E7, chess.Pawn, chess.Black) {
				return "e7e6", true
			}
			// If ...e6 is done and White has Nc3, prefer ...Bb4.
			if hasPieceAt(chess.E6, chess.Pawn, chess.Black) && hasPieceAt(chess.C3, chess.Knight, chess.White) && hasPieceAt(chess.F8, chess.Bishop, chess.Black) {
				return "f8b4", true
			}
		}
	}

	return "", false
}

func resolveBookLineForSteering(history []string, gambit string) (LineDef, bool) {
	query := strings.TrimSpace(gambit)
	if query == "" {
		return LineDef{}, false
	}

	// Normalize
	qLower := strings.ToLower(strings.TrimSpace(query))
	qLower = strings.Trim(qLower, "\"'")
	qLower = strings.TrimSpace(qLower)

	// Optional explicit prefixes.
	mode := "auto"
	if strings.HasPrefix(qLower, "eco:") {
		mode = "eco"
		qLower = strings.TrimSpace(strings.TrimPrefix(qLower, "eco:"))
	}
	if strings.HasPrefix(qLower, "key:") {
		mode = "key"
		qLower = strings.TrimSpace(strings.TrimPrefix(qLower, "key:"))
	}
	if strings.HasPrefix(qLower, "name:") {
		mode = "name"
		qLower = strings.TrimSpace(strings.TrimPrefix(qLower, "name:"))
	}

	// ECO shorthand.
	if mode == "auto" && reECOTag.MatchString(qLower) {
		mode = "eco"
	}

	h := normalizeHistory(history)

	best := LineDef{}
	bestScore := -1
	bestMatchLen := -1
	for _, def := range openingAndGambitDefs() {
		moves := normalizeHistory(def.MovesUCI)
		if len(moves) == 0 {
			continue
		}

		// We only steer if current played history can still be a prefix.
		if len(h) > len(moves) || !prefixEqual(h, moves) {
			continue
		}

		keyLower := strings.ToLower(strings.TrimSpace(def.Key))
		nameLower := strings.ToLower(strings.TrimSpace(def.NamePT))
		ecoLower := strings.ToLower(strings.TrimSpace(def.ECO))

		matches := false
		score := 0
		switch mode {
		case "eco":
			matches = ecoLower == strings.ToLower(qLower)
			score = 3
		case "key":
			matches = keyLower == strings.ToLower(qLower)
			score = 3
		case "name":
			matches = qLower != "" && strings.Contains(nameLower, qLower)
			score = 2
		default:
			// auto: prefer exact ECO, else name contains, else key contains
			if qLower != "" && ecoLower == strings.ToLower(qLower) {
				matches = true
				score = 3
			} else if qLower != "" && strings.Contains(nameLower, qLower) {
				matches = true
				score = 2
			} else if qLower != "" && strings.Contains(keyLower, strings.ToLower(qLower)) {
				matches = true
				score = 1
			}
		}
		if !matches {
			continue
		}

		matchLen := len(h) // since history is prefix
		if score > bestScore || (score == bestScore && matchLen > bestMatchLen) {
			best = def
			bestScore = score
			bestMatchLen = matchLen
		}
	}
	if bestScore < 0 {
		return LineDef{}, false
	}
	return best, true
}
