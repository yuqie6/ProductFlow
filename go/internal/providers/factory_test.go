package providers

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestImageFactoryFilesDoNotImportDomainPackages(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	dir := filepath.Dir(file)
	needles := []string{
		`"github.com/yuqie6/productflow/internal/graph"`,
		`"github.com/yuqie6/productflow/internal/imagesession"`,
		`"github.com/yuqie6/productflow/internal/localedit"`,
	}
	for _, name := range []string{"factory.go", "contract.go", "errors.go", "http.go", "image.go", "image_edit.go", "image_tool.go", "gemini.go"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)
		for _, needle := range needles {
			if strings.Contains(src, needle) {
				t.Errorf("%s must not import %s", name, needle)
			}
		}
	}
}

func TestMockImageGenerateAndEdit(t *testing.T) {
	ctx := context.Background()
	mock := MockImage{}
	workflow, err := mock.Generate(ctx, GenerateRequest{Prompt: "hero"})
	if err != nil {
		t.Fatal(err)
	}
	if len(workflow.Bytes) == 0 || workflow.MIME != "image/png" {
		t.Fatalf("workflow mock %+v", workflow)
	}
	chat, err := mock.Generate(ctx, GenerateRequest{Prompt: "x", Size: "64x64", Count: 2, Mode: ModeChat})
	if err != nil {
		t.Fatal(err)
	}
	if len(chat.Images) != 2 || len(chat.Bytes) == 0 {
		t.Fatalf("chat mock images=%d bytes=%d", len(chat.Images), len(chat.Bytes))
	}
	if mock.Capability().Supported {
		t.Fatal("mock must not declare masked local edit")
	}
	source := []byte("src")
	edited, err := mock.Edit(ctx, EditRequest{SourceBytes: source, Instruction: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if string(edited.Bytes) != "src" {
		t.Fatalf("edit %+v", edited)
	}
}

func TestImageFactoryMockWhenStoreNil(t *testing.T) {
	got, err := Image(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name() != "mock" {
		t.Fatalf("name %s", got.Name())
	}
}
