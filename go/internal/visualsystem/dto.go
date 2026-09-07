package visualsystem

import (
	"strings"
	"time"
)

// Brand 层占位状态：表已建；未选定、已选定但无可解析风格、或已合并。
const (
	BrandStatusUnavailable     = "unavailable"
	BrandStatusAvailable       = "available"
	BrandReasonNotSelected     = "brand_not_selected"
	BrandReasonExistsNoMerge   = "brand_exists_no_style_merge"
	BrandReasonMerged          = "brand_style_merged"
	// BrandReasonNotReady 保留别名，指向 brand_not_selected（表已存在，不再表示「表缺失」）。
	BrandReasonNotReady = BrandReasonNotSelected
)

// Layer 是 IQ-CF-07 继承优先级层。
const (
	LayerProductOverride = "product_override"
	LayerSelectedVisual  = "selected_visual_system_version"
	LayerBrandVersion    = "brand_version"
	LayerProductDefault  = "product_default"
)

// BrandPlaceholder 声明品牌层贡献状态；未合并时 status=unavailable。
type BrandPlaceholder struct {
	Status string `json:"status"` // unavailable | available
	Reason string `json:"reason"` // brand_not_selected | brand_exists_no_style_merge | brand_style_merged
	Detail string `json:"detail"` // 用户可读说明
}

// DefaultBrandPlaceholder 表示继承输入未携带品牌选定（诚实「未选定」）。
func DefaultBrandPlaceholder() BrandPlaceholder {
	return BrandPlaceholder{
		Status: BrandStatusUnavailable,
		Reason: BrandReasonNotSelected,
		Detail: "未选定品牌；继承链跳过品牌层",
	}
}

// BrandExistsPlaceholder 表示品牌已选定，但无可解析的品牌风格载荷（无 visual_system 或无 style/colors）。
func BrandExistsPlaceholder() BrandPlaceholder {
	return BrandPlaceholder{
		Status: BrandStatusUnavailable,
		Reason: BrandReasonExistsNoMerge,
		Detail: "品牌已选定，但无可解析的品牌风格载荷；继承链跳过本层",
	}
}

// MergedBrandPlaceholder 表示品牌层 style/colors 已并入 EffectivePayload。
func MergedBrandPlaceholder() BrandPlaceholder {
	return BrandPlaceholder{
		Status: BrandStatusAvailable,
		Reason: BrandReasonMerged,
		Detail: "已合并品牌挂接视觉方案当前版本的风格色",
	}
}

