package main

import "github.com/notnil/chess"

func kingSquare(pos *chess.Position, color chess.Color) chess.Square {
	b := pos.Board()
	for sq, p := range b.SquareMap() {
		if p == chess.NoPiece {
			continue
		}
		if p.Color() == color && p.Type() == chess.King {
			return sq
		}
	}
	return chess.NoSquare
}

func inCheck(pos *chess.Position, color chess.Color) bool {
	ksq := kingSquare(pos, color)
	if ksq == chess.NoSquare {
		return false
	}
	opp := chess.White
	if color == chess.White {
		opp = chess.Black
	}
	att := attackCounts(pos, opp)
	return att[ksq] > 0
}

func isCheckmate(pos *chess.Position) bool {
	// Checkmate for side-to-move.
	if len(pos.ValidMoves()) != 0 {
		return false
	}
	return inCheck(pos, pos.Turn())
}

func attackCounts(pos *chess.Position, color chess.Color) map[chess.Square]int {
	b := pos.Board()
	occ := b.SquareMap()
	out := map[chess.Square]int{}

	for sq, p := range occ {
		if p == chess.NoPiece {
			continue
		}
		if p.Color() != color {
			continue
		}
		addAttacksForPiece(out, occ, sq, p)
	}
	return out
}

func addAttacksForPiece(out map[chess.Square]int, occ map[chess.Square]chess.Piece, from chess.Square, p chess.Piece) {
	f := int(from.File())
	r := int(from.Rank())

	switch p.Type() {
	case chess.Pawn:
		dir := 1
		if p.Color() == chess.Black {
			dir = -1
		}
		for _, df := range []int{-1, 1} {
			nf, nr := f+df, r+dir
			if inBoard(nf, nr) {
				out[chess.NewSquare(chess.File(nf), chess.Rank(nr))]++
			}
		}
	case chess.Knight:
		offs := [][2]int{{1, 2}, {2, 1}, {2, -1}, {1, -2}, {-1, -2}, {-2, -1}, {-2, 1}, {-1, 2}}
		for _, o := range offs {
			nf, nr := f+o[0], r+o[1]
			if inBoard(nf, nr) {
				out[chess.NewSquare(chess.File(nf), chess.Rank(nr))]++
			}
		}
	case chess.Bishop:
		addRays(out, occ, f, r, [][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}})
	case chess.Rook:
		addRays(out, occ, f, r, [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}})
	case chess.Queen:
		addRays(out, occ, f, r, [][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}, {1, 0}, {-1, 0}, {0, 1}, {0, -1}})
	case chess.King:
		for df := -1; df <= 1; df++ {
			for dr := -1; dr <= 1; dr++ {
				if df == 0 && dr == 0 {
					continue
				}
				nf, nr := f+df, r+dr
				if inBoard(nf, nr) {
					out[chess.NewSquare(chess.File(nf), chess.Rank(nr))]++
				}
			}
		}
	}
}

func addRays(out map[chess.Square]int, occ map[chess.Square]chess.Piece, f int, r int, dirs [][2]int) {
	for _, d := range dirs {
		nf, nr := f+d[0], r+d[1]
		for inBoard(nf, nr) {
			sq := chess.NewSquare(chess.File(nf), chess.Rank(nr))
			out[sq]++
			if occ[sq] != chess.NoPiece {
				break
			}
			nf += d[0]
			nr += d[1]
		}
	}
}

func inBoard(f int, r int) bool {
	return f >= 0 && f <= 7 && r >= 0 && r <= 7
}
