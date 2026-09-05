package imageeval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/product"
)

// ProductInput records the exact business input supplied to product creation.
type ProductInput struct {
	CaseID      string                       `json:"case_id"`
	IdentitySHA string                       `json:"identity_sha256"`
	SourceNote  string                       `json:"source_note"`
	Generated   *product.GeneratedSourceNote `json:"generated,omitempty"`
}

type ProductInputSnapshot struct {
	Commit    string         `json:"commit"`
	CreatedAt time.Time      `json:"created_at"`
	Seed      int64          `json:"seed"`
	N         int            `json:"n"`
	Complete  bool           `json:"complete"`
	Inputs    []ProductInput `json:"inputs"`
}

// PrepareProductInputs makes one source-note request per sampled product. The
// exclusive output file also preserves partial progress without automatic retry.
func PrepareProductInputs(ctx context.Context, cfg RunConfig, path string) (ProductInputSnapshot, error) {
	var snapshot ProductInputSnapshot
	if os.Getenv(RunSwitch) != "1" {
		return snapshot, fmt.Errorf("set %s=1 to prepare product inputs", RunSwitch)
	}
	pool, err := LoadAdmitted(cfg.StorageRoot)
	if err != nil {
		return snapshot, err
	}
	sample, err := SampleStratified(pool, cfg.N, cfg.Seed)
	if err != nil {
		return snapshot, err
	}
	inputs, _, err := resolveProductInputs(cfg.StorageRoot, sample, "")
	if err != nil {
		return snapshot, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return snapshot, err
	}
	defer f.Close()
	snapshot = ProductInputSnapshot{Commit: cfg.Commit, CreatedAt: time.Now().UTC(), Seed: cfg.Seed, N: len(sample), Inputs: []ProductInput{}}
	if err := writeProductInputSnapshot(f, snapshot); err != nil {
		return snapshot, err
	}
	client, err := cfg.http()
	if err != nil {
		return snapshot, err
	}
	if err := login(ctx, client, cfg.APIBase, cfg.AdminKey); err != nil {
		return snapshot, err
	}
	for i, m := range sample {
		generated, err := generateProductInput(ctx, client, cfg, m)
		if err != nil {
			return snapshot, fmt.Errorf("prepare %s (partial snapshot retained; no retry): %w", m.ID, err)
		}
		input := inputs[i]
		input.Generated = &generated
		input.SourceNote = formatGeneratedSourceNote(generated)
		if input.SourceNote == "" {
			return snapshot, fmt.Errorf("prepare %s: empty source note", m.ID)
		}
		snapshot.Inputs = append(snapshot.Inputs, input)
		if err := writeProductInputSnapshot(f, snapshot); err != nil {
			return snapshot, err
		}
	}
	snapshot.Complete = true
	return snapshot, writeProductInputSnapshot(f, snapshot)
}

func writeProductInputSnapshot(f *os.File, snapshot ProductInputSnapshot) error {
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := f.Write(raw); err != nil {
		return err
	}
	if err := f.Truncate(int64(len(raw))); err != nil {
		return err
	}
	return f.Sync()
}

func generateProductInput(ctx context.Context, client *http.Client, cfg RunConfig, m Manifest) (product.GeneratedSourceNote, error) {
	var note product.GeneratedSourceNote
	refs, err := loadRefs(cfg.StorageRoot, m)
	if err != nil {
		return note, err
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("product_name", truncateRunes(m.Title, 80))
	_ = w.WriteField("current_note", truncateRunes(factsNote(m), 4000))
	for _, ref := range refs {
		part, err := w.CreateFormFile("images", ref.Filename)
		if err != nil {
			return note, err
		}
		if _, err := part.Write(ref.Bytes); err != nil {
			return note, err
		}
	}
	if err := w.Close(); err != nil {
		return note, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.APIBase, "/")+"/api/v2/product-source-notes/generate", &body)
	if err != nil {
		return note, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		return note, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return note, fmt.Errorf("source note http %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&note); err != nil {
		return note, err
	}
	return note, nil
}

// Matches web/src/pages/product-create/sourceNote.ts: omit unknown values and
// separate visible prose and nonempty specifications with blank lines.
func formatGeneratedSourceNote(note product.GeneratedSourceNote) string {
	parts := []string{}
	if visible := strings.TrimSpace(note.Visible); visible != "" {
		parts = append(parts, visible)
	}
	for _, field := range note.Fields {
		label, value := strings.TrimSpace(field.Label), strings.TrimSpace(field.Value)
		if label != "" && value != "" {
			parts = append(parts, label+"："+value)
		}
	}
	return truncateRunes(strings.Join(parts, "\n\n"), 4000)
}

func productInputIdentity(root string, m Manifest) (string, error) {
	refs, err := loadRefs(root, m)
	if err != nil {
		return "", err
	}
	hashes := make([]string, len(refs))
	for i, ref := range refs {
		hashes[i] = FileSHA256(ref.Bytes)
		if hashes[i] != m.References[i].SHA256 {
			return "", fmt.Errorf("case %s reference %d bytes differ from manifest", m.ID, i)
		}
	}
	raw, err := json.Marshal(struct {
		ID, URL, Title, Category string
		Props                    map[string]string
		References               []string
	}{m.ID, m.URL, m.Title, m.Category, m.Props, hashes})
	if err != nil {
		return "", err
	}
	return FileSHA256(raw), nil
}

func resolveProductInputs(root string, sample []Manifest, path string) ([]ProductInput, string, error) {
	prepared := map[string]ProductInput{}
	sha := ""
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, "", err
		}
		var snapshot ProductInputSnapshot
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			return nil, "", err
		}
		if !snapshot.Complete || snapshot.N != len(snapshot.Inputs) {
			return nil, "", fmt.Errorf("product input snapshot is incomplete")
		}
		sha = FileSHA256(raw)
		for _, input := range snapshot.Inputs {
			if _, duplicate := prepared[input.CaseID]; duplicate {
				return nil, "", fmt.Errorf("duplicate product input %s", input.CaseID)
			}
			prepared[input.CaseID] = input
		}
	}
	inputs := make([]ProductInput, 0, len(sample))
	for _, m := range sample {
		identity, err := productInputIdentity(root, m)
		if err != nil {
			return nil, "", err
		}
		input := ProductInput{CaseID: m.ID, IdentitySHA: identity, SourceNote: factsNote(m)}
		if path != "" {
			frozen, ok := prepared[m.ID]
			if !ok || frozen.IdentitySHA != identity || strings.TrimSpace(frozen.SourceNote) == "" {
				return nil, "", fmt.Errorf("missing, empty or mismatched product input for %s", m.ID)
			}
			input = frozen
		}
		inputs = append(inputs, input)
	}
	return inputs, sha, nil
}
