package graph

import (
	"fmt"
	"sort"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/prompts"
)

func ListingLookRule() string { return prompts.ListingLook().Rule }

// ListingLookContext 出站 user JSON 必须带这份对象，不能只发 rule 字符串。权威在 go/prompts/listing/look.md。
func ListingLookContext() map[string]any {
	look := prompts.ListingLook()
	return map[string]any{
		"rule":                        look.Rule,
		"product_share_percent":       look.ProductSharePercent,
		"benefit_count":               look.BenefitCount,
		"source_note_is_product_fact": look.SourceNoteIsProductFact,
		"ignore_as_art_direction":     stringSliceToAny(look.IgnoreAsArtDirection),
		"do_not_invert_into":          stringSliceToAny(look.DoNotInvertInto),
	}
}

func stringSliceToAny(in []string) []any {
	out := make([]any, len(in))
	for i, item := range in {
		out[i] = item
	}
	return out
}

type imageTypeOption struct {
	Key         string
	Title       string
	Description string
	Order       int
}

func imageTypeJob(key string) string {
	item, ok := prompts.ImageTypeByKey(key)
	if !ok {
		return ""
	}
	return item.Job
}

var agentProductImageTypeCatalog = func() []imageTypeOption {
	types := prompts.ImageTypes()
	out := make([]imageTypeOption, 0, len(types))
	for _, item := range types {
		out = append(out, imageTypeOption{
			Key: item.Key, Title: item.Title, Description: item.Description, Order: item.Order,
		})
	}
	return out
}()

// ImageTypeCatalogJSON 给 Agent 商品上下文；标题与描述来自 go/prompts/listing/image-types.md。
func ImageTypeCatalogJSON() []map[string]any {
	out := make([]map[string]any, 0, len(agentProductImageTypeCatalog))
	for _, option := range agentProductImageTypeCatalog {
		out = append(out, map[string]any{
			"key":         option.Key,
			"title":       option.Title,
			"description": option.Description,
			"order":       option.Order,
		})
	}
	return out
}

var imageTypeByKey = func() map[string]imageTypeOption {
	out := map[string]imageTypeOption{}
	for _, option := range agentProductImageTypeCatalog {
		out[option.Key] = option
	}
	return out
}()

var imageTypeTitles = func() map[string]string {
	out := map[string]string{}
	for _, option := range agentProductImageTypeCatalog {
		out[option.Key] = option.Title
	}
	return out
}()

var evidenceImageTypeKeys = map[string]struct{}{"certification": {}, "factory": {}}
var infographicImageTypeKeys = map[string]struct{}{
	"selling_point": {}, "dimensions": {}, "specifications": {}, "after_sales": {},
	"precautions": {}, "faq": {}, "shipping": {}, "brand_story": {},
}

func ImageTypeFamily(key string) string { return imageTypeFamily(key) }

func imageTypeFamily(key string) string {
	if _, ok := evidenceImageTypeKeys[key]; ok {
		return "evidence"
	}
	if _, ok := infographicImageTypeKeys[key]; ok {
		return "infographic"
	}
	return "photography"
}

func imageTypePromptGoal(key string) string {
	option, ok := imageTypeByKey[key]
	title := key
	if ok {
		title = option.Title
	}
	job := imageTypeJob(key)
	if job == "" && ok {
		job = option.Description
	}
	if job == "" {
		return title
	}
	return title + "：" + job
}

const sourceNoteDesignGoalPrefix = "商品与受众资料："

func creativeBriefConfigFromSourceNote(sourceNote *string) map[string]any {
	text := ""
	if sourceNote != nil {
		text = strings.TrimSpace(*sourceNote)
	}
	if text == "" {
		return map[string]any{}
	}
	runes := []rune(text)
	if len(runes) > 3900 {
		text = string(runes[:3900])
	}
	look := prompts.ListingLook()
	return map[string]any{
		"goal":         look.Rule,
		"design_goals": []string{sourceNoteDesignGoalPrefix + text},
		"prohibitions": append([]string{}, look.BriefProhibitions...),
	}
}

