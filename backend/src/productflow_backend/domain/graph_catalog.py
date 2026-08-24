"""schema-v3 Node Catalog：输出类型、可接受输入、基数、运行必需性和可编辑配置字段。"""

from __future__ import annotations

from copy import deepcopy
from dataclasses import dataclass, field, replace
from math import isfinite
from typing import Any, Literal

from pydantic import ValidationError

from productflow_backend.domain.enums import GraphEdgeDataType, GraphEdgeRole, GraphNodeType
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.domain.image_specs import DeliverySpec, GenerationSpec

GraphNodeKind = Literal["source", "processing"]
GraphConfigValueKind = Literal[
    "string",
    "string_or_null",
    "string_list",
    "boolean",
    "number",
    "number_or_null",
    "object",
    "object_or_null",
    "object_list",
]
GraphConfigControl = Literal[
    "text",
    "textarea",
    "string_list",
    "select",
    "checkbox",
    "number",
    "aspect_ratio",
    "optional_object",
    "visual_background",
    "hidden",
    "group",
]
GraphConfigVisibleWhenOp = Literal["in"]

GRAPH_CATALOG_VERSION = 5

IMAGE_ASSET_ROLES = ("product_identity", "environment", "style", "evidence")

FORBIDDEN_GRAPH_CONFIG_KEYS = frozenset(
    {
        "prompt_plan_key",
        "image_plan_key",
        "prompt_plan_keys",
        "image_plan_keys",
    }
)

_DEFAULT_GENERATION_SPEC: dict[str, object] = {
    "aspect_ratio": "1:1",
    "resolution_tier": "high",
    "quality_intent": "high",
    "reference_fidelity": "high",
    "background_intent": "auto",
    "text_policy": "none",
    "text_language": None,
}

_DEFAULT_DELIVERY_SPEC: dict[str, object] = {
    "width": 1200,
    "height": 1200,
    "format": "png",
    "max_byte_size": None,
    "fit": "contain",
    "background_color": None,
    "crop_anchor": None,
}


@dataclass(frozen=True, slots=True)
class GraphInputContract:
    data_type: GraphEdgeDataType
    role: GraphEdgeRole
    max_count: int | None
    required_to_run: bool


@dataclass(frozen=True, slots=True)
class GraphConfigVisibleWhen:
    field: str
    op: GraphConfigVisibleWhenOp
    values: tuple[str, ...]


@dataclass(frozen=True, slots=True)
class GraphConfigFieldDocument:
    key: str
    value_kind: GraphConfigValueKind
    control: GraphConfigControl
    required: bool = False
    label_key: str = ""
    hint_key: str = ""
    toggle_label_key: str = ""
    choices: tuple[str, ...] = ()
    min_value: int | None = None
    max_value: int | None = None
    max_length: int | None = None
    default: object | None = None
    panel: str | None = None
    visible_when: GraphConfigVisibleWhen | None = None
    fields: tuple[GraphConfigFieldDocument, ...] = field(default_factory=tuple)


def _field(
    key: str,
    value_kind: GraphConfigValueKind,
    *,
    control: GraphConfigControl | None = None,
    required: bool = False,
    label_key: str = "",
    hint_key: str = "",
    toggle_label_key: str = "",
    choices: tuple[str, ...] = (),
    min_value: int | None = None,
    max_value: int | None = None,
    max_length: int | None = None,
    default: object | None = None,
    panel: str | None = None,
    visible_when: GraphConfigVisibleWhen | None = None,
    fields: tuple[GraphConfigFieldDocument, ...] = (),
) -> GraphConfigFieldDocument:
    resolved = control
    if resolved is None:
        if value_kind == "boolean":
            resolved = "checkbox"
        elif value_kind == "number":
            resolved = "number"
        elif value_kind == "string_list":
            resolved = "string_list"
        elif value_kind in {"object", "object_or_null"}:
            resolved = "optional_object" if value_kind == "object_or_null" else "group"
        else:
            resolved = "text"
    return GraphConfigFieldDocument(
        key=key,
        value_kind=value_kind,
        control=resolved,
        required=required,
        label_key=label_key,
        hint_key=hint_key,
        toggle_label_key=toggle_label_key,
        choices=choices,
        min_value=min_value,
        max_value=max_value,
        max_length=max_length,
        default=default,
        panel=panel,
        visible_when=visible_when,
        fields=fields,
    )


