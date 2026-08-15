from __future__ import annotations

from collections.abc import Iterable, Mapping
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Any

from sqlalchemy import delete, select
from sqlalchemy.orm import Session

from productflow_backend.application.runtime_settings import get_runtime_settings
from productflow_backend.application.time import now_utc
from productflow_backend.config import (
    RUNTIME_CONFIG_KEYS,
    Settings,
    build_settings_with_overrides,
    normalize_config_values,
)
from productflow_backend.infrastructure.db.models import AppSetting, ProviderBinding, ProviderProfile
from productflow_backend.infrastructure.provider_config import (
    PROVIDER_PURPOSES,
    PROVIDER_TYPES,
    UNSET_PROVIDER_FIELD,
    capability_for_provider_kind,
    ensure_provider_bindings_initialized,
    list_provider_bindings,
    list_provider_profiles,
    normalize_provider_binding_model_settings,
    normalize_provider_binding_runtime_config,
    provider_config_tables_available,
    provider_kinds_for_purpose,
    validate_provider_capabilities,
    validate_provider_profile_contract,
)
from productflow_backend.infrastructure.provider_config import (
    archive_provider_profile as persist_archived_provider_profile,
)
from productflow_backend.infrastructure.provider_config import (
    create_provider_profile as persist_provider_profile,
)
from productflow_backend.infrastructure.provider_config import (
    update_provider_binding as persist_provider_binding,
)
from productflow_backend.infrastructure.provider_config import (
    update_provider_profile as persist_updated_provider_profile,
)

SETTINGS_EXPORT_SCHEMA_VERSION = 3
SETTINGS_EXPORT_COMPATIBILITY = "productflow-settings-v3"
REQUIRED_PROVIDER_PURPOSES = PROVIDER_PURPOSES


@dataclass(frozen=True, slots=True)
class RuntimeSettingValue:
    value: str
    updated_at: datetime


@dataclass(frozen=True, slots=True)
class RuntimeSettingsView:
    settings: Settings
    database_values: dict[str, RuntimeSettingValue]


@dataclass(frozen=True, slots=True)
class ProviderProfileView:
    id: str
    name: str
    provider_type: str
    base_url: str | None
    capabilities: list[str]
    default_models: dict[str, Any]
    config: dict[str, Any]
    enabled: bool
    archived_at: datetime | None
    has_api_key: bool
    created_at: datetime
    updated_at: datetime


@dataclass(frozen=True, slots=True)
class ProviderBindingView:
    id: str
    purpose: str
    provider_kind: str
    provider_profile_id: str | None
    model_settings: dict[str, Any]
    config: dict[str, Any]
    created_at: datetime
    updated_at: datetime


@dataclass(frozen=True, slots=True)
class ProviderConfigView:
    profiles: list[ProviderProfileView]
    bindings: list[ProviderBindingView]


@dataclass(frozen=True, slots=True)
class ProviderProfileExport:
    id: str
    name: str
    provider_type: str
    base_url: str | None
    api_key: str | None
    capabilities: list[str]
    default_models: dict[str, Any]
    config: dict[str, Any]
    enabled: bool


@dataclass(frozen=True, slots=True)
class ProviderBindingExport:
    purpose: str
    provider_kind: str
    provider_profile_id: str | None
    model_settings: dict[str, Any]
    config: dict[str, Any]


@dataclass(frozen=True, slots=True)
class SettingsExportView:
    exported_at: datetime
    runtime_config: dict[str, Any]
    provider_profiles: list[ProviderProfileExport]
    provider_bindings: list[ProviderBindingExport]


@dataclass(frozen=True, slots=True)
class SettingsImportDocument:
    schema_version: int
    compatibility: str
    runtime_config: dict[str, Any]
    provider_profiles: list[dict[str, Any]]
    provider_bindings: list[dict[str, Any]]


@dataclass(frozen=True, slots=True)
class SettingsImportPreview:
    schema_version: int
    runtime_config_count: int
    provider_profile_count: int
    provider_binding_count: int
    provider_profile_names: list[str]
    provider_binding_purposes: list[str]
    includes_api_keys: bool
    provider_profiles_with_api_key_count: int


@dataclass(frozen=True, slots=True)
class SettingsImportBundle:
    normalized_runtime_config: dict[str, str]
    provider_profiles: list[dict[str, Any]]
    provider_bindings: list[dict[str, Any]]
    preview: SettingsImportPreview


def get_runtime_settings_view(session: Session) -> RuntimeSettingsView:
    rows = session.scalars(select(AppSetting).where(AppSetting.key.in_(RUNTIME_CONFIG_KEYS))).all()
    return RuntimeSettingsView(
        settings=get_runtime_settings(session),
        database_values={
            row.key: RuntimeSettingValue(value=row.value, updated_at=row.updated_at)
            for row in rows
        },
    )


