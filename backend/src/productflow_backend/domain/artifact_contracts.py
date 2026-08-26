"""Database-free artifact contracts shared by application and infrastructure."""

from __future__ import annotations

from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field, JsonValue, StringConstraints, model_validator

from productflow_backend.domain.enums import ProductFactSourceType, ProductFactStatus

PRODUCT_INTAKE_MIN_IMAGE_TYPES = 1
PRODUCT_INTAKE_MIN_IMAGES_PER_TYPE = 1
PRODUCT_INTAKE_MAX_IMAGES_PER_TYPE = 6
PRODUCT_INTAKE_MAX_TOTAL_IMAGES = 30
PRODUCT_INTAKE_MAX_REFERENCE_ASSETS = 6

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
        max_length=PRODUCT_INTAKE_MAX_REFERENCE_ASSETS,
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


def _require_unique_keys(items: list[object], *, label: str) -> set[str]:
    keys = [item.key for item in items]
    if len(keys) != len(set(keys)):
        raise ValueError(f"{label} key 不能重复")
    return set(keys)


def _require_unique_values(values: list[str], *, label: str) -> set[str]:
    if len(values) != len(set(values)):
        raise ValueError(f"{label} 不能重复")
    return set(values)


__all__ = [
    "PRODUCT_INTAKE_MAX_IMAGES_PER_TYPE",
    "PRODUCT_INTAKE_MAX_REFERENCE_ASSETS",
    "PRODUCT_INTAKE_MAX_TOTAL_IMAGES",
    "PRODUCT_INTAKE_MIN_IMAGE_TYPES",
    "PRODUCT_INTAKE_MIN_IMAGES_PER_TYPE",
    "BusinessKey",
    "EntityId",
    "FactConflictCandidate",
    "HexColor",
    "ImagePromptPayloadV1",
    "NonEmptyText",
    "PerImagePromptPlan",
    "ProductFactDraft",
    "PromptAtmosphere",
    "PromptComposition",
    "PromptContentElements",
    "PromptProductFidelity",
    "PromptTextContent",
    "StrictArtifactModel",
    "VisualColor",
    "VisualColorsOverride",
    "VisualDecorations",
    "VisualDecorationsOverride",
    "VisualExceptionPlan",
    "VisualExceptionScope",
    "VisualFieldOverride",
    "VisualLockedField",
    "VisualPhotography",
    "VisualPhotographyOverride",
    "VisualProductFidelity",
    "VisualProductFidelityOverride",
    "VisualProhibitionsOverride",
    "VisualQuality",
    "VisualQualityOverride",
    "VisualReferenceAsset",
    "VisualSpacing",
    "VisualSpacingOverride",
    "VisualStyleOverride",
    "VisualSystemDraftPayload",
    "VisualTypeScale",
    "VisualTypography",
    "VisualTypographyOverride",
    "VisualVariant",
]