def _hidden(key: str, value_kind: GraphConfigValueKind) -> GraphConfigFieldDocument:
    return _field(key, value_kind, control="hidden")


_OUTPUT_TYPE: dict[GraphNodeType, GraphEdgeDataType] = {
    GraphNodeType.PRODUCT_SOURCE: GraphEdgeDataType.PRODUCT_FACTS,
    GraphNodeType.IMAGE_ASSET: GraphEdgeDataType.IMAGE_ASSET,
    GraphNodeType.CREATIVE_BRIEF: GraphEdgeDataType.CREATIVE_BRIEF,
    GraphNodeType.VISUAL_SYSTEM: GraphEdgeDataType.VISUAL_SYSTEM,
    GraphNodeType.PROMPT_GENERATION: GraphEdgeDataType.PROMPT,
    GraphNodeType.IMAGE_GENERATION: GraphEdgeDataType.IMAGE_ASSET,
}

_ACCEPTANCE: dict[tuple[GraphEdgeDataType, GraphNodeType], GraphInputContract] = {
    (GraphEdgeDataType.PRODUCT_FACTS, GraphNodeType.CREATIVE_BRIEF): GraphInputContract(
        GraphEdgeDataType.PRODUCT_FACTS, GraphEdgeRole.FACTS, 1, False
    ),
    (GraphEdgeDataType.IMAGE_ASSET, GraphNodeType.CREATIVE_BRIEF): GraphInputContract(
        GraphEdgeDataType.IMAGE_ASSET, GraphEdgeRole.REFERENCE, None, False
    ),
    (GraphEdgeDataType.PRODUCT_FACTS, GraphNodeType.VISUAL_SYSTEM): GraphInputContract(
        GraphEdgeDataType.PRODUCT_FACTS, GraphEdgeRole.FACTS, 1, False
    ),
    (GraphEdgeDataType.IMAGE_ASSET, GraphNodeType.VISUAL_SYSTEM): GraphInputContract(
        GraphEdgeDataType.IMAGE_ASSET, GraphEdgeRole.REFERENCE, None, False
    ),
    (GraphEdgeDataType.PRODUCT_FACTS, GraphNodeType.PROMPT_GENERATION): GraphInputContract(
        GraphEdgeDataType.PRODUCT_FACTS, GraphEdgeRole.FACTS, None, False
    ),
    (GraphEdgeDataType.IMAGE_ASSET, GraphNodeType.PROMPT_GENERATION): GraphInputContract(
        GraphEdgeDataType.IMAGE_ASSET, GraphEdgeRole.REFERENCE, None, False
    ),
    (GraphEdgeDataType.CREATIVE_BRIEF, GraphNodeType.PROMPT_GENERATION): GraphInputContract(
        GraphEdgeDataType.CREATIVE_BRIEF, GraphEdgeRole.BRIEF, None, False
    ),
    (GraphEdgeDataType.VISUAL_SYSTEM, GraphNodeType.PROMPT_GENERATION): GraphInputContract(
        GraphEdgeDataType.VISUAL_SYSTEM, GraphEdgeRole.VISUAL_GUIDANCE, 1, False
    ),
    (GraphEdgeDataType.IMAGE_ASSET, GraphNodeType.IMAGE_GENERATION): GraphInputContract(
        GraphEdgeDataType.IMAGE_ASSET, GraphEdgeRole.REFERENCE, None, True
    ),
    (GraphEdgeDataType.VISUAL_SYSTEM, GraphNodeType.IMAGE_GENERATION): GraphInputContract(
        GraphEdgeDataType.VISUAL_SYSTEM, GraphEdgeRole.VISUAL_GUIDANCE, 1, False
    ),
    (GraphEdgeDataType.PROMPT, GraphNodeType.IMAGE_GENERATION): GraphInputContract(
        GraphEdgeDataType.PROMPT, GraphEdgeRole.PROMPT, 1, True
    ),
}