def get_provider_config_view(session: Session) -> ProviderConfigView:
    return ProviderConfigView(
        profiles=[_provider_profile_view(profile) for profile in list_provider_profiles(session)],
        bindings=[_provider_binding_view(binding) for binding in list_provider_bindings(session)],
    )


def initialize_provider_bindings_if_available() -> bool:
    """Initialize current-purpose bindings when provider tables already exist."""

    if not provider_config_tables_available():
        return False
    ensure_provider_bindings_initialized()
    return True


def export_settings(session: Session) -> SettingsExportView:
    settings = get_runtime_settings(session)
    runtime_config = {
        definition.key: _export_config_value(getattr(settings, definition.key), input_type=definition.input_type)
        for definition in _config_definitions()
    }
    profiles = session.scalars(
        select(ProviderProfile)
        .where(ProviderProfile.archived_at.is_(None))
        .order_by(ProviderProfile.created_at, ProviderProfile.name)
    ).all()
    bindings = session.scalars(select(ProviderBinding).order_by(ProviderBinding.purpose)).all()
    return SettingsExportView(
        exported_at=now_utc(),
        runtime_config=runtime_config,
        provider_profiles=[_provider_profile_export(profile) for profile in profiles],
        provider_bindings=[_provider_binding_export(binding) for binding in bindings],
    )


def update_runtime_settings(
    session: Session,
    *,
    values: Mapping[str, Any],
    reset_keys: Iterable[str],
) -> RuntimeSettingsView:
    reset_key_set = set(reset_keys)
    unknown_keys = (set(values) | reset_key_set) - set(_config_definition_by_key())
    if unknown_keys:
        raise ValueError(f"未知配置项: {', '.join(sorted(unknown_keys))}")
    if reset_key_set & set(values):
        raise ValueError("同一个配置项不能同时更新和恢复默认")

    normalized_values = normalize_config_values(dict(values))
    current_values = _load_database_values(session)
    next_values = {key: row.value for key, row in current_values.items() if key not in reset_key_set}
    next_values.update(normalized_values)
    _validate_runtime_settings(next_values)

    def apply() -> None:
        for key in reset_key_set:
            existing = session.get(AppSetting, key)
            if existing is not None:
                session.delete(existing)
        for key, value in normalized_values.items():
            _upsert_app_setting(session, key=key, value=value)

    _run_transaction(session, apply)
    return get_runtime_settings_view(session)


def create_provider_profile(
    session: Session,
    *,
    name: str,
    provider_type: str,
    base_url: str | None,
    api_key: str | None,
    capabilities: list[str],
    default_models: dict[str, Any],
    config: dict[str, Any],
    enabled: bool,
) -> ProviderProfileView:
    def apply() -> ProviderProfile:
        ensure_provider_bindings_initialized(session, commit=False)
        return persist_provider_profile(
            session,
            name=name,
            provider_type=provider_type,
            base_url=base_url,
            api_key=api_key,
            capabilities=capabilities,
            default_models=default_models,
            config=config,
            enabled=enabled,
            commit=False,
        )

    profile = _run_transaction(session, apply)
    return _provider_profile_view(profile)


def update_provider_profile(
    session: Session,
    profile_id: str,
    *,
    name: str | None,
    provider_type: str | None,
    base_url: str | None,
    base_url_provided: bool,
    api_key: str | None,
    api_key_provided: bool,
    capabilities: list[str] | None,
    default_models: dict[str, Any] | None,
    config: dict[str, Any] | None,
    enabled: bool | None,
) -> ProviderProfileView:
    def apply() -> ProviderProfile:
        ensure_provider_bindings_initialized(session, commit=False)
        return persist_updated_provider_profile(
            session,
            profile_id,
            name=name,
            provider_type=provider_type,
            base_url=base_url if base_url_provided else UNSET_PROVIDER_FIELD,
            api_key=api_key if api_key_provided else UNSET_PROVIDER_FIELD,
            capabilities=capabilities,
            default_models=default_models,
            config=config,
            enabled=enabled,
            commit=False,
        )

    profile = _run_transaction(session, apply)
    return _provider_profile_view(profile)


def archive_provider_profile(session: Session, profile_id: str) -> ProviderProfileView:
    def apply() -> ProviderProfile:
        ensure_provider_bindings_initialized(session, commit=False)
        return persist_archived_provider_profile(session, profile_id, commit=False)

    profile = _run_transaction(session, apply)
    return _provider_profile_view(profile)


