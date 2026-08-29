package media

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/storage"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInspectPNG(t *testing.T) {
	content := pngBytes(t, 2, 2)
	got, err := Inspect(content, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if got.MIMEType != "image/png" || got.Width != 2 || got.Height != 2 || got.ByteSize != len(content) || len(got.SHA256) != 64 {
		t.Fatalf("%+v", got)
	}
}

func TestInspectRejectsEmpty(t *testing.T) {
	if _, err := Inspect(nil, ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestFormatByteSizeMatchesPython(t *testing.T) {
	if got := FormatByteSize(10 * 1024 * 1024); got != "10MB" {
		t.Fatalf("got %q", got)
	}
	if got := FormatByteSize(1536); got != "1.5KB" {
		t.Fatalf("got %q", got)
	}
}

func TestValidateUploadMIMEAndSize(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxImageBytes = 50
	var e apperr.Error
	_, err := ValidateUpload("too_large.png", "image/png", pngBytes(t, 8, 8), limits)
	if !errors.As(err, &e) || e.Status != 413 {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(e.Detail, "too_large.png") || !strings.Contains(e.Detail, "超过单张大小限制") {
		t.Fatalf("detail %q", e.Detail)
	}

	_, err = ValidateUpload("bad.gif", "image/gif", pngBytes(t, 2, 2), DefaultLimits())
	if !errors.As(err, &e) || e.Status != 415 || !strings.Contains(e.Detail, "格式不受支持") {
		t.Fatalf("err=%v", err)
	}

	_, err = ValidateUpload("cream.png", "image/png", []byte("not an image"), DefaultLimits())
	if !errors.As(err, &e) || e.Status != 400 || !strings.Contains(e.Detail, "不是可解码的有效图片") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateUploadAcceptsOctetStreamAndAliases(t *testing.T) {
	content := pngBytes(t, 4, 4)
	got, err := ValidateUpload("x.png", "application/octet-stream", content, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.MIMEType != "image/png" {
		t.Fatalf("%+v", got)
	}
	if _, err := ValidateUpload("x.png", "image/x-png", content, DefaultLimits()); err != nil {
		t.Fatal(err)
	}
}

func TestWriteMediaCompensatesIncludingVariants(t *testing.T) {
	root := t.TempDir()
	store := storage.Local{Root: root}
	var compensation storage.Compensation
	rel, err := store.WriteMedia("11111111-1111-4111-8111-111111111111", ".png", pngBytes(t, 24, 18), &compensation)
	if err != nil {
		t.Fatal(err)
	}
	abs, err := store.Resolve(rel)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatal(err)
	}
	preview, err := store.ResolveForVariant(rel, "image/png", storage.VariantPreview)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(preview.AbsPath); err != nil {
		t.Fatal(err)
	}
	compensation.Rollback()
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Fatalf("original still present: %v", err)
	}
	if _, err := os.Stat(preview.AbsPath); !os.IsNotExist(err) {
		t.Fatalf("preview still present: %v", err)
	}
}

func TestPreviewFitsMaxEdge(t *testing.T) {
	root := t.TempDir()
	store := storage.Local{Root: root}
	rel, err := store.WriteMedia("22222222-2222-4222-8222-222222222222", ".png", pngBytes(t, 2400, 1800), nil)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := store.ResolveForVariant(rel, "image/png", storage.VariantPreview)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(mustRead(t, preview.AbsPath)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width > 1600 || cfg.Height > 1600 {
		t.Fatalf("preview too large %+v", cfg)
	}
	thumb, err := store.ResolveForVariant(rel, "image/png", storage.VariantThumbnail)
	if err != nil {
		t.Fatal(err)
	}
	thumbCfg, _, err := image.DecodeConfig(bytes.NewReader(mustRead(t, thumb.AbsPath)))
	if err != nil {
		t.Fatal(err)
	}
	if thumbCfg.Width > 320 || thumbCfg.Height > 320 {
		t.Fatalf("thumbnail too large %+v", thumbCfg)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
