package settings

import (
	"strconv"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

const (
	exportSchemaVersion   = 3
	exportCompatibility   = "productflow-settings-v3"
	exportAppVersion      = "0.1.0"
	defaultPromptTemplate = `Create an image from the current user request.
Output size: {size}
{history_block}
Current user request:
{prompt}
Generate the image directly. Do not return explanatory text.`
)

var imageToolFieldKeys = []string{
	"model", "quality", "output_format", "output_compression",
	"background", "moderation", "action", "input_fidelity", "partial_images",
}

type configOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type configDefinition struct {
	Key         string
	Label       string
	Category    string
	InputType   string
	Description string
	Options     []configOption
	Secret      bool
	Minimum     *int
	Maximum     *int
	Optional    bool
}

func intPtr(v int) *int { return &v }

// configDefinitions 是设置页可编辑键的闭集。导入/导出和 UpdateConfig 都必须走这份表，不要在别处再列一遍。
func configDefinitions() []configDefinition {
	toolKeys := make([]configOption, 0, len(imageToolFieldKeys))
	for _, key := range imageToolFieldKeys {
		toolKeys = append(toolKeys, configOption{Value: key, Label: key})
	}
	empty := []configOption{{Value: "", Label: "默认"}}
	return []configDefinition{
		{Key: "image_tool_allowed_fields", Label: "可用 Tool 字段", Category: "图片工具参数", InputType: "multi_select", Options: toolKeys, Description: "控制前端可展示、后端可持久化并发送给 Responses image_generation tool 的高级字段；Images API n 由候选数量或下游承载节点数自动计算，不作为可选字段展示。"},
		{Key: "image_tool_model", Label: "Tool 模型", Category: "图片工具参数", InputType: "text", Description: "留空不发送；需要 provider 支持。", Optional: true},
		{Key: "image_tool_quality", Label: "质量", Category: "图片工具参数", InputType: "select", Options: append(empty, configOption{"auto", "Auto"}, configOption{"low", "Low"}, configOption{"medium", "Medium"}, configOption{"high", "High"}), Optional: true},
		{Key: "image_tool_output_format", Label: "格式", Category: "图片工具参数", InputType: "select", Options: append(empty, configOption{"png", "PNG"}, configOption{"jpeg", "JPEG"}, configOption{"webp", "WebP"}), Optional: true},
		{Key: "image_tool_output_compression", Label: "压缩", Category: "图片工具参数", InputType: "number", Description: "0-100；留空不发送。", Minimum: intPtr(0), Maximum: intPtr(100), Optional: true},
		{Key: "image_tool_background", Label: "背景", Category: "图片工具参数", InputType: "select", Options: append(empty, configOption{"auto", "Auto"}, configOption{"opaque", "Opaque"}, configOption{"transparent", "Transparent"}), Description: "仅在可用 Tool 字段勾选 background 后发送。", Optional: true},
		{Key: "image_tool_moderation", Label: "审核", Category: "图片工具参数", InputType: "select", Options: append(empty, configOption{"auto", "Auto"}, configOption{"low", "Low"}), Optional: true},
		{Key: "image_tool_action", Label: "Action", Category: "图片工具参数", InputType: "select", Options: append(empty, configOption{"auto", "Auto"}, configOption{"generate", "Generate"}, configOption{"edit", "Edit"}), Optional: true},
		{Key: "image_tool_input_fidelity", Label: "Input fidelity", Category: "图片工具参数", InputType: "select", Options: append(empty, configOption{"low", "Low"}, configOption{"high", "High"}), Optional: true},
		{Key: "image_tool_partial_images", Label: "Partial", Category: "图片工具参数", InputType: "number", Description: "0-3；留空不发送。", Minimum: intPtr(0), Maximum: intPtr(3), Optional: true},
		{Key: "image_generation_max_dimension", Label: "生图最大单边", Category: "图片生成", InputType: "number", Description: "文/图生图和工作流生图的最大宽/高像素；最大面积同步使用该值的平方。", Minimum: intPtr(512), Maximum: intPtr(8192)},
		{Key: "prompt_image_chat_template", Label: "文/图生图提示词模板", Category: "提示词", InputType: "textarea", Description: "用于文/图生图对话。可用占位符：prompt、size、history_block。"},
		{Key: "upload_max_image_bytes", Label: "单图最大字节数", Category: "图片与上传", InputType: "number", Minimum: intPtr(1)},
		{Key: "upload_max_batch_bytes", Label: "批量上传最大总字节数", Category: "图片与上传", InputType: "number", Minimum: intPtr(1)},
		{Key: "upload_max_batch_files", Label: "批量上传最多文件数", Category: "图片与上传", InputType: "number", Minimum: intPtr(1)},
		{Key: "upload_max_reference_images", Label: "最多参考图数量", Category: "图片与上传", InputType: "number", Minimum: intPtr(0)},
		{Key: "upload_max_pixels", Label: "最大像素数", Category: "图片与上传", InputType: "number", Minimum: intPtr(1)},
		{Key: "upload_allowed_image_mime_types", Label: "允许图片 MIME", Category: "图片与上传", InputType: "textarea", Description: "逗号分隔，例如 image/png,image/jpeg,image/webp。"},
		{Key: "generation_max_concurrent_tasks", Label: "全局生成并发上限", Category: "生成队列", InputType: "number", Description: "全局资源保护阈值；工作流和文/图生图达到上限时会提示稍后重试。", Minimum: intPtr(1), Maximum: intPtr(20)},
		{Key: "image_session_stale_running_after_minutes", Label: "文/图生图进度闲置恢复阈值（分钟）", Category: "生成队列", InputType: "number", Description: "worker 启动恢复时，running 文/图生图任务会按最近 progress heartbeat 判断是否闲置；旧任务没有 progress 时回退到 started_at。", Minimum: intPtr(1), Maximum: intPtr(24 * 60)},
		{Key: "workflow_image_generation_provider_timeout_seconds", Label: "工作流生图 Provider 超时（秒）", Category: "生成队列", InputType: "number", Description: "工作流 AI 生图节点单次 provider 调用的项目级超时上界；超时后会安全失败并释放生成队列容量。", Minimum: intPtr(1), Maximum: intPtr(24 * 60 * 60)},
		{Key: "admin_access_required", Label: "要求登录访问密钥", Category: "安全与运维", InputType: "boolean", Description: "默认开启，普通工作台和私有 API 需要 ADMIN_ACCESS_KEY 登录；关闭后仍需 SETTINGS_ACCESS_TOKEN 才能查看和修改系统配置。"},
		{Key: "deletion_enabled", Label: "启用业务删除", Category: "安全与运维", InputType: "boolean", Description: "默认关闭，用于体验站禁止整条商品和文/图生图会话被删除，保留溯源证据。"},
	}
}

func definitionByKey(key string) (configDefinition, bool) {
	for _, def := range configDefinitions() {
		if def.Key == key {
			return def, true
		}
	}
	return configDefinition{}, false
}

// parseImageToolAllowedFields 解析逗号/空白分隔的 tool 字段。未知字段 400；输出按 imageToolFieldKeys 原序，方便稳定展示。
func parseImageToolAllowedFields(value string) ([]string, error) {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' })
	selected := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		selected[part] = struct{}{}
	}
	allowed := map[string]struct{}{}
	for _, key := range imageToolFieldKeys {
		allowed[key] = struct{}{}
	}
	unknown := make([]string, 0)
	for part := range selected {
		if _, ok := allowed[part]; !ok {
			unknown = append(unknown, part)
		}
	}
	out := make([]string, 0, len(imageToolFieldKeys))
	for _, key := range imageToolFieldKeys {
		if _, ok := selected[key]; ok {
			out = append(out, key)
		}
	}
	if len(unknown) > 0 {
		return out, apperr.Validation("可用 Tool 字段包含不支持的字段: " + strings.Join(sortedUnique(unknown), ", "))
	}
	return out, nil
}

