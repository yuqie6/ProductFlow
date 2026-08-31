// Package storage 在 STORAGE_ROOT 下保存媒体原图与 preview/thumbnail 变体。
package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/clockid"
)

// Local 是 STORAGE_ROOT 上的文件系统后端。Root 必须是绝对或相对仓库根的目录。
type Local struct {
	Root string // STORAGE_ROOT，绝对或相对仓库根
}

type write struct {
	store Local
	rel   string
}

// Compensation 跟踪一次命令里写入的相对路径，失败时 Rollback 删原图与变体。
type Compensation struct {
	writes []write
}

// Track 记录一次写入的相对路径，供失败时 Rollback。
// c 为 nil 时只返回 rel，不记录——调用方若忘了传 Compensation，事务失败不会删文件。
func (c *Compensation) Track(store Local, rel string) string {
	if c == nil {
		return rel
	}
	c.writes = append(c.writes, write{store: store, rel: rel})
	return rel
}

// Release 在事务提交成功后丢掉跟踪，避免随后误 Rollback 删掉已提交文件。
// c 为 nil 时是空操作。必须先 commit 再 Release；顺序反了会在失败路径漏删。
func (c *Compensation) Release() {
	if c == nil {
		return
	}
	c.writes = nil
}

// Rollback 逆序删除已 Track 的文件；删除 error 被吞掉。c 为 nil 时是空操作。
func (c *Compensation) Rollback() {
	if c == nil {
		return
	}
	for i := len(c.writes) - 1; i >= 0; i-- {
		_ = c.writes[i].store.DeleteWithVariants(c.writes[i].rel)
	}
	c.writes = nil
}

// WriteMedia 把字节写到 media/{id前2位}/{uuid}{ext} 并预热变体。变体失败会删刚写入的文件。
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

// Resolve 把相对存储路径变成 Root 下的绝对路径。绝对路径、".." 越界返回 error。
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

// RemoveEmptyProductDirs 尽力删掉 products/{id} 下已空的目录；新写入走 media/ 时通常是空操作。
func (s Local) RemoveEmptyProductDirs(productID string) {
	if productID == "" || strings.Contains(productID, "..") {
		return
	}
	abs, err := s.Resolve(filepath.ToSlash(filepath.Join("products", productID)))
	if err != nil {
		return
	}
	_ = os.Remove(abs)
}

// NewID 生成媒体对象主键（UUID v4，经 clockid）。
// 调用时机：Stage 新 MediaObject。不要用业务资产 id 当存储文件名。
func NewID() string {
	return clockid.New()
}
