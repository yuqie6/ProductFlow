from __future__ import annotations

from sqlalchemy import select
from sqlalchemy.exc import SQLAlchemyError
from sqlalchemy.orm import Session

from productflow_backend.config import RUNTIME_CONFIG_KEYS
from productflow_backend.infrastructure.db.models import AppSetting
from productflow_backend.infrastructure.db.session import get_session_factory


def load_runtime_overrides(session: Session | None = None) -> dict[str, str]:
    """Read database-backed runtime settings, falling back when the table is unavailable."""

    if session is not None:
        return _load_runtime_overrides(session)

    owned_session = get_session_factory()()
    try:
        return _load_runtime_overrides(owned_session)
    finally:
        owned_session.close()


def _load_runtime_overrides(session: Session) -> dict[str, str]:
    try:
        with session.begin_nested():
            rows = session.scalars(select(AppSetting).where(AppSetting.key.in_(RUNTIME_CONFIG_KEYS))).all()
    except SQLAlchemyError:
        return {}
    return {row.key: row.value for row in rows}
