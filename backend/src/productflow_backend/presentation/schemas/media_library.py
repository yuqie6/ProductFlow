from __future__ import annotations

from datetime import datetime
from typing import Annotated

from pydantic import BaseModel, ConfigDict, Field

from productflow_backend.domain.enums import MediaVerificationStatus
from productflow_backend.domain.errors import NotFoundError
from productflow_backend.infrastructure.db.models import MediaLibraryAsset
from productflow_backend.presentation.image_variants import build_image_urls

MediaLibraryAssetId = Annotated[str, Field(min_length=1, max_length=36)]
MediaLibraryTagName = Annotated[str, Field(min_length=1, max_length=80)]
MediaLibraryRevision = Annotated[int, Field(ge=1, le=2_147_483_647)]


class MediaLibraryFolderResponse(BaseModel):
    id: str
    name: str
    count: int = 0


class MediaLibraryTagResponse(BaseModel):
    id: str
    name: str
    count: int = 0


class MediaLibraryAssetResponse(BaseModel):
    id: str
    media_object_id: str
    source_type: str
    source_id: str
    display_name: str
    folder_id: str | None
    folder_name: str | None
    tags: list[MediaLibraryTagResponse]
    original_filename: str
    revision: int
    is_archived: bool
    archived_at: datetime | None
    provenance_hash: str
    mime_type: str
    byte_size: int | None
    width: int | None
    height: int | None
    verification_status: MediaVerificationStatus
    download_url: str
    preview_url: str
    thumbnail_url: str
    created_at: datetime
    updated_at: datetime


class MediaLibraryAssetListResponse(BaseModel):
    items: list[MediaLibraryAssetResponse]
    next_cursor: str | None


class MediaLibraryBootstrapResponse(BaseModel):
    total_count: int
    active_count: int
    archived_count: int
    unorganized_count: int
    folders: list[MediaLibraryFolderResponse]
    tags: list[MediaLibraryTagResponse]


class MediaLibraryNameRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str = Field(min_length=1, max_length=120)


class MediaLibraryTagNameRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str = Field(min_length=1, max_length=80)


class MediaLibraryRenameFolderRequest(MediaLibraryNameRequest):
    expected_name: str = Field(min_length=1, max_length=120)


class MediaLibraryRenameTagRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    expected_name: str = Field(min_length=1, max_length=80)
    name: str = Field(min_length=1, max_length=80)


class MediaLibraryMoveAssetsRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    asset_ids: list[MediaLibraryAssetId] = Field(min_length=1, max_length=100)
    folder_id: MediaLibraryAssetId | None = None
    expected_revisions: dict[MediaLibraryAssetId, MediaLibraryRevision] = Field(min_length=1, max_length=100)


class MediaLibrarySetTagsRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    asset_ids: list[MediaLibraryAssetId] = Field(min_length=1, max_length=100)
    tag_names: list[MediaLibraryTagName] = Field(max_length=40)
    expected_revisions: dict[MediaLibraryAssetId, MediaLibraryRevision] = Field(min_length=1, max_length=100)


class MediaLibrarySaveFromSessionRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    image_session_asset_id: str = Field(min_length=1, max_length=36)


class MediaLibrarySaveFromProductRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    product_image_asset_id: str = Field(min_length=1, max_length=36)


class MediaLibraryCollectBatchToProductRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    product_id: str = Field(min_length=1, max_length=36)
    media_library_asset_ids: list[MediaLibraryAssetId] = Field(min_length=1, max_length=100)


class WorkflowMediaLibraryAssetResponse(BaseModel):
    asset: MediaLibraryAssetResponse
    product_image_asset_id: str | None
    linked_at: datetime


class WorkflowMediaLibraryAssetListResponse(BaseModel):
    workflow_id: str
    items: list[WorkflowMediaLibraryAssetResponse]


class WorkflowMediaLibrarySyncRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    media_library_asset_ids: list[MediaLibraryAssetId] = Field(min_length=1, max_length=100)


def serialize_media_library_asset(asset: MediaLibraryAsset) -> MediaLibraryAssetResponse:
    media = asset.media_object
    if media is None:
        raise NotFoundError("素材库媒体对象不存在")
    urls = build_image_urls(f"/api/media-library/{asset.id}/download")
    return MediaLibraryAssetResponse(
        id=asset.id,
        media_object_id=asset.media_object_id,
        source_type=asset.source_type,
        source_id=asset.source_id,
        display_name=asset.display_name,
        folder_id=asset.folder_id,
        folder_name=asset.folder.name if asset.folder is not None else None,
        tags=[
            MediaLibraryTagResponse(id=item.tag.id, name=item.tag.name)
            for item in asset.tag_assignments
            if item.tag is not None
        ],
        original_filename=asset.original_filename,
        revision=asset.revision,
        is_archived=asset.is_archived,
        archived_at=asset.archived_at,
        provenance_hash=asset.provenance_hash,
        mime_type=media.mime_type,
        byte_size=media.byte_size,
        width=media.width,
        height=media.height,
        verification_status=media.verification_status,
        **urls,
        created_at=asset.created_at,
        updated_at=asset.updated_at,
    )
