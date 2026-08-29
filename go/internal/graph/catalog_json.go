package graph

// CatalogJSON 对齐 Python graph_catalog_json：Web 与 Agent 共用这份节点合同。
func CatalogJSON() map[string]any {
	nodes := make([]map[string]any, 0, len(catalogNodeOrder))
	for _, nodeType := range catalogNodeOrder {
		out, err := GraphNodeOutputType(nodeType)
		if err != nil {
			continue
		}
		kind := "source"
		if IsProcessingNode(nodeType) {
			kind = "processing"
		}
		accepts := catalogAccepts(nodeType)
		acceptJSON := make([]map[string]any, 0, len(accepts))
		for _, item := range accepts {
			acceptJSON = append(acceptJSON, map[string]any{
				"data_type":       item.DataType,
				"role":            item.Role,
				"max_count":       item.MaxCount,
				"required_to_run": item.RequiredToRun,
			})
		}
		fields, ok := nodeConfigFields(nodeType)
		if !ok {
			fields = []configField{}
		}
		fieldJSON := make([]map[string]any, 0, len(fields))
		for _, field := range fields {
			fieldJSON = append(fieldJSON, configFieldJSON(field))
		}
		nodes = append(nodes, map[string]any{
			"node_type":        nodeType,
			"output_data_type": out,
			"kind":             kind,
			"accepts":          acceptJSON,
			"config_fields":    fieldJSON,
		})
	}
	return map[string]any{
		"version": CatalogVersion,
		"nodes":   nodes,
	}
}

func configFieldJSON(item configField) map[string]any {
	choices := item.choices
	if choices == nil {
		choices = []string{}
	}
	children := make([]map[string]any, 0, len(item.fields))
	for _, child := range item.fields {
		children = append(children, configFieldJSON(child))
	}
	var visible any
	if item.visibleWhen != nil {
		visible = map[string]any{
			"field":  item.visibleWhen.Field,
			"op":     item.visibleWhen.Op,
			"values": item.visibleWhen.Values,
		}
	}
	return map[string]any{
		"key":              item.key,
		"value_kind":       item.valueKind,
		"control":          item.control,
		"required":         item.required,
		"label_key":        emptyToNil(item.labelKey),
		"hint_key":         emptyToNil(item.hintKey),
		"toggle_label_key": emptyToNil(item.toggleLabelKey),
		"choices":          choices,
		"min_value":        item.minValue,
		"max_value":        item.maxValue,
		"max_length":       item.maxLength,
		"default":          cloneValue(item.defaultValue),
		"panel":            emptyToNil(item.panel),
		"visible_when":     visible,
		"fields":           children,
	}
}

func emptyToNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}
