package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/clockid"
)

type Local struct {
	Root string
}

type write struct {
	store Local
	rel   string
}

type Compensation struct {
	writes []write
}

func (c *Compensation) Track(store Local, rel string) string {
	if c == nil {
		return rel
	}
	c.writes = append(c.writes, write{store: store, rel: rel})
	return rel
}

func (c *Compensation) Release() {
	if c == nil {
		return
	}
	c.writes = nil
}

func (c *Compensation) Rollback() {
	if c == nil {
		return
	}
	for i := len(c.writes) - 1; i >= 0; i-- {
		_ = c.writes[i].store.DeleteWithVariants(c.writes[i].rel)
	}
	c.writes = nil
}

func (s Local) WriteMedia(mediaID, extension string, content []byte, compensation *Compensation) (rel string, err error) {
	normalized, err := clockid.Normalize(mediaID)
	if err != nil {
		return "", fmt.Errorf("媒体 ID 必须是 UUID")
	}
	if extension == "" {
		extension = ".bin"
	}
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	rel = filepath.ToSlash(filepath.Join("media", normalized[:2], normalized+extension))
	if err := s.writeRelative(rel, content); err != nil {
		return "", err
	}
	compensation.Track(s, rel)
	if err := s.warmVariants(rel); err != nil {
		_ = s.DeleteWithVariants(rel)
		if compensation != nil && len(compensation.writes) > 0 {
			compensation.writes = compensation.writes[:len(compensation.writes)-1]
		}
		return "", err
	}
	return rel, nil
}

func (s Local) writeRelative(rel string, content []byte) error {
	abs, err := s.Resolve(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, content, 0o644)
}

func (s Local) Resolve(relativePath string) (string, error) {
	if relativePath == "" || filepath.IsAbs(relativePath) {
		return "", fmt.Errorf("存储路径必须是相对路径")
	}
	cleaned := filepath.Clean(filepath.FromSlash(relativePath))
	if strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("存储路径越界")
	}
	root, err := filepath.Abs(s.Root)
	if err != nil {
		return "", err
	}
	abs := filepath.Join(root, cleaned)
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("存储路径越界")
	}
	return abs, nil
}

func NewID() string {
	return clockid.New()
}
