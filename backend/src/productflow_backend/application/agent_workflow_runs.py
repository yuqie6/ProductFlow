from __future__ import annotations

from dataclasses import dataclass

from sqlalchemy.orm import Session

from productflow_backend.application.agent_conversations import get_agent_conversation_by_id_or_raise
from productflow_backend.application.product_workflow.v2_runs import list_v2_workflow_runs
from productflow_backend.application.workflow_drafts.materialization import get_active_v2_workflow_snapshot
from productflow_backend.infrastructure.db.models import WorkflowRun


@dataclass(frozen=True, slots=True)
class AgentWorkflowRunPage:
    workflow_id: str | None
    workflow_revision: int
    runs: tuple[WorkflowRun, ...]


def list_agent_workflow_runs(
    session: Session,
    *,
    conversation_id: str,
    limit: int = 20,
) -> AgentWorkflowRunPage:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    snapshot = get_active_v2_workflow_snapshot(session, product_id=conversation.product_id)
    if snapshot.workflow is None:
        return AgentWorkflowRunPage(
            workflow_id=None,
            workflow_revision=snapshot.latest_revision,
            runs=(),
        )
    listed = list_v2_workflow_runs(
        session,
        product_id=conversation.product_id,
        workflow_id=snapshot.workflow.id,
        limit=limit,
    )
    return AgentWorkflowRunPage(
        workflow_id=listed.workflow.id,
        workflow_revision=listed.workflow.revision,
        runs=listed.runs,
    )


__all__ = ["AgentWorkflowRunPage", "list_agent_workflow_runs"]
