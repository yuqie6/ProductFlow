from __future__ import annotations

from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, Literal

from sqlalchemy import select
from sqlalchemy.exc import SQLAlchemyError
from sqlalchemy.orm import Session

from productflow_backend.config import Settings, build_settings_with_overrides
from productflow_backend.infrastructure.db.models import AppSetting, ProviderBinding, ProviderProfile
from productflow_backend.infrastructure.db.session import get_session_factory

TEXT_PURPOSE = "text"
IMAGE_PURPOSE = "image"
PROVIDER_TYPE_OPENAI_COMPATIBLE = "openai_compatible"
PROVIDER_TYPE_GOOGLE_GEMINI = "google_gemini"
PROVIDER_TYPES = {PROVIDER_TYPE_OPENAI_COMPATIBLE, PROVIDER_TYPE_GOOGLE_GEMINI}

TEXT_PROVIDER_KINDS = {"mock", "openai"}
IMAGE_PROVIDER_KINDS = {"mock", "openai_responses", "openai_images", "google_gemini_image"}
REAL_IMAGE_PROVIDER_KINDS = IMAGE_PROVIDER_KINDS - {"mock"}
PROVIDER_PURPOSES = {TEXT_PURPOSE, IMAGE_PURPOSE}
CAPABILITY_TEXT_RESPONSES = "text_responses"
CAPABILITY_IMAGE_RESPONSES = "image_responses"
CAPABILITY_IMAGE_IMAGES = "image_images"
CAPABILITY_IMAGE_GOOGLE_GEMINI = "image_google_gemini"
PROVIDER_CAPABILITIES = {
    CAPABILITY_TEXT_RESPONSES,
    CAPABILITY_IMAGE_RESPONSES,
    CAPABILITY_IMAGE_IMAGES,
    CAPABILITY_IMAGE_GOOGLE_GEMINI,
}
UNSET_PROVIDER_FIELD = object()

LEGACY_PROVIDER_CONFIG_KEYS = {
    "text_provider_kind",
    "text_api_key",
    "text_base_url",
    "text_brief_model",
    "text_copy_model",
    "image_provider_kind",
    "image_api_key",
    "image_base_url",
    "image_generate_model",
    "image_images_quality",
    "image_images_style",
    "image_responses_background_enabled",
}


@dataclass(frozen=True, slots=True)
class ResolvedTextProviderConfig:
    provider_kind: Literal["mock", "openai"]
    brief_model: str
    copy_model: str
    provider_profile_id: str | None = None
    api_key: str | None = None
    base_url: str | None = None


@dataclass(frozen=True, slots=True)
class ResolvedImageProviderConfig:
    provider_kind: Literal["mock", "openai_responses", "openai_images", "google_gemini_image"]
    model: str
    provider_profile_id: str | None = None
    api_key: str | None = None
    base_url: str | None = None
    images_quality: str | None = None
    images_style: str | None = None
    responses_background_enabled: bool = False
    gemini_api_version: str = "v1beta"
    gemini_output_mime_type: str | None = None


