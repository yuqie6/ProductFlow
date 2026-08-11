from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from typing import Any

from productflow_backend.application.workflow_drafts.contracts import (
    ImagePromptPayloadV1,
    VisualSystemDraftPayload,
)


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
    visual_system: VisualSystemDraftPayload
    visual_exceptions: tuple[dict[str, Any], ...]
    current_prompt: ImagePromptPayloadV1
    text_languages: tuple[str, ...]
    reference_images: tuple[PromptReferenceImage, ...]


@dataclass(frozen=True, slots=True)
class PromptGenerationResult:
    payload: ImagePromptPayloadV1
    model: str
    response_id: str | None = None


class PromptGenerationProvider(ABC):
    provider_name: str

    @abstractmethod
    def generate_prompt(self, request: PromptGenerationRequest) -> PromptGenerationResult:
        raise NotImplementedError


__all__ = [
    "PromptGenerationProvider",
    "PromptGenerationRequest",
    "PromptGenerationResult",
    "PromptReferenceImage",
]
