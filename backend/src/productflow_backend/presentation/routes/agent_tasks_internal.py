from __future__ import annotations

from fastapi import APIRouter, Depends
from sqlalchemy.orm import Session

from productflow_backend.application.agent.agent_context import get_agent_task_contract
from productflow_backend.presentation.deps import get_session, require_agent_service
from productflow_backend.presentation.schemas.agent_conversations import AgentContractResponse

router = APIRouter(
    prefix="/api/internal/v1/agent-tasks",
    tags=["agent-internal"],
    dependencies=[Depends(require_agent_service)],
)


@router.get("/{task_id}/contract", response_model=AgentContractResponse)
def get_agent_task_contract_endpoint(
    task_id: str,
    session: Session = Depends(get_session),
) -> AgentContractResponse:
    return AgentContractResponse.model_validate(get_agent_task_contract(session, task_id))


__all__ = ["router"]
