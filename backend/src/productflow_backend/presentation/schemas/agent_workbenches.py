from __future__ import annotations

from typing import Literal

from pydantic import BaseModel

from productflow_backend.application.agent.workbenches import AgentWorkbenchBootstrap
from productflow_backend.presentation.schemas.agent_conversations import (
    AgentConversationResponse,
    serialize_agent_conversation,
)
from productflow_backend.presentation.schemas.graphs import (
    GraphProjectionResponse,
    serialize_graph_projection,
)
from productflow_backend.presentation.schemas.products import (
    CanonicalProductDetailResponse,
    serialize_canonical_product_detail,
)


class AgentWorkbenchBootstrapResponse(BaseModel):
    mode: Literal["agent"]
    product: CanonicalProductDetailResponse
    conversation: AgentConversationResponse
    graph: GraphProjectionResponse | None
    latest_workflow_revision: int


def serialize_agent_workbench_bootstrap(
    bootstrap: AgentWorkbenchBootstrap,
) -> AgentWorkbenchBootstrapResponse:
    return AgentWorkbenchBootstrapResponse(
        mode="agent",
        product=serialize_canonical_product_detail(bootstrap.product),
        conversation=serialize_agent_conversation(bootstrap.conversation),
        graph=serialize_graph_projection(bootstrap.graph) if bootstrap.graph is not None else None,
        latest_workflow_revision=bootstrap.latest_workflow_revision,
    )


__all__ = [
    "AgentWorkbenchBootstrapResponse",
    "serialize_agent_workbench_bootstrap",
]
