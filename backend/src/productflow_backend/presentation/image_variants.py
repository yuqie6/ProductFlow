from __future__ import annotations

from pathlib import Path

from fastapi import HTTPException
from fastapi.responses import FileResponse

from productflow_backend.infrastructure.storage import ImageVariantName, LocalStorage


def build_image_urls(base_download_url: str) -> dict[str, str]:
    return {
        "download_url": base_download_url,
        "preview_url": f"{base_download_url}?variant=preview",
        "thumbnail_url": f"{base_download_url}?variant=thumbnail",
    }


def build_variant_filename(original_filename: str, *, variant: str, resolved_suffix: str) -> str:
    if variant == "original":
        return original_filename
    stem = Path(original_filename).stem or "image"
    return f"{stem}-{variant}{resolved_suffix}"


def serve_image_variant(
    *,
    storage_path: str,
    original_filename: str,
    mime_type: str,
    variant: ImageVariantName,
    missing_file_detail: str,
) -> FileResponse:
    try:
        path, media_type = LocalStorage().resolve_for_variant(storage_path, variant, fallback_media_type=mime_type)
    except ValueError as exc:
        raise HTTPException(status_code=404, detail=missing_file_detail) from exc
    if not path.exists():
        raise HTTPException(status_code=404, detail=missing_file_detail)
    filename = build_variant_filename(original_filename, variant=variant, resolved_suffix=path.suffix)
    return FileResponse(path, media_type=media_type, filename=filename)
