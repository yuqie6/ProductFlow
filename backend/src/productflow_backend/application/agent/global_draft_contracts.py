"""全局 Agent 唯一可选 artifact 合同。当前只接受素材整理 Draft。"""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict

from productflow_backend.application.media_library.draft_contracts import (
    LibraryOrganizationDraftPayloadV1,
)

GLOBAL_AGENT_DRAFT_SCHEMA_VERSION = 1
GLOBAL_AGENT_DRAFT_ARTIFACT_NAME = "propose_global_draft"


class StrictGlobalAgentDraftModel(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)


class GlobalAgentDraftPayloadV1(StrictGlobalAgentDraftModel):
    """全局 Agent conversation 暴露的唯一可选 artifact。"""

    schema_version: Literal[1] = GLOBAL_AGENT_DRAFT_SCHEMA_VERSION
    draft_kind: Literal["library_organization"]
    library_payload: LibraryOrganizationDraftPayloadV1


def normalize_tool_schema(schema: dict[str, object]) -> None:
    schema.pop("default", None)
    schema.pop("deprecated", None)
    schema.pop("discriminator", None)

    one_of = schema.pop("oneOf", None)
    if one_of is not None:
        if "anyOf" in schema:
            raise ValueError("工具 Schema 不能同时包含 oneOf 和 anyOf")
        schema["anyOf"] = one_of

    properties = schema.get("properties")
    if properties is not None:
        if not isinstance(properties, dict):
            raise TypeError("工具 Schema properties 必须是对象")
        schema["required"] = list(properties)
        schema["additionalProperties"] = False
        for property_schema in properties.values():
            if isinstance(property_schema, dict):
                normalize_tool_schema(property_schema)
    elif schema.get("type") == "object":
        raise ValueError("工具 Schema 不允许开放对象")

    definitions = schema.get("$defs")
    if isinstance(definitions, dict):
        for definition in definitions.values():
            if isinstance(definition, dict):
                normalize_tool_schema(definition)

    items = schema.get("items")
    if isinstance(items, dict):
        normalize_tool_schema(items)

    any_of = schema.get("anyOf")
    if isinstance(any_of, list):
        for variant in any_of:
            if isinstance(variant, dict):
                normalize_tool_schema(variant)


def global_agent_draft_schema() -> dict[str, object]:
    schema = GlobalAgentDraftPayloadV1.model_json_schema()
    normalize_tool_schema(schema)
    return schema


__all__ = [
    "GLOBAL_AGENT_DRAFT_ARTIFACT_NAME",
    "GLOBAL_AGENT_DRAFT_SCHEMA_VERSION",
    "GlobalAgentDraftPayloadV1",
    "global_agent_draft_schema",
]
