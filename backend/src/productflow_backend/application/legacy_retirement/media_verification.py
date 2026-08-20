from __future__ import annotations

from datetime import datetime
from pathlib import Path
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field
from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.application.media_objects import inspect_image_bytes
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import MediaVerificationStatus
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.infrastructure.db.models import MediaObject
from productflow_backend.infrastructure.storage import LocalStorage

LegacyMediaVerificationResult = Literal[
    "would_verify",
    "verified",
    "missing",
    "invalid",
    "path_error",
]
_RESULT_VALUES: tuple[LegacyMediaVerificationResult, ...] = (
    "would_verify",
    "verified",
    "missing",
    "invalid",
    "path_error",
)


class LegacyMediaVerificationItem(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    media_id: str
    result: LegacyMediaVerificationResult
    detail: str | None = None
    mime_type: str | None = None
    byte_size: int | None = Field(default=None, ge=1)
    width: int | None = Field(default=None, ge=1)
    height: int | None = Field(default=None, ge=1)
    sha256: str | None = Field(default=None, pattern=r"^[0-9a-f]{64}$")


class LegacyMediaVerificationReport(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)

    schema_version: Literal[1] = 1
    generated_at: datetime
    apply: bool
    scanned_count: int = Field(ge=0)
    would_verify_count: int = Field(ge=0)
    verified_count: int = Field(ge=0)
    missing_count: int = Field(ge=0)
    invalid_count: int = Field(ge=0)
    path_error_count: int = Field(ge=0)
    items: list[LegacyMediaVerificationItem]


def verify_legacy_media(
    session: Session,
    *,
    storage_root: Path,
    apply: bool = False,
    generated_at: datetime | None = None,
) -> LegacyMediaVerificationReport:
    """Measure recoverable legacy image files and optionally persist verified metadata."""

    storage = LocalStorage(storage_root)
    media_objects = list(
        session.scalars(
            select(MediaObject)
            .where(MediaObject.verification_status == MediaVerificationStatus.LEGACY_PENDING)
            .order_by(MediaObject.id)
        ).all()
    )
    timestamp = generated_at or now_utc()
    plans: list[tuple[MediaObject, LegacyMediaVerificationItem]] = []

    for media in media_objects:
        try:
            path = storage.resolve(media.storage_path)
        except ValueError as exc:
            plans.append(
                (
                    media,
                    LegacyMediaVerificationItem(
                        media_id=media.id,
                        result="path_error",
                        detail=str(exc),
                    ),
                )
            )
            continue
        if not path.is_file():
            plans.append(
                (
                    media,
                    LegacyMediaVerificationItem(
                        media_id=media.id,
                        result="missing",
                        detail="存储文件不存在",
                    ),
                )
            )
            continue
        try:
            content = path.read_bytes()
            metadata = inspect_image_bytes(content, expected_mime_type=media.mime_type)
        except (OSError, BusinessValidationError) as exc:
            plans.append(
                (
                    media,
                    LegacyMediaVerificationItem(
                        media_id=media.id,
                        result="invalid",
                        detail=str(exc),
                    ),
                )
            )
            continue

        plans.append(
            (
                media,
                LegacyMediaVerificationItem(
                    media_id=media.id,
                    result="verified" if apply else "would_verify",
                    mime_type=metadata.mime_type,
                    byte_size=metadata.byte_size,
                    width=metadata.width,
                    height=metadata.height,
                    sha256=metadata.sha256,
                ),
            )
        )

    if apply:
        for media, item in plans:
            if item.result != "verified":
                continue
            media.mime_type = item.mime_type or media.mime_type
            media.byte_size = item.byte_size
            media.width = item.width
            media.height = item.height
            media.sha256 = item.sha256
            media.verification_status = MediaVerificationStatus.VERIFIED
            media.verified_at = timestamp
        if any(item.result == "verified" for _, item in plans):
            session.flush()

    counts = {result: sum(item.result == result for _, item in plans) for result in _RESULT_VALUES}
    return LegacyMediaVerificationReport(
        generated_at=timestamp,
        apply=apply,
        scanned_count=len(plans),
        would_verify_count=counts["would_verify"],
        verified_count=counts["verified"],
        missing_count=counts["missing"],
        invalid_count=counts["invalid"],
        path_error_count=counts["path_error"],
        items=[item for _, item in plans],
    )


__all__ = ["LegacyMediaVerificationItem", "LegacyMediaVerificationReport", "verify_legacy_media"]
