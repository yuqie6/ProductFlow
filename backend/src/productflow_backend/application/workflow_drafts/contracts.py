from __future__ import annotations

import hashlib
import json
from typing import Annotated, Any, Literal

from pydantic import BaseModel, ConfigDict, Field, JsonValue, StringConstraints, model_validator

from productflow_backend.domain.enums import (
    ProductFactSourceType,
    ProductFactStatus,
    WorkflowNodeType,
)
from productflow_backend.domain.workflow_rules import canonical_workflow_edge_handles

WORKFLOW_DRAFT_SCHEMA_VERSION = 1
WORKFLOW_SCHEMA_VERSION = 2
WORKFLOW_DRAFT_MIN_IMAGE_TYPES = 1
WORKFLOW_DRAFT_MIN_IMAGES_PER_TYPE = 1
WORKFLOW_DRAFT_MAX_IMAGES_PER_TYPE = 6
WORKFLOW_DRAFT_MAX_TOTAL_IMAGES = 30
WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS = 6
DELIVERY_SPEC_MAX_TOTAL_PIXELS = 64 * 1024 * 1024

BusinessKey = Annotated[
    str,
    StringConstraints(
        strip_whitespace=True,
        min_length=1,
        max_length=80,
        pattern=r"^[a-z0-9][a-z0-9_-]*$",
    ),
]
NonEmptyText = Annotated[str, StringConstraints(strip_whitespace=True, min_length=1)]
EntityId = Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=36)]
HexColor = Annotated[str, StringConstraints(pattern=r"^#[0-9A-Fa-f]{6}$")]
VisualLockedField = Literal[
    "style",
    "colors",
    "typography",
    "spacing",
    "decorations",
    "photography",
    "quality",
    "product_fidelity",
    "prohibitions",
]


class StrictArtifactModel(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)


class FactConflictCandidate(StrictArtifactModel):
    value: JsonValue
    source_type: ProductFactSourceType
    evidence_asset_ids: list[EntityId] = Field(default_factory=list)


class ProductFactDraft(StrictArtifactModel):
    key: BusinessKey
    value: JsonValue
    source_type: ProductFactSourceType
    status: ProductFactStatus
    requires_confirmation: bool = False
    evidence_asset_ids: list[EntityId] = Field(default_factory=list)
    conflicts: list[FactConflictCandidate] = Field(default_factory=list)

    @model_validator(mode="after")
    def validate_conflict_state(self) -> ProductFactDraft:
        if self.status == ProductFactStatus.CONFLICTED and not self.conflicts:
            raise ValueError("conflicted 商品事实必须包含冲突候选")
        if self.status != ProductFactStatus.CONFLICTED and self.conflicts:
            raise ValueError("只有 conflicted 商品事实可以包含冲突候选")
        if len(self.evidence_asset_ids) != len(set(self.evidence_asset_ids)):
            raise ValueError("商品事实的证据资产不能重复")
        return self


class ReferenceBindingPlan(StrictArtifactModel):
    key: BusinessKey
    asset_id: EntityId
    role: NonEmptyText
    label: NonEmptyText


class VisualColor(StrictArtifactModel):
    role: BusinessKey
    value: HexColor
    label: NonEmptyText


class VisualTypeScale(StrictArtifactModel):
    headline: float = Field(gt=0, le=20)
    subtitle: float = Field(gt=0, le=20)
    body: float = Field(gt=0, le=20)


class VisualTypography(StrictArtifactModel):
    title_font: NonEmptyText
    body_font: NonEmptyText
    scale: VisualTypeScale


class VisualSpacing(StrictArtifactModel):
    min_edge_whitespace_percent: float = Field(ge=0, le=80)
    principles: list[NonEmptyText] = Field(min_length=1)


class VisualDecorations(StrictArtifactModel):
    elements: list[NonEmptyText] = Field(default_factory=list)
    icon_style: NonEmptyText


class VisualPhotography(StrictArtifactModel):
    lighting: NonEmptyText
    depth_of_field: NonEmptyText
    camera_parameters: list[NonEmptyText] = Field(default_factory=list)


