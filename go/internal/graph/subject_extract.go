package graph

import (
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
