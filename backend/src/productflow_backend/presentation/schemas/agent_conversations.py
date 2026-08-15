from __future__ import annotations

from datetime import datetime
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator

from productflow_backend.application.agent_conversations import (
    AGENT_MAX_INPUT_ASSETS,
    AGENT_MAX_INPUT_TEXT_CHARS,
)
from productflow_backend.domain.enums import AgentConversationStatus, AgentTurnStatus
from productflow_backend.infrastructure.db.models import AgentConversation, AgentTurnProjection


class StrictAgentRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")


class AgentContractResponse(BaseModel):
    schema_version: Literal[1]
    conversation_id: str
    product_id: str
    workflow_draft_id: str
    harness_run_id: str
    current_draft_version: int
    system_prompt: str
    workflow_draft_schema: dict[str, Any]
    tool_contract_version: int


class AgentWorkflowDraftValidationRequest(StrictAgentRequest):
    value: dict[str, Any]


class AgentWorkflowDraftValidationResponse(BaseModel):
    accepted: Literal[True] = True


class AgentAssetMetadataResponse(BaseModel):
    id: str
    display_name: str
    original_filename: str
    origin_type: str
    image_type_key: str | None
    image_type_title: str | None
    user_folder_id: str | None
    user_folder_name: str | None
    mime_type: Literal["image/png", "image/jpeg", "image/webp"]
    byte_size: int | None = Field(default=None, gt=0)
    width: int | None = Field(default=None, gt=0)
    height: int | None = Field(default=None, gt=0)
    verification_status: Literal["verified", "legacy_pending", "missing"]
    parent_asset_id: str | None
    generation: dict[str, str] | None
    created_at: str


class AgentAssetListResponse(BaseModel):
    items: list[AgentAssetMetadataResponse]
    next_cursor: str | None = None


class InspectAgentAssetsRequest(StrictAgentRequest):
    asset_ids: list[str] = Field(min_length=1, max_length=AGENT_MAX_INPUT_ASSETS)


class InspectAgentAssetsResponse(BaseModel):
    items: list[AgentAssetMetadataResponse]


class PrepareAgentAssetRenameRequest(StrictAgentRequest):
    asset_id: str = Field(min_length=1, max_length=64)
    target_display_name: str = Field(min_length=1, max_length=255)


class AgentAssetRenamePreparedRequest(StrictAgentRequest):
    asset_id: str = Field(min_length=1, max_length=64)
    expected_display_name: str = Field(min_length=1, max_length=255)
    target_display_name: str = Field(min_length=1, max_length=255)


class AgentAssetRenamePreparedResponse(BaseModel):
    asset_id: str
    expected_display_name: str
    target_display_name: str


class AgentAssetRenameResultResponse(BaseModel):
    asset_id: str
    display_name: str
    applied: bool


class AgentAssetRenameReconcileResponse(BaseModel):
    state: Literal["applied", "not_applied", "conflict", "unknown"]
    result: AgentAssetRenameResultResponse | None = None
    detail: str | None = None


class PrepareAgentFolderCreateRequest(StrictAgentRequest):
    name: str = Field(min_length=1, max_length=120)


class AgentFolderCreatePreparedRequest(StrictAgentRequest):
    folder_id: str = Field(min_length=1, max_length=36)
    name: str = Field(min_length=1, max_length=120)


class AgentFolderCreateResultResponse(BaseModel):
    folder_id: str
    name: str
    sort_order: int = Field(ge=0)
    applied: bool


class AgentFolderCreateReconcileResponse(BaseModel):
    state: Literal["applied", "not_applied", "conflict", "unknown"]
    result: AgentFolderCreateResultResponse | None = None
    detail: str | None = None


class PrepareAgentFolderRenameRequest(StrictAgentRequest):
    folder_id: str = Field(min_length=1, max_length=36)
    target_name: str = Field(min_length=1, max_length=120)


class AgentFolderRenamePreparedRequest(StrictAgentRequest):
    folder_id: str = Field(min_length=1, max_length=36)
    expected_name: str = Field(min_length=1, max_length=120)
    target_name: str = Field(min_length=1, max_length=120)


class AgentFolderRenameResultResponse(BaseModel):
    folder_id: str
    name: str
    applied: bool


class AgentFolderRenameReconcileResponse(BaseModel):
    state: Literal["applied", "not_applied", "conflict", "unknown"]
    result: AgentFolderRenameResultResponse | None = None
    detail: str | None = None