class VisualQuality(StrictArtifactModel):
    resolution: NonEmptyText
    commercial_grade: NonEmptyText
    realism: NonEmptyText
    minimum_quality: Literal["draft", "standard", "high"] = "high"


class VisualProductFidelity(StrictArtifactModel):
    preserve_shape: bool = True
    preserve_proportions: bool = True
    preserve_materials: bool = True
    requirements: list[NonEmptyText] = Field(default_factory=list)


class VisualVariant(StrictArtifactModel):
    key: BusinessKey
    title: NonEmptyText
    guidance: list[NonEmptyText] = Field(min_length=1)


class VisualReferenceAsset(StrictArtifactModel):
    asset_id: EntityId
    role: NonEmptyText
    label: NonEmptyText


class VisualSystemDraftPayload(StrictArtifactModel):
    name: Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=255)]
    style: list[NonEmptyText] = Field(min_length=1)
    colors: list[VisualColor] = Field(min_length=1)
    typography: VisualTypography
    spacing: VisualSpacing
    decorations: VisualDecorations
    photography: VisualPhotography
    quality: VisualQuality
    product_fidelity: VisualProductFidelity
    locked_fields: list[VisualLockedField] = Field(min_length=1)
    variants: list[VisualVariant] = Field(default_factory=list)
    prohibitions: list[NonEmptyText] = Field(default_factory=list)
    reference_assets: list[VisualReferenceAsset] = Field(
        default_factory=list,
        max_length=WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS,
    )

    @model_validator(mode="after")
    def validate_unique_values(self) -> VisualSystemDraftPayload:
        _require_unique_values([color.role for color in self.colors], label="视觉颜色 role")
        _require_unique_keys(self.variants, label="视觉变体")
        _require_unique_values(self.locked_fields, label="视觉体系 locked_fields")
        reference_keys = [(reference.asset_id, reference.role) for reference in self.reference_assets]
        if len(reference_keys) != len(set(reference_keys)):
            raise ValueError("视觉参考资产和 role 不能重复")
        return self


class VisualSystemPlan(StrictArtifactModel):
    mode: Literal["draft", "confirmed_version"]
    version_id: EntityId | None = None
    payload: VisualSystemDraftPayload | None = None
    source_markdown: str | None = None

    @model_validator(mode="after")
    def validate_mode_fields(self) -> VisualSystemPlan:
        if self.mode == "draft":
            if not self.payload:
                raise ValueError("draft 视觉体系必须包含 payload")
            if self.version_id is not None:
                raise ValueError("draft 视觉体系不能引用 version_id")
        else:
            if self.version_id is None:
                raise ValueError("confirmed_version 视觉体系必须引用 version_id")
            if self.payload is not None:
                raise ValueError("confirmed_version 视觉体系不能内嵌 draft payload")
            if self.source_markdown is not None:
                raise ValueError("confirmed_version 视觉体系不能重复保存 source_markdown")
        return self


class VisualStyleOverride(StrictArtifactModel):
    field: Literal["style"]
    value: list[NonEmptyText] = Field(min_length=1)


class VisualColorsOverride(StrictArtifactModel):
    field: Literal["colors"]
    value: list[VisualColor] = Field(min_length=1)


class VisualTypographyOverride(StrictArtifactModel):
    field: Literal["typography"]
    value: VisualTypography


class VisualSpacingOverride(StrictArtifactModel):
    field: Literal["spacing"]
    value: VisualSpacing


class VisualDecorationsOverride(StrictArtifactModel):
    field: Literal["decorations"]
    value: VisualDecorations


class VisualPhotographyOverride(StrictArtifactModel):
    field: Literal["photography"]
    value: VisualPhotography


class VisualQualityOverride(StrictArtifactModel):
    field: Literal["quality"]
    value: VisualQuality


class VisualProductFidelityOverride(StrictArtifactModel):
    field: Literal["product_fidelity"]
    value: VisualProductFidelity


class VisualProhibitionsOverride(StrictArtifactModel):
    field: Literal["prohibitions"]
    value: list[NonEmptyText]