def update_provider_binding(
    session: Session,
    *,
    purpose: str,
    provider_kind: str,
    provider_profile_id: str | None,
    model_settings: dict[str, Any],
    config: dict[str, Any],
) -> ProviderBindingView:
    def apply() -> ProviderBinding:
        ensure_provider_bindings_initialized(session, commit=False)
        binding = persist_provider_binding(
            session,
            purpose=purpose,
            provider_kind=provider_kind,
            provider_profile_id=provider_profile_id,
            model_settings=model_settings,
            config=config,
            commit=False,
        )
        session.flush()
        return binding

    binding = _run_transaction(session, apply)
    return _provider_binding_view(binding)


def preview_settings_import(document: SettingsImportDocument) -> SettingsImportBundle:
    if document.schema_version != SETTINGS_EXPORT_SCHEMA_VERSION:
        raise ValueError("配置文件版本不支持")
    if document.compatibility != SETTINGS_EXPORT_COMPATIBILITY:
        raise ValueError("配置文件兼容标识不支持")

    normalized_runtime_config = _normalize_runtime_import_config(document.runtime_config)
    profiles = _normalize_import_profiles(document.provider_profiles)
    bindings = _normalize_import_bindings(document.provider_bindings, profiles)
    preview = SettingsImportPreview(
        schema_version=document.schema_version,
        runtime_config_count=len(normalized_runtime_config),
        provider_profile_count=len(profiles),
        provider_binding_count=len(bindings),
        provider_profile_names=[profile["name"] for profile in profiles],
        provider_binding_purposes=sorted(binding["purpose"] for binding in bindings),
        includes_api_keys=any(bool(profile["api_key"]) for profile in profiles),
        provider_profiles_with_api_key_count=sum(1 for profile in profiles if profile["api_key"]),
    )
    return SettingsImportBundle(
        normalized_runtime_config=normalized_runtime_config,
        provider_profiles=profiles,
        provider_bindings=bindings,
        preview=preview,
    )


def apply_settings_import(session: Session, bundle: SettingsImportBundle) -> None:
    def apply() -> None:
        for key, value in bundle.normalized_runtime_config.items():
            _upsert_app_setting(session, key=key, value=value)

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

    _run_transaction(session, apply)
    session.expire_all()


def _run_transaction(session: Session, operation):
    try:
        if session.in_transaction():
            session.rollback()
        with session.begin():
            return operation()
    except Exception:
        session.rollback()
        raise


def _load_database_values(session: Session) -> dict[str, AppSetting]:
    rows = session.scalars(select(AppSetting).where(AppSetting.key.in_(RUNTIME_CONFIG_KEYS))).all()
    return {row.key: row for row in rows}


def _upsert_app_setting(session: Session, *, key: str, value: str) -> None:
    existing = session.get(AppSetting, key)
    if existing is None:
        session.add(AppSetting(key=key, value=value))
    else:
        existing.value = value


def _validate_runtime_settings(overrides: dict[str, str]) -> None:
    settings = build_settings_with_overrides(overrides)
    if not settings.allowed_image_mime_types:
        raise ValueError("允许图片 MIME 不能为空")


def _normalize_runtime_import_config(runtime_config: Mapping[str, Any]) -> dict[str, str]:
    unknown_keys = set(runtime_config) - RUNTIME_CONFIG_KEYS
    if unknown_keys:
        raise ValueError(f"未知配置项: {', '.join(sorted(unknown_keys))}")
    missing_keys = RUNTIME_CONFIG_KEYS - set(runtime_config)
    if missing_keys:
        raise ValueError(f"配置文件缺少配置项: {', '.join(sorted(missing_keys))}")
    normalized_values = normalize_config_values(dict(runtime_config))
    _validate_runtime_settings(normalized_values)
    return normalized_values


def _normalize_import_profiles(profiles: list[dict[str, Any]]) -> list[dict[str, Any]]:
    seen_profile_ids: set[str] = set()
    normalized: list[dict[str, Any]] = []
    for profile in profiles:
        profile_id = str(profile["id"])
        if profile_id in seen_profile_ids:
            raise ValueError("供应商档案不能重复")
        seen_profile_ids.add(profile_id)
        provider_type = profile["provider_type"]
        if provider_type not in PROVIDER_TYPES:
            raise ValueError("供应商类型不支持")
        capabilities = _dedupe_ordered([str(value).strip() for value in profile.get("capabilities", [])])
        validate_provider_capabilities(capabilities)
        name = str(profile["name"]).strip()
        if not name:
            raise ValueError("供应商名称不能为空")
        base_url = _normalize_optional_text(profile.get("base_url"))
        validate_provider_profile_contract(
            provider_type=provider_type,
            capabilities=capabilities,
            base_url=base_url,
        )
        normalized.append(
            {
                "id": profile_id,
                "name": name,
                "provider_type": provider_type,
                "base_url": base_url,
                "api_key": _normalize_optional_text(profile.get("api_key")),
                "capabilities_json": capabilities,
                "default_models_json": dict(profile.get("default_models") or {}),
                "config_json": dict(profile.get("config") or {}),
                "enabled": bool(profile.get("enabled", True)),
            }
        )
    return normalized


