from __future__ import annotations

import hashlib
import json
from typing import Annotated, Any, Literal

from pydantic import BaseModel, ConfigDict, Field, StringConstraints, model_validator

from productflow_backend.application.workflow_drafts.contracts import (
    DeliverySpec,
    GenerationSpec,
    VisualLockedField,
)

RECIPE_SCHEMA_VERSION = 1
RECIPE_MAX_NODES = 128
RECIPE_MAX_EDGES = 256
RECIPE_MAX_FOLDERS = 32
RECIPE_MAX_JSON_BYTES = 512 * 1024

RecipeKey = Annotated[
    str,
    StringConstraints(
        strip_whitespace=True,
        min_length=1,
        max_length=80,
        pattern=r"^[a-z0-9][a-z0-9_-]*$",
    ),
]
RecipeText = Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=255)]
RecipeNodeType = Literal[
    "product_context",
    "reference_image",
    "prompt_generation",
    "image_generation",
]


class StrictRecipeModel(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)


class RecipeFolder(StrictRecipeModel):
    key: RecipeKey
    title: RecipeText
    order: int = Field(ge=0)


class RecipeNodeBase(StrictRecipeModel):
    key: RecipeKey
    position_x: int
    position_y: int
    folder_key: RecipeKey | None = None


class RecipeProductContextNode(RecipeNodeBase):
    node_type: Literal["product_context"] = "product_context"


class RecipeReferenceImageNode(RecipeNodeBase):
    node_type: Literal["reference_image"] = "reference_image"
    reference_requirement_key: RecipeKey


class RecipePromptGenerationNode(RecipeNodeBase):
    node_type: Literal["prompt_generation"] = "prompt_generation"
    image_type_key: RecipeKey


class RecipeImageGenerationNode(RecipeNodeBase):
    node_type: Literal["image_generation"] = "image_generation"
    image_type_key: RecipeKey
    image_plan_key: RecipeKey


RecipeNode = Annotated[
    RecipeProductContextNode
    | RecipeReferenceImageNode
    | RecipePromptGenerationNode
    | RecipeImageGenerationNode,
    Field(discriminator="node_type"),
]


class RecipeEdge(StrictRecipeModel):
    key: RecipeKey
    source_node_key: RecipeKey
    target_node_key: RecipeKey
    source_handle: Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=80)] | None = None
    target_handle: Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=80)] | None = None


class RecipeBoundaryRequirement(StrictRecipeModel):
    key: RecipeKey
    direction: Literal["inbound", "outbound"]
    local_node_key: RecipeKey
    external_node_type: RecipeNodeType
    role: RecipeText


class RecipeReferenceRequirement(StrictRecipeModel):
    key: RecipeKey
    role: RecipeText
    label: RecipeText
    required: bool = True


class RecipePlannedImage(StrictRecipeModel):
    key: RecipeKey
    order: int = Field(ge=0)
    generation_spec: GenerationSpec
    delivery_spec: DeliverySpec | None = None


class RecipeImageType(StrictRecipeModel):
    key: RecipeKey
    title: RecipeText
    order: int = Field(ge=0)
    default_quantity: int = Field(ge=1, le=6)
    images: list[RecipePlannedImage] = Field(min_length=1, max_length=6)

    @model_validator(mode="after")
    def validate_images(self) -> RecipeImageType:
        if self.default_quantity != len(self.images):
            raise ValueError("配方图片类型默认数量必须等于图片计划数量")
        _require_unique([image.key for image in self.images], label="配方图片计划 key")
        _require_unique([image.order for image in self.images], label="配方图片计划 order")
        return self


class RecipePerImagePromptSlots(StrictRecipeModel):
    image_plan_key: RecipeKey
    viewpoint: bool
    composition_adjustments: bool
    lighting: bool


class RecipePromptShape(StrictRecipeModel):
    image_type_key: RecipeKey
    product_present: bool
    picture_in_picture: Literal["none", "allowed", "required"]
    product_share_percent: float = Field(gt=0, le=100)
    text_slots: list[Literal["headline", "subtitle", "body"]] = Field(default_factory=list)
    fact_keys: list[RecipeKey] = Field(default_factory=list)
    visual_variant_key: RecipeKey | None = None
    per_image_slots: list[RecipePerImagePromptSlots] = Field(min_length=1, max_length=6)

    @model_validator(mode="after")
    def validate_unique_slots(self) -> RecipePromptShape:
        _require_unique(self.text_slots, label="配方提示词文字槽位")
        _require_unique(self.fact_keys, label="配方提示词事实 key")
        _require_unique(
            [slots.image_plan_key for slots in self.per_image_slots],
            label="配方提示词逐图槽位",
        )
        return self


class RecipeVisualRequirements(StrictRecipeModel):
    required_locked_fields: list[VisualLockedField] = Field(min_length=1)
    required_variant_keys: list[RecipeKey] = Field(default_factory=list)

    @model_validator(mode="after")
    def validate_unique_requirements(self) -> RecipeVisualRequirements:
        _require_unique(self.required_locked_fields, label="配方视觉 locked field")
        _require_unique(self.required_variant_keys, label="配方视觉 variant key")
        return self


