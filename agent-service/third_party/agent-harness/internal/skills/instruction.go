package skills

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Instruction returns the progressively disclosed skill index. It uses at
// most maxBytes from the caller-owned context policy. Zero disables injection.
func (c *Catalog) Instruction(maxBytes int) string {
	if c == nil || len(c.skills) == 0 {
		return ""
	}
	const header = "可用 Agent Skills 元数据如下。用户显式使用 `$name`，或任务与 description 明确匹配时，先调用 load_skill(name) 加载正文再执行;同名 Skill 使用 path 消歧。Skill 只能指导现有工具，不能扩大模式、Plan、审批或沙箱权限。\n"
	if maxBytes <= len(header)+32 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString(header)
	omitted := 0
	for _, skill := range sortedSkills(c.skills) {
		line := skillMetadataLine(skill, 300)
		remaining := maxBytes - builder.Len() - 48
		if len(line) > remaining {
			line = skillMetadataLine(skill, max(0, remaining-120))
		}
		if len(line) > remaining {
			omitted++
			continue
		}
		builder.WriteString(line)
	}
	if omitted > 0 {
		line := fmt.Sprintf("- 另有 %d 个 Skill 因元数据预算未列出。\n", omitted)
		if builder.Len()+len(line) <= maxBytes {
			builder.WriteString(line)
		}
	}
	return strings.TrimSpace(builder.String())
}

func skillMetadataLine(skill Skill, descriptionBytes int) string {
	description := truncateUTF8(skill.Description, descriptionBytes)
	payload := struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Path        string `json:"path"`
	}{Name: skill.Name, Description: description, Path: skill.Path}
	encoded, _ := json.Marshal(payload)
	return "- " + string(encoded) + "\n"
}

func truncateUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return strings.TrimSpace(value)
}