_VISUAL_OVERLAY_FIELDS = (
    _field("style", "string_list", label_key="graph.inspector.visualStyle"),
    _field(
        "colors",
        "object_list",
        control="visual_background",
        label_key="graph.inspector.visualBackground",
        max_length=32,
        fields=(
            _field("role", "string", max_length=80),
            _field("value", "string", max_length=7),
            _field("label", "string", max_length=255),
        ),
    ),
    _field("prohibitions", "string_list", label_key="workflowConfirmation.creativeBoundary"),
)
VISUAL_OVERLAY_FIELD_KEYS = frozenset(item.key for item in _VISUAL_OVERLAY_FIELDS)


def catalog_visual_overlay(overlay: dict[str, Any] | None) -> dict[str, Any] | None:
    if not isinstance(overlay, dict) or not overlay:
        return None
    filtered = {key: value for key, value in overlay.items() if key in VISUAL_OVERLAY_FIELD_KEYS}
    return filtered or None


_PROMPT_FIELDS = (
    _hidden("schema_version", "number"),
    _field("design_goal", "string", control="textarea", label_key="workflowConfirmation.designGoal"),
    _field("shared_rules", "string_list", label_key="workflowConfirmation.sharedRules"),
    _field("creative_boundary", "string_list", label_key="workflowConfirmation.creativeBoundary"),
    _field(
        "product_fidelity",
        "object",
        control="group",
        label_key="workflowConfirmation.productFidelity",
        fields=(
            _field("complex_structure", "boolean", label_key="agentWorkbench.nodeEditor.complexStructure"),
            _field(
                "product_present",
                "boolean",
                label_key="agentWorkbench.nodeEditor.productPresent",
                default=True,
            ),
            _field(
                "picture_in_picture",
                "string",
                control="select",
                label_key="agentWorkbench.nodeEditor.pictureInPicture",
                choices=("none", "allowed", "required"),
                default="none",
            ),
            _field("requirements", "string_list", label_key="agentWorkbench.nodeEditor.requirements"),
        ),
    ),
    _field(
        "composition",
        "object",
        control="group",
        label_key="workflowConfirmation.composition",
        fields=(
            _field("viewpoint", "string", label_key="agentWorkbench.nodeEditor.viewpoint"),
            _field(
                "product_share_percent",
                "number",
                label_key="agentWorkbench.nodeEditor.productShare",
                min_value=1,
                max_value=100,
                default=70,
            ),
            _field("layout", "string", control="textarea", label_key="agentWorkbench.nodeEditor.layout"),
            _field("copy_regions", "string_list", label_key="agentWorkbench.nodeEditor.copyRegions"),
        ),
    ),
    _field(
        "content",
        "object",
        control="group",
        label_key="workflowConfirmation.content",
        fields=(
            _field("focus", "string_list", label_key="agentWorkbench.nodeEditor.focus"),
            _field("selling_points", "string_list", label_key="agentWorkbench.nodeEditor.sellingPoints"),
            _field("background", "string", control="textarea", label_key="agentWorkbench.nodeEditor.background"),
            _field("decorations", "string_list", label_key="agentWorkbench.nodeEditor.decorations"),
        ),
    ),
    _field(
        "text",
        "object",
        control="group",
        label_key="workflowConfirmation.textContent",
        fields=(
            _field("headline", "string_or_null", label_key="agentWorkbench.nodeEditor.headline"),
            _field("subtitle", "string_or_null", label_key="agentWorkbench.nodeEditor.subtitle"),
            _field("body", "string_or_null", control="textarea", label_key="agentWorkbench.nodeEditor.body"),
        ),
    ),
    _field(
        "atmosphere",
        "object",
        control="group",
        label_key="workflowConfirmation.atmosphere",
        fields=(
            _field("keywords", "string_list", label_key="agentWorkbench.nodeEditor.keywords"),
            _field("lighting", "string", control="textarea", label_key="agentWorkbench.nodeEditor.lighting"),
        ),
    ),
    _hidden("visual_variant_key", "string_or_null"),
)