func BuildProductSourceCreateGraph(productTitle, sourceProductID string, factSetVersionID *string) (ChangeSet, error) {
	var fact any
	if factSetVersionID != nil {
		fact = *factSetVersionID
	}
	cs := ChangeSet{
		BaseGraphRevision: 0,
		Summary:           "商品资料",
		ActorType:         ActorUser,
		Operations: []Operation{
			CreateNodeOp{
				ClientRef: "product-source",
				NodeType:  NodeProductSource,
				Title:     productTitle,
				PositionX: 80,
				PositionY: 220,
				Config: map[string]any{
					"source_product_id":   sourceProductID,
					"fact_set_version_id": fact,
				},
			},
		},
	}
	return cs, validateChangeSet(cs)
}

func BuildDirectCreateTemplate(in DirectCreateInput) (ChangeSet, error) {
	if len(in.ImageTypes) == 0 {
		return ChangeSet{}, apperr.Validation("至少选择一种图片类型")
	}
	if len(in.ReferenceAssetIDs) == 0 {
		return ChangeSet{}, apperr.Validation("至少上传一张参考图")
	}
	seenAssets := map[string]struct{}{}
	for _, id := range in.ReferenceAssetIDs {
		if _, ok := seenAssets[id]; ok {
			return ChangeSet{}, apperr.Validation("参考图资产不能重复")
		}
		seenAssets[id] = struct{}{}
	}
	if len(in.ReferenceAssetIDs) > maxReferenceAssets {
		return ChangeSet{}, apperr.Validation(fmt.Sprintf("参考图不能超过 %d 张", maxReferenceAssets))
	}
	seenTypes := map[string]struct{}{}
	for _, item := range in.ImageTypes {
		if _, ok := seenTypes[item.Key]; ok {
			return ChangeSet{}, apperr.Validation("图片类型不能重复")
		}
		seenTypes[item.Key] = struct{}{}
	}
	var generating []DirectCreateImageType
	var evidence []DirectCreateImageType
	for _, item := range in.ImageTypes {
		if imageTypeFamily(item.Key) == "evidence" {
			evidence = append(evidence, item)
			continue
		}
		generating = append(generating, item)
	}
	totalImages := 0
	for _, item := range generating {
		if item.Quantity < minImagePerType || item.Quantity > maxImagePerType {
			return ChangeSet{}, apperr.Validation(fmt.Sprintf("每种图片数量必须在 %d 到 %d 之间", minImagePerType, maxImagePerType))
		}
		totalImages += item.Quantity
	}
	if totalImages > maxTotalImages {
		return ChangeSet{}, apperr.Validation(fmt.Sprintf("图片生成总数不能超过 %d", maxTotalImages))
	}

	productTitle := in.ProductTitle
	if productTitle == "" {
		productTitle = "商品资料"
	}
	var sourceProduct any
	if in.SourceProductID != nil {
		sourceProduct = *in.SourceProductID
	}
	var factSet any
	if in.FactSetVersionID != nil {
		factSet = *in.FactSetVersionID
	}

	ops := []Operation{
		CreateNodeOp{
			ClientRef: "product-source",
			NodeType:  NodeProductSource,
			Title:     productTitle,
			PositionX: 80,
			PositionY: 220,
			Config: map[string]any{
				"source_product_id":   sourceProduct,
				"fact_set_version_id": factSet,
			},
		},
		CreateNodeOp{
			ClientRef: "visual-system",
			NodeType:  NodeVisualSystem,
			Title:     "视觉规范",
			PositionX: 80,
			PositionY: 40,
			Config:    map[string]any{},
		},
		CreateNodeOp{
			ClientRef: "creative-brief",
			NodeType:  NodeCreativeBrief,
			Title:     "创作要求",
			PositionX: 80,
			PositionY: 400,
			Config:    creativeBriefConfigFromSourceNote(in.SourceNote),
		},
	}

	identityRefs := make([]string, len(in.ReferenceAssetIDs))
	for index, assetID := range in.ReferenceAssetIDs {
		ref := fmt.Sprintf("image-asset-%d", index+1)
		identityRefs[index] = ref
		aid := assetID
		ops = append(ops, CreateNodeOp{
			ClientRef:    ref,
			NodeType:     NodeImageAsset,
			Title:        fmt.Sprintf("参考图 %d", index+1),
			PositionX:    80 + index*220,
			PositionY:    560,
			Config:       map[string]any{"role": "product_identity"},
			BoundAssetID: &aid,
		})
	}

	sort.SliceStable(generating, func(i, j int) bool {
		if generating[i].Order != generating[j].Order {
			return generating[i].Order < generating[j].Order
		}
		return generating[i].Key < generating[j].Key
	})
	sort.SliceStable(evidence, func(i, j int) bool {
		if evidence[i].Order != evidence[j].Order {
			return evidence[i].Order < evidence[j].Order
		}
		return evidence[i].Key < evidence[j].Key
	})

	var processingRefs []string
	var promptRefs []string
	for typeIndex, imageType := range generating {
		typeTitle := imageType.Title
		if typeTitle == "" {
			if catalogTitle, ok := imageTypeTitles[imageType.Key]; ok {
				typeTitle = catalogTitle
			} else {
				typeTitle = imageType.Key
			}
		}
		_, typeOption := imageTypeByKey[imageType.Key]
		groupRef := "shot-" + imageType.Key
		promptRef := "prompt-" + imageType.Key
		promptRefs = append(promptRefs, promptRef)
		processingRefs = append(processingRefs, promptRef)
		promptGoal := typeTitle
		if typeOption {
			promptGoal = imageTypePromptGoal(imageType.Key)
		}
		groupY := 40 + typeIndex*280
		ops = append(ops, CreateGroupOp{ClientRef: groupRef, Title: typeTitle, MemberRefs: []string{}})
		groupRefCopy := groupRef
		ops = append(ops, CreateNodeOp{
			ClientRef: promptRef,
			NodeType:  NodePromptGeneration,
			Title:     typeTitle + "提示词",
			PositionX: 420,
			PositionY: groupY,
			GroupRef:  &groupRefCopy,
			Config: map[string]any{
				"image_type_key": imageType.Key,
				"prompt":         map[string]any{"design_goal": promptGoal},
			},
		})
		for assetIndex, identityRef := range identityRefs {
			ops = append(ops, ConnectNodesOp{
				ClientRef: fmt.Sprintf("edge-ref-%d-%s", assetIndex+1, promptRef),
				SourceRef: identityRef,
				TargetRef: promptRef,
				Order:     assetIndex,
			})
		}
		typeGenerationSpec, err := generationSpecForShot(imageType, in.GenerationSpec)
		if err != nil {
			return ChangeSet{}, err
		}
		for imageIndex := 0; imageIndex < imageType.Quantity; imageIndex++ {
			imageRef := fmt.Sprintf("image-%s-%d", imageType.Key, imageIndex+1)
			processingRefs = append(processingRefs, imageRef)
			imageConfig := map[string]any{
				"image_type_key":  imageType.Key,
				"generation_spec": cloneMap(typeGenerationSpec),
			}
			if in.DeliverySpec != nil {
				imageConfig["delivery_spec"] = cloneMap(in.DeliverySpec)
			}
			shotGroup := groupRef
			ops = append(ops, CreateNodeOp{
				ClientRef: imageRef,
				NodeType:  NodeImageGeneration,
				Title:     fmt.Sprintf("%s %d", typeTitle, imageIndex+1),
				PositionX: 760,
				PositionY: groupY + imageIndex*90,
				GroupRef:  &shotGroup,
				Config:    imageConfig,
			})
			ops = append(ops, ConnectNodesOp{
				ClientRef: fmt.Sprintf("edge-prompt-%s-%d", imageType.Key, imageIndex+1),
				SourceRef: promptRef,
				TargetRef: imageRef,
				Order:     imageIndex,
			})
			for assetIndex, identityRef := range identityRefs {
				ops = append(ops, ConnectNodesOp{
					ClientRef: fmt.Sprintf("edge-ref-%d-%s", assetIndex+1, imageRef),
					SourceRef: identityRef,
					TargetRef: imageRef,
					Order:     assetIndex,
				})
			}
		}
	}

	for evidenceIndex, imageType := range evidence {
		typeTitle := imageType.Title
		if typeTitle == "" {
			if catalogTitle, ok := imageTypeTitles[imageType.Key]; ok {
				typeTitle = catalogTitle
			} else {
				typeTitle = imageType.Key
			}
		}
		ops = append(ops, CreateNodeOp{
			ClientRef: "evidence-" + imageType.Key,
			NodeType:  NodeImageAsset,
			Title:     typeTitle + "（待绑定）",
			PositionX: 80 + evidenceIndex*220,
			PositionY: 760,
			Config:    map[string]any{"role": "evidence"},
		})
	}

	for order, promptRef := range promptRefs {
		ops = append(ops, ConnectNodesOp{
			ClientRef: "edge-facts-" + promptRef,
			SourceRef: "product-source",
			TargetRef: promptRef,
			Order:     order,
		})
		ops = append(ops, ConnectNodesOp{
			ClientRef: "edge-brief-" + promptRef,
			SourceRef: "creative-brief",
			TargetRef: promptRef,
			Order:     order,
		})
	}
	ops = append(ops, ConnectNodesOp{
		ClientRef: "edge-facts-visual-system",
		SourceRef: "product-source",
		TargetRef: "visual-system",
		Order:     0,
	})
	ops = append(ops, ConnectNodesOp{
		ClientRef: "edge-facts-creative-brief",
		SourceRef: "product-source",
		TargetRef: "creative-brief",
		Order:     0,
	})
	for assetIndex, identityRef := range identityRefs {
		ops = append(ops, ConnectNodesOp{
			ClientRef: fmt.Sprintf("edge-ref-%d-visual-system", assetIndex+1),
			SourceRef: identityRef,
			TargetRef: "visual-system",
			Order:     assetIndex,
		})
		ops = append(ops, ConnectNodesOp{
			ClientRef: fmt.Sprintf("edge-ref-%d-creative-brief", assetIndex+1),
			SourceRef: identityRef,
			TargetRef: "creative-brief",
			Order:     assetIndex,
		})
	}
	for order, nodeRef := range processingRefs {
		ops = append(ops, ConnectNodesOp{
			ClientRef: "edge-visual-" + nodeRef,
			SourceRef: "visual-system",
			TargetRef: nodeRef,
			Order:     order,
		})
	}

	cs := ChangeSet{
		BaseGraphRevision: 0,
		Summary:           "直接创建：按构思表单生成预设工作流模版",
		ActorType:         ActorUser,
		Operations:        ops,
	}
	return cs, validateChangeSet(cs)
}

