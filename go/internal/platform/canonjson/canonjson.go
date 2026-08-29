package canonjson

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// Compact 对齐 Python json.dumps(sort_keys=True, separators=(",", ":"), ensure_ascii=False)。
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

// SHA256Hex 对 Compact 结果做 sha256 hex。
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
