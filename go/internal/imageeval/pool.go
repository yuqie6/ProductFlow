package imageeval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// PoolRoot 是 STORAGE_ROOT/image-evals。
func PoolRoot(storageRoot string) string {
	return filepath.Join(storageRoot, StorageDir)
}

func caseDir(storageRoot, caseID string) string {
	return filepath.Join(PoolRoot(storageRoot), PoolDir, caseID)
}

// WriteAdmittedCase 把过线 Manifest 和像素写进 pool。像素已在 references/gold 相对路径下。
func WriteAdmittedCase(storageRoot string, m Manifest) error {
	if m.RejectReason != "" {
		return fmt.Errorf("refusing to write rejected case: %s", m.RejectReason)
	}
	dir := caseDir(storageRoot, m.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o644); err != nil {
		return err
	}
	return upsertIndex(storageRoot, m.ID, m.Category)
}

type poolIndex struct {
	Cases []indexEntry `json:"cases"`
}

type indexEntry struct {
	ID       string `json:"id"`
	Category string `json:"category"`
}

func upsertIndex(storageRoot, id, category string) error {
	path := filepath.Join(PoolRoot(storageRoot), PoolDir, "index.json")
	var idx poolIndex
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &idx)
	}
	found := false
	for i, item := range idx.Cases {
		if item.ID == id {
			idx.Cases[i].Category = category
			found = true
			break
		}
	}
	if !found {
		idx.Cases = append(idx.Cases, indexEntry{ID: id, Category: category})
	}
	sort.Slice(idx.Cases, func(i, j int) bool { return idx.Cases[i].ID < idx.Cases[j].ID })
	raw, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

// LoadAdmitted 读取 pool 里全部过线 Manifest。缺文件的条目跳过。
func LoadAdmitted(storageRoot string) ([]Manifest, error) {
	path := filepath.Join(PoolRoot(storageRoot), PoolDir, "index.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var idx poolIndex
	if err := json.Unmarshal(raw, &idx); err != nil {
		return nil, err
	}
	out := make([]Manifest, 0, len(idx.Cases))
	for _, item := range idx.Cases {
		m, err := LoadManifest(storageRoot, item.ID)
		if err != nil {
			continue
		}
		if m.RejectReason != "" {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// LoadManifest 读一个 case 的 manifest.json。
func LoadManifest(storageRoot, caseID string) (Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(caseDir(storageRoot, caseID), "manifest.json"))
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if extra := json.Unmarshal(raw, &m); extra != nil {
		return Manifest{}, extra
	}
	if m.ID == "" || m.URL == "" || len(m.References) == 0 || len(m.Gold) == 0 || len(m.ImageTypes) == 0 {
		return Manifest{}, fmt.Errorf("invalid manifest %s", caseID)
	}
	refHashes := make(map[string]struct{}, len(m.References))
	for _, ref := range m.References {
		refHashes[ref.SHA256] = struct{}{}
	}
	for _, gold := range m.Gold {
		if _, overlap := refHashes[gold.SHA256]; overlap {
			return Manifest{}, fmt.Errorf("invalid manifest %s: reference/gold content overlap", caseID)
		}
	}
	return m, nil
}

func ResolvePoolFile(storageRoot, caseID, rel string) string {
	return filepath.Join(caseDir(storageRoot, caseID), rel)
}
