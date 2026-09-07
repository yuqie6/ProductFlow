package graph

import (
	"strings"

	"github.com/yuqie6/productflow/internal/subjectextract"
)

// ApplySubjectPreserveExtract 对参考图跑主体提取并写回 produce_route（IQ-CF-04 B0）。
// 仅 subject_preserve 路线执行；失败强制 route_qualified=false。不接入采用 HTTP。
func ApplySubjectPreserveExtract(
	refPNG []byte,
	produceRoute map[string]any,
) (map[string]any, subjectextract.Result, error) {
	if produceRoute == nil {
		produceRoute = map[string]any{}
	}
	route, _ := produceRoute["route"].(string)
	if route == "" {
		route = ProduceRouteSubjectPreserve
		produceRoute = cloneMap(produceRoute)
		produceRoute["route"] = route
	}
	if NormalizeProduceRoute(route) != ProduceRouteSubjectPreserve {
		return produceRoute, subjectextract.Result{}, nil
	}
	result := subjectextract.ExtractPNG(refPNG)
	return subjectextract.ApplySubjectPreserveGate(produceRoute, result), result, nil
}

// gateImageProduceRouteWithSubjectExtract 在 image_generation 持久化前对 subject_preserve
// 自动 Apply 主体提取；generative 原样返回。参考图取入边 product_identity 字节。
func gateImageProduceRouteWithSubjectExtract(refs []ReferenceImage, produceRoute map[string]any) map[string]any {
	if produceRoute == nil {
		return produceRoute
	}
	route, _ := produceRoute["route"].(string)
	if NormalizeProduceRoute(route) != ProduceRouteSubjectPreserve {
		return produceRoute
	}
	gated, _, _ := ApplySubjectPreserveExtract(subjectPreserveReferencePNG(refs), produceRoute)
	return gated
}

// subjectPreserveReferencePNG 取商品本体参考字节；无则空（提取失败→route_qualified=false）。
func subjectPreserveReferencePNG(refs []ReferenceImage) []byte {
	for _, ref := range refs {
		if strings.TrimSpace(ref.Role) != "product_identity" {
			continue
		}
		if len(ref.Bytes) == 0 {
			continue
		}
		return ref.Bytes
	}
	return nil
}
