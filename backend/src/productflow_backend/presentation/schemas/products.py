from __future__ import annotations

from datetime import datetime
from decimal import Decimal
from typing import Any

from pydantic import BaseModel, ConfigDict, Field

from productflow_backend.application.copy_payloads import copy_set_structured_payload
from productflow_backend.application.delivery_renditions.contracts import DeliveryRenditionStatus
from productflow_backend.application.gallery_assets import (
    GalleryAssetRecord,
    GalleryBootstrap,
)
from productflow_backend.application.use_cases import derive_product_state
from productflow_backend.application.workflow_drafts.contracts import DeliverySpec
from productflow_backend.domain.enums import (
    CopyStatus,
    MediaVerificationStatus,
    PosterKind,
    ProductImageOriginType,
    ProductWorkflowState,
    SourceAssetKind,
)
from productflow_backend.infrastructure.db.models import (
    CopySet,
    CreativeBrief,
    PosterVariant,
    Product,
    ProductImageAsset,
    SourceAsset,
)
from productflow_backend.presentation.image_variants import build_image_urls


class SourceAssetResponse(BaseModel):
    id: str
    kind: SourceAssetKind
    original_filename: str
    mime_type: str
    source_poster_variant_id: str | None = None
    download_url: str
    preview_url: str
    thumbnail_url: str
    created_at: datetime


class CreativeBriefSummaryResponse(BaseModel):
    id: str
    payload: dict[str, Any]
    provider_name: str
    model_name: str
    prompt_version: str
    created_at: datetime


class CopySetResponse(BaseModel):
    id: str
    creative_brief_id: str | None
    status: CopyStatus
    structured_payload: dict[str, Any]
    model_structured_payload: dict[str, Any] | None = None
    provider_name: str
    model_name: str
    prompt_version: str
    created_at: datetime
    updated_at: datetime
    edited_at: datetime | None = None
    confirmed_at: datetime | None = None


class PosterVariantResponse(BaseModel):
    id: str
    product_id: str
    copy_set_id: str
    kind: PosterKind
    template_name: str
    mime_type: str
    width: int
    height: int
    download_url: str
    preview_url: str
    thumbnail_url: str
    created_at: datetime


class ProductSummaryResponse(BaseModel):
    id: str
    name: str
    category: str | None = None
    price: Decimal | None = None
    workflow_state: ProductWorkflowState
    latest_copy_status: CopyStatus | None = None
    latest_poster_at: datetime | None = None
    source_image_filename: str | None = None
    source_image_download_url: str | None = None
    source_image_preview_url: str | None = None
    source_image_thumbnail_url: str | None = None
    created_at: datetime
    updated_at: datetime


class ProductListResponse(BaseModel):
    items: list[ProductSummaryResponse]
    total: int
    page: int
    page_size: int


class ProductDetailResponse(BaseModel):
    id: str
    name: str
    category: str | None = None
    price: Decimal | None = None
    source_note: str | None = None
    workflow_state: ProductWorkflowState
    source_assets: list[SourceAssetResponse]
    latest_brief: CreativeBriefSummaryResponse | None = None
    current_confirmed_copy_set: CopySetResponse | None = None
    copy_sets: list[CopySetResponse]
    poster_variants: list[PosterVariantResponse]
    created_at: datetime
    updated_at: datetime


class ProductImageAssetResponse(BaseModel):
    id: str
    product_id: str
    media_object_id: str
    origin_type: ProductImageOriginType
    display_name: str
    original_filename: str
    image_type_key: str | None = None
    user_folder_id: str | None = None
    parent_asset_id: str | None = None
    source_image_session_asset_id: str | None = None
    mime_type: str
    byte_size: int | None = None
    width: int | None = None
    height: int | None = None
    verification_status: MediaVerificationStatus
    download_url: str
    preview_url: str
    thumbnail_url: str
    created_at: datetime
    updated_at: datetime


class ProductImageAssetListResponse(BaseModel):
    items: list[ProductImageAssetResponse]


class GalleryGenerationSummaryResponse(BaseModel):
    workflow_id: str
    node_id: str
    node_run_id: str
    prompt_artifact_version_id: str
    visual_system_version_id: str


class GalleryRenditionSummaryResponse(BaseModel):
    job_id: str
    source_asset_id: str
    delivery_spec: DeliverySpec
    status: DeliveryRenditionStatus


class GalleryAssetResponse(ProductImageAssetResponse):
    user_folder_name: str | None = None
    image_type_title: str | None = None
    generation: GalleryGenerationSummaryResponse | None = None
    rendition: GalleryRenditionSummaryResponse | None = None


class GalleryAssetPageResponse(BaseModel):
    items: list[GalleryAssetResponse]
    next_cursor: str | None = None


class GallerySystemDirectoryResponse(BaseModel):
    kind: str
    count: int


class GalleryImageTypeResponse(BaseModel):
    directory_key: str
    image_type_key: str | None = None
    title: str
    count: int


