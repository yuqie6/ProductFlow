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

from productflow_backend.domain.local_image_edits import LocalImageEditOperation
from productflow_backend.infrastructure.image.base import (
    LOCAL_EDIT_MODE,
    ImageProvider,
    LocalEditCapability,
    LocalEditImage,
    LocalEditRequest,
    LocalEditResult,
    UnsupportedLocalEditError,
    WorkflowGeneratedImage,
    WorkflowImageReference,
    WorkflowImageRequest,
    WorkflowImageResult,
    decode_b64_image,
    infer_extension,
    map_generation_spec_to_openai_size,
    map_pixel_size_to_openai_size,
)
from productflow_backend.infrastructure.provider_config import (
    ResolvedImageProviderConfig,
    resolve_image_provider_config,
)

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

    def __init__(self, provider_config: ResolvedImageProviderConfig | None = None) -> None:
        resolved_config = provider_config or resolve_image_provider_config()
        self.api_key = resolved_config.api_key
        self.base_url = resolved_config.base_url
        self.model = resolved_config.model
        self.quality = resolved_config.images_quality
        self.style = resolved_config.images_style

    def _client(self) -> OpenAI:
        if not self.api_key:
            raise RuntimeError("图片供应商档案缺少 API Key")
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
                logger.error("OpenAI Images API generate 失败: error_class=%s", type(exc).__name__)
                raise RuntimeError(PROVIDER_REQUEST_FAILURE_MESSAGE) from exc
            fallback_used = True
            fallback_params = {
                key: value for key, value in request_params.items() if key not in {"quality", "style"}
            }
            try:
                response = client.images.generate(**fallback_params)
                request_params = fallback_params
            except Exception as fallback_exc:  # noqa: BLE001
                logger.error("OpenAI Images API generate fallback 失败: error_class=%s", type(fallback_exc).__name__)
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
        mask_mime_type: str = "image/png",
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
            mask_file.name = f"mask{infer_extension(mask_mime_type)}"
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
                logger.error("OpenAI Images API edit 失败: error_class=%s", type(exc).__name__)
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
                logger.error("OpenAI Images API edit fallback 失败: error_class=%s", type(fallback_exc).__name__)
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

    def __init__(self, provider_config: ResolvedImageProviderConfig | None = None) -> None:
        self.provider_config = provider_config or resolve_image_provider_config()

    @property
    def local_edit_capability(self) -> LocalEditCapability:
        if not self.provider_config.masked_local_edit_available:
            return LocalEditCapability.unsupported(
                self.provider_name,
                reason="openai_images 未显式声明 image_mask_edit 能力",
            )
        return LocalEditCapability(
            provider_name=self.provider_name,
            supported=True,
            mode=LOCAL_EDIT_MODE,
            operations=(
                LocalImageEditOperation.REMOVE,
                LocalImageEditOperation.REPLACE_TEXT,
                LocalImageEditOperation.INPAINT,
            ),
            requires_mask=True,
        )

    def generate_workflow_image(self, request: WorkflowImageRequest) -> WorkflowImageResult:
        size = map_generation_spec_to_openai_size(request.generation_spec)
        quality = {
            "draft": "low",
            "standard": "medium",
            "high": "high",
        }[request.generation_spec.quality_intent]
        client = OpenAIImagesClient(self.provider_config)
        if request.references:
            operation = "edit"
            results = client.edit(
                image=[_images_reference(reference) for reference in request.references],
                prompt=request.compiled_prompt,
                size=size,
                quality=quality,
                n=1,
            )
        else:
            operation = "generation"
            results = client.generate(
                prompt=request.compiled_prompt,
                size=size,
                quality=quality,
                n=1,
            )
        if len(results) != 1:
            raise RuntimeError("单图生成要求 Images API 恰好返回一张图片")
        result = results[0]
        output_metadata = result.provider_output_json.get("_productflow")
        output_metadata = output_metadata if isinstance(output_metadata, dict) else {}
        notes = [dict(note) for note in output_metadata.get("notes", []) if isinstance(note, dict)]
        if request.references:
            notes.append(
                {
                    "kind": "reference_fidelity_prompt_only",
                    "requested": request.generation_spec.reference_fidelity,
                    "message": "Images API 没有独立 reference_fidelity 参数，参考保真要求仅写入编译提示词。",
                }
            )
        if request.generation_spec.background_intent != "auto":
            notes.append(
                {
                    "kind": "background_prompt_only",
                    "requested": request.generation_spec.background_intent,
                    "message": "当前 Images API adapter 没有发送 background 参数，背景要求仅写入编译提示词。",
                }
            )
        effective_parameters = _images_effective_parameters(
            result,
            operation=operation,
            reference_count=len(request.references),
            notes=notes,
        )
        return WorkflowImageResult(
            images=(WorkflowGeneratedImage(bytes_data=result.bytes_data, mime_type=result.mime_type),),
            model=result.model_name,
            provider_status="completed",
            effective_parameters=effective_parameters,
            provider_request_json=result.provider_request_json,
            provider_output_json=result.provider_output_json,
        )

    def edit_local(self, request: LocalEditRequest) -> LocalEditResult:
        capability = self.local_edit_capability
        if not capability.supported:
            raise UnsupportedLocalEditError(capability.reason or "OpenAI Images 不支持 masked local edit")

        image_inputs = [
            _local_edit_reference(request.source_image),
            *(_local_edit_reference(reference) for reference in request.reference_images),
        ]
        effective_size = map_pixel_size_to_openai_size(request.size)
        results = OpenAIImagesClient(self.provider_config).edit(
            image=image_inputs,
            prompt=request.instruction,
            size=effective_size,
            mask=request.mask.bytes_data,
            mask_mime_type=request.mask.mime_type,
            n=1,
        )
        if len(results) != 1:
            raise RuntimeError("局部编辑要求 Images API 恰好返回一张图片")
        result = results[0]
        provider_output_json = _with_local_edit_metadata(
            result.provider_output_json,
            operation=request.operation,
            requested_size=request.size,
            effective_size=effective_size,
        )
        effective_parameters = _local_edit_effective_parameters(
            result,
            operation=request.operation,
            requested_reference_count=len(request.reference_images),
            requested_size=request.size,
        )
        return LocalEditResult(
            images=(WorkflowGeneratedImage(bytes_data=result.bytes_data, mime_type=result.mime_type),),
            model=result.model_name,
            provider_status="completed",
            effective_mode=LOCAL_EDIT_MODE,
            effective_parameters=effective_parameters,
            provider_request_json=result.provider_request_json,
            provider_output_json=provider_output_json,
        )


