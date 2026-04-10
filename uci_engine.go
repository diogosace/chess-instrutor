package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type UCIEdge struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
}

func NewUCIEdge(path string, options map[string]string) (*UCIEdge, error) {
	cmd := exec.Command(path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	e := &UCIEdge{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(out)}

	// Handshake
	if err := e.send("uci"); err != nil {
		_ = e.Close()
		return nil, err
	}
	if err := e.readUntil("uciok", 5*time.Second); err != nil {
		_ = e.Close()
		return nil, err
	}

	// Options
	for k, v := range options {
		_ = e.send(fmt.Sprintf("setoption name %s value %s", k, v))
	}

	if err := e.send("isready"); err != nil {
		_ = e.Close()
		return nil, err
	}
	if err := e.readUntil("readyok", 5*time.Second); err != nil {
		_ = e.Close()
		return nil, err
	}

	_ = e.send("ucinewgame")
	_ = e.send("isready")
	_ = e.readUntil("readyok", 5*time.Second)

	return e, nil
}

func (e *UCIEdge) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stdin != nil {
		_, _ = io.WriteString(e.stdin, "quit\n")
		_ = e.stdin.Close()
		e.stdin = nil
	}
	if e.cmd != nil {
		_ = e.cmd.Process.Kill()
		_, _ = e.cmd.Process.Wait()
		e.cmd = nil
	}
	return nil
}

func (e *UCIEdge) send(cmd string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stdin == nil {
		return errors.New("engine closed")
	}
	_, err := io.WriteString(e.stdin, cmd+"\n")
	return err
}

func (e *UCIEdge) readLine(timeout time.Duration) (string, error) {
	// NOTE: bufio.Reader não tem timeout nativo; como usamos um único processo local,
	// tratamos timeout de forma simples via goroutine.
	ch := make(chan struct {
		line string
		err  error
	}, 1)

	go func() {
		line, err := e.stdout.ReadString('\n')
		ch <- struct {
			line string
			err  error
		}{strings.TrimSpace(line), err}
	}()

	select {
	case r := <-ch:
		return r.line, r.err
	case <-time.After(timeout):
		return "", errors.New("timeout reading from engine")
	}
}

func (e *UCIEdge) readUntil(token string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return errors.New("timeout")
		}
		line, err := e.readLine(left)
		if err != nil {
			return err
		}
		if strings.TrimSpace(line) == token {
			return nil
		}
	}
}

type UCIAnalysisLine struct {
	MultiPV int
	Depth   int
	Score   UCIScore
	PV      []string // UCI moves
}

type UCIScore struct {
	IsMate bool
	Mate   int
	CP     int
}

func ParseUCIScore(tokens []string) UCIScore {
	// expects: score cp <n> OR score mate <n>
	for i := 0; i+2 < len(tokens); i++ {
		if tokens[i] != "score" {
			continue
		}
		typeTok := tokens[i+1]
		valTok := tokens[i+2]
		if typeTok == "cp" {
			v, _ := strconv.Atoi(valTok)
			return UCIScore{IsMate: false, CP: v}
		}
		if typeTok == "mate" {
			v, _ := strconv.Atoi(valTok)
			return UCIScore{IsMate: true, Mate: v}
		}
	}
	return UCIScore{IsMate: false, CP: 0}
}

func (e *UCIEdge) AnalyseFEN(fen string, movetime time.Duration, multipv int) ([]UCIAnalysisLine, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.stdin == nil {
		return nil, errors.New("engine closed")
	}
	// Set position
	_, _ = io.WriteString(e.stdin, "position fen "+fen+"\n")
	// Ensure multipv option matches requested (python-chess sets it per call).
	if multipv < 1 {
		multipv = 1
	}
	_, _ = io.WriteString(e.stdin, fmt.Sprintf("setoption name MultiPV value %d\n", multipv))
	_, _ = io.WriteString(e.stdin, "isready\n")
	for {
		line, err := e.stdout.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "readyok" {
			break
		}
	}
	_, _ = io.WriteString(e.stdin, fmt.Sprintf("go movetime %d\n", movetime.Milliseconds()))

	lines := make(map[int]UCIAnalysisLine)
	depths := make(map[int]int)

	for {
		line, err := e.stdout.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "bestmove ") {
			break
		}
		if !strings.HasPrefix(line, "info ") {
			continue
		}
		toks := strings.Fields(line)
		depth := 0
		for i := 0; i+1 < len(toks); i++ {
			if toks[i] == "depth" {
				if v, err := strconv.Atoi(toks[i+1]); err == nil {
					depth = v
				}
			}
		}
		mpv := 1
		for i := 0; i+1 < len(toks); i++ {
			if toks[i] == "multipv" {
				if v, err := strconv.Atoi(toks[i+1]); err == nil {
					mpv = v
				}
			}
		}
		score := ParseUCIScore(toks)
		pv := []string{}
		for i := 0; i < len(toks); i++ {
			if toks[i] == "pv" {
				pv = append([]string{}, toks[i+1:]...)
				break
			}
		}
		if len(pv) == 0 {
			continue
		}
		if d0, ok := depths[mpv]; ok {
			if depth < d0 {
				continue
			}
		}
		depths[mpv] = depth
		lines[mpv] = UCIAnalysisLine{MultiPV: mpv, Depth: depth, Score: score, PV: pv}
	}

	out := make([]UCIAnalysisLine, 0, len(lines))
	for i := 1; i <= multipv; i++ {
		if l, ok := lines[i]; ok {
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		// fallback: at least return whatever exists
		for _, l := range lines {
			out = append(out, l)
			break
		}
	}
	return out, nil
}

