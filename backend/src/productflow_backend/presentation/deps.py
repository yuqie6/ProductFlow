from __future__ import annotations

import secrets

from fastapi import Depends, HTTPException, Request, status
from sqlalchemy.orm import Session

from productflow_backend.config import get_settings
from productflow_backend.infrastructure.db.session import get_db_session
from productflow_backend.infrastructure.runtime_settings import get_runtime_settings


def get_session(session: Session = Depends(get_db_session)) -> Session:
    return session


def require_admin(request: Request, session: Session = Depends(get_db_session, use_cache=False)) -> None:
    if not get_runtime_settings(session).admin_access_required:
        return
    if not request.session.get("is_authenticated"):
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="请先登录")


def require_deletion_enabled(session: Session = Depends(get_db_session, use_cache=False)) -> None:
    if not get_runtime_settings(session).deletion_enabled:
        raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="删除功能已关闭，请联系管理员")


def require_agent_service(request: Request) -> None:
    expected = get_settings().agent_service_internal_token
    if expected is None:
        raise HTTPException(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
            detail="Agent 内部服务尚未配置",
        )
    scheme, separator, provided = request.headers.get("Authorization", "").partition(" ")
    if separator != " " or scheme.lower() != "bearer" or not secrets.compare_digest(provided, expected):
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Agent 内部服务认证失败",
            headers={"WWW-Authenticate": "Bearer"},
        )
