from __future__ import annotations

import logging
from base64 import b64decode
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any

from google import genai
from google.genai import types

from productflow_backend.application.contracts import PosterGenerationInput
from productflow_backend.application.language_policy import image_visible_text_requirements
from productflow_backend.config import get_runtime_settings
from productflow_backend.domain.enums import PosterKind
from productflow_backend.infrastructure.image.base import (
    GeneratedImagePayload,
    ImageProvider,
    image_dimensions_from_bytes,
    parse_size,
)
from productflow_backend.infrastructure.image.responses_provider import (
    build_responses_reference_images_from_poster,
    poster_has_reference_input,
)
from productflow_backend.infrastructure.prompts import render_prompt_template
from productflow_backend.infrastructure.provider_config import (
    ResolvedImageProviderConfig,
    resolve_image_provider_config,
)

logger = logging.getLogger(__name__)

PROVIDER_REQUEST_FAILURE_MESSAGE = "图片供应商请求失败，请检查供应商配置后重试"
PROVIDER_MISSING_OUTPUT_MESSAGE = "图片供应商没有返回图片结果，请稍后重试"

GEMINI_DEFAULT_IMAGE_MODEL = "gemini-2.5-flash-image"
GEMINI_3_IMAGE_MODELS = {"gemini-3.1-flash-image-preview", "gemini-3-pro-image-preview"}
SUPPORTED_ASPECT_RATIOS = {
    "1:1": 1.0,
    "2:3": 2 / 3,
    "3:2": 3 / 2,
    "3:4": 3 / 4,
    "4:3": 4 / 3,
    "4:5": 4 / 5,
    "5:4": 5 / 4,
    "9:16": 9 / 16,
    "16:9": 16 / 9,
    "21:9": 21 / 9,
}


@dataclass(frozen=True, slots=True)
class GeminiImageConfig:
    aspect_ratio: str
    image_size: str | None
    output_mime_type: str | None
    notes: list[dict[str, Any]]


@dataclass(frozen=True, slots=True)
class GoogleGeminiReferenceImage:
    bytes_data: bytes
    mime_type: str
    filename: str | None = None


@dataclass(slots=True)
class GeminiImageResult:
    bytes_data: bytes
    mime_type: str
    model_name: str
    provider_name: str
    prompt_version: str
    size: str
    generated_at: datetime
    provider_response_id: str | None
    provider_request_json: dict[str, Any]
    provider_output_json: dict[str, Any]


def _mime_type_from_image_bytes(data: bytes) -> str:
    if data.startswith(b"\x89PNG\r\n\x1a\n"):
        return "image/png"
    if data.startswith(b"\xff\xd8\xff"):
        return "image/jpeg"
    if data.startswith(b"RIFF") and data[8:12] == b"WEBP":
        return "image/webp"
    return "image/png"


def _nearest_aspect_ratio(width: int, height: int) -> str:
    requested_ratio = width / height
    return min(SUPPORTED_ASPECT_RATIOS, key=lambda ratio: abs(SUPPORTED_ASPECT_RATIOS[ratio] - requested_ratio))


def _gemini_image_size_for_model(width: int, height: int, model: str) -> str | None:
    if model not in GEMINI_3_IMAGE_MODELS:
        return None
    largest_edge = max(width, height)
    if largest_edge <= 1024:
        return "1K"
    if largest_edge <= 2048:
        return "2K"
    return "4K"


def map_productflow_size_to_gemini_image_config(
    size: str,
    model: str,
    *,
    output_mime_type: str | None = None,
) -> GeminiImageConfig:
    width, height = parse_size(size)
    aspect_ratio = _nearest_aspect_ratio(width, height)
    notes: list[dict[str, Any]] = []
    requested_ratio = width / height
    if abs(SUPPORTED_ASPECT_RATIOS[aspect_ratio] - requested_ratio) > 0.01:
        notes.append(
            {
                "kind": "aspect_ratio_mapped",
                "message": f"Gemini 使用最接近的画幅比例 {aspect_ratio}。",
                "requested_size": size,
                "effective_aspect_ratio": aspect_ratio,
            }
        )
    return GeminiImageConfig(
        aspect_ratio=aspect_ratio,
        image_size=_gemini_image_size_for_model(width, height, model),
        output_mime_type=output_mime_type,
        notes=notes,
    )


def _response_parts(response: Any) -> list[Any]:
    parts = getattr(response, "parts", None)
    if parts:
        return list(parts)
    candidates = getattr(response, "candidates", None) or []
    all_parts: list[Any] = []
    for candidate in candidates:
        content = getattr(candidate, "content", None)
        all_parts.extend(getattr(content, "parts", None) or [])
    return all_parts


