from __future__ import annotations

from fastapi import APIRouter, Depends, Query, status
from fastapi.responses import Response
from sqlalchemy.orm import Session

from productflow_backend.application.legacy_archive_rebuilds import create_legacy_archive_rebuild
from productflow_backend.application.legacy_archives import (
    LEGACY_ARCHIVE_DEFAULT_LIMIT,
    LEGACY_ARCHIVE_MAX_LIMIT,
    LegacyArchiveKind,
    get_legacy_archive_detail,
    legacy_archive_export_bytes,
    legacy_archive_export_sha256,
    list_legacy_archives,
)
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.schemas.legacy_archives import (
    LegacyArchiveAgentRebuildRequest,
    LegacyArchiveAgentRebuildResponse,
    LegacyArchiveDetailResponse,
    LegacyArchivePageResponse,
    serialize_legacy_archive_detail,
    serialize_legacy_archive_page,
    serialize_legacy_archive_rebuild,
)

router = APIRouter(
    prefix="/api/v2/legacy-archives",
    tags=["legacy-archives"],
    dependencies=[Depends(require_admin)],
)


@router.get("", response_model=LegacyArchivePageResponse)
def list_legacy_archives_endpoint(
    kind: LegacyArchiveKind | None = Query(default=None),
    product_id: str | None = Query(default=None, max_length=36),
    q: str = Query(default="", max_length=255),
    after: str = Query(default="", max_length=4096),
    limit: int = Query(default=LEGACY_ARCHIVE_DEFAULT_LIMIT, ge=1, le=LEGACY_ARCHIVE_MAX_LIMIT),
    session: Session = Depends(get_session),
) -> LegacyArchivePageResponse:
    return serialize_legacy_archive_page(
        list_legacy_archives(
            session,
            kind=kind,
            product_id=product_id,
            query=q,
            after=after,
            limit=limit,
        )
    )


@router.get("/{kind}/{archive_id}", response_model=LegacyArchiveDetailResponse)
def get_legacy_archive_endpoint(
    kind: LegacyArchiveKind,
    archive_id: str,
    session: Session = Depends(get_session),
) -> LegacyArchiveDetailResponse:
    return serialize_legacy_archive_detail(
        get_legacy_archive_detail(session, kind=kind, archive_id=archive_id)
    )


@router.get("/{kind}/{archive_id}/export")
def export_legacy_archive_endpoint(
    kind: LegacyArchiveKind,
    archive_id: str,
    session: Session = Depends(get_session),
) -> Response:
    detail = get_legacy_archive_detail(session, kind=kind, archive_id=archive_id)
    content = legacy_archive_export_bytes(detail)
    digest = legacy_archive_export_sha256(detail)
    filename = f"legacy-{kind}-{archive_id}.json"
    return Response(
        content=content,
        media_type="application/json",
        headers={
            "Content-Disposition": f'attachment; filename="{filename}"',
            "ETag": f'"{digest}"',
        },
    )


@router.post(
    "/{kind}/{archive_id}/agent-rebuilds",
    response_model=LegacyArchiveAgentRebuildResponse,
    status_code=status.HTTP_201_CREATED,
)
def create_legacy_archive_agent_rebuild_endpoint(
    kind: LegacyArchiveKind,
    archive_id: str,
    payload: LegacyArchiveAgentRebuildRequest,
    session: Session = Depends(get_session),
) -> LegacyArchiveAgentRebuildResponse:
    return serialize_legacy_archive_rebuild(
        create_legacy_archive_rebuild(
            session,
            kind=kind,
            archive_id=archive_id,
            target_product_id=payload.target_product_id,
            idempotency_key=payload.idempotency_key,
        )
    )


__all__ = ["router"]
