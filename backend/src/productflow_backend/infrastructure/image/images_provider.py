"""OpenAI Images API provider (/v1/images/generations, /v1/images/edits).

Supports any OpenAI-compatible image generation endpoint (DALL-E, SD WebUI, ComfyUI wrappers, etc.).
"""

from __future__ import annotations

import logging
from collections.abc import Sequence
from dataclasses import dataclass
from datetime import UTC, datetime
from io import BytesIO
from typing import Any

from openai import OpenAI

from productflow_backend.application.contracts import PosterGenerationInput
from productflow_backend.config import get_runtime_settings
from productflow_backend.domain.enums import PosterKind
from productflow_backend.infrastructure.image.base import (
    GeneratedImagePayload,
    ImageProvider,
    decode_b64_image,
    image_dimensions_from_bytes,
    parse_size,
)
from productflow_backend.infrastructure.image.responses_provider import (
    build_responses_reference_images_from_poster,
    poster_has_reference_input,
)
from productflow_backend.infrastructure.prompts import render_prompt_template

logger = logging.getLogger(__name__)

PROVIDER_REQUEST_FAILURE_MESSAGE = "图片供应商请求失败，请检查供应商配置后重试"
PROVIDER_MISSING_OUTPUT_MESSAGE = "图片供应商没有返回图片结果，请稍后重试"
OPTIONAL_FIELDS_FALLBACK_NOTE = {
    "kind": "fallback",
    "message": "供应商不支持部分可选参数，已按基础参数完成。",
}
MULTI_IMAGE_FALLBACK_NOTE = {
    "kind": "multi_image_fallback",
    "message": "供应商不支持多张编辑输入，已仅使用基图完成。",
}


@dataclass(slots=True)
class ImagesAPIResult:
    bytes_data: bytes
    mime_type: str
    model_name: str
    size: str
    generated_at: datetime
    revised_prompt: str | None
    provider_request_json: dict[str, Any]
    provider_output_json: dict[str, Any]


@dataclass(slots=True)
class ImagesReferenceImage:
    bytes_data: bytes
    mime_type: str
    filename: str


def _mime_type_from_image_bytes(data: bytes) -> str:
    if data.startswith(b"\x89PNG\r\n\x1a\n"):
        return "image/png"
    if data.startswith(b"\xff\xd8\xff"):
        return "image/jpeg"
    if data.startswith(b"RIFF") and data[8:12] == b"WEBP":
        return "image/webp"
    return "image/png"


