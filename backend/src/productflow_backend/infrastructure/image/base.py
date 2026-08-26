"""Shared image-provider contracts and helpers.

OpenAIResponsesImageClient, OpenAIImagesClient, and GoogleGeminiImageClient are
internal HTTP lower-seam clients shared by ImageProvider and ImageChatProvider.
"""

from __future__ import annotations

from abc import ABC, abstractmethod
from base64 import b64decode
from collections.abc import Callable
from io import BytesIO
from typing import Any, Literal

from PIL import Image, UnidentifiedImageError
from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator

from productflow_backend.domain.image_specs import GenerationSpec
from productflow_backend.domain.local_image_edits import LocalImageEditOperation
from productflow_backend.infrastructure.image.chat_types import GeneratedChatImage, ImageChatTurn
from productflow_backend.infrastructure.provider_effects import ProviderEffectQueryResult


class WorkflowImageReference(BaseModel):
    asset_id: str
    role: str
    label: str
    filename: str
    mime_type: str
    bytes_data: bytes


class WorkflowImageRequest(BaseModel):
    compiled_prompt: str
    generation_spec: GenerationSpec
    references: tuple[WorkflowImageReference, ...] = ()
    image_type_key: str | None = None


LOCAL_EDIT_MODE = "masked_edit"
MAX_LOCAL_EDIT_REFERENCE_IMAGES = 6


class LocalEditImage(BaseModel):
    """Image bytes crossing the provider boundary; no product or persistence identity."""

    model_config = ConfigDict(extra="forbid", frozen=True)

    bytes_data: bytes = Field(min_length=1)
    mime_type: str = Field(min_length=1)
    filename: str = Field(default="image.png", min_length=1)


