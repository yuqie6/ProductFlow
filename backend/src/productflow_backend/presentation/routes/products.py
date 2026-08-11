from __future__ import annotations

from pathlib import Path

from fastapi import APIRouter, Depends, File, Form, HTTPException, Query, UploadFile, status
from fastapi.responses import FileResponse
from sqlalchemy.orm import Session

from productflow_backend.application.media_assets import (
    clear_product_cover,
    delete_product_image_asset,
    get_product_image_asset,
    list_product_image_assets,
    set_product_cover,
)
from productflow_backend.application.use_cases import (
    DEFAULT_PRODUCT_LIST_SORT,
    ProductListSort,
    add_canonical_product_images,
    add_reference_images,
    confirm_copy_set,
    create_canonical_product,
    create_product,
    delete_product,
    delete_reference_image,
    get_product_detail,
    get_product_history,
    list_products,
    update_copy_set,
)
from productflow_backend.domain.enums import MediaVerificationStatus, ProductWorkflowState
from productflow_backend.infrastructure.db.models import PosterVariant, SourceAsset
from productflow_backend.infrastructure.storage import ImageVariantName
from productflow_backend.presentation.deps import get_session, require_admin, require_deletion_enabled
from productflow_backend.presentation.image_variants import serve_image_variant
from productflow_backend.presentation.schemas.products import (
    CanonicalProductDetailResponse,
    CopySetResponse,
    CopySetUpdateRequest,
    ProductDetailResponse,
    ProductHistoryResponse,
    ProductImageAssetListResponse,
    ProductListResponse,
    SetProductCoverRequest,
    serialize_canonical_product_detail,
    serialize_copy_set,
    serialize_poster_variant,
    serialize_product_detail,
    serialize_product_image_asset,
    serialize_product_summary,
)
from productflow_backend.presentation.upload_validation import (
    read_validated_image_upload,
    validate_reference_image_count,
)

router = APIRouter(prefix="/api", tags=["products"], dependencies=[Depends(require_admin)])


@router.post("/v2/products", response_model=CanonicalProductDetailResponse, status_code=status.HTTP_201_CREATED)
async def create_canonical_product_endpoint(
    name: str = Form(...),
    images: list[UploadFile] = File(...),
    category: str | None = Form(default=None),
    price: str | None = Form(default=None),
    source_note: str | None = Form(default=None),
    session: Session = Depends(get_session),
) -> CanonicalProductDetailResponse:
    validate_reference_image_count(len(images))
    image_payloads: list[tuple[bytes, str, str]] = []
    for image in images:
        validated = await read_validated_image_upload(image, fallback_filename="reference.bin")
        image_payloads.append((validated.content, validated.filename, validated.mime_type))
    product = create_canonical_product(
        session,
        name=name,
        category=category,
        price=price,
        source_note=source_note,
        image_uploads=image_payloads,
    )
    return serialize_canonical_product_detail(product)


@router.get("/v2/products/{product_id}", response_model=CanonicalProductDetailResponse)
def get_canonical_product_endpoint(
    product_id: str,
    session: Session = Depends(get_session),
) -> CanonicalProductDetailResponse:
    return serialize_canonical_product_detail(get_product_detail(session, product_id))


@router.get("/v2/products/{product_id}/image-assets", response_model=ProductImageAssetListResponse)
def list_product_image_assets_endpoint(
    product_id: str,
    session: Session = Depends(get_session),
) -> ProductImageAssetListResponse:
    assets = list_product_image_assets(session, product_id)
    return ProductImageAssetListResponse(items=[serialize_product_image_asset(asset) for asset in assets])


@router.post(
    "/v2/products/{product_id}/image-assets",
    response_model=ProductImageAssetListResponse,
    status_code=status.HTTP_201_CREATED,
)
async def add_canonical_product_images_endpoint(
    product_id: str,
    images: list[UploadFile] = File(...),
    session: Session = Depends(get_session),
) -> ProductImageAssetListResponse:
    validate_reference_image_count(len(images))
    image_payloads: list[tuple[bytes, str, str]] = []
    for image in images:
        validated = await read_validated_image_upload(image, fallback_filename="image.bin")
        image_payloads.append((validated.content, validated.filename, validated.mime_type))
    assets = add_canonical_product_images(session, product_id=product_id, image_uploads=image_payloads)
    return ProductImageAssetListResponse(items=[serialize_product_image_asset(asset) for asset in assets])