def _images_reference(reference: WorkflowImageReference) -> ImagesReferenceImage:
    return ImagesReferenceImage(
        bytes_data=reference.bytes_data,
        mime_type=reference.mime_type,
        filename=reference.filename,
    )


def _local_edit_reference(image: LocalEditImage) -> ImagesReferenceImage:
    return ImagesReferenceImage(
        bytes_data=image.bytes_data,
        mime_type=image.mime_type,
        filename=image.filename,
    )


def _with_local_edit_metadata(
    provider_output_json: dict[str, Any],
    *,
    operation: LocalImageEditOperation,
    requested_size: str,
    effective_size: str,
) -> dict[str, Any]:
    output = dict(provider_output_json)
    metadata = dict(output.get("_productflow") or {})
    metadata.update(
        {
            "effective_mode": LOCAL_EDIT_MODE,
            "operation": operation,
            "requested_size": requested_size,
            "effective_size": effective_size,
        }
    )
    output["_productflow"] = metadata
    return output


def _local_edit_effective_parameters(
    result: ImagesAPIResult,
    *,
    operation: LocalImageEditOperation,
    requested_reference_count: int,
    requested_size: str,
) -> dict[str, Any]:
    output_metadata = result.provider_output_json.get("_productflow")
    effective_image_count = output_metadata.get("effective_image_count") if isinstance(output_metadata, dict) else None
    effective_reference_count = requested_reference_count
    if isinstance(effective_image_count, int):
        effective_reference_count = max(0, effective_image_count - 1)
    effective = {
        key: value
        for key, value in result.provider_request_json.items()
        if key in {"model", "size", "n", "quality", "image_count", "has_mask"}
    }
    return {
        "adapter": "openai_images",
        "mode": LOCAL_EDIT_MODE,
        "operation": operation,
        "requested_size": requested_size,
        "reference_image_count": effective_reference_count,
        **effective,
    }


def _images_effective_parameters(
    result: ImagesAPIResult,
    *,
    operation: str,
    reference_count: int,
    notes: list[dict[str, Any]],
) -> dict[str, Any]:
    request_json = result.provider_request_json
    effective = {
        key: value
        for key, value in request_json.items()
        if key in {"model", "size", "n", "quality", "style", "image_count"}
    }
    effective_reference_count = reference_count
    output_metadata = result.provider_output_json.get("_productflow")
    if isinstance(output_metadata, dict):
        effective_count = output_metadata.get("effective_image_count")
        if isinstance(effective_count, int):
            effective_reference_count = effective_count
    return {
        "adapter": "openai_images",
        "operation": operation,
        "reference_image_count": effective_reference_count,
        **effective,
        "notes": notes,
    }
