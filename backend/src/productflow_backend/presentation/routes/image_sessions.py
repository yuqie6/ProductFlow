from __future__ import annotations

from fastapi import APIRouter, Depends, File, HTTPException, Query, UploadFile, status
from fastapi.responses import FileResponse
from sqlalchemy.orm import Session

from productflow_backend.application.image_sessions import (
    add_image_session_reference_images,
    attach_image_session_asset_to_product,
    create_image_session,
    delete_image_session,
    delete_image_session_reference_image,
    generate_image_session_round,
    get_image_session_detail,
    list_image_sessions,
    update_image_session,
)
from productflow_backend.infrastructure.db.models import ImageSessionAsset
from productflow_backend.infrastructure.storage import ImageVariantName, LocalStorage
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.errors import raise_value_error_as_http
from productflow_backend.presentation.image_variants import build_variant_filename
from productflow_backend.presentation.schemas.image_sessions import (
    AttachImageSessionAssetRequest,
    CreateImageSessionRequest,
    GenerateImageSessionRoundRequest,
    ImageSessionDetailResponse,
    ImageSessionListResponse,
    ProductWritebackResponse,
    UpdateImageSessionRequest,
    serialize_image_session_detail,
    serialize_image_session_summary,
)
from productflow_backend.presentation.upload_validation import (
    read_validated_image_upload,
    validate_reference_image_count,
)

router = APIRouter(prefix="/api", tags=["image-sessions"], dependencies=[Depends(require_admin)])


@router.get("/image-sessions", response_model=ImageSessionListResponse)
def list_image_sessions_endpoint(
    product_id: str | None = Query(default=None),
    session: Session = Depends(get_session),
) -> ImageSessionListResponse:
    items = list_image_sessions(session, product_id=product_id)
    return ImageSessionListResponse(items=[serialize_image_session_summary(item) for item in items])


@router.post("/image-sessions", response_model=ImageSessionDetailResponse, status_code=status.HTTP_201_CREATED)
def create_image_session_endpoint(
    payload: CreateImageSessionRequest,
    session: Session = Depends(get_session),
) -> ImageSessionDetailResponse:
    try:
        image_session = create_image_session(session, product_id=payload.product_id, title=payload.title)
    except ValueError as exc:
        raise_value_error_as_http(exc)
    return serialize_image_session_detail(image_session)


@router.get("/image-sessions/{image_session_id}", response_model=ImageSessionDetailResponse)
def get_image_session_detail_endpoint(
    image_session_id: str,
    session: Session = Depends(get_session),
) -> ImageSessionDetailResponse:
    try:
        image_session = get_image_session_detail(session, image_session_id)
    except ValueError as exc:
        raise_value_error_as_http(exc)
    return serialize_image_session_detail(image_session)


@router.patch("/image-sessions/{image_session_id}", response_model=ImageSessionDetailResponse)
def update_image_session_endpoint(
    image_session_id: str,
    payload: UpdateImageSessionRequest,
    session: Session = Depends(get_session),
) -> ImageSessionDetailResponse:
    try:
        image_session = update_image_session(session, image_session_id=image_session_id, title=payload.title)
    except ValueError as exc:
        raise_value_error_as_http(exc)
    return serialize_image_session_detail(image_session)


@router.delete("/image-sessions/{image_session_id}", status_code=status.HTTP_204_NO_CONTENT)
def delete_image_session_endpoint(
    image_session_id: str,
    session: Session = Depends(get_session),
) -> None:
    try:
        delete_image_session(session, image_session_id=image_session_id)
    except ValueError as exc:
        raise_value_error_as_http(exc)


@router.post("/image-sessions/{image_session_id}/reference-images", response_model=ImageSessionDetailResponse)
async def upload_image_session_reference_images_endpoint(
    image_session_id: str,
    reference_images: list[UploadFile] = File(...),
    session: Session = Depends(get_session),
) -> ImageSessionDetailResponse:
    payloads: list[tuple[bytes, str, str]] = []
    validate_reference_image_count(len(reference_images))
    for image in reference_images:
        validated_image = await read_validated_image_upload(image, fallback_filename="reference.bin")
        payloads.append(
            (
                validated_image.content,
                validated_image.filename,
                validated_image.mime_type,
            )
        )
    try:
        image_session = add_image_session_reference_images(
            session,
            image_session_id=image_session_id,
            reference_image_uploads=payloads,
        )
    except ValueError as exc:
        raise_value_error_as_http(exc)
    return serialize_image_session_detail(image_session)


@router.delete(
    "/image-sessions/{image_session_id}/reference-images/{asset_id}",
    response_model=ImageSessionDetailResponse,
)
def delete_image_session_reference_image_endpoint(
    image_session_id: str,
    asset_id: str,
    session: Session = Depends(get_session),
) -> ImageSessionDetailResponse:
    try:
        image_session = delete_image_session_reference_image(
            session,
            image_session_id=image_session_id,
            asset_id=asset_id,
        )
    except ValueError as exc:
        raise_value_error_as_http(exc)
    return serialize_image_session_detail(image_session)


@router.post("/image-sessions/{image_session_id}/generate", response_model=ImageSessionDetailResponse)
def generate_image_session_round_endpoint(
    image_session_id: str,
    payload: GenerateImageSessionRoundRequest,
    session: Session = Depends(get_session),
) -> ImageSessionDetailResponse:
    try:
        image_session = generate_image_session_round(
            session,
            image_session_id=image_session_id,
            prompt=payload.prompt,
            size=payload.size,
        )
    except ValueError as exc:
        raise_value_error_as_http(exc)
    except RuntimeError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    return serialize_image_session_detail(image_session)


@router.post(
    "/image-sessions/{image_session_id}/assets/{asset_id}/attach-to-product",
    response_model=ProductWritebackResponse,
)
def attach_image_session_asset_to_product_endpoint(
    image_session_id: str,
    asset_id: str,
    payload: AttachImageSessionAssetRequest,
    session: Session = Depends(get_session),
) -> ProductWritebackResponse:
    try:
        product = attach_image_session_asset_to_product(
            session,
            image_session_id=image_session_id,
            asset_id=asset_id,
            target=payload.target,
            product_id=payload.product_id,
        )
    except ValueError as exc:
        raise_value_error_as_http(exc)
    message = "已加入商品参考图" if payload.target == "reference" else "已设为商品主图"
    return ProductWritebackResponse(product_id=product.id, message=message)


@router.get("/image-session-assets/{asset_id}/download")
def download_image_session_asset_endpoint(
    asset_id: str,
    variant: ImageVariantName = Query(default="original"),
    session: Session = Depends(get_session),
) -> FileResponse:
    asset = session.get(ImageSessionAsset, asset_id)
    if asset is None:
        raise HTTPException(status_code=404, detail="会话图片不存在")
    storage = LocalStorage()
    try:
        path, media_type = storage.resolve_for_variant(
            asset.storage_path,
            variant,
            fallback_media_type=asset.mime_type,
        )
    except ValueError as exc:
        raise HTTPException(status_code=404, detail="会话图片文件不存在") from exc
    if not path.exists():
        raise HTTPException(status_code=404, detail="会话图片文件不存在")
    filename = build_variant_filename(asset.original_filename, variant=variant, resolved_suffix=path.suffix)
    return FileResponse(path, media_type=media_type, filename=filename)