@router.put("/v2/products/{product_id}/cover", response_model=CanonicalProductDetailResponse)
def set_product_cover_endpoint(
    product_id: str,
    payload: SetProductCoverRequest,
    session: Session = Depends(get_session),
) -> CanonicalProductDetailResponse:
    set_product_cover(session, product_id=product_id, asset_id=payload.asset_id)
    return serialize_canonical_product_detail(get_product_detail(session, product_id))


@router.delete("/v2/products/{product_id}/cover", response_model=CanonicalProductDetailResponse)
def clear_product_cover_endpoint(
    product_id: str,
    session: Session = Depends(get_session),
) -> CanonicalProductDetailResponse:
    clear_product_cover(session, product_id=product_id)
    return serialize_canonical_product_detail(get_product_detail(session, product_id))


@router.delete(
    "/v2/product-image-assets/{asset_id}",
    status_code=status.HTTP_204_NO_CONTENT,
    dependencies=[Depends(require_deletion_enabled)],
)
def delete_product_image_asset_endpoint(
    asset_id: str,
    session: Session = Depends(get_session),
) -> None:
    delete_product_image_asset(session, asset_id=asset_id)


@router.get("/v2/product-image-assets/{asset_id}/download")
def download_product_image_asset_endpoint(
    asset_id: str,
    variant: ImageVariantName = Query(default="original"),
    session: Session = Depends(get_session),
) -> FileResponse:
    asset = get_product_image_asset(session, asset_id)
    if asset.media_object.verification_status == MediaVerificationStatus.MISSING:
        raise HTTPException(status_code=404, detail="商品图片文件不存在")
    return serve_image_variant(
        storage_path=asset.media_object.storage_path,
        original_filename=asset.original_filename,
        mime_type=asset.media_object.mime_type,
        variant=variant,
        missing_file_detail="商品图片文件不存在",
    )


@router.post("/products", response_model=ProductDetailResponse, status_code=status.HTTP_201_CREATED)
async def create_product_endpoint(
    name: str = Form(...),
    image: UploadFile = File(...),
    reference_images: list[UploadFile] | None = File(default=None),
    category: str | None = Form(default=None),
    price: str | None = Form(default=None),
    source_note: str | None = Form(default=None),
    canvas_template_key: str | None = Form(default=None),
    template_language: str | None = Form(default=None),
    session: Session = Depends(get_session),
) -> ProductDetailResponse:
    main_image = await read_validated_image_upload(image, fallback_filename="upload.bin")
    reference_payloads: list[tuple[bytes, str, str]] = []
    validate_reference_image_count(len(reference_images or []))
    for reference_image in reference_images or []:
        validated_reference = await read_validated_image_upload(reference_image, fallback_filename="reference.bin")
        reference_payloads.append(
            (
                validated_reference.content,
                validated_reference.filename,
                validated_reference.mime_type,
            )
        )
    product = create_product(
        session,
        name=name,
        category=category,
        price=price,
        source_note=source_note,
        image_bytes=main_image.content,
        filename=main_image.filename,
        content_type=main_image.mime_type,
        reference_image_uploads=reference_payloads,
        canvas_template_key=canvas_template_key,
        template_language=template_language,
    )
    return serialize_product_detail(product)


@router.get("/products", response_model=ProductListResponse)
def list_products_endpoint(
    status: ProductWorkflowState | None = None,
    q: str | None = Query(default=None, max_length=100),
    sort: ProductListSort = Query(default=DEFAULT_PRODUCT_LIST_SORT),
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=20, ge=1, le=100),
    session: Session = Depends(get_session),
) -> ProductListResponse:
    items, total = list_products(session, status=status, page=page, page_size=page_size, q=q, sort=sort)
    return ProductListResponse(
        items=[serialize_product_summary(item) for item in items],
        total=total,
        page=page,
        page_size=page_size,
    )