class GalleryOriginResponse(BaseModel):
    origin_type: ProductImageOriginType
    count: int


class GalleryFolderResponse(BaseModel):
    id: str
    name: str
    sort_order: int
    count: int


class CreateGalleryFolderRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str = Field(min_length=1, max_length=120)


class RenameGalleryFolderRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    expected_name: str = Field(min_length=1, max_length=120)
    name: str = Field(min_length=1, max_length=120)


class RenameGalleryAssetRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    expected_display_name: str = Field(min_length=1, max_length=255)
    display_name: str = Field(min_length=1, max_length=255)


class GalleryAssetMoveRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    asset_id: str = Field(min_length=1, max_length=36)
    expected_folder_id: str | None = Field(default=None, max_length=36)


class MoveGalleryAssetsRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    items: list[GalleryAssetMoveRequest] = Field(min_length=1, max_length=100)
    folder_id: str | None = Field(default=None, max_length=36)


class DownloadGalleryArchiveRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    asset_ids: list[str] = Field(min_length=1, max_length=100)


class GalleryFolderMutationResponse(BaseModel):
    id: str
    name: str
    sort_order: int


class DeleteGalleryFolderResponse(BaseModel):
    folder_id: str
    moved_to_unorganized_count: int


class GalleryBootstrapResponse(BaseModel):
    product_id: str
    cover_image_asset_id: str | None = None
    system_directories: list[GallerySystemDirectoryResponse]
    image_types: list[GalleryImageTypeResponse]
    origins: list[GalleryOriginResponse]
    user_folders: list[GalleryFolderResponse]
    unorganized_count: int


class CanonicalProductDetailResponse(BaseModel):
    id: str
    name: str
    category: str | None = None
    price: Decimal | None = None
    source_note: str | None = None
    cover_image_asset_id: str | None = None
    created_at: datetime
    updated_at: datetime


class CanonicalProductCreateResponse(BaseModel):
    product: CanonicalProductDetailResponse
    created_assets: list[ProductImageAssetResponse]


class SetProductCoverRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    asset_id: str = Field(min_length=1)


class ProductHistoryResponse(BaseModel):
    copy_sets: list[CopySetResponse]
    poster_variants: list[PosterVariantResponse]


class CopySetUpdateRequest(BaseModel):
    structured_payload: dict[str, Any]


def serialize_source_asset(asset: SourceAsset) -> SourceAssetResponse:
    urls = build_image_urls(f"/api/source-assets/{asset.id}/download")
    return SourceAssetResponse(
        id=asset.id,
        kind=asset.kind,
        original_filename=asset.original_filename,
        mime_type=asset.mime_type,
        source_poster_variant_id=asset.source_poster_variant_id,
        **urls,
        created_at=asset.created_at,
    )


def serialize_brief(brief: CreativeBrief) -> CreativeBriefSummaryResponse:
    return CreativeBriefSummaryResponse(
        id=brief.id,
        payload=brief.payload,
        provider_name=brief.provider_name,
        model_name=brief.model_name,
        prompt_version=brief.prompt_version,
        created_at=brief.created_at,
    )


def serialize_copy_set(copy_set: CopySet) -> CopySetResponse:
    return CopySetResponse(
        id=copy_set.id,
        creative_brief_id=copy_set.creative_brief_id,
        status=copy_set.status,
        structured_payload=copy_set_structured_payload(copy_set).model_dump(mode="json"),
        model_structured_payload=copy_set.model_structured_payload,
        provider_name=copy_set.provider_name,
        model_name=copy_set.model_name,
        prompt_version=copy_set.prompt_version,
        created_at=copy_set.created_at,
        updated_at=copy_set.updated_at,
        edited_at=copy_set.edited_at,
        confirmed_at=copy_set.confirmed_at,
    )


def serialize_poster_variant(poster: PosterVariant) -> PosterVariantResponse:
    urls = build_image_urls(f"/api/posters/{poster.id}/download")
    return PosterVariantResponse(
        id=poster.id,
        product_id=poster.product_id,
        copy_set_id=poster.copy_set_id,
        kind=poster.kind,
        template_name=poster.template_name,
        mime_type=poster.mime_type,
        width=poster.width,
        height=poster.height,
        **urls,
        created_at=poster.created_at,
    )


def serialize_product_image_asset(asset: ProductImageAsset) -> ProductImageAssetResponse:
    urls = build_image_urls(f"/api/v2/product-image-assets/{asset.id}/download")
    media = asset.media_object
    return ProductImageAssetResponse(
        id=asset.id,
        product_id=asset.product_id,
        media_object_id=asset.media_object_id,
        origin_type=asset.origin_type,
        display_name=asset.display_name,
        original_filename=asset.original_filename,
        image_type_key=asset.image_type_key,
        user_folder_id=asset.user_folder_id,
        parent_asset_id=asset.parent_asset_id,
        source_image_session_asset_id=asset.source_image_session_asset_id,
        mime_type=media.mime_type,
        byte_size=media.byte_size,
        width=media.width,
        height=media.height,
        verification_status=media.verification_status,
        **urls,
        created_at=asset.created_at,
        updated_at=asset.updated_at,
    )