def ensure_provider_config_bootstrapped(session: Session | None = None) -> None:
    """Create provider profiles and bindings from legacy effective settings once."""

    if session is None:
        owned_session = get_session_factory()()
        try:
            ensure_provider_config_bootstrapped(owned_session)
        finally:
            owned_session.close()
        return

    if _provider_config_exists(session):
        return

    settings = _load_effective_legacy_settings(session)
    profiles_by_connection: dict[tuple[str, str], ProviderProfile] = {}

    text_kind = _normalize_provider_kind(settings.text_provider_kind, allowed=TEXT_PROVIDER_KINDS, default="mock")
    image_kind = _normalize_provider_kind(settings.image_provider_kind, allowed=IMAGE_PROVIDER_KINDS, default="mock")

    if text_kind == "openai":
        profile = _profile_for_legacy_connection(
            session,
            profiles_by_connection,
            base_url=settings.text_base_url,
            api_key=settings.text_api_key,
            capability=CAPABILITY_TEXT_RESPONSES,
        )
        _add_binding(
            session,
            purpose=TEXT_PURPOSE,
            provider_kind="openai",
            provider_profile=profile,
            model_settings={
                "brief_model": settings.text_brief_model,
                "copy_model": settings.text_copy_model,
            },
        )
    else:
        _add_binding(
            session,
            purpose=TEXT_PURPOSE,
            provider_kind="mock",
            provider_profile=None,
            model_settings={
                "brief_model": settings.text_brief_model,
                "copy_model": settings.text_copy_model,
            },
        )

    if image_kind in {"openai_responses", "openai_images"}:
        capability = CAPABILITY_IMAGE_RESPONSES if image_kind == "openai_responses" else CAPABILITY_IMAGE_IMAGES
        profile = _profile_for_legacy_connection(
            session,
            profiles_by_connection,
            base_url=settings.image_base_url,
            api_key=settings.image_api_key,
            capability=capability,
        )
        _add_binding(
            session,
            purpose=IMAGE_PURPOSE,
            provider_kind=image_kind,
            provider_profile=profile,
            model_settings={"model": settings.image_generate_model},
            config={
                "images_quality": settings.image_images_quality,
                "images_style": settings.image_images_style,
                "responses_background_enabled": settings.image_responses_background_enabled,
            },
        )
    else:
        _add_binding(
            session,
            purpose=IMAGE_PURPOSE,
            provider_kind="mock",
            provider_profile=None,
            model_settings={"model": settings.image_generate_model},
            config={},
        )

    session.commit()


def list_provider_profiles(session: Session) -> list[ProviderProfile]:
    query = select(ProviderProfile).order_by(ProviderProfile.created_at, ProviderProfile.name)
    return list(session.scalars(query).all())


def list_provider_bindings(session: Session) -> list[ProviderBinding]:
    return list(session.scalars(select(ProviderBinding).order_by(ProviderBinding.purpose)).all())


def create_provider_profile(
    session: Session,
    *,
    name: str,
    base_url: str | None,
    api_key: str | None,
    capabilities: list[str],
    provider_type: str = PROVIDER_TYPE_OPENAI_COMPATIBLE,
    default_models: dict[str, Any] | None = None,
    config: dict[str, Any] | None = None,
    enabled: bool = True,
) -> ProviderProfile:
    provider_type = _normalize_provider_type(provider_type)
    normalized_capabilities = _dedupe_ordered(capabilities)
    _validate_capabilities_for_provider_type(normalized_capabilities, provider_type=provider_type)
    normalized_base_url = _normalize_optional_text(base_url)
    _validate_provider_profile_connection(provider_type=provider_type, base_url=normalized_base_url)
    normalized_name = _normalize_required_text(name, "供应商名称")
    profile = ProviderProfile(
        name=normalized_name,
        provider_type=provider_type,
        base_url=normalized_base_url,
        api_key=_normalize_optional_text(api_key),
        capabilities_json=normalized_capabilities,
        default_models_json=default_models or {},
        config_json=config or {},
        enabled=enabled,
    )
    session.add(profile)
    session.commit()
    session.refresh(profile)
    return profile


