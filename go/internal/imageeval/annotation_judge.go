package imageeval

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// AnnotationSystemPrompt is versioned separately from the historical slot
// judge prompt. It asks for evidence-bearing observations instead of a gate.
const AnnotationSystemPrompt = `你是分类电商图片质量评审员。只能依据当前请求中的图片和文字标准作判断，不得补写图片中看不见的品牌、参数、材质、功能或商品事实。
身份参考图仅用于核对商品身份，不能充当质量评分对照。reference 模式只给待标注图 scores，quality_reference_scores 必须为 null，也不要输出 comparison_basis；comparison 模式必须提供 comparison_basis，并且仅对明确标注的质量对照图填写 quality_reference_scores。
请按给定类目和图种职责，评估待标注图。先检查身份一致性，再判断图种职责、商业可用性和视觉完成度。quality_reference_scores 只在请求中存在质量对照图时填写，表示你对该质量对照图的同一套评分。
适用性与证据规则：
1. 类目展示重点是可检查项的集合。根据图种和本图可观察的展示目的，选择适用项；只把妨碍本图目的的缺失写成缺点。局部结构图可以聚焦一个部件，无需同时呈现容量、完整控制面板、包装、品牌文字或整件商品；这些信息适合在其他图位出现。没有展示不等于画错，无法核实的事实放入 uncertainties。
2. 身份判断区分品牌、具体型号、系列名、营销名和销售变体。不同层级的名称可以同时成立；只有可见结构、同一事实字段或明确变体出现可证明的不兼容，才判 mismatch。仅有不同字符串或缺少身份外观时，说明证据不足并保留 unknown；不要补写两者的关联，也不要因画面好看而忽略明确错误。
3. 比较前必须在 comparison_basis 中说明两图各自展示目的、共同适用要求、判定理由和两张对应图片的 asset_id。status 只能是 comparable、different_purpose 或 insufficient_evidence。同属一个图种也可能呈现不同部件或信息；内部结构说明与外部局部摄影的差异本身不是缺陷。四维分数依据各自明确目的和共同适用要求；只有 status=comparable 且有足够共同依据时填写 quality_reference_scores，否则保持 null，并在 uncertainties 说明无法形成可靠对照。共同要求需覆盖两图主要展示职责；不能只凭清晰、好看等通用审美进行整体胜负，但营销文字或背景差异本身也不能取消相同主体主视觉的共同依据。
4. 每条优缺点说明可见区域或具体文字、可观察现象以及它对本图目的的影响。裁切、遮挡、密度属于现象，是否影响关键展示另作判断；主观偏好和待核实推测不得升级为事实错误。
四维量表统一使用以下锚点：fidelity 评可见身份/结构的一致性，fit 评图种与本图目的完成度，utility 评该图所需信息的准确清楚与实际可用性，aesthetics 评构图、层次与视觉完成度。1=明显错误或无法使用，2=主要目标受阻，3=目标部分完成且存在实质缺陷，4=目标清楚完成但有局部问题，5=适用目标完成且未见实质缺陷。缺乏证据时说明不确定，不用偏好或其他图位的职责制造一分差距。
status 只能是 complete 或 unknown。complete 必须填写四个 1 到 5 的 scores；unknown 不得用 0 代替无法判断，并应在 uncertainties 说明原因。每条 identity claim、strength、weakness、critical_error 和 recommendation 都必须引用请求中真实存在的 evidence_asset_ids；certainty 只能是 observed、likely 或 unknown；severity 只能是 info、minor、major 或 critical。
critical_error 用于严重的商品身份或事实错误；不要因为平均分高而省略它。recommendation 必须写出需要后续验证的 validation。没有质量对照图时不要填写或猜测对照分数。
只输出一个 JSON 对象，不要 Markdown、解释文字或额外字段：
{"status":"complete|unknown","scores":{"fidelity":1,"fit":1,"utility":1,"aesthetics":1},"quality_reference_scores":null或同样对象,"comparison_basis":{"status":"comparable|different_purpose|insufficient_evidence","target_purpose":"…","reference_purpose":"…","shared_requirements":["…"],"reason":"…","evidence_asset_ids":["target_asset_id","quality_reference_asset_id"]},"identity":{"status":"match|mismatch|unknown","claims":[{"text":"…","evidence_asset_ids":["…"],"severity":"info|minor|major|critical","certainty":"observed|likely|unknown"}]},"strengths":[{"text":"…","evidence_asset_ids":["…"],"severity":"info|minor|major|critical","certainty":"observed|likely|unknown"}],"weaknesses":[{"text":"…","evidence_asset_ids":["…"],"severity":"info|minor|major|critical","certainty":"observed|likely|unknown"}],"uncertainties":[{"text":"…","reason":"…","next_check":"…","evidence_asset_ids":["…"]}],"recommendations":[{"text":"…","rationale":"…","evidence_asset_ids":["…"],"validation":"…"}],"critical_errors":[{"text":"…","evidence_asset_ids":["…"],"severity":"critical","certainty":"observed|likely|unknown"}]}`

