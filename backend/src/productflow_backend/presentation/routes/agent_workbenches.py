from __future__ import annotations

from fastapi import APIRouter, Depends, Query
from sqlalchemy.orm import Session

from productflow_backend.application.agent_workbenches import get_agent_workbench_bootstrap
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.schemas.agent_workbenches import (
    AgentWorkbenchBootstrapResponse,
    serialize_agent_workbench_bootstrap,
)

router = APIRouter(
    prefix="/api/v2/products",
    tags=["agent-workbenches"],
    dependencies=[Depends(require_admin)],
)


@router.get("/{product_id}/agent-workbench", response_model=AgentWorkbenchBootstrapResponse)
def get_agent_workbench_bootstrap_endpoint(
    product_id: str,
    agent_session_id: str | None = Query(default=None),
    agent_task_id: str | None = Query(default=None),
    session: Session = Depends(get_session),
) -> AgentWorkbenchBootstrapResponse:
    return serialize_agent_workbench_bootstrap(
        get_agent_workbench_bootstrap(
            session,
            product_id=product_id,
            agent_session_id=agent_session_id,
            agent_task_id=agent_task_id,
        )
    )


__all__ = ["router"]
