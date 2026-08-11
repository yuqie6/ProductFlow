// Package artifact stores large tool outputs outside model context while
// keeping a content-addressed reference that survives session restore.
package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	EnvelopeType     = "harness.tool_output/v1"
	MaxPageBytes     = 1 << 20
	maxArtifactBytes = 16 << 20
)

var validID = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type Reference struct {
	ID    string `json:"id"`
	Bytes int    `json:"bytes"`
}

type Envelope struct {
	Type     string    `json:"type"`
	Artifact Reference `json:"artifact"`
	Preview  string    `json:"preview"`
	Notice   string    `json:"notice"`
}

type Page struct {
	ID         string `json:"id"`
	Offset     int64  `json:"offset"`
	EndOffset  int64  `json:"end_offset"`
	NextOffset int64  `json:"next_offset,omitempty"`
	TotalBytes int64  `json:"total_bytes"`
	Content    string `json:"content"`
}

type FileStore struct {
	dir string
}

func NewFileStore(harnessHome string) (*FileStore, error) {
	harnessHome = strings.TrimSpace(harnessHome)
	if harnessHome == "" {
		return nil, errors.New("artifact 存储目录不能为空")
	}
	dir := filepath.Join(harnessHome, "artifacts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建 artifact 目录: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("设置 artifact 目录权限: %w", err)
	}
	return &FileStore{dir: dir}, nil
}

// Pack leaves small output inline. Large output is persisted exactly once and
// replaced with a bounded JSON envelope that is safe to place in model context.
func (s *FileStore) Pack(output string, inlineLimit int) (string, *Reference, error) {
	if s == nil || inlineLimit <= 0 || len(output) <= inlineLimit {
		return output, nil, nil
	}
	if len(output) > maxArtifactBytes {
		return "", nil, fmt.Errorf("tool output 超过 artifact 上限 %d MiB", maxArtifactBytes>>20)
	}
	digest := sha256.Sum256([]byte(output))
	reference := Reference{ID: "sha256:" + hex.EncodeToString(digest[:]), Bytes: len(output)}
	if err := s.write(reference, []byte(output)); err != nil {
		return "", nil, err
	}
	envelope := Envelope{
		Type: EnvelopeType, Artifact: reference,
		Notice: "完整输出已保存。需要更多内容时调用 read_artifact，并使用 next_offset 分页。",
	}
	previewLimit := inlineLimit
	for {
		envelope.Preview = Preview(output, previewLimit)
		encoded, err := json.Marshal(envelope)
		if err != nil {
			return "", nil, err
		}
		if len(encoded) <= inlineLimit+1024 || previewLimit == 0 {
			return string(encoded), &reference, nil
		}
		previewLimit /= 2
	}
}

func ParseEnvelope(value string) (Envelope, bool) {
	var envelope Envelope
	if json.Unmarshal([]byte(value), &envelope) != nil || envelope.Type != EnvelopeType ||
		!validID.MatchString(envelope.Artifact.ID) || envelope.Artifact.Bytes < 0 {
		return Envelope{}, false
	}
	return envelope, true
}

func (s *FileStore) Load(reference Reference) (string, error) {
	if s == nil {
		return "", errors.New("artifact store 未配置")
	}
	if !validID.MatchString(reference.ID) {
		return "", fmt.Errorf("artifact ID 无效: %q", reference.ID)
	}
	file, err := os.Open(s.path(reference.ID))
	if err != nil {
		return "", fmt.Errorf("打开 artifact %s: %w", reference.ID, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxArtifactBytes+1))
	if err != nil {
		return "", fmt.Errorf("读取 artifact %s: %w", reference.ID, err)
	}
	if len(data) > maxArtifactBytes {
		return "", fmt.Errorf("artifact %s 超过 %d MiB 上限", reference.ID, maxArtifactBytes>>20)
	}
	if err := verify(reference, data); err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *FileStore) ReadPage(id string, offset int64, limit int) (Page, error) {
	if s == nil {
		return Page{}, errors.New("artifact store 未配置")
	}
	if !validID.MatchString(id) {
		return Page{}, fmt.Errorf("artifact ID 无效: %q", id)
	}
	if offset < 0 {
		return Page{}, errors.New("artifact offset 不能为负数")
	}
	if limit < 1 || limit > MaxPageBytes {
		return Page{}, fmt.Errorf("artifact limit 必须在 1..%d", MaxPageBytes)
	}
	file, err := os.Open(s.path(id))
	if err != nil {
		return Page{}, fmt.Errorf("打开 artifact %s: %w", id, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Page{}, err
	}
	if info.Size() > maxArtifactBytes {
		return Page{}, fmt.Errorf("artifact %s 超过 %d MiB 上限", id, maxArtifactBytes>>20)
	}
	if offset > info.Size() {
		return Page{}, fmt.Errorf("artifact offset=%d 超出总字节数 %d", offset, info.Size())
	}
	data := make([]byte, min(limit, int(info.Size()-offset)))
	if _, err := file.ReadAt(data, offset); err != nil && !errors.Is(err, io.EOF) {
		return Page{}, err
	}
	end := offset + int64(len(data))
	page := Page{ID: id, Offset: offset, EndOffset: end, TotalBytes: info.Size(), Content: strings.ToValidUTF8(string(data), "\uFFFD")}
	if end < info.Size() {
		page.NextOffset = end
	}
	return page, nil
}

func (s *FileStore) write(reference Reference, data []byte) error {
	path := s.path(reference.ID)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		existing, loadErr := s.Load(reference)
		if loadErr != nil {
			return fmt.Errorf("校验已有 artifact %s: %w", reference.ID, loadErr)
		}
		if existing != string(data) {
			return fmt.Errorf("已有 artifact %s 内容不一致", reference.ID)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("创建 artifact %s: %w", reference.ID, err)
	}
	pathCommitted := false
	defer func() {
		file.Close()
		if !pathCommitted {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("写入 artifact %s: %w", reference.ID, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("同步 artifact %s: %w", reference.ID, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("关闭 artifact %s: %w", reference.ID, err)
	}
	if err := syncDirectory(s.dir); err != nil {
		return err
	}
	pathCommitted = true
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开 artifact 目录: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("同步 artifact 目录: %w", err)
	}
	return nil
}

func (s *FileStore) path(id string) string {
	if !validID.MatchString(id) {
		return filepath.Join(s.dir, "invalid")
	}
	return filepath.Join(s.dir, strings.TrimPrefix(id, "sha256:")+".txt")
}

func verify(reference Reference, data []byte) error {
	if !validID.MatchString(reference.ID) {
		return fmt.Errorf("artifact ID 无效: %q", reference.ID)
	}
	digest := sha256.Sum256(data)
	actual := "sha256:" + hex.EncodeToString(digest[:])
	if actual != reference.ID || (reference.Bytes > 0 && reference.Bytes != len(data)) {
		return fmt.Errorf("artifact %s 内容校验失败", reference.ID)
	}
	return nil
}

func Preview(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	const marker = "\n... [artifact 中省略内容] ...\n"
	available := max(0, limit-len(marker))
	head := available * 3 / 4
	tail := available - head
	preview := value[:head] + marker + value[len(value)-tail:]
	return strings.ToValidUTF8(preview, "\uFFFD")
}