VisualFieldOverride = Annotated[
    VisualStyleOverride
    | VisualColorsOverride
    | VisualTypographyOverride
    | VisualSpacingOverride
    | VisualDecorationsOverride
    | VisualPhotographyOverride
    | VisualQualityOverride
    | VisualProductFidelityOverride
    | VisualProhibitionsOverride,
    Field(discriminator="field"),
]


class VisualExceptionScope(StrictArtifactModel):
    type: Literal["workflow", "image_type", "image_plan"]
    key: BusinessKey | None = None

    @model_validator(mode="after")
    def validate_scope_key(self) -> VisualExceptionScope:
        if self.type == "workflow" and self.key is not None:
            raise ValueError("workflow 视觉例外不能指定 scope key")
        if self.type != "workflow" and self.key is None:
            raise ValueError("图片类型或逐图视觉例外必须指定 scope key")
        return self


class VisualExceptionPlan(StrictArtifactModel):
    key: BusinessKey
    scope: VisualExceptionScope
    overrides: list[VisualFieldOverride] = Field(min_length=1)
    reason: NonEmptyText

    @model_validator(mode="after")
    def validate_unique_fields(self) -> VisualExceptionPlan:
        _require_unique_values([override.field for override in self.overrides], label="视觉例外 override field")
        return self


class GenerationSpec(StrictArtifactModel):
    aspect_ratio: Annotated[str, StringConstraints(pattern=r"^[1-9][0-9]{0,2}:[1-9][0-9]{0,2}$")]
    resolution_tier: Literal["standard", "high", "ultra"] = "high"
    quality_intent: Literal["draft", "standard", "high"] = "high"
    reference_fidelity: Literal["low", "medium", "high"] = "high"
    background_intent: Literal["auto", "opaque", "transparent"] = "auto"
    text_policy: Literal["none", "allow", "required"] = "none"
    text_language: Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=80)] | None = None

    @model_validator(mode="after")
    def validate_text_language(self) -> GenerationSpec:
        if self.text_policy == "required" and self.text_language is None:
            raise ValueError("要求图片文字时必须指定 text_language")
        if self.text_policy == "none" and self.text_language is not None:
            raise ValueError("禁止图片文字时不能指定 text_language")
        return self


class DeliverySpec(StrictArtifactModel):
    width: int = Field(ge=1, le=16384)
    height: int = Field(ge=1, le=16384)
    format: Literal["png", "jpeg", "webp"]
    max_byte_size: int | None = Field(default=None, ge=1)
    fit: Literal["contain", "cover"]
    background_color: HexColor | None = None
    crop_anchor: Literal["center", "top", "bottom", "left", "right"] | None = None

    @model_validator(mode="after")
    def validate_fit_options(self) -> DeliverySpec:
        if self.width * self.height > DELIVERY_SPEC_MAX_TOTAL_PIXELS:
            raise ValueError(f"交付规格总像素不能超过 {DELIVERY_SPEC_MAX_TOTAL_PIXELS}")
        if self.fit == "contain" and self.crop_anchor is not None:
            raise ValueError("contain 交付规格不能指定 crop_anchor")
        if self.fit == "cover" and self.background_color is not None:
            raise ValueError("cover 交付规格不能指定 background_color")
        return self


class PlannedImage(StrictArtifactModel):
    key: BusinessKey
    order: int = Field(ge=0)
    variation_instruction: str | None = None
    generation_spec: GenerationSpec
    delivery_spec: DeliverySpec | None = None