_GENERATION_SPEC_FIELDS = (
    _field(
        "aspect_ratio",
        "string",
        control="aspect_ratio",
        label_key="agentWorkbench.nodeEditor.aspectRatio",
        panel="basic",
        default="1:1",
    ),
    _field(
        "resolution_tier",
        "string",
        control="select",
        label_key="agentWorkbench.nodeEditor.resolution",
        choices=("standard", "high", "ultra"),
        panel="basic",
        default="high",
    ),
    _field(
        "quality_intent",
        "string",
        control="select",
        label_key="agentWorkbench.nodeEditor.quality",
        choices=("draft", "standard", "high"),
        panel="basic",
        default="high",
    ),
    _field(
        "reference_fidelity",
        "string",
        control="select",
        label_key="agentWorkbench.nodeEditor.referenceFidelity",
        choices=("low", "medium", "high"),
        panel="advanced",
        default="high",
    ),
    _field(
        "background_intent",
        "string",
        control="select",
        label_key="agentWorkbench.nodeEditor.backgroundIntent",
        choices=("auto", "opaque", "transparent"),
        panel="advanced",
        default="auto",
    ),
    _field(
        "text_policy",
        "string",
        control="select",
        label_key="agentWorkbench.nodeEditor.textPolicy",
        choices=("none", "allow", "required"),
        panel="advanced",
        default="none",
    ),
    _field(
        "text_language",
        "string_or_null",
        label_key="agentWorkbench.nodeEditor.textLanguage",
        max_length=80,
        panel="advanced",
        visible_when=GraphConfigVisibleWhen("text_policy", "in", ("allow", "required")),
    ),
)

_DELIVERY_SPEC_FIELDS = (
    _field(
        "width",
        "number",
        label_key="agentWorkbench.nodeEditor.width",
        min_value=1,
        max_value=16384,
        default=1200,
    ),
    _field(
        "height",
        "number",
        label_key="agentWorkbench.nodeEditor.height",
        min_value=1,
        max_value=16384,
        default=1200,
    ),
    _field(
        "format",
        "string",
        control="select",
        label_key="agentWorkbench.nodeEditor.format",
        choices=("png", "jpeg", "webp"),
        default="png",
    ),
    _hidden("max_byte_size", "number_or_null"),
    _field(
        "fit",
        "string",
        control="select",
        label_key="agentWorkbench.nodeEditor.fit",
        choices=("contain", "cover"),
        default="contain",
    ),
    _field(
        "background_color",
        "string_or_null",
        label_key="agentWorkbench.nodeEditor.backgroundColor",
        max_length=7,
        visible_when=GraphConfigVisibleWhen("fit", "in", ("contain",)),
    ),
    _field(
        "crop_anchor",
        "string_or_null",
        control="select",
        label_key="agentWorkbench.nodeEditor.cropAnchor",
        choices=("center", "top", "bottom", "left", "right"),
        visible_when=GraphConfigVisibleWhen("fit", "in", ("cover",)),
    ),
)