def update_provider_profile(
    session: Session,
    profile_id: str,
    *,
    name: str | None = None,
    provider_type: str | None = None,
    base_url: str | None | object = UNSET_PROVIDER_FIELD,
    api_key: str | None | object = UNSET_PROVIDER_FIELD,
    capabilities: list[str] | None = None,
    default_models: dict[str, Any] | None = None,
    config: dict[str, Any] | None = None,
    enabled: bool | None = None,
) -> ProviderProfile:
    profile = session.get(ProviderProfile, profile_id)
    if profile is None or profile.archived_at is not None:
        raise ValueError("供应商不存在")
    next_provider_type = _normalize_provider_type(provider_type) if provider_type is not None else profile.provider_type
    next_base_url = profile.base_url
    next_capabilities = list(profile.capabilities_json or [])
    if name is not None:
        profile.name = _normalize_required_text(name, "供应商名称")
    if base_url is not UNSET_PROVIDER_FIELD:
        next_base_url = _normalize_optional_text(base_url if isinstance(base_url, str) else None)
    if api_key is not UNSET_PROVIDER_FIELD:
        normalized_api_key = _normalize_optional_text(api_key if isinstance(api_key, str) else None)
        if normalized_api_key is not None:
            profile.api_key = normalized_api_key
    if capabilities is not None:
        next_capabilities = _dedupe_ordered(capabilities)
    _validate_capabilities_for_provider_type(next_capabilities, provider_type=next_provider_type)
    _validate_provider_profile_connection(provider_type=next_provider_type, base_url=next_base_url)
    if provider_type is not None or capabilities is not None:
        _validate_profile_update_keeps_active_bindings(
            session,
            profile,
            capabilities=next_capabilities,
            enabled=enabled,
        )
    elif enabled is not None:
        _validate_profile_update_keeps_active_bindings(session, profile, capabilities=None, enabled=enabled)
    profile.provider_type = next_provider_type
    profile.base_url = next_base_url
    profile.capabilities_json = next_capabilities
    if default_models is not None:
        profile.default_models_json = default_models
    if config is not None:
        profile.config_json = config
    if enabled is not None:
        profile.enabled = enabled
    session.commit()
    session.refresh(profile)
    return profile


def archive_provider_profile(session: Session, profile_id: str) -> ProviderProfile:
    profile = session.get(ProviderProfile, profile_id)
    if profile is None or profile.archived_at is not None:
        raise ValueError("供应商不存在")
    active_bindings = session.scalars(
        select(ProviderBinding).where(ProviderBinding.provider_profile_id == profile_id)
    ).all()
    if active_bindings:
        raise ValueError("供应商仍被文案或图片配置使用，不能归档")
    profile.archived_at = datetime.now(UTC)
    profile.enabled = False
    session.commit()
    session.refresh(profile)
    return profile


def update_provider_binding(
    session: Session,
    *,
    purpose: str,
    provider_kind: str,
    provider_profile_id: str | None,
    model_settings: dict[str, Any],
    config: dict[str, Any],
    commit: bool = True,
) -> ProviderBinding:
    _validate_binding_payload(
        session,
        purpose=purpose,
        provider_kind=provider_kind,
        provider_profile_id=provider_profile_id,
        model_settings=model_settings,
        config=config,
    )
    if provider_kind == "mock":
        provider_profile_id = None
    normalized_model_settings = _normalize_binding_model_settings(
        purpose=purpose,
        model_settings=model_settings,
    )
    normalized_config = _normalize_binding_config(
        purpose=purpose,
        provider_kind=provider_kind,
        config=config,
    )
    binding = _get_binding(session, purpose)
    if binding is None:
        binding = ProviderBinding(
            purpose=purpose,
            provider_kind=provider_kind,
            provider_profile_id=provider_profile_id,
            model_settings_json=normalized_model_settings,
            config_json=normalized_config,
        )
        session.add(binding)
    else:
        binding.provider_kind = provider_kind
        binding.provider_profile_id = provider_profile_id
        binding.model_settings_json = normalized_model_settings
        binding.config_json = normalized_config
    if commit:
        session.commit()
        session.refresh(binding)
    else:
        session.flush()
    return binding


def capability_for_provider_kind(provider_kind: str) -> str:
    return _capability_for_kind(provider_kind)


def is_real_image_provider_kind(provider_kind: str | None) -> bool:
    return provider_kind in REAL_IMAGE_PROVIDER_KINDS


def validate_provider_capabilities(capabilities: list[str]) -> None:
    _validate_capabilities(capabilities)


def validate_provider_profile_contract(
    *,
    provider_type: str,
    capabilities: list[str],
    base_url: str | None,
) -> None:
    normalized_provider_type = _normalize_provider_type(provider_type)
    _validate_capabilities_for_provider_type(capabilities, provider_type=normalized_provider_type)
    _validate_provider_profile_connection(provider_type=normalized_provider_type, base_url=base_url)


