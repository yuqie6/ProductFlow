from __future__ import annotations

from dataclasses import dataclass
from io import BytesIO

from fastapi import HTTPException, UploadFile, status
from PIL import Image, UnidentifiedImageError

from productflow_backend.infrastructure.runtime_settings import get_runtime_settings


@dataclass(frozen=True, slots=True)
class ValidatedUpload:
    content: bytes
    filename: str
    mime_type: str


_IMAGE_FORMAT_MIME_TYPES = {
    "PNG": "image/png",
    "JPEG": "image/jpeg",
    "WEBP": "image/webp",
}

_IMAGE_MIME_ALIASES = {
    "image/jpg": "image/jpeg",
    "image/pjpeg": "image/jpeg",
    "image/x-png": "image/png",
}


def format_byte_size(size_bytes: int) -> str:
    """将字节数格式化为人类友好的 MB/KB/B 格式。"""
    if size_bytes >= 1024 * 1024:
        mb = size_bytes / (1024 * 1024)
        return f"{mb:.1f}MB".replace(".0MB", "MB")
    if size_bytes >= 1024:
        kb = size_bytes / 1024
        return f"{kb:.1f}KB".replace(".0KB", "KB")
    return f"{size_bytes}B"


def normalize_declared_mime_type(declared: str | None) -> str:
    cleaned = (declared or "application/octet-stream").split(";", maxsplit=1)[0].strip().lower()
    return _IMAGE_MIME_ALIASES.get(cleaned, cleaned)


async def read_validated_image_upload(upload: UploadFile, *, fallback_filename: str) -> ValidatedUpload:
    """校验单张上传图片：MIME 类型 / 单图大小 / 像素 / 内容真实格式。"""
    settings = get_runtime_settings()
    filename = upload.filename or fallback_filename
    declared_mime = normalize_declared_mime_type(upload.content_type)

    if declared_mime != "application/octet-stream" and declared_mime not in settings.allowed_image_mime_types:
        raise HTTPException(
            status_code=status.HTTP_415_UNSUPPORTED_MEDIA_TYPE,
            detail=f"图片“{filename}”格式不受支持: {declared_mime}",
        )

    content = await upload.read(settings.upload_max_image_bytes + 1)
    if len(content) > settings.upload_max_image_bytes:
        raise HTTPException(
            status_code=status.HTTP_413_CONTENT_TOO_LARGE,
            detail=f"图片“{filename}”超过单张大小限制: {format_byte_size(settings.upload_max_image_bytes)}",
        )
    if not content:
        raise HTTPException(status_code=400, detail=f"图片“{filename}”内容不能为空")

    try:
        with Image.open(BytesIO(content)) as image:
            image.verify()
        with Image.open(BytesIO(content)) as image:
            width, height = image.size
            detected_mime = _IMAGE_FORMAT_MIME_TYPES.get(image.format or "")
    except (OSError, UnidentifiedImageError) as exc:
        raise HTTPException(status_code=400, detail=f"上传文件“{filename}”不是可解码的有效图片") from exc

    if width <= 0 or height <= 0 or width * height > settings.upload_max_pixels:
        raise HTTPException(
            status_code=400,
            detail=f"图片“{filename}”像素数 ({width}x{height}) 超过系统限制: {settings.upload_max_pixels}",
        )
    if detected_mime not in settings.allowed_image_mime_types:
        raise HTTPException(status_code=415, detail=f"图片“{filename}”真实格式不受支持: {detected_mime}")
    if declared_mime != "application/octet-stream" and detected_mime != declared_mime:
        raise HTTPException(
            status_code=400,
            detail=f"图片“{filename}”内容格式 ({detected_mime}) 与声明类型 ({declared_mime}) 不一致",
        )

    return ValidatedUpload(content=content, filename=filename, mime_type=detected_mime)


async def read_validated_image_uploads_batch(
    uploads: list[UploadFile],
    *,
    fallback_prefix: str = "upload",
) -> list[ValidatedUpload]:
    """校验多图批量上传：批次文件数上限 / 批次总大小上限 / 逐张严格校验。"""
    settings = get_runtime_settings()
    if not uploads:
        raise HTTPException(status_code=400, detail="至少需要上传一张图片")

    max_batch_files = getattr(settings, "upload_max_batch_files", 20)
    if len(uploads) > max_batch_files:
        raise HTTPException(
            status_code=400,
            detail=f"单批次最多上传 {max_batch_files} 张图片，当前提交了 {len(uploads)} 张",
        )

    max_batch_bytes = getattr(settings, "upload_max_batch_bytes", 50 * 1024 * 1024)
    total_bytes = 0
    validated_list: list[ValidatedUpload] = []

    for index, upload in enumerate(uploads):
        validated = await read_validated_image_upload(
            upload,
            fallback_filename=f"{fallback_prefix}-{index + 1}.png",
        )
        total_bytes += len(validated.content)
        if total_bytes > max_batch_bytes:
            raise HTTPException(
                status_code=status.HTTP_413_CONTENT_TOO_LARGE,
                detail=(
                    f"批量上传总大小超过限制: {format_byte_size(max_batch_bytes)} "
                    f"(当前已读取 {format_byte_size(total_bytes)})"
                ),
            )
        validated_list.append(validated)

    return validated_list


def validate_reference_image_count(count: int) -> None:
    max_count = get_runtime_settings().upload_max_reference_images
    if count > max_count:
        raise HTTPException(status_code=400, detail=f"参考图最多上传 {max_count} 张")

