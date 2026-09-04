package imageeval

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
)

// IngestListing 下载抽出 JSON 里的图、分类、准入，过线则写入 pool。
func IngestListing(ctx context.Context, storageRoot string, listing ExtractListing, client *http.Client) (Manifest, error) {
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second}
	}
	if strings.TrimSpace(listing.URL) == "" || strings.TrimSpace(listing.Title) == "" {
		return Manifest{}, fmt.Errorf("listing url and title are required")
	}
	if listing.Source == "" {
		listing.Source = "taobao"
	}
	caseID := CaseIDFromURL(listing.URL)
	work := filepath.Join(PoolRoot(storageRoot), "inbox", caseID)
	if err := os.MkdirAll(filepath.Join(work, "raw"), 0o755); err != nil {
		return Manifest{}, err
	}
	var downloaded []PoolImage
	add := func(items []ExtractImage, role string) error {
		for i, item := range items {
			if len(downloaded) >= maxImagesPerListing {
				return nil
			}
			item.Role = role
			if item.Index == 0 && i > 0 {
				item.Index = i
			}
			if IsUGCImage(item.Alt, item.Label) {
				continue
			}
			pool, err := downloadOne(ctx, client, listing.URL, work, role, i, item)
			if err != nil {
				continue
			}
			downloaded = append(downloaded, pool)
		}
		return nil
	}
	if err := add(listing.Gallery, "gallery"); err != nil {
		return Manifest{}, err
	}
	if err := add(listing.SKUImages, "sku"); err != nil {
		return Manifest{}, err
	}
	if err := add(listing.DetailImgs, "detail"); err != nil {
		return Manifest{}, err
	}
	m, err := Admit(listing, downloaded)
	if err != nil {
		_ = os.WriteFile(filepath.Join(work, "reject.txt"), []byte(err.Error()), 0o644)
		return m, err
	}
	if err := materializeAdmitted(storageRoot, work, &m); err != nil {
		return Manifest{}, err
	}
	if err := WriteAdmittedCase(storageRoot, m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func downloadOne(ctx context.Context, client *http.Client, pageURL, work, role string, index int, item ExtractImage) (PoolImage, error) {
	url := UpgradeCDNURL(item.URL)
	if url == "" {
		return PoolImage{}, fmt.Errorf("empty url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return PoolImage{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 ProductFlow-image-eval")
	if pageURL != "" {
		req.Header.Set("Referer", pageURL)
	}
	resp, err := client.Do(req)
	if err != nil {
		return PoolImage{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return PoolImage{}, fmt.Errorf("http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return PoolImage{}, err
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return PoolImage{}, err
	}
	ext := "." + format
	if ext == "." {
		ext = ".bin"
	}
	rel := filepath.Join("raw", fmt.Sprintf("%s-%02d%s", role, index, ext))
	if err := os.MkdirAll(filepath.Join(work, "raw"), 0o755); err != nil {
		return PoolImage{}, err
	}
	if err := os.WriteFile(filepath.Join(work, rel), body, 0o644); err != nil {
		return PoolImage{}, err
	}
	key, conf := ClassifyImage(item, cfg.Width, cfg.Height)
	return PoolImage{
		Path:       rel,
		SHA256:     FileSHA256(body),
		Width:      cfg.Width,
		Height:     cfg.Height,
		ByteSize:   len(body),
		TypeKey:    key,
		Confidence: conf,
		SourceURL:  url,
		Role:       role,
	}, nil
}

func materializeAdmitted(storageRoot, work string, m *Manifest) error {
	dest := caseDir(storageRoot, m.ID)
	if err := os.MkdirAll(filepath.Join(dest, "references"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dest, "gold"), 0o755); err != nil {
		return err
	}
	copySlot := func(items []PoolImage, folder string) ([]PoolImage, error) {
		out := make([]PoolImage, 0, len(items))
		for i, item := range items {
			src := filepath.Join(work, item.Path)
			ext := filepath.Ext(item.Path)
			rel := filepath.Join(folder, fmt.Sprintf("%s-%02d%s", item.TypeKey, i, ext))
			body, err := os.ReadFile(src)
			if err != nil {
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(dest, rel), body, 0o644); err != nil {
				return nil, err
			}
			item.Path = rel
			out = append(out, item)
		}
		return out, nil
	}
	refs, err := copySlot(m.References, "references")
	if err != nil {
		return err
	}
	gold, err := copySlot(m.Gold, "gold")
	if err != nil {
		return err
	}
	m.References = refs
	m.Gold = gold
	return nil
}

var cdnExt = regexp.MustCompile(`(?i)(\.(?:jpe?g|png|gif|webp|bmp))`)

// UpgradeCDNURL 去掉淘宝缩略后缀，尽量拿大图。
func UpgradeCDNURL(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return ""
	}
	if strings.HasPrefix(u, "//") {
		u = "https:" + u
	}
	if i := strings.Index(u, "?"); i >= 0 {
		u = u[:i]
	}
	u = strings.TrimSuffix(u, "_.webp")
	u = strings.TrimSuffix(u, ".avif")
	loc := cdnExt.FindStringIndex(u)
	if loc == nil {
		return u
	}
	return u[:loc[1]]
}
