package wfilecontext

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/gofrs/flock"
)

// CachedDoc is a processed workspace document stored on disk.
type CachedDoc struct {
	ContentHash string    `json:"content_hash"`
	Path        string    `json:"path"`
	Markdown    string    `json:"markdown"`
	Embedding   []float32 `json:"embedding"`
	TokenCount  int       `json:"token_count"`
	SourceSize  int64     `json:"source_size"`
	SourceMtime int64     `json:"source_mtime"`
}

// DocStore persists cached documents keyed by content hash.
type DocStore struct {
	dir string
	mu  sync.Mutex
}

func NewDocStore(dir string) (*DocStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create doc store: %w", err)
	}
	return &DocStore{dir: dir}, nil
}

func (s *DocStore) cachePath(hash string) string {
	return filepath.Join(s.dir, hash+".json")
}

func (s *DocStore) Get(hash string, size int64, mtime int64) (*CachedDoc, bool) {
	path := s.cachePath(hash)
	lock := flock.New(path + ".lock")
	if err := lock.RLock(); err != nil {
		return nil, false
	}
	defer func() { _ = lock.Unlock() }()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var doc CachedDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, false
	}
	if doc.SourceSize != size || doc.SourceMtime != mtime {
		return nil, false
	}
	return &doc, true
}

func (s *DocStore) Put(doc *CachedDoc) error {
	if doc == nil || doc.ContentHash == "" {
		return errors.New("invalid cached doc")
	}
	path := s.cachePath(doc.ContentHash)
	s.mu.Lock()
	defer s.mu.Unlock()

	lock := flock.New(path + ".lock")
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("lock cache file: %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("open cache file: %w", err)
	}
	defer func() { _ = f.Close() }()

	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err = f.Write(data)
	return err
}
