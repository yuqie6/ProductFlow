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

// ImageProvider 是 Executor 跑 image_generation 时的供应商缝，实现放在 providers 包。
// GenerateImage 无法证明成功时调用方标 unknown，不得把空 Bytes 当 failed 自动重试。
// 为 nil 时 Executor 回落 MockImageProvider。不要和 PromptProvider（brief/visual/prompt 文稿）搞混。
type ImageProvider interface {
	Name() string
	GenerateImage(ctx context.Context, req ImageRequest) (ImageResult, error)
}

// ReferenceImage 是交给 prompt/image provider 的参考图字节。Role 常用 product_identity。
type ReferenceImage struct {
	AssetID  string
	Role     string // 常用 product_identity；来自入边 role
	Label    string
	MIME     string
	Filename string
	Bytes    []byte // 已读出的像素；空表示未加载
	EdgeID   string
}

// PromptRequest 是内容节点发给 PromptProvider 的组装结果，不是节点 config 原样 dump。
type PromptRequest struct {
	// NodeType 决定走 brief / visual / prompt 哪条组装。
	NodeType  NodeType
	NodeTitle string
	// InputDigest 是本次组装对应的编译 digest，写入产物后用于 skip/stale。
	InputDigest          string
	Facts                []map[string]any // 编译后的商品 facts
	Brief                map[string]any   // 第一条入边 brief
	Briefs               []map[string]any // 全部入边 brief；Brief 取第一条
	Visual               map[string]any   // 完整 visual_system payload；内联 overlay 时可能为空
	Config               map[string]any   // 节点 config 副本，不是出站 user JSON
	References           []ReferenceImage
	ImageTypeKey         string // 图种闭集 key；listing 等节点可空
	ImageTypeTitle       string // 图种展示名，给模型看
	ImageTypeDescription string // 图种职责说明，给模型看
	ImageTypeFamily      string // photography | infographic 等
	ImageTypeJob         string // 图种 job 文案，给模型看
	// GenerateFromContext 为 true 时按 seed 从 facts/brief 起草；仅 seed 文稿为 true。
	GenerateFromContext bool
	CurrentPrompt       map[string]any // seed 或当前 prompt 文档
	// DocumentAction 是 complete|rewrite|replace；空视为 complete。
	DocumentAction   string
	DocumentSection  string
	CurrentDocument  map[string]any   // complete/rewrite 时的当前文稿；seed 起草可空
	VisualExceptions []map[string]any // 内联 overlay 拆出的例外；完整 visual 时为 nil
	TextPolicy       string           // none|required 等，来自下游或 listing
	TextLanguage     string           // text_policy=required 时的文案语言
	ImageTypes       []map[string]any // 图上全部图种摘要，给 listing 用
}

// PromptResult 是 prompt provider 的结构化输出。Payload 为空视为调用失败。
type PromptResult struct {
	Payload    map[string]any // 结构化文稿；空视为调用失败
	Model      string
	ResponseID string
}

// ImageRequest 是 image_generation 发给 ImageProvider 的输入；Prompt 来自入边 live 文档。
type ImageRequest struct {
	NodeTitle string
	// InputDigest 是本次生图对应的编译 digest，写入产物后用于 skip/stale。
	InputDigest          string
	ImageTypeKey         string         // 图种闭集 key，写入生成图身份
	GenerationSpec       map[string]any // 节点 generation_spec；不进 image digest 的交付项不在这里
	Prompt               map[string]any // 来自入边 live prompt 文档，不是节点 config dump
	References           []ReferenceImage
	VisualSystem         map[string]any // 完整 visual_system 版本；与 VisualOverlay 互斥使用
	VisualOverlay        map[string]any // 内联 overlay；有完整 VisualSystem 时为空
	VariationInstruction string         // 同组多张图的差异说明；单张可空
	IncomingEdgeIDs      []string       // 实际用到的入边 id
	PromptArtifactID     string
}

// ImageResult 是生图结果。无法证明成功时调用方标 unknown，不把 Bytes 当失败重试。
type ImageResult struct {
	Bytes      []byte // 无法证明成功时不要把空 Bytes 当 failed
	MIME       string
	Model      string
	ResponseID string
	// ProviderStatus 无法证明成功时调用方标 unknown，不把空 Bytes 当 failed 自动重试。
	ProviderStatus      string
	Width               int
	Height              int
	EffectiveParameters map[string]any // 供应商实际生效参数；可空
}

