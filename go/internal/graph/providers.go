package graph

import (
	"context"

	"gorm.io/gorm"
)

// PromptProvider 是内容节点（brief / visual / prompt）的供应商缝。
type PromptProvider interface {
	Name() string
	GenerateCreativeBrief(ctx context.Context, req PromptRequest) (PromptResult, error)
	GenerateVisualOverlay(ctx context.Context, req PromptRequest) (PromptResult, error)
	GeneratePrompt(ctx context.Context, req PromptRequest) (PromptResult, error)
}

// ImageProvider 是图片生成节点的供应商缝。
type ImageProvider interface {
	Name() string
	GenerateImage(ctx context.Context, req ImageRequest) (ImageResult, error)
}

type ReferenceImage struct {
	AssetID  string
	Role     string
	Label    string
	MIME     string
	Filename string
	Bytes    []byte
	EdgeID   string
}

type PromptRequest struct {
	NodeType             NodeType
	NodeTitle            string
	InputDigest          string
	Facts                []map[string]any
	Brief                map[string]any
	Briefs               []map[string]any
	Visual               map[string]any
	Config               map[string]any
	References           []ReferenceImage
	ImageTypeKey         string
	ImageTypeTitle       string
	ImageTypeDescription string
	ImageTypeFamily      string
	ImageTypeJob         string
	GenerateFromContext  bool
	CurrentPrompt        map[string]any
	VisualExceptions     []map[string]any
	TextPolicy           string
	TextLanguage         string
	ImageTypes           []map[string]any
}

type PromptResult struct {
	Payload    map[string]any
	Model      string
	ResponseID string
}

type ImageRequest struct {
	NodeTitle            string
	InputDigest          string
	ImageTypeKey         string
	GenerationSpec       map[string]any
	Prompt               map[string]any
	References           []ReferenceImage
	VisualSystem         map[string]any
	VisualOverlay        map[string]any
	VariationInstruction string
	IncomingEdgeIDs      []string
	PromptArtifactID     string
}

type ImageResult struct {
	Bytes               []byte
	MIME                string
	Model               string
	ResponseID          string
	ProviderStatus      string
	Width               int
	Height              int
	EffectiveParameters map[string]any
}

type GeneratedImageInput struct {
	ProductID    string
	Title        string
	Filename     string
	Bytes        []byte
	MIME         string
	ImageTypeKey *string
}

// GeneratedImageWriter 把生成图写成商品图片身份，并按商品收口读回参考图字节。实现放在 product，避免 graph import product。
type GeneratedImageWriter interface {
	Write(ctx context.Context, tx *gorm.DB, in GeneratedImageInput) (assetID string, err error)
	ReadAssetBytes(ctx context.Context, tx *gorm.DB, productID, assetID string) (data []byte, mime, filename string, err error)
}

// DeliveryQueuer 在图片节点成功后排队确定性交付派生。实现放在 delivery，避免 graph import delivery。
type DeliveryQueuer interface {
	QueueAfterImageSuccess(ctx context.Context, tx *gorm.DB, nodeID, sourceAssetID string) error
}

type Dependencies struct {
	Prompt   PromptProvider
	Image    ImageProvider
	Assets   GeneratedImageWriter
	Delivery DeliveryQueuer
}

// MockPromptProvider 用于测试与未配置真实供应商时的可注入默认。
type MockPromptProvider struct {
	ProviderName string
	Brief        map[string]any
	Overlay      map[string]any
	Prompt       map[string]any
	Err          error
}

func (m MockPromptProvider) Name() string {
	if m.ProviderName != "" {
		return m.ProviderName
	}
	return "mock"
}

func (m MockPromptProvider) GenerateCreativeBrief(ctx context.Context, req PromptRequest) (PromptResult, error) {
	if m.Err != nil {
		return PromptResult{}, m.Err
	}
	payload := m.Brief
	if payload == nil {
		payload = map[string]any{
			"goal":          "清晰展示商品",
			"design_goals":  []any{"突出主体"},
			"required_copy": []any{},
			"prohibitions":  []any{},
		}
	}
	return PromptResult{Payload: payload, Model: "mock-brief"}, nil
}

func (m MockPromptProvider) GenerateVisualOverlay(ctx context.Context, req PromptRequest) (PromptResult, error) {
	if m.Err != nil {
		return PromptResult{}, m.Err
	}
	payload := m.Overlay
	if payload == nil {
		payload = map[string]any{
			"style": []any{"商业套图", "商品是主角", "层次清楚"},
			"colors": []any{
				map[string]any{"role": "background", "value": "#F3EFE8", "label": "暖白底"},
				map[string]any{"role": "headline", "value": "#1C1917", "label": "标题色"},
				map[string]any{"role": "accent", "value": "#6B7C6A", "label": "克制点缀"},
			},
			"prohibitions": []any{"不要改变商品结构、颜色或材质"},
		}
	}
	return PromptResult{Payload: payload, Model: "mock-visual"}, nil
}

func (m MockPromptProvider) GeneratePrompt(ctx context.Context, req PromptRequest) (PromptResult, error) {
	if m.Err != nil {
		return PromptResult{}, m.Err
	}
	payload := m.Prompt
	if payload == nil {
		payload = map[string]any{
			"schema_version": 1,
			"shared_rules":   []any{"商品外形以参考图为准"},
			"composition":    map[string]any{"layout": "商品居中", "product_share_percent": 70, "copy_regions": []any{}},
			"content":        map[string]any{"focus": []any{req.NodeTitle}, "background": "干净背景"},
			"text":           map[string]any{},
			"atmosphere":     map[string]any{"keywords": []any{"清晰"}, "lighting": "均匀照明"},
		}
	}
	return PromptResult{Payload: payload, Model: "mock-prompt"}, nil
}

// MockImageProvider 返回一张最小 PNG，供图运行测试走完整产物路径。
type MockImageProvider struct {
	ProviderName string
	PNG          []byte
	Err          error
}

func (m MockImageProvider) Name() string {
	if m.ProviderName != "" {
		return m.ProviderName
	}
	return "mock"
}

func (m MockImageProvider) GenerateImage(ctx context.Context, req ImageRequest) (ImageResult, error) {
	if m.Err != nil {
		return ImageResult{}, m.Err
	}
	png := m.PNG
	if len(png) == 0 {
		png = smallestPNG()
	}
	return ImageResult{Bytes: png, MIME: "image/png", Model: "mock-image", ProviderStatus: "completed"}, nil
}

func smallestPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}
}
