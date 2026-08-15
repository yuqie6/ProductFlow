from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException, status
from sqlalchemy.orm import Session

from productflow_backend.infrastructure.provider_config import resolve_agent_provider_config
from productflow_backend.presentation.deps import get_session, require_agent_service
from productflow_backend.presentation.schemas.settings import AgentProviderRuntimeConfigResponse

router = APIRouter(
    prefix="/api/internal/v1/agent-runtime",
    tags=["agent-internal"],
    dependencies=[Depends(require_agent_service)],
)


@router.get("/provider-config", response_model=AgentProviderRuntimeConfigResponse)
def get_agent_provider_runtime_config_endpoint(
    session: Session = Depends(get_session),
) -> AgentProviderRuntimeConfigResponse:
    try:
        config = resolve_agent_provider_config(session)
    except RuntimeError as exc:
        raise HTTPException(status_code=status.HTTP_503_SERVICE_UNAVAILABLE, detail=str(exc)) from exc
    return AgentProviderRuntimeConfigResponse(
        provider_kind=config.provider_kind,
        api_key=config.api_key,
        base_url=config.base_url,
        model=config.model,
        reasoning_effort=config.reasoning_effort,
        reasoning_summary=config.reasoning_summary,
        text_verbosity=config.text_verbosity,
        service_tier=config.service_tier,
    )