def serialize_gallery_asset(record: GalleryAssetRecord) -> GalleryAssetResponse:
    asset_payload = serialize_product_image_asset(record.asset).model_dump()
    generation = (
        GalleryGenerationSummaryResponse(
            workflow_id=record.generation.workflow_id,
            node_id=record.generation.node_id,
            node_run_id=record.generation.node_run_id,
            prompt_artifact_version_id=record.generation.prompt_artifact_version_id,
            visual_system_version_id=record.generation.visual_system_version_id,
        )
        if record.generation is not None
        else None
    )
    rendition = (
        GalleryRenditionSummaryResponse(
            job_id=record.rendition.job_id,
            source_asset_id=record.rendition.source_asset_id,
            delivery_spec=record.rendition.delivery_spec,
            status=record.rendition.status,
        )
        if record.rendition is not None
        else None
    )
    return GalleryAssetResponse(
        **asset_payload,
        user_folder_name=record.asset.user_folder.name if record.asset.user_folder is not None else None,
        image_type_title=record.image_type_title,
        generation=generation,
        rendition=rendition,
    )


def serialize_gallery_bootstrap(bootstrap: GalleryBootstrap) -> GalleryBootstrapResponse:
    return GalleryBootstrapResponse(
        product_id=bootstrap.product_id,
        cover_image_asset_id=bootstrap.cover_image_asset_id,
        system_directories=[
            GallerySystemDirectoryResponse(kind=item.kind.value, count=item.count)
            for item in bootstrap.system_directories
        ],
        image_types=[
            GalleryImageTypeResponse(
                directory_key=item.directory_key,
                image_type_key=item.image_type_key,
                title=item.title,
                count=item.count,
            )
            for item in bootstrap.image_types
        ],
        origins=[
            GalleryOriginResponse(origin_type=item.origin_type, count=item.count)
            for item in bootstrap.origins
        ],
        user_folders=[
            GalleryFolderResponse(
                id=item.id,
                name=item.name,
                sort_order=item.sort_order,
                count=item.count,
            )
            for item in bootstrap.user_folders
        ],
        unorganized_count=bootstrap.unorganized_count,
    )


def serialize_canonical_product_detail(product: Product) -> CanonicalProductDetailResponse:
    return CanonicalProductDetailResponse(
        id=product.id,
        name=product.name,
        category=product.category,
        price=product.price,
        source_note=product.source_note,
        cover_image_asset_id=product.cover_image_asset_id,
        created_at=product.created_at,
        updated_at=product.updated_at,
    )


def serialize_product_summary(product: Product) -> ProductSummaryResponse:
    latest_copy = max(product.copy_sets, key=lambda item: item.created_at, default=None)
    latest_poster = max(product.poster_variants, key=lambda item: item.created_at, default=None)
    source = next((item for item in product.source_assets if item.kind == SourceAssetKind.ORIGINAL_IMAGE), None)
    source_urls = build_image_urls(f"/api/source-assets/{source.id}/download") if source else {}
    return ProductSummaryResponse(
        id=product.id,
        name=product.name,
        category=product.category,
        price=product.price,
        workflow_state=derive_product_state(product),
        latest_copy_status=latest_copy.status if latest_copy else None,
        latest_poster_at=latest_poster.created_at if latest_poster else None,
        source_image_filename=source.original_filename if source else None,
        source_image_download_url=source_urls.get("download_url"),
        source_image_preview_url=source_urls.get("preview_url"),
        source_image_thumbnail_url=source_urls.get("thumbnail_url"),
        created_at=product.created_at,
        updated_at=product.updated_at,
    )


def serialize_product_detail(product: Product) -> ProductDetailResponse:
    latest_brief = max(product.creative_briefs, key=lambda item: item.created_at, default=None)
    copy_sets = sorted(product.copy_sets, key=lambda item: item.created_at, reverse=True)
    poster_variants = sorted(product.poster_variants, key=lambda item: item.created_at, reverse=True)
    return ProductDetailResponse(
        id=product.id,
        name=product.name,
        category=product.category,
        price=product.price,
        source_note=product.source_note,
        workflow_state=derive_product_state(product),
        source_assets=[serialize_source_asset(item) for item in product.source_assets],
        latest_brief=serialize_brief(latest_brief) if latest_brief else None,
        current_confirmed_copy_set=(
            serialize_copy_set(product.confirmed_copy_set) if product.confirmed_copy_set else None
        ),
        copy_sets=[serialize_copy_set(item) for item in copy_sets],
        poster_variants=[serialize_poster_variant(item) for item in poster_variants],
        created_at=product.created_at,
        updated_at=product.updated_at,
    )