def _inline_data_bytes(inline_data: Any) -> bytes | None:
    raw = getattr(inline_data, "data", None)
    if raw is None:
        return None
    if isinstance(raw, bytes):
        return raw
    if isinstance(raw, str):
        return b64decode(raw)
    return None


class GoogleGeminiImageClient:
    provider_name = "google-gemini-image"
    prompt_version = "gemini-generate-content-image-v1"

    def __init__(self, provider_config: ResolvedImageProviderConfig | None = None) -> None:
        resolved_config = provider_config or resolve_image_provider_config()
        self.api_key = resolved_config.api_key
        self.base_url = resolved_config.base_url
        self.model = resolved_config.model or GEMINI_DEFAULT_IMAGE_MODEL
        self.api_version = resolved_config.gemini_api_version or "v1beta"
        self.output_mime_type = resolved_config.gemini_output_mime_type

    def _client(self) -> genai.Client:
        if not self.api_key:
            raise RuntimeError("图片供应商档案缺少 API Key")
        if self.base_url:
            raise RuntimeError("Google Gemini 供应商暂不支持自定义 Base URL")
        return genai.Client(
            api_key=self.api_key,
            http_options=types.HttpOptions(apiVersion=self.api_version),
        )

    def generate_image(
        self,
        *,
        prompt: str,
        size: str,
        reference_images: list[GoogleGeminiReferenceImage] | None = None,
    ) -> GeminiImageResult:
        reference_images = reference_images or []
        client = self._client()
        image_config = map_productflow_size_to_gemini_image_config(
            size,
            self.model,
            output_mime_type=self.output_mime_type,
        )
        parts = [
            types.Part.from_text(text=prompt),
            *[
                types.Part.from_bytes(data=reference.bytes_data, mime_type=reference.mime_type)
                for reference in reference_images
            ],
        ]
        request_json = {
            "model": self.model,
            "prompt": prompt,
            "size": size,
            "reference_image_count": len(reference_images),
            "reference_images": [
                {
                    "filename": reference.filename,
                    "mime_type": reference.mime_type,
                    "byte_count": len(reference.bytes_data),
                }
                for reference in reference_images
            ],
            "image_config": {
                "aspect_ratio": image_config.aspect_ratio,
                **({"image_size": image_config.image_size} if image_config.image_size else {}),
                **({"output_mime_type": image_config.output_mime_type} if image_config.output_mime_type else {}),
            },
        }
        config = types.GenerateContentConfig(
            response_modalities=[types.Modality.TEXT, types.Modality.IMAGE],
            image_config=types.ImageConfig(
                aspectRatio=image_config.aspect_ratio,
                imageSize=image_config.image_size,
                outputMimeType=image_config.output_mime_type,
            ),
        )
        try:
            response = client.models.generate_content(
                model=self.model,
                contents=parts,
                config=config,
            )
        except Exception as exc:  # noqa: BLE001
            logger.error("Google Gemini 图片供应商请求失败: error_class=%s", type(exc).__name__)
            raise RuntimeError(PROVIDER_REQUEST_FAILURE_MESSAGE) from exc

        image_bytes: bytes | None = None
        mime_type: str | None = None
        text_parts: list[str] = []
        for part in _response_parts(response):
            inline_data = getattr(part, "inline_data", None)
            if inline_data is not None:
                image_bytes = _inline_data_bytes(inline_data)
                mime_type = getattr(inline_data, "mime_type", None)
                if image_bytes:
                    break
            text = getattr(part, "text", None)
            if isinstance(text, str) and text.strip():
                text_parts.append(text.strip())

        if image_bytes is None:
            raise RuntimeError(PROVIDER_MISSING_OUTPUT_MESSAGE)

        response_id = str(getattr(response, "response_id", "") or "") or None
        model_version = str(getattr(response, "model_version", "") or "") or None
        productflow_metadata: dict[str, Any] = {
            "model": self.model,
            "requested_size": size,
            "effective_aspect_ratio": image_config.aspect_ratio,
        }
        if image_config.image_size:
            productflow_metadata["effective_image_size"] = image_config.image_size
        if image_config.notes:
            productflow_metadata["notes"] = image_config.notes

        output_json = {
            "response_id": response_id,
            "model_version": model_version,
            "text_part_count": len(text_parts),
            "_productflow": productflow_metadata,
        }
        return GeminiImageResult(
            bytes_data=image_bytes,
            mime_type=(
                mime_type
                if isinstance(mime_type, str) and mime_type.startswith("image/")
                else _mime_type_from_image_bytes(image_bytes)
            ),
            model_name=self.model,
            provider_name=self.provider_name,
            prompt_version=self.prompt_version,
            size=size,
            generated_at=datetime.now(UTC),
            provider_response_id=response_id,
            provider_request_json=request_json,
            provider_output_json=output_json,
        )


