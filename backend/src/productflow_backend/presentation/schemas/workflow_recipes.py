from __future__ import annotations

from datetime import datetime
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator

from productflow_backend.application.workflow_recipes.contracts import RECIPE_SCHEMA_VERSION, RecipePayload
from productflow_backend.application.workflow_recipes.service import (
    WorkflowRecipeApplicationResult,
    WorkflowRecipeArchiveResult,
    parse_recipe_payload_or_raise,
)
from productflow_backend.domain.enums import WorkflowRecipeKind
from productflow_backend.infrastructure.db.models import WorkflowRecipe, WorkflowRecipeVersion
from productflow_backend.presentation.schemas.agent_conversations import (
    AgentConversationResponse,
    serialize_agent_conversation,
)
from productflow_backend.presentation.schemas.workflow_drafts import (
    WorkflowDraftResponse,
    serialize_workflow_draft,
)


class StrictRecipeRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")


class RecipeSourceRequest(StrictRecipeRequest):
    source_type: Literal["workflow", "group", "selection"]
    group_id: str | None = Field(default=None, min_length=1, max_length=36)
    node_ids: list[str] = Field(default_factory=list)
    expected_graph_revision: int = Field(ge=0)

    @model_validator(mode="after")
    def validate_source_fields(self) -> RecipeSourceRequest:
        if self.source_type == "workflow" and (self.group_id is not None or self.node_ids):
            raise ValueError("完整工作流来源不能指定 group_id 或 node_ids")
        if self.source_type == "group" and (self.group_id is None or self.node_ids):
            raise ValueError("分组来源必须且只能指定 group_id")
        if self.source_type == "selection" and (self.group_id is not None or not self.node_ids):
            raise ValueError("多选来源必须且只能指定非空 node_ids")
        if len(self.node_ids) != len(set(self.node_ids)):
            raise ValueError("node_ids 不能重复")
        return self


class CreateWorkflowRecipeRequest(RecipeSourceRequest):
    title: str = Field(min_length=1, max_length=255)
    description: str | None = Field(default=None, max_length=4000)
    preferred_visual_system_version_id: str | None = Field(default=None, max_length=36)


class AppendWorkflowRecipeVersionRequest(CreateWorkflowRecipeRequest):
    expected_recipe_version: int = Field(ge=1)


class ApplyWorkflowRecipeRequest(StrictRecipeRequest):
    expected_recipe_version: int = Field(ge=1)
    idempotency_key: str = Field(min_length=1, max_length=120)


class WorkflowRecipeVersionResponse(BaseModel):
    id: str
    recipe_id: str
    version: int
    schema_version: Literal[3]
    title: str
    description: str | None
    payload: RecipePayload
    payload_hash: str
    preferred_visual_system_version_id: str | None
    created_at: datetime


class WorkflowRecipeSummaryResponse(BaseModel):
    id: str
    kind: WorkflowRecipeKind
    current_version_id: str
    current_version: WorkflowRecipeVersionResponse
    archived_at: datetime | None
    created_at: datetime
    updated_at: datetime


class WorkflowRecipeResponse(WorkflowRecipeSummaryResponse):
    versions: list[WorkflowRecipeVersionResponse]


class WorkflowRecipeArchiveResponse(BaseModel):
    changed: bool
    recipe: WorkflowRecipeResponse


class WorkflowRecipeApplicationResponse(BaseModel):
    created: bool
    recipe_id: str
    recipe_version_id: str
    recipe_version: int
    draft: WorkflowDraftResponse
    conversation: AgentConversationResponse


def serialize_workflow_recipe_version(
    version: WorkflowRecipeVersion,
) -> WorkflowRecipeVersionResponse:
    return WorkflowRecipeVersionResponse(
        id=version.id,
        recipe_id=version.recipe_id,
        version=version.version,
        schema_version=version.schema_version,
        title=version.title,
        description=version.description,
        payload=parse_recipe_payload_or_raise(version),
        payload_hash=version.payload_hash,
        preferred_visual_system_version_id=version.preferred_visual_system_version_id,
        created_at=version.created_at,
    )


def serialize_workflow_recipe_summary(
    recipe: WorkflowRecipe,
) -> WorkflowRecipeSummaryResponse:
    if recipe.current_version is None or recipe.current_version_id is None:
        raise ValueError("WorkflowRecipe 缺少 current version")
    return WorkflowRecipeSummaryResponse(
        id=recipe.id,
        kind=recipe.kind,
        current_version_id=recipe.current_version_id,
        current_version=serialize_workflow_recipe_version(recipe.current_version),
        archived_at=recipe.archived_at,
        created_at=recipe.created_at,
        updated_at=recipe.updated_at,
    )


def serialize_workflow_recipe(recipe: WorkflowRecipe) -> WorkflowRecipeResponse:
    summary = serialize_workflow_recipe_summary(recipe)
    return WorkflowRecipeResponse(
        **summary.model_dump(),
        versions=[serialize_workflow_recipe_version(version) for version in recipe.versions],
    )


def serialize_workflow_recipe_archive(
    result: WorkflowRecipeArchiveResult,
) -> WorkflowRecipeArchiveResponse:
    return WorkflowRecipeArchiveResponse(
        changed=result.changed,
        recipe=serialize_workflow_recipe(result.recipe),
    )


def serialize_workflow_recipe_application(
    result: WorkflowRecipeApplicationResult,
) -> WorkflowRecipeApplicationResponse:
    return WorkflowRecipeApplicationResponse(
        created=result.created,
        recipe_id=result.recipe.id,
        recipe_version_id=result.recipe_version.id,
        recipe_version=result.recipe_version.version,
        draft=serialize_workflow_draft(result.draft),
        conversation=serialize_agent_conversation(result.conversation),
    )


__all__ = [
    "AppendWorkflowRecipeVersionRequest",
    "ApplyWorkflowRecipeRequest",
    "CreateWorkflowRecipeRequest",
    "RECIPE_SCHEMA_VERSION",
    "WorkflowRecipeArchiveResponse",
    "WorkflowRecipeApplicationResponse",
    "WorkflowRecipeResponse",
    "WorkflowRecipeSummaryResponse",
    "serialize_workflow_recipe",
    "serialize_workflow_recipe_archive",
    "serialize_workflow_recipe_application",
    "serialize_workflow_recipe_summary",
]
