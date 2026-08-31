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

type Look struct {
	Rule                    string
	ProductSharePercent     string
	BenefitCount            string
	SourceNoteIsProductFact bool
	IgnoreAsArtDirection    []string
	DoNotInvertInto         []string
	BriefProhibitions       []string
}

type ImageType struct {
	Key         string
	Title       string
	Description string
	Job         string
	Order       int
}

type Identity struct {
	Shared               []string
	NoOnImageText        string
	NoCaptionOnReference string
	ContextDerived       string
}

type CompileImage struct {
	Lead                                string
	Identity                            string
	Recompose                           string
	Invent                              string
	Photography                         string
	Infographic                         string
	Evidence                            string
	TextNone                            string
	TextRequired                        string
	TextRequiredWithLanguage            string
	TextRequiredInfographic             string
	TextRequiredInfographicWithLanguage string
	TypeLines                           map[string]string
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

func AgentWorkflow() string { return agentWorkflow }
func AgentGlobal() string   { return agentGlobal }
func AgentGoalLoop() string { return agentGoalLoop }

func BriefInstructions() string   { return briefInstr }
func OverlayInstructions() string { return overlayInstr }
func PromptInstructions() string  { return promptInstr }

// SourceNoteInstructions 返回创建页看图起草商品说明的指令，正文在 providers/source-note.md。
func SourceNoteInstructions() string { return sourceNoteInstr }

func ListingLook() Look       { return look }
func ImageTypes() []ImageType { return append([]ImageType(nil), imageTypes...) }
func ImageTypeByKey(key string) (ImageType, bool) {
	item, ok := imageTypeMap[key]
	return item, ok
}
func IdentityRules() Identity             { return identity }
func CompileImageTemplates() CompileImage { return compileImage }

func Expand(template string, vars map[string]string) string {
	out := template
	for key, value := range vars {
		out = strings.ReplaceAll(out, "{"+key+"}", value)
	}
	return out
}

func (c CompileImage) LeadFor(typeTitle string) string {
	return Expand(c.Lead, map[string]string{"type_title": typeTitle})
}

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

func (c CompileImage) TypeLine(key string) string {
	key = strings.TrimSpace(key)
	if key == "" || c.TypeLines == nil {
		return ""
	}
	return strings.TrimSpace(c.TypeLines[key])
}

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
