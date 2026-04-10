package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/notnil/chess"
)

type Color int

const (
	White Color = iota
	Black
)

func toChessColor(c Color) chess.Color {
	if c == Black {
		return chess.Black
	}
	return chess.White
}

func fromChessColor(c chess.Color) Color {
	if c == chess.Black {
		return Black
	}
	return White
}

type State struct {
	Game      *chess.Game
	HeroColor Color

	// Optional: guide engine into a specific opening/gambit line.
	EngineGambit string

	// Detected opening/gambit (fixed once recognized).
	OpeningBase   string
	OpeningDetail string

	// history for printing
	MovesSAN []string
	Grades   []string
	MovesUCI []string
}

func NewState(hero Color) *State {
	return &State{Game: chess.NewGame(), HeroColor: hero}
}

func (s *State) Position() *chess.Position { return s.Game.Position() }

func (s *State) Turn() Color { return fromChessColor(s.Position().Turn()) }

func (s *State) MoveCount() int { return len(s.Game.Moves()) }

func (s *State) GameOver() bool { return s.Game.Outcome() != chess.NoOutcome }

func (s *State) FEN() string { return s.Position().String() }

func (s *State) LegalMovesSAN() []string {
	pos := s.Position()
	moves := pos.ValidMoves()
	not := chess.AlgebraicNotation{}
	out := make([]string, 0, len(moves))
	for _, m := range moves {
		out = append(out, not.Encode(pos, m))
	}
	return out
}

func (s *State) ApplyMoveSAN(input string) (*chess.Move, string, error) {
	pos := s.Position()
	not := chess.AlgebraicNotation{}
	mv, err := not.Decode(pos, strings.TrimSpace(input))
	if err != nil {
		return nil, "", err
	}
	if err := s.Game.Move(mv); err != nil {
		return nil, "", err
	}
	return mv, not.Encode(pos, mv), nil
}

func (s *State) ApplyMoveUCI(uci string) (*chess.Move, string, error) {
	pos := s.Position()
	not := chess.UCINotation{}
	mv, err := not.Decode(pos, strings.TrimSpace(uci))
	if err != nil {
		return nil, "", err
	}
	if err := s.Game.Move(mv); err != nil {
		return nil, "", err
	}
	return mv, (chess.AlgebraicNotation{}).Encode(pos, mv), nil
}

func (s *State) PrintGame() {
	if strings.TrimSpace(s.OpeningBase) != "" {
		name := strings.TrimSpace(s.OpeningBase)
		if strings.TrimSpace(s.OpeningDetail) != "" {
			name = name + " (" + strings.TrimSpace(s.OpeningDetail) + ")"
		}
		fmt.Printf("\n%s %s\n", label("Abertura/Gambito:"), name)
	}

	// Print as PGN-like: 1. e4 e5 ... using stored SAN + grades.
	parts := []string{}
	for i := 0; i < len(s.MovesSAN); i += 2 {
		n := (i / 2) + 1
		w := annotateMove(s.MovesSAN[i], getOrEmpty(s.Grades, i))
		if i+1 < len(s.MovesSAN) {
			b := annotateMove(s.MovesSAN[i+1], getOrEmpty(s.Grades, i+1))
			parts = append(parts, fmt.Sprintf("%d. %s %s", n, w, b))
		} else {
			parts = append(parts, fmt.Sprintf("%d. %s", n, w))
		}
	}
	fmt.Printf("\n📜 %s\n", strings.Join(parts, " "))
}

func getOrEmpty(xs []string, i int) string {
	if i < 0 || i >= len(xs) {
		return ""
	}
	return xs[i]
}

func (s *State) Hero() Color { return s.HeroColor }

func (s *State) ApplyUCIOrSAN(input string) (uci string, san string, err error) {
	in := strings.TrimSpace(input)
	if in == "" {
		return "", "", errors.New("lance vazio")
	}
	// Quick guess: UCI is like e2e4 or e7e8q
	if len(in) == 4 || len(in) == 5 {
		mv, e := (chess.UCINotation{}).Decode(s.Position(), in)
		if e == nil {
			sanTmp := (chess.AlgebraicNotation{}).Encode(s.Position(), mv)
			if err := s.Game.Move(mv); err != nil {
				return "", "", err
			}
			return in, sanTmp, nil
		}
	}
	// Fallback SAN
	pos := s.Position()
	move, err := (chess.AlgebraicNotation{}).Decode(pos, in)
	if err != nil {
		return "", "", err
	}
	uciTmp := (chess.UCINotation{}).Encode(pos, move)
	sanTmp := (chess.AlgebraicNotation{}).Encode(pos, move)
	if err := s.Game.Move(move); err != nil {
		return "", "", err
	}
	return uciTmp, sanTmp, nil
}
