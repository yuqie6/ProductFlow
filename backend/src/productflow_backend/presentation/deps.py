from __future__ import annotations

from fastapi import Depends, HTTPException, Request, status
from sqlalchemy.orm import Session

from productflow_backend.config import get_runtime_settings
from productflow_backend.infrastructure.db.session import get_db_session


def get_session(session: Session = Depends(get_db_session)) -> Session:
    return session


def require_admin(request: Request) -> None:
    if not request.session.get("is_authenticated"):
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="请先登录")


def require_deletion_enabled() -> None:
    if not get_runtime_settings().deletion_enabled:
        raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="删除功能已关闭，请联系管理员")
