package imageeval

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/yuqie6/productflow/internal/product"
)

func productInputFixture(t *testing.T, root, id string) Manifest {
	t.Helper()
	body := []byte("identity reference " + id)
	m := Manifest{
		ID: id, URL: "https://item.taobao.com/item.htm?id=" + id, Title: "玻璃密封瓶", Category: "home",
		Props:      map[string]string{"材质": "玻璃"},
		References: []PoolImage{{Path: "reference.png", SHA256: FileSHA256(body)}},
		Gold:       []PoolImage{{Path: "gold-must-not-be-read.png", SHA256: "gold", TypeKey: "hero"}},
		ImageTypes: []TypeCount{{Key: "hero", Quantity: 1}},
	}
	if err := WriteAdmittedCase(root, m); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ResolvePoolFile(root, id, m.References[0].Path), body, 0o644); err != nil {
		t.Fatal(err)
	}
	return m
}

func writeInputFixture(t *testing.T, path string, snapshot ProductInputSnapshot) {
	t.Helper()
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedProductInputReachesCreateAndReportWithoutRegeneration(t *testing.T) {
	t.Setenv(RunSwitch, "1")
	root := t.TempDir()
	m := productInputFixture(t, root, "one")
	wantNote := "厚壁玻璃密封瓶\n\n材质：玻璃"
	prepared, created := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/session":
			w.WriteHeader(http.StatusOK)
		case "/api/v2/product-source-notes/generate", "/api/v3/products":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			defer r.MultipartForm.RemoveAll()
			files := r.MultipartForm.File["images"]
			if len(files) != 1 {
				t.Errorf("reference count %d", len(files))
			}
			for _, file := range files {
				f, err := file.Open()
				if err != nil {
					t.Error(err)
					continue
				}
				body, _ := io.ReadAll(f)
				f.Close()
				if string(body) != "identity reference one" {
					t.Errorf("unexpected provider input %q", body)
				}
			}
			if r.URL.Path == "/api/v2/product-source-notes/generate" {
				prepared++
				if r.FormValue("product_name") != m.Title || r.FormValue("current_note") != factsNote(m) {
					t.Error("source note request lost product facts")
				}
				_ = json.NewEncoder(w).Encode(product.GeneratedSourceNote{Visible: " 厚壁玻璃密封瓶 ", Fields: []product.GeneratedSourceNoteField{{Label: "材质", Value: "玻璃"}, {Label: "容量", Value: ""}}})
				return
			}
			created++
			if got := r.FormValue("source_note"); got != wantNote {
				t.Errorf("create source_note = %q", got)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"product":{"id":"product"},"graph":{"id":"graph"}}`)
		case "/api/v3/products/product/workflows/graph/runs":
			_, _ = io.WriteString(w, `{"id":"run"}`)
		case "/api/v3/products/product/workflows/graph/runs/run":
			_, _ = io.WriteString(w, `{"status":"unknown","failure_reason":"test stops before image generation"}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	cfg := RunConfig{StorageRoot: root, APIBase: server.URL, N: 1, Seed: 1, Commit: "candidate", Image: stubImage{}, Judge: stubJudge{}}
	path := filepath.Join(root, "inputs.json")
	snapshot, err := PrepareProductInputs(context.Background(), cfg, path)
	if err != nil || !snapshot.Complete || len(snapshot.Inputs) != 1 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	// The exact editable note is the run input; no fresh AI call occurs in run.
	cfg.ProductInputsPath = path
	report, err := RunSampled(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRunReport(root, report.RunID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if loaded.InputMode != "frozen_source_note" || loaded.ProductInputsSHA != FileSHA256(raw) || loaded.ProductInputs[0].SourceNote != wantNote || loaded.Passed {
		t.Fatalf("unexpected recorded input or result %+v", loaded)
	}
	if _, _, err := createDirect(context.Background(), server.Client(), cfg, m, loaded.ProductInputs[0].SourceNote); err != nil {
		t.Fatal(err)
	}
	if prepared != 1 || created != 2 {
		t.Fatalf("source-note calls=%d create calls=%d", prepared, created)
	}
	if _, err := PrepareProductInputs(context.Background(), cfg, path); err == nil || prepared != 1 {
		t.Fatal("existing snapshot must not be overwritten or trigger another model request")
	}
}

func TestProductInputPreflightRejectsWholeBatchBeforeHTTP(t *testing.T) {
	t.Setenv(RunSwitch, "1")
	root := t.TempDir()
	m := productInputFixture(t, root, "one")
	second := productInputFixture(t, root, "two")
	base, _, err := resolveProductInputs(root, []Manifest{m, second}, "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "inputs.json")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
	defer server.Close()
	cfg := RunConfig{StorageRoot: root, APIBase: server.URL, N: 2, Seed: 1, Image: stubImage{}, Judge: stubJudge{}, ProductInputsPath: path}
	for _, kind := range []string{"missing", "mismatch", "empty", "duplicate", "partial", "changed-bytes"} {
		t.Run(kind, func(t *testing.T) {
			inputs := append([]ProductInput(nil), base...)
			snapshot := ProductInputSnapshot{Complete: true, N: 2, Inputs: inputs}
			switch kind {
			case "missing":
				snapshot.N = 1
				snapshot.Inputs = inputs[:1]
			case "mismatch":
				snapshot.Inputs[1].IdentitySHA = "different product"
			case "empty":
				snapshot.Inputs[1].SourceNote = " "
			case "duplicate":
				snapshot.Inputs[1] = snapshot.Inputs[0]
			case "partial":
				snapshot.Complete = false
			case "changed-bytes":
				if err := os.WriteFile(ResolvePoolFile(root, second.ID, second.References[0].Path), []byte("replaced"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			writeInputFixture(t, path, snapshot)
			if _, err := RunSampled(context.Background(), cfg); err == nil {
				t.Fatal("invalid input accepted")
			}
			if calls != 0 {
				t.Fatalf("preflight made %d HTTP calls", calls)
			}
		})
	}
}

func TestPrepareProductInputsRetainsPartialAndDoesNotRetry(t *testing.T) {
	t.Setenv(RunSwitch, "1")
	root := t.TempDir()
	productInputFixture(t, root, "one")
	productInputFixture(t, root, "two")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/session" {
			return
		}
		calls++
		if calls == 1 {
			_, _ = io.WriteString(w, `{"visible":"confirmed description","fields":[]}`)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	path := filepath.Join(root, "partial.json")
	cfg := RunConfig{StorageRoot: root, APIBase: server.URL, N: 2, Seed: 1}
	if _, err := PrepareProductInputs(context.Background(), cfg, path); err == nil {
		t.Fatal("expected preparation failure")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved ProductInputSnapshot
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Complete || len(saved.Inputs) != 1 || calls != 2 {
		t.Fatalf("partial result=%+v calls=%d", saved, calls)
	}
	if _, err := PrepareProductInputs(context.Background(), cfg, path); err == nil || calls != 2 {
		t.Fatal("partial preparation retried")
	}
}

func TestProductInputIdentityTracksFactsAndReferenceOrder(t *testing.T) {
	root := t.TempDir()
	m := productInputFixture(t, root, "one")
	other := productInputFixture(t, root, "two")
	secondPath := "second.png"
	body, _ := os.ReadFile(ResolvePoolFile(root, other.ID, other.References[0].Path))
	if err := os.WriteFile(ResolvePoolFile(root, m.ID, secondPath), body, 0o644); err != nil {
		t.Fatal(err)
	}
	m.References = append(m.References, PoolImage{Path: secondPath, SHA256: FileSHA256(body)})
	before, err := productInputIdentity(root, m)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"title", "props", "order"} {
		changed := m
		switch change {
		case "title":
			changed.Title += "different"
		case "props":
			changed.Props = map[string]string{"材质": "塑料"}
		case "order":
			changed.References = []PoolImage{m.References[1], m.References[0]}
		}
		after, err := productInputIdentity(root, changed)
		if err != nil || after == before {
			t.Fatalf("%s not distinguished: %v", change, err)
		}
	}
}

func TestFormatGeneratedSourceNoteMatchesCreatePage(t *testing.T) {
	note := product.GeneratedSourceNote{Visible: " 厚壁玻璃密封瓶 ", Fields: []product.GeneratedSourceNoteField{
		{Label: " 材质 ", Value: " 玻璃 "}, {Label: "容量", Value: ""}, {Label: " ", Value: "unknown"},
	}}
	if got := formatGeneratedSourceNote(note); got != "厚壁玻璃密封瓶\n\n材质：玻璃" {
		t.Fatalf("unexpected source note %q", got)
	}
	note.Visible = strings.Repeat("瓶", 4001)
	if got := formatGeneratedSourceNote(note); utf8.RuneCountInString(got) != 4000 || !utf8.ValidString(got) {
		t.Fatal("source note must truncate at the same codepoint limit as create page")
	}
}

func TestPrepareProductInputsRequiresSwitch(t *testing.T) {
	t.Setenv(RunSwitch, "")
	path := filepath.Join(t.TempDir(), "unused.json")
	if _, err := PrepareProductInputs(context.Background(), RunConfig{}, path); err == nil {
		t.Fatal("expected opt-in gate")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("preparation wrote output without opt-in")
	}
}
