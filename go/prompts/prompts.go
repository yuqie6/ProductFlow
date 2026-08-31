// Package prompts 通过 go:embed 提供固定模型 prompt 文本；graph、providers 与 agent 加载它们。
//
// 改文案只改本目录 markdown，然后确认 Must 解析测试仍过。不要把 Agent Skill 正文、工具 JSON schema
// 或按图类分支的拼装逻辑放进本包——拼装在 graph.prompt_assemble / providers。
package prompts

import (
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

//go:embed agent/*.md listing/*.md providers/*.md
var files embed.FS

var requiredImageTypeKeys = []string{
	"hero", "selling_point", "scene", "detail", "sku",
	"dimensions", "specifications", "after_sales", "brand_story", "precautions",
	"certification", "faq", "factory", "packaging", "shipping",
}

var requiredLookSections = []string{
	"product_share_percent", "benefit_count", "source_note_is_product_fact",
	"ignore_as_art_direction", "do_not_invert_into", "brief_prohibitions",
}

var requiredIdentitySections = []string{
	"shared", "no_on_image_text", "no_caption_on_reference", "context_derived",
}

var requiredCompileSections = []string{
	"lead", "identity", "recompose", "invent",
	"photography", "infographic", "evidence",
	"text_none", "text_required", "text_required_with_language",
	"text_required_infographic", "text_required_infographic_with_language",
}

// Look 是 listing/look.md 解析出的刊登视觉规则，进程内缓存，不是 HTTP DTO。
// SourceNoteIsProductFact 等是规则开关，不要写成商品 FactSet。
type Look struct {
	Rule                    string   // look.md 导言
	ProductSharePercent     string   // 图种主体占比规则
	BenefitCount            string   // 卖点条数范围
	SourceNoteIsProductFact bool     // source_note 当商品事实，不是美术方向
	IgnoreAsArtDirection    []string // 参考图里要忽略的偶然因素
	DoNotInvertInto         []string // 禁止反推成的视觉套路
	BriefProhibitions       []string // brief 禁写项
}

// ImageType 是 listing/image-types.md 中的一种图类（hero、selling_point 等闭集）。
type ImageType struct {
	Key         string // 闭集 hero / selling_point 等
	Title       string
	Description string // 图种说明
	Job         string // 该图种要完成的拍摄任务
	Order       int    // 排序
}

// Identity 是 listing/identity-rules.md 的商品身份约束。
type Identity struct {
	Shared               []string // 外形材质以参考图为准
	NoOnImageText        string   // 画面不含文字/价格/Logo
	NoCaptionOnReference string   // 参考图只作商品本体
	ContextDerived       string   // 根据参考图、资料与图种生成
}

// CompileImage 是 listing/compile-image.md 的拼装模板，含 {type_title}/{language} 占位符。
type CompileImage struct {
	Lead                                string            // 含 {type_title}
	Identity                            string            // 身份约束段
	Recompose                           string            // 按图种重做构图
	Invent                              string            // 禁止虚构资料未出现的内容
	Photography                         string            // 摄影家族模板
	Infographic                         string            // 信息图家族模板
	Evidence                            string            // 资质/工厂说明图
	TextNone                            string            // 画面无字
	TextRequired                        string            // 画面需要文字
	TextRequiredWithLanguage            string            // 含 {language}
	TextRequiredInfographic             string            // 信息图需要文字
	TextRequiredInfographicWithLanguage string            // 信息图 + {language}
	TypeLines                           map[string]string // 图类 key → compile 行
}

var (
	agentWorkflow   string
	agentGlobal     string
	agentGoalLoop   string
	briefInstr      string
	overlayInstr    string
	promptInstr     string
	sourceNoteInstr string
	look            Look
	imageTypes      []ImageType
	imageTypeMap    map[string]ImageType
	identity        Identity
	compileImage    CompileImage
)

func init() {
	if err := load(); err != nil {
		panic("prompts: " + err.Error())
	}
}

// AgentWorkflow 返回 agent/workflow-live-graph.md 全文。
func AgentWorkflow() string { return agentWorkflow }

// AgentGlobal 返回 agent/global.md 全文（进程内缓存）。
// 调用时机：全局 Agent 系统提示。改文案只改 markdown。不要把 Skill/JSON schema 塞进本文件。
func AgentGlobal() string { return agentGlobal }

// AgentGoalLoop 返回 agent/goal-loop.md 全文。
func AgentGoalLoop() string { return agentGoalLoop }

// BriefInstructions 返回 providers/creative-brief.md 全文。
func BriefInstructions() string { return briefInstr }

// OverlayInstructions 返回 providers/visual-overlay.md 全文。
func OverlayInstructions() string { return overlayInstr }

// PromptInstructions 返回 providers/prompt-generation.md 全文。
func PromptInstructions() string { return promptInstr }

// SourceNoteInstructions 返回创建页看图起草商品说明的指令，正文在 providers/source-note.md。
func SourceNoteInstructions() string { return sourceNoteInstr }

// ListingLook 返回进程内缓存的刊登视觉规则副本语义（结构体值拷贝）。
func ListingLook() Look { return look }

// ImageTypes 返回按 order 排序的图类副本；调用方可改切片，不会写回缓存。
func ImageTypes() []ImageType { return append([]ImageType(nil), imageTypes...) }

// ImageTypeByKey 按闭集 key（hero、selling_point 等）查找图类。未知 key 返回零值与 false。
func ImageTypeByKey(key string) (ImageType, bool) {
	item, ok := imageTypeMap[key]
	return item, ok
}

// IdentityRules 返回进程内缓存的身份约束。调用时机：graph 拼装提示词。无 IO。
func IdentityRules() Identity { return identity }

// CompileImageTemplates 返回进程内缓存的拼装模板（含 {type_title}/{language}）。
// 真正 Expand 在 graph/providers；本函数不按图类分支。
func CompileImageTemplates() CompileImage { return compileImage }

// Expand 把 template 里的 {key} 替换成 vars 的值。
func Expand(template string, vars map[string]string) string {
	out := template
	for key, value := range vars {
		out = strings.ReplaceAll(out, "{"+key+"}", value)
	}
	return out
}

// LeadFor 用 type_title 展开 Lead 模板。调用时机：拼装某图类的 lead 段。缺占位则原样返回。
func (c CompileImage) LeadFor(typeTitle string) string {
	return Expand(c.Lead, map[string]string{"type_title": typeTitle})
}

// TextPolicyLine 按 policy（required/none）与 family（infographic 等）选出一行文案；未知 policy 返回空串。
func (c CompileImage) TextPolicyLine(policy, language, family string) string {
	switch policy {
	case "required":
		if family == "infographic" {
			if strings.TrimSpace(language) != "" {
				return Expand(c.TextRequiredInfographicWithLanguage, map[string]string{"language": strings.TrimSpace(language)})
			}
			return c.TextRequiredInfographic
		}
		if strings.TrimSpace(language) != "" {
			return Expand(c.TextRequiredWithLanguage, map[string]string{"language": strings.TrimSpace(language)})
		}
		return c.TextRequired
	case "none":
		return c.TextNone
	default:
		return ""
	}
}

// FamilyLine 返回 photography/infographic/evidence 家族说明；其他 family 走 photography。
func (c CompileImage) FamilyLine(family string) string {
	switch family {
	case "infographic":
		return c.Infographic
	case "evidence":
		return c.Evidence
	default:
		return c.Photography
	}
}

// TypeLine 返回该图类的 compile 行；缺 key 返回空串。
func (c CompileImage) TypeLine(key string) string {
	key = strings.TrimSpace(key)
	if key == "" || c.TypeLines == nil {
		return ""
	}
	return strings.TrimSpace(c.TypeLines[key])
}

// load 在 init 里一次性读 embed 文件并解析 listing 结构。任一文件空或章节缺字段直接失败，
// 进程起不来，避免运行时才发现 prompt 半残。不要在请求路径再读磁盘。
func load() error {
	var err error
	if agentWorkflow, err = wholeFile("agent/workflow-live-graph.md"); err != nil {
		return err
	}
	if agentGlobal, err = wholeFile("agent/global.md"); err != nil {
		return err
	}
	if agentGoalLoop, err = wholeFile("agent/goal-loop.md"); err != nil {
		return err
	}
	if briefInstr, err = wholeFile("providers/creative-brief.md"); err != nil {
		return err
	}
	if overlayInstr, err = wholeFile("providers/visual-overlay.md"); err != nil {
		return err
	}
	if promptInstr, err = wholeFile("providers/prompt-generation.md"); err != nil {
		return err
	}
	if sourceNoteInstr, err = wholeFile("providers/source-note.md"); err != nil {
		return err
	}
	if look, err = parseLook(); err != nil {
		return err
	}
	if imageTypes, imageTypeMap, err = parseImageTypes(); err != nil {
		return err
	}
	if identity, err = parseIdentity(); err != nil {
		return err
	}
	if compileImage, err = parseCompileImage(); err != nil {
		return err
	}
	return nil
}

func wholeFile(path string) (string, error) {
	raw, err := files.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return text, nil
}

// parseLook 解析 listing/look.md：正文前的 preamble 是总规则，## 章节必须齐全。
// source_note_is_product_fact 只能是 true/false；三个列表空则失败，禁止悄悄当「没有禁令」。
func parseLook() (Look, error) {
	raw, err := files.ReadFile("listing/look.md")
	if err != nil {
		return Look{}, err
	}
	preamble, sections, err := parseSections(string(raw))
	if err != nil {
		return Look{}, fmt.Errorf("listing/look.md: %w", err)
	}
	if preamble == "" {
		return Look{}, fmt.Errorf("listing/look.md: missing listing look rule before first heading")
	}
	if err := requireSections("listing/look.md", sections, requiredLookSections); err != nil {
		return Look{}, err
	}
	fact := strings.TrimSpace(sections["source_note_is_product_fact"])
	if fact != "true" && fact != "false" {
		return Look{}, fmt.Errorf("listing/look.md: source_note_is_product_fact must be true or false")
	}
	out := Look{
		Rule:                    preamble,
		ProductSharePercent:     strings.TrimSpace(sections["product_share_percent"]),
		BenefitCount:            strings.TrimSpace(sections["benefit_count"]),
		SourceNoteIsProductFact: fact == "true",
		IgnoreAsArtDirection:    listLines(sections["ignore_as_art_direction"]),
		DoNotInvertInto:         listLines(sections["do_not_invert_into"]),
		BriefProhibitions:       listLines(sections["brief_prohibitions"]),
	}
	if out.ProductSharePercent == "" || out.BenefitCount == "" {
		return Look{}, fmt.Errorf("listing/look.md: product_share_percent and benefit_count are required")
	}
	if len(out.IgnoreAsArtDirection) == 0 || len(out.DoNotInvertInto) == 0 || len(out.BriefProhibitions) == 0 {
		return Look{}, fmt.Errorf("listing/look.md: ignore, invert, and brief_prohibitions lists must not be empty")
	}
	return out, nil
}

// parseImageTypes 解析 listing/image-types.md。章节名必须正好是 requiredImageTypeKeys 闭集：
// 少一个、多一个都失败。返回值按 Order 再按 Key 排好，给创建页和下拉用。
func parseImageTypes() ([]ImageType, map[string]ImageType, error) {
	raw, err := files.ReadFile("listing/image-types.md")
	if err != nil {
		return nil, nil, err
	}
	_, sections, err := parseSections(string(raw))
	if err != nil {
		return nil, nil, fmt.Errorf("listing/image-types.md: %w", err)
	}
	byKey := map[string]ImageType{}
	for key, body := range sections {
		item, err := parseImageType(key, body)
		if err != nil {
			return nil, nil, err
		}
		byKey[key] = item
	}
	if len(byKey) != len(requiredImageTypeKeys) {
		return nil, nil, fmt.Errorf("listing/image-types.md: want %d types, got %d", len(requiredImageTypeKeys), len(byKey))
	}
	for _, key := range requiredImageTypeKeys {
		if _, ok := byKey[key]; !ok {
			return nil, nil, fmt.Errorf("listing/image-types.md: missing image type %s", key)
		}
	}
	for key := range byKey {
		found := false
		for _, want := range requiredImageTypeKeys {
			if key == want {
				found = true
				break
			}
		}
		if !found {
			return nil, nil, fmt.Errorf("listing/image-types.md: unknown image type %s", key)
		}
	}
	out := make([]ImageType, 0, len(byKey))
	for _, key := range requiredImageTypeKeys {
		out = append(out, byKey[key])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].Key < out[j].Key
	})
	return out, byKey, nil
}