class LocalEditMask(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    bytes_data: bytes = Field(min_length=1)
    mime_type: Literal["image/png"]


class LocalEditRequest(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    source_image: LocalEditImage
    instruction: str = Field(min_length=1)
    operation: LocalImageEditOperation
    mask: LocalEditMask
    reference_images: tuple[LocalEditImage, ...] = Field(
        default_factory=tuple,
        max_length=MAX_LOCAL_EDIT_REFERENCE_IMAGES,
    )
    size: str = Field(min_length=1)

    @field_validator("instruction", "size")
    @classmethod
    def normalize_non_empty_text(cls, value: str) -> str:
        normalized = value.strip()
        if not normalized:
            raise ValueError("局部编辑 instruction 和 size 不能为空")
        return normalized


class WorkflowGeneratedImage(BaseModel):
    bytes_data: bytes
    mime_type: str


class WorkflowImageResult(BaseModel):
    images: tuple[WorkflowGeneratedImage, ...]
    model: str
    provider_response_id: str | None = None
    provider_status: str
    effective_parameters: dict[str, Any]
    provider_request_json: dict[str, Any] | None = None
    provider_output_json: dict[str, Any] | None = None


class LocalEditCapability(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    provider_name: str
    supported: bool
    mode: Literal["masked_edit"] | None = None
    operations: tuple[LocalImageEditOperation, ...] = ()
    requires_mask: bool = True
    max_reference_images: int = Field(default=MAX_LOCAL_EDIT_REFERENCE_IMAGES, ge=0)
    reason: str | None = None

    @model_validator(mode="after")
    def validate_support_contract(self) -> LocalEditCapability:
        if self.supported:
            if self.mode != LOCAL_EDIT_MODE or not self.operations:
                raise ValueError("支持局部编辑时必须声明 masked_edit mode 和至少一个 operation")
            return self
        if self.mode is not None or self.operations or self.max_reference_images != 0 or not self.reason:
            raise ValueError("不支持局部编辑时不得声明 mode、operation 或 reference capacity，且必须说明原因")
        return self

    @classmethod
    def unsupported(cls, provider_name: str, *, reason: str) -> LocalEditCapability:
        return cls(
            provider_name=provider_name,
            supported=False,
            operations=(),
            max_reference_images=0,
            reason=reason,
        )


class LocalEditResult(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    images: tuple[WorkflowGeneratedImage, ...]
    model: str
    provider_response_id: str | None = None
    provider_status: str
    effective_mode: Literal["masked_edit"]
    effective_parameters: dict[str, Any]
    provider_request_json: dict[str, Any] | None = None
    provider_output_json: dict[str, Any] | None = None


class UnsupportedLocalEditError(RuntimeError):
    """Raised when a provider does not explicitly advertise masked local edit."""


class ImageProvider(ABC):
    """Schema-v3 shared image generation adapter with explicit local-edit capability."""

    provider_name: str
    prompt_version: str = "v1"

    @abstractmethod
    def generate_workflow_image(self, request: WorkflowImageRequest) -> WorkflowImageResult:
        raise NotImplementedError

    @property
    def local_edit_capability(self) -> LocalEditCapability:
        return LocalEditCapability.unsupported(
            self.provider_name,
            reason="图片 provider 未显式声明 masked local edit 能力",
        )

    def edit_local(self, request: LocalEditRequest) -> LocalEditResult:
        del request
        raise UnsupportedLocalEditError(
            f"图片 provider {self.provider_name} 不支持 masked local edit",
        )

    def reconcile_workflow_image_effect(
        self,
        *,
        operation_key: str,
        request_hash: str,
        provider_response_id: str | None,
    ) -> ProviderEffectQueryResult:
        """Query provider state without submitting another image generation request."""

        return ProviderEffectQueryResult.unsupported(
            f"图片 provider {self.provider_name} 没有提供可查询的生成记录接口"
        )


class ImageChatProvider(ABC):
    """Continuous image-session generation adapter with a provider-neutral contract."""

    provider_kind: str

    @abstractmethod
    def generate(
        self,
        prompt: str,
        size: str,
        history: list[ImageChatTurn],
        manual_reference_images: list[str],
        previous_response_id: str | None = None,
        tool_options: dict | None = None,
        progress_callback: Callable[[dict[str, Any]], None] | None = None,
    ) -> GeneratedChatImage:
        raise NotImplementedError

    @abstractmethod
    def generate_many(
        self,
        prompt: str,
        size: str,
        history: list[ImageChatTurn],
        manual_reference_images: list[str],
        *,
        candidate_count: int,
        tool_options: dict | None = None,
    ) -> list[GeneratedChatImage]:
        raise NotImplementedError

    @abstractmethod
    def reconcile_generation_effect(
        self,
        *,
        operation_key: str,
        request_hash: str,
        provider_response_id: str | None,
    ) -> ProviderEffectQueryResult:
        raise NotImplementedError


def parse_size(size: str) -> tuple[int, int]:
    width_str, height_str = size.lower().split("x", maxsplit=1)
    return int(width_str), int(height_str)


def decode_b64_image(data: str) -> bytes:
    return b64decode(data)


def infer_extension(mime_type: str) -> str:
    return {
        "image/png": ".png",
        "image/jpeg": ".jpg",
        "image/webp": ".webp",
    }.get(mime_type, ".bin")


def image_dimensions_from_bytes(bytes_data: bytes) -> tuple[int, int] | None:
    try:
        with Image.open(BytesIO(bytes_data)) as image:
            return image.width, image.height
    except (OSError, UnidentifiedImageError):
        return None


def aspect_ratio_value(aspect_ratio: str) -> float:
    width, height = (int(value) for value in aspect_ratio.split(":", maxsplit=1))
    return width / height


ASPECT_RATIO_MISMATCH_TOLERANCE = 0.08


def measured_aspect_matches_spec(
    spec: GenerationSpec,
    width: int,
    height: int,
    *,
    tolerance: float = ASPECT_RATIO_MISMATCH_TOLERANCE,
) -> bool:
    if width <= 0 or height <= 0:
        return False
    requested = aspect_ratio_value(spec.aspect_ratio)
    actual = width / height
    return abs(actual - requested) <= requested * tolerance


def aspect_mismatch_message(spec: GenerationSpec, width: int, height: int) -> str:
    return f"供应商没有按 {spec.aspect_ratio} 出图（实际 {width}×{height}）"


def map_generation_spec_to_openai_size(spec: GenerationSpec) -> str:
    ratio = aspect_ratio_value(spec.aspect_ratio)
    return map_aspect_ratio_to_openai_size(ratio)


def map_pixel_size_to_openai_size(size: str) -> str:
    width, height = parse_size(size)
    if width <= 0 or height <= 0:
        raise ValueError("图片尺寸必须大于零")
    return map_aspect_ratio_to_openai_size(width / height)


def map_aspect_ratio_to_openai_size(ratio: float) -> str:
    if ratio > 1.25:
        return "1536x1024"
    if ratio < 0.8:
        return "1024x1536"
    return "1024x1024"


def map_generation_spec_to_pixel_size(spec: GenerationSpec) -> str:
    longest_edge = {
        "standard": 1024,
        "high": 2048,
        "ultra": 4096,
    }[spec.resolution_tier]
    ratio = aspect_ratio_value(spec.aspect_ratio)
    if ratio >= 1:
        width = longest_edge
        height = max(1, round(longest_edge / ratio))
    else:
        width = max(1, round(longest_edge * ratio))
        height = longest_edge
    return f"{width}x{height}"
