from __future__ import annotations

from productflow_backend.config import normalize_image_generation_size
from productflow_backend.infrastructure.runtime_settings import get_runtime_settings


def validate_image_generation_size(size: str) -> str:
    return normalize_image_generation_size(
        size,
        max_dimension=int(get_runtime_settings().image_generation_max_dimension),
    )
