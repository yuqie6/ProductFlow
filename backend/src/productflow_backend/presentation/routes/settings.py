from __future__ import annotations

import secrets
from dataclasses import dataclass
from pathlib import Path
from typing import Any, NoReturn

from fastapi import APIRouter, Body, Depends, HTTPException, Request, status
from pydantic import ValidationError
from sqlalchemy import delete, select
from sqlalchemy.orm import Session

from productflow_backend import __version__
from productflow_backend.application.time import now_utc
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
from productflow_backend.infrastructure.db.models import AppSetting, ProviderBinding, ProviderProfile
from productflow_backend.infrastructure.provider_config import (
    IMAGE_PROVIDER_KINDS,
    PROVIDER_PURPOSES,
    PROVIDER_TYPES,
    TEXT_PROVIDER_KINDS,
    UNSET_PROVIDER_FIELD,
    archive_provider_profile,
    capability_for_provider_kind,
    create_provider_profile,
    ensure_provider_config_bootstrapped,
    is_real_image_provider_kind,
    list_provider_bindings,
    list_provider_profiles,
    normalize_provider_binding_model_settings,
    normalize_provider_binding_runtime_config,
    update_provider_binding,
    update_provider_profile,
    validate_provider_capabilities,
    validate_provider_profile_contract,
)
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.schemas.settings import (
    ConfigItemResponse,
    ConfigOptionResponse,
    ConfigResponse,
    ConfigUpdateRequest,
    ProviderBindingResponse,
    ProviderBindingUpdateRequest,
    ProviderConfigResponse,
    ProviderProfileCreateRequest,
    ProviderProfileResponse,
    ProviderProfileUpdateRequest,
    RuntimeConfigResponse,
    SettingsExportDocument,
    SettingsExportMetadataResponse,
    SettingsImportCommitResponse,
    SettingsImportPreviewResponse,
    SettingsLockStateResponse,
    SettingsProviderBindingExport,
    SettingsProviderProfileExport,
    SettingsUnlockRequest,
)

router = APIRouter(prefix="/api/settings", tags=["settings"], dependencies=[Depends(require_admin)])
SETTINGS_EXPORT_SCHEMA_VERSION = 1
SETTINGS_EXPORT_COMPATIBILITY = "productflow-settings-v1"


@dataclass(frozen=True, slots=True)
class _SettingsImportBundle:
    normalized_runtime_config: dict[str, str]
    provider_profiles: list[dict[str, Any]]
    provider_bindings: list[dict[str, Any]]
    preview: SettingsImportPreviewResponse


def _settings_token_configured() -> bool:
    token = get_settings().settings_access_token
    return bool(token and token.strip())


def require_settings_unlocked(request: Request) -> None:
    if not _settings_token_configured():
        raise HTTPException(status_code=status.HTTP_503_SERVICE_UNAVAILABLE, detail="设置解锁令牌未配置，请联系管理员")
    if not request.session.get("settings_unlocked"):
        raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="请先解锁系统配置")


def _raise_bad_request(exc: Exception) -> NoReturn:
    raise HTTPException(status_code=400, detail=str(exc)) from exc


def _load_database_values(session: Session) -> dict[str, AppSetting]:
    rows = session.scalars(select(AppSetting).where(AppSetting.key.in_(RUNTIME_CONFIG_KEYS))).all()
    return {row.key: row for row in rows}


def _upsert_app_setting(session: Session, *, key: str, value: str) -> None:
    existing = session.get(AppSetting, key)
    if existing is None:
        session.add(AppSetting(key=key, value=value))
    else:
        existing.value = value


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
        db_value = db_values.get(definition.key)
        has_value = bool(db_value.value if db_value is not None else raw_value)
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
                updated_at=db_value.updated_at.isoformat() if db_value is not None else None,
            )
        )
    return ConfigResponse(items=items)