def normalize_provider_binding_runtime_config(
    *,
    purpose: str,
    provider_kind: str,
    model_settings: dict[str, Any],
    config: dict[str, Any],
) -> dict[str, Any]:
    _validate_binding_runtime_config(
        purpose=purpose,
        provider_kind=provider_kind,
        model_settings=model_settings,
        config=config,
    )
    return _normalize_binding_config(purpose=purpose, provider_kind=provider_kind, config=config)


def normalize_provider_binding_model_settings(*, purpose: str, model_settings: dict[str, Any]) -> dict[str, Any]:
    return _normalize_binding_model_settings(purpose=purpose, model_settings=model_settings)


def resolve_text_provider_config(session: Session | None = None) -> ResolvedTextProviderConfig:
    owns_session = session is None
    session = session or get_session_factory()()
    try:
        ensure_provider_config_bootstrapped(session)
        binding = _require_binding(session, TEXT_PURPOSE)
        kind = binding.provider_kind
        if kind == "mock":
            return ResolvedTextProviderConfig(
                provider_kind="mock",
                brief_model=_require_text_value(binding.model_settings_json, "brief_model", "文案商品理解模型未配置"),
                copy_model=_require_text_value(binding.model_settings_json, "copy_model", "文案生成模型未配置"),
            )
        if kind != "openai":
            raise RuntimeError(f"暂不支持的文案 provider: {kind}")
        profile = _require_active_profile(binding)
        _require_capability(profile, CAPABILITY_TEXT_RESPONSES)
        brief_model = _require_text_value(
            binding.model_settings_json,
            "brief_model",
            "文案商品理解模型未配置",
            fallback_values=profile.default_models_json,
        )
        copy_model = _require_text_value(
            binding.model_settings_json,
            "copy_model",
            "文案生成模型未配置",
            fallback_values=profile.default_models_json,
        )
        return ResolvedTextProviderConfig(
            provider_kind="openai",
            brief_model=brief_model,
            copy_model=copy_model,
            provider_profile_id=profile.id,
            api_key=profile.api_key,
            base_url=profile.base_url,
        )
    finally:
        if owns_session:
            session.close()


def resolve_image_provider_config(session: Session | None = None) -> ResolvedImageProviderConfig:
    owns_session = session is None
    session = session or get_session_factory()()
    try:
        ensure_provider_config_bootstrapped(session)
        binding = _require_binding(session, IMAGE_PURPOSE)
        kind = binding.provider_kind
        if kind == "mock":
            return ResolvedImageProviderConfig(
                provider_kind="mock",
                model=_require_text_value(binding.model_settings_json, "model", "图片模型未配置"),
            )
        if kind not in {"openai_responses", "openai_images", "google_gemini_image"}:
            raise RuntimeError(f"暂不支持的图片 provider: {kind}")
        profile = _require_active_profile(binding)
        capability = _capability_for_kind(kind)
        _require_capability(profile, capability)
        return ResolvedImageProviderConfig(
            provider_kind=kind,  # type: ignore[arg-type]
            model=_require_text_value(
                binding.model_settings_json,
                "model",
                "图片模型未配置",
                fallback_values=profile.default_models_json,
                fallback_key="image_model",
            ),
            provider_profile_id=profile.id,
            api_key=profile.api_key,
            base_url=profile.base_url,
            images_quality=(
                _optional_str(binding.config_json.get("images_quality")) if kind == "openai_images" else None
            ),
            images_style=_optional_str(binding.config_json.get("images_style")) if kind == "openai_images" else None,
            responses_background_enabled=(
                _require_bool_value(
                    binding.config_json,
                    "responses_background_enabled",
                    "图片 Responses 后台响应模式未配置",
                )
                if kind == "openai_responses"
                else False
            ),
            gemini_api_version=(
                (_optional_str(binding.config_json.get("gemini_api_version")) or "v1beta")
                if kind == "google_gemini_image"
                else "v1beta"
            ),
            gemini_output_mime_type=(
                _optional_str(binding.config_json.get("gemini_output_mime_type"))
                if kind == "google_gemini_image"
                else None
            ),
        )
    finally:
        if owns_session:
            session.close()


