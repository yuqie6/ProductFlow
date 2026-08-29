package library

import "github.com/yuqie6/productflow/internal/platform/canonjson"

func canonicalJSON(v any) ([]byte, error) {
	return canonjson.Compact(v)
}

func marshalUnescaped(v any) ([]byte, error) {
	return canonjson.Compact(v)
}