def _serialize_provider_profile(profile) -> ProviderProfileResponse:
    return ProviderProfileResponse(
        id=profile.id,
        name=profile.name,
        provider_type=profile.provider_type,
        base_url=profile.base_url,
        capabilities=list(profile.capabilities_json or []),
        default_models=dict(profile.default_models_json or {}),
        config=dict(profile.config_json or {}),
        enabled=profile.enabled,
        archived_at=profile.archived_at.isoformat() if profile.archived_at is not None else None,
        has_api_key=bool(profile.api_key),
        created_at=profile.created_at.isoformat(),
        updated_at=profile.updated_at.isoformat(),
    )


def _serialize_provider_binding(binding) -> ProviderBindingResponse:
    return ProviderBindingResponse(
        id=binding.id,
        purpose=binding.purpose,
        provider_kind=binding.provider_kind,
        provider_profile_id=binding.provider_profile_id,
        model_settings=dict(binding.model_settings_json or {}),
        config=dict(binding.config_json or {}),
        created_at=binding.created_at.isoformat(),
        updated_at=binding.updated_at.isoformat(),
    )


def _serialize_provider_config(session: Session) -> ProviderConfigResponse:
    return ProviderConfigResponse(
        profiles=[_serialize_provider_profile(profile) for profile in list_provider_profiles(session)],
        bindings=[_serialize_provider_binding(binding) for binding in list_provider_bindings(session)],
    )


def _export_config_value(value: Any, *, input_type: str) -> str | int | bool | list[str] | None:
    if isinstance(value, Path):
        return str(value)
    if input_type == "multi_select":
        return list(parse_image_tool_allowed_fields(value))
    return value


def _build_settings_export_document(session: Session) -> SettingsExportDocument:
    ensure_provider_config_bootstrapped(session)
    settings = get_runtime_settings()
    runtime_config = {
        definition.key: _export_config_value(getattr(settings, definition.key), input_type=definition.input_type)
        for definition in CONFIG_DEFINITIONS
    }
    profiles = session.scalars(
        select(ProviderProfile)
        .where(ProviderProfile.archived_at.is_(None))
        .order_by(ProviderProfile.created_at, ProviderProfile.name)
    ).all()
    bindings = session.scalars(select(ProviderBinding).order_by(ProviderBinding.purpose)).all()
    return SettingsExportDocument(
        metadata=SettingsExportMetadataResponse(
            schema_version=SETTINGS_EXPORT_SCHEMA_VERSION,
            exported_at=now_utc(),
            app="ProductFlow",
            app_version=__version__,
            compatibility=SETTINGS_EXPORT_COMPATIBILITY,
        ),
        runtime_config=runtime_config,
        provider_profiles=[
            SettingsProviderProfileExport(
                id=profile.id,
                name=profile.name,
                provider_type=profile.provider_type,
                base_url=profile.base_url,
                api_key=profile.api_key,
                capabilities=list(profile.capabilities_json or []),
                default_models=dict(profile.default_models_json or {}),
                config=dict(profile.config_json or {}),
                enabled=profile.enabled,
            )
            for profile in profiles
        ],
        provider_bindings=[
            SettingsProviderBindingExport(
                purpose=binding.purpose,
                provider_kind=binding.provider_kind,
                provider_profile_id=binding.provider_profile_id,
                model_settings=dict(binding.model_settings_json or {}),
                config=dict(binding.config_json or {}),
            )
            for binding in bindings
        ],
    )


def _parse_settings_import_document(payload: Any) -> SettingsExportDocument:
    try:
        document = SettingsExportDocument.model_validate(payload)
    except ValidationError as exc:
        raise ValueError("配置文件格式不正确") from exc
    if document.metadata.schema_version != SETTINGS_EXPORT_SCHEMA_VERSION:
        raise ValueError("配置文件版本不支持")
    if document.metadata.compatibility != SETTINGS_EXPORT_COMPATIBILITY:
        raise ValueError("配置文件兼容标识不支持")
    return document


