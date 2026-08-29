package product

import "github.com/yuqie6/productflow/internal/platform/canonjson"

func canonicalJSON(v any) ([]byte, error) {
	return canonjson.Compact(v)
}
