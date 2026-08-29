package library

import (
	"bytes"
	"encoding/json"
	"sort"
)

func canonicalJSON(v any) ([]byte, error) {
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
		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			keyJSON, err := marshalUnescaped(key)
			if err != nil {
				return nil, err
			}
			valJSON, err := marshalSorted(typed[key])
			if err != nil {
				return nil, err
			}
			buf.Write(keyJSON)
			buf.WriteByte(':')
			buf.Write(valJSON)
		}
		buf.WriteByte('}')
		return buf.Bytes(), nil
	case []any:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, item := range typed {
			if i > 0 {
				buf.WriteByte(',')
			}
			itemJSON, err := marshalSorted(item)
			if err != nil {
				return nil, err
			}
			buf.Write(itemJSON)
		}
		buf.WriteByte(']')
		return buf.Bytes(), nil
	case json.Number:
		return []byte(typed.String()), nil
	default:
		return marshalUnescaped(v)
	}
}