def _normalize_runtime_import_config(document: SettingsExportDocument) -> dict[str, str]:
    unknown_keys = set(document.runtime_config) - RUNTIME_CONFIG_KEYS
    if unknown_keys:
        raise ValueError(f"未知配置项: {', '.join(sorted(unknown_keys))}")
    missing_keys = RUNTIME_CONFIG_KEYS - set(document.runtime_config)
    if missing_keys:
        raise ValueError(f"配置文件缺少配置项: {', '.join(sorted(missing_keys))}")
    normalized_values = normalize_config_values(document.runtime_config)
    _validate_runtime_settings(normalized_values)
    return normalized_values


def _dedupe_ordered(values: list[str]) -> list[str]:
    result: list[str] = []
    for value in values:
        if value not in result:
            result.append(value)
    return result


def _normalize_optional_text(value: str | None) -> str | None:
    normalized = "" if value is None else str(value).strip()
    return normalized or None


def _normalize_import_profiles(document: SettingsExportDocument) -> list[dict[str, Any]]:
    seen_profile_ids: set[str] = set()
    profiles: list[dict[str, Any]] = []
    for profile in document.provider_profiles:
        if profile.id in seen_profile_ids:
            raise ValueError("供应商档案不能重复")
        seen_profile_ids.add(profile.id)
        if profile.provider_type not in PROVIDER_TYPES:
            raise ValueError("供应商类型不支持")
        capabilities = _dedupe_ordered([str(capability).strip() for capability in profile.capabilities])
        validate_provider_capabilities(capabilities)
        name = profile.name.strip()
        if not name:
            raise ValueError("供应商名称不能为空")
        base_url = _normalize_optional_text(profile.base_url)
        validate_provider_profile_contract(
            provider_type=profile.provider_type,
            capabilities=capabilities,
            base_url=base_url,
        )
        profiles.append(
            {
                "id": profile.id,
                "name": name,
                "provider_type": profile.provider_type,
                "base_url": base_url,
                "api_key": _normalize_optional_text(profile.api_key),
                "capabilities_json": capabilities,
                "default_models_json": dict(profile.default_models),
                "config_json": dict(profile.config),
                "enabled": profile.enabled,
            }
        )
    return profiles


def _normalize_import_bindings(
    document: SettingsExportDocument,
    profiles: list[dict[str, Any]],
) -> list[dict[str, Any]]:
    profiles_by_id = {profile["id"]: profile for profile in profiles}
    seen_purposes: set[str] = set()
    bindings: list[dict[str, Any]] = []
    for binding in document.provider_bindings:
        if binding.purpose in seen_purposes:
            raise ValueError("供应商用途绑定不能重复")
        seen_purposes.add(binding.purpose)
        if binding.purpose not in PROVIDER_PURPOSES:
            raise ValueError("用途必须是 text 或 image")
        allowed_kinds = TEXT_PROVIDER_KINDS if binding.purpose == "text" else IMAGE_PROVIDER_KINDS
        if binding.provider_kind not in allowed_kinds:
            raise ValueError("供应商接口类型不支持当前用途")
        normalized_config = normalize_provider_binding_runtime_config(
            purpose=binding.purpose,
            provider_kind=binding.provider_kind,
            model_settings=binding.model_settings,
            config=binding.config,
        )
        normalized_model_settings = normalize_provider_binding_model_settings(
            purpose=binding.purpose,
            model_settings=binding.model_settings,
        )
        provider_profile_id = binding.provider_profile_id
        if binding.provider_kind == "mock":
            provider_profile_id = None
        else:
            if not provider_profile_id:
                raise ValueError("真实供应商必须选择供应商档案")
            profile = profiles_by_id.get(provider_profile_id)
            if profile is None:
                raise ValueError("供应商不存在")
            if not profile["enabled"]:
                raise ValueError("供应商已停用")
            capability = capability_for_provider_kind(binding.provider_kind)
            if capability not in set(profile["capabilities_json"]):
                raise ValueError("供应商档案不支持当前接口能力")
        bindings.append(
            {
                "purpose": binding.purpose,
                "provider_kind": binding.provider_kind,
                "provider_profile_id": provider_profile_id,
                "model_settings_json": normalized_model_settings,
                "config_json": normalized_config,
            }
        )
    missing_purposes = PROVIDER_PURPOSES - seen_purposes
    if missing_purposes:
        raise ValueError(f"配置文件缺少供应商绑定: {', '.join(sorted(missing_purposes))}")
    return bindings


