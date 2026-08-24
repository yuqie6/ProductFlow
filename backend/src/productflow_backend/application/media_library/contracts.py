"""全局素材 provenance 与收录/上传请求身份。

provenance 冻结 MediaObject 核验指纹；request hash 标识命令，不嵌入媒体 bytes。
"""

from __future__ import annotations

import json
from collections.abc import Sequence
from datetime import datetime
from hashlib import sha256
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

MEDIA_LIBRARY_PROVENANCE_SCHEMA_VERSION = 1
MAX_PROVENANCE_BYTES = 32 * 1024
MEDIA_LIBRARY_COLLECTION_MAX_IDEMPOTENCY_KEY_BYTES = 200
MEDIA_LIBRARY_UPLOAD_MAX_IDEMPOTENCY_KEY_BYTES = 200
MediaLibrarySourceType = Literal["legacy_gallery", "image_session_generated", "product_asset", "direct_upload"]


class MediaLibraryProvenanceV1(BaseModel):
    """一条全局素材的不可变来源与内容指纹。"""

    model_config = ConfigDict(extra="forbid", frozen=True)

    schema_version: Literal[1] = MEDIA_LIBRARY_PROVENANCE_SCHEMA_VERSION
    source_type: MediaLibrarySourceType
    source_id: str = Field(min_length=1, max_length=36)
    sha256: str = Field(pattern=r"^[0-9a-f]{64}$")
    mime_type: str = Field(min_length=1, max_length=100)
    byte_size: int = Field(ge=1)
    width: int = Field(ge=1)
    height: int = Field(ge=1)
    original_filename: str = Field(min_length=1, max_length=255)
    origin_type: str | None = Field(default=None, max_length=40)
    captured_at: datetime


def canonical_provenance_hash(payload: dict[str, Any]) -> str:
    """对冻结 provenance JSON 取指纹；超限拒绝，避免把 bytes 塞进 provenance。"""

    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    if len(encoded.encode("utf-8")) > MAX_PROVENANCE_BYTES:
        raise ValueError("media library provenance payload exceeds maximum size")
    return sha256(encoded.encode("utf-8")).hexdigest()


def normalize_media_library_collection_idempotency_key(value: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise ValueError("素材收录 Idempotency-Key 不能为空")
    if len(normalized.encode("utf-8")) > MEDIA_LIBRARY_COLLECTION_MAX_IDEMPOTENCY_KEY_BYTES:
        raise ValueError(
            f"素材收录 Idempotency-Key 不能超过 {MEDIA_LIBRARY_COLLECTION_MAX_IDEMPOTENCY_KEY_BYTES} bytes"
        )
    return normalized


def media_library_collection_request_hash(*, product_id: str, library_asset_ids: list[str]) -> str:
    return canonical_provenance_hash(
        {
            "request_kind": "media_library_collect_to_product_v1",
            "product_id": product_id,
            "media_library_asset_ids": library_asset_ids,
        }
    )


def normalize_media_library_upload_idempotency_key(value: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise ValueError("素材库上传 Idempotency-Key 不能为空")
    if len(normalized.encode("utf-8")) > MEDIA_LIBRARY_UPLOAD_MAX_IDEMPOTENCY_KEY_BYTES:
        raise ValueError(
            f"素材库上传 Idempotency-Key 不能超过 {MEDIA_LIBRARY_UPLOAD_MAX_IDEMPOTENCY_KEY_BYTES} bytes"
        )
    return normalized


def media_library_upload_request_hash(
    *,
    folder_id: str | None,
    files: Sequence[tuple[str, bytes, str | None]],
) -> str:
    """files: (filename, content, mime_type). Content bytes are hashed, not embedded."""

    return canonical_provenance_hash(
        {
            "request_kind": "media_library_upload_v1",
            "folder_id": folder_id,
            "files": sorted(
                (filename, sha256(content).hexdigest(), mime_type) for filename, content, mime_type in files
            ),
        }
    )


def parse_provenance_v1(payload: dict[str, Any]) -> MediaLibraryProvenanceV1:
    if payload.get("schema_version") != MEDIA_LIBRARY_PROVENANCE_SCHEMA_VERSION:
        raise ValueError("unsupported media library provenance schema version")
    return MediaLibraryProvenanceV1.model_validate(payload)