def _provider_config_exists(session: Session) -> bool:
    return bool(
        session.scalar(select(ProviderProfile.id).limit(1))
        or session.scalar(select(ProviderBinding.id).limit(1))
    )


def _load_effective_legacy_settings(session: Session) -> Settings:
    rows = session.scalars(select(AppSetting).where(AppSetting.key.in_(LEGACY_PROVIDER_CONFIG_KEYS))).all()
    overrides = {row.key: row.value for row in rows}
    return build_settings_with_overrides(overrides)


def _profile_for_legacy_connection(
    session: Session,
    profiles_by_connection: dict[tuple[str, str], ProviderProfile],
    *,
    base_url: str | None,
    api_key: str | None,
    capability: str,
) -> ProviderProfile:
    key = (_normalize_optional_text(base_url) or "", _normalize_optional_text(api_key) or "")
    profile = profiles_by_connection.get(key)
    if profile is None:
        profile = ProviderProfile(
            name=f"OpenAI 兼容供应商 {len(profiles_by_connection) + 1}",
            provider_type=PROVIDER_TYPE_OPENAI_COMPATIBLE,
            base_url=key[0] or None,
            api_key=key[1] or None,
            capabilities_json=[capability],
            default_models_json={},
            config_json={},
            enabled=True,
        )
        session.add(profile)
        profiles_by_connection[key] = profile
    else:
        capabilities = _dedupe_ordered([*profile.capabilities_json, capability])
        profile.capabilities_json = capabilities
    return profile


def _add_binding(
    session: Session,
    *,
    purpose: str,
    provider_kind: str,
    provider_profile: ProviderProfile | None,
    model_settings: dict[str, Any],
    config: dict[str, Any] | None = None,
) -> None:
    session.add(
        ProviderBinding(
            purpose=purpose,
            provider_kind=provider_kind,
            provider_profile=provider_profile,
            model_settings_json=model_settings,
            config_json=_normalize_binding_config(
                purpose=purpose,
                provider_kind=provider_kind,
                config=config or {},
            ),
        )
    )


def _get_binding(session: Session, purpose: str) -> ProviderBinding | None:
    return session.scalar(select(ProviderBinding).where(ProviderBinding.purpose == purpose))


def _require_binding(session: Session, purpose: str) -> ProviderBinding:
    binding = _get_binding(session, purpose)
    if binding is None:
        raise RuntimeError("供应商用途绑定未初始化")
    return binding


def _require_active_profile(binding: ProviderBinding) -> ProviderProfile:
    profile = binding.provider_profile
    if profile is None:
        raise RuntimeError("真实供应商绑定缺少供应商档案")
    if not profile.enabled or profile.archived_at is not None:
        raise RuntimeError("供应商已停用或已归档")
    return profile


def _validate_binding_payload(
    session: Session,
    *,
    purpose: str,
    provider_kind: str,
    provider_profile_id: str | None,
    model_settings: dict[str, Any],
    config: dict[str, Any],
) -> None:
    if purpose not in PROVIDER_PURPOSES:
        raise ValueError("用途必须是 text 或 image")
    allowed_kinds = TEXT_PROVIDER_KINDS if purpose == TEXT_PURPOSE else IMAGE_PROVIDER_KINDS
    if provider_kind not in allowed_kinds:
        raise ValueError("供应商接口类型不支持当前用途")
    _validate_binding_runtime_config(
        purpose=purpose,
        provider_kind=provider_kind,
        model_settings=model_settings,
        config=config,
    )
    if provider_kind == "mock":
        return
    if not provider_profile_id:
        raise ValueError("真实供应商必须选择供应商档案")
    profile = session.get(ProviderProfile, provider_profile_id)
    if profile is None or profile.archived_at is not None:
        raise ValueError("供应商不存在")
    if not profile.enabled:
        raise ValueError("供应商已停用")
    capability = _capability_for_kind(provider_kind)
    _require_capability(profile, capability)
    _validate_profile_type_supports_capability(profile.provider_type, capability)