def _build_settings_import_bundle(payload: Any) -> _SettingsImportBundle:
    document = _parse_settings_import_document(payload)
    normalized_runtime_config = _normalize_runtime_import_config(document)
    profiles = _normalize_import_profiles(document)
    bindings = _normalize_import_bindings(document, profiles)
    if any(
        binding["purpose"] == "image" and is_real_image_provider_kind(binding["provider_kind"])
        for binding in bindings
    ):
        normalized_runtime_config["poster_generation_mode"] = "generated"
    preview = SettingsImportPreviewResponse(
        schema_version=document.metadata.schema_version,
        runtime_config_count=len(normalized_runtime_config),
        provider_profile_count=len(profiles),
        provider_binding_count=len(bindings),
        provider_profile_names=[profile["name"] for profile in profiles],
        provider_binding_purposes=sorted(binding["purpose"] for binding in bindings),
        includes_api_keys=any(bool(profile["api_key"]) for profile in profiles),
        provider_profiles_with_api_key_count=sum(1 for profile in profiles if profile["api_key"]),
    )
    return _SettingsImportBundle(
        normalized_runtime_config=normalized_runtime_config,
        provider_profiles=profiles,
        provider_bindings=bindings,
        preview=preview,
    )


def _apply_settings_import_bundle(session: Session, bundle: _SettingsImportBundle) -> None:
    with session.begin():
        for key, value in bundle.normalized_runtime_config.items():
            existing = session.get(AppSetting, key)
            if existing is None:
                session.add(AppSetting(key=key, value=value))
            else:
                existing.value = value

        session.execute(delete(ProviderBinding))
        session.execute(delete(ProviderProfile))
        session.flush()

        for profile in bundle.provider_profiles:
            session.add(
                ProviderProfile(
                    id=profile["id"],
                    name=profile["name"],
                    provider_type=profile["provider_type"],
                    base_url=profile["base_url"],
                    api_key=profile["api_key"],
                    capabilities_json=profile["capabilities_json"],
                    default_models_json=profile["default_models_json"],
                    config_json=profile["config_json"],
                    enabled=profile["enabled"],
                )
            )
        session.flush()
        for binding in bundle.provider_bindings:
            session.add(
                ProviderBinding(
                    purpose=binding["purpose"],
                    provider_kind=binding["provider_kind"],
                    provider_profile_id=binding["provider_profile_id"],
                    model_settings_json=binding["model_settings_json"],
                    config_json=binding["config_json"],
                )
            )
    session.expire_all()


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


@router.get(
    "/provider-config",
    response_model=ProviderConfigResponse,
    dependencies=[Depends(require_settings_unlocked)],
)
def get_provider_config_endpoint(session: Session = Depends(get_session)) -> ProviderConfigResponse:
    ensure_provider_config_bootstrapped(session)
    return _serialize_provider_config(session)


@router.get(
    "/export",
    response_model=SettingsExportDocument,
    dependencies=[Depends(require_settings_unlocked)],
)
def export_settings_endpoint(session: Session = Depends(get_session)) -> SettingsExportDocument:
    return _build_settings_export_document(session)


@router.post(
    "/import/preview",
    response_model=SettingsImportPreviewResponse,
    dependencies=[Depends(require_settings_unlocked)],
)
def preview_settings_import_endpoint(payload: Any = Body(...)) -> SettingsImportPreviewResponse:
    try:
        bundle = _build_settings_import_bundle(payload)
    except ValueError as exc:
        _raise_bad_request(exc)
    return bundle.preview


@router.post(
    "/import",
    response_model=SettingsImportCommitResponse,
    dependencies=[Depends(require_settings_unlocked)],
)
def import_settings_endpoint(
    payload: Any = Body(...),
    session: Session = Depends(get_session),
) -> SettingsImportCommitResponse:
    try:
        bundle = _build_settings_import_bundle(payload)
        _apply_settings_import_bundle(session, bundle)
    except ValueError as exc:
        session.rollback()
        _raise_bad_request(exc)
    return SettingsImportCommitResponse(
        preview=bundle.preview,
        config=_serialize_config(session),
        provider_config=_serialize_provider_config(session),
    )


