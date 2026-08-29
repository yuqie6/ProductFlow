package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

type Local struct {
	Root string
}

type Compensation struct {
	created []string
}

func (c *Compensation) Track(absPath string) {
	c.created = append(c.created, absPath)
}

func (c *Compensation) Rollback() {
	for i := len(c.created) - 1; i >= 0; i-- {
		_ = os.Remove(c.created[i])
	}
}

func (s Local) WriteMedia(mediaID, extension string, content []byte, compensation *Compensation) (rel string, err error) {
	if extension == "" {
		extension = ".bin"
	}
	rel = filepath.ToSlash(filepath.Join("media", mediaID[:2], mediaID+extension))
	abs := filepath.Join(s.Root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, content, 0o644); err != nil {
		return "", err
	}
	if compensation != nil {
		compensation.Track(abs)
	}
	return rel, nil
}

func NewID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic(err)
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

func RandomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