// TemplateForExistingProductSource 把名称-only 出生图扩成与直接创建相同的模板，复用已有商品资料节点。
func TemplateForExistingProductSource(productSourceNodeID string, baseRevision int, in DirectCreateInput) (ChangeSet, error) {
	changeSet, err := BuildDirectCreateTemplate(in)
	if err != nil {
		return ChangeSet{}, err
	}
	ops := make([]Operation, 0, len(changeSet.Operations))
	for _, operation := range changeSet.Operations {
		switch op := operation.(type) {
		case CreateNodeOp:
			if op.ClientRef == "product-source" {
				continue
			}
			ops = append(ops, op)
		case ConnectNodesOp:
			if op.SourceRef == "product-source" {
				op.SourceRef = productSourceNodeID
			}
			if op.TargetRef == "product-source" {
				op.TargetRef = productSourceNodeID
			}
			ops = append(ops, op)
		default:
			ops = append(ops, operation)
		}
	}
	cs := ChangeSet{
		BaseGraphRevision: baseRevision,
		Summary:           "按商品输入补全画布",
		ActorType:         ActorUser,
		Operations:        ops,
	}
	return cs, validateChangeSet(cs)
}

func generationSpecForShot(imageType DirectCreateImageType, generationSpec map[string]any) (map[string]any, error) {
	overrides := cloneMap(generationSpec)
	if imageType.AspectRatio != "" {
		overrides["aspect_ratio"] = imageType.AspectRatio
	}
	hasTextPolicy := false
	if generationSpec != nil {
		_, hasTextPolicy = generationSpec["text_policy"]
	}
	if imageTypeFamily(imageType.Key) == "infographic" && !hasTextPolicy {
		overrides["text_policy"] = "required"
		if _, ok := overrides["text_language"]; !ok {
			overrides["text_language"] = "zh-CN"
		}
	}
	return resolveTemplateGenerationSpec(overrides)
}
