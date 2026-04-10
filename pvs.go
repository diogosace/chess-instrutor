package main

import (
	"strings"

	"github.com/notnil/chess"
)

func pvEmSAN(pos *chess.Position, pvUCI []string, maxPlies int) string {
	temp := pos
	out := []string{}
	for i, u := range pvUCI {
		if i >= maxPlies {
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
		out = append(out, san)
		temp = after
	}
	return strings.Join(out, " ")
}

func pvEmSANNumbered(pos *chess.Position, pvUCI []string, maxPlies int) string {
	temp := pos
	out := []string{}
	fullmove := fenFullmove(temp.String())

	for i, u := range pvUCI {
		if i >= maxPlies {
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

		turn := temp.Turn()
		if turn == chess.White {
			out = append(out, itoa(fullmove)+". "+san)
		} else {
			if len(out) == 0 {
				out = append(out, itoa(fullmove)+"... "+san)
			} else {
				out = append(out, san)
			}
		}

		temp = after
		if turn == chess.Black {
			fullmove++
		}
	}
	return strings.Join(out, " ")
}

func itoa(n int) string {
	// tiny local helper to avoid strconv import chatter
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + (n % 10))
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