type annotationEndpointError struct {
	endpoint string
	status   int
	cause    error
}

func (e annotationEndpointError) Error() string {
	if e.status != 0 {
		return fmt.Sprintf("annotation %s endpoint returned HTTP %d", e.endpoint, e.status)
	}
	return fmt.Sprintf("annotation %s endpoint request failed", e.endpoint)
}

func (e annotationEndpointError) Unwrap() error {
	return e.cause
}

type annotationResponseError struct {
	endpoint string
}

// Only successful model output is retained here. HTTP bodies, request headers
// and transport errors must not be copied into annotation evidence.
type annotationOutputError struct {
	output string
	cause  error
}

func (e annotationOutputError) Error() string { return "invalid annotation output" }
func (e annotationOutputError) Unwrap() error { return e.cause }

func (e annotationResponseError) Error() string {
	return fmt.Sprintf("annotation %s endpoint returned an invalid response", e.endpoint)
}

func annotationFallbackAllowed(err error) bool {
	var endpointErr annotationEndpointError
	if !errors.As(err, &endpointErr) {
		return false
	}
	return endpointErr.status == http.StatusNotFound || endpointErr.status == http.StatusMethodNotAllowed
}

// AnnotationCategoryFocus supplies category-specific content checks. It is
// included in both the prompt and the persisted per-record criteria.
func AnnotationCategoryFocus(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "3c":
		return "关注型号外形、接口/按键等可见结构、材质颜色和屏幕可读信息；不要把未显示的参数当成事实。"
	case "appliance":
		return "关注容量/控制区等可见结构、使用安全感、操作与功能信息是否可读；不要凭图猜额定参数。"
	case "home":
		return "关注材质触感、尺度关系、家居环境和摆放用途；避免把装饰背景误当成商品组成。"
	case "womenswear", "menswear":
		return "关注版型、穿着状态、面料纹理、颜色和细节展示；避免凭图片断言尺码、成分或功能。"
	case "beauty":
		return "关注包装/瓶身身份、容量与成分文字可读性、质地表现和使用语境；看不清的功效或成分必须标不确定。"
	case "food":
		return "关注包装身份、规格/口味文字可读性、食品状态和食用场景；不要把宣传语当成已证实的营养或产地事实。"
	case "baby":
		return "关注婴童用品身份、尺寸安全感、使用姿态和材质可见细节；安全、年龄和承重等看不见的事实必须标不确定。"
	case "sports":
		return "关注运动装备结构、穿着/使用动作、支撑或抓地等可见表现；不要把未展示的性能指标当成事实。"
	default:
		return "关注商品身份、材质颜色、主体结构、文字可读性和该图种的商业展示职责；看不见的事实标不确定。"
	}
}

func AnnotationCriteria(category, imageType string) string {
	return "类目展示重点：" + AnnotationCategoryFocus(category) + " 图种职责：" + TypeJobSummary(imageType)
}