class ImageTypePlan(StrictArtifactModel):
    key: BusinessKey
    title: NonEmptyText
    order: int = Field(ge=0)
    quantity: int = Field(
        ge=WORKFLOW_DRAFT_MIN_IMAGES_PER_TYPE,
        le=WORKFLOW_DRAFT_MAX_IMAGES_PER_TYPE,
    )
    prompt_plan_key: BusinessKey
    images: list[PlannedImage] = Field(
        min_length=WORKFLOW_DRAFT_MIN_IMAGES_PER_TYPE,
        max_length=WORKFLOW_DRAFT_MAX_IMAGES_PER_TYPE,
    )

    @model_validator(mode="after")
    def validate_images(self) -> ImageTypePlan:
        if len(self.images) != self.quantity:
            raise ValueError("图片类型 quantity 必须等于逐图计划数量")
        image_keys = [image.key for image in self.images]
        if len(image_keys) != len(set(image_keys)):
            raise ValueError("同一图片类型的逐图计划 key 不能重复")
        image_orders = [image.order for image in self.images]
        if len(image_orders) != len(set(image_orders)):
            raise ValueError("同一图片类型的逐图计划 order 不能重复")
        return self


class PromptProductFidelity(StrictArtifactModel):
    complex_structure: bool
    product_present: bool = True
    picture_in_picture: Literal["none", "allowed", "required"] = "none"
    requirements: list[NonEmptyText] = Field(min_length=1)


class PromptComposition(StrictArtifactModel):
    viewpoint: NonEmptyText
    product_share_percent: float = Field(gt=0, le=100)
    layout: NonEmptyText
    copy_regions: list[NonEmptyText] = Field(default_factory=list)


class PromptContentElements(StrictArtifactModel):
    focus: list[NonEmptyText] = Field(min_length=1)
    selling_points: list[NonEmptyText] = Field(default_factory=list)
    background: NonEmptyText
    decorations: list[NonEmptyText] = Field(default_factory=list)


class PromptTextContent(StrictArtifactModel):
    headline: str | None = None
    subtitle: str | None = None
    body: str | None = None

    @model_validator(mode="after")
    def normalize_blank_text(self) -> PromptTextContent:
        for field_name in ("headline", "subtitle", "body"):
            value = getattr(self, field_name)
            if value is not None and not value.strip():
                object.__setattr__(self, field_name, None)
        return self


class PromptAtmosphere(StrictArtifactModel):
    keywords: list[NonEmptyText] = Field(min_length=1)
    lighting: NonEmptyText


class PerImagePromptPlan(StrictArtifactModel):
    image_plan_key: BusinessKey
    instruction: NonEmptyText
    viewpoint: str | None = None
    composition_adjustments: list[NonEmptyText] = Field(default_factory=list)
    lighting: str | None = None


class ImagePromptPayloadV1(StrictArtifactModel):
    schema_version: Literal[1] = 1
    shared_rules: list[NonEmptyText] = Field(min_length=1)
    design_goal: NonEmptyText
    product_fidelity: PromptProductFidelity
    creative_boundary: list[NonEmptyText] = Field(default_factory=list)
    composition: PromptComposition
    content: PromptContentElements
    text: PromptTextContent
    atmosphere: PromptAtmosphere
    visual_variant_key: BusinessKey | None = None
    fact_keys: list[BusinessKey] = Field(default_factory=list)
    evidence_asset_ids: list[EntityId] = Field(default_factory=list)
    images: list[PerImagePromptPlan] = Field(min_length=1)

    @model_validator(mode="after")
    def validate_unique_values(self) -> ImagePromptPayloadV1:
        _require_unique_values(self.fact_keys, label="提示词 fact_keys")
        _require_unique_values(self.evidence_asset_ids, label="提示词 evidence_asset_ids")
        _require_unique_values([image.image_plan_key for image in self.images], label="提示词逐图计划 key")
        return self


class PromptPlan(StrictArtifactModel):
    key: BusinessKey
    image_type_key: BusinessKey
    title: NonEmptyText
    payload: ImagePromptPayloadV1


class WorkflowFolderPlan(StrictArtifactModel):
    key: BusinessKey
    title: NonEmptyText
    order: int = Field(ge=0)
    position_x: int = Field(default=0, deprecated=True)
    position_y: int = Field(default=0, deprecated=True)
    width: int = Field(default=640, ge=240, le=4000, deprecated=True)
    height: int = Field(default=420, ge=180, le=4000, deprecated=True)


