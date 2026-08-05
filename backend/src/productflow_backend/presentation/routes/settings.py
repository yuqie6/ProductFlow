from __future__ import annotations

import secrets
from pathlib import Path
from typing import Any, NoReturn

from fastapi import APIRouter, Body, Depends, HTTPException, Request, status
from pydantic import ValidationError
from sqlalchemy.orm import Session

from productflow_backend import __version__
from productflow_backend.application.settings import (
    SETTINGS_EXPORT_COMPATIBILITY,
    SETTINGS_EXPORT_SCHEMA_VERSION,
    ProviderBindingView,
    ProviderConfigView,
    ProviderProfileView,
    RuntimeSettingsView,
    SettingsExportView,
    SettingsImportDocument,
    apply_settings_import,
    archive_provider_profile,
    create_provider_profile,
    export_settings,
    get_provider_config_view,
    get_runtime_settings_view,
    preview_settings_import,
    update_provider_binding,
    update_provider_profile,
    update_runtime_settings,
)
from productflow_backend.config import (
    CONFIG_DEFINITIONS,
    get_settings,
    parse_image_tool_allowed_fields,
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


def _public_value(value: Any, *, secret: bool) -> str | int | bool | None:
    if secret:
        return ""
    if isinstance(value, Path):
        return str(value)
    return value


def _serialize_config(view: RuntimeSettingsView) -> ConfigResponse:
    items: list[ConfigItemResponse] = []
    for definition in CONFIG_DEFINITIONS:
        database_value = view.database_values.get(definition.key)
        raw_value = getattr(view.settings, definition.key)
        effective_value = (
            list(parse_image_tool_allowed_fields(raw_value))
            if definition.input_type == "multi_select"
            else _public_value(raw_value, secret=definition.secret)
        )
        items.append(
            ConfigItemResponse(
                key=definition.key,
                label=definition.label,
                category=definition.category,
                input_type=definition.input_type,
                description=definition.description,
                value=effective_value,
                source="database" if database_value is not None else "env_default",
                secret=definition.secret,
                has_value=bool(database_value.value if database_value is not None else raw_value),
                options=[ConfigOptionResponse(value=option.value, label=option.label) for option in definition.options],
                minimum=definition.minimum,
                maximum=definition.maximum,
                updated_at=database_value.updated_at.isoformat() if database_value is not None else None,
            )
        )
    return ConfigResponse(items=items)


def _serialize_provider_profile(profile: ProviderProfileView) -> ProviderProfileResponse:
    return ProviderProfileResponse(
        id=profile.id,
        name=profile.name,
        provider_type=profile.provider_type,
        base_url=profile.base_url,
        capabilities=profile.capabilities,
        default_models=profile.default_models,
        config=profile.config,
        enabled=profile.enabled,
        archived_at=profile.archived_at.isoformat() if profile.archived_at is not None else None,
        has_api_key=profile.has_api_key,
        created_at=profile.created_at.isoformat(),
        updated_at=profile.updated_at.isoformat(),
    )


def _serialize_provider_binding(binding: ProviderBindingView) -> ProviderBindingResponse:
    return ProviderBindingResponse(
        id=binding.id,
        purpose=binding.purpose,
        provider_kind=binding.provider_kind,
        provider_profile_id=binding.provider_profile_id,
        model_settings=binding.model_settings,
        config=binding.config,
        created_at=binding.created_at.isoformat(),
        updated_at=binding.updated_at.isoformat(),
    )


def _serialize_provider_config(view: ProviderConfigView) -> ProviderConfigResponse:
    return ProviderConfigResponse(
        profiles=[_serialize_provider_profile(profile) for profile in view.profiles],
        bindings=[_serialize_provider_binding(binding) for binding in view.bindings],
    )


def _serialize_export(view: SettingsExportView) -> SettingsExportDocument:
    return SettingsExportDocument(
        metadata=SettingsExportMetadataResponse(
            schema_version=SETTINGS_EXPORT_SCHEMA_VERSION,
            exported_at=view.exported_at,
            app="ProductFlow",
            app_version=__version__,
            compatibility=SETTINGS_EXPORT_COMPATIBILITY,
        ),
        runtime_config=view.runtime_config,
        provider_profiles=[
            SettingsProviderProfileExport(
                id=profile.id,
                name=profile.name,
                provider_type=profile.provider_type,
                base_url=profile.base_url,
                api_key=profile.api_key,
                capabilities=profile.capabilities,
                default_models=profile.default_models,
                config=profile.config,
                enabled=profile.enabled,
            )
            for profile in view.provider_profiles
        ],
        provider_bindings=[
            SettingsProviderBindingExport(
                purpose=binding.purpose,
                provider_kind=binding.provider_kind,
                provider_profile_id=binding.provider_profile_id,
                model_settings=binding.model_settings,
                config=binding.config,
            )
            for binding in view.provider_bindings
        ],
    )


def _parse_import_document(payload: Any) -> SettingsImportDocument:
    try:
        document = SettingsExportDocument.model_validate(payload)
    except ValidationError as exc:
        raise ValueError("配置文件格式不正确") from exc
    return SettingsImportDocument(
        schema_version=document.metadata.schema_version,
        compatibility=document.metadata.compatibility,
        runtime_config=dict(document.runtime_config),
        provider_profiles=[profile.model_dump(mode="python") for profile in document.provider_profiles],
        provider_bindings=[binding.model_dump(mode="python") for binding in document.provider_bindings],
    )


def _serialize_import_preview(preview) -> SettingsImportPreviewResponse:
    return SettingsImportPreviewResponse(
        schema_version=preview.schema_version,
        runtime_config_count=preview.runtime_config_count,
        provider_profile_count=preview.provider_profile_count,
        provider_binding_count=preview.provider_binding_count,
        provider_profile_names=preview.provider_profile_names,
        provider_binding_purposes=preview.provider_binding_purposes,
        includes_api_keys=preview.includes_api_keys,
        provider_profiles_with_api_key_count=preview.provider_profiles_with_api_key_count,
    )


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
    return _serialize_config(get_runtime_settings_view(session))


@router.get(
    "/provider-config",
    response_model=ProviderConfigResponse,
    dependencies=[Depends(require_settings_unlocked)],
)
def get_provider_config_endpoint(session: Session = Depends(get_session)) -> ProviderConfigResponse:
    return _serialize_provider_config(get_provider_config_view(session))


@router.get(
    "/export",
    response_model=SettingsExportDocument,
    dependencies=[Depends(require_settings_unlocked)],
)
def export_settings_endpoint(session: Session = Depends(get_session)) -> SettingsExportDocument:
    return _serialize_export(export_settings(session))


@router.post(
    "/import/preview",
    response_model=SettingsImportPreviewResponse,
    dependencies=[Depends(require_settings_unlocked)],
)
def preview_settings_import_endpoint(payload: Any = Body(...)) -> SettingsImportPreviewResponse:
    try:
        bundle = preview_settings_import(_parse_import_document(payload))
    except ValueError as exc:
        _raise_bad_request(exc)
    return _serialize_import_preview(bundle.preview)


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
        bundle = preview_settings_import(_parse_import_document(payload))
        apply_settings_import(session, bundle)
    except ValueError as exc:
        _raise_bad_request(exc)
    return SettingsImportCommitResponse(
        preview=_serialize_import_preview(bundle.preview),
        config=_serialize_config(get_runtime_settings_view(session)),
        provider_config=_serialize_provider_config(get_provider_config_view(session)),
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
        fields_set = payload.model_fields_set
        profile = update_provider_profile(
            session,
            profile_id,
            name=payload.name,
            provider_type=payload.provider_type,
            base_url=payload.base_url,
            base_url_provided="base_url" in fields_set,
            api_key=payload.api_key,
            api_key_provided="api_key" in fields_set,
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
        binding = update_provider_binding(
            session,
            purpose=purpose,
            provider_kind=payload.provider_kind,
            provider_profile_id=payload.provider_profile_id,
            model_settings=payload.model_settings,
            config=payload.config,
        )
    except (RuntimeError, ValueError) as exc:
        _raise_bad_request(exc)
    return _serialize_provider_binding(binding)


@router.get("/runtime", response_model=RuntimeConfigResponse)
def get_runtime_config_endpoint(session: Session = Depends(get_session)) -> RuntimeConfigResponse:
    settings = get_runtime_settings_view(session).settings
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
    try:
        view = update_runtime_settings(session, values=payload.values, reset_keys=payload.reset_keys)
    except ValueError as exc:
        _raise_bad_request(exc)
    return _serialize_config(view)
