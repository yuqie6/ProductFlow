package graph

import (
	"strings"

	"github.com/yuqie6/productflow/internal/subjectextract"
)

// ApplySubjectPreserveExtract 对参考图跑主体提取+B1 合成并写回 produce_route（IQ-CF-04）。
// 仅 subject_preserve 路线执行；提取或合成失败强制 route_qualified=false。不接入采用 HTTP。
// 成功时 ComposeResult 含可交付 PNG；调用方须另置 SubjectPreserveDeliveryFromCompose 才可声明外观不变。
func ApplySubjectPreserveExtract(
	refPNG []byte,
	produceRoute map[string]any,
) (map[string]any, subjectextract.Result, subjectextract.ComposeResult, error) {
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
		return produceRoute, subjectextract.Result{}, subjectextract.ComposeResult{}, nil
	}
	result := subjectextract.ExtractPNG(refPNG)
	gated := subjectextract.ApplySubjectPreserveGate(produceRoute, result)
	if !result.Pass {
		return gated, result, subjectextract.ComposeResult{}, nil
	}
	composed := subjectextract.ComposeFromExtract(result, subjectextract.DefaultComposeSpec())
	return subjectextract.ApplySubjectComposeGate(gated, composed), result, composed, nil
}

// subjectPreserveImageDelivery 是 image_generation 在 subject_preserve 下的交付决议。
type subjectPreserveImageDelivery struct {
	RouteMap            map[string]any
	Compose             subjectextract.ComposeResult
	DeliveryFromCompose bool
}

// resolveSubjectPreserveImageDelivery 在持久化前决议交付字节来源。
// compose Pass：重建 produce_route 并置 SubjectPreserveDeliveryFromCompose，appearance_may_change=false 才诚实。
// extract/compose 失败：保留 route_qualified=false，不置交付标志；调用方仍可持久化生成式字节（不合格）。
// generative 路线原样返回声明，不做提取。
func resolveSubjectPreserveImageDelivery(refs []ReferenceImage, in ProduceRouteInput) subjectPreserveImageDelivery {
	route := NormalizeProduceRoute(in.Route)
	if route == "" {
		route = DefaultProduceRoute(in.ImageTypeKey)
	}
	in.Route = route
	base := ProduceRouteAsMap(BuildProduceRouteRecord(in))
	if route != ProduceRouteSubjectPreserve {
		return subjectPreserveImageDelivery{RouteMap: base}
	}
	gated, extract, composed, _ := ApplySubjectPreserveExtract(subjectPreserveReferencePNG(refs), base)
	if !composed.Pass || len(composed.PNG) == 0 || composed.PNGSHA256 == "" {
		// 失败策略：可仍存生成式成片，但不得合格采用、不得宣称外观不变。
		return subjectPreserveImageDelivery{RouteMap: gated, Compose: composed}
	}
	deliverIn := in
	deliverIn.SubjectPreserveDeliveryFromCompose = true
	routeRec := ProduceRouteAsMap(BuildProduceRouteRecord(deliverIn))
	routeRec = subjectextract.ApplySubjectPreserveGate(routeRec, extract)
	routeRec = subjectextract.ApplySubjectComposeGate(routeRec, composed)
	return subjectPreserveImageDelivery{
		RouteMap:            routeRec,
		Compose:             composed,
		DeliveryFromCompose: true,
	}
}

// gateImageProduceRouteWithSubjectExtract 对 subject_preserve 自动 Apply 主体提取+合成元数据。
// 仅写闸元数据，不置 SubjectPreserveDeliveryFromCompose；交付字节路径见 resolveSubjectPreserveImageDelivery。
func gateImageProduceRouteWithSubjectExtract(refs []ReferenceImage, produceRoute map[string]any) map[string]any {
	if produceRoute == nil {
		return produceRoute
	}
	route, _ := produceRoute["route"].(string)
	if NormalizeProduceRoute(route) != ProduceRouteSubjectPreserve {
		return produceRoute
	}
	gated, _, _, _ := ApplySubjectPreserveExtract(subjectPreserveReferencePNG(refs), produceRoute)
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

// imageResultFromSubjectCompose 把 B1 合成 PNG 投影为 ImageResult（MIME/尺寸来自 compose）。
func imageResultFromSubjectCompose(composed subjectextract.ComposeResult) ImageResult {
	return ImageResult{
		Bytes:          composed.PNG,
		MIME:           "image/png",
		Model:          "subject_compose",
		ProviderStatus: "completed",
		Width:          composed.Width,
		Height:         composed.Height,
	}
}