_CONFIG_FIELDS: dict[GraphNodeType, tuple[GraphConfigFieldDocument, ...]] = {
    GraphNodeType.PRODUCT_SOURCE: (
        _hidden("source_product_id", "string_or_null"),
        _hidden("fact_set_version_id", "string_or_null"),
    ),
    GraphNodeType.IMAGE_ASSET: (
        _field(
            "role",
            "string_or_null",
            control="select",
            label_key="graph.inspector.assetRole",
            choices=IMAGE_ASSET_ROLES,
            max_length=120,
        ),
        _field("label", "string_or_null", label_key="graph.inspector.assetLabel", max_length=255),
    ),
    GraphNodeType.CREATIVE_BRIEF: (
        _hidden("title", "string"),
        _field("goal", "string", control="textarea", label_key="workflowConfirmation.designGoal"),
        _field("design_goals", "string_list", label_key="graph.inspector.designGoals"),
        _field("required_copy", "string_list", label_key="graph.inspector.requiredCopy"),
        _field("prohibitions", "string_list", label_key="workflowConfirmation.creativeBoundary"),
    ),
    GraphNodeType.VISUAL_SYSTEM: (
        _hidden("visual_system_version_id", "string_or_null"),
        _field(
            "visual_overlay",
            "object_or_null",
            control="group",
            hint_key="graph.inspector.visualVersionHint",
            fields=_VISUAL_OVERLAY_FIELDS,
        ),
        _hidden("visual_overrides", "object"),
    ),
    GraphNodeType.PROMPT_GENERATION: (
        _hidden("image_type_key", "string"),
        _field(
            "prompt",
            "object",
            control="group",
            label_key="graph.inspector.promptSection",
            fields=_PROMPT_FIELDS,
        ),
    ),
    GraphNodeType.IMAGE_GENERATION: (
        _hidden("image_type_key", "string"),
        _field(
            "variation_instruction",
            "string_or_null",
            control="textarea",
            label_key="workflowConfirmation.variation",
            max_length=4000,
        ),
        _field(
            "generation_spec",
            "object",
            control="group",
            label_key="agentWorkbench.nodeEditor.generationSettings",
            default=dict(_DEFAULT_GENERATION_SPEC),
            fields=_GENERATION_SPEC_FIELDS,
        ),
        _field(
            "delivery_spec",
            "object_or_null",
            control="optional_object",
            label_key="workflowConfirmation.deliverySpec",
            toggle_label_key="agentWorkbench.nodeEditor.deliveryEnabled",
            panel="advanced",
            default=dict(_DEFAULT_DELIVERY_SPEC),
            fields=_DELIVERY_SPEC_FIELDS,
        ),
        _field(
            "visual_overlay",
            "object_or_null",
            control="group",
            fields=_VISUAL_OVERLAY_FIELDS,
        ),
        _hidden("visual_overrides", "object"),
    ),
}

PROCESSING_NODE_TYPES = frozenset(
    {
        GraphNodeType.CREATIVE_BRIEF,
        GraphNodeType.VISUAL_SYSTEM,
        GraphNodeType.PROMPT_GENERATION,
        GraphNodeType.IMAGE_GENERATION,
    }
)
SOURCE_NODE_TYPES = frozenset(
    {
        GraphNodeType.PRODUCT_SOURCE,
        GraphNodeType.IMAGE_ASSET,
    }
)


@dataclass(frozen=True, slots=True)
class GraphCatalogNodeDocument:
    node_type: GraphNodeType
    output_data_type: GraphEdgeDataType
    kind: GraphNodeKind
    accepts: tuple[GraphInputContract, ...]
    config_fields: tuple[GraphConfigFieldDocument, ...]


@dataclass(frozen=True, slots=True)
class GraphCatalogDocument:
    version: int
    nodes: tuple[GraphCatalogNodeDocument, ...]


def graph_node_kind(node_type: GraphNodeType) -> GraphNodeKind:
    return "processing" if node_type in PROCESSING_NODE_TYPES else "source"


def node_config_fields(node_type: GraphNodeType) -> tuple[GraphConfigFieldDocument, ...]:
    return tuple(_copy_config_field(item) for item in _CONFIG_FIELDS[node_type])


def allowed_config_keys(node_type: GraphNodeType) -> frozenset[str]:
    return frozenset(field.key for field in node_config_fields(node_type))


