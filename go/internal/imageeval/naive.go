package imageeval

import "fmt"

// naivePrompts 是直调臂冻结的一句图种提示词。禁止临场改写。
var naivePrompts = map[string]string{
	"hero":           "根据参考商品图，生成一张电商封面主图。商品约占画面七成，棚拍或使用瞬间，保留外形、结构和材质。",
	"selling_point":  "根据参考商品图，生成一张电商核心卖点详情页。一页一个钩子，大标题加商品，保留真实外形。",
	"scene":          "根据参考商品图，生成一张使用场景图。商品在真实环境里，保留外形和材质。",
	"detail":         "根据参考商品图，生成一张材质工艺特写。只拍一处结构或纹理，侧光，保留真实材质。",
	"sku":            "根据参考商品图，生成一张白底规格对照图。商品摆正、边缘干净，保留外形和颜色。",
	"specifications": "根据参考商品图和商品资料，生成一张规格参数信息图。只写已有参数，模块对齐。",
	"dimensions":     "根据参考商品图，生成一张尺寸标注信息图。商品完整，尺寸线贴边，不要编造数字。",
	"packaging":      "根据参考商品图，生成一张包装展示图。看清盒型和内容物，商品可辨认。",
}

// NaivePrompt 返回直调臂冻结提示词。未知图种返回 error。
func NaivePrompt(imageType string) (string, error) {
	text, ok := naivePrompts[imageType]
	if !ok {
		return "", fmt.Errorf("no naive prompt for image type %q", imageType)
	}
	return text, nil
}

// TypeJobSummary 给评委看的图种职责，摘自 listing 合同，不是直调提示词。
func TypeJobSummary(imageType string) string {
	switch imageType {
	case "hero":
		return "封面主图：商品约占 70%，棚拍或使用瞬间，缩略图能认；不要卖点引导线页。"
	case "selling_point":
		return "核心卖点图：详情转化页，一页一个钩子，标题可读，商品仍是主体。"
	case "scene":
		return "场景展示图：真实使用环境，商品对焦清楚，不要和首图同一台面。"
	case "detail":
		return "细节图：一处材质或结构占满画面，不是首图中心裁切。"
	case "sku":
		return "SKU 图：白底或纯色，同一机位，方便选款。"
	case "specifications":
		return "规格参数信息图：键值对照，只呈现资料里已有的参数。"
	case "dimensions":
		return "尺寸图：尺寸线贴边，数字只用来自资料的。"
	case "packaging":
		return "包装图：开盒或内容物铺开，不要再拍一张主体冒充包装。"
	default:
		return imageType
	}
}
