from __future__ import annotations

from fastapi import APIRouter, Depends, Header, Query, status
from sqlalchemy.orm import Session

from productflow_backend.application.agent.workbenches import (
    ensure_agent_workbench_bootstrap,
    get_agent_workbench_bootstrap,
)
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


@router.post(
    "/{product_id}/agent-workbench",
    response_model=AgentWorkbenchBootstrapResponse,
    status_code=status.HTTP_200_OK,
)
def ensure_agent_workbench_bootstrap_endpoint(
    product_id: str,
    agent_session_id: str | None = Query(default=None),
    idempotency_key: str = Header(alias="Idempotency-Key", min_length=1, max_length=200),
    session: Session = Depends(get_session),
) -> AgentWorkbenchBootstrapResponse:
    return serialize_agent_workbench_bootstrap(
        ensure_agent_workbench_bootstrap(
            session,
            product_id=product_id,
            idempotency_key=idempotency_key,
            agent_session_id=agent_session_id,
        )
    )


__all__ = ["router"]