class WorkflowNodePlanBase(StrictArtifactModel):
    key: BusinessKey
    title: Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=255)]
    position_x: int = 0
    position_y: int = 0
    folder_key: BusinessKey | None = None


class ProductContextNodePlan(WorkflowNodePlanBase):
    node_type: Literal[WorkflowNodeType.PRODUCT_CONTEXT] = WorkflowNodeType.PRODUCT_CONTEXT


class ReferenceImageNodePlan(WorkflowNodePlanBase):
    node_type: Literal[WorkflowNodeType.REFERENCE_IMAGE] = WorkflowNodeType.REFERENCE_IMAGE
    reference_key: BusinessKey


class PromptGenerationNodePlan(WorkflowNodePlanBase):
    node_type: Literal[WorkflowNodeType.PROMPT_GENERATION] = WorkflowNodeType.PROMPT_GENERATION
    prompt_plan_key: BusinessKey


class ImageGenerationNodePlan(WorkflowNodePlanBase):
    node_type: Literal[WorkflowNodeType.IMAGE_GENERATION] = WorkflowNodeType.IMAGE_GENERATION
    image_plan_key: BusinessKey


WorkflowNodePlan = Annotated[
    ProductContextNodePlan | ReferenceImageNodePlan | PromptGenerationNodePlan | ImageGenerationNodePlan,
    Field(discriminator="node_type"),
]


class WorkflowEdgePlan(StrictArtifactModel):
    key: BusinessKey
    source_node_key: BusinessKey
    target_node_key: BusinessKey
    source_handle: Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=80)] | None = None
    target_handle: Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=80)] | None = None