// Annotate sends one independent multimodal annotation request. It shares the
// old VisionJudge transport helpers while using its own prompt and decoder.
func (v VisionJudge) Annotate(ctx context.Context, in AnnotationInput) (AnnotationResult, error) {
	if strings.TrimSpace(v.APIKey) == "" || strings.TrimSpace(v.Model) == "" {
		return AnnotationResult{}, fmt.Errorf("vision annotation missing model or api key")
	}
	if in.Target.AssetID == "" || strings.TrimSpace(in.Target.Path) == "" {
		return AnnotationResult{}, fmt.Errorf("vision annotation target is required")
	}
	parts := []map[string]any{{
		"type": "input_text",
		"text": fmt.Sprintf("类目：%s\n商品标题：%s\n图种：%s\nvariant：%s\n%s\n请求模式：%s", in.Category, in.Title, in.ImageType, firstNonEmptyString(in.Variant, "reference"), firstNonEmptyString(in.Criteria, AnnotationCriteria(in.Category, in.ImageType)), annotationRequestMode(in)),
	}}
	roleContract := fmt.Sprintf("评分目标 asset_id=%s；本请求没有质量对照图，quality_reference_scores 必须为 null，comparison_basis 不得输出。身份参考图仅核对身份。", in.Target.AssetID)
	if in.QualityReference != nil {
		roleContract = fmt.Sprintf("评分目标 asset_id=%s；唯一质量对照 asset_id=%s。必须输出 comparison_basis 并引用这两个 asset_id；只有 comparison_basis.status=comparable 时填写 quality_reference_scores，否则必须为 null。身份参考图仅核对身份。", in.Target.AssetID, in.QualityReference.AssetID)
	}
	parts = append(parts, map[string]any{"type": "input_text", "text": roleContract})
	attach := func(label string, asset AnnotationAsset) error {
		if strings.TrimSpace(asset.Path) == "" {
			return fmt.Errorf("annotation asset %s path is empty", asset.AssetID)
		}
		body, mime, err := readDataURL(asset.Path)
		if err != nil {
			return fmt.Errorf("read annotation asset %s: %w", asset.AssetID, err)
		}
		parts = append(parts, map[string]any{"type": "input_text", "text": label + " asset_id=" + asset.AssetID})
		parts = append(parts, map[string]any{
			"type":      "input_image",
			"image_url": "data:" + mime + ";base64," + body,
		})
		return nil
	}
	for i, asset := range in.IdentityReferences {
		if err := attach(fmt.Sprintf("身份参考图 %d", i+1), asset); err != nil {
			return AnnotationResult{}, err
		}
	}
	if in.QualityReference != nil {
		if err := attach("质量对照真实商品图", *in.QualityReference); err != nil {
			return AnnotationResult{}, err
		}
	}
	label := "待标注真实商品图"
	if in.Target.Role == "evaluated_result" {
		label = "待评估候选图"
	}
	if err := attach(label, in.Target); err != nil {
		return AnnotationResult{}, err
	}
	client := v.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Minute}
	}
	text, responseErr := v.callAnnotationResponses(ctx, client, parts)
	if responseErr != nil {
		if !annotationFallbackAllowed(responseErr) {
			return AnnotationResult{}, responseErr
		}
		var chatErr error
		text, chatErr = v.callAnnotationChatCompletions(ctx, client, parts)
		if chatErr != nil {
			return AnnotationResult{}, fmt.Errorf("annotation responses endpoint unsupported; chat fallback failed: %w", chatErr)
		}
	}
	result, err := ParseAnnotationJSON(text)
	if err != nil {
		return AnnotationResult{}, annotationOutputError{output: text, cause: err}
	}
	result.rawOutput = text
	return result, nil
}

func annotationRequestMode(in AnnotationInput) string {
	if in.QualityReference != nil {
		return AnnotationModeComparison
	}
	return AnnotationModeReference
}