def validate_node_config(node_type: GraphNodeType, config: dict[str, object] | None) -> None:
    normalize_node_config(node_type, config)


def normalize_node_config(node_type: GraphNodeType, config: dict[str, object] | None) -> dict[str, object]:
    payload: dict[str, object] = deepcopy(config) if config is not None else {}
    illegal = FORBIDDEN_GRAPH_CONFIG_KEYS.intersection(payload)
    if illegal:
        raise BusinessValidationError(f"节点配置不能包含拓扑字段: {', '.join(sorted(illegal))}")
    _validate_config_fields(node_config_fields(node_type), payload, path="")
    if node_type == GraphNodeType.IMAGE_GENERATION:
        if "generation_spec" in payload:
            payload["generation_spec"] = _normalize_generation_spec(payload["generation_spec"])
        if "delivery_spec" in payload and payload["delivery_spec"] is not None:
            payload["delivery_spec"] = _normalize_delivery_spec(payload["delivery_spec"])
    return payload


def graph_catalog_document() -> GraphCatalogDocument:
    return GraphCatalogDocument(
        version=GRAPH_CATALOG_VERSION,
        nodes=tuple(
            GraphCatalogNodeDocument(
                node_type=node_type,
                output_data_type=graph_node_output_type(node_type),
                kind=graph_node_kind(node_type),
                accepts=accepted_inputs(node_type),
                config_fields=node_config_fields(node_type),
            )
            for node_type in GraphNodeType
        ),
    )


def graph_node_output_type(node_type: GraphNodeType) -> GraphEdgeDataType:
    return _OUTPUT_TYPE[node_type]


def graph_input_contract(
    source_type: GraphNodeType,
    target_type: GraphNodeType,
) -> GraphInputContract | None:
    return _ACCEPTANCE.get((graph_node_output_type(source_type), target_type))


def require_graph_connection(
    source_type: GraphNodeType,
    target_type: GraphNodeType,
) -> GraphInputContract:
    contract = graph_input_contract(source_type, target_type)
    if contract is None:
        raise BusinessValidationError("节点类型不兼容，不能创建连线")
    return contract


def accepted_inputs(node_type: GraphNodeType) -> tuple[GraphInputContract, ...]:
    return tuple(contract for (data_type, target), contract in _ACCEPTANCE.items() if target == node_type)


def run_required_inputs(node_type: GraphNodeType) -> tuple[GraphInputContract, ...]:
    return tuple(contract for contract in accepted_inputs(node_type) if contract.required_to_run)


def graph_catalog_json(document: GraphCatalogDocument | None = None) -> dict[str, Any]:
    catalog = document or graph_catalog_document()
    return {
        "version": catalog.version,
        "nodes": [
            {
                "node_type": node.node_type.value,
                "output_data_type": node.output_data_type.value,
                "kind": node.kind,
                "accepts": [
                    {
                        "data_type": item.data_type.value,
                        "role": item.role.value,
                        "max_count": item.max_count,
                        "required_to_run": item.required_to_run,
                    }
                    for item in node.accepts
                ],
                "config_fields": [_config_field_json(item) for item in node.config_fields],
            }
            for node in catalog.nodes
        ],
    }


def _config_field_json(item: GraphConfigFieldDocument) -> dict[str, Any]:
    return {
        "key": item.key,
        "value_kind": item.value_kind,
        "control": item.control,
        "required": item.required,
        "label_key": item.label_key or None,
        "hint_key": item.hint_key or None,
        "toggle_label_key": item.toggle_label_key or None,
        "choices": list(item.choices),
        "min_value": item.min_value,
        "max_value": item.max_value,
        "max_length": item.max_length,
        "default": deepcopy(item.default),
        "panel": item.panel,
        "visible_when": (
            {
                "field": item.visible_when.field,
                "op": item.visible_when.op,
                "values": list(item.visible_when.values),
            }
            if item.visible_when is not None
            else None
        ),
        "fields": [_config_field_json(child) for child in item.fields],
    }