// SystemView 是商家内视觉方案主档投影。
type SystemView struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	ArchivedAt     *time.Time     `json:"archived_at"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	CurrentVersion *VersionView   `json:"current_version,omitempty"`
	Versions       []VersionView  `json:"versions,omitempty"`
}

// VersionView 是不可变视觉方案版本投影。
type VersionView struct {
	ID            string         `json:"id"`
	SystemID      string         `json:"visual_system_id"`
	Version       int            `json:"version"`
	SchemaVersion int            `json:"schema_version"`
	Payload       map[string]any `json:"payload"`
	PayloadHash   string         `json:"payload_hash"`
	CreatedAt     time.Time      `json:"created_at"`
}

// SelectionView 是商品当前选定的视觉方案版本。
type SelectionView struct {
	ProductID             string      `json:"product_id"`
	VisualSystemVersionID string      `json:"visual_system_version_id"`
	SelectedAt            time.Time   `json:"selected_at"`
	Version               VersionView `json:"version"`
	System                SystemView  `json:"system"`
}

// LayerContribution 描述某一继承层是否提供了有效载荷。
type LayerContribution struct {
	Layer       string         `json:"layer"`
	Active      bool           `json:"active"`
	VersionID   *string        `json:"version_id,omitempty"`
	SystemID    *string        `json:"system_id,omitempty"`
	SystemName  *string        `json:"system_name,omitempty"`
	Payload     map[string]any `json:"payload,omitempty"`
	Placeholder *BrandPlaceholder `json:"placeholder,omitempty"`
	Note        string         `json:"note"`
}

// InheritanceView 是商品视觉继承解析结果；优先级对本商品覆盖 > 选定方案版本 > 品牌层 > 产品默认。
// 品牌层：未选定 → brand_not_selected；已选定但无可解析风格 → brand_exists_no_style_merge；可解析则 Active + brand_style_merged。
type InheritanceView struct {
	ProductID             string              `json:"product_id"`
	SelectedVersionID     *string             `json:"selected_visual_system_version_id"`
	EffectivePayload      map[string]any      `json:"effective_payload"`
	Layers                []LayerContribution `json:"layers"`
	BrandPlaceholder      BrandPlaceholder    `json:"brand_placeholder"`
	NewerVersionAvailable *VersionView        `json:"newer_version_available,omitempty"`
}

// ImpactProduct 是仍钉在旧版本、未显式采用新版本的商品。
type ImpactProduct struct {
	ProductID             string    `json:"product_id"`
	ProductName           string    `json:"product_name"`
	VisualSystemVersionID string    `json:"visual_system_version_id"`
	SelectedAt            time.Time `json:"selected_at"`
}

// ImpactView 列出追加新版本后仍钉旧版的商品；旧交付快照不在此列表（本身不可变）。
type ImpactView struct {
	SystemID          string          `json:"visual_system_id"`
	LatestVersionID   string          `json:"latest_version_id"`
	LatestVersion     int             `json:"latest_version"`
	PinnedToOlder     []ImpactProduct `json:"pinned_to_older"`
	BrandPlaceholder  BrandPlaceholder `json:"brand_placeholder"`
}

// ReuseItem 是第二商品预览中的继承或待填项。
type ReuseItem struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Source string `json:"source"` // inherited | pending | placeholder
	Detail string `json:"detail"`
}

// ReusePreview 供配方创建预览列出继承与待填项（消费 IQ-CF-07）。
type ReusePreview struct {
	Inherited                    []ReuseItem      `json:"inherited"`
	Pending                      []ReuseItem      `json:"pending"`
	BrandPlaceholder             BrandPlaceholder `json:"brand_placeholder"`
	PreferredVisualSystemVersionID *string        `json:"preferred_visual_system_version_id"`
	InheritancePriority          []string         `json:"inheritance_priority"`
}

// BuildReusePreview 根据配方偏好与图结构生成第二商品预览分区。
func BuildReusePreview(preferredVersionID *string, hasVisualNode bool, requiredBindings []string) ReusePreview {
	priority := []string{LayerProductOverride, LayerSelectedVisual, LayerBrandVersion, LayerProductDefault}
	inherited := make([]ReuseItem, 0, 4)
	pending := make([]ReuseItem, 0, 6)
	brand := DefaultBrandPlaceholder()

	inherited = append(inherited, ReuseItem{
		Key: "workflow_structure", Label: "图结构与镜头布局", Source: "inherited",
		Detail: "配方节点、连线与分组模板；不含来源商品身份与事实",
	})
	if hasVisualNode {
		inherited = append(inherited, ReuseItem{
			Key: "visual_node_template", Label: "视觉规范节点模板", Source: "inherited",
			Detail: "清身份后的系列风格节点配置骨架",
		})
	}
	if preferredVersionID != nil && strings.TrimSpace(*preferredVersionID) != "" {
		id := strings.TrimSpace(*preferredVersionID)
		inherited = append(inherited, ReuseItem{
			Key: "preferred_visual_version", Label: "选定视觉方案版本", Source: "inherited",
			Detail: "将绑定版本 " + id + "；需在目标商品显式确认后写入选择",
		})
	} else {
		pending = append(pending, ReuseItem{
			Key: "visual_version", Label: "视觉方案版本", Source: "pending",
			Detail: "配方未绑定偏好版本；可在工作台保存或选择后再采用",
		})
	}
	pending = append(pending, ReuseItem{
		Key: "product_facts", Label: "商品事实与规格", Source: "pending",
		Detail: "必须使用目标商品新输入；不参与风格继承链",
	})
	pending = append(pending, ReuseItem{
		Key: "product_identity", Label: "身份参考图", Source: "pending",
		Detail: "须重新绑定目标商品身份素材",
	})
	for _, binding := range requiredBindings {
		if binding == "product_identity" {
			continue
		}
		pending = append(pending, ReuseItem{
			Key: binding, Label: binding, Source: "pending",
			Detail: "目标图须满足的绑定",
		})
	}
	pending = append(pending, ReuseItem{
		Key: "brand_version", Label: "品牌版本", Source: "placeholder",
		Detail: brand.Detail,
	})
	return ReusePreview{
		Inherited:                      inherited,
		Pending:                        pending,
		BrandPlaceholder:               brand,
		PreferredVisualSystemVersionID: preferredVersionID,
		InheritancePriority:            priority,
	}
}