func (e *UCIEdge) AnalyseFENDepth(fen string, depth int, multipv int) ([]UCIAnalysisLine, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.stdin == nil {
		return nil, errors.New("engine closed")
	}
	// Set position
	_, _ = io.WriteString(e.stdin, "position fen "+fen+"\n")
	if multipv < 1 {
		multipv = 1
	}
	_, _ = io.WriteString(e.stdin, fmt.Sprintf("setoption name MultiPV value %d\n", multipv))
	_, _ = io.WriteString(e.stdin, "isready\n")
	for {
		line, err := e.stdout.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "readyok" {
			break
		}
	}
	if depth < 1 {
		depth = 1
	}
	_, _ = io.WriteString(e.stdin, fmt.Sprintf("go depth %d\n", depth))

	lines := make(map[int]UCIAnalysisLine)
	depths := make(map[int]int)

	for {
		line, err := e.stdout.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "bestmove ") {
			break
		}
		if !strings.HasPrefix(line, "info ") {
			continue
		}
		toks := strings.Fields(line)
		dep := 0
		for i := 0; i+1 < len(toks); i++ {
			if toks[i] == "depth" {
				if v, err := strconv.Atoi(toks[i+1]); err == nil {
					dep = v
				}
			}
		}
		mpv := 1
		for i := 0; i+1 < len(toks); i++ {
			if toks[i] == "multipv" {
				if v, err := strconv.Atoi(toks[i+1]); err == nil {
					mpv = v
				}
			}
		}
		score := ParseUCIScore(toks)
		pv := []string{}
		for i := 0; i < len(toks); i++ {
			if toks[i] == "pv" {
				pv = append([]string{}, toks[i+1:]...)
				break
			}
		}
		if len(pv) == 0 {
			continue
		}
		if d0, ok := depths[mpv]; ok {
			if dep < d0 {
				continue
			}
		}
		depths[mpv] = dep
		lines[mpv] = UCIAnalysisLine{MultiPV: mpv, Depth: dep, Score: score, PV: pv}
	}

	out := make([]UCIAnalysisLine, 0, len(lines))
	for i := 1; i <= multipv; i++ {
		if l, ok := lines[i]; ok {
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		for _, l := range lines {
			out = append(out, l)
			break
		}
	}
	if len(out) == 0 {
		out = []UCIAnalysisLine{{MultiPV: 1, Score: UCIScore{CP: 0}, PV: nil}}
	}
	return out, nil
}

func (e *UCIEdge) BestMoveFEN(fen string, movetime time.Duration) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.stdin == nil {
		return "", errors.New("engine closed")
	}
	_, _ = io.WriteString(e.stdin, "position fen "+fen+"\n")
	_, _ = io.WriteString(e.stdin, "isready\n")
	for {
		line, err := e.stdout.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimSpace(line)
		if line == "readyok" {
			break
		}
	}
	_, _ = io.WriteString(e.stdin, fmt.Sprintf("go movetime %d\n", movetime.Milliseconds()))

	for {
		line, err := e.stdout.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "bestmove ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1], nil
			}
			return "", errors.New("no bestmove")
		}
	}
}

func (e *UCIEdge) BestMoveFENDepth(fen string, depth int) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.stdin == nil {
		return "", errors.New("engine closed")
	}
	_, _ = io.WriteString(e.stdin, "position fen "+fen+"\n")
	_, _ = io.WriteString(e.stdin, "isready\n")
	for {
		line, err := e.stdout.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimSpace(line)
		if line == "readyok" {
			break
		}
	}
	_, _ = io.WriteString(e.stdin, fmt.Sprintf("go depth %d\n", depth))

	for {
		line, err := e.stdout.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "bestmove ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1], nil
			}
			return "", errors.New("no bestmove")
		}
	}
}