func (v VisionJudge) callAnnotationResponses(ctx context.Context, client *http.Client, parts []map[string]any) (string, error) {
	payload := map[string]any{
		"model":        v.Model,
		"instructions": AnnotationSystemPrompt,
		"input":        []map[string]any{{"role": "user", "content": parts}},
	}
	body, status, err := v.postJSON(ctx, client, judgeEndpoint(v.BaseURL, "/v1/responses"), payload)
	if err != nil {
		return "", annotationEndpointError{endpoint: "responses", cause: err}
	}
	if status < 200 || status >= 300 {
		return "", annotationEndpointError{endpoint: "responses", status: status}
	}
	text, err := parseResponsesText(body)
	if err != nil {
		return "", annotationResponseError{endpoint: "responses"}
	}
	return text, nil
}

func (v VisionJudge) callAnnotationChatCompletions(ctx context.Context, client *http.Client, parts []map[string]any) (string, error) {
	chatParts := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		switch part["type"] {
		case "input_text":
			chatParts = append(chatParts, map[string]any{"type": "text", "text": part["text"]})
		case "input_image":
			url, _ := part["image_url"].(string)
			chatParts = append(chatParts, map[string]any{
				"type":      "image_url",
				"image_url": map[string]string{"url": url},
			})
		}
	}
	payload := map[string]any{
		"model": v.Model,
		"messages": []map[string]any{
			{"role": "system", "content": AnnotationSystemPrompt},
			{"role": "user", "content": chatParts},
		},
	}
	body, status, err := v.postJSON(ctx, client, judgeEndpoint(v.BaseURL, "/v1/chat/completions"), payload)
	if err != nil {
		return "", annotationEndpointError{endpoint: "chat completions", cause: err}
	}
	if status < 200 || status >= 300 {
		return "", annotationEndpointError{endpoint: "chat completions", status: status}
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := unmarshalJSON(body, &parsed); err != nil {
		return "", annotationResponseError{endpoint: "chat completions"}
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return "", annotationResponseError{endpoint: "chat completions"}
	}
	return parsed.Choices[0].Message.Content, nil
}

// ParseAnnotationJSON strictly decodes the versioned model response. It does
// not strip prose or accept unknown fields, so malformed evidence cannot enter
// a report as if it were a valid assessment.
func ParseAnnotationJSON(raw string) (AnnotationResult, error) {
	var result AnnotationResult
	if err := decodeStrictJSON([]byte(strings.TrimSpace(raw)), &result); err != nil {
		return AnnotationResult{}, fmt.Errorf("annotation json: %w", err)
	}
	if err := validateAnnotationShape(result); err != nil {
		return AnnotationResult{}, err
	}
	return result, nil
}

