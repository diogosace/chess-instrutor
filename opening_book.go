package main

import (
	"strings"
	"sync"

	"github.com/notnil/chess"
)

type LineKind int

const (
	KindOpening LineKind = iota
	KindGambit
)

type LineDef struct {
	Key    string
	Kind   LineKind
	NamePT string
	NameEN string
	ECO    string
	// TagAtPly controls when we consider it meaningful to label this line.
	// Ply is 1-based (1 = White's first move). If 0, defaults to len(MovesUCI).
	TagAtPly int
	MovesUCI []string // defining moves from the initial position
}

type NextLineCandidate struct {
	Def         LineDef
	NextMoveUCI string
	Completes   bool
}

func normalizeUCI(u string) string { return strings.ToLower(strings.TrimSpace(u)) }

func tagPly(def LineDef) int {
	if def.TagAtPly > 0 {
		return def.TagAtPly
	}
	if len(def.MovesUCI) > 0 {
		return len(def.MovesUCI)
	}
	return 0
}

func normalizeHistory(history []string) []string {
	out := make([]string, 0, len(history))
	for _, h := range history {
		h = normalizeUCI(h)
		if h == "" {
			continue
		}
		out = append(out, h)
	}
	return out
}

func prefixEqual(a, b []string) bool {
	if len(a) > len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func lineNextCandidates(history []string, sideToMove chess.Color, kindFilter *LineKind) []NextLineCandidate {
	h := normalizeHistory(history)
	idx := len(h)
	wantWhite := idx%2 == 0
	if wantWhite != (sideToMove == chess.White) {
		// History parity mismatch; no candidates.
		return nil
	}

	out := []NextLineCandidate{}
	for _, def := range openingAndGambitDefs() {
		if kindFilter != nil && def.Kind != *kindFilter {
			continue
		}
		moves := make([]string, 0, len(def.MovesUCI))
		for _, m := range def.MovesUCI {
			moves = append(moves, normalizeUCI(m))
		}
		if idx >= len(moves) {
			continue
		}
		if !prefixEqual(h, moves) {
			continue
		}
		next := moves[idx]
		out = append(out, NextLineCandidate{Def: def, NextMoveUCI: next, Completes: idx+1 == len(moves)})
	}
	return out
}

func bestCompletedLine(history []string) (LineDef, bool) {
	h := normalizeHistory(history)
	var best LineDef
	bestLen := 0
	bestKind := KindOpening
	for _, def := range openingAndGambitDefs() {
		moves := make([]string, 0, len(def.MovesUCI))
		for _, m := range def.MovesUCI {
			moves = append(moves, normalizeUCI(m))
		}
		if len(h) < len(moves) {
			continue
		}
		if !prefixEqual(moves, h) {
			continue
		}
		if len(moves) > bestLen || (len(moves) == bestLen && def.Kind == KindGambit && bestKind != KindGambit) {
			best = def
			bestLen = len(moves)
			bestKind = def.Kind
		}
	}
	if bestLen == 0 {
		return LineDef{}, false
	}
	return best, true
}

var (
	openingDefsOnce sync.Once
	openingDefsAll  []LineDef
)

func openingAndGambitDefs() []LineDef {
	openingDefsOnce.Do(func() {
		// Keep built-ins small and fast; extend via optional import file.
		builtIn := []LineDef{
			// Openings
			{Key: "sicilian", Kind: KindOpening, NamePT: "Siciliana", NameEN: "Sicilian Defense", ECO: "B20", TagAtPly: 2, MovesUCI: []string{"e2e4", "c7c5"}},
			{Key: "nimzo", Kind: KindOpening, NamePT: "Nimzo-Índia", NameEN: "Nimzo-Indian Defense", ECO: "E20", TagAtPly: 6, MovesUCI: []string{"d2d4", "g8f6", "c2c4", "e7e6", "b1c3", "f8b4"}},

			// Gambits (subset)
			{Key: "queens-gambit", Kind: KindGambit, NamePT: "Gambito da Dama", NameEN: "Queen's Gambit", ECO: "D06", TagAtPly: 3, MovesUCI: []string{"d2d4", "d7d5", "c2c4"}},
			{Key: "kings-gambit", Kind: KindGambit, NamePT: "Gambito do Rei", NameEN: "King's Gambit", ECO: "C30", TagAtPly: 3, MovesUCI: []string{"e2e4", "e7e5", "f2f4"}},
			{Key: "kings-gambit-accepted", Kind: KindGambit, NamePT: "Gambito do Rei", NameEN: "King's Gambit", ECO: "C33", TagAtPly: 4, MovesUCI: []string{"e2e4", "e7e5", "f2f4", "e5f4"}},
			{Key: "danish-gambit", Kind: KindGambit, NamePT: "Gambito Dinamarquês", NameEN: "Danish Gambit", ECO: "C21", TagAtPly: 3, MovesUCI: []string{"e2e4", "e7e5", "d2d4", "e5d4", "c2c3"}},
			{Key: "smith-morra", Kind: KindGambit, NamePT: "Gambito Smith-Morra", NameEN: "Smith–Morra Gambit", ECO: "B21", TagAtPly: 3, MovesUCI: []string{"e2e4", "c7c5", "d2d4", "c5d4", "c2c3"}},
			{Key: "wing-gambit-sicilian", Kind: KindGambit, NamePT: "Gambito Wing (Siciliana)", NameEN: "Wing Gambit", ECO: "B20", TagAtPly: 3, MovesUCI: []string{"e2e4", "c7c5", "b2b4"}},
			{Key: "evans-gambit", Kind: KindGambit, NamePT: "Gambito Evans", NameEN: "Evans Gambit", ECO: "C51", TagAtPly: 7, MovesUCI: []string{"e2e4", "e7e5", "g1f3", "b8c6", "f1c4", "f8c5", "b2b4"}},
			{Key: "budapest", Kind: KindGambit, NamePT: "Gambito Budapest", NameEN: "Budapest Gambit", ECO: "A51", TagAtPly: 4, MovesUCI: []string{"d2d4", "g8f6", "c2c4", "e7e5"}},
			{Key: "benko", Kind: KindGambit, NamePT: "Gambito Benko", NameEN: "Benko Gambit", ECO: "A57", TagAtPly: 6, MovesUCI: []string{"d2d4", "g8f6", "c2c4", "c7c5", "d4d5", "b7b5"}},
			{Key: "blackmar-diemer", Kind: KindGambit, NamePT: "Gambito Blackmar–Diemer", NameEN: "Blackmar–Diemer Gambit", ECO: "D00", TagAtPly: 3, MovesUCI: []string{"d2d4", "d7d5", "e2e4"}},
			{Key: "froms", Kind: KindGambit, NamePT: "Gambito de From", NameEN: "From's Gambit", ECO: "A02", TagAtPly: 2, MovesUCI: []string{"f2f4", "e7e5"}},

			// Modern-ish / popular gambits (extra built-ins)
			{Key: "stafford", Kind: KindGambit, NamePT: "Gambito Stafford", NameEN: "Stafford Gambit", ECO: "C42", TagAtPly: 6, MovesUCI: []string{"e2e4", "e7e5", "g1f3", "g8f6", "f3e5", "b8c6"}},
			{Key: "icelandic", Kind: KindGambit, NamePT: "Gambito Islandês", NameEN: "Icelandic Gambit", ECO: "B01", TagAtPly: 6, MovesUCI: []string{"e2e4", "d7d5", "e4d5", "g8f6", "c2c4", "e7e6"}},
			{Key: "latvian", Kind: KindGambit, NamePT: "Gambito Letão", NameEN: "Latvian Gambit", ECO: "C40", TagAtPly: 4, MovesUCI: []string{"e2e4", "e7e5", "g1f3", "f7f5"}},
			{Key: "elephant", Kind: KindGambit, NamePT: "Gambito do Elefante", NameEN: "Elephant Gambit", ECO: "C40", TagAtPly: 4, MovesUCI: []string{"e2e4", "e7e5", "g1f3", "d7d5"}},
		}

		extra := []LineDef(nil)
		if defs, err := loadExternalBookDefs("opening_book_extra.txt"); err == nil {
			extra = defs
		}
		openingDefsAll = append(append([]LineDef{}, builtIn...), extra...)
	})
	return openingDefsAll
}