// parseImageType 读一种图类：开头连续的 title/description/order 行，其余整段当 Job。
// 缺 title、description 或 Job 返回 error，不要用空 Job 让 compile 拼出半截提示词。
func parseImageType(key, body string) (ImageType, error) {
	item := ImageType{Key: key}
	lines := strings.Split(body, "\n")
	index := 0
	for index < len(lines) {
		line := strings.TrimRight(lines[index], " \t")
		if strings.TrimSpace(line) == "" && item.Title == "" {
			index++
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		field = strings.TrimSpace(field)
		if !ok || (field != "title" && field != "description" && field != "order") {
			break
		}
		value = strings.TrimSpace(value)
		switch field {
		case "title":
			item.Title = value
		case "description":
			item.Description = value
		case "order":
			n, err := strconv.Atoi(value)
			if err != nil {
				return ImageType{}, fmt.Errorf("listing/image-types.md: %s order: %w", key, err)
			}
			item.Order = n
		}
		index++
	}
	item.Job = strings.TrimSpace(strings.Join(lines[index:], "\n"))
	if item.Title == "" || item.Description == "" || item.Job == "" {
		return ImageType{}, fmt.Errorf("listing/image-types.md: %s needs title, description, and job", key)
	}
	return item, nil
}

// parseIdentity 解析 listing/identity-rules.md。shared 必须至少一行；三个具名规则空则失败。
func parseIdentity() (Identity, error) {
	raw, err := files.ReadFile("listing/identity-rules.md")
	if err != nil {
		return Identity{}, err
	}
	_, sections, err := parseSections(string(raw))
	if err != nil {
		return Identity{}, fmt.Errorf("listing/identity-rules.md: %w", err)
	}
	if err := requireSections("listing/identity-rules.md", sections, requiredIdentitySections); err != nil {
		return Identity{}, err
	}
	shared := listLines(sections["shared"])
	if len(shared) == 0 {
		return Identity{}, fmt.Errorf("listing/identity-rules.md: shared rules must not be empty")
	}
	out := Identity{
		Shared:               shared,
		NoOnImageText:        strings.TrimSpace(sections["no_on_image_text"]),
		NoCaptionOnReference: strings.TrimSpace(sections["no_caption_on_reference"]),
		ContextDerived:       strings.TrimSpace(sections["context_derived"]),
	}
	if out.NoOnImageText == "" || out.NoCaptionOnReference == "" || out.ContextDerived == "" {
		return Identity{}, fmt.Errorf("listing/identity-rules.md: named rules must not be empty")
	}
	return out, nil
}

// parseCompileImage 解析 listing/compile-image.md。每个必填章节空则失败；
// Lead 必须含 {type_title}，带语言的 text 模板必须含 {language}，否则 Expand 会留下字面占位符。
func parseCompileImage() (CompileImage, error) {
	raw, err := files.ReadFile("listing/compile-image.md")
	if err != nil {
		return CompileImage{}, err
	}
	_, sections, err := parseSections(string(raw))
	if err != nil {
		return CompileImage{}, fmt.Errorf("listing/compile-image.md: %w", err)
	}
	if err := requireSections("listing/compile-image.md", sections, requiredCompileSections); err != nil {
		return CompileImage{}, err
	}
	out := CompileImage{
		Lead:                                strings.TrimSpace(sections["lead"]),
		Identity:                            strings.TrimSpace(sections["identity"]),
		Recompose:                           strings.TrimSpace(sections["recompose"]),
		Invent:                              strings.TrimSpace(sections["invent"]),
		Photography:                         strings.TrimSpace(sections["photography"]),
		Infographic:                         strings.TrimSpace(sections["infographic"]),
		Evidence:                            strings.TrimSpace(sections["evidence"]),
		TextNone:                            strings.TrimSpace(sections["text_none"]),
		TextRequired:                        strings.TrimSpace(sections["text_required"]),
		TextRequiredWithLanguage:            strings.TrimSpace(sections["text_required_with_language"]),
		TextRequiredInfographic:             strings.TrimSpace(sections["text_required_infographic"]),
		TextRequiredInfographicWithLanguage: strings.TrimSpace(sections["text_required_infographic_with_language"]),
		TypeLines:                           map[string]string{},
	}
	for _, pair := range []struct {
		name, value string
	}{
		{"lead", out.Lead}, {"identity", out.Identity}, {"recompose", out.Recompose}, {"invent", out.Invent},
		{"photography", out.Photography}, {"infographic", out.Infographic}, {"evidence", out.Evidence},
		{"text_none", out.TextNone}, {"text_required", out.TextRequired},
		{"text_required_with_language", out.TextRequiredWithLanguage},
		{"text_required_infographic", out.TextRequiredInfographic},
		{"text_required_infographic_with_language", out.TextRequiredInfographicWithLanguage},
	} {
		if pair.value == "" {
			return CompileImage{}, fmt.Errorf("listing/compile-image.md: %s is empty", pair.name)
		}
	}
	for _, key := range requiredImageTypeKeys {
		line := strings.TrimSpace(sections[key])
		if line == "" {
			return CompileImage{}, fmt.Errorf("listing/compile-image.md: missing type line %s", key)
		}
		out.TypeLines[key] = line
	}
	if !strings.Contains(out.Lead, "{type_title}") {
		return CompileImage{}, fmt.Errorf("listing/compile-image.md: lead must contain {type_title}")
	}
	if !strings.Contains(out.TextRequiredWithLanguage, "{language}") {
		return CompileImage{}, fmt.Errorf("listing/compile-image.md: text_required_with_language must contain {language}")
	}
	if !strings.Contains(out.TextRequiredInfographicWithLanguage, "{language}") {
		return CompileImage{}, fmt.Errorf("listing/compile-image.md: text_required_infographic_with_language must contain {language}")
	}
	return out, nil
}

// parseSections 按 ## 标题切 Markdown。第一个标题前的正文是 preamble；空标题或重复标题返回 error。
// 只认行首「## 」，不要把正文里的 ## 当章节。
func parseSections(src string) (preamble string, sections map[string]string, err error) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	sections = map[string]string{}
	var current string
	var buf []string
	flush := func() {
		body := strings.TrimSpace(strings.Join(buf, "\n"))
		if current == "" {
			preamble = body
		} else {
			sections[current] = body
		}
		buf = nil
	}
	for _, line := range strings.Split(src, "\n") {
		if rest, ok := strings.CutPrefix(line, "## "); ok {
			name := strings.TrimSpace(rest)
			if name == "" {
				return "", nil, fmt.Errorf("empty section heading")
			}
			if _, dup := sections[name]; dup || current == name {
				return "", nil, fmt.Errorf("duplicate section %q", name)
			}
			flush()
			current = name
			continue
		}
		buf = append(buf, line)
	}
	flush()
	return preamble, sections, nil
}

func requireSections(path string, sections map[string]string, names []string) error {
	for _, name := range names {
		if _, ok := sections[name]; !ok {
			return fmt.Errorf("%s: missing section %s", path, name)
		}
	}
	return nil
}

func listLines(body string) []string {
	out := make([]string, 0)
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "- ")
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}