// GeneratedImageInput 把生成图写成 ProductImageAsset；实现放在 product 包。
type GeneratedImageInput struct {
	ProductID    string
	Title        string
	Filename     string
	Bytes        []byte // 已生成像素，尚未写成 ProductImageAsset
	MIME         string
	ImageTypeKey *string // 图种闭集；nil 表示未分类
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

// Dependencies 是 Executor 的供应商缝。Prompt/Image 为 nil 时用 Mock。
type Dependencies struct {
	Prompt   PromptProvider       // nil 时 Executor 回落 MockPromptProvider
	Image    ImageProvider        // nil 时 Executor 回落 MockImageProvider
	Assets   GeneratedImageWriter // 生成图写成 ProductImageAsset；Write 失败则 cook 失败
	Delivery DeliveryQueuer       // 图片节点成功后排队交付派生；失败只记日志，不失败 cook
}

// MockPromptProvider 用于测试与未配置真实供应商时的可注入默认。
type MockPromptProvider struct {
	ProviderName string         // Name() 返回值；空则 "mock"
	Brief        map[string]any // GenerateCreativeBrief 的 payload；nil 用内置默认
	Overlay      map[string]any // GenerateVisualOverlay 的 payload；nil 用内置默认
	Prompt       map[string]any // GeneratePrompt 的 payload；nil 用内置默认
	SourceNote   map[string]any // GenerateSourceNote 的 payload；nil 用内置默认
	Err          error          // 非空时 Generate* 直接返回该错误
}

// Name 实现 PromptProvider：Executor 把返回值写入 workflow_graph_provider_effects.provider_name；空则 "mock"。
// 无副作用、不打网。测试可注入 ProviderName。不要改默认值而不改断言 "mock" 的测试。
func (m MockPromptProvider) Name() string {
	if m.ProviderName != "" {
		return m.ProviderName
	}
	return "mock"
}

// GenerateCreativeBrief 返回 Brief 或内置默认 payload。Err 非空时直接失败。
func (m MockPromptProvider) GenerateCreativeBrief(ctx context.Context, req PromptRequest) (PromptResult, error) {
	if m.Err != nil {
		return PromptResult{}, m.Err
	}
	payload := m.Brief
	if payload == nil {
		payload = map[string]any{
			"goal":              "清晰展示商品",
			"key_messages":      []any{"突出主体"},
			"required_elements": []any{},
			"prohibitions":      []any{},
			"fact_gaps":         []any{},
		}
	}
	return PromptResult{Payload: payload, Model: "mock-brief"}, nil
}

// GenerateVisualOverlay 返回 Overlay 或内置默认 visual_overlay。
// Err 非空时直接返回该错误。
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
		}
	}
	return PromptResult{Payload: payload, Model: "mock-visual"}, nil
}

// GeneratePrompt 返回 Prompt 或内置 listing 骨架。
// Err 非空时直接返回该错误。
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

// GenerateSourceNote 供创建页看图起草，不走画布 cook。
// Err 非空时直接返回该错误。
func (m MockPromptProvider) GenerateSourceNote(ctx context.Context, req PromptRequest) (PromptResult, error) {
	if m.Err != nil {
		return PromptResult{}, m.Err
	}
	payload := m.SourceNote
	if payload == nil {
		payload = map[string]any{
			"visible": "厚壁玻璃密封瓶，球盖锁扣。瓶身通透能看见内容，厚壁更耐磕。",
			"fields": []any{
				map[string]any{"label": "材质", "value": "玻璃"},
				map[string]any{"label": "容量", "value": ""},
				map[string]any{"label": "价格", "value": ""},
			},
		}
	}
	return PromptResult{Payload: payload, Model: "mock-source-note"}, nil
}

// MockImageProvider 返回一张最小 PNG，供图运行测试走完整产物路径。
type MockImageProvider struct {
	ProviderName string // Name() 返回值；空则 "mock"
	PNG          []byte // 空则用 1×1 PNG
	Err          error  // 非空时 GenerateImage 直接返回该错误
}

// Name 实现 ImageProvider：Executor 把返回值写入 workflow_graph_provider_effects.provider_name；空则 "mock"。
// 无副作用、不打网。测试可注入 ProviderName。不要改默认值而不改断言 "mock" 的测试。
func (m MockImageProvider) Name() string {
	if m.ProviderName != "" {
		return m.ProviderName
	}
	return "mock"
}

// GenerateImage 返回 1×1 PNG。ProviderStatus 为 completed。
// Err 非空时直接返回该错误。
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
