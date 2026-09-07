package product

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/httpx"
)

// GeneratedSourceNoteField 是看图起草返回的一条可编辑规格。照片上看不到的值保持空串。
type GeneratedSourceNoteField struct {
	Label string `json:"label"`
	Value string `json:"value"` // 照片上看不到则空串
}

// GeneratedSourceNote 是 POST /api/v2/product-source-notes/generate 200 体：创建页看图起草，不走画布 cook、不写 products。
// Visible 是可见描述；Fields 是可编辑规格，照片上看不到的 Value 保持空串。不要当成 CreativeBrief 文档或 facts 版本。
type GeneratedSourceNote struct {
	Visible string                     `json:"visible"` // 可见商品说明
	Fields  []GeneratedSourceNoteField `json:"fields"`  // 可编辑规格；空列表是 [] 不是 nil
}

// generateSourceNote 是 POST /api/v2/product-source-notes/generate：200 起草结果；未配置供应商 503；额度不足 409。
func (h HTTP) generateSourceNote(c *gin.Context) {
	if h.Service.SourceNote == nil {
		httpx.AbortErr(c, apperr.Unavailable("未配置提示词供应商"))
		return
	}
	uploads, err := h.readImages(c, "images", "reference.bin", "至少上传一张商品参考图")
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	refs := make([]graph.ReferenceImage, 0, len(uploads))
	for _, item := range uploads {
		refs = append(refs, graph.ReferenceImage{
			Role:     "product_identity",
			Label:    item.Filename,
			MIME:     item.MIMEType,
			Filename: item.Filename,
			Bytes:    item.Content,
		})
	}
	currentNote := strings.TrimSpace(c.PostForm("current_note"))
	current := map[string]any{}
	if currentNote != "" {
		if utf8.RuneCountInString(currentNote) > 4000 {
			currentNote = string([]rune(currentNote)[:4000])
		}
		current["source_note"] = currentNote
	}
	out, err := h.Service.GenerateSourceNoteDraft(c.Request.Context(), graph.PromptRequest{
		NodeTitle:       strings.TrimSpace(c.PostForm("product_name")),
		References:      refs,
		CurrentDocument: current,
	})
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(200, out)
}

// GenerateSourceNoteDraft 在发出 prompt provider 前按商家 Reserve；成功 Settle；未发出 Release；结果不明 MarkUnknown。
func (s Service) GenerateSourceNoteDraft(ctx context.Context, req graph.PromptRequest) (GeneratedSourceNote, error) {
	if s.SourceNote == nil {
		return GeneratedSourceNote{}, apperr.Unavailable("未配置提示词供应商")
	}
	merchantID, err := auth.RequireMerchantID(ctx)
	if err != nil {
		return GeneratedSourceNote{}, err
	}
	requestID := clockid.New()
	if err := s.reserveSourceNoteQuota(ctx, merchantID, requestID); err != nil {
		return GeneratedSourceNote{}, err
	}
	// Reserve 后、尚未调用 provider：明确未发出 → Release。
	if err := ctx.Err(); err != nil {
		_ = s.releaseSourceNoteQuota(context.WithoutCancel(ctx), merchantID, requestID)
		return GeneratedSourceNote{}, err
	}
	result, err := s.SourceNote.GenerateSourceNote(ctx, req)
	if err != nil {
		// 可能已联系供应商：禁止当零消费 Release。
		_ = s.markSourceNoteQuotaUnknown(context.WithoutCancel(ctx), merchantID, requestID)
		return GeneratedSourceNote{}, sourceNoteGenerateErr(err)
	}
	if err := s.settleSourceNoteQuota(ctx, merchantID, requestID); err != nil {
		return GeneratedSourceNote{}, err
	}
	return generatedSourceNoteFromPayload(result.Payload), nil
}

func sourceNoteGenerateErr(err error) error {
	var e apperr.Error
	if errors.As(err, &e) {
		return err
	}
	return apperr.Unavailable("看图填写商品说明失败")
}

func generatedSourceNoteFromPayload(payload map[string]any) GeneratedSourceNote {
	return GeneratedSourceNote{
		Visible: payloadString(payload, "visible"),
		Fields:  payloadFields(payload["fields"]),
	}
}

func payloadFields(raw any) []GeneratedSourceNoteField {
	items := anySlice(raw)
	out := make([]GeneratedSourceNoteField, 0, len(items))
	for _, item := range items {
		row, _ := item.(map[string]any)
		if row == nil {
			continue
		}
		label := payloadString(row, "label")
		if label == "" {
			continue
		}
		out = append(out, GeneratedSourceNoteField{
			Label: label,
			Value: payloadString(row, "value"),
		})
	}
	return out
}

func anySlice(raw any) []any {
	switch value := raw.(type) {
	case []any:
		return value
	case []map[string]any:
		out := make([]any, 0, len(value))
		for _, item := range value {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func payloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}
