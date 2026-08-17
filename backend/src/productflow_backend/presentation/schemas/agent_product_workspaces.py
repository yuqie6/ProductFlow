from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from productflow_backend.application.agent_product_intake import (
    AGENT_PRODUCT_ALLOWED_IMAGE_MIME_TYPES,
    AGENT_PRODUCT_DEFAULT_IMAGE_QUANTITY,
    AGENT_PRODUCT_IMAGE_TYPE_CATALOG,
    AGENT_PRODUCT_SELECTION_SCHEMA_VERSION,
)
from productflow_backend.application.workflow_drafts.contracts import (
    WORKFLOW_DRAFT_MAX_IMAGES_PER_TYPE,
    WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS,
    WORKFLOW_DRAFT_MAX_TOTAL_IMAGES,
    WORKFLOW_DRAFT_MIN_IMAGE_TYPES,
    WORKFLOW_DRAFT_MIN_IMAGES_PER_TYPE,
)
from productflow_backend.presentation.schemas.agent_conversations import AgentConversationResponse
from productflow_backend.presentation.schemas.products import (
    CanonicalProductDetailResponse,
    ProductImageAssetResponse,
)
from productflow_backend.presentation.schemas.workflow_drafts import WorkflowDraftResponse


class AgentProductImageTypeOptionResponse(BaseModel):
    key: str
    title: str
    description: str
    order: int


class AgentProductWorkspaceLimitsResponse(BaseModel):
    min_image_types: int = WORKFLOW_DRAFT_MIN_IMAGE_TYPES
    default_images_per_type: int = AGENT_PRODUCT_DEFAULT_IMAGE_QUANTITY
    min_images_per_type: int = WORKFLOW_DRAFT_MIN_IMAGES_PER_TYPE
    max_images_per_type: int = WORKFLOW_DRAFT_MAX_IMAGES_PER_TYPE
    max_total_images: int = WORKFLOW_DRAFT_MAX_TOTAL_IMAGES
    min_reference_images: int = 1
    max_reference_images: int = WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS
    allowed_image_mime_types: list[str] = list(AGENT_PRODUCT_ALLOWED_IMAGE_MIME_TYPES)


class AgentProductWorkspaceOptionsResponse(BaseModel):
    schema_version: Literal[1] = AGENT_PRODUCT_SELECTION_SCHEMA_VERSION
    image_types: list[AgentProductImageTypeOptionResponse]
    limits: AgentProductWorkspaceLimitsResponse


class AgentProductWorkspaceCreateResponse(BaseModel):
    product: CanonicalProductDetailResponse
    created_assets: list[ProductImageAssetResponse]
    workflow_draft: WorkflowDraftResponse
    conversation: AgentConversationResponse


class AgentProductDraftWorkspaceRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str = Field(min_length=1, max_length=255)
    agent_session_id: str | None = Field(default=None, min_length=1, max_length=36)


class AgentProductWorkspaceSnapshotResponse(BaseModel):
    created: bool
    intake_finalized: bool
    product: CanonicalProductDetailResponse
    created_assets: list[ProductImageAssetResponse]
    workflow_draft: WorkflowDraftResponse
    conversation: AgentConversationResponse


def serialize_agent_product_workspace_options() -> AgentProductWorkspaceOptionsResponse:
    return AgentProductWorkspaceOptionsResponse(
        image_types=[
            AgentProductImageTypeOptionResponse(
                key=option.key,
                title=option.title,
                description=option.description,
                order=option.order,
            )
            for option in AGENT_PRODUCT_IMAGE_TYPE_CATALOG
        ],
        limits=AgentProductWorkspaceLimitsResponse(),
    )


__all__ = [
    "AgentProductDraftWorkspaceRequest",
    "AgentProductWorkspaceCreateResponse",
    "AgentProductWorkspaceOptionsResponse",
    "AgentProductWorkspaceSnapshotResponse",
    "serialize_agent_product_workspace_options",
]
