from __future__ import annotations

from datetime import UTC, datetime
from io import BytesIO
from pathlib import Path

from PIL import Image

from productflow_backend.application.legacy_retirement.media_verification import verify_legacy_media
from productflow_backend.domain.enums import MediaVerificationStatus
from productflow_backend.infrastructure.db.models import MediaObject, new_id


def _jpeg_bytes() -> bytes:
    output = BytesIO()
    Image.new("RGB", (640, 480), (20, 40, 60)).save(output, format="JPEG")
    return output.getvalue()


def _add_pending_media(db_session, *, storage_path: str, mime_type: str = "image/jpeg") -> MediaObject:
    media = MediaObject(
        id=new_id(),
        storage_path=storage_path,
        mime_type=mime_type,
        verification_status=MediaVerificationStatus.LEGACY_PENDING,
    )
    db_session.add(media)
    db_session.commit()
    return media


def test_verify_legacy_media_dry_run_does_not_mutate_and_apply_persists_metadata(
    configured_env: Path,
    db_session,
) -> None:
    relative_path = "products/legacy/source.jpg"
    path = configured_env / relative_path
    path.parent.mkdir(parents=True)
    path.write_bytes(_jpeg_bytes())
    media = _add_pending_media(db_session, storage_path=relative_path)
    generated_at = datetime(2026, 8, 16, 12, 0, tzinfo=UTC)

    dry_run = verify_legacy_media(
        db_session,
        storage_root=configured_env,
        generated_at=generated_at,
    )

    assert dry_run.scanned_count == 1
    assert dry_run.would_verify_count == 1
    assert dry_run.verified_count == 0
    assert dry_run.items[0].result == "would_verify"
    assert media.verification_status == MediaVerificationStatus.LEGACY_PENDING
    assert media.byte_size is None

    applied = verify_legacy_media(
        db_session,
        storage_root=configured_env,
        apply=True,
        generated_at=generated_at,
    )

    assert applied.verified_count == 1
    assert applied.would_verify_count == 0
    assert applied.items[0].result == "verified"
    assert media.verification_status == MediaVerificationStatus.VERIFIED
    assert media.mime_type == "image/jpeg"
    assert media.byte_size == path.stat().st_size
    assert (media.width, media.height) == (640, 480)
    assert media.sha256 is not None and len(media.sha256) == 64
    assert media.verified_at == generated_at

    db_session.rollback()
    db_session.expire_all()
    persisted = db_session.get(MediaObject, media.id)
    assert persisted is not None
    assert persisted.verification_status == MediaVerificationStatus.LEGACY_PENDING
    assert persisted.byte_size is None


def test_verify_legacy_media_keeps_missing_and_invalid_files_pending(
    configured_env: Path,
    db_session,
) -> None:
    missing = _add_pending_media(db_session, storage_path="products/legacy/missing.jpg")
    invalid_path = "products/legacy/invalid.jpg"
    path = configured_env / invalid_path
    path.parent.mkdir(parents=True)
    path.write_bytes(b"not-an-image")
    invalid = _add_pending_media(db_session, storage_path=invalid_path)

    report = verify_legacy_media(db_session, storage_root=configured_env, apply=True)

    assert report.missing_count == 1
    assert report.invalid_count == 1
    assert report.verified_count == 0
    assert {item.result for item in report.items} == {"missing", "invalid"}
    assert missing.verification_status == MediaVerificationStatus.LEGACY_PENDING
    assert invalid.verification_status == MediaVerificationStatus.LEGACY_PENDING
