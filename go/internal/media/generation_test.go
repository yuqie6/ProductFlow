package media

import (
	"errors"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

func TestGenerationBudgetsMatchDefaultLimits(t *testing.T) {
	d := DefaultLimits()
	if GenerationMaxImageBytes != d.MaxImageBytes {
		t.Fatalf("image %d want %d", GenerationMaxImageBytes, d.MaxImageBytes)
	}
	if GenerationMaxBatchBytes != d.MaxBatchBytes {
		t.Fatalf("batch %d want %d", GenerationMaxBatchBytes, d.MaxBatchBytes)
	}
}

func TestSumBytes(t *testing.T) {
	if SumBytes(nil) != 0 || SumBytes([][]byte{nil, {}}) != 0 {
		t.Fatal("empty")
	}
	if SumBytes([][]byte{{1, 2}, {3}}) != 3 {
		t.Fatal("sum")
	}
}

func TestRejectGenerationInput(t *testing.T) {
	if err := RejectGenerationInput(nil); err != nil {
		t.Fatal(err)
	}
	exact := make([]byte, GenerationMaxBatchBytes)
	if err := RejectGenerationInput([][]byte{exact}); err != nil {
		t.Fatal(err)
	}
	err := RejectGenerationInput([][]byte{make([]byte, GenerationMaxBatchBytes+1)})
	var ae apperr.Error
	if !errors.As(err, &ae) || ae.Status != 400 || !strings.Contains(ae.Detail, "发给供应商的图片总大小超过限制") {
		t.Fatalf("err=%v", err)
	}
}

func TestRejectGenerationOutput(t *testing.T) {
	small := []byte{1, 2, 3}
	if err := RejectGenerationOutput([][]byte{small}, "image/png"); err != nil {
		t.Fatal(err)
	}
	if err := RejectGenerationOutput([][]byte{small}, ""); err != nil {
		t.Fatal(err)
	}

	var ae apperr.Error
	err := RejectGenerationOutput([][]byte{small}, "image/gif")
	if !errors.As(err, &ae) || ae.Status != 400 || !strings.Contains(ae.Detail, "格式不受支持") {
		t.Fatalf("mime err=%v", err)
	}

	err = RejectGenerationOutput([][]byte{make([]byte, GenerationMaxImageBytes+1)}, "image/png")
	if !errors.As(err, &ae) || !strings.Contains(ae.Detail, "超过单张大小限制") {
		t.Fatalf("single err=%v", err)
	}

	chunk := make([]byte, GenerationMaxImageBytes)
	err = RejectGenerationOutput([][]byte{chunk, chunk, chunk, chunk, chunk, chunk}, "image/png")
	if !errors.As(err, &ae) || !strings.Contains(ae.Detail, "图片总大小超过限制") {
		t.Fatalf("batch err=%v", err)
	}

	if err := RejectGenerationOutput([][]byte{chunk, chunk, chunk, chunk, chunk}, "image/png"); err != nil {
		t.Fatal(err)
	}
}

func TestInspectAcceptsTrailingBytes(t *testing.T) {
	base := pngBytes(t, 2, 2)
	padded := append(append([]byte{}, base...), make([]byte, 2048)...)
	if _, err := Inspect(padded, "image/png"); err != nil {
		t.Fatal(err)
	}
}