class OpenAIImagesClient:
    """Thin wrapper around the OpenAI Images API (generations + edits)."""

    provider_name = "openai-images"

    def __init__(self) -> None:
        settings = get_runtime_settings()
        self.api_key = settings.image_api_key
        self.base_url = settings.image_base_url
        self.model = settings.image_generate_model
        self.quality = settings.image_images_quality
        self.style = settings.image_images_style

    def _client(self) -> OpenAI:
        if not self.api_key:
            raise RuntimeError("图片供应商缺少 IMAGE_API_KEY")
        kwargs: dict[str, Any] = {"api_key": self.api_key}
        if self.base_url:
            kwargs["base_url"] = self.base_url
        return OpenAI(**kwargs)

    def _parse_response(
        self,
        response: Any,
        *,
        model: str,
        size: str,
        provider_request_json: dict[str, Any],
        provider_output_json: dict[str, Any] | None = None,
    ) -> list[ImagesAPIResult]:
        results: list[ImagesAPIResult] = []
        now = datetime.now(UTC)
        for item in getattr(response, "data", []) or []:
            b64 = getattr(item, "b64_json", None)
            if not b64:
                continue
            image_bytes = decode_b64_image(b64)
            results.append(
                ImagesAPIResult(
                    bytes_data=image_bytes,
                    mime_type=_mime_type_from_image_bytes(image_bytes),
                    model_name=model,
                    size=size,
                    generated_at=now,
                    revised_prompt=getattr(item, "revised_prompt", None),
                    provider_request_json=provider_request_json,
                    provider_output_json=provider_output_json or {},
                )
            )

        if not results:
            raise RuntimeError(PROVIDER_MISSING_OUTPUT_MESSAGE)
        return results

    def _with_productflow_metadata(
        self,
        provider_output_json: dict[str, Any] | None,
        *,
        notes: list[dict[str, Any]],
        requested_image_count: int | None = None,
        effective_image_count: int | None = None,
    ) -> dict[str, Any]:
        output = dict(provider_output_json or {})
        metadata = dict(output.get("_productflow") or {})
        if notes:
            metadata["notes"] = notes
        if requested_image_count is not None:
            metadata["requested_image_count"] = requested_image_count
        if effective_image_count is not None:
            metadata["effective_image_count"] = effective_image_count
        if metadata:
            output["_productflow"] = metadata
        return output

    def _should_retry_without_optional_fields(self, request_params: dict[str, Any]) -> bool:
        return any(key in request_params for key in ("quality", "style"))

    def generate(
        self,
        *,
        prompt: str,
        size: str,
        model: str | None = None,
        quality: str | None = None,
        style: str | None = None,
        n: int = 1,
    ) -> list[ImagesAPIResult]:
        client = self._client()
        req_model = model or self.model
        req_quality = quality or self.quality
        req_style = style or self.style

        request_params: dict[str, Any] = {
            "model": req_model,
            "prompt": prompt,
            "size": size,
            "n": n,
            "response_format": "b64_json",
        }
        if req_quality:
            request_params["quality"] = req_quality
        if req_style:
            request_params["style"] = req_style

        fallback_used = False
        try:
            response = client.images.generate(**request_params)
        except Exception as exc:  # noqa: BLE001
            if not self._should_retry_without_optional_fields(request_params):
                logger.error("OpenAI Images API generate 失败: %s", exc, exc_info=True)
                raise RuntimeError(PROVIDER_REQUEST_FAILURE_MESSAGE) from exc
            fallback_used = True
            fallback_params = {
                key: value for key, value in request_params.items() if key not in {"quality", "style"}
            }
            try:
                response = client.images.generate(**fallback_params)
                request_params = fallback_params
            except Exception as fallback_exc:  # noqa: BLE001
                logger.error("OpenAI Images API generate 失败: %s", fallback_exc, exc_info=True)
                raise RuntimeError(PROVIDER_REQUEST_FAILURE_MESSAGE) from fallback_exc

        provider_output_json = self._with_productflow_metadata(
            None,
            notes=[OPTIONAL_FIELDS_FALLBACK_NOTE] if fallback_used else [],
        )
        return self._parse_response(
            response,
            model=req_model,
            size=size,
            provider_request_json={k: v for k, v in request_params.items() if k != "response_format"},
            provider_output_json=provider_output_json,
        )

    def edit(
        self,
        *,
        image: bytes | Sequence[ImagesReferenceImage],
        prompt: str,
        size: str,
        mask: bytes | None = None,
        model: str | None = None,
        quality: str | None = None,
        n: int = 1,
    ) -> list[ImagesAPIResult]:
        client = self._client()
        req_model = model or self.model
        req_quality = quality or self.quality

        image_files, image_metadata = self._build_image_files(image)

        request_params: dict[str, Any] = {
            "model": req_model,
            "image": image_files[0] if len(image_files) == 1 else image_files,
            "prompt": prompt,
            "size": size,
            "n": n,
            "response_format": "b64_json",
        }
        if req_quality:
            request_params["quality"] = req_quality
        if mask is not None:
            mask_file = BytesIO(mask)
            mask_file.name = "mask.png"
            request_params["mask"] = mask_file

        log_params = self._sanitize_edit_request_params(
            request_params,
            image_count=len(image_files),
            image_metadata=image_metadata,
            has_mask=mask is not None,
        )

        fallback_notes: list[dict[str, Any]] = []
        requested_image_count = len(image_files)
        effective_image_count = len(image_files)
        try:
            response = client.images.edit(**request_params)
        except Exception as exc:  # noqa: BLE001
            fallback_params = dict(request_params)
            can_reduce_optional = self._should_retry_without_optional_fields(fallback_params)
            can_reduce_images = len(image_files) > 1
            if not can_reduce_optional and not can_reduce_images:
                logger.error("OpenAI Images API edit 失败: %s", exc, exc_info=True)
                raise RuntimeError(PROVIDER_REQUEST_FAILURE_MESSAGE) from exc
            if can_reduce_optional:
                fallback_params = {
                    key: value for key, value in fallback_params.items() if key not in {"quality", "style"}
                }
                fallback_notes.append(OPTIONAL_FIELDS_FALLBACK_NOTE)
            if can_reduce_images:
                fallback_params["image"] = image_files[0]
                effective_image_count = 1
                fallback_notes.append(MULTI_IMAGE_FALLBACK_NOTE)
            try:
                response = client.images.edit(**fallback_params)
                request_params = fallback_params
                log_params = self._sanitize_edit_request_params(
                    request_params,
                    image_count=effective_image_count,
                    image_metadata=image_metadata[:effective_image_count],
                    has_mask=mask is not None,
                )
            except Exception as fallback_exc:  # noqa: BLE001
                logger.error("OpenAI Images API edit 失败: %s", fallback_exc, exc_info=True)
                raise RuntimeError(PROVIDER_REQUEST_FAILURE_MESSAGE) from fallback_exc

        provider_output_json = self._with_productflow_metadata(
            None,
            notes=fallback_notes,
            requested_image_count=requested_image_count,
            effective_image_count=effective_image_count,
        )
        return self._parse_response(
            response,
            model=req_model,
            size=size,
            provider_request_json=log_params,
            provider_output_json=provider_output_json,
        )

    def _build_image_files(
        self,
        image: bytes | Sequence[ImagesReferenceImage],
    ) -> tuple[list[BytesIO], list[dict[str, str]]]:
        if isinstance(image, bytes):
            image_file = BytesIO(image)
            image_file.name = "image.png"
            return [image_file], [{"filename": "image.png", "mime_type": _mime_type_from_image_bytes(image)}]

        files: list[BytesIO] = []
        metadata: list[dict[str, str]] = []
        for index, reference in enumerate(image, start=1):
            image_file = BytesIO(reference.bytes_data)
            image_file.name = reference.filename or f"image-{index}.png"
            files.append(image_file)
            metadata.append({"filename": image_file.name, "mime_type": reference.mime_type})
        if not files:
            raise RuntimeError("图片供应商缺少编辑输入图片")
        return files, metadata

    def _sanitize_edit_request_params(
        self,
        request_params: dict[str, Any],
        *,
        image_count: int,
        image_metadata: list[dict[str, str]],
        has_mask: bool,
    ) -> dict[str, Any]:
        log_params = {k: v for k, v in request_params.items() if k not in {"image", "mask", "response_format"}}
        log_params["image_count"] = image_count
        log_params["images"] = image_metadata
        log_params["has_mask"] = has_mask
        return log_params