class WorkflowDraftPayloadV1(StrictArtifactModel):
    schema_version: Literal[1] = WORKFLOW_DRAFT_SCHEMA_VERSION
    title: Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=255)]
    facts: list[ProductFactDraft] = Field(min_length=1)
    required_fact_keys: list[BusinessKey] = Field(default_factory=list)
    missing_fact_keys: list[BusinessKey] = Field(default_factory=list)
    reference_bindings: list[ReferenceBindingPlan] = Field(
        min_length=1,
        max_length=WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS,
    )
    visual_system: VisualSystemPlan
    visual_exceptions: list[VisualExceptionPlan] = Field(default_factory=list)
    prompt_plans: list[PromptPlan] = Field(min_length=WORKFLOW_DRAFT_MIN_IMAGE_TYPES)
    image_types: list[ImageTypePlan] = Field(min_length=WORKFLOW_DRAFT_MIN_IMAGE_TYPES)
    folders: list[WorkflowFolderPlan] = Field(default_factory=list)
    nodes: list[WorkflowNodePlan] = Field(min_length=4)
    edges: list[WorkflowEdgePlan] = Field(min_length=1)
    confirmation_summary: NonEmptyText

    @model_validator(mode="after")
    def validate_artifact(self) -> WorkflowDraftPayloadV1:
        fact_keys = _require_unique_keys(self.facts, label="商品事实")
        required_fact_keys = _require_unique_values(self.required_fact_keys, label="required_fact_keys")
        missing_fact_keys = _require_unique_values(self.missing_fact_keys, label="missing_fact_keys")
        if not required_fact_keys.issubset(fact_keys | missing_fact_keys):
            raise ValueError("required_fact_keys 必须引用已有或明确缺失的商品事实")
        if not missing_fact_keys.issubset(required_fact_keys):
            raise ValueError("missing_fact_keys 必须属于 required_fact_keys")

        reference_keys = _require_unique_keys(self.reference_bindings, label="参考资产计划")
        reference_by_key = {item.key: item for item in self.reference_bindings}
        prompt_keys = _require_unique_keys(self.prompt_plans, label="提示词计划")
        prompt_by_key = {item.key: item for item in self.prompt_plans}
        image_type_keys = _require_unique_keys(self.image_types, label="图片类型")
        folder_keys = _require_unique_keys(self.folders, label="文件夹")
        node_keys = _require_unique_keys(self.nodes, label="工作流节点")
        _require_unique_keys(self.edges, label="工作流连线")

        if len({item.order for item in self.image_types}) != len(self.image_types):
            raise ValueError("图片类型 order 不能重复")
        if len({item.order for item in self.folders}) != len(self.folders):
            raise ValueError("文件夹 order 不能重复")
        total_images = sum(item.quantity for item in self.image_types)
        if total_images > WORKFLOW_DRAFT_MAX_TOTAL_IMAGES:
            raise ValueError(f"计划图片总数不能超过 {WORKFLOW_DRAFT_MAX_TOTAL_IMAGES}")

        prompt_image_type_keys = [item.image_type_key for item in self.prompt_plans]
        if len(prompt_image_type_keys) != len(set(prompt_image_type_keys)):
            raise ValueError("同一图片类型不能关联多个提示词计划")
        prompt_by_image_type = {item.image_type_key: item for item in self.prompt_plans}
        if set(prompt_by_image_type) != image_type_keys:
            raise ValueError("每个图片类型必须且只能关联一个提示词计划")
        for image_type in self.image_types:
            prompt = prompt_by_image_type[image_type.key]
            if image_type.prompt_plan_key != prompt.key:
                raise ValueError("图片类型 prompt_plan_key 与提示词计划不一致")
            prompt_image_keys = {image.image_plan_key for image in prompt.payload.images}
            image_plan_keys_for_type = {image.key for image in image_type.images}
            if prompt_image_keys != image_plan_keys_for_type:
                raise ValueError("提示词逐图计划必须与图片类型的 image plan 一一对应")
            if not set(prompt.payload.fact_keys).issubset(fact_keys):
                raise ValueError("提示词 fact_keys 必须引用 Draft 已有商品事实")

        if self.visual_system.mode == "draft":
            visual_payload = self.visual_system.payload
            assert visual_payload is not None
            visual_variant_keys = {variant.key for variant in visual_payload.variants}
            for prompt in self.prompt_plans:
                variant_key = prompt.payload.visual_variant_key
                if variant_key is not None and variant_key not in visual_variant_keys:
                    raise ValueError("提示词引用了不存在的视觉变体")

        image_plan_keys: set[str] = set()
        image_plan_type_by_key: dict[str, str] = {}
        for image_type in self.image_types:
            for image in image_type.images:
                if image.key in image_plan_keys:
                    raise ValueError("逐图计划 key 在整个 Draft 内不能重复")
                image_plan_keys.add(image.key)
                image_plan_type_by_key[image.key] = image_type.key

        _require_unique_keys(self.visual_exceptions, label="视觉例外")
        locked_fields = (
            set(self.visual_system.payload.locked_fields)
            if self.visual_system.mode == "draft" and self.visual_system.payload is not None
            else None
        )
        for exception in self.visual_exceptions:
            if exception.scope.type == "image_type" and exception.scope.key not in image_type_keys:
                raise ValueError("视觉例外引用了不存在的图片类型")
            if exception.scope.type == "image_plan" and exception.scope.key not in image_plan_keys:
                raise ValueError("视觉例外引用了不存在的逐图计划")
            if locked_fields is not None and any(
                override.field not in locked_fields for override in exception.overrides
            ):
                raise ValueError("视觉例外只能覆盖 VisualSystem locked_fields")

        product_nodes = [node for node in self.nodes if node.node_type == WorkflowNodeType.PRODUCT_CONTEXT]
        if len(product_nodes) != 1:
            raise ValueError("Draft 必须且只能包含一个 product_context 节点")
        for node in self.nodes:
            if node.folder_key is not None and node.folder_key not in folder_keys:
                raise ValueError("工作流节点引用了不存在的文件夹")
        member_folder_keys = {node.folder_key for node in self.nodes if node.folder_key is not None}
        empty_folder_keys = sorted(folder_keys - member_folder_keys)
        if empty_folder_keys:
            raise ValueError(f"文件夹必须至少包含一个工作流节点: {', '.join(empty_folder_keys)}")

        reference_nodes = [node for node in self.nodes if isinstance(node, ReferenceImageNodePlan)]
        if {node.reference_key for node in reference_nodes} != reference_keys:
            raise ValueError("每个参考资产计划必须且只能关联一个 reference_image 节点")
        if len(reference_nodes) != len(reference_keys):
            raise ValueError("reference_image 节点不能重复绑定参考资产计划")

        prompt_nodes = [node for node in self.nodes if isinstance(node, PromptGenerationNodePlan)]
        if {node.prompt_plan_key for node in prompt_nodes} != prompt_keys:
            raise ValueError("每个提示词计划必须且只能关联一个 prompt_generation 节点")
        if len(prompt_nodes) != len(prompt_keys):
            raise ValueError("prompt_generation 节点不能重复绑定提示词计划")

        image_nodes = [node for node in self.nodes if isinstance(node, ImageGenerationNodePlan)]
        if {node.image_plan_key for node in image_nodes} != image_plan_keys:
            raise ValueError("每张逐图计划必须且只能关联一个 image_generation 节点")
        if len(image_nodes) != len(image_plan_keys):
            raise ValueError("image_generation 节点不能重复绑定逐图计划")

        node_type_by_key = {node.key: node.node_type for node in self.nodes}
        edges_by_pair: set[tuple[str, str]] = set()
        adjacency = {key: set() for key in node_keys}
        indegree = dict.fromkeys(node_keys, 0)
        for edge in self.edges:
            if edge.source_node_key not in node_keys or edge.target_node_key not in node_keys:
                raise ValueError("工作流连线引用了不存在的节点")
            if edge.source_node_key == edge.target_node_key:
                raise ValueError("工作流连线不能连接到自身")
            edge_types = (
                node_type_by_key[edge.source_node_key],
                node_type_by_key[edge.target_node_key],
            )
            if canonical_workflow_edge_handles(*edge_types) is None:
                raise ValueError("工作流连线包含不支持的 v2 节点类型组合")
            pair = (edge.source_node_key, edge.target_node_key)
            if pair in edges_by_pair:
                raise ValueError("相同工作流节点之间不能重复连线")
            edges_by_pair.add(pair)
            adjacency[edge.source_node_key].add(edge.target_node_key)
            indegree[edge.target_node_key] += 1
        _validate_acyclic(adjacency, indegree)

        prompt_node_by_plan = {node.prompt_plan_key: node.key for node in prompt_nodes}
        image_node_by_plan = {node.image_plan_key: node.key for node in image_nodes}
        prompt_plan_by_type = {prompt.image_type_key: prompt.key for prompt in self.prompt_plans}
        product_context_key = product_nodes[0].key
        for prompt_node in prompt_nodes:
            if (product_context_key, prompt_node.key) not in edges_by_pair:
                raise ValueError("每个 prompt_generation 节点必须连接 product_context 节点")
            incoming_reference_asset_ids = {
                reference_by_key[reference_node.reference_key].asset_id
                for reference_node in reference_nodes
                if (reference_node.key, prompt_node.key) in edges_by_pair
            }
            prompt_evidence_asset_ids = set(prompt_by_key[prompt_node.prompt_plan_key].payload.evidence_asset_ids)
            if incoming_reference_asset_ids != prompt_evidence_asset_ids:
                raise ValueError("提示词 evidence_asset_ids 必须与画布直接连接的参考图片一致")
        for image_plan_key, image_type_key in image_plan_type_by_key.items():
            required_pair = (
                prompt_node_by_plan[prompt_plan_by_type[image_type_key]],
                image_node_by_plan[image_plan_key],
            )
            if required_pair not in edges_by_pair:
                raise ValueError("每个 image_generation 节点必须连接对应图片类型的 prompt_generation 节点")
        if len(self.referenced_asset_ids()) > WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS:
            raise ValueError(f"WorkflowDraft 引用的不同图片资产不能超过 {WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS} 张")
        return self

    def referenced_asset_ids(self) -> set[str]:
        asset_ids = {binding.asset_id for binding in self.reference_bindings}
        if self.visual_system.mode == "draft" and self.visual_system.payload is not None:
            asset_ids.update(reference.asset_id for reference in self.visual_system.payload.reference_assets)
        for prompt in self.prompt_plans:
            asset_ids.update(prompt.payload.evidence_asset_ids)
        for fact in self.facts:
            asset_ids.update(fact.evidence_asset_ids)
            for conflict in fact.conflicts:
                asset_ids.update(conflict.evidence_asset_ids)
        return asset_ids


