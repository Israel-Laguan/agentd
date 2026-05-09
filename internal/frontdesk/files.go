package frontdesk

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"agentd/internal/gateway"
	"agentd/internal/sandbox"
)

type FileStash struct {
	Dir            string
	StashThreshold int
}

func (fs *FileStash) EnsureDir() error {
	if fs == nil {
		return fmt.Errorf("file stash is nil")
	}
	if err := os.MkdirAll(fs.Dir, 0o755); err != nil {
		return fmt.Errorf("create file stash %s: %w", fs.Dir, err)
	}
	return nil
}

func (fs *FileStash) Stash(content string) (string, error) {
	if fs == nil || fs.StashThreshold <= 0 || len(content) <= fs.StashThreshold {
		return "", nil
	}
	return fs.writeHashed(content)
}

func (fs *FileStash) Write(name, content string) (string, error) {
	if fs == nil {
		return "", fmt.Errorf("file stash is nil")
	}
	if name == "" {
		return fs.writeHashed(content)
	}
	sum := sha256.Sum256([]byte(name + "\x00" + content))
	ext := filepath.Ext(name)
	if ext == "" {
		ext = ".txt"
	}
	path := filepath.Join(fs.Dir, hex.EncodeToString(sum[:])[:16]+ext)
	if err := fs.EnsureDir(); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("stash file %s: %w", path, err)
	}
	return filepath.Abs(path)
}

func (fs *FileStash) writeHashed(content string) (string, error) {
	if err := fs.EnsureDir(); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(content))
	name := hex.EncodeToString(sum[:])[:16] + ".txt"
	path := filepath.Join(fs.Dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("stash file %s: %w", path, err)
	}
	return filepath.Abs(path)
}

func (fs *FileStash) Read(path string, strategy gateway.TruncationStrategy, budget int) (string, error) {
	if fs == nil {
		return "", fmt.Errorf("file stash is nil")
	}
	jailed, err := sandbox.JailPath(fs.Dir, path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(jailed)
	if err != nil {
		return "", fmt.Errorf("read stashed file %s: %w", jailed, err)
	}
	if strategy == nil {
		strategy = gateway.MiddleOutStrategy{}
	}
	return strategy.Truncate(string(data), budget), nil
}
