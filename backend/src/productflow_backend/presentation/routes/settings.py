from __future__ import annotations

from pathlib import Path
from typing import Any

from fastapi import APIRouter, Depends, HTTPException
from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.config import (
    CONFIG_DEFINITION_BY_KEY,
    CONFIG_DEFINITIONS,
    RUNTIME_CONFIG_KEYS,
    build_settings_with_overrides,
    get_runtime_settings,
    normalize_config_values,
    normalize_image_size,
    normalize_image_size_list,
)
from productflow_backend.infrastructure.db.models import AppSetting
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.schemas.settings import (
    ConfigItemResponse,
    ConfigOptionResponse,
    ConfigResponse,
    ConfigUpdateRequest,
)

router = APIRouter(prefix="/api/settings", tags=["settings"], dependencies=[Depends(require_admin)])


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
    main_size = normalize_image_size(settings.image_main_image_size, label="主图尺寸")
    promo_poster_size = normalize_image_size(settings.image_promo_poster_size, label="促销海报尺寸")
    allowed_image_sizes = set(normalize_image_size_list(settings.image_allowed_sizes, label="允许生图尺寸"))
    if not allowed_image_sizes:
        raise ValueError("允许生图尺寸不能为空")
    if main_size not in allowed_image_sizes:
        raise ValueError("主图尺寸必须包含在允许生图尺寸列表中")
    if promo_poster_size not in allowed_image_sizes:
        raise ValueError("促销海报尺寸必须包含在允许生图尺寸列表中")
    if not settings.allowed_image_mime_types:
        raise ValueError("允许图片 MIME 不能为空")


def _serialize_config(session: Session) -> ConfigResponse:
    db_values = _load_database_values(session)
    settings = get_runtime_settings()
    items: list[ConfigItemResponse] = []
    for definition in CONFIG_DEFINITIONS:
        source = "database" if definition.key in db_values else "env_default"
        raw_value = getattr(settings, definition.key)
        effective_value = _public_value(raw_value, secret=definition.secret)
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


@router.get("", response_model=ConfigResponse)
def get_config_endpoint(session: Session = Depends(get_session)) -> ConfigResponse:
    return _serialize_config(session)


@router.patch("", response_model=ConfigResponse)
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