def _validate_profile_update_keeps_active_bindings(
    session: Session,
    profile: ProviderProfile,
    *,
    capabilities: list[str] | None,
    enabled: bool | None,
) -> None:
    active_bindings = list(
        session.scalars(select(ProviderBinding).where(ProviderBinding.provider_profile_id == profile.id)).all()
    )
    if not active_bindings:
        return
    if enabled is False:
        raise ValueError("供应商仍被文案或图片配置使用，不能停用")
    if capabilities is None:
        return

    capability_set = set(capabilities)
    for binding in active_bindings:
        if binding.provider_kind == "mock":
            continue
        required_capability = _capability_for_kind(binding.provider_kind)
        if required_capability not in capability_set:
            raise ValueError("供应商仍被文案或图片配置使用，不能移除当前接口能力")


def _capability_for_kind(provider_kind: str) -> str:
    if provider_kind == "openai":
        return CAPABILITY_TEXT_RESPONSES
    if provider_kind == "openai_responses":
        return CAPABILITY_IMAGE_RESPONSES
    if provider_kind == "openai_images":
        return CAPABILITY_IMAGE_IMAGES
    if provider_kind == "google_gemini_image":
        return CAPABILITY_IMAGE_GOOGLE_GEMINI
    raise ValueError("供应商接口类型不支持真实供应商档案")


def _require_capability(profile: ProviderProfile, capability: str) -> None:
    if capability not in set(profile.capabilities_json or []):
        raise RuntimeError("供应商档案不支持当前接口能力")


def _validate_binding_runtime_config(
    *,
    purpose: str,
    provider_kind: str,
    model_settings: dict[str, Any],
    config: dict[str, Any],
) -> None:
    if purpose == TEXT_PURPOSE:
        normalized_settings = _normalize_text_model_settings(model_settings)
        _require_text_value(normalized_settings, "brief_model", "文案商品理解模型未配置", exc_type=ValueError)
        _require_text_value(normalized_settings, "copy_model", "文案生成模型未配置", exc_type=ValueError)
        return
    _require_text_value(model_settings, "model", "图片模型未配置", exc_type=ValueError)
    if provider_kind == "openai_responses":
        _require_bool_value(
            config,
            "responses_background_enabled",
            "图片 Responses 后台响应模式未配置",
            exc_type=ValueError,
        )
    if provider_kind == "google_gemini_image":
        gemini_api_version = _optional_str(config.get("gemini_api_version")) or "v1beta"
        if gemini_api_version not in {"v1", "v1beta"}:
            raise ValueError("Gemini API 版本必须是 v1 或 v1beta")


def _normalize_binding_model_settings(*, purpose: str, model_settings: dict[str, Any]) -> dict[str, Any]:
    if purpose == TEXT_PURPOSE:
        return _normalize_text_model_settings(model_settings)
    return {key: value for key, value in model_settings.items() if value is not None}


def _normalize_text_model_settings(model_settings: dict[str, Any]) -> dict[str, Any]:
    normalized: dict[str, Any] = {}
    for key in ("brief_model", "copy_model"):
        value = _optional_str(model_settings.get(key))
        if value is not None:
            normalized[key] = value

    return normalized


def _normalize_binding_config(*, purpose: str, provider_kind: str, config: dict[str, Any]) -> dict[str, Any]:
    if purpose != IMAGE_PURPOSE:
        return {}
    if provider_kind == "openai_responses":
        return {
            "responses_background_enabled": _require_bool_value(
                config,
                "responses_background_enabled",
                "图片 Responses 后台响应模式未配置",
                exc_type=ValueError,
            )
        }
    if provider_kind == "openai_images":
        return {
            key: value
            for key, value in {
                "images_quality": _optional_str(config.get("images_quality")),
                "images_style": _optional_str(config.get("images_style")),
            }.items()
            if value is not None
        }
    if provider_kind == "google_gemini_image":
        gemini_api_version = _optional_str(config.get("gemini_api_version")) or "v1beta"
        if gemini_api_version not in {"v1", "v1beta"}:
            raise ValueError("Gemini API 版本必须是 v1 或 v1beta")
        return {
            key: value
            for key, value in {
                "gemini_api_version": gemini_api_version,
                "gemini_output_mime_type": _optional_str(config.get("gemini_output_mime_type")),
            }.items()
            if value is not None
        }
    return {}