def parse_workflow_draft_payload(payload: WorkflowDraftPayloadV1 | dict[str, Any]) -> WorkflowDraftPayloadV1:
    if isinstance(payload, WorkflowDraftPayloadV1):
        return payload
    return WorkflowDraftPayloadV1.model_validate(payload)


def workflow_draft_tool_schema() -> dict[str, Any]:
    schema = WorkflowDraftPayloadV1.model_json_schema()
    schema["$defs"]["JsonValue"] = {
        "anyOf": [
            {"type": "string"},
            {"type": "number"},
            {"type": "boolean"},
            {"type": "array", "items": {"$ref": "#/$defs/JsonValue"}},
            {"type": "null"},
        ]
    }
    normalize_tool_schema(schema)
    return schema


def normalize_tool_schema(schema: dict[str, Any]) -> None:
    schema.pop("default", None)
    schema.pop("deprecated", None)
    schema.pop("discriminator", None)

    one_of = schema.pop("oneOf", None)
    if one_of is not None:
        if "anyOf" in schema:
            raise ValueError("工作流草稿工具 Schema 不能同时包含 oneOf 和 anyOf")
        schema["anyOf"] = one_of

    properties = schema.get("properties")
    if properties is not None:
        if not isinstance(properties, dict):
            raise TypeError("工作流草稿工具 Schema properties 必须是对象")
        schema["required"] = list(properties)
        schema["additionalProperties"] = False
        for property_schema in properties.values():
            normalize_tool_schema(property_schema)
    elif schema.get("type") == "object":
        raise ValueError("工作流草稿工具 Schema 不允许开放对象")

    definitions = schema.get("$defs")
    if definitions is not None:
        if not isinstance(definitions, dict):
            raise TypeError("工作流草稿工具 Schema $defs 必须是对象")
        for definition in definitions.values():
            normalize_tool_schema(definition)

    items = schema.get("items")
    if isinstance(items, dict):
        normalize_tool_schema(items)

    any_of = schema.get("anyOf")
    if any_of is not None:
        if not isinstance(any_of, list):
            raise TypeError("工作流草稿工具 Schema anyOf 必须是数组")
        for variant in any_of:
            if not isinstance(variant, dict):
                raise TypeError("工作流草稿工具 Schema anyOf 分支必须是对象")
            normalize_tool_schema(variant)


