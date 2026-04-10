package main

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	EnginePath string

	// Persistent cache (disk). Best-effort; still uses in-memory caches.
	CacheEnabled bool
	CachePath    string

	// Deterministic mode: favors stable output and lower CPU heat.
	// When enabled, analysis is performed by fixed depth (instead of movetime)
	// and the engine is configured with Threads=1.
	Deterministic bool
	DepthMove     int
	DepthSuggest  int
	DepthHint     int
	DepthDeep     int

	AnalysisTimeMove    time.Duration
	AnalysisTimeSuggest time.Duration
	SuggestMultiPV      int
	ShowMainLine        int
	ShowBetterMoves     int
	ShowOpponentBest    int

	PVPliesMain int

	// Hint code
	ShowHintCode     bool
	AnalysisTimeHint time.Duration

	// Dangers
	MaxDangers                 int
	MaxDangerSubs              int
	OpponentMistakesPreviewMax int

	// Output
	DedupDangersAcrossLines bool

	// Training heuristics
	ForceEarlyF3AsMistake bool
}

func defaultConfig() Config {
	deterministic := true
	if v := strings.TrimSpace(strings.ToLower(os.Getenv("CHESS_DETERMINISTIC"))); v != "" {
		if v == "0" || v == "false" || v == "no" || v == "off" {
			deterministic = false
		} else {
			deterministic = true
		}
	}

	cacheEnabled := true
	if v := strings.TrimSpace(strings.ToLower(os.Getenv("CHESS_CACHE"))); v != "" {
		if v == "0" || v == "false" || v == "no" || v == "off" {
			cacheEnabled = false
		} else {
			cacheEnabled = true
		}
	}
	cachePath := strings.TrimSpace(os.Getenv("CHESS_CACHE_PATH"))

	return Config{
		EnginePath: "/Users/diogocerqueira/Desktop/Chess/stockfish/stockfish-macos-x86-64-bmi2",

		Deterministic: deterministic,
		CacheEnabled:  cacheEnabled,
		CachePath:     cachePath,
		DepthMove:     14,
		DepthSuggest:  15,
		DepthHint:     10,
		DepthDeep:     18,

		AnalysisTimeMove:    1000 * time.Millisecond,
		AnalysisTimeSuggest: 1200 * time.Millisecond,
		SuggestMultiPV:      5,
		ShowMainLine:        5,
		ShowBetterMoves:     5,
		ShowOpponentBest:    5,

		PVPliesMain: 8,

		ShowHintCode:     true,
		AnalysisTimeHint: 120 * time.Millisecond,

		MaxDangers:                 5,
		MaxDangerSubs:              3,
		OpponentMistakesPreviewMax: 3,

		DedupDangersAcrossLines: true,
		ForceEarlyF3AsMistake:   true,
	}
}

func (c Config) EngineOptions() map[string]string {
	// Mirror Python's configurar_stockfish_rapido().
	cpu := runtime.NumCPU()
	threads := cpu - 1
	if threads < 1 {
		threads = 1
	}
	if threads > 4 {
		threads = 4
	}
	hashMB := 256
	if cpu <= 4 {
		hashMB = 128
	}
	if c.Deterministic {
		threads = 1
		// Keep hash modest to avoid memory pressure.
		hashMB = 128
	}

	return map[string]string{
		"Threads": strconv.Itoa(threads),
		"Hash":    strconv.Itoa(hashMB),
		"Ponder":  "false",
		// Não limita força.
		"UCI_LimitStrength": "false",
		"Skill Level":       "20",
		"MultiPV":           "5",
	}
}
