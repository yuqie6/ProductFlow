from __future__ import annotations

import hashlib
from dataclasses import dataclass
from datetime import datetime

import sqlalchemy as sa
from sqlalchemy.orm import Session

from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.db.models import AppSetting

LEGACY_V1_WRITE_FREEZE_SETTING_KEY = "legacy_v1_write_frozen"
LEGACY_V1_WRITE_FREEZE_CONFLICT_DETAIL = (
    "旧工作流已经进入只读维护状态，不能继续编辑、运行、重试或修改旧产物；请使用 Agent 工作流。"
)
LEGACY_V1_WRITE_FREEZE_INVALID_DETAIL = "旧工作流冻结状态无效，系统已按只读状态拒绝旧写入。"

# Transaction-scoped PostgreSQL advisory locks close the race between a request
# reading "unfrozen" and the maintenance command committing the freeze.
_LEGACY_V1_WRITE_LOCK_KEY = int.from_bytes(
    hashlib.sha256(b"productflow:legacy-v1-write-freeze:v1").digest()[:8],
    byteorder="big",
    signed=True,
)


@dataclass(frozen=True, slots=True)
class LegacyV1WriteFreezeState:
    configured: bool
    frozen: bool
    valid: bool
    updated_at: datetime | None


def get_legacy_v1_write_freeze_state(session: Session) -> LegacyV1WriteFreezeState:
    row = session.scalar(
        sa.select(AppSetting)
        .where(AppSetting.key == LEGACY_V1_WRITE_FREEZE_SETTING_KEY)
        .execution_options(populate_existing=True)
    )
    return _state_from_row(row)


def legacy_v1_writes_are_frozen(session: Session) -> bool:
    state = get_legacy_v1_write_freeze_state(session)
    return state.frozen or not state.valid


def ensure_legacy_v1_write_allowed(session: Session) -> LegacyV1WriteFreezeState:
    state = _lock_and_get_state(session)
    if not state.valid:
        raise ConflictError(LEGACY_V1_WRITE_FREEZE_INVALID_DETAIL)
    if state.frozen:
        raise ConflictError(LEGACY_V1_WRITE_FREEZE_CONFLICT_DETAIL)
    return state


def set_legacy_v1_write_freeze_state(
    session: Session,
    *,
    frozen: bool,
) -> LegacyV1WriteFreezeState:
    try:
        _acquire_transaction_lock(session)
        row = session.scalar(
            sa.select(AppSetting)
            .where(AppSetting.key == LEGACY_V1_WRITE_FREEZE_SETTING_KEY)
            .with_for_update()
            .execution_options(populate_existing=True)
        )
        value = "true" if frozen else "false"
        if row is None:
            row = AppSetting(key=LEGACY_V1_WRITE_FREEZE_SETTING_KEY, value=value)
            session.add(row)
        else:
            row.value = value
        session.commit()
        session.refresh(row)
        return _state_from_row(row)
    except Exception:
        session.rollback()
        raise


def _lock_and_get_state(session: Session) -> LegacyV1WriteFreezeState:
    _acquire_transaction_lock(session)
    row = session.scalar(
        sa.select(AppSetting)
        .where(AppSetting.key == LEGACY_V1_WRITE_FREEZE_SETTING_KEY)
        .with_for_update()
        .execution_options(populate_existing=True)
    )
    return _state_from_row(row)


def _acquire_transaction_lock(session: Session) -> None:
    if session.get_bind().dialect.name != "postgresql":
        return
    session.execute(
        sa.text("SELECT pg_advisory_xact_lock(:lock_key)"),
        {"lock_key": _LEGACY_V1_WRITE_LOCK_KEY},
    )


def _state_from_row(row: AppSetting | None) -> LegacyV1WriteFreezeState:
    if row is None:
        return legacy_v1_write_freeze_state_from_value(None)
    return legacy_v1_write_freeze_state_from_value(row.value, updated_at=row.updated_at)


def legacy_v1_write_freeze_state_from_value(
    value: str | None,
    *,
    updated_at: datetime | None = None,
) -> LegacyV1WriteFreezeState:
    if value is None:
        return LegacyV1WriteFreezeState(configured=False, frozen=False, valid=True, updated_at=None)
    normalized = value.strip().lower()
    if normalized == "true":
        return LegacyV1WriteFreezeState(
            configured=True,
            frozen=True,
            valid=True,
            updated_at=updated_at,
        )
    if normalized == "false":
        return LegacyV1WriteFreezeState(
            configured=True,
            frozen=False,
            valid=True,
            updated_at=updated_at,
        )
    return LegacyV1WriteFreezeState(
        configured=True,
        frozen=True,
        valid=False,
        updated_at=updated_at,
    )


__all__ = [
    "LEGACY_V1_WRITE_FREEZE_CONFLICT_DETAIL",
    "LEGACY_V1_WRITE_FREEZE_INVALID_DETAIL",
    "LEGACY_V1_WRITE_FREEZE_SETTING_KEY",
    "LegacyV1WriteFreezeState",
    "ensure_legacy_v1_write_allowed",
    "get_legacy_v1_write_freeze_state",
    "legacy_v1_write_freeze_state_from_value",
    "legacy_v1_writes_are_frozen",
    "set_legacy_v1_write_freeze_state",
]
