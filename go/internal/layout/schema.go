package layout

import (
	"fmt"
	"strings"
)

const SchemaVersion = 1

// Align 水平对齐闭集。
const (
	AlignLeft   = "left"
	AlignCenter = "center"
	AlignRight  = "right"
)

// VAlign 垂直对齐闭集。
const (
	VAlignTop    = "top"
	VAlignMiddle = "middle"
	VAlignBottom = "bottom"
)

// LayerType 图层类型闭集。
const (
	LayerImage = "image"
	LayerText  = "text"
	LayerShape = "shape"
)

// ShapeKind 形状闭集。
const (
	ShapeRect = "rect"
)

// Insets 是安全区边距（像素，画布内侧）。
type Insets struct {
	Top    int `json:"top"`
	Right  int `json:"right"`
	Bottom int `json:"bottom"`
	Left   int `json:"left"`
}

// Document 是受控排版的唯一结构输入（浏览器预览与服务端导出同形）。
type Document struct {
	SchemaVersion  int     `json:"schema_version"`
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	Background     string  `json:"background,omitempty"` // #RRGGBB 或空=透明
	SafeArea       Insets  `json:"safe_area"`
	SubjectAssetID string  `json:"subject_asset_id"` // 主体位图资产；改字号不得改其内容
	Layers         []Layer `json:"layers"`
}

// Layer 是一层；Type 决定使用哪些字段。
type Layer struct {
	ID     string `json:"id"`
	Type   string `json:"type"` // image|text|shape
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	ZIndex int    `json:"z_index"`

	// image
	AssetID string `json:"asset_id,omitempty"`

	// text
	Text      string `json:"text,omitempty"`
	FontID    string `json:"font_id,omitempty"`
	FontSize  float64 `json:"font_size,omitempty"`
	Color     string `json:"color,omitempty"` // #RRGGBB
	Align     string `json:"align,omitempty"`
	VAlign    string `json:"valign,omitempty"`

	// shape
	Shape string `json:"shape,omitempty"` // rect
	Fill  string `json:"fill,omitempty"`
}

// ValidateDocument 校验结构闭集与几何；不渲染。
func ValidateDocument(doc Document) error {
	if doc.SchemaVersion != SchemaVersion {
		return fmt.Errorf("layout schema_version 须为 %d", SchemaVersion)
	}
	if doc.Width <= 0 || doc.Height <= 0 {
		return fmt.Errorf("画布宽高须为正")
	}
	if doc.Width > 8192 || doc.Height > 8192 {
		return fmt.Errorf("画布边长不得超过 8192")
	}
	if strings.TrimSpace(doc.SubjectAssetID) == "" {
		return fmt.Errorf("subject_asset_id 必填")
	}
	sa := doc.SafeArea
	if sa.Top < 0 || sa.Right < 0 || sa.Bottom < 0 || sa.Left < 0 {
		return fmt.Errorf("安全区不得为负")
	}
	if sa.Left+sa.Right >= doc.Width || sa.Top+sa.Bottom >= doc.Height {
		return fmt.Errorf("安全区过大")
	}
	if len(doc.Layers) == 0 {
		return fmt.Errorf("至少一层")
	}
	ids := map[string]struct{}{}
	subjectSeen := false
	for i, layer := range doc.Layers {
		if strings.TrimSpace(layer.ID) == "" {
			return fmt.Errorf("layers[%d].id 必填", i)
		}
		if _, ok := ids[layer.ID]; ok {
			return fmt.Errorf("重复 layer id %q", layer.ID)
		}
		ids[layer.ID] = struct{}{}
		if layer.Width <= 0 || layer.Height <= 0 {
			return fmt.Errorf("layer %q 宽高须为正", layer.ID)
		}
		switch layer.Type {
		case LayerImage:
			if strings.TrimSpace(layer.AssetID) == "" {
				return fmt.Errorf("image 层 %q 须有 asset_id", layer.ID)
			}
			if layer.AssetID == doc.SubjectAssetID {
				subjectSeen = true
			}
		case LayerText:
			if strings.TrimSpace(layer.FontID) == "" {
				return fmt.Errorf("text 层 %q 须有 font_id", layer.ID)
			}
			if layer.FontSize <= 0 {
				return fmt.Errorf("text 层 %q font_size 须为正", layer.ID)
			}
			if err := normalizeAlign(layer.Align); err != nil {
				return fmt.Errorf("text 层 %q: %w", layer.ID, err)
			}
			if err := normalizeVAlign(layer.VAlign); err != nil {
				return fmt.Errorf("text 层 %q: %w", layer.ID, err)
			}
		case LayerShape:
			if layer.Shape != ShapeRect {
				return fmt.Errorf("shape 层 %q 仅支持 rect", layer.ID)
			}
		default:
			return fmt.Errorf("layer %q 类型非法: %q", layer.ID, layer.Type)
		}
	}
	if !subjectSeen {
		return fmt.Errorf("layers 须包含 subject_asset_id=%q 的 image 层", doc.SubjectAssetID)
	}
	return nil
}

func normalizeAlign(raw string) error {
	switch strings.TrimSpace(raw) {
	case "", AlignLeft, AlignCenter, AlignRight:
		return nil
	default:
		return fmt.Errorf("align 非法: %q", raw)
	}
}

func normalizeVAlign(raw string) error {
	switch strings.TrimSpace(raw) {
	case "", VAlignTop, VAlignMiddle, VAlignBottom:
		return nil
	default:
		return fmt.Errorf("valign 非法: %q", raw)
	}
}

func resolveAlign(raw string) string {
	switch strings.TrimSpace(raw) {
	case AlignCenter, AlignRight:
		return strings.TrimSpace(raw)
	default:
		return AlignLeft
	}
}

func resolveVAlign(raw string) string {
	switch strings.TrimSpace(raw) {
	case VAlignMiddle, VAlignBottom:
		return strings.TrimSpace(raw)
	default:
		return VAlignTop
	}
}
