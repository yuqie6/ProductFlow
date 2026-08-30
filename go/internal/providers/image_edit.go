package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"net/textproto"
	"strconv"
	"strings"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/imagesession"
	"github.com/yuqie6/productflow/internal/media"
)

type imagePart struct {
	Bytes    []byte
	MIME     string
	Filename string
}

func (p OpenAIImages) edit(ctx context.Context, prompt, size, quality string, images []imagePart, mask []byte, n int, classify func(int, []byte) error) ([]byte, string, string, string, error) {
	batch, mime, model, id, err := p.editN(ctx, prompt, size, quality, images, mask, n, classify)
	if err != nil {
		return nil, "", "", "", err
	}
	if len(batch) == 0 {
		return nil, "", "", "", fmt.Errorf("图片供应商没有返回图片结果，请稍后重试")
	}
	return batch[0], mime, model, id, nil
}

func (p OpenAIImages) editN(ctx context.Context, prompt, size, quality string, images []imagePart, mask []byte, n int, classify func(int, []byte) error) ([][]byte, string, string, string, error) {
	if len(images) == 0 {
		return nil, "", "", "", fmt.Errorf("图片供应商缺少编辑输入图片")
	}
	if quality == "" {
		quality = p.Quality
	}
	n = clampImageN(n)
	status, raw, err := p.postEdit(ctx, prompt, size, quality, images, mask, n)
	if err != nil {
		return nil, "", "", "", err
	}
	if status >= 400 && (quality != "" || len(images) > 1) {
		fallbackQuality := quality
		fallbackImages := images
		if quality != "" {
			fallbackQuality = ""
		}
		if len(images) > 1 {
			fallbackImages = images[:1]
		}
		status, raw, err = p.postEdit(ctx, prompt, size, fallbackQuality, fallbackImages, mask, n)
		if err != nil {
			return nil, "", "", "", err
		}
	}
	if err := classify(status, raw); err != nil {
		return nil, "", "", "", err
	}
	return parseImageResponses(raw, p.Model)
}

func (p OpenAIImages) postEdit(ctx context.Context, prompt, size, quality string, images []imagePart, mask []byte, n int) (int, []byte, error) {
	body, contentType, err := buildImagesEditMultipart(p.Model, prompt, size, quality, images, mask, n)
	if err != nil {
		return 0, nil, err
	}
	return p.callTyped(ctx, "POST", endpoint(p.BaseURL, "/v1/images/edits"), contentType, body)
}

func buildImagesEditMultipart(model, prompt, size, quality string, images []imagePart, mask []byte, n int) ([]byte, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	n = clampImageN(n)
	fields := [][2]string{
		{"model", model},
		{"prompt", prompt},
		{"size", size},
		{"n", strconv.Itoa(n)},
		{"response_format", "b64_json"},
	}
	if quality != "" {
		fields = append(fields, [2]string{"quality", quality})
	}
	for _, field := range fields {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			return nil, "", err
		}
	}
	fieldName := "image"
	if len(images) > 1 {
		fieldName = "image[]"
	}
	for i, img := range images {
		filename := img.Filename
		if filename == "" {
			filename = fmt.Sprintf("image-%d%s", i+1, media.ExtensionForMIME(partMIME(img)))
		}
		if err := writeMultipartFile(writer, fieldName, filename, partMIME(img), img.Bytes); err != nil {
			return nil, "", err
		}
	}
	if len(mask) > 0 {
		if err := writeMultipartFile(writer, "mask", "mask.png", "image/png", mask); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), writer.FormDataContentType(), nil
}

func writeMultipartFile(writer *multipart.Writer, field, filename, mimeType string, data []byte) error {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, field, filename))
	header.Set("Content-Type", mimeType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	_, err = part.Write(data)
	return err
}

func partMIME(img imagePart) string {
	if img.MIME != "" {
		return img.MIME
	}
	return sniffMIME(img.Bytes)
}

func graphRefsToParts(refs []graph.ReferenceImage) []imagePart {
	out := make([]imagePart, 0, len(refs))
	for _, ref := range refs {
		out = append(out, imagePart{Bytes: ref.Bytes, MIME: ref.MIME, Filename: ref.Filename})
	}
	return out
}

func chatImageParts(req imagesession.ChatRequest, includeBase bool) []imagePart {
	var out []imagePart
	if includeBase && len(req.BaseBytes) > 0 {
		out = append(out, imagePart{Bytes: req.BaseBytes, MIME: sniffMIME(req.BaseBytes), Filename: "base.png"})
	}
	for i, data := range req.ReferenceBytes {
		out = append(out, imagePart{Bytes: data, MIME: sniffMIME(data), Filename: fmt.Sprintf("reference-%d.png", i+1)})
	}
	return out
}

func chatGraphRefs(req imagesession.ChatRequest, includeBase bool) []graph.ReferenceImage {
	parts := chatImageParts(req, includeBase)
	out := make([]graph.ReferenceImage, 0, len(parts))
	for _, part := range parts {
		out = append(out, graph.ReferenceImage{Bytes: part.Bytes, MIME: part.MIME, Filename: part.Filename})
	}
	return out
}

func responsesInput(prompt string, refs []graph.ReferenceImage) any {
	if len(refs) == 0 {
		return prompt
	}
	content := []map[string]any{{"type": "input_text", "text": prompt}}
	for _, ref := range refs {
		mime := ref.MIME
		if mime == "" {
			mime = sniffMIME(ref.Bytes)
		}
		content = append(content, map[string]any{
			"type":      "input_image",
			"image_url": fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(ref.Bytes)),
		})
	}
	return []map[string]any{{"role": "user", "content": content}}
}

func finishImageResult(adapter string, data []byte, mime, model, id, size, quality string, refCount int) graph.ImageResult {
	width, height := 0, 0
	if verified, err := media.Inspect(data, ""); err == nil {
		width, height = verified.Width, verified.Height
		if mime == "" {
			mime = verified.MIMEType
		}
	}
	effective := map[string]any{
		"adapter":               adapter,
		"model":                 model,
		"size":                  size,
		"reference_image_count": refCount,
	}
	if quality != "" {
		effective["quality"] = quality
	}
	if width > 0 {
		effective["measured_width"] = width
		effective["measured_height"] = height
	}
	return graph.ImageResult{
		Bytes: data, MIME: mime, Model: model, ResponseID: id,
		ProviderStatus: "completed", Width: width, Height: height,
		EffectiveParameters: effective,
	}
}

func cloneJSONMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func chatImagesOverrides(opts map[string]any, model, quality string) (string, string) {
	if len(opts) == 0 {
		return model, quality
	}
	if m := optionalToolText(opts["model"]); m != "" {
		model = m
	}
	if q := optionalToolText(opts["quality"]); q != "" {
		quality = q
	}
	return model, quality
}

func optionalToolText(value any) string {
	if value == nil {
		return ""
	}
	s, ok := value.(string)
	if !ok {
		s = fmt.Sprint(value)
	}
	return strings.TrimSpace(s)
}

func dataURL(mimeType string, data []byte) string {
	if strings.TrimSpace(mimeType) == "" {
		mimeType = sniffMIME(data)
	}
	return fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
}
