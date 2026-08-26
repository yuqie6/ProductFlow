"""Shared idempotency-key normalization and canonical request hashing."""

from __future__ import annotations

import hashlib
import json
from typing import Any

from productflow_backend.domain.errors import BusinessValidationError

DEFAULT_IDEMPOTENCY_KEY_MAX_BYTES = 200


def normalize_idempotency_key(
    value: str,
    *,
    max_bytes: int = DEFAULT_IDEMPOTENCY_KEY_MAX_BYTES,
    field_name: str = "idempotency key",
) -> str:
    """Normalize one persisted idempotency key at the application boundary."""
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError(f"{field_name} 不能为空")
    if len(normalized.encode("utf-8")) > max_bytes:
        raise BusinessValidationError(f"{field_name} 不能超过 {max_bytes} bytes")
    return normalized


def canonical_json_request_hash(value: Any) -> str:
    """Hash JSON using one stable encoding for all Agent request contracts."""
    try:
        encoded = json.dumps(
            value,
            ensure_ascii=False,
            sort_keys=True,
            separators=(",", ":"),
            allow_nan=False,
        ).encode("utf-8")
    except (TypeError, ValueError) as exc:
        raise BusinessValidationError("请求内容不是有效 JSON") from exc
    return hashlib.sha256(encoded).hexdigest()


__all__ = [
    "DEFAULT_IDEMPOTENCY_KEY_MAX_BYTES",
    "canonical_json_request_hash",
    "normalize_idempotency_key",
]
