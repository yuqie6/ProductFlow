package imageeval

import "strings"

// ClassifyImage 给一张抽出图标图种。suggested_type 优先；否则按角色和画幅启发式。
// 置信不足的图种仍返回，由准入看整套是否完整。
func ClassifyImage(img ExtractImage, width, height int) (typeKey string, confidence float64) {
	if key := strings.TrimSpace(img.SuggestedType); IsGeneratingType(key) {
		return key, 0.95
	}
	aspect := 0.0
	if width > 0 {
		aspect = float64(height) / float64(width)
	}
	switch img.Role {
	case "sku":
		return "sku", 0.9
	case "gallery":
		if img.Index == 0 {
			return "hero", 0.85
		}
		if img.Index == 1 {
			return "scene", 0.55
		}
		return "detail", 0.5
	case "detail":
		if aspect >= 1.7 {
			if looksLikeSpec(img) {
				return "specifications", 0.6
			}
			return "selling_point", 0.65
		}
		if looksLikeSpec(img) {
			return "specifications", 0.55
		}
		if img.Index == 0 {
			return "selling_point", 0.55
		}
		return "selling_point", 0.5
	default:
		if img.Index == 0 {
			return "hero", 0.4
		}
		return "detail", 0.35
	}
}

func looksLikeSpec(img ExtractImage) bool {
	blob := strings.ToLower(img.Alt + img.Label)
	for _, needle := range []string{"规格", "参数", "尺寸", "spec", "size chart", "成分"} {
		if strings.Contains(blob, needle) {
			return true
		}
	}
	return false
}
