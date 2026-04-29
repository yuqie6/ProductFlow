from __future__ import annotations

import secrets
from pathlib import Path
from typing import Any

from fastapi import APIRouter, Depends, HTTPException, Request, status
from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.config import (
    CONFIG_DEFINITION_BY_KEY,
    CONFIG_DEFINITIONS,
    RUNTIME_CONFIG_KEYS,
    build_settings_with_overrides,
    get_runtime_settings,
    get_settings,
    normalize_config_values,
    normalize_image_generation_size,
    parse_image_tool_allowed_fields,
)
from productflow_backend.infrastructure.db.models import AppSetting
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.schemas.settings import (
    ConfigItemResponse,
    ConfigOptionResponse,
    ConfigResponse,
    ConfigUpdateRequest,
    RuntimeConfigResponse,
    SettingsLockStateResponse,
    SettingsUnlockRequest,
)

router = APIRouter(prefix="/api/settings", tags=["settings"], dependencies=[Depends(require_admin)])


def _settings_token_configured() -> bool:
    token = get_settings().settings_access_token
    return bool(token and token.strip())


def require_settings_unlocked(request: Request) -> None:
    if not _settings_token_configured():
        raise HTTPException(status_code=status.HTTP_503_SERVICE_UNAVAILABLE, detail="设置解锁令牌未配置，请联系管理员")
    if not request.session.get("settings_unlocked"):
        raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="请先解锁系统配置")


def _load_database_values(session: Session) -> dict[str, str]:
    rows = session.scalars(select(AppSetting).where(AppSetting.key.in_(RUNTIME_CONFIG_KEYS))).all()
    return {row.key: row.value for row in rows}


def _public_value(value: Any, *, secret: bool) -> str | int | bool | None:
    if secret:
        return ""
    if isinstance(value, Path):
        return str(value)
    return value


def _validate_runtime_settings(overrides: dict[str, str]) -> None:
    settings = build_settings_with_overrides(overrides)
    normalize_image_generation_size(settings.image_main_image_size, label="主图尺寸")
    normalize_image_generation_size(settings.image_promo_poster_size, label="促销海报尺寸")
    if not settings.allowed_image_mime_types:
        raise ValueError("允许图片 MIME 不能为空")


def _serialize_config(session: Session) -> ConfigResponse:
    db_values = _load_database_values(session)
    settings = get_runtime_settings()
    items: list[ConfigItemResponse] = []
    for definition in CONFIG_DEFINITIONS:
        source = "database" if definition.key in db_values else "env_default"
        raw_value = getattr(settings, definition.key)
        effective_value = (
            list(parse_image_tool_allowed_fields(raw_value))
            if definition.input_type == "multi_select"
            else _public_value(raw_value, secret=definition.secret)
        )
        has_value = bool(db_values.get(definition.key) if source == "database" else raw_value)
        items.append(
            ConfigItemResponse(
                key=definition.key,
                label=definition.label,
                category=definition.category,
                input_type=definition.input_type,
                description=definition.description,
                value=effective_value,
                source=source,
                secret=definition.secret,
                has_value=has_value,
                options=[ConfigOptionResponse(value=option.value, label=option.label) for option in definition.options],
                minimum=definition.minimum,
                maximum=definition.maximum,
            )
        )
    return ConfigResponse(items=items)


@router.get("/lock-state", response_model=SettingsLockStateResponse)
def get_settings_lock_state_endpoint(request: Request) -> SettingsLockStateResponse:
    configured = _settings_token_configured()
    return SettingsLockStateResponse(
        unlocked=configured and bool(request.session.get("settings_unlocked")),
        configured=configured,
    )


@router.post("/unlock", response_model=SettingsLockStateResponse)
def unlock_settings_endpoint(payload: SettingsUnlockRequest, request: Request) -> SettingsLockStateResponse:
    expected_token = (get_settings().settings_access_token or "").strip()
    if not expected_token:
        raise HTTPException(status_code=503, detail="设置解锁令牌未配置，请联系管理员")
    if not secrets.compare_digest(payload.token, expected_token):
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="设置解锁令牌不正确")
    request.session["settings_unlocked"] = True
    return SettingsLockStateResponse(unlocked=True, configured=True)


@router.get("", response_model=ConfigResponse, dependencies=[Depends(require_settings_unlocked)])
def get_config_endpoint(session: Session = Depends(get_session)) -> ConfigResponse:
    return _serialize_config(session)


@router.get("/runtime", response_model=RuntimeConfigResponse)
def get_runtime_config_endpoint() -> RuntimeConfigResponse:
    settings = get_runtime_settings()
    return RuntimeConfigResponse(
        image_generation_max_dimension=settings.image_generation_max_dimension,
        image_tool_allowed_fields=list(parse_image_tool_allowed_fields(settings.image_tool_allowed_fields)),
        admin_access_required=settings.admin_access_required,
        deletion_enabled=settings.deletion_enabled,
    )


@router.patch("", response_model=ConfigResponse, dependencies=[Depends(require_settings_unlocked)])
def update_config_endpoint(
    payload: ConfigUpdateRequest,
    session: Session = Depends(get_session),
) -> ConfigResponse:
    unknown_keys = (set(payload.values) | set(payload.reset_keys)) - set(CONFIG_DEFINITION_BY_KEY)
    if unknown_keys:
        raise HTTPException(status_code=400, detail=f"未知配置项: {', '.join(sorted(unknown_keys))}")

    reset_keys = set(payload.reset_keys)
    if reset_keys & set(payload.values):
        raise HTTPException(status_code=400, detail="同一个配置项不能同时更新和恢复默认")

    try:
        normalized_values = normalize_config_values(payload.values)
        current_values = _load_database_values(session)
        next_values = {key: value for key, value in current_values.items() if key not in reset_keys}
        next_values.update(normalized_values)
        _validate_runtime_settings(next_values)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc

    for key in reset_keys:
        existing = session.get(AppSetting, key)
        if existing is not None:
            session.delete(existing)
    for key, value in normalized_values.items():
        existing = session.get(AppSetting, key)
        if existing is None:
            session.add(AppSetting(key=key, value=value))
        else:
            existing.value = value
    session.commit()
    return _serialize_config(session)
