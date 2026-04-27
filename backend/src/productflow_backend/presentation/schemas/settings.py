from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, Field

ConfigSource = Literal["database", "env_default"]
ConfigInputType = Literal["text", "password", "number", "boolean", "select", "textarea"]


class ConfigOptionResponse(BaseModel):
    value: str
    label: str


class ConfigItemResponse(BaseModel):
    key: str
    label: str
    category: str
    input_type: ConfigInputType
    description: str = ""
    value: str | int | bool | None
    source: ConfigSource
    secret: bool = False
    has_value: bool = False
    options: list[ConfigOptionResponse] = Field(default_factory=list)
    minimum: int | None = None
    maximum: int | None = None


class ConfigResponse(BaseModel):
    items: list[ConfigItemResponse]


class RuntimeConfigResponse(BaseModel):
    image_generation_max_dimension: int


class ConfigUpdateRequest(BaseModel):
    values: dict[str, Any] = Field(default_factory=dict)
    reset_keys: list[str] = Field(default_factory=list)


class SettingsLockStateResponse(BaseModel):
    unlocked: bool
    configured: bool


class SettingsUnlockRequest(BaseModel):
    token: str = Field(min_length=1)
