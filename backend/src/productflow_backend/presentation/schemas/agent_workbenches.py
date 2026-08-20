from __future__ import annotations

from typing import Literal

from pydantic import BaseModel

from productflow_backend.application.agent.workbenches import AgentWorkbenchBootstrap
from productflow_backend.presentation.schemas.agent_conversations import (
    AgentConversationResponse,
    serialize_agent_conversation,
)
from productflow_backend.presentation.schemas.products import (
    CanonicalProductDetailResponse,
    serialize_canonical_product_detail,
)
from productflow_backend.presentation.schemas.workflow_drafts import (
    ProductWorkflowV2Response,
    WorkflowDraftResponse,
    serialize_product_workflow_v2,
    serialize_workflow_draft,
)


class AgentV2WorkbenchBootstrapResponse(BaseModel):
    mode: Literal["agent_v2"]
    product: CanonicalProductDetailResponse
    conversation: AgentConversationResponse
    workflow_draft: WorkflowDraftResponse
    active_workflow: ProductWorkflowV2Response | None
    latest_workflow_revision: int


AgentWorkbenchBootstrapResponse = AgentV2WorkbenchBootstrapResponse


def serialize_agent_workbench_bootstrap(
    bootstrap: AgentWorkbenchBootstrap,
) -> AgentWorkbenchBootstrapResponse:
    snapshot = bootstrap.active_workflow
    return AgentV2WorkbenchBootstrapResponse(
        mode="agent_v2",
        product=serialize_canonical_product_detail(bootstrap.product),
        conversation=serialize_agent_conversation(bootstrap.conversation),
        workflow_draft=serialize_workflow_draft(bootstrap.workflow_draft),
        active_workflow=(
            serialize_product_workflow_v2(snapshot.workflow)
            if snapshot.workflow is not None
            else None
        ),
        latest_workflow_revision=snapshot.latest_revision,
    )


__all__ = [
    "AgentV2WorkbenchBootstrapResponse",
    "AgentWorkbenchBootstrapResponse",
    "serialize_agent_workbench_bootstrap",
]
