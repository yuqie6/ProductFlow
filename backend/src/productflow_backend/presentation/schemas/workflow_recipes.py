from __future__ import annotations

from datetime import datetime
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator

from productflow_backend.application.workflow_recipes.contracts import (
    RECIPE_SCHEMA_VERSION,
    RecipeGovernance,
    RecipePayload,
    parse_recipe_governance,
)
from productflow_backend.application.workflow_recipes.live_apply import RecipeApplyPreview
from productflow_backend.application.workflow_recipes.service import (
    WorkflowRecipeApplicationResult,
    WorkflowRecipeArchiveResult,
    parse_recipe_payload_or_raise,
)
from productflow_backend.domain.enums import (
    GraphNodeType,
    WorkflowRecipeCreationSource,
    WorkflowRecipeKind,
    WorkflowRecipeOrigin,
)
from productflow_backend.presentation.schemas.graphs import GraphProjectionResponse


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


class PreviewWorkflowRecipeRequest(StrictRecipeRequest):
    expected_recipe_version: int = Field(ge=1)


class ApplyWorkflowRecipeRequest(StrictRecipeRequest):
    expected_recipe_version: int = Field(ge=1)
    expected_graph_revision: int = Field(ge=0)
    preview_digest: str = Field(
        min_length=64,
        max_length=64,
        pattern=r"^[0-9a-f]{64}$",
    )
    idempotency_key: str = Field(min_length=1, max_length=120)


class WorkflowRecipeVersionResponse(BaseModel):
    id: str
    recipe_id: str
    version: int
    schema_version: Literal[3]
    catalog_version: int
    creation_source: WorkflowRecipeCreationSource
    title: str
    description: str | None
    payload: RecipePayload
    payload_hash: str
    governance: RecipeGovernance | None
    preferred_visual_system_version_id: str | None
    created_at: datetime


class WorkflowRecipeSummaryResponse(BaseModel):
    id: str
    kind: WorkflowRecipeKind
    origin: WorkflowRecipeOrigin
    official_key: str | None
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


class RecipePreviewNodeResponse(BaseModel):
    key: str
    node_type: GraphNodeType
    title: str
    position_x: int
    position_y: int


class RecipePreviewEdgeResponse(BaseModel):
    key: str
    source_node_key: str
    target_node_key: str
    role: str
    data_type: str
    order: int


class RecipePreviewGroupResponse(BaseModel):
    key: str
    title: str
    member_keys: list[str]


class RecipePreviewUpdatedNodeResponse(BaseModel):
    id: str
    node_type: GraphNodeType
    title: str
    changed_config_keys: list[str]


class WorkflowRecipePreviewResponse(BaseModel):
    mode: Literal["create", "merge"]
    recipe_id: str
    recipe_version: int
    base_graph_revision: int
    preview_digest: str
    nodes: list[RecipePreviewNodeResponse]
    edges: list[RecipePreviewEdgeResponse]
    groups: list[RecipePreviewGroupResponse]
    updated_nodes: list[RecipePreviewUpdatedNodeResponse]
    required_bindings: list[str]


class WorkflowRecipeApplicationResponse(BaseModel):
    created: bool
    recipe_id: str
    recipe_version_id: str
    recipe_version: int
    mode: Literal["create", "merge"]
    graph: GraphProjectionResponse
    added_node_ids: list[str]
    added_edge_ids: list[str]
    updated_node_ids: list[str]
    base_graph_revision: int | None
    preview_digest: str | None
    required_bindings: list[str]


def serialize_workflow_recipe_version(
    version: Any,
) -> WorkflowRecipeVersionResponse:
    return WorkflowRecipeVersionResponse(
        id=version.id,
        recipe_id=version.recipe_id,
        version=version.version,
        schema_version=version.schema_version,
        catalog_version=version.catalog_version,
        creation_source=version.creation_source,
        title=version.title,
        description=version.description,
        payload=parse_recipe_payload_or_raise(version),
        payload_hash=version.payload_hash,
        governance=parse_recipe_governance(version.governance_json),
        preferred_visual_system_version_id=version.preferred_visual_system_version_id,
        created_at=version.created_at,
    )


def serialize_workflow_recipe_summary(
    recipe: Any,
) -> WorkflowRecipeSummaryResponse:
    if recipe.current_version is None or recipe.current_version_id is None:
        raise ValueError("Any 缺少 current version")
    return WorkflowRecipeSummaryResponse(
        id=recipe.id,
        kind=recipe.kind,
        origin=recipe.origin,
        official_key=recipe.official_key,
        current_version_id=recipe.current_version_id,
        current_version=serialize_workflow_recipe_version(recipe.current_version),
        archived_at=recipe.archived_at,
        created_at=recipe.created_at,
        updated_at=recipe.updated_at,
    )


def serialize_workflow_recipe(recipe: Any) -> WorkflowRecipeResponse:
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
    graph: GraphProjectionResponse,
) -> WorkflowRecipeApplicationResponse:
    return WorkflowRecipeApplicationResponse(
        created=result.created,
        recipe_id=result.recipe.id,
        recipe_version_id=result.recipe_version.id,
        recipe_version=result.recipe_version.version,
        mode=result.mode,
        graph=graph,
        added_node_ids=list(result.added_node_ids),
        added_edge_ids=list(result.added_edge_ids),
        updated_node_ids=list(result.updated_node_ids),
        base_graph_revision=result.preview_graph_revision,
        preview_digest=result.preview_digest,
        required_bindings=list(result.required_bindings),
    )


def serialize_workflow_recipe_preview(preview: RecipeApplyPreview) -> WorkflowRecipePreviewResponse:
    return WorkflowRecipePreviewResponse(
        mode=preview.mode,
        recipe_id=preview.recipe_id,
        recipe_version=preview.recipe_version,
        base_graph_revision=preview.base_graph_revision,
        preview_digest=preview.preview_digest,
        nodes=[
            RecipePreviewNodeResponse(
                key=node.key,
                node_type=node.node_type,
                title=node.title,
                position_x=node.position_x,
                position_y=node.position_y,
            )
            for node in preview.nodes
        ],
        edges=[
            RecipePreviewEdgeResponse(
                key=edge.key,
                source_node_key=edge.source_node_key,
                target_node_key=edge.target_node_key,
                role=edge.role,
                data_type=edge.data_type,
                order=edge.order,
            )
            for edge in preview.edges
        ],
        groups=[
            RecipePreviewGroupResponse(
                key=group.key,
                title=group.title,
                member_keys=list(group.member_keys),
            )
            for group in preview.groups
        ],
        updated_nodes=[
            RecipePreviewUpdatedNodeResponse(
                id=node.id,
                node_type=node.node_type,
                title=node.title,
                changed_config_keys=list(node.changed_config_keys),
            )
            for node in preview.updated_nodes
        ],
        required_bindings=list(preview.required_bindings),
    )


__all__ = [
    "AppendWorkflowRecipeVersionRequest",
    "ApplyWorkflowRecipeRequest",
    "CreateWorkflowRecipeRequest",
    "PreviewWorkflowRecipeRequest",
    "RECIPE_SCHEMA_VERSION",
    "WorkflowRecipeArchiveResponse",
    "WorkflowRecipeApplicationResponse",
    "WorkflowRecipePreviewResponse",
    "WorkflowRecipeResponse",
    "WorkflowRecipeSummaryResponse",
    "serialize_workflow_recipe",
    "serialize_workflow_recipe_archive",
    "serialize_workflow_recipe_application",
    "serialize_workflow_recipe_preview",
    "serialize_workflow_recipe_summary",
]
