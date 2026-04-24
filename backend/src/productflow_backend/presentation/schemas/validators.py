from __future__ import annotations

from productflow_backend.config import get_runtime_settings, normalize_image_size


def validate_allowed_image_size(size: str) -> str:
    normalized = normalize_image_size(size)
    allowed = get_runtime_settings().allowed_image_sizes
    if normalized not in allowed:
        allowed_text = ", ".join(sorted(allowed))
        raise ValueError(f"图片尺寸必须是以下之一: {allowed_text}")
    return normalized
