package graph

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneValue(value)
	}
	return out
}

// cloneValue 深拷贝 map/slice，标量原样返回。改图与 digest 前必须 clone，避免共享 config 被原地改。
func cloneValue(value any) any {
	switch t := value.(type) {
	case map[string]any:
		return cloneMap(t)
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = cloneValue(item)
		}
		return out
	case []string:
		out := make([]string, len(t))
		copy(out, t)
		return out
	case []map[string]any:
		out := make([]map[string]any, len(t))
		for i, item := range t {
			out[i] = cloneMap(item)
		}
		return out
	default:
		return value
	}
}

func asMap(value any) (map[string]any, bool) {
	m, ok := value.(map[string]any)
	return m, ok
}

func cloneStringPtr(v *string) *string {
	if v == nil {
		return nil
	}
	s := *v
	return &s
}

func stringPtr(s string) *string { return &s }
