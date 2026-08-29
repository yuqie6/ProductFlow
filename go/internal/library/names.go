package library

import (
	"regexp"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"golang.org/x/text/cases"
)

var whitespace = regexp.MustCompile(`\s+`)
var folder = cases.Fold()

const (
	kindFolder = "folder"
	kindTag    = "tag"
)

func normalizeName(value, kind string) (string, error) {
	label, maximum := "文件夹", maxFolderName
	if kind == kindTag {
		label, maximum = "标签", maxTagName
	}
	normalized := whitespace.ReplaceAllString(strings.TrimSpace(value), " ")
	if normalized == "" {
		return "", apperr.Validation(label + "名称不能为空")
	}
	if len([]rune(normalized)) > maximum {
		return "", apperr.Validationf("%s名称不能超过 %d 个字符", label, maximum)
	}
	return normalized, nil
}

func normalizeKey(value, kind string) (string, error) {
	display, err := normalizeName(value, kind)
	if err != nil {
		return "", err
	}
	return folder.String(display), nil
}

func normalizeSearch(search string) string {
	return strings.Join(strings.Fields(search), " ")
}