class GoogleGeminiImageProvider(ImageProvider):
    provider_name = "google-gemini-image"
    prompt_version = "gemini-poster-image-v1"

    def __init__(self, provider_config: ResolvedImageProviderConfig | None = None) -> None:
        self.provider_config = provider_config or resolve_image_provider_config()

    def generate_poster_image(
        self,
        poster: PosterGenerationInput,
        kind: PosterKind,
    ) -> tuple[GeneratedImagePayload, str]:
        settings = get_runtime_settings()
        size = poster.image_size or (
            settings.image_main_image_size if kind == PosterKind.MAIN_IMAGE else settings.image_promo_poster_size
        )
        prompt = self._build_prompt(poster, kind, size, settings)
        result = GoogleGeminiImageClient(self.provider_config).generate_image(
            prompt=prompt,
            size=size,
            reference_images=self._build_reference_images_from_poster(poster),
        )
        width, height = parse_size(size)
        dims = image_dimensions_from_bytes(result.bytes_data)
        if dims:
            width, height = dims
        payload = GeneratedImagePayload(
            kind=kind,
            bytes_data=result.bytes_data,
            mime_type=result.mime_type,
            width=width,
            height=height,
            variant_label="v1",
            provider_response_id=result.provider_response_id,
            provider_output_json=result.provider_output_json,
        )
        return payload, result.model_name

    def _build_prompt(self, poster: PosterGenerationInput, kind: PosterKind, size: str, settings: Any) -> str:
        copy_mode = poster.copy_prompt_mode == "copy"
        template = settings.prompt_poster_image_template if copy_mode else settings.prompt_poster_image_edit_template
        return render_prompt_template(
            template,
            {
                "product_name": poster.product_name,
                "category": poster.category or "",
                "price": poster.price or "",
                "source_note": poster.source_note or "",
                "instruction": poster.instruction or "Free image generation.",
                "context_block": self._build_context_block(poster),
                "reference_policy": (
                    settings.prompt_poster_image_reference_policy if poster_has_reference_input(poster) else ""
                ),
                "visible_text_language_hint": poster.visible_text_language_hint or "",
                "size": size,
                "kind": kind.value,
                "kind_label": "main image" if kind == PosterKind.MAIN_IMAGE else "promotional poster",
                "kind_requirements": self._build_kind_requirements(
                    kind,
                    visible_text_language_hint=poster.visible_text_language_hint,
                ),
            },
        )

    def _build_context_block(self, poster: PosterGenerationInput) -> str:
        lines: list[str] = []
        if poster.product_name:
            lines.append(f"- Subject: {poster.product_name}")
        if poster.category:
            lines.append(f"- Category/type: {poster.category}")
        if poster.price:
            lines.append(f"- Price: {poster.price}")
        if poster.source_note:
            lines.append(f"- Additional notes: {poster.source_note}")
        if poster.copy_prompt_mode == "copy" and poster.structured_copy_context:
            lines.append(
                "- Available copy text (use only when visible text is requested or clearly useful; "
                "do not render field names, labels, or context notes):\n"
                f"{poster.structured_copy_context}"
            )
        if poster.reference_images or poster.source_image is not None:
            reference_paths = {str(reference.path.resolve()) for reference in poster.reference_images}
            if poster.source_image is not None:
                reference_paths.add(str(poster.source_image.resolve()))
            lines.append(f"- Reference image count: {len(reference_paths)}")
            if poster.source_image is not None:
                lines.append("- Source product image: input image 1")
            reference_labels = [
                f"{reference.label or reference.filename} (role: {reference.role or 'reference'})"
                for reference in poster.reference_images
            ]
            if reference_labels:
                lines.append(f"- Reference images: {'; '.join(reference_labels)}")
        return "\n".join(lines) if lines else "- No explicit upstream context."

    def _build_kind_requirements(self, kind: PosterKind, *, visible_text_language_hint: str | None = None) -> str:
        return image_visible_text_requirements(kind, visible_text_language_hint=visible_text_language_hint)

    def _build_reference_images_from_poster(self, poster: PosterGenerationInput) -> list[GoogleGeminiReferenceImage]:
        return [
            GoogleGeminiReferenceImage(
                bytes_data=reference.bytes_data,
                mime_type=reference.mime_type,
                filename=reference.filename,
            )
            for reference in build_responses_reference_images_from_poster(poster)
        ]
