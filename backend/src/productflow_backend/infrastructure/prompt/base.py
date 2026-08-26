from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from typing import Any

from pydantic import ValidationError

from productflow_backend.domain.artifact_contracts import (
    ImagePromptPayloadV1,
    VisualSystemDraftPayload,
)
from productflow_backend.infrastructure.prompt.context_payloads import (
    DEFAULT_CREATIVE_BRIEF,
    DEFAULT_VISUAL_OVERLAY,
    GeneratedCreativeBrief,
    GeneratedVisualOverlay,
)
from productflow_backend.infrastructure.provider_effects import ProviderEffectQueryResult


@dataclass(frozen=True, slots=True)
class PromptReferenceImage:
    asset_id: str
    role: str
    label: str
    filename: str
    mime_type: str
    image_bytes: bytes = field(repr=False)


@dataclass(frozen=True, slots=True)
class PromptGenerationRequest:
    image_type_key: str
    image_plan_keys: tuple[str, ...]
    facts: tuple[dict[str, Any], ...]
    visual_system: VisualSystemDraftPayload | None
    visual_exceptions: tuple[dict[str, Any], ...]
    current_prompt: ImagePromptPayloadV1
    text_languages: tuple[str, ...]
    reference_images: tuple[PromptReferenceImage, ...]
    generate_from_context: bool = False
    image_type_title: str | None = None
    image_type_description: str | None = None
    text_policy: str = "none"
    image_type_family: str = "photography"
    image_type_job: str | None = None


@dataclass(frozen=True, slots=True)
class PromptGenerationResult:
    payload: ImagePromptPayloadV1
    model: str
    response_id: str | None = None


@dataclass(frozen=True, slots=True)
class ContextGenerationRequest:
    facts: tuple[dict[str, Any], ...]
    reference_images: tuple[PromptReferenceImage, ...]
    current_brief: dict[str, Any] | None = None
    current_overlay: dict[str, Any] | None = None
    text_policy: str = "none"
    text_language: str | None = None
    node_title: str = ""
    image_types: tuple[dict[str, Any], ...] = ()


@dataclass(frozen=True, slots=True)
class CreativeBriefGenerationResult:
    payload: GeneratedCreativeBrief
    model: str
    response_id: str | None = None


@dataclass(frozen=True, slots=True)
class VisualOverlayGenerationResult:
    payload: GeneratedVisualOverlay
    model: str
    response_id: str | None = None


class PromptGenerationProvider(ABC):
    provider_name: str

    @abstractmethod
    def generate_prompt(self, request: PromptGenerationRequest) -> PromptGenerationResult:
        raise NotImplementedError

    def generate_creative_brief(self, request: ContextGenerationRequest) -> CreativeBriefGenerationResult:
        payload = _validated_brief(request.current_brief)
        if request.text_policy == "none":
            payload = payload.model_copy(update={"required_copy": []})
        return CreativeBriefGenerationResult(payload=payload, model=self.provider_name)

    def generate_visual_overlay(self, request: ContextGenerationRequest) -> VisualOverlayGenerationResult:
        return VisualOverlayGenerationResult(
            payload=_validated_overlay(request.current_overlay),
            model=self.provider_name,
        )

    def reconcile_prompt_effect(
        self,
        *,
        operation_key: str,
        request_hash: str,
        provider_response_id: str | None,
    ) -> ProviderEffectQueryResult:
        """查询 provider 状态，不再提交一次 prompt 生成请求。"""

        return ProviderEffectQueryResult.unsupported(
            f"提示词 provider {self.provider_name} 没有提供可查询的生成记录接口"
        )


def _validated_brief(payload: dict[str, Any] | None) -> GeneratedCreativeBrief:
    if not payload:
        return DEFAULT_CREATIVE_BRIEF
    try:
        return GeneratedCreativeBrief.model_validate(payload)
    except ValidationError:
        return DEFAULT_CREATIVE_BRIEF


def _validated_overlay(payload: dict[str, Any] | None) -> GeneratedVisualOverlay:
    if not payload:
        return DEFAULT_VISUAL_OVERLAY
    try:
        return GeneratedVisualOverlay.model_validate(payload)
    except ValidationError:
        return DEFAULT_VISUAL_OVERLAY


__all__ = [
    "ContextGenerationRequest",
    "CreativeBriefGenerationResult",
    "PromptGenerationProvider",
    "PromptGenerationRequest",
    "PromptGenerationResult",
    "PromptReferenceImage",
    "VisualOverlayGenerationResult",
]
