package product

import (
	"math/big"
	"strings"
	"unicode/utf8"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

func normalizeName(value string) (string, error) {
	return requireText(value, "商品名", 255)
}

func requireText(value, field string, max int) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", apperr.Validation(field + "不能为空")
	}
	if utf8.RuneCountInString(normalized) > max {
		return "", apperr.Validationf("%s不能超过 %d 个字符", field, max)
	}
	return normalized, nil
}

func optionalText(value, field string, max int) (*string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(normalized) > max {
		return nil, apperr.Validationf("%s不能超过 %d 个字符", field, max)
	}
	return &normalized, nil
}

func normalizePrice(value string) (*string, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return nil, nil
	}
	if strings.HasPrefix(raw, "+") {
		raw = raw[1:]
	}
	if _, ok := new(big.Rat).SetString(raw); !ok {
		return nil, apperr.Validation("价格格式不正确")
	}
	if strings.HasPrefix(raw, "-") {
		return nil, apperr.Validation("价格必须是非负数字")
	}
	if i := strings.IndexByte(raw, '.'); i >= 0 {
		if len(raw)-i-1 > 2 {
			return nil, apperr.Validation("价格最多保留两位小数")
		}
	}
	return &raw, nil
}