def _normalize_import_bindings(
    bindings: list[dict[str, Any]],
    profiles: list[dict[str, Any]],
) -> list[dict[str, Any]]:
    profiles_by_id = {profile["id"]: profile for profile in profiles}
    seen_purposes: set[str] = set()
    normalized: list[dict[str, Any]] = []
    for binding in bindings:
        purpose = binding["purpose"]
        if purpose in seen_purposes:
            raise ValueError("供应商用途绑定不能重复")
        seen_purposes.add(purpose)
        if purpose not in PROVIDER_PURPOSES:
            raise ValueError("用途必须是 prompt、agent 或 image")
        provider_kind = binding["provider_kind"]
        allowed_kinds = provider_kinds_for_purpose(purpose)
        if provider_kind not in allowed_kinds:
            raise ValueError("供应商接口类型不支持当前用途")
        model_settings = dict(binding.get("model_settings") or {})
        config = dict(binding.get("config") or {})
        normalized_config = normalize_provider_binding_runtime_config(
            purpose=purpose,
            provider_kind=provider_kind,
            model_settings=model_settings,
            config=config,
        )
        normalized_model_settings = normalize_provider_binding_model_settings(
            purpose=purpose,
            model_settings=model_settings,
        )
        provider_profile_id = binding.get("provider_profile_id")
        if provider_kind == "mock":
            provider_profile_id = None
        else:
            if not provider_profile_id:
                raise ValueError("真实供应商必须选择供应商档案")
            profile = profiles_by_id.get(provider_profile_id)
            if profile is None:
                raise ValueError("供应商不存在")
            if not profile["enabled"]:
                raise ValueError("供应商已停用")
            capability = capability_for_provider_kind(provider_kind)
            if capability not in set(profile["capabilities_json"]):
                raise ValueError("供应商档案不支持当前接口能力")
        normalized.append(
            {
                "purpose": purpose,
                "provider_kind": provider_kind,
                "provider_profile_id": provider_profile_id,
                "model_settings_json": normalized_model_settings,
                "config_json": normalized_config,
            }
        )
    missing_purposes = REQUIRED_PROVIDER_PURPOSES - seen_purposes
    if missing_purposes:
        raise ValueError(f"配置文件缺少供应商绑定: {', '.join(sorted(missing_purposes))}")
    return normalized


def _config_definitions():
    from productflow_backend.config import CONFIG_DEFINITIONS

    return CONFIG_DEFINITIONS


def _config_definition_by_key():
    from productflow_backend.config import CONFIG_DEFINITION_BY_KEY

    return CONFIG_DEFINITION_BY_KEY


def _export_config_value(value: Any, *, input_type: str) -> str | int | bool | list[str] | None:
    if isinstance(value, Path):
        return str(value)
    if input_type == "multi_select":
        from productflow_backend.config import parse_image_tool_allowed_fields

        return list(parse_image_tool_allowed_fields(value))
    return value


def _provider_profile_view(profile: ProviderProfile) -> ProviderProfileView:
    return ProviderProfileView(
        id=profile.id,
        name=profile.name,
        provider_type=profile.provider_type,
        base_url=profile.base_url,
        capabilities=list(profile.capabilities_json or []),
        default_models=dict(profile.default_models_json or {}),
        config=dict(profile.config_json or {}),
        enabled=profile.enabled,
        archived_at=profile.archived_at,
        has_api_key=bool(profile.api_key),
        created_at=profile.created_at,
        updated_at=profile.updated_at,
    )


def _provider_binding_view(binding: ProviderBinding) -> ProviderBindingView:
    return ProviderBindingView(
        id=binding.id,
        purpose=binding.purpose,
        provider_kind=binding.provider_kind,
        provider_profile_id=binding.provider_profile_id,
        model_settings=dict(binding.model_settings_json or {}),
        config=dict(binding.config_json or {}),
        created_at=binding.created_at,
        updated_at=binding.updated_at,
    )


def _provider_profile_export(profile: ProviderProfile) -> ProviderProfileExport:
    return ProviderProfileExport(
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


def _provider_binding_export(binding: ProviderBinding) -> ProviderBindingExport:
    return ProviderBindingExport(
        purpose=binding.purpose,
        provider_kind=binding.provider_kind,
        provider_profile_id=binding.provider_profile_id,
        model_settings=dict(binding.model_settings_json or {}),
        config=dict(binding.config_json or {}),
    )


def _dedupe_ordered(values: list[str]) -> list[str]:
    result: list[str] = []
    for value in values:
        if value not in result:
            result.append(value)
    return result


def _normalize_optional_text(value: Any) -> str | None:
    normalized = "" if value is None else str(value).strip()
    return normalized or None
