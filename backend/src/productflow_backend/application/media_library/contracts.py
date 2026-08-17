from __future__ import annotations

import json
from datetime import datetime
from hashlib import sha256
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

MEDIA_LIBRARY_PROVENANCE_SCHEMA_VERSION = 1
MAX_PROVENANCE_BYTES = 32 * 1024
MediaLibrarySourceType = Literal["legacy_gallery", "image_session_generated", "product_asset"]


class MediaLibraryProvenanceV1(BaseModel):
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
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    if len(encoded.encode("utf-8")) > MAX_PROVENANCE_BYTES:
        raise ValueError("media library provenance payload exceeds maximum size")
    return sha256(encoded.encode("utf-8")).hexdigest()


def parse_provenance_v1(payload: dict[str, Any]) -> MediaLibraryProvenanceV1:
    if payload.get("schema_version") != MEDIA_LIBRARY_PROVENANCE_SCHEMA_VERSION:
        raise ValueError("unsupported media library provenance schema version")
    return MediaLibraryProvenanceV1.model_validate(payload)
