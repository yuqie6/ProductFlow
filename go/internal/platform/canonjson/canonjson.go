// Package canonjson 产出稳定 JSON（对象键排序、无多余空白），供哈希与契约比对。
package canonjson

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// Compact 对齐 Python json.dumps(sort_keys=True, separators=(",", ":"), ensure_ascii=False)。
// JSON 无法 compact（不可序列化的值）时失败并返回 error。
func Compact(v any) ([]byte, error) {
	raw, err := marshalUnescaped(v)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	return marshalSorted(decoded)
}

// SHA256Hex 对 Compact 结果做 sha256 hex，供幂等键和契约比对。
// 调用时机：DeliverySpec、配方 payload、上传请求哈希。error 来自无法 JSON 化的值。
// 禁区：不要对未 Compact 的 json.Marshal 字节哈希，键序不稳会导致假冲突。
func SHA256Hex(v any) (string, error) {
	raw, err := Compact(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func marshalUnescaped(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// marshalSorted 递归产出稳定 JSON：对象键按 UTF-16 码点排序（Go sort.Strings）、无空白。
// map 和 []any 自己拼；json.Number 原样写出，避免 float64 把大整数洗掉。其他类型走 marshalUnescaped。
func marshalSorted(v any) ([]byte, error) {
	switch typed := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		buf := []byte{'{'}
		for i, key := range keys {
			if i > 0 {
				buf = append(buf, ',')
			}
			keyJSON, err := marshalUnescaped(key)
			if err != nil {
				return nil, err
			}
			valJSON, err := marshalSorted(typed[key])
			if err != nil {
				return nil, err
			}
			buf = append(buf, keyJSON...)
			buf = append(buf, ':')
			buf = append(buf, valJSON...)
		}
		buf = append(buf, '}')
		return buf, nil
	case []any:
		buf := []byte{'['}
		for i, item := range typed {
			if i > 0 {
				buf = append(buf, ',')
			}
			itemJSON, err := marshalSorted(item)
			if err != nil {
				return nil, err
			}
			buf = append(buf, itemJSON...)
		}
		buf = append(buf, ']')
		return buf, nil
	case json.Number:
		return []byte(typed.String()), nil
	default:
		return marshalUnescaped(v)
	}
}
