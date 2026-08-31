package library

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func provenanceHash(p Provenance) (string, error) {
	encoded, err := canonicalJSON(dumpForHash(p))
	if err != nil {
		return "", err
	}
	if len(encoded) > maxProvenance {
		return "", fmt.Errorf("media library provenance payload exceeds maximum size")
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func dumpForHash(p Provenance) map[string]any {
	var origin any
	if p.OriginType != nil {
		origin = *p.OriginType
	}
	return map[string]any{
		"schema_version":    p.SchemaVersion,
		"source_type":       p.SourceType,
		"source_id":         p.SourceID,
		"sha256":            p.SHA256,
		"mime_type":         p.MIMEType,
		"byte_size":         p.ByteSize,
		"width":             p.Width,
		"height":            p.Height,
		"original_filename": p.OriginalFilename,
		"origin_type":       origin,
		"captured_at":       pydanticDateTime(p.CapturedAt),
	}
}

func storeProvenance(p Provenance) map[string]any {
	payload := map[string]any{
		"schema_version":    p.SchemaVersion,
		"source_type":       p.SourceType,
		"source_id":         p.SourceID,
		"sha256":            p.SHA256,
		"mime_type":         p.MIMEType,
		"byte_size":         p.ByteSize,
		"width":             p.Width,
		"height":            p.Height,
		"original_filename": p.OriginalFilename,
		"captured_at":       pythonISOFormat(p.CapturedAt),
	}
	if p.OriginType != nil {
		payload["origin_type"] = *p.OriginType
	}
	return payload
}

func pydanticDateTime(t time.Time) string {
	t = t.UTC()
	usec := t.Nanosecond() / 1000
	if usec == 0 {
		return t.Format("2006-01-02T15:04:05Z")
	}
	return fmt.Sprintf("%s.%06dZ", t.Format("2006-01-02T15:04:05"), usec)
}

func pythonISOFormat(t time.Time) string {
	t = t.UTC()
	usec := t.Nanosecond() / 1000
	if usec == 0 {
		return t.Format("2006-01-02T15:04:05+00:00")
	}
	return fmt.Sprintf("%s.%06d+00:00", t.Format("2006-01-02T15:04:05"), usec)
}

// parseProvenance 校验闭集字段与 schema_version=1。未知键或非法尺寸/摘要返回 error，供哈希前拦截。
func parseProvenance(raw map[string]any) (Provenance, error) {
	allowed := map[string]struct{}{
		"schema_version": {}, "source_type": {}, "source_id": {}, "sha256": {},
		"mime_type": {}, "byte_size": {}, "width": {}, "height": {},
		"original_filename": {}, "origin_type": {}, "captured_at": {},
	}
	for key := range raw {
		if _, ok := allowed[key]; !ok {
			return Provenance{}, fmt.Errorf("unsupported media library provenance field")
		}
	}
	version, err := asInt(raw["schema_version"])
	if err != nil || version != 1 {
		return Provenance{}, fmt.Errorf("unsupported media library provenance schema version")
	}
	sourceType, _ := raw["source_type"].(string)
	if sourceType != SourceSession && sourceType != SourceProduct && sourceType != SourceUpload {
		return Provenance{}, fmt.Errorf("unsupported media library provenance source_type")
	}
	sourceID, _ := raw["source_id"].(string)
	if sourceID == "" || utf8.RuneCountInString(sourceID) > 36 {
		return Provenance{}, fmt.Errorf("invalid provenance source_id")
	}
	sum, _ := raw["sha256"].(string)
	if !sha256Pattern.MatchString(sum) {
		return Provenance{}, fmt.Errorf("invalid provenance sha256")
	}
	mime, _ := raw["mime_type"].(string)
	if mime == "" || utf8.RuneCountInString(mime) > 100 {
		return Provenance{}, fmt.Errorf("invalid provenance mime_type")
	}
	byteSize, err := asInt(raw["byte_size"])
	if err != nil || byteSize < 1 {
		return Provenance{}, fmt.Errorf("invalid provenance byte_size")
	}
	width, err := asInt(raw["width"])
	if err != nil || width < 1 {
		return Provenance{}, fmt.Errorf("invalid provenance width")
	}
	height, err := asInt(raw["height"])
	if err != nil || height < 1 {
		return Provenance{}, fmt.Errorf("invalid provenance height")
	}
	filename, _ := raw["original_filename"].(string)
	if filename == "" || utf8.RuneCountInString(filename) > 255 {
		return Provenance{}, fmt.Errorf("invalid provenance original_filename")
	}
	var origin *string
	if value, ok := raw["origin_type"]; ok && value != nil {
		text, ok := value.(string)
		if !ok || utf8.RuneCountInString(text) > 40 {
			return Provenance{}, fmt.Errorf("invalid provenance origin_type")
		}
		origin = &text
	}
	capturedRaw, _ := raw["captured_at"].(string)
	captured, err := parseDateTime(capturedRaw)
	if err != nil {
		return Provenance{}, err
	}
	return Provenance{
		SchemaVersion:    1,
		SourceType:       sourceType,
		SourceID:         sourceID,
		SHA256:           sum,
		MIMEType:         mime,
		ByteSize:         byteSize,
		Width:            width,
		Height:           height,
		OriginalFilename: filename,
		OriginType:       origin,
		CapturedAt:       captured,
	}, nil
}

func parseDateTime(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, fmt.Errorf("invalid provenance captured_at")
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02T15:04:05.999999-07:00", raw); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("invalid provenance captured_at")
}

func asInt(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		if n != float64(int(n)) {
			return 0, fmt.Errorf("not int")
		}
		return int(n), nil
	case jsonNumber:
		i, err := n.Int64()
		return int(i), err
	default:
		return 0, fmt.Errorf("not int")
	}
}

type jsonNumber interface {
	Int64() (int64, error)
}

func normalizeIdempotency(value, emptyDetail string) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", apperr.Validation(emptyDetail)
	}
	if len(normalized) > maxIdempotency {
		return "", apperr.Validationf("%s不能超过 %d bytes", strings.TrimSuffix(emptyDetail, "不能为空"), maxIdempotency)
	}
	return normalized, nil
}

func collectRequestHash(productID string, libraryIDs []string) (string, error) {
	encoded, err := canonicalJSON(map[string]any{
		"request_kind":            "media_library_collect_to_product_v1",
		"product_id":              productID,
		"media_library_asset_ids": libraryIDs,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// uploadRequestHash 按文件内容 sha256 排序后做幂等摘要，与上传顺序无关。
func uploadRequestHash(folderID *string, items []UploadItem) (string, error) {
	files := make([]any, 0, len(items))
	for _, item := range items {
		sum := sha256.Sum256(item.Content)
		var mime any
		if item.MIMEType != "" {
			mime = item.MIMEType
		}
		files = append(files, []any{item.Filename, hex.EncodeToString(sum[:]), mime})
	}
	sort.Slice(files, func(i, j int) bool {
		left, _ := marshalUnescaped(files[i])
		right, _ := marshalUnescaped(files[j])
		return string(left) < string(right)
	})
	var folder any
	if folderID != nil {
		folder = *folderID
	}
	encoded, err := canonicalJSON(map[string]any{
		"request_kind": "media_library_upload_v1",
		"folder_id":    folder,
		"files":        files,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func filterSignature(search, sourceType string, includeArchived bool, folderID, tag string) string {
	encoded, err := canonicalJSON(map[string]any{
		"folder_id":        folderID,
		"include_archived": includeArchived,
		"search":           search,
		"source_type":      sourceType,
		"tag":              tag,
	})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
