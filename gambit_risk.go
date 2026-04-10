package main

import (
	"fmt"
	"strings"

	"github.com/notnil/chess"
)

func gambitRiskTags(pos *chess.Position, moveUCI string) []string {
	moveUCI = normalizeUCI(moveUCI)
	mv, err := (chess.UCINotation{}).Decode(pos, moveUCI)
	if err != nil {
		return nil
	}
	if !moveInValidMoves(pos, mv) {
		return nil
	}
	after := pos.Update(mv)

	mover := pos.Turn()
	matDiff := materialScore(after.Board()) // white - black
	moverDown := false
	if mover == chess.White {
		moverDown = matDiff < 0
	} else {
		moverDown = matDiff > 0
	}

	tags := []string{}
	if moverDown {
		tags = append(tags, "material a menos")
	}

	// King exposure heuristic: early f-pawn push while king still on e-file.
	if moveLooksLikeFPawnPush(moveUCI) {
		if mover == chess.White {
			if hasKingOn(after.Board(), chess.White, chess.E1) {
				tags = append(tags, "rei mais exposto (f-pawn)")
			}
		} else {
			if hasKingOn(after.Board(), chess.Black, chess.E8) {
				tags = append(tags, "rei mais exposto (f-pawn)")
			}
		}
	}

	devMover := developmentCount(after.Board(), mover)
	devOther := developmentCount(after.Board(), opposite(fromChessColor(mover)).toChess())
	if devMover >= devOther+2 {
		tags = append(tags, "vantagem de desenvolvimento")
	}
	if devMover+2 <= devOther {
		tags = append(tags, "atraso de desenvolvimento")
	}

	return dedupStrings(tags)
}

func formatRiskTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	return "Risco: " + strings.Join(tags, ", ")
}

func materialScore(b *chess.Board) int {
	// Returns White material - Black material using basic piece values.
	occ := b.SquareMap()
	val := func(pt chess.PieceType) int {
		switch pt {
		case chess.Pawn:
			return 1
		case chess.Knight, chess.Bishop:
			return 3
		case chess.Rook:
			return 5
		case chess.Queen:
			return 9
		default:
			return 0
		}
	}
	w, bl := 0, 0
	for _, p := range occ {
		if p == chess.NoPiece {
			continue
		}
		v := val(p.Type())
		if p.Color() == chess.White {
			w += v
		} else {
			bl += v
		}
	}
	return w - bl
}

func hasKingOn(b *chess.Board, c chess.Color, sq chess.Square) bool {
	occ := b.SquareMap()
	p := occ[sq]
	return p != chess.NoPiece && p.Type() == chess.King && p.Color() == c
}

func moveLooksLikeFPawnPush(uci string) bool {
	// f2f4 / f2f3 / f7f5 / f7f6
	uci = normalizeUCI(uci)
	return strings.HasPrefix(uci, "f2f") || strings.HasPrefix(uci, "f7f")
}

func developmentCount(b *chess.Board, c chess.Color) int {
	// Count minor pieces not on starting squares.
	occ := b.SquareMap()
	countMoved := 0
	if c == chess.White {
		if pieceNotOn(occ, chess.B1, chess.Knight, c) {
			countMoved++
		}
		if pieceNotOn(occ, chess.G1, chess.Knight, c) {
			countMoved++
		}
		if pieceNotOn(occ, chess.C1, chess.Bishop, c) {
			countMoved++
		}
		if pieceNotOn(occ, chess.F1, chess.Bishop, c) {
			countMoved++
		}
		return countMoved
	}
	// Black
	if pieceNotOn(occ, chess.B8, chess.Knight, c) {
		countMoved++
	}
	if pieceNotOn(occ, chess.G8, chess.Knight, c) {
		countMoved++
	}
	if pieceNotOn(occ, chess.C8, chess.Bishop, c) {
		countMoved++
	}
	if pieceNotOn(occ, chess.F8, chess.Bishop, c) {
		countMoved++
	}
	return countMoved
}

func pieceNotOn(occ map[chess.Square]chess.Piece, sq chess.Square, pt chess.PieceType, c chess.Color) bool {
	p := occ[sq]
	// If the piece isn't on its start square, we consider it developed.
	return p == chess.NoPiece || p.Type() != pt || p.Color() != c
}

// Small helper to avoid importing chess.Color in lots of places.
func (c Color) toChess() chess.Color { return toChessColor(c) }

func fmtMaterialDiff(diff int) string {
	if diff == 0 {
		return "0"
	}
	if diff > 0 {
		return fmt.Sprintf("+%d", diff)
	}
	return fmt.Sprintf("%d", diff)
}
