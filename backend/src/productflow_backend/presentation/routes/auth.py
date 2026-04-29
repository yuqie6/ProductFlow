from __future__ import annotations

from fastapi import APIRouter, HTTPException, Request, Response, status

from productflow_backend.config import get_runtime_settings, get_settings
from productflow_backend.presentation.schemas.auth import (
    SessionCreateRequest,
    SessionResponse,
    SessionStateResponse,
)

router = APIRouter(prefix="/api/auth", tags=["auth"])


@router.post("/session", response_model=SessionResponse)
def create_session(payload: SessionCreateRequest, request: Request) -> SessionResponse:
    if not get_runtime_settings().admin_access_required:
        return SessionResponse()
    settings = get_settings()
    if payload.admin_key != settings.admin_access_key:
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="管理员密钥不正确")
    request.session.clear()
    request.session["is_authenticated"] = True
    return SessionResponse()


@router.get("/session", response_model=SessionStateResponse)
def get_session_state(request: Request) -> SessionStateResponse:
    access_required = get_runtime_settings().admin_access_required
    return SessionStateResponse(
        authenticated=not access_required or bool(request.session.get("is_authenticated")),
        access_required=access_required,
    )


@router.delete("/session", response_model=SessionResponse)
def destroy_session(request: Request, response: Response) -> SessionResponse:
    request.session.clear()
    response.delete_cookie("session")
    return SessionResponse()