@router.get("/products/{product_id}", response_model=ProductDetailResponse)
def get_product_detail_endpoint(product_id: str, session: Session = Depends(get_session)) -> ProductDetailResponse:
    return serialize_product_detail(get_product_detail(session, product_id))


@router.delete(
    "/products/{product_id}",
    status_code=status.HTTP_204_NO_CONTENT,
    dependencies=[Depends(require_deletion_enabled)],
)
def delete_product_endpoint(product_id: str, session: Session = Depends(get_session)) -> None:
    delete_product(session, product_id=product_id)


@router.post("/products/{product_id}/reference-images", response_model=ProductDetailResponse)
async def upload_reference_images_endpoint(
    product_id: str,
    reference_images: list[UploadFile] = File(...),
    session: Session = Depends(get_session),
) -> ProductDetailResponse:
    reference_payloads: list[tuple[bytes, str, str]] = []
    validate_reference_image_count(len(reference_images))
    for reference_image in reference_images:
        validated_reference = await read_validated_image_upload(reference_image, fallback_filename="reference.bin")
        reference_payloads.append(
            (
                validated_reference.content,
                validated_reference.filename,
                validated_reference.mime_type,
            )
        )
    product = add_reference_images(
        session,
        product_id=product_id,
        reference_image_uploads=reference_payloads,
    )
    return serialize_product_detail(product)


@router.patch("/copy-sets/{copy_set_id}", response_model=CopySetResponse)
def update_copy_set_endpoint(
    copy_set_id: str,
    payload: CopySetUpdateRequest,
    session: Session = Depends(get_session),
) -> CopySetResponse:
    copy_set = update_copy_set(
        session,
        copy_set_id=copy_set_id,
        structured_payload=payload.structured_payload,
    )
    return serialize_copy_set(copy_set)


@router.post("/copy-sets/{copy_set_id}/confirm", response_model=CopySetResponse)
def confirm_copy_set_endpoint(copy_set_id: str, session: Session = Depends(get_session)) -> CopySetResponse:
    copy_set = confirm_copy_set(session, copy_set_id=copy_set_id)
    return serialize_copy_set(copy_set)


@router.get("/posters/{poster_id}/download")
def download_poster_endpoint(
    poster_id: str,
    variant: ImageVariantName = Query(default="original"),
    session: Session = Depends(get_session),
) -> FileResponse:
    poster = session.get(PosterVariant, poster_id)
    if poster is None:
        raise HTTPException(status_code=404, detail="海报不存在")
    return serve_image_variant(
        storage_path=poster.storage_path,
        original_filename=f"{poster.kind.value}{Path(poster.storage_path).suffix or '.png'}",
        mime_type=poster.mime_type,
        variant=variant,
        missing_file_detail="海报文件不存在",
    )


@router.get("/source-assets/{asset_id}/download")
def download_source_asset_endpoint(
    asset_id: str,
    variant: ImageVariantName = Query(default="original"),
    session: Session = Depends(get_session),
) -> FileResponse:
    asset = session.get(SourceAsset, asset_id)
    if asset is None:
        raise HTTPException(status_code=404, detail="源图不存在")
    return serve_image_variant(
        storage_path=asset.storage_path,
        original_filename=asset.original_filename,
        mime_type=asset.mime_type,
        variant=variant,
        missing_file_detail="源图文件不存在",
    )


@router.delete("/source-assets/{asset_id}", response_model=ProductDetailResponse)
def delete_source_asset_endpoint(
    asset_id: str,
    session: Session = Depends(get_session),
) -> ProductDetailResponse:
    product = delete_reference_image(session, asset_id=asset_id)
    return serialize_product_detail(product)


@router.get("/products/{product_id}/history", response_model=ProductHistoryResponse)
def get_product_history_endpoint(product_id: str, session: Session = Depends(get_session)) -> ProductHistoryResponse:
    history = get_product_history(session, product_id)
    return ProductHistoryResponse(
        copy_sets=[serialize_copy_set(item) for item in history["copy_sets"]],
        poster_variants=[serialize_poster_variant(item) for item in history["poster_variants"]],
    )
