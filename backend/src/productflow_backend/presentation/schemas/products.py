from __future__ import annotations

from datetime import datetime
from decimal import Decimal
from typing import Any

from pydantic import BaseModel, Field

from productflow_backend.application.use_cases import derive_product_state
from productflow_backend.domain.enums import (
    CopyStatus,
    PosterKind,
    ProductWorkflowState,
    SourceAssetKind,
)
from productflow_backend.infrastructure.db.models import (
    CopySet,
    CreativeBrief,
    PosterVariant,
    Product,
    SourceAsset,
)
from productflow_backend.presentation.image_variants import build_image_urls
from productflow_backend.presentation.schemas.jobs import JobRunResponse, serialize_job


class SourceAssetResponse(BaseModel):
    id: str
    kind: SourceAssetKind
    original_filename: str
    mime_type: str
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
    title: str
    selling_points: list[str]
    poster_headline: str
    cta: str
    model_title: str
    model_selling_points: list[str]
    model_poster_headline: str
    model_cta: str
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
    recent_jobs: list[JobRunResponse]
    created_at: datetime
    updated_at: datetime


class ProductHistoryResponse(BaseModel):
    copy_sets: list[CopySetResponse]
    poster_variants: list[PosterVariantResponse]
    jobs: list[JobRunResponse]


class CopySetUpdateRequest(BaseModel):
    title: str | None = Field(default=None, min_length=1, max_length=500)
    selling_points: list[str] | None = Field(default=None, min_length=3, max_length=5)
    poster_headline: str | None = Field(default=None, min_length=1, max_length=500)
    cta: str | None = Field(default=None, min_length=1, max_length=300)


def serialize_source_asset(asset: SourceAsset) -> SourceAssetResponse:
    urls = build_image_urls(f"/api/source-assets/{asset.id}/download")
    return SourceAssetResponse(
        id=asset.id,
        kind=asset.kind,
        original_filename=asset.original_filename,
        mime_type=asset.mime_type,
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
        title=copy_set.title,
        selling_points=copy_set.selling_points,
        poster_headline=copy_set.poster_headline,
        cta=copy_set.cta,
        model_title=copy_set.model_title,
        model_selling_points=copy_set.model_selling_points,
        model_poster_headline=copy_set.model_poster_headline,
        model_cta=copy_set.model_cta,
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


def serialize_product_summary(product: Product) -> ProductSummaryResponse:
    latest_copy = max(product.copy_sets, key=lambda item: item.created_at, default=None)
    latest_poster = max(product.poster_variants, key=lambda item: item.created_at, default=None)
    source = next((item for item in product.source_assets if item.kind == SourceAssetKind.ORIGINAL_IMAGE), None)
    return ProductSummaryResponse(
        id=product.id,
        name=product.name,
        category=product.category,
        price=product.price,
        workflow_state=derive_product_state(product),
        latest_copy_status=latest_copy.status if latest_copy else None,
        latest_poster_at=latest_poster.created_at if latest_poster else None,
        source_image_filename=source.original_filename if source else None,
        created_at=product.created_at,
        updated_at=product.updated_at,
    )


def serialize_product_detail(product: Product) -> ProductDetailResponse:
    latest_brief = max(product.creative_briefs, key=lambda item: item.created_at, default=None)
    copy_sets = sorted(product.copy_sets, key=lambda item: item.created_at, reverse=True)
    poster_variants = sorted(product.poster_variants, key=lambda item: item.created_at, reverse=True)
    jobs = sorted(product.job_runs, key=lambda item: item.created_at, reverse=True)[:10]
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
        recent_jobs=[serialize_job(item) for item in jobs],
        created_at=product.created_at,
        updated_at=product.updated_at,
    )
