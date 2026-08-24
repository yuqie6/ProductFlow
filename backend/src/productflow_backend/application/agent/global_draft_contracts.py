"""全局 Agent 唯一可选 artifact 合同。workflow 分支必须带目标商品作用域。"""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator

from productflow_backend.application.media_library.draft_contracts import (
    LibraryOrganizationDraftPayloadV1,
)
from productflow_backend.application.workflow_drafts.contracts import (
    WorkflowDraftPayloadV1,
    normalize_tool_schema,
)

GLOBAL_AGENT_DRAFT_SCHEMA_VERSION = 1
GLOBAL_AGENT_DRAFT_ARTIFACT_NAME = "propose_global_draft"


class StrictGlobalAgentDraftModel(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)


class GlobalAgentDraftPayloadV1(StrictGlobalAgentDraftModel):
    """The single optional artifact exposed by a global Agent conversation."""

    schema_version: Literal[1] = GLOBAL_AGENT_DRAFT_SCHEMA_VERSION
    draft_kind: Literal["library_organization", "workflow"]
    product_id: str | None = Field(default=None, min_length=1, max_length=64)
    workflow_draft_id: str | None = Field(default=None, min_length=1, max_length=64)
    expected_draft_version: int | None = Field(default=None, ge=0)
    workflow_payload: WorkflowDraftPayloadV1 | None = None
    library_payload: LibraryOrganizationDraftPayloadV1 | None = None

    @model_validator(mode="after")
    def validate_branch(self) -> GlobalAgentDraftPayloadV1:
        if self.draft_kind == "library_organization":
            if any(
                value is not None
                for value in (
                    self.product_id,
                    self.workflow_draft_id,
                    self.expected_draft_version,
                    self.workflow_payload,
                )
            ):
                raise ValueError("素材整理 Draft 不能包含商品或工作流 Draft 作用域")
            if self.library_payload is None:
                raise ValueError("素材整理 Draft 必须包含 library_payload")
            return self

        if self.product_id is None or self.workflow_draft_id is None:
            raise ValueError("工作流 Draft 必须包含 product_id 和 workflow_draft_id")
        if self.expected_draft_version is None:
            raise ValueError("工作流 Draft 必须包含 expected_draft_version")
        if self.workflow_payload is None:
            raise ValueError("工作流 Draft 必须包含 workflow_payload")
        if self.library_payload is not None:
            raise ValueError("工作流 Draft 不能包含 library_payload")
        return self


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