func defaultImageToolAllowedFieldsText() string {
	fields := make([]string, 0, len(imageToolFieldKeys))
	for _, key := range imageToolFieldKeys {
		if key != "background" {
			fields = append(fields, key)
		}
	}
	return strings.Join(fields, ",")
}

// envDefault 给出 app_settings 没有覆盖时的启动默认值。上传上限来自 env overlay；模板/尺寸是内置常数。
func envDefault(s *Store, key string) string {
	switch key {
	case "image_tool_allowed_fields":
		return defaultImageToolAllowedFieldsText()
	case "image_generation_max_dimension":
		return "3840"
	case "prompt_image_chat_template":
		return defaultPromptTemplate
	case "upload_max_image_bytes":
		return strconv.Itoa(s.env.UploadMaxImageBytes)
	case "upload_max_batch_bytes":
		return strconv.Itoa(s.env.UploadMaxBatchBytes)
	case "upload_max_batch_files":
		return strconv.Itoa(s.env.UploadMaxBatchFiles)
	case "upload_max_reference_images":
		return strconv.Itoa(s.env.UploadMaxReferenceImages)
	case "upload_max_pixels":
		return strconv.Itoa(s.env.UploadMaxPixels)
	case "upload_allowed_image_mime_types":
		return s.env.UploadAllowedMIMETypes
	case "generation_max_concurrent_tasks":
		return "3"
	case "image_session_stale_running_after_minutes":
		return "90"
	case "workflow_image_generation_provider_timeout_seconds":
		return "900"
	case "admin_access_required":
		if s.env.AdminAccessRequired {
			return "true"
		}
		return "false"
	case "deletion_enabled":
		if s.env.DeletionEnabled {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}
