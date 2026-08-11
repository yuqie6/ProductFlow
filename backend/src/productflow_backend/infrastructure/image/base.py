from __future__ import annotations

from abc import ABC, abstractmethod
from base64 import b64decode, b64encode
from io import BytesIO
from typing import Any

from PIL import Image, UnidentifiedImageError
from pydantic import BaseModel

from productflow_backend.application.contracts import PosterGenerationInput, ReferenceImageInput
from productflow_backend.application.workflow_drafts.contracts import GenerationSpec
from productflow_backend.domain.enums import PosterKind


class GeneratedImagePayload(BaseModel):
    kind: PosterKind
    bytes_data: bytes
    mime_type: str = "image/png"
    width: int
    height: int
    variant_label: str
    provider_response_id: str | None = None
    provider_response_status: str | None = None
    provider_output_json: dict[str, Any] | None = None


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


class ImageProvider(ABC):
    """图片生成器抽象接口：用于 AI 海报生成。"""

    provider_name: str
    prompt_version: str = "v1"

    @abstractmethod
    def generate_poster_image(
        self,
        poster: PosterGenerationInput,
        kind: PosterKind,
    ) -> tuple[GeneratedImagePayload, str]:
        raise NotImplementedError

    def generate_workflow_image(self, request: WorkflowImageRequest) -> WorkflowImageResult:
        raise NotImplementedError("当前图片 provider 尚未实现 schema-v2 单图生成")


def parse_size(size: str) -> tuple[int, int]:
    width_str, height_str = size.lower().split("x", maxsplit=1)
    return int(width_str), int(height_str)


def decode_b64_image(data: str) -> bytes:
    return b64decode(data)


def encode_reference_image(reference: ReferenceImageInput) -> str:
    raw = reference.path.read_bytes()
    encoded = b64encode(raw).decode("utf-8")
    return f"data:{reference.mime_type};base64,{encoded}"


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


def map_generation_spec_to_openai_size(spec: GenerationSpec) -> str:
    ratio = aspect_ratio_value(spec.aspect_ratio)
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
