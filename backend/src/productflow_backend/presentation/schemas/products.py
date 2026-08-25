from __future__ import annotations

from datetime import datetime
from decimal import Decimal

from pydantic import BaseModel, ConfigDict, Field, JsonValue, model_validator

from productflow_backend.application.agent.product_intake import WorkflowIntakeV1, parse_product_intake
from productflow_backend.application.delivery_renditions.contracts import DeliveryRenditionStatus
from productflow_backend.application.product_images.queries import (
    GalleryAssetRecord,
    GalleryBootstrap,
)
from productflow_backend.application.workflow_drafts.contracts import DeliverySpec
from productflow_backend.domain.enums import (
    MediaVerificationStatus,
    ProductFactSourceType,
    ProductFactStatus,
    ProductImageOriginType,
)
from productflow_backend.infrastructure.db.models import (
    Product,
    ProductImageAsset,
)
from productflow_backend.presentation.image_variants import build_image_urls


class ProductSummaryResponse(BaseModel):
    id: str
    name: str
    category: str | None = None
    price: Decimal | None = None
    cover_image_asset_id: str | None = None
    cover_image_filename: str | None = None
    cover_image_download_url: str | None = None
    cover_image_preview_url: str | None = None
    cover_image_thumbnail_url: str | None = None
    created_at: datetime
    updated_at: datetime


class ProductListResponse(BaseModel):
    items: list[ProductSummaryResponse]
    total: int
    page: int
    page_size: int


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
    source_library_asset_id: str | None = None
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
    prompt_artifact_version_id: str | None = None
    visual_system_version_id: str | None = None


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
    intake: WorkflowIntakeV1 | None = None
    created_at: datetime
    updated_at: datetime


class CanonicalProductCreateResponse(BaseModel):
    product: CanonicalProductDetailResponse
    created_assets: list[ProductImageAssetResponse]


class ProductFactResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    key: str
    value: JsonValue
    source_type: ProductFactSourceType
    status: ProductFactStatus
    requires_confirmation: bool = False
    evidence_asset_ids: list[str] = Field(default_factory=list)
    conflicts: list[dict[str, JsonValue]] = Field(default_factory=list)


class ProductFactSetResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    product_id: str
    version: int
    facts: list[ProductFactResponse]
    created_at: datetime


class ProductFactsResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    product: CanonicalProductDetailResponse
    current_fact_set_version_id: str | None = None
    current_fact_version: int | None = None
    fact_set: ProductFactSetResponse | None = None
    facts: list[ProductFactResponse]


class ProductFactInput(BaseModel):
    model_config = ConfigDict(extra="forbid")

    key: str = Field(min_length=1, max_length=120)
    value: JsonValue
    source_type: ProductFactSourceType = ProductFactSourceType.USER
    status: ProductFactStatus = ProductFactStatus.CONFIRMED
    requires_confirmation: bool = False
    evidence_asset_ids: list[str] = Field(default_factory=list)
    conflicts: list[dict[str, JsonValue]] = Field(default_factory=list)


class UpdateProductFactsRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    expected_fact_set_version_id: str | None = Field(default=None, min_length=1, max_length=36)
    expected_fact_version: int | None = Field(default=None, ge=1)
    name: str | None = Field(default=None, max_length=255)
    category: str | None = Field(default=None, max_length=120)
    price: str | None = Field(default=None, max_length=40)
    source_note: str | None = Field(default=None, max_length=4000)
    facts: list[ProductFactInput] | None = None

    @model_validator(mode="after")
    def require_expected_version_shape(self) -> UpdateProductFactsRequest:
        if self.expected_fact_set_version_id is not None and self.expected_fact_version is not None:
            if not self.expected_fact_set_version_id.strip():
                raise ValueError("expected_fact_set_version_id 不能为空")
        return self


class SetProductCoverRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    asset_id: str = Field(min_length=1)


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
        source_library_asset_id=asset.source_library_asset_id,
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
        intake=parse_product_intake(product),
        created_at=product.created_at,
        updated_at=product.updated_at,
    )


def serialize_product_facts(product: Product) -> ProductFactsResponse:
    current = product.current_fact_set_version
    facts = _fact_payloads(current)
    fact_set = (
        ProductFactSetResponse(
            id=current.id,
            product_id=current.product_id,
            version=current.version,
            facts=[serialize_product_fact(item) for item in facts],
            created_at=current.created_at,
        )
        if current is not None
        else None
    )
    return ProductFactsResponse(
        product=serialize_canonical_product_detail(product),
        current_fact_set_version_id=current.id if current is not None else None,
        current_fact_version=current.version if current is not None else None,
        fact_set=fact_set,
        facts=[serialize_product_fact(item) for item in facts],
    )


def serialize_product_fact(payload: dict) -> ProductFactResponse:
    return ProductFactResponse(
        key=str(payload.get("key") or "unknown"),
        value=payload.get("value"),
        source_type=payload.get("source_type") or ProductFactSourceType.LEGACY_PRODUCT,
        status=payload.get("status") or ProductFactStatus.CONFIRMED,
        requires_confirmation=bool(payload.get("requires_confirmation", False)),
        evidence_asset_ids=list(payload.get("evidence_asset_ids") or []),
        conflicts=list(payload.get("conflicts") or []),
    )


def _fact_payloads(fact_set) -> list[dict]:
    if fact_set is None:
        return []
    facts = fact_set.payload_json.get("facts")
    return [dict(item) for item in facts if isinstance(item, dict)] if isinstance(facts, list) else []


def serialize_product_summary(product: Product) -> ProductSummaryResponse:
    cover = product.cover_image_asset
    cover_urls = build_image_urls(f"/api/v2/product-image-assets/{cover.id}/download") if cover else {}
    return ProductSummaryResponse(
        id=product.id,
        name=product.name,
        category=product.category,
        price=product.price,
        cover_image_asset_id=cover.id if cover else None,
        cover_image_filename=cover.original_filename if cover else None,
        cover_image_download_url=cover_urls.get("download_url"),
        cover_image_preview_url=cover_urls.get("preview_url"),
        cover_image_thumbnail_url=cover_urls.get("thumbnail_url"),
        created_at=product.created_at,
        updated_at=product.updated_at,
    )
