package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode"

	"github.com/notnil/chess"
)

const importedMaxPlies = 14

var (
	reECO = regexp.MustCompile(`(?i)\b([A-E][0-9]{2})\b`)
	// Removes citations like [123] and footnote markers.
	reCite = regexp.MustCompile(`\[[^\]]*\]`)
)

func loadExternalBookDefs(path string) ([]LineDef, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	defs, err := parseBookDefsFromText(string(b))
	if err != nil {
		return nil, err
	}
	return defs, nil
}

func parseBookDefsFromText(text string) ([]LineDef, error) {
	scanner := bufio.NewScanner(strings.NewReader(text))
	// allow long lines
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	out := make([]LineDef, 0, 256)
	seen := map[string]struct{}{}
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		defs := parseOneBookLine(line)
		for _, d := range defs {
			if len(d.MovesUCI) == 0 {
				continue
			}
			k := strings.ToLower(strings.TrimSpace(d.Key))
			if k == "" {
				continue
			}
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			out = append(out, d)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	// Not having any defs isn't an error.
	_ = lineNo
	return out, nil
}

func parseOneBookLine(line string) []LineDef {
	// Normalize dashes.
	line = strings.ReplaceAll(line, "—", "-")
	line = strings.ReplaceAll(line, "–", "-")
	line = reCite.ReplaceAllString(line, "")
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	m := reECO.FindStringSubmatch(line)
	if len(m) == 0 {
		// Probably a section header like "Bird's Opening".
		return nil
	}
	eco := strings.ToUpper(m[1])
	i := strings.Index(strings.ToUpper(line), eco)
	if i < 0 {
		return nil
	}

	namePart := strings.TrimSpace(strings.Trim(line[:i], " -\t"))
	after := strings.TrimSpace(strings.Trim(line[i+len(eco):], " -\t"))
	if namePart == "" || after == "" {
		return nil
	}

	kind := KindOpening
	lp := strings.ToLower(namePart)
	if strings.Contains(lp, "gambit") || strings.Contains(lp, "countergambit") {
		kind = KindGambit
	}

	// Some entries contain multiple alternatives separated by OR / also.
	alts := splitAlternatives(after)
	defs := make([]LineDef, 0, len(alts))
	for idx, alt := range alts {
		movesUCI := uciMovesFromMoveText(alt, importedMaxPlies)
		if len(movesUCI) < 2 {
			continue
		}
		key := makeBookKey(namePart, eco, movesUCI, idx)
		defs = append(defs, LineDef{
			Key:      key,
			Kind:     kind,
			NamePT:   strings.TrimSpace(namePart),
			NameEN:   "",
			ECO:      eco,
			TagAtPly: len(movesUCI),
			MovesUCI: movesUCI,
		})
	}
	return defs
}

func splitAlternatives(after string) []string {
	// Normalize separators.
	clean := strings.TrimSpace(after)
	clean = strings.ReplaceAll(clean, " also ", " OR ")
	clean = strings.ReplaceAll(clean, " Also ", " OR ")
	clean = strings.ReplaceAll(clean, " ALSO ", " OR ")
	clean = strings.ReplaceAll(clean, " or ", " OR ")
	clean = strings.ReplaceAll(clean, " Or ", " OR ")
	clean = strings.ReplaceAll(clean, " OR ", " OR ")

	parts := []string{}
	for _, p := range strings.Split(clean, " OR ") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return []string{after}
	}
	return parts
}

func uciMovesFromMoveText(moveText string, maxPlies int) []string {
	// Keep only the move sequence portion; most lines start with "1.".
	moveText = strings.TrimSpace(moveText)
	if moveText == "" {
		return nil
	}

	toks := extractSANTokens(moveText)
	if len(toks) == 0 {
		return nil
	}
	pos := chess.NewGame().Position()
	uci := make([]string, 0, min(len(toks), maxPlies))
	an := chess.AlgebraicNotation{}
	un := chess.UCINotation{}

	for _, t := range toks {
		if len(uci) >= maxPlies {
			break
		}
		mv, err := an.Decode(pos, t)
		if err != nil {
			// Some sources include stray tokens after the move list; stop here.
			break
		}
		// Safety: ensure it's legal.
		if !moveInValidMoves(pos, mv) {
			break
		}
		uci = append(uci, un.Encode(pos, mv))
		pos = pos.Update(mv)
	}
	return uci
}

