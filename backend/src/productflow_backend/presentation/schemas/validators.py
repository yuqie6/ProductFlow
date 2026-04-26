from __future__ import annotations

from productflow_backend.config import normalize_image_generation_size


def validate_image_generation_size(size: str) -> str:
    return normalize_image_generation_size(size)