class OpenAIImagesImageProvider(ImageProvider):
    """ImageProvider implementation backed by the standard OpenAI Images API."""

    provider_name = "openai-images"
    prompt_version = "images-api-v1"

    def generate_poster_image(
        self,
        poster: PosterGenerationInput,
        kind: PosterKind,
    ) -> tuple[GeneratedImagePayload, str]:
        settings = get_runtime_settings()
        client = OpenAIImagesClient()

        size = poster.image_size or (
            settings.image_main_image_size if kind == PosterKind.MAIN_IMAGE else settings.image_promo_poster_size
        )
        prompt = self._build_prompt(poster, kind, size, settings)
        reference_images = self._build_reference_images_from_poster(poster)

        if reference_images:
            results = client.edit(image=reference_images, prompt=prompt, size=size)
        else:
            results = client.generate(prompt=prompt, size=size)

        result = results[0]
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
                "instruction": poster.instruction or "自由生成。",
                "context_block": self._build_context_block(poster),
                "reference_policy": (
                    settings.prompt_poster_image_reference_policy if poster_has_reference_input(poster) else ""
                ),
                "size": size,
                "kind": kind.value,
                "kind_label": "主图" if kind == PosterKind.MAIN_IMAGE else "促销海报",
                "kind_requirements": self._build_kind_requirements(kind),
            },
        )

    def _build_context_block(self, poster: PosterGenerationInput) -> str:
        lines: list[str] = []
        if poster.product_name:
            lines.append(f"- 画面主体：{poster.product_name}")
        if poster.category:
            lines.append(f"- 类目/类型：{poster.category}")
        if poster.price:
            lines.append(f"- 价格：{poster.price}")
        if poster.source_note:
            lines.append(f"- 补充说明：{poster.source_note}")
        if poster.copy_prompt_mode == "copy" and poster.structured_copy_context:
            lines.append(
                "- 可用文案参考（仅在用户要求图片包含文字时使用，不要绘制字段名、标签名或上下文说明）：\n"
                f"{poster.structured_copy_context}"
            )
        if poster.reference_images or poster.source_image is not None:
            reference_paths = {str(reference.path.resolve()) for reference in poster.reference_images}
            if poster.source_image is not None:
                reference_paths.add(str(poster.source_image.resolve()))
            lines.append(f"- 参考图片数量：{len(reference_paths)}")
            if poster.source_image is not None:
                lines.append("- 商品原图：第 1 张输入图片")
            reference_labels = [
                f"{reference.label or reference.filename}（角色：{reference.role or '参考图'}）"
                for reference in poster.reference_images
            ]
            if reference_labels:
                lines.append(f"- 参考图：{'；'.join(reference_labels)}")
        return "\n".join(lines) if lines else "- 无显式上游上下文。"

    def _build_kind_requirements(self, kind: PosterKind) -> str:
        kind_label = "主图" if kind == PosterKind.MAIN_IMAGE else "海报/竖图"
        return (
            f"输出用途：{kind_label}。上游上下文只用于理解画面主体、材质、场景和文案参考；"
            "不要把字段名、标签名、JSON key、上下文说明、品牌、水印或 UI 面板画进图片。"
        )

    def _build_reference_images_from_poster(self, poster: PosterGenerationInput) -> list[ImagesReferenceImage]:
        return [
            ImagesReferenceImage(
                bytes_data=reference.bytes_data,
                mime_type=reference.mime_type,
                filename=reference.filename or f"reference-{index}.png",
            )
            for index, reference in enumerate(build_responses_reference_images_from_poster(poster), start=1)
        ]
