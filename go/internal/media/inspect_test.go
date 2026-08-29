package media

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/storage"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInspectPNG(t *testing.T) {
	content := pngBytes(t)
	got, err := Inspect(content, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if got.MIMEType != "image/png" || got.Width != 2 || got.Height != 2 || got.ByteSize != len(content) || len(got.SHA256) != 64 {
		t.Fatalf("%+v", got)
	}
}

func TestInspectRejectsEmpty(t *testing.T) {
	_, err := Inspect(nil, "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWriteMediaCompensates(t *testing.T) {
	root := t.TempDir()
	store := storage.Local{Root: root}
	var compensation storage.Compensation
	rel, err := store.WriteMedia("11111111-1111-4111-8111-111111111111", ".png", pngBytes(t), &compensation)
	if err != nil {
		t.Fatal(err)
	}
	if rel == "" {
		t.Fatal("empty rel")
	}
	compensation.Rollback()
}
