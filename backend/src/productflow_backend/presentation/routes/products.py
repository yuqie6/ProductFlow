from __future__ import annotations

from fastapi import APIRouter, Depends, File, Form, HTTPException, Query, UploadFile, status
from fastapi.responses import FileResponse
from sqlalchemy.orm import Session
from starlette.background import BackgroundTask

from productflow_backend.application.product_images.archives import (
    build_gallery_archive,
    cleanup_gallery_archive,
)
from productflow_backend.application.product_images.assets import (
    clear_product_cover,
    delete_product_image_asset,
    get_product_image_asset,
    set_product_cover,
)
from productflow_backend.application.product_images.mutations import (
    GalleryAssetMove,
    create_gallery_folder,
    delete_gallery_folder,
    move_gallery_assets,
    rename_gallery_asset,
    rename_gallery_folder,
)
from productflow_backend.application.product_images.queries import (
    GALLERY_DEFAULT_LIMIT,
    GALLERY_MAX_LIMIT,
    GalleryAssetSort,
    GalleryDirectoryKind,
    get_gallery_asset_detail,
    get_gallery_bootstrap,
    list_gallery_assets,
)
from productflow_backend.application.products import (
    DEFAULT_PRODUCT_LIST_SORT,
    ProductListSort,
    add_canonical_product_images,
    create_canonical_product_with_assets,
    delete_product,
    get_product_detail,
    list_products,
)
from productflow_backend.domain.enums import MediaVerificationStatus
from productflow_backend.infrastructure.storage import ImageVariantName
from productflow_backend.presentation.deps import get_session, require_admin, require_deletion_enabled
from productflow_backend.presentation.image_variants import serve_image_variant
from productflow_backend.presentation.schemas.products import (
    CanonicalProductCreateResponse,
    CanonicalProductDetailResponse,
    CreateGalleryFolderRequest,
    DeleteGalleryFolderResponse,
    DownloadGalleryArchiveRequest,
    GalleryAssetPageResponse,
    GalleryAssetResponse,
    GalleryBootstrapResponse,
    GalleryFolderMutationResponse,
    MoveGalleryAssetsRequest,
    ProductImageAssetListResponse,
    ProductListResponse,
    RenameGalleryAssetRequest,
    RenameGalleryFolderRequest,
    SetProductCoverRequest,
    serialize_canonical_product_detail,
    serialize_gallery_asset,
    serialize_gallery_bootstrap,
    serialize_product_image_asset,
    serialize_product_summary,
)
from productflow_backend.presentation.upload_validation import (
    read_validated_image_upload,
    validate_reference_image_count,
)

router = APIRouter(prefix="/api", tags=["products"], dependencies=[Depends(require_admin)])


@router.post("/v2/products", response_model=CanonicalProductCreateResponse, status_code=status.HTTP_201_CREATED)
async def create_canonical_product_endpoint(
    name: str = Form(...),
    images: list[UploadFile] = File(...),
    category: str | None = Form(default=None),
    price: str | None = Form(default=None),
    source_note: str | None = Form(default=None),
    session: Session = Depends(get_session),
) -> CanonicalProductCreateResponse:
    validate_reference_image_count(len(images))
    image_payloads: list[tuple[bytes, str, str]] = []
    for image in images:
        validated = await read_validated_image_upload(image, fallback_filename="reference.bin")
        image_payloads.append((validated.content, validated.filename, validated.mime_type))
    creation = create_canonical_product_with_assets(
        session,
        name=name,
        category=category,
        price=price,
        source_note=source_note,
        image_uploads=image_payloads,
    )
    return CanonicalProductCreateResponse(
        product=serialize_canonical_product_detail(creation.product),
        created_assets=[serialize_product_image_asset(asset) for asset in creation.created_assets],
    )


@router.get("/v2/products/{product_id}", response_model=CanonicalProductDetailResponse)
def get_canonical_product_endpoint(
    product_id: str,
    session: Session = Depends(get_session),
) -> CanonicalProductDetailResponse:
    return serialize_canonical_product_detail(get_product_detail(session, product_id))


@router.get("/v2/products/{product_id}/image-library", response_model=GalleryBootstrapResponse)
def get_product_image_library_endpoint(
    product_id: str,
    session: Session = Depends(get_session),
) -> GalleryBootstrapResponse:
    return serialize_gallery_bootstrap(get_gallery_bootstrap(session, product_id=product_id))


@router.post(
    "/v2/products/{product_id}/image-folders",
    response_model=GalleryFolderMutationResponse,
    status_code=status.HTTP_201_CREATED,
)
def create_product_image_folder_endpoint(
    product_id: str,
    payload: CreateGalleryFolderRequest,
    session: Session = Depends(get_session),
) -> GalleryFolderMutationResponse:
    folder = create_gallery_folder(session, product_id=product_id, name=payload.name)
    return GalleryFolderMutationResponse(id=folder.id, name=folder.name, sort_order=folder.sort_order)


@router.patch(
    "/v2/products/{product_id}/image-folders/{folder_id}",
    response_model=GalleryFolderMutationResponse,
)
def rename_product_image_folder_endpoint(
    product_id: str,
    folder_id: str,
    payload: RenameGalleryFolderRequest,
    session: Session = Depends(get_session),
) -> GalleryFolderMutationResponse:
    folder = rename_gallery_folder(
        session,
        product_id=product_id,
        folder_id=folder_id,
        expected_name=payload.expected_name,
        name=payload.name,
    )
    return GalleryFolderMutationResponse(id=folder.id, name=folder.name, sort_order=folder.sort_order)