def _copy_config_field(item: GraphConfigFieldDocument) -> GraphConfigFieldDocument:
    return replace(
        item,
        default=deepcopy(item.default),
        fields=tuple(_copy_config_field(child) for child in item.fields),
    )


def _validate_config_fields(
    fields: tuple[GraphConfigFieldDocument, ...],
    payload: dict[str, object],
    *,
    path: str,
) -> None:
    known_keys = {item.key for item in fields}
    unknown = sorted(key for key in payload if key not in known_keys)
    if unknown:
        scope = path or "节点配置"
        raise BusinessValidationError(f"{scope}包含未登记字段: {', '.join(unknown)}")
    for item in fields:
        if item.key not in payload:
            continue
        field_path = f"{path}.{item.key}" if path else item.key
        _validate_config_field(item, payload[item.key], field_path)


def _validate_config_field(item: GraphConfigFieldDocument, value: object, path: str) -> None:
    if item.control == "hidden" and item.value_kind in {"object", "object_or_null", "object_list"}:
        return
    if not _matches_value_kind(item.value_kind, value):
        raise BusinessValidationError(f"节点配置字段 {path} 不符合 value_kind={item.value_kind}")
    if value is None:
        return
    if item.choices and isinstance(value, str) and value not in item.choices:
        raise BusinessValidationError(f"节点配置字段 {path} 不在 choices 范围内")
    if item.min_value is not None or item.max_value is not None:
        if not isinstance(value, (int, float)) or isinstance(value, bool):
            raise BusinessValidationError(f"节点配置字段 {path} 不是可比较的 number")
        if item.min_value is not None and value < item.min_value:
            raise BusinessValidationError(f"节点配置字段 {path} 不能小于 {item.min_value}")
        if item.max_value is not None and value > item.max_value:
            raise BusinessValidationError(f"节点配置字段 {path} 不能大于 {item.max_value}")
    if item.max_length is not None and isinstance(value, (str, list, tuple)) and len(value) > item.max_length:
        raise BusinessValidationError(f"节点配置字段 {path} 不能超过 max_length={item.max_length}")
    if item.value_kind in {"object", "object_or_null"} and isinstance(value, dict) and item.fields:
        _validate_config_fields(item.fields, value, path=path)
    elif item.value_kind == "object_list" and isinstance(value, list) and item.fields:
        for index, entry in enumerate(value):
            _validate_config_fields(item.fields, entry, path=f"{path}[{index}]")


def _matches_value_kind(value_kind: GraphConfigValueKind, value: object) -> bool:
    if value_kind == "string":
        return isinstance(value, str)
    if value_kind == "string_or_null":
        return value is None or isinstance(value, str)
    if value_kind == "string_list":
        return isinstance(value, list) and all(isinstance(item, str) for item in value)
    if value_kind == "boolean":
        return isinstance(value, bool)
    if value_kind == "number":
        return isinstance(value, (int, float)) and not isinstance(value, bool) and isfinite(value)
    if value_kind == "number_or_null":
        return value is None or (
            isinstance(value, (int, float)) and not isinstance(value, bool) and isfinite(value)
        )
    if value_kind == "object":
        return isinstance(value, dict)
    if value_kind == "object_or_null":
        return value is None or isinstance(value, dict)
    if value_kind == "object_list":
        return isinstance(value, list) and all(isinstance(item, dict) for item in value)
    return False


def _normalize_generation_spec(value: object) -> dict[str, object]:
    try:
        return GenerationSpec.model_validate(value).model_dump(mode="json")
    except ValidationError as exc:
        raise BusinessValidationError(f"generation_spec 无效: {exc}") from exc


def _normalize_delivery_spec(value: object) -> dict[str, object]:
    try:
        return DeliverySpec.model_validate(value).model_dump(mode="json")
    except ValidationError as exc:
        raise BusinessValidationError(f"delivery_spec 无效: {exc}") from exc
