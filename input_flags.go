package main

import (
	"strings"
)

type InputFlags struct {
	Review bool
	Gambit string
}

func parseInputFlags(raw string) (base string, flags InputFlags) {
	toks := strings.Fields(strings.TrimSpace(raw))
	if len(toks) == 0 {
		return "", InputFlags{}
	}
	base = toks[0]
	for _, t := range toks[1:] {
		if t == "--review" {
			flags.Review = true
			continue
		}
		if strings.HasPrefix(t, "--gambit=") {
			v := strings.TrimSpace(strings.TrimPrefix(t, "--gambit="))
			v = strings.Trim(v, "\"'")
			flags.Gambit = strings.ToLower(v)
			continue
		}
	}
	return base, flags
}
