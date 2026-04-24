from __future__ import annotations

from abc import ABC, abstractmethod
from base64 import b64decode, b64encode

from pydantic import BaseModel

from productflow_backend.application.contracts import PosterGenerationInput, ReferenceImageInput
from productflow_backend.domain.enums import PosterKind


class GeneratedImagePayload(BaseModel):
    kind: PosterKind
    bytes_data: bytes
    mime_type: str = "image/png"
    width: int
    height: int
    variant_label: str


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