func extractSANTokens(s string) []string {
	// Remove citations and normalize castling.
	s = reCite.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "0-0-0", "O-O-O")
	s = strings.ReplaceAll(s, "0-0", "O-O")
	s = strings.ReplaceAll(s, "…", "...")

	fields := strings.Fields(s)
	out := []string{}
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		// Strip leading move numbers like 1.e4 or 12...Nf6
		f = stripMoveNumberPrefix(f)
		f = strings.Trim(f, ",;:")
		if f == "" {
			continue
		}

		upper := strings.ToUpper(f)
		if upper == "OR" || upper == "ALSO" {
			continue
		}

		// Remove trailing annotations (!, ?, +, #) that might confuse the SAN decoder.
		f = strings.TrimRightFunc(f, func(r rune) bool {
			switch r {
			case '!', '?', '+', '#':
				return true
			default:
				return false
			}
		})
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}

		// Ignore obvious non-move tokens.
		if looksLikeWord(f) && !strings.ContainsAny(f, "abcdefgh12345678O-oKQRBNx=") {
			continue
		}

		out = append(out, f)
	}
	return out
}

func stripMoveNumberPrefix(tok string) string {
	// Examples: "1.e4" -> "e4", "12...Nf6" -> "Nf6", "3." -> "".
	// We'll remove a leading digits+dot(s) prefix.
	for {
		i := strings.IndexByte(tok, '.')
		if i <= 0 {
			break
		}
		allDigits := true
		for _, r := range tok[:i] {
			if r < '0' || r > '9' {
				allDigits = false
				break
			}
		}
		if !allDigits {
			break
		}
		// Remove up to the last dot in the run (handles 12...)
		j := i
		for j < len(tok) && tok[j] == '.' {
			j++
		}
		tok = strings.TrimSpace(tok[j:])
		break
	}
	return tok
}

func looksLikeWord(s string) bool {
	hasLetter := false
	hasDigit := false
	for _, r := range s {
		if unicode.IsLetter(r) {
			hasLetter = true
		}
		if unicode.IsDigit(r) {
			hasDigit = true
		}
	}
	return hasLetter && !hasDigit
}

func makeBookKey(name string, eco string, moves []string, altIdx int) string {
	base := strings.ToLower(strings.TrimSpace(name))
	base = strings.Map(func(r rune) rune {
		if r == ' ' || r == '_' {
			return '-'
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		// Drop other chars (apostrophes, accents, punctuation).
		return -1
	}, base)
	base = strings.Trim(base, "-")
	if base == "" {
		base = "line"
	}

	sig := ""
	if len(moves) > 0 {
		// Use the first few moves as a stable signature.
		end := min(len(moves), 6)
		sig = strings.Join(moves[:end], "")
		sig = strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				return r
			}
			return -1
		}, sig)
		if len(sig) > 16 {
			sig = sig[:16]
		}
	}

	k := fmt.Sprintf("%s-%s-%s", base, strings.ToLower(eco), sig)
	if altIdx > 0 {
		k = fmt.Sprintf("%s-a%d", k, altIdx+1)
	}
	return k
}

func validateImportedDefs(defs []LineDef) error {
	for _, d := range defs {
		if strings.TrimSpace(d.Key) == "" {
			return errors.New("imported def missing Key")
		}
		if strings.TrimSpace(d.NamePT) == "" {
			return errors.New("imported def missing NamePT")
		}
		if strings.TrimSpace(d.ECO) == "" {
			return errors.New("imported def missing ECO")
		}
		if len(d.MovesUCI) == 0 {
			return errors.New("imported def missing MovesUCI")
		}
	}
	return nil
}
