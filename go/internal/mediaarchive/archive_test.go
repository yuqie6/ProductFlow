package mediaarchive

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteSameFilesHashStable(t *testing.T) {
	dir := t.TempDir()
	files := []File{
		{Name: "manifest.json", Data: []byte("{\"k\":1}\n")},
		{Name: "图片.png", Data: []byte("\x89PNG-fake-bytes")},
	}
	opts := Options{Filename: "out.zip", Pattern: "mediaarchive-*.zip", Dir: dir}

	first, err := Write(context.Background(), opts, files)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(first.Path) })
	second, err := Write(context.Background(), opts, files)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(second.Path) })

	a, err := os.ReadFile(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(second.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("zip bytes drifted: %d vs %d", len(a), len(b))
	}
	ha := sha256.Sum256(a)
	hb := sha256.Sum256(b)
	if ha != hb {
		t.Fatalf("sha256 %s vs %s", hex.EncodeToString(ha[:]), hex.EncodeToString(hb[:]))
	}
	if first.Filename != "out.zip" || second.Filename != "out.zip" {
		t.Fatalf("filename %q %q", first.Filename, second.Filename)
	}

	zr, err := zip.OpenReader(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if len(zr.File) != 2 || zr.File[0].Name != "manifest.json" || zr.File[1].Name != "图片.png" {
		t.Fatalf("entries %v", entryNames(zr))
	}
	for _, f := range zr.File {
		if f.Method != zip.Deflate {
			t.Fatalf("%s method %d", f.Name, f.Method)
		}
		if f.CreatorVersion>>8 != 3 {
			t.Fatalf("%s create_system %d want 3", f.Name, f.CreatorVersion>>8)
		}
		if f.ExternalAttrs != 0o600<<16 {
			t.Fatalf("%s external_attr %d want %d", f.Name, f.ExternalAttrs, 0o600<<16)
		}
		if len(f.Extra) != 0 {
			t.Fatalf("%s extra %q", f.Name, f.Extra)
		}
		if f.Comment != "" {
			t.Fatalf("%s comment %q", f.Name, f.Comment)
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		var want []byte
		for _, file := range files {
			if file.Name == f.Name {
				want = file.Data
				break
			}
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s payload mismatch", f.Name)
		}
	}
}

func TestBeginAddFinishMatchesWrite(t *testing.T) {
	dir := t.TempDir()
	files := []File{
		{Name: "manifest.json", Data: []byte("{\"k\":1}\n")},
		{Name: "图片.png", Data: []byte("\x89PNG-fake-bytes")},
	}
	opts := Options{Filename: "out.zip", Pattern: "mediaarchive-*.zip", Dir: dir}

	batched, err := Write(context.Background(), opts, files)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(batched.Path) })

	w, err := Begin(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Abort()
	for _, f := range files {
		if err := w.Add(context.Background(), f); err != nil {
			t.Fatal(err)
		}
	}
	streamed, err := w.Finish()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(streamed.Path) })

	a, err := os.ReadFile(batched.Path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(streamed.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("zip bytes drifted: %d vs %d", len(a), len(b))
	}
}

func TestWriteFailureAfterCreateTempLeavesNoFiles(t *testing.T) {
	dir := t.TempDir()
	orig := createTemp
	t.Cleanup(func() { createTemp = orig })
	createTemp = func(dir, pattern string) (tempFile, error) {
		f, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return nil, err
		}
		return failWrite{File: f}, nil
	}

	_, err := Write(context.Background(), Options{Filename: "out.zip", Pattern: "mediaarchive-*.zip", Dir: dir}, []File{
		{Name: "a.bin", Data: []byte("payload")},
	})
	if err == nil {
		t.Fatal("expected write failure")
	}
	if !errors.Is(err, errSimulatedWrite) {
		t.Fatalf("err %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "mediaarchive-*.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("left behind %v", matches)
	}
}

func TestWriteCancelledContextLeavesNoFiles(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Write(ctx, Options{Filename: "out.zip", Pattern: "mediaarchive-*.zip", Dir: dir}, []File{
		{Name: "a.bin", Data: []byte("payload")},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "mediaarchive-*.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("left behind %v", matches)
	}
}

func TestWriteCancelAfterCreateTempLeavesNoFiles(t *testing.T) {
	dir := t.TempDir()
	ctx := &nthCancel{Context: context.Background(), remaining: 1}
	_, err := Write(ctx, Options{Filename: "out.zip", Pattern: "mediaarchive-*.zip", Dir: dir}, []File{
		{Name: "a.bin", Data: []byte("payload")},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "mediaarchive-*.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("left behind %v", matches)
	}
}

type nthCancel struct {
	context.Context
	remaining int
}

func (c *nthCancel) Err() error {
	if c.remaining <= 0 {
		return context.Canceled
	}
	c.remaining--
	return nil
}

func TestAbortAfterCreateTempLeavesNoFiles(t *testing.T) {
	dir := t.TempDir()
	w, err := newWriter(Options{Filename: "out.zip", Pattern: "mediaarchive-*.zip", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	w.Abort()
	matches, err := filepath.Glob(filepath.Join(dir, "mediaarchive-*.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("left behind %v", matches)
	}
}

var errSimulatedWrite = errors.New("simulated write failure")

type failWrite struct {
	*os.File
}

func (f failWrite) Write([]byte) (int, error) {
	return 0, errSimulatedWrite
}

func entryNames(zr *zip.ReadCloser) []string {
	out := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		out = append(out, f.Name)
	}
	return out
}
