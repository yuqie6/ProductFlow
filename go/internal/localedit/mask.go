package localedit

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

// parseDraft 收草稿字段。operation 必须在闭集；geometry JSON 非法返回 400。不在这里读 mask 字节。
func parseDraft(operation, instruction, sourceText, replacementText, geometryJSON, refsJSON string) (Draft, error) {
	if strings.TrimSpace(geometryJSON) == "" {
		return Draft{}, apperr.Validation("局部编辑 mask_geometry JSON 不能为空")
	}
	geo, err := parseGeometry(geometryJSON)
	if err != nil {
		return Draft{}, err
	}
	var refs []string
	if strings.TrimSpace(refsJSON) == "" {
		refs = []string{}
	} else if err := json.Unmarshal([]byte(refsJSON), &refs); err != nil {
		return Draft{}, apperr.Validation("局部编辑 draft 参数无效")
	}
	seen := map[string]struct{}{}
	cleaned := make([]string, 0, len(refs))
	for _, id := range refs {
		id = strings.TrimSpace(id)
		if id == "" {
			return Draft{}, apperr.Validation("局部编辑 reference asset id 不能为空")
		}
		if _, ok := seen[id]; ok {
			return Draft{}, apperr.Validation("局部编辑 reference asset id 不能重复")
		}
		seen[id] = struct{}{}
		cleaned = append(cleaned, id)
	}
	if len(cleaned) > maxReferences {
		return Draft{}, apperr.Validation("局部编辑 draft 参数无效")
	}
	instr := trimPtr(instruction)
	src := trimPtr(sourceText)
	rep := trimPtr(replacementText)
	switch operation {
	case opRemove, opInpaint:
		if instr == nil {
			return Draft{}, apperr.Validation(operation + " 操作必须提供非空 instruction")
		}
	case opReplaceText:
		if src == nil || rep == nil {
			return Draft{}, apperr.Validation("replace_text 必须同时提供非空 source_text 和 replacement_text")
		}
	default:
		return Draft{}, apperr.Validation("局部编辑 draft 参数无效")
	}
	return Draft{
		Operation: operation, Instruction: instr, SourceText: src, ReplacementText: rep,
		MaskGeometry: geo, ReferenceAssetIDs: cleaned,
	}, nil
}

// parseGeometry 解析视口到源图的仿射。缺字段或非有限数字返回 400，避免 mask 对不齐却静默提交。
func parseGeometry(raw string) (MaskGeometry, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return MaskGeometry{}, apperr.Validation("局部编辑 draft 参数无效")
	}
	known := map[string]struct{}{
		"source_width": {}, "source_height": {}, "viewport_width": {}, "viewport_height": {},
		"viewport_to_source": {}, "transform_direction": {},
	}
	for key := range payload {
		if _, ok := known[key]; !ok {
			return MaskGeometry{}, apperr.Validation("局部编辑 draft 参数无效")
		}
	}
	var geo MaskGeometry
	if err := json.Unmarshal([]byte(raw), &geo); err != nil {
		return MaskGeometry{}, apperr.Validation("局部编辑 draft 参数无效")
	}
	if geo.TransformDirection == "" {
		geo.TransformDirection = "viewport_to_source"
	}
	if geo.TransformDirection != "viewport_to_source" {
		return MaskGeometry{}, apperr.Validation("局部编辑 draft 参数无效")
	}
	if geo.SourceWidth <= 0 || geo.SourceHeight <= 0 || geo.ViewportWidth <= 0 || geo.ViewportHeight <= 0 {
		return MaskGeometry{}, apperr.Validation("局部编辑 draft 参数无效")
	}
	for _, n := range geo.ViewportToSource {
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return MaskGeometry{}, apperr.Validation("局部编辑 draft 参数无效")
		}
	}
	if math.IsNaN(geo.ViewportWidth) || math.IsInf(geo.ViewportWidth, 0) || math.IsNaN(geo.ViewportHeight) || math.IsInf(geo.ViewportHeight, 0) {
		return MaskGeometry{}, apperr.Validation("局部编辑 draft 参数无效")
	}
	det := geo.ViewportToSource[0]*geo.ViewportToSource[3] - geo.ViewportToSource[1]*geo.ViewportToSource[2]
	if math.Abs(det) <= 1e-12 {
		return MaskGeometry{}, apperr.Validation("局部编辑 draft 参数无效")
	}
	return geo, nil
}

func trimPtr(s string) *string {
	t := strings.TrimSpace(s)
	if t == "" {
		return nil
	}
	return &t
}

// normalizeMask 按 geometry 把画布 mask 对齐到源图像素并重编码 PNG。尺寸对不上返回 400。
func normalizeMask(sourceW, sourceH int, maskPNG []byte, geo MaskGeometry) ([]byte, error) {
	if geo.SourceWidth != sourceW || geo.SourceHeight != sourceH {
		return nil, apperr.Validation("mask geometry 的 source 尺寸必须匹配已核验的源图尺寸")
	}
	if len(maskPNG) == 0 {
		return nil, apperr.Validation("局部编辑 mask 必须是非空 PNG bytes")
	}
	img, err := png.Decode(bytes.NewReader(maskPNG))
	if err != nil {
		return nil, apperr.Validation("局部编辑 mask 不是可读取的 PNG")
	}
	b := img.Bounds()
	if b.Dx() != sourceW || b.Dy() != sourceH {
		return nil, apperr.Validation("局部编辑 mask 像素尺寸必须匹配源图尺寸")
	}
	selected, protected := 0, 0
	out := image.NewRGBA(image.Rect(0, 0, sourceW, sourceH))
	for y := 0; y < sourceH; y++ {
		for x := 0; x < sourceW; x++ {
			_, _, _, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			alpha := uint8(a >> 8)
			if alpha < 255 {
				selected++
			}
			if alpha > 0 {
				protected++
			}
			out.SetRGBA(x, y, color.RGBA{255, 255, 255, alpha})
		}
	}
	if selected == 0 {
		return nil, apperr.Validation("局部编辑 mask 不能没有可编辑像素")
	}
	if protected == 0 {
		return nil, apperr.Validation("局部编辑 mask 不能覆盖整张图，必须保留保护像素")
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, apperr.Validation("局部编辑 mask 不是可读取的 PNG")
	}
	return buf.Bytes(), nil
}

// providerInstruction 拼给供应商的英文指令。replace_text 把原文/替换文拼进去；空 instruction 用 operation 默认句。
func providerInstruction(d Draft) string {
	if d.Operation == opReplaceText {
		src, rep := "", ""
		if d.SourceText != nil {
			src = *d.SourceText
		}
		if d.ReplacementText != nil {
			rep = *d.ReplacementText
		}
		extra := ""
		if d.Instruction != nil {
			extra = "；补充要求：" + *d.Instruction
		}
		return "将图中原文字“" + src + "”替换为“" + rep + "”，只修改 mask 指定区域，不改变其他内容" + extra + "。"
	}
	if d.Instruction != nil {
		return *d.Instruction
	}
	return ""
}