def workflow_draft_payload_dict(payload: WorkflowDraftPayloadV1 | dict[str, Any]) -> dict[str, Any]:
    return parse_workflow_draft_payload(payload).model_dump(mode="json")


def workflow_draft_payload_hash(payload: WorkflowDraftPayloadV1 | dict[str, Any]) -> str:
    canonical = json.dumps(
        workflow_draft_payload_dict(payload),
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")
    return hashlib.sha256(canonical).hexdigest()


def _require_unique_keys(items: list[Any], *, label: str) -> set[str]:
    keys = [item.key for item in items]
    if len(keys) != len(set(keys)):
        raise ValueError(f"{label} key 不能重复")
    return set(keys)


def _require_unique_values(values: list[str], *, label: str) -> set[str]:
    if len(values) != len(set(values)):
        raise ValueError(f"{label} 不能重复")
    return set(values)


def _validate_acyclic(adjacency: dict[str, set[str]], indegree: dict[str, int]) -> None:
    ready = sorted(key for key, degree in indegree.items() if degree == 0)
    visited = 0
    while ready:
        current = ready.pop(0)
        visited += 1
        for target in sorted(adjacency[current]):
            indegree[target] -= 1
            if indegree[target] == 0:
                ready.append(target)
                ready.sort()
    if visited != len(indegree):
        raise ValueError("工作流不能包含循环依赖")
