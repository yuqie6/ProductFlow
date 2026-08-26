from __future__ import annotations

import hashlib
import json
from typing import Annotated, Any, Literal

from pydantic import BaseModel, ConfigDict, Field, StringConstraints, model_validator

from productflow_backend.domain.enums import GraphEdgeDataType, GraphEdgeRole, GraphNodeType
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.domain.graph_catalog import FORBIDDEN_GRAPH_CONFIG_KEYS, validate_node_config

RECIPE_SCHEMA_VERSION = 3
RECIPE_MAX_NODES = 128
RECIPE_MAX_EDGES = 256
RECIPE_MAX_GROUPS = 32
RECIPE_MAX_JSON_BYTES = 512 * 1024

RECIPE_IDENTITY_CONFIG_KEYS = frozenset(
    {
        "source_product_id",
        "fact_set_version_id",
        "visual_system_version_id",
    }
)
RECIPE_STRIPPED_PROMPT_KEYS = frozenset(
    {
        "images",
        "fact_keys",
        "evidence_asset_ids",
        "prompt_plan_key",
        "image_plan_key",
        "prompt_plan_keys",
        "image_plan_keys",
    }
)

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
RecipeReference = Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=500)]


class StrictRecipeModel(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)


class RecipeGraphNode(StrictRecipeModel):
    key: RecipeKey
    node_type: GraphNodeType
    title: RecipeText
    position_x: int
    position_y: int
    group_key: RecipeKey | None = None
    config: dict[str, Any] = Field(default_factory=dict)

    @model_validator(mode="after")
    def validate_config(self) -> RecipeGraphNode:
        illegal = FORBIDDEN_GRAPH_CONFIG_KEYS.intersection(self.config)
        if illegal:
            raise ValueError(f"配方节点配置不能包含拓扑字段: {', '.join(sorted(illegal))}")
        try:
            validate_node_config(self.node_type, self.config)
        except BusinessValidationError as exc:
            raise ValueError(str(exc)) from exc
        return self


class RecipeGraphEdge(StrictRecipeModel):
    key: RecipeKey
    source_node_key: RecipeKey
    target_node_key: RecipeKey
    data_type: GraphEdgeDataType
    role: GraphEdgeRole
    order: int = Field(default=0, ge=0)


class RecipeGraphGroup(StrictRecipeModel):
    key: RecipeKey
    title: RecipeText
    member_keys: tuple[RecipeKey, ...] = Field(min_length=1)


class RecipePayload(StrictRecipeModel):
    schema_version: Literal[3] = RECIPE_SCHEMA_VERSION
    nodes: list[RecipeGraphNode] = Field(min_length=1, max_length=RECIPE_MAX_NODES)
    edges: list[RecipeGraphEdge] = Field(default_factory=list, max_length=RECIPE_MAX_EDGES)
    groups: list[RecipeGraphGroup] = Field(default_factory=list, max_length=RECIPE_MAX_GROUPS)

    @model_validator(mode="after")
    def validate_graph(self) -> RecipePayload:
        node_keys = _require_unique([node.key for node in self.nodes], label="配方节点 key")
        _require_unique([edge.key for edge in self.edges], label="配方连线 key")
        group_keys = _require_unique([group.key for group in self.groups], label="配方分组 key")
        member_group_keys: set[str] = set()
        for node in self.nodes:
            if node.group_key is None:
                continue
            if node.group_key not in group_keys:
                raise ValueError("配方节点引用了不存在的分组")
            member_group_keys.add(node.group_key)
        if member_group_keys != group_keys:
            raise ValueError("配方分组必须至少包含一个节点")
        for group in self.groups:
            _require_unique(list(group.member_keys), label="配方分组成员")
            for member_key in group.member_keys:
                if member_key not in node_keys:
                    raise ValueError("配方分组引用了不存在的节点")
            expected = {node.key for node in self.nodes if node.group_key == group.key}
            if expected != set(group.member_keys):
                raise ValueError("配方分组成员必须与节点 group_key 一致")
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
        return self


class RecipeGovernance(StrictRecipeModel):
    """与可复用图载荷分开存放的配方版本元数据。"""

    applicable_image_types: tuple[RecipeKey, ...] = Field(min_length=1, max_length=32)
    required_inputs: tuple[RecipeKey, ...] = Field(default_factory=tuple, max_length=32)
    default_result: RecipeText
    thumbnail: RecipeReference | None = None
    provider_sample: RecipeReference | None = None

    @model_validator(mode="after")
    def validate_unique_values(self) -> RecipeGovernance:
        _require_unique(list(self.applicable_image_types), label="适用图片类型")
        _require_unique(list(self.required_inputs), label="配方所需输入")
        return self


def parse_recipe_governance(value: RecipeGovernance | dict[str, Any] | None) -> RecipeGovernance | None:
    if value is None or isinstance(value, RecipeGovernance):
        return value
    return RecipeGovernance.model_validate(value)


def recipe_governance_dict(value: RecipeGovernance | dict[str, Any] | None) -> dict[str, Any] | None:
    parsed = parse_recipe_governance(value)
    return parsed.model_dump(mode="json") if parsed is not None else None


def recipe_payload_dict(payload: RecipePayload | dict[str, Any]) -> dict[str, Any]:
    parsed = payload if isinstance(payload, RecipePayload) else RecipePayload.model_validate(payload)
    return parsed.model_dump(mode="json")


def recipe_payload_json(payload: RecipePayload | dict[str, Any]) -> bytes:
    encoded = json.dumps(
        recipe_payload_dict(payload),
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")
    if len(encoded) > RECIPE_MAX_JSON_BYTES:
        raise ValueError(f"配方 JSON 不能超过 {RECIPE_MAX_JSON_BYTES} 字节")
    return encoded


def recipe_payload_hash(payload: RecipePayload | dict[str, Any]) -> str:
    return hashlib.sha256(recipe_payload_json(payload)).hexdigest()


def _require_unique(values: list[Any], *, label: str) -> set[Any]:
    if len(values) != len(set(values)):
        raise ValueError(f"{label} 不能重复")
    return set(values)


__all__ = [
    "RECIPE_IDENTITY_CONFIG_KEYS",
    "RECIPE_MAX_EDGES",
    "RECIPE_MAX_GROUPS",
    "RECIPE_MAX_JSON_BYTES",
    "RECIPE_MAX_NODES",
    "RECIPE_SCHEMA_VERSION",
    "RECIPE_STRIPPED_PROMPT_KEYS",
    "RecipeGraphEdge",
    "RecipeGraphGroup",
    "RecipeGraphNode",
    "RecipeGovernance",
    "RecipePayload",
    "parse_recipe_governance",
    "recipe_governance_dict",
    "recipe_payload_dict",
    "recipe_payload_hash",
    "recipe_payload_json",
]
