from __future__ import annotations

from datetime import datetime
from typing import Any

from pydantic import BaseModel

from productflow_backend.application.legacy_archives import (
    LegacyArchiveAsset,
    LegacyArchiveDetail,
    LegacyArchiveKind,
    LegacyArchiveListItem,
    LegacyArchivePage,
)
from productflow_backend.domain.enums import MediaVerificationStatus
from productflow_backend.presentation.image_variants import build_image_urls


class LegacyArchiveListItemResponse(BaseModel):
    kind: LegacyArchiveKind
    id: str
    source_id: str
    source_key: str | None = None
    product_id: str | None = None
    product_name: str | None = None
    title: str
    description: str | None = None
    source_status: str | None = None
    archive_schema_version: int
    payload_sha256: str
    source_updated_at: datetime | None = None
    created_at: datetime
    counts: dict[str, int]


class LegacyArchivePageResponse(BaseModel):
    items: list[LegacyArchiveListItemResponse]
    next_cursor: str | None = None
    total: int
    kind_counts: dict[str, int]


class LegacyArchiveAssetResponse(BaseModel):
    product_image_asset_id: str
    role: str
    legacy_source_type: str
    legacy_source_id: str
    display_name: str
    original_filename: str
    mime_type: str
    byte_size: int | None = None
    width: int | None = None
    height: int | None = None
    verification_status: MediaVerificationStatus
    download_url: str
    preview_url: str
    thumbnail_url: str
    created_at: datetime


class LegacyArchiveDetailResponse(BaseModel):
    item: LegacyArchiveListItemResponse
    source_profile: str
    source_fingerprint_sha256: str
    payload: dict[str, Any]
    diagnostics: list[dict[str, Any]]
    assets: list[LegacyArchiveAssetResponse]


def serialize_legacy_archive_item(item: LegacyArchiveListItem) -> LegacyArchiveListItemResponse:
    return LegacyArchiveListItemResponse(
        kind=item.kind,
        id=item.id,
        source_id=item.source_id,
        source_key=item.source_key,
        product_id=item.product_id,
        product_name=item.product_name,
        title=item.title,
        description=item.description,
        source_status=item.source_status,
        archive_schema_version=item.archive_schema_version,
        payload_sha256=item.payload_sha256,
        source_updated_at=item.source_updated_at,
        created_at=item.created_at,
        counts=item.counts,
    )


def serialize_legacy_archive_page(page: LegacyArchivePage) -> LegacyArchivePageResponse:
    return LegacyArchivePageResponse(
        items=[serialize_legacy_archive_item(item) for item in page.items],
        next_cursor=page.next_cursor,
        total=page.total,
        kind_counts=page.kind_counts,
    )


def serialize_legacy_archive_asset(asset: LegacyArchiveAsset) -> LegacyArchiveAssetResponse:
    urls = build_image_urls(f"/api/v2/product-image-assets/{asset.product_image_asset_id}/download")
    return LegacyArchiveAssetResponse(
        product_image_asset_id=asset.product_image_asset_id,
        role=asset.role,
        legacy_source_type=asset.legacy_source_type,
        legacy_source_id=asset.legacy_source_id,
        display_name=asset.display_name,
        original_filename=asset.original_filename,
        mime_type=asset.mime_type,
        byte_size=asset.byte_size,
        width=asset.width,
        height=asset.height,
        verification_status=asset.verification_status,
        **urls,
        created_at=asset.created_at,
    )


def serialize_legacy_archive_detail(detail: LegacyArchiveDetail) -> LegacyArchiveDetailResponse:
    return LegacyArchiveDetailResponse(
        item=serialize_legacy_archive_item(detail.item),
        source_profile=detail.source_profile,
        source_fingerprint_sha256=detail.source_fingerprint_sha256,
        payload=detail.payload_json,
        diagnostics=detail.diagnostics,
        assets=[serialize_legacy_archive_asset(asset) for asset in detail.assets],
    )


__all__ = [
    "LegacyArchiveDetailResponse",
    "LegacyArchivePageResponse",
    "serialize_legacy_archive_detail",
    "serialize_legacy_archive_page",
]