class PrepareAgentAssetMoveRequest(StrictAgentRequest):
    asset_ids: list[str] = Field(min_length=1, max_length=100)
    target_folder_id: str | None = Field(max_length=36)


class AgentAssetMoveItemRequest(StrictAgentRequest):
    asset_id: str = Field(min_length=1, max_length=36)
    expected_folder_id: str | None = Field(max_length=36)


class AgentAssetMovePreparedRequest(StrictAgentRequest):
    moves: list[AgentAssetMoveItemRequest] = Field(min_length=1, max_length=100)
    target_folder_id: str | None = Field(max_length=36)


class AgentAssetMoveResultResponse(BaseModel):
    asset_ids: list[str]
    folder_id: str | None
    applied: bool


class AgentAssetMoveReconcileResponse(BaseModel):
    state: Literal["applied", "not_applied", "conflict", "unknown"]
    result: AgentAssetMoveResultResponse | None = None
    detail: str | None = None


class CreateAgentConversationRequest(StrictAgentRequest):
    workflow_draft_id: str = Field(min_length=1, max_length=64)


class StartAgentTurnRequest(StrictAgentRequest):
    input_text: str = Field(min_length=1, max_length=AGENT_MAX_INPUT_TEXT_CHARS)
    asset_ids: list[str] = Field(default_factory=list, max_length=AGENT_MAX_INPUT_ASSETS)
    idempotency_key: str = Field(min_length=1, max_length=200)


class AgentQuestionAnswerRequest(StrictAgentRequest):
    option: int | None = Field(default=None, ge=0)
    text: str | None = Field(default=None, max_length=4000)

    @model_validator(mode="after")
    def validate_one_answer(self) -> AgentQuestionAnswerRequest:
        has_option = self.option is not None
        normalized_text = self.text.strip() if self.text is not None else ""
        if has_option == bool(normalized_text):
            raise ValueError("回答必须且只能提供 option 或 text")
        if len(normalized_text.encode("utf-8")) > 4000:
            raise ValueError("自由文本回答不能超过 4000 bytes")
        self.text = normalized_text or None
        return self

    def to_gateway_payload(self) -> dict[str, int | str]:
        if self.option is not None:
            return {"option": self.option}
        return {"text": self.text or ""}


class AgentConversationResponse(BaseModel):
    id: str
    product_id: str
    workflow_draft_id: str
    harness_run_id: str
    status: AgentConversationStatus
    created_at: datetime
    updated_at: datetime


class AgentTurnResponse(BaseModel):
    id: str
    conversation_id: str
    harness_turn_id: str | None
    idempotency_key: str
    input_text: str
    input_asset_ids: list[str]
    status: AgentTurnStatus
    resume_required: bool
    output_text: str | None
    error_text: str | None
    question: dict[str, Any] | None
    artifact_name: str | None
    artifact_step_id: str | None
    workflow_draft_revision_id: str | None
    sync_error: str | None
    finished_at: datetime | None
    created_at: datetime
    updated_at: datetime


class AgentTurnPageResponse(BaseModel):
    items: list[AgentTurnResponse]
    next_cursor: str | None = None


class SubmitAgentTurnResponse(BaseModel):
    created: bool
    turn: AgentTurnResponse


def serialize_agent_conversation(conversation: AgentConversation) -> AgentConversationResponse:
    return AgentConversationResponse(
        id=conversation.id,
        product_id=conversation.product_id,
        workflow_draft_id=conversation.workflow_draft_id,
        harness_run_id=conversation.harness_run_id,
        status=conversation.status,
        created_at=conversation.created_at,
        updated_at=conversation.updated_at,
    )


def serialize_agent_turn(projection: AgentTurnProjection) -> AgentTurnResponse:
    return AgentTurnResponse(
        id=projection.id,
        conversation_id=projection.conversation_id,
        harness_turn_id=projection.harness_turn_id,
        idempotency_key=projection.idempotency_key,
        input_text=projection.input_text,
        input_asset_ids=list(projection.input_asset_ids_json),
        status=projection.status,
        resume_required=projection.resume_required,
        output_text=projection.output_text,
        error_text=projection.error_text,
        question=dict(projection.question_json) if projection.question_json is not None else None,
        artifact_name=projection.artifact_name,
        artifact_step_id=projection.artifact_step_id,
        workflow_draft_revision_id=projection.workflow_draft_revision_id,
        sync_error=projection.sync_error,
        finished_at=projection.finished_at,
        created_at=projection.created_at,
        updated_at=projection.updated_at,
    )