func validateAnnotationShape(result AnnotationResult) error {
	switch result.Status {
	case AnnotationStatusComplete, AnnotationStatusUnknown:
	default:
		return fmt.Errorf("annotation status %q is unsupported", result.Status)
	}
	if result.Status == AnnotationStatusComplete {
		if result.Scores == nil {
			return fmt.Errorf("complete annotation requires scores")
		}
		if err := ValidateScores(*result.Scores); err != nil {
			return err
		}
	} else if result.Scores != nil {
		return fmt.Errorf("unknown annotation cannot contain scores")
	}
	if result.Status == AnnotationStatusUnknown && result.QualityReferenceScores != nil {
		return fmt.Errorf("unknown annotation cannot contain quality reference scores")
	}
	if result.QualityReferenceScores != nil {
		if err := ValidateScores(*result.QualityReferenceScores); err != nil {
			return fmt.Errorf("quality reference scores: %w", err)
		}
	}
	if result.ComparisonBasis != nil {
		if err := validateComparisonBasisShape(*result.ComparisonBasis); err != nil {
			return err
		}
		switch result.ComparisonBasis.Status {
		case AnnotationComparisonComparable:
			if result.Status == AnnotationStatusComplete && result.QualityReferenceScores == nil {
				return fmt.Errorf("comparable comparison requires quality reference scores")
			}
		case AnnotationComparisonDifferentPurpose, AnnotationComparisonInsufficientEvidence:
			if result.QualityReferenceScores != nil {
				return fmt.Errorf("non-comparable comparison cannot contain quality reference scores")
			}
		}
	}
	switch result.Identity.Status {
	case "match", "mismatch", "unknown":
	default:
		return fmt.Errorf("identity status %q is unsupported", result.Identity.Status)
	}
	if result.Status == AnnotationStatusUnknown && len(result.Uncertainties) == 0 {
		return fmt.Errorf("unknown annotation requires uncertainty")
	}
	for _, item := range result.Identity.Claims {
		if err := validateObservation(item, "identity claim"); err != nil {
			return err
		}
	}
	for _, item := range result.Strengths {
		if err := validateObservation(item, "strength"); err != nil {
			return err
		}
	}
	for _, item := range result.Weaknesses {
		if err := validateObservation(item, "weakness"); err != nil {
			return err
		}
	}
	for _, item := range result.CriticalErrors {
		if err := validateObservation(item, "critical error"); err != nil {
			return err
		}
		if item.Severity != "critical" {
			return fmt.Errorf("critical error severity must be critical")
		}
	}
	for _, item := range result.Uncertainties {
		if strings.TrimSpace(item.Text) == "" || strings.TrimSpace(item.Reason) == "" || strings.TrimSpace(item.NextCheck) == "" {
			return fmt.Errorf("uncertainty requires text, reason, and next_check")
		}
		if err := validateEvidenceIDs(item.EvidenceAssetIDs, "uncertainty"); err != nil {
			return err
		}
	}
	for _, item := range result.Recommendations {
		if strings.TrimSpace(item.Text) == "" || strings.TrimSpace(item.Rationale) == "" || strings.TrimSpace(item.Validation) == "" {
			return fmt.Errorf("recommendation requires text, rationale, and validation")
		}
		if err := validateEvidenceIDs(item.EvidenceAssetIDs, "recommendation"); err != nil {
			return err
		}
	}
	return nil
}

func validateComparisonBasisShape(basis AnnotationComparisonBasis) error {
	switch basis.Status {
	case AnnotationComparisonComparable, AnnotationComparisonDifferentPurpose, AnnotationComparisonInsufficientEvidence:
	default:
		return fmt.Errorf("comparison basis status %q is unsupported", basis.Status)
	}
	if strings.TrimSpace(basis.TargetPurpose) == "" {
		return fmt.Errorf("comparison basis target_purpose is required")
	}
	if strings.TrimSpace(basis.ReferencePurpose) == "" {
		return fmt.Errorf("comparison basis reference_purpose is required")
	}
	if basis.SharedRequirements == nil {
		return fmt.Errorf("comparison basis shared_requirements is required")
	}
	for _, requirement := range basis.SharedRequirements {
		requirement = strings.TrimSpace(requirement)
		if requirement == "" {
			return fmt.Errorf("comparison basis shared_requirements contains empty item")
		}
	}
	if basis.Status == AnnotationComparisonComparable && len(basis.SharedRequirements) == 0 {
		return fmt.Errorf("comparable comparison basis requires shared_requirements")
	}
	if strings.TrimSpace(basis.Reason) == "" {
		return fmt.Errorf("comparison basis reason is required")
	}
	return validateEvidenceIDs(basis.EvidenceAssetIDs, "comparison basis")
}

func validateObservation(item AnnotationObservation, label string) error {
	if strings.TrimSpace(item.Text) == "" {
		return fmt.Errorf("%s text is required", label)
	}
	switch item.Severity {
	case "info", "minor", "major", "critical":
	default:
		return fmt.Errorf("%s severity %q is unsupported", label, item.Severity)
	}
	switch item.Certainty {
	case "observed", "likely", "unknown":
	default:
		return fmt.Errorf("%s certainty %q is unsupported", label, item.Certainty)
	}
	return validateEvidenceIDs(item.EvidenceAssetIDs, label)
}

func validateEvidenceIDs(ids []string, label string) error {
	if len(ids) == 0 {
		return fmt.Errorf("%s requires evidence asset ids", label)
	}
	seen := map[string]struct{}{}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("%s contains empty evidence asset id", label)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("%s repeats evidence asset id %q", label, id)
		}
		seen[id] = struct{}{}
	}
	return nil
}