class RecipePayloadV1(StrictRecipeModel):
    schema_version: Literal[1] = RECIPE_SCHEMA_VERSION
    folders: list[RecipeFolder] = Field(default_factory=list, max_length=RECIPE_MAX_FOLDERS)
    nodes: list[RecipeNode] = Field(min_length=1, max_length=RECIPE_MAX_NODES)
    edges: list[RecipeEdge] = Field(default_factory=list, max_length=RECIPE_MAX_EDGES)
    boundary_requirements: list[RecipeBoundaryRequirement] = Field(default_factory=list)
    reference_requirements: list[RecipeReferenceRequirement] = Field(default_factory=list)
    image_types: list[RecipeImageType] = Field(default_factory=list)
    prompt_shapes: list[RecipePromptShape] = Field(default_factory=list)
    visual_requirements: RecipeVisualRequirements

    @model_validator(mode="after")
    def validate_graph(self) -> RecipePayloadV1:
        folder_keys = _require_unique([folder.key for folder in self.folders], label="配方文件夹 key")
        _require_unique([folder.order for folder in self.folders], label="配方文件夹 order")
        node_keys = _require_unique([node.key for node in self.nodes], label="配方节点 key")
        _require_unique([edge.key for edge in self.edges], label="配方连线 key")
        _require_unique(
            [boundary.key for boundary in self.boundary_requirements],
            label="配方边界需求 key",
        )
        reference_keys = _require_unique(
            [reference.key for reference in self.reference_requirements],
            label="配方参考需求 key",
        )
        image_type_keys = _require_unique(
            [image_type.key for image_type in self.image_types],
            label="配方图片类型 key",
        )
        _require_unique([image_type.order for image_type in self.image_types], label="配方图片类型 order")
        prompt_shape_keys = _require_unique(
            [shape.image_type_key for shape in self.prompt_shapes],
            label="配方提示词形状 image type",
        )
        image_plan_keys = {
            image.key
            for image_type in self.image_types
            for image in image_type.images
        }
        if len(image_plan_keys) != sum(len(image_type.images) for image_type in self.image_types):
            raise ValueError("配方图片计划 key 在所有图片类型中必须唯一")

        member_folder_keys: set[str] = set()
        for node in self.nodes:
            if node.folder_key is not None:
                if node.folder_key not in folder_keys:
                    raise ValueError("配方节点引用了不存在的文件夹")
                member_folder_keys.add(node.folder_key)
            if isinstance(node, RecipeReferenceImageNode):
                if node.reference_requirement_key not in reference_keys:
                    raise ValueError("配方参考图节点引用了不存在的参考需求")
            if isinstance(node, (RecipePromptGenerationNode, RecipeImageGenerationNode)):
                if node.image_type_key not in image_type_keys:
                    raise ValueError("配方生成节点引用了不存在的图片类型")
            if isinstance(node, RecipeImageGenerationNode) and node.image_plan_key not in image_plan_keys:
                raise ValueError("配方图片节点引用了不存在的图片计划")
        if member_folder_keys != folder_keys:
            raise ValueError("配方文件夹必须至少包含一个节点")
        if prompt_shape_keys != image_type_keys:
            raise ValueError("每个配方图片类型必须且只能有一个提示词形状")
        image_type_by_key = {image_type.key: image_type for image_type in self.image_types}
        for shape in self.prompt_shapes:
            if {slot.image_plan_key for slot in shape.per_image_slots} != {
                image.key for image in image_type_by_key[shape.image_type_key].images
            }:
                raise ValueError("配方提示词逐图槽位必须与图片计划一一对应")

        pairs: set[tuple[str, str]] = set()
        for edge in self.edges:
            if edge.source_node_key not in node_keys or edge.target_node_key not in node_keys:
                raise ValueError("配方连线引用了不存在的节点")
            if edge.source_node_key == edge.target_node_key:
                raise ValueError("配方连线不能连接节点自身")
            pair = (edge.source_node_key, edge.target_node_key)
            if pair in pairs:
                raise ValueError("配方节点之间不能重复连线")
            pairs.add(pair)
        for boundary in self.boundary_requirements:
            if boundary.local_node_key not in node_keys:
                raise ValueError("配方边界需求引用了不存在的本地节点")
        return self


def recipe_payload_dict(payload: RecipePayloadV1 | dict[str, Any]) -> dict[str, Any]:
    parsed = payload if isinstance(payload, RecipePayloadV1) else RecipePayloadV1.model_validate(payload)
    return parsed.model_dump(mode="json")


def recipe_payload_json(payload: RecipePayloadV1 | dict[str, Any]) -> bytes:
    encoded = json.dumps(
        recipe_payload_dict(payload),
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")
    if len(encoded) > RECIPE_MAX_JSON_BYTES:
        raise ValueError(f"配方 JSON 不能超过 {RECIPE_MAX_JSON_BYTES} 字节")
    return encoded


def recipe_payload_hash(payload: RecipePayloadV1 | dict[str, Any]) -> str:
    return hashlib.sha256(recipe_payload_json(payload)).hexdigest()


def _require_unique(values: list[Any], *, label: str) -> set[Any]:
    if len(values) != len(set(values)):
        raise ValueError(f"{label} 不能重复")
    return set(values)


__all__ = [
    "RECIPE_MAX_EDGES",
    "RECIPE_MAX_FOLDERS",
    "RECIPE_MAX_JSON_BYTES",
    "RECIPE_MAX_NODES",
    "RecipeBoundaryRequirement",
    "RecipeEdge",
    "RecipeFolder",
    "RecipeImageGenerationNode",
    "RecipeImageType",
    "RecipePayloadV1",
    "RecipePlannedImage",
    "RecipeProductContextNode",
    "RecipePromptGenerationNode",
    "RecipePromptShape",
    "RecipeReferenceImageNode",
    "RecipeReferenceRequirement",
    "RecipeVisualRequirements",
    "recipe_payload_dict",
    "recipe_payload_hash",
    "recipe_payload_json",
]