@router.post(
    "/provider-profiles",
    response_model=ProviderProfileResponse,
    dependencies=[Depends(require_settings_unlocked)],
)
def create_provider_profile_endpoint(
    payload: ProviderProfileCreateRequest,
    session: Session = Depends(get_session),
) -> ProviderProfileResponse:
    try:
        ensure_provider_config_bootstrapped(session)
        profile = create_provider_profile(
            session,
            name=payload.name,
            provider_type=payload.provider_type,
            base_url=payload.base_url,
            api_key=payload.api_key,
            capabilities=payload.capabilities,
            default_models=payload.default_models,
            config=payload.config,
            enabled=payload.enabled,
        )
    except ValueError as exc:
        _raise_bad_request(exc)
    return _serialize_provider_profile(profile)


@router.patch(
    "/provider-profiles/{profile_id}",
    response_model=ProviderProfileResponse,
    dependencies=[Depends(require_settings_unlocked)],
)
def update_provider_profile_endpoint(
    profile_id: str,
    payload: ProviderProfileUpdateRequest,
    session: Session = Depends(get_session),
) -> ProviderProfileResponse:
    try:
        ensure_provider_config_bootstrapped(session)
        fields_set = payload.model_fields_set
        profile = update_provider_profile(
            session,
            profile_id,
            name=payload.name,
            provider_type=payload.provider_type,
            base_url=payload.base_url if "base_url" in fields_set else UNSET_PROVIDER_FIELD,
            api_key=payload.api_key if "api_key" in fields_set else UNSET_PROVIDER_FIELD,
            capabilities=payload.capabilities,
            default_models=payload.default_models,
            config=payload.config,
            enabled=payload.enabled,
        )
    except ValueError as exc:
        _raise_bad_request(exc)
    return _serialize_provider_profile(profile)


@router.delete(
    "/provider-profiles/{profile_id}",
    response_model=ProviderProfileResponse,
    dependencies=[Depends(require_settings_unlocked)],
)
def archive_provider_profile_endpoint(
    profile_id: str,
    session: Session = Depends(get_session),
) -> ProviderProfileResponse:
    try:
        ensure_provider_config_bootstrapped(session)
        profile = archive_provider_profile(session, profile_id)
    except ValueError as exc:
        _raise_bad_request(exc)
    return _serialize_provider_profile(profile)


@router.patch(
    "/provider-bindings/{purpose}",
    response_model=ProviderBindingResponse,
    dependencies=[Depends(require_settings_unlocked)],
)
def update_provider_binding_endpoint(
    purpose: str,
    payload: ProviderBindingUpdateRequest,
    session: Session = Depends(get_session),
) -> ProviderBindingResponse:
    try:
        ensure_provider_config_bootstrapped(session)
        binding = update_provider_binding(
            session,
            purpose=purpose,
            provider_kind=payload.provider_kind,
            provider_profile_id=payload.provider_profile_id,
            model_settings=payload.model_settings,
            config=payload.config,
            commit=False,
        )
        if binding.purpose == "image" and is_real_image_provider_kind(binding.provider_kind):
            _upsert_app_setting(session, key="poster_generation_mode", value="generated")
        session.commit()
        session.refresh(binding)
    except (RuntimeError, ValueError) as exc:
        session.rollback()
        _raise_bad_request(exc)
    return _serialize_provider_binding(binding)


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
        next_values = {key: row.value for key, row in current_values.items() if key not in reset_keys}
        next_values.update(normalized_values)
        _validate_runtime_settings(next_values)
    except ValueError as exc:
        _raise_bad_request(exc)

    for key in reset_keys:
        existing = session.get(AppSetting, key)
        if existing is not None:
            session.delete(existing)
    for key, value in normalized_values.items():
        _upsert_app_setting(session, key=key, value=value)
    session.commit()
    return _serialize_config(session)
