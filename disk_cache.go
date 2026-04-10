package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

type DiskCache struct {
	db *bolt.DB
}

type diskEntry struct {
	CreatedUnix int64
	Line        UCIAnalysisLine
	Lines       []UCIAnalysisLine
}

var diskCache *DiskCache

func defaultCachePath() string {
	// Keep it in the user cache dir to avoid cluttering the repo.
	base, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(base) == "" {
		// Fallback to current directory.
		return ".chess-train-cache.bbolt"
	}
	return filepath.Join(base, "chess-train", "cache.bbolt")
}

func openDiskCache(path string) (*DiskCache, error) {
	if strings.TrimSpace(path) == "" {
		path = defaultCachePath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, err
	}
	c := &DiskCache{db: db}
	if err := c.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("single"))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte("multi"))
		return err
	}); err != nil {
		_ = db.Close()
		return nil, err
	}
	return c, nil
}

func (c *DiskCache) Close() {
	if c == nil || c.db == nil {
		return
	}
	_ = c.db.Close()
	c.db = nil
}

func cacheKey(engineFingerprint string, fen string, mode string, depthOrMs int64, multipv int) []byte {
	h := sha1.New()
	_, _ = h.Write([]byte(engineFingerprint))
	_, _ = h.Write([]byte("\n"))
	_, _ = h.Write([]byte(fen))
	_, _ = h.Write([]byte("\n"))
	_, _ = h.Write([]byte(mode))
	_, _ = h.Write([]byte("\n"))
	_, _ = h.Write([]byte(fmt.Sprintf("%d|%d", depthOrMs, multipv)))
	return h.Sum(nil)
}

func encodeGob(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeGob(b []byte, v any) error {
	dec := gob.NewDecoder(bytes.NewReader(b))
	return dec.Decode(v)
}

func (c *DiskCache) GetSingle(key []byte) (UCIAnalysisLine, bool) {
	if c == nil || c.db == nil {
		return UCIAnalysisLine{}, false
	}
	var out diskEntry
	err := c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("single"))
		if b == nil {
			return nil
		}
		raw := b.Get(key)
		if raw == nil {
			return nil
		}
		return decodeGob(raw, &out)
	})
	if err != nil {
		return UCIAnalysisLine{}, false
	}
	if out.CreatedUnix == 0 {
		return UCIAnalysisLine{}, false
	}
	return out.Line, true
}

func (c *DiskCache) PutSingle(key []byte, line UCIAnalysisLine) {
	if c == nil || c.db == nil {
		return
	}
	entry := diskEntry{CreatedUnix: time.Now().Unix(), Line: line}
	raw, err := encodeGob(entry)
	if err != nil {
		return
	}
	_ = c.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("single"))
		if b == nil {
			return nil
		}
		return b.Put(key, raw)
	})
}

func (c *DiskCache) GetMulti(key []byte) ([]UCIAnalysisLine, bool) {
	if c == nil || c.db == nil {
		return nil, false
	}
	var out diskEntry
	err := c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("multi"))
		if b == nil {
			return nil
		}
		raw := b.Get(key)
		if raw == nil {
			return nil
		}
		return decodeGob(raw, &out)
	})
	if err != nil {
		return nil, false
	}
	if out.CreatedUnix == 0 {
		return nil, false
	}
	if len(out.Lines) == 0 {
		return nil, false
	}
	return out.Lines, true
}

func (c *DiskCache) PutMulti(key []byte, lines []UCIAnalysisLine) {
	if c == nil || c.db == nil {
		return
	}
	copyLines := make([]UCIAnalysisLine, len(lines))
	copy(copyLines, lines)
	entry := diskEntry{CreatedUnix: time.Now().Unix(), Lines: copyLines}
	raw, err := encodeGob(entry)
	if err != nil {
		return
	}
	_ = c.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("multi"))
		if b == nil {
			return nil
		}
		return b.Put(key, raw)
	})
}

func engineFingerprint(path string, options map[string]string, deterministic bool) string {
	keys := make([]string, 0, len(options))
	for k := range options {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys)+2)
	parts = append(parts, "path="+path)
	parts = append(parts, fmt.Sprintf("det=%t", deterministic))
	for _, k := range keys {
		parts = append(parts, k+"="+options[k])
	}
	return strings.Join(parts, ";")
}
