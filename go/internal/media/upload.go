package media

import (
	"fmt"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

type Validated struct {
	Content  []byte
	Filename string
	MIMEType string
}

func ValidateUpload(filename, declaredMIME string, content []byte, limits Limits) (Validated, error) {
	if filename == "" {
		filename = "reference.bin"
	}
	declared := NormalizeMIME(declaredMIME)
	if declared == "" {
		declared = "application/octet-stream"
	}
	if declared != "application/octet-stream" && !limits.Allows(declared) {
		return Validated{}, apperr.UnsupportedMedia(fmt.Sprintf("图片“%s”格式不受支持: %s", filename, declared))
	}
	if len(content) > limits.MaxImageBytes {
		return Validated{}, apperr.TooLarge(fmt.Sprintf("图片“%s”超过单张大小限制: %s", filename, FormatByteSize(limits.MaxImageBytes)))
	}
	if len(content) == 0 {
		return Validated{}, apperr.Validationf("图片“%s”内容不能为空", filename)
	}
	verified, err := Inspect(content, "")
	if err != nil {
		return Validated{}, apperr.Validationf("上传文件“%s”不是可解码的有效图片", filename)
	}
	if verified.Width <= 0 || verified.Height <= 0 || verified.Width*verified.Height > limits.MaxPixels {
		return Validated{}, apperr.Validationf(
			"图片“%s”像素数 (%dx%d) 超过系统限制: %d",
			filename, verified.Width, verified.Height, limits.MaxPixels,
		)
	}
	if !limits.Allows(verified.MIMEType) {
		return Validated{}, apperr.UnsupportedMedia(fmt.Sprintf("图片“%s”真实格式不受支持: %s", filename, verified.MIMEType))
	}
	if declared != "application/octet-stream" && verified.MIMEType != declared {
		return Validated{}, apperr.Validationf(
			"图片“%s”内容格式 (%s) 与声明类型 (%s) 不一致",
			filename, verified.MIMEType, declared,
		)
	}
	return Validated{Content: content, Filename: filename, MIMEType: verified.MIMEType}, nil
}

func ValidateBatch(uploads []Validated, limits Limits) error {
	if len(uploads) == 0 {
		return apperr.Validation("至少需要上传一张图片")
	}
	if len(uploads) > limits.MaxBatchFiles {
		return apperr.Validationf("单批次最多上传 %d 张图片，当前提交了 %d 张", limits.MaxBatchFiles, len(uploads))
	}
	total := 0
	for _, item := range uploads {
		total += len(item.Content)
		if total > limits.MaxBatchBytes {
			return apperr.TooLarge(fmt.Sprintf(
				"批量上传总大小超过限制: %s (当前已读取 %s)",
				FormatByteSize(limits.MaxBatchBytes),
				FormatByteSize(total),
			))
		}
	}
	return nil
}

func NormalizeDeclaredMIME(declared string) string {
	cleaned := NormalizeMIME(declared)
	if cleaned == "" {
		return "application/octet-stream"
	}
	return cleaned
}

func NormalizeMIME(raw string) string {
	cleaned := strings.ToLower(strings.TrimSpace(strings.Split(raw, ";")[0]))
	switch cleaned {
	case "image/jpg", "image/pjpeg":
		return "image/jpeg"
	case "image/x-png":
		return "image/png"
	default:
		return cleaned
	}
}
