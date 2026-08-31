package product

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/httpx"
)

type GeneratedSourceNoteField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type GeneratedSourceNote struct {
	Visible string                     `json:"visible"`
	Fields  []GeneratedSourceNoteField `json:"fields"`
}

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
	result, err := h.Service.SourceNote.GenerateSourceNote(c.Request.Context(), graph.PromptRequest{
		NodeTitle:       strings.TrimSpace(c.PostForm("product_name")),
		References:      refs,
		CurrentDocument: current,
	})
	if err != nil {
		httpx.AbortErr(c, sourceNoteGenerateErr(err))
		return
	}
	c.JSON(200, generatedSourceNoteFromPayload(result.Payload))
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