@router.delete(
    "/v2/products/{product_id}/image-folders/{folder_id}",
    response_model=DeleteGalleryFolderResponse,
)
def delete_product_image_folder_endpoint(
    product_id: str,
    folder_id: str,
    expected_name: str = Query(min_length=1, max_length=120),
    session: Session = Depends(get_session),
) -> DeleteGalleryFolderResponse:
    moved_count = delete_gallery_folder(
        session,
        product_id=product_id,
        folder_id=folder_id,
        expected_name=expected_name,
    )
    return DeleteGalleryFolderResponse(
        folder_id=folder_id,
        moved_to_unorganized_count=moved_count,
    )


@router.post(
    "/v2/products/{product_id}/image-assets/move",
    response_model=GalleryAssetPageResponse,
)
def move_product_image_assets_endpoint(
    product_id: str,
    payload: MoveGalleryAssetsRequest,
    session: Session = Depends(get_session),
) -> GalleryAssetPageResponse:
    assets = move_gallery_assets(
        session,
        product_id=product_id,
        moves=[
            GalleryAssetMove(
                asset_id=item.asset_id,
                expected_folder_id=item.expected_folder_id,
            )
            for item in payload.items
        ],
        folder_id=payload.folder_id,
    )
    return GalleryAssetPageResponse(
        items=[
            serialize_gallery_asset(
                get_gallery_asset_detail(session, product_id=product_id, asset_id=asset.id)
            )
            for asset in assets
        ],
        next_cursor=None,
    )


@router.post("/v2/products/{product_id}/image-assets/download-archive")
def download_product_image_archive_endpoint(
    product_id: str,
    payload: DownloadGalleryArchiveRequest,
    session: Session = Depends(get_session),
) -> FileResponse:
    archive = build_gallery_archive(
        session,
        product_id=product_id,
        asset_ids=payload.asset_ids,
    )
    return FileResponse(
        path=archive.path,
        filename=archive.filename,
        media_type="application/zip",
        background=BackgroundTask(cleanup_gallery_archive, archive),
    )


@router.get("/v2/products/{product_id}/image-assets", response_model=GalleryAssetPageResponse)
def list_product_image_assets_endpoint(
    product_id: str,
    directory_kind: GalleryDirectoryKind = Query(default=GalleryDirectoryKind.ALL),
    directory_key: str | None = Query(default=None, max_length=120),
    q: str = Query(default="", max_length=255),
    sort: GalleryAssetSort = Query(default=GalleryAssetSort.CREATED_DESC),
    after: str = Query(default="", max_length=4096),
    limit: int = Query(default=GALLERY_DEFAULT_LIMIT, ge=1, le=GALLERY_MAX_LIMIT),
    session: Session = Depends(get_session),
) -> GalleryAssetPageResponse:
    page = list_gallery_assets(
        session,
        product_id=product_id,
        directory_kind=directory_kind,
        directory_key=directory_key,
        query=q,
        sort=sort,
        after=after,
        limit=limit,
    )
    return GalleryAssetPageResponse(
        items=[serialize_gallery_asset(record) for record in page.items],
        next_cursor=page.next_cursor,
    )


@router.get(
    "/v2/products/{product_id}/image-assets/{asset_id}",
    response_model=GalleryAssetResponse,
)
def get_product_image_asset_detail_endpoint(
    product_id: str,
    asset_id: str,
    session: Session = Depends(get_session),
) -> GalleryAssetResponse:
    return serialize_gallery_asset(
        get_gallery_asset_detail(session, product_id=product_id, asset_id=asset_id)
    )


@router.patch(
    "/v2/products/{product_id}/image-assets/{asset_id}",
    response_model=GalleryAssetResponse,
)
def rename_product_image_asset_endpoint(
    product_id: str,
    asset_id: str,
    payload: RenameGalleryAssetRequest,
    session: Session = Depends(get_session),
) -> GalleryAssetResponse:
    rename_gallery_asset(
        session,
        product_id=product_id,
        asset_id=asset_id,
        expected_display_name=payload.expected_display_name,
        display_name=payload.display_name,
    )
    return serialize_gallery_asset(
        get_gallery_asset_detail(session, product_id=product_id, asset_id=asset_id)
    )


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


@router.get("/v2/products", response_model=ProductListResponse)
def list_products_endpoint(
    q: str | None = Query(default=None, max_length=100),
    sort: ProductListSort = Query(default=DEFAULT_PRODUCT_LIST_SORT),
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=20, ge=1, le=100),
    session: Session = Depends(get_session),
) -> ProductListResponse:
    items, total = list_products(session, page=page, page_size=page_size, q=q, sort=sort)
    return ProductListResponse(
        items=[serialize_product_summary(item) for item in items],
        total=total,
        page=page,
        page_size=page_size,
    )


@router.delete(
    "/v2/products/{product_id}",
    status_code=status.HTTP_204_NO_CONTENT,
    dependencies=[Depends(require_deletion_enabled)],
)
def delete_product_endpoint(product_id: str, session: Session = Depends(get_session)) -> None:
    delete_product(session, product_id=product_id)
