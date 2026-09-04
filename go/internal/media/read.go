package media

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"gorm.io/gorm"
)

// ReadKind 区分 ReadVerified 失败原因。调用方映射到各自的用户文案，不要把 Kind 写进 HTTP JSON。
type ReadKind string

const (
	ReadNotFound    ReadKind = "not_found"    // media_objects 行不存在
	ReadNotVerified ReadKind = "not_verified" // 状态不是 verified，或核验字段不完整
	ReadMissingFile ReadKind = "missing_file" // 磁盘上没有原图
	ReadCorrupt     ReadKind = "corrupt"      // 字节不是可解码的 PNG/JPEG/WEBP
	ReadIdentity    ReadKind = "identity"     // 文件与行上的 SHA/大小/尺寸/MIME 不一致
	ReadIO          ReadKind = "io"           // 普通 I/O（权限、瞬时读失败）
)

// 身份不一致时 ReadError.Field 的取值。
const (
	FieldMetadata = "metadata"
	FieldByteSize = "byte_size"
	FieldSHA256   = "sha256"
	FieldDims     = "dims"
	FieldMIME     = "mime_type"
)

var errSizeChanged = errors.New("media file size changed")

// ReadError 是 ReadVerified 的分类失败。Detail 是默认中文，调用方应改写成模块文案。
type ReadError struct {
	Kind   ReadKind
	Field  string // Identity/NotVerified 时指出哪一列；可空
	Detail string
	Err    error
}

func (e *ReadError) Error() string {
	if e == nil {
		return ""
	}
	if e.Detail != "" {
		return e.Detail
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Kind)
}

func (e *ReadError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// AsReadError 取出 [ReadError]。不是读核验失败时 ok=false。
func AsReadError(err error) (*ReadError, bool) {
	var re *ReadError
	if errors.As(err, &re) && re != nil {
		return re, true
	}
	return nil, false
}

// Content 是一次已核验读取：行投影、磁盘字节、解码后的身份。
type Content struct {
	Object   Object
	Bytes    []byte
	Verified Verified
}

// ReadVerified 按 MediaObject id 读回已核验字节。状态非 verified、文件缺失、损坏或与行身份不一致时返回 [ReadError]，不回退未核验字节。
func (s Store) ReadVerified(ctx context.Context, q *gorm.DB, mediaObjectID string) (Content, error) {
	obj, err := s.Get(ctx, q, mediaObjectID)
	if err != nil {
		var ae apperr.Error
		if errors.As(err, &ae) && ae.Status == 404 {
			return Content{}, &ReadError{Kind: ReadNotFound, Detail: "媒体对象不存在"}
		}
		return Content{}, err
	}
	if obj.VerificationStatus != StatusVerified {
		return Content{}, &ReadError{Kind: ReadNotVerified, Detail: "媒体尚未通过核验"}
	}
	if obj.ByteSize <= 0 || obj.Width <= 0 || obj.Height <= 0 || obj.SHA256 == "" || strings.TrimSpace(obj.MIMEType) == "" {
		return Content{}, &ReadError{Kind: ReadNotVerified, Field: FieldMetadata, Detail: "媒体缺少核验元数据"}
	}
	abs, err := s.Files.Resolve(obj.StoragePath)
	if err != nil {
		return Content{}, &ReadError{Kind: ReadMissingFile, Detail: "媒体文件不存在", Err: err}
	}
	data, err := readExactBounded(abs, obj.ByteSize)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Content{}, &ReadError{Kind: ReadMissingFile, Detail: "媒体文件不存在", Err: err}
		}
		if errors.Is(err, errSizeChanged) {
			return Content{}, &ReadError{Kind: ReadIdentity, Field: FieldByteSize, Detail: "媒体文件与核验元数据不一致"}
		}
		return Content{}, &ReadError{Kind: ReadIO, Detail: "读取媒体文件失败", Err: err}
	}
	verified, err := Inspect(data, obj.MIMEType)
	if err != nil {
		return Content{}, &ReadError{Kind: ReadCorrupt, Detail: "媒体文件已损坏", Err: err}
	}
	if verified.MIMEType != obj.MIMEType {
		return Content{}, &ReadError{Kind: ReadIdentity, Field: FieldMIME, Detail: "媒体文件与核验元数据不一致"}
	}
	if verified.ByteSize != obj.ByteSize {
		return Content{}, &ReadError{Kind: ReadIdentity, Field: FieldByteSize, Detail: "媒体文件与核验元数据不一致"}
	}
	if verified.Width != obj.Width || verified.Height != obj.Height {
		return Content{}, &ReadError{Kind: ReadIdentity, Field: FieldDims, Detail: "媒体文件与核验元数据不一致"}
	}
	if verified.SHA256 != obj.SHA256 {
		return Content{}, &ReadError{Kind: ReadIdentity, Field: FieldSHA256, Detail: "媒体文件与核验元数据不一致"}
	}
	return Content{Object: obj, Bytes: data, Verified: verified}, nil
}

func readExactBounded(abs string, expected int) ([]byte, error) {
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	content, err := io.ReadAll(io.LimitReader(f, int64(expected)+1))
	if err != nil {
		return nil, err
	}
	if len(content) != expected {
		return nil, errSizeChanged
	}
	return content, nil
}
