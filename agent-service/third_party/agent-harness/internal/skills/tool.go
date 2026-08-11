package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/yuqie6/agent-harness/internal/tools"
)

const LoadToolName = "load_skill"

type loadRequest struct {
	Name         string `json:"name"`
	SkillPath    string `json:"skill_path"`
	ResourcePath string `json:"resource_path,omitempty"`
}

// Tool returns a read-only loader restricted to the catalog snapshot.
func (c *Catalog) Tool() tools.Tool {
	return tools.Tool{
		Name:         LoadToolName,
		Description:  "按 name 加载已发现 Agent Skill 的 SKILL.md 或其目录内相对文本资源。同名时使用 skill_path 消歧。只读取内容,不会执行脚本或授予额外权限。",
		Capabilities: tools.CapabilityRead,
		Effect:       tools.EffectPure,
		ParallelSafe: true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":          map[string]any{"type": "string", "description": "Skill name,例如 diagnose"},
				"skill_path":    map[string]any{"type": "string", "description": "可选;同名时使用元数据中给出的 SKILL.md 完整路径消歧"},
				"resource_path": map[string]any{"type": "string", "description": "可选;Skill 目录内的相对资源路径"},
			},
			"required":             []string{"name"},
			"additionalProperties": false,
		},
		AutoApprove: func(raw json.RawMessage) bool {
			_, _, _, err := c.resolve(raw)
			return err == nil
		},
		Handler: func(_ context.Context, raw json.RawMessage) (string, error) {
			entry, target, resource, err := c.resolve(raw)
			if err != nil {
				return "", err
			}
			content, err := readTextFile(target)
			if err != nil {
				return "", err
			}
			if target == entry.canonicalFile {
				metadata, err := parseMetadata(content, filepath.Base(filepath.Dir(entry.Path)))
				if err != nil {
					return "", fmt.Errorf("Skill 在发现后变为无效: %w", err)
				}
				if metadata.Name != entry.Name {
					return "", errors.New("Skill name 在发现后发生变化,请重启 harness")
				}
			}
			result := struct {
				Name          string `json:"name"`
				SkillPath     string `json:"skill_path"`
				BaseDirectory string `json:"base_directory"`
				ResourcePath  string `json:"resource_path,omitempty"`
				Content       string `json:"content"`
			}{
				Name: entry.Name, SkillPath: entry.Path, BaseDirectory: entry.canonicalRoot,
				ResourcePath: filepath.ToSlash(resource), Content: string(content),
			}
			encoded, err := json.Marshal(result)
			return string(encoded), err
		},
	}
}

func (c *Catalog) resolve(raw json.RawMessage) (catalogEntry, string, string, error) {
	if c == nil {
		return catalogEntry{}, "", "", errors.New("Skill catalog 不可用")
	}
	var request loadRequest
	if err := tools.ParseArguments(raw, &request); err != nil {
		return catalogEntry{}, "", "", fmt.Errorf("参数解析失败: %w", err)
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		return catalogEntry{}, "", "", errors.New("缺少 Skill name")
	}
	entry, err := c.lookup(request.Name, request.SkillPath)
	if err != nil {
		return catalogEntry{}, "", "", err
	}
	currentRoot, err := filepath.EvalSymlinks(filepath.Dir(entry.Path))
	if err != nil || currentRoot != entry.canonicalRoot {
		return catalogEntry{}, "", "", errors.New("Skill 目录在发现后发生变化,请重启 harness")
	}

	resource := strings.TrimSpace(request.ResourcePath)
	if resource == "" {
		currentFile, err := filepath.EvalSymlinks(entry.Path)
		if err != nil || currentFile != entry.canonicalFile {
			return catalogEntry{}, "", "", errors.New("SKILL.md 在发现后改变了文件边界,请重启 harness")
		}
		return entry, currentFile, "", nil
	}
	if filepath.IsAbs(resource) {
		return catalogEntry{}, "", "", errors.New("resource_path 必须是 Skill 目录内的相对路径")
	}
	resource = filepath.Clean(resource)
	if resource == "." || resource == ".." || strings.HasPrefix(resource, ".."+string(filepath.Separator)) {
		return catalogEntry{}, "", "", errors.New("resource_path 不能离开 Skill 目录")
	}
	target, err := filepath.EvalSymlinks(filepath.Join(entry.canonicalRoot, resource))
	if err != nil {
		return catalogEntry{}, "", "", err
	}
	if !within(entry.canonicalRoot, target) {
		return catalogEntry{}, "", "", errors.New("resource_path 通过符号链接离开 Skill 目录")
	}
	return entry, target, resource, nil
}

func (c *Catalog) lookup(name, path string) (catalogEntry, error) {
	path = strings.TrimSpace(path)
	if path != "" {
		path = filepath.Clean(path)
		if !filepath.IsAbs(path) {
			return catalogEntry{}, errors.New("skill_path 必须使用目录中给出的完整路径")
		}
		entry, ok := c.byPath[path]
		if !ok {
			return catalogEntry{}, errors.New("skill_path 不在已发现目录中")
		}
		if entry.Name != name {
			return catalogEntry{}, errors.New("name 与 skill_path 指向的 Skill 不一致")
		}
		return entry, nil
	}
	var found catalogEntry
	matches := 0
	for _, candidate := range c.byPath {
		if candidate.Name != name {
			continue
		}
		found = candidate
		matches++
	}
	switch matches {
	case 0:
		return catalogEntry{}, fmt.Errorf("未发现 Skill %q", name)
	case 1:
		return found, nil
	default:
		return catalogEntry{}, fmt.Errorf("Skill %q 存在 %d 个同名项,请提供 skill_path", name, matches)
	}
}
