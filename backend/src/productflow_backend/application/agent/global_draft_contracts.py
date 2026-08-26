"""全局 Agent 唯一可选 artifact 合同。当前只接受素材整理 Draft。"""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict

from productflow_backend.application.media_library.draft_contracts import (
    LibraryOrganizationDraftPayloadV1,
)
from productflow_backend.application.workflow_drafts.contracts import (
    normalize_tool_schema,
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