def _require_text_value(
    values: dict[str, Any],
    key: str,
    message: str,
    *,
    fallback_values: dict[str, Any] | None = None,
    fallback_key: str | None = None,
    exc_type: type[Exception] = RuntimeError,
) -> str:
    value = _optional_str(values.get(key))
    if value is None and fallback_values is not None:
        value = _optional_str(fallback_values.get(fallback_key or key))
    if value is None:
        raise exc_type(message)
    return value


def _require_bool_value(
    values: dict[str, Any],
    key: str,
    message: str,
    *,
    exc_type: type[Exception] = RuntimeError,
) -> bool:
    if key not in values or values.get(key) is None:
        raise exc_type(message)
    return _optional_bool(values.get(key), default=False)


def _validate_capabilities(capabilities: list[str]) -> None:
    if not capabilities:
        raise ValueError("供应商能力不能为空")
    unknown = set(capabilities) - PROVIDER_CAPABILITIES
    if unknown:
        raise ValueError(f"供应商能力不支持: {', '.join(sorted(unknown))}")


def _normalize_provider_type(provider_type: str) -> str:
    normalized = str(provider_type or "").strip()
    if normalized not in PROVIDER_TYPES:
        raise ValueError("供应商类型不支持")
    return normalized


def _validate_profile_type_supports_capability(provider_type: str, capability: str) -> None:
    if provider_type == PROVIDER_TYPE_GOOGLE_GEMINI:
        if capability != CAPABILITY_IMAGE_GOOGLE_GEMINI:
            raise ValueError("Google Gemini 供应商档案只支持 Gemini 图片能力")
        return
    if provider_type == PROVIDER_TYPE_OPENAI_COMPATIBLE:
        if capability == CAPABILITY_IMAGE_GOOGLE_GEMINI:
            raise ValueError("OpenAI 兼容供应商档案不支持 Gemini 图片能力")
        return
    raise ValueError("供应商类型不支持")


def _validate_capabilities_for_provider_type(capabilities: list[str], *, provider_type: str) -> None:
    _validate_capabilities(capabilities)
    for capability in capabilities:
        _validate_profile_type_supports_capability(provider_type, capability)


def _validate_provider_profile_connection(*, provider_type: str, base_url: str | None) -> None:
    if provider_type == PROVIDER_TYPE_GOOGLE_GEMINI and base_url:
        raise ValueError("Google Gemini 供应商暂不支持自定义 Base URL")


def _normalize_required_text(value: str, label: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise ValueError(f"{label}不能为空")
    return normalized


def _normalize_optional_text(value: str | None) -> str | None:
    normalized = "" if value is None else str(value).strip()
    return normalized or None


def _optional_str(value: Any) -> str | None:
    normalized = "" if value is None else str(value).strip()
    return normalized or None


def _optional_bool(value: Any, *, default: bool) -> bool:
    if value is None:
        return default
    if isinstance(value, bool):
        return value
    return str(value).strip().lower() in {"1", "true", "yes", "on"}


def _normalize_provider_kind(value: str, *, allowed: set[str], default: str) -> str:
    normalized = str(value or default).strip()
    return normalized if normalized in allowed else default


def _dedupe_ordered(values: list[str]) -> list[str]:
    result: list[str] = []
    for value in values:
        if value not in result:
            result.append(value)
    return result


def provider_config_tables_available() -> bool:
    try:
        session = get_session_factory()()
        try:
            session.scalar(select(ProviderBinding.id).limit(1))
            return True
        finally:
            session.close()
    except SQLAlchemyError:
        return False
