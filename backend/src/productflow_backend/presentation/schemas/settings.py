from __future__ import annotations

from datetime import datetime
from typing import Any, Literal

from pydantic import BaseModel, Field

ConfigSource = Literal["database", "env_default"]
ConfigInputType = Literal["text", "password", "number", "boolean", "select", "multi_select", "textarea"]


class ConfigOptionResponse(BaseModel):
    value: str
    label: str


class ConfigItemResponse(BaseModel):
    key: str
    label: str
    category: str
    input_type: ConfigInputType
    description: str = ""
    value: str | int | bool | list[str] | None
    source: ConfigSource
    secret: bool = False
    has_value: bool = False
    options: list[ConfigOptionResponse] = Field(default_factory=list)
    minimum: int | None = None
    maximum: int | None = None
    updated_at: str | None = None


class ConfigResponse(BaseModel):
    items: list[ConfigItemResponse]


class RuntimeConfigResponse(BaseModel):
    image_generation_max_dimension: int
    image_tool_allowed_fields: list[str]
    admin_access_required: bool
    deletion_enabled: bool


class ConfigUpdateRequest(BaseModel):
    values: dict[str, Any] = Field(default_factory=dict)
    reset_keys: list[str] = Field(default_factory=list)


class SettingsLockStateResponse(BaseModel):
    unlocked: bool
    configured: bool


class SettingsUnlockRequest(BaseModel):
    token: str = Field(min_length=1)


class ProviderProfileResponse(BaseModel):
    id: str
    name: str
    provider_type: str
    base_url: str | None = None
    capabilities: list[str]
    default_models: dict[str, Any] = Field(default_factory=dict)
    config: dict[str, Any] = Field(default_factory=dict)
    enabled: bool
    archived_at: str | None = None
    has_api_key: bool
    created_at: str
    updated_at: str


class ProviderBindingResponse(BaseModel):
    id: str
    purpose: str
    provider_kind: str
    provider_profile_id: str | None = None
    model_settings: dict[str, Any] = Field(default_factory=dict)
    config: dict[str, Any] = Field(default_factory=dict)
    created_at: str
    updated_at: str


class ProviderConfigResponse(BaseModel):
    profiles: list[ProviderProfileResponse]
    bindings: list[ProviderBindingResponse]


class AgentProviderRuntimeConfigResponse(BaseModel):
    schema_version: Literal[1] = 1
    provider_kind: Literal["openai"]
    api_key: str = Field(min_length=1)
    base_url: str | None = None
    model: str = Field(min_length=1)
    reasoning_effort: str | None = None
    reasoning_summary: str | None = None
    text_verbosity: str | None = None
    service_tier: str | None = None


class ProviderProfileCreateRequest(BaseModel):
    name: str = Field(min_length=1)
    provider_type: str = "openai_compatible"
    base_url: str | None = None
    api_key: str | None = None
    capabilities: list[str] = Field(min_length=1)
    default_models: dict[str, Any] = Field(default_factory=dict)
    config: dict[str, Any] = Field(default_factory=dict)
    enabled: bool = True


class ProviderProfileUpdateRequest(BaseModel):
    name: str | None = None
    provider_type: str | None = None
    base_url: str | None = None
    api_key: str | None = None
    capabilities: list[str] | None = None
    default_models: dict[str, Any] | None = None
    config: dict[str, Any] | None = None
    enabled: bool | None = None


class ProviderBindingUpdateRequest(BaseModel):
    provider_kind: str
    provider_profile_id: str | None = None
    model_settings: dict[str, Any] = Field(default_factory=dict)
    config: dict[str, Any] = Field(default_factory=dict)


class SettingsExportMetadataResponse(BaseModel):
    schema_version: int
    exported_at: datetime
    app: str
    app_version: str
    compatibility: str


class SettingsProviderProfileExport(BaseModel):
    id: str = Field(min_length=1, max_length=36)
    name: str = Field(min_length=1, max_length=120)
    provider_type: str = Field(min_length=1, max_length=40)
    base_url: str | None = None
    api_key: str | None = None
    capabilities: list[str] = Field(default_factory=list)
    default_models: dict[str, Any] = Field(default_factory=dict)
    config: dict[str, Any] = Field(default_factory=dict)
    enabled: bool = True


class SettingsProviderBindingExport(BaseModel):
    purpose: str = Field(min_length=1, max_length=40)
    provider_kind: str = Field(min_length=1, max_length=40)
    provider_profile_id: str | None = Field(default=None, max_length=36)
    model_settings: dict[str, Any] = Field(default_factory=dict)
    config: dict[str, Any] = Field(default_factory=dict)


class SettingsExportDocument(BaseModel):
    metadata: SettingsExportMetadataResponse
    runtime_config: dict[str, Any]
    provider_profiles: list[SettingsProviderProfileExport] = Field(default_factory=list)
    provider_bindings: list[SettingsProviderBindingExport] = Field(default_factory=list)


class SettingsImportPreviewResponse(BaseModel):
    schema_version: int
    runtime_config_count: int
    provider_profile_count: int
    provider_binding_count: int
    provider_profile_names: list[str]
    provider_binding_purposes: list[str]
    includes_api_keys: bool
    provider_profiles_with_api_key_count: int


class SettingsImportCommitResponse(BaseModel):
    preview: SettingsImportPreviewResponse
    config: ConfigResponse
    provider_config: ProviderConfigResponse
