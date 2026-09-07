package layout

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// FontCatalog 按 font_id 提供已解析字体字节（调用方注入；本包不下载字体）。
type FontCatalog map[string][]byte

// Assets 按 asset_id 提供位图像素（通常为 PNG/JPEG/WebP 原字节）。
type Assets map[string][]byte

// Lineage 把排版导出接到现有 ProductImageAsset 链：结果 parent → 主体。
type Lineage struct {
	SubjectAssetID       string `json:"subject_asset_id"`
	SubjectContentSHA256 string `json:"subject_content_sha256"`
	ParentAssetID        string `json:"parent_asset_id"` // 等于主体，供 InsertAssetIdentity
	DocumentHash         string `json:"document_hash"`
	Kind                 string `json:"kind"` // layout_compose
}

// Result 是一次 Compose 的导出与合格判据。
type Result struct {
	PNG             []byte     `json:"-"`
	PNGSHA256       string     `json:"png_sha256"`
	Plan            LayoutPlan `json:"plan"`
	Lineage         Lineage    `json:"lineage"`
	LayoutQualified bool       `json:"layout_qualified"`
	UnresolvedItems []string   `json:"unresolved_items,omitempty"`
	MissingGlyphs   []string   `json:"missing_glyphs,omitempty"`
}

// LayoutPlan 是预览/导出对齐用的几何计划（权威由 Go Compose 写出）。
type LayoutPlan struct {
	SchemaVersion int           `json:"schema_version"`
	Width         int           `json:"width"`
	Height        int           `json:"height"`
	SafeArea      Insets        `json:"safe_area"`
	Layers        []PlacedLayer `json:"layers"`
}

// PlacedLayer 是一层放置结果。
type PlacedLayer struct {
	ID           string     `json:"id"`
	Type         string     `json:"type"`
	X            int        `json:"x"`
	Y            int        `json:"y"`
	Width        int        `json:"width"`
	Height       int        `json:"height"`
	ZIndex       int        `json:"z_index"`
	AssetID      string     `json:"asset_id,omitempty"`
	Text         string     `json:"text,omitempty"`
	FontID       string     `json:"font_id,omitempty"`
	FontSize     float64    `json:"font_size,omitempty"`
	Color        string     `json:"color,omitempty"`
	Align        string     `json:"align,omitempty"`
	VAlign       string     `json:"valign,omitempty"`
	BaselineY    int        `json:"baseline_y,omitempty"`
	Lines        []PlanLine `json:"lines,omitempty"`
	MissingRunes []string   `json:"missing_runes,omitempty"`
	OutsideSafe  bool       `json:"outside_safe,omitempty"`
}

// PlanLine 是一行文字的放置。
type PlanLine struct {
	Text  string `json:"text"`
	X     int    `json:"x"`
	Y     int    `json:"y"` // baseline
	Width int    `json:"width"`
}

// ContentSHA256 对字节做 SHA-256。
func ContentSHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// BuildLineage 在主体字节已知时组装 lineage；不写库。
func BuildLineage(doc Document, subjectBytes []byte) (Lineage, error) {
	hash, err := DocumentHash(doc)
	if err != nil {
		return Lineage{}, err
	}
	return Lineage{
		SubjectAssetID:       doc.SubjectAssetID,
		SubjectContentSHA256: ContentSHA256(subjectBytes),
		ParentAssetID:        doc.SubjectAssetID,
		DocumentHash:         hash,
		Kind:                 "layout_compose",
	}, nil
}

func sortedLayers(layers []Layer) []Layer {
	out := append([]Layer(nil), layers...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ZIndex != out[j].ZIndex {
			return out[i].ZIndex < out[j].ZIndex
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func outsideSafe(doc Document, x, y, w, h int) bool {
	sa := doc.SafeArea
	left, top := sa.Left, sa.Top
	right, bottom := doc.Width-sa.Right, doc.Height-sa.Bottom
	return x < left || y < top || x+w > right || y+h > bottom
}

func collectUnresolved(plan LayoutPlan, missing []string) (qualified bool, items []string, glyphs []string) {
	seen := map[string]struct{}{}
	for _, g := range missing {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		if _, ok := seen[g]; ok {
			continue
		}
		seen[g] = struct{}{}
		glyphs = append(glyphs, g)
		items = append(items, fmt.Sprintf("missing_glyph:%s", g))
	}
	sort.Strings(glyphs)
	for _, layer := range plan.Layers {
		if layer.OutsideSafe {
			items = append(items, fmt.Sprintf("outside_safe:%s", layer.ID))
		}
	}
	sort.Strings(items)
	return len(items) == 0, items, glyphs
}
