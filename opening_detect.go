package main

import (
	"strings"
)

func (s *State) UpdateOpeningDetection() {
	// Fix base when we complete any known line. Detail can evolve later.
	if strings.TrimSpace(s.OpeningBase) == "" {
		if def, ok := bestCompletedLine(s.MovesUCI); ok {
			s.OpeningBase = def.NamePT
		}
	}

	base := strings.ToLower(strings.TrimSpace(s.OpeningBase))
	s.OpeningDetail = ""

	// Opening details (cheap heuristics).
	if strings.Contains(base, "sicil") {
		if v := classifySicilianVariation(s.MovesUCI); v != "" {
			s.OpeningDetail = v
		}
		return
	}
	if strings.Contains(base, "gambito do rei") {
		if v := kingsGambitDetail(s.MovesUCI); v != "" {
			s.OpeningDetail = v
		}
		return
	}
}

func classifySicilianVariation(history []string) string {
	h := normalizeHistory(history)
	// Need at least 2 plies for "Siciliana".
	if len(h) < 2 || h[0] != "e2e4" || h[1] != "c7c5" {
		return ""
	}

	// Look for typical move markers.
	// Notes: this is heuristic and intentionally cheap.
	has := func(m string) bool {
		m = normalizeUCI(m)
		for _, x := range h {
			if x == m {
				return true
			}
		}
		return false
	}

	// Early markers by Black:
	if has("c7c5") && has("g7g6") {
		return "Dragão (…g6)"
	}
	if has("c7c5") && has("e7e5") {
		return "Sveshnikov/Kalashnikov (…e5)"
	}
	if has("c7c5") && has("a7a6") {
		return "Najdorf (…a6)"
	}
	if has("c7c5") && has("e7e6") && has("d7d6") {
		return "Scheveningen (…e6 e …d6)"
	}
	if has("c7c5") && has("e7e6") {
		return "Taimanov/Kan (…e6)"
	}
	if has("c7c5") && has("d7d6") {
		return "Clássica (…d6)"
	}
	return ""
}

func kingsGambitDetail(history []string) string {
	h := normalizeHistory(history)
	if len(h) >= 4 && prefixEqual([]string{"e2e4", "e7e5", "f2f4", "e5f4"}, h) {
		return "Aceito"
	}
	if len(h) >= 3 && prefixEqual([]string{"e2e4", "e7e5", "f2f4"}, h) {
		return "Oferecido"
	}
	return ""
}

func openingTagsForNextMove(history []string, nextMoveUCI string) []string {
	h := normalizeHistory(history)
	n := normalizeUCI(nextMoveUCI)
	aug := append(append([]string{}, h...), n)
	ply := len(aug)

	tags := []string{}
	if def, ok := bestCompletedLine(aug); ok {
		name := strings.TrimSpace(def.NamePT)
		if strings.ToLower(def.Key) == "sicilian" {
			if v := classifySicilianVariation(aug); v != "" {
				name = name + " — " + v
			}
		}
		if strings.ToLower(def.Key) == "kings-gambit-accepted" {
			name = "Gambito do Rei — Aceito"
		}
		tags = append(tags, name)
		return tags
	}

	// Otherwise: possible candidates, but only when we reach the line's signal ply.
	for _, def := range openingAndGambitDefs() {
		moves := normalizeHistory(def.MovesUCI)
		if ply > len(moves) {
			continue
		}
		if tagPly(def) != ply {
			continue
		}
		if prefixEqual(aug, moves) {
			name := strings.TrimSpace(def.NamePT)
			if strings.ToLower(def.Key) == "sicilian" {
				if v := classifySicilianVariation(aug); v != "" {
					name = name + " — " + v
				}
			}
			tags = append(tags, name)
		}
	}
	return dedupStrings(tags)
}

func dedupStrings(xs []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, x := range xs {
		k := strings.ToLower(strings.TrimSpace(x))
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, strings.TrimSpace(x))
	}
	return out
}
