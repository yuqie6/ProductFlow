package imageeval

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPoolRoundTrip(t *testing.T) {
	root := t.TempDir()
	listing := ExtractListing{
		Source: "taobao", URL: "https://item.taobao.com/item.htm?id=pool1",
		Title: "陶瓷马克杯", Category: "home",
	}
	m, err := Admit(listing, completeImages())
	if err != nil {
		t.Fatal(err)
	}
	dir := caseDir(root, m.ID)
	if err := os.MkdirAll(filepath.Join(dir, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "gold"), 0o755); err != nil {
		t.Fatal(err)
	}
	png := solidPNG(t, 640, 640)
	for i := range m.References {
		rel := filepath.Join("references", "ref.png")
		if err := os.WriteFile(filepath.Join(dir, rel), png, 0o644); err != nil {
			t.Fatal(err)
		}
		m.References[i].Path = rel
	}
	for i := range m.Gold {
		rel := filepath.Join("gold", "g.png")
		if err := os.WriteFile(filepath.Join(dir, rel), png, 0o644); err != nil {
			t.Fatal(err)
		}
		m.Gold[i].Path = rel
	}
	if err := WriteAdmittedCase(root, m); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadAdmitted(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].ID != m.ID {
		t.Fatalf("%+v", loaded)
	}
}

func TestWriteAndLoadRunReport(t *testing.T) {
	root := t.TempDir()
	report := RunReport{RunID: "run-1", Seed: 3, N: 1, Passed: false, FailCount: 1}
	if err := writeRun(root, report); err != nil {
		t.Fatal(err)
	}
	got, err := LoadRunReport(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Seed != 3 || got.FailCount != 1 {
		t.Fatalf("%+v", got)
	}
}
