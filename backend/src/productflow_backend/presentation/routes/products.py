from __future__ import annotations

from pathlib import Path

from fastapi import APIRouter, Depends, File, Form, HTTPException, Query, UploadFile, status
from fastapi.responses import FileResponse
from sqlalchemy.orm import Session

from productflow_backend.application.use_cases import (
    add_reference_images,
    confirm_copy_set,
    create_copy_job,
    create_poster_job,
    create_product,
    create_regenerate_poster_job,
    delete_product,
    delete_reference_image,
    get_product_detail,
    get_product_history,
    list_products,
    mark_job_enqueue_failed,
    update_copy_set,
)
from productflow_backend.domain.enums import ProductWorkflowState
from productflow_backend.infrastructure.db.models import PosterVariant, SourceAsset
from productflow_backend.infrastructure.queue import enqueue_copy_job, enqueue_poster_job
from productflow_backend.infrastructure.storage import ImageVariantName, LocalStorage
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.image_variants import build_variant_filename
from productflow_backend.presentation.schemas.jobs import JobRunResponse, serialize_job
from productflow_backend.presentation.schemas.products import (
    CopySetResponse,
    CopySetUpdateRequest,
    ProductDetailResponse,
    ProductHistoryResponse,
    ProductListResponse,
    serialize_copy_set,
    serialize_poster_variant,
    serialize_product_detail,
    serialize_product_summary,
)
from productflow_backend.presentation.upload_validation import (
    read_validated_image_upload,
    validate_reference_image_count,
)

router = APIRouter(prefix="/api", tags=["products"], dependencies=[Depends(require_admin)])


def _raise_http_error(exc: ValueError) -> None:
    detail = str(exc)
    if detail.endswith("不存在"):
        raise HTTPException(status_code=404, detail=detail) from exc
    raise HTTPException(status_code=400, detail=detail) from exc


@router.post("/products", response_model=ProductDetailResponse, status_code=status.HTTP_201_CREATED)
async def create_product_endpoint(
    name: str = Form(...),
    image: UploadFile = File(...),
    reference_images: list[UploadFile] | None = File(default=None),
    category: str | None = Form(default=None),
    price: str | None = Form(default=None),
    source_note: str | None = Form(default=None),
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
    try:
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
        )
    except ValueError as exc:
        _raise_http_error(exc)
    return serialize_product_detail(product)


@router.get("/products", response_model=ProductListResponse)
def list_products_endpoint(
    status: ProductWorkflowState | None = None,
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=20, ge=1, le=100),
    session: Session = Depends(get_session),
) -> ProductListResponse:
    items, total = list_products(session, status=status, page=page, page_size=page_size)
    return ProductListResponse(
        items=[serialize_product_summary(item) for item in items],
        total=total,
        page=page,
        page_size=page_size,
    )


@router.get("/products/{product_id}", response_model=ProductDetailResponse)
def get_product_detail_endpoint(product_id: str, session: Session = Depends(get_session)) -> ProductDetailResponse:
    try:
        return serialize_product_detail(get_product_detail(session, product_id))
    except ValueError as exc:
        _raise_http_error(exc)


@router.delete("/products/{product_id}", status_code=status.HTTP_204_NO_CONTENT)
def delete_product_endpoint(product_id: str, session: Session = Depends(get_session)) -> None:
    try:
        delete_product(session, product_id=product_id)
    except ValueError as exc:
        _raise_http_error(exc)


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
    try:
        product = add_reference_images(
            session,
            product_id=product_id,
            reference_image_uploads=reference_payloads,
        )
    except ValueError as exc:
        _raise_http_error(exc)
    return serialize_product_detail(product)


@router.post("/products/{product_id}/copy-jobs", response_model=JobRunResponse, status_code=status.HTTP_202_ACCEPTED)
def create_copy_job_endpoint(product_id: str, session: Session = Depends(get_session)) -> JobRunResponse:
    try:
        result = create_copy_job(session, product_id=product_id)
    except ValueError as exc:
        _raise_http_error(exc)
    job = result.job
    if result.created:
        try:
            enqueue_copy_job(job.id)
        except Exception as exc:  # noqa: BLE001
            mark_job_enqueue_failed(session, job_id=job.id, reason=str(exc))
            raise HTTPException(status_code=503, detail="任务队列暂不可用，请稍后重试") from exc
    return serialize_job(job)


@router.patch("/copy-sets/{copy_set_id}", response_model=CopySetResponse)
def update_copy_set_endpoint(
    copy_set_id: str,
    payload: CopySetUpdateRequest,
    session: Session = Depends(get_session),
) -> CopySetResponse:
    try:
        copy_set = update_copy_set(
            session,
            copy_set_id=copy_set_id,
            title=payload.title,
            selling_points=payload.selling_points,
            poster_headline=payload.poster_headline,
            cta=payload.cta,
        )
    except ValueError as exc:
        _raise_http_error(exc)
    return serialize_copy_set(copy_set)


@router.post("/copy-sets/{copy_set_id}/confirm", response_model=CopySetResponse)
def confirm_copy_set_endpoint(copy_set_id: str, session: Session = Depends(get_session)) -> CopySetResponse:
    try:
        copy_set = confirm_copy_set(session, copy_set_id=copy_set_id)
    except ValueError as exc:
        _raise_http_error(exc)
    return serialize_copy_set(copy_set)


@router.post("/products/{product_id}/poster-jobs", response_model=JobRunResponse, status_code=status.HTTP_202_ACCEPTED)
def create_poster_job_endpoint(product_id: str, session: Session = Depends(get_session)) -> JobRunResponse:
    try:
        result = create_poster_job(session, product_id=product_id)
    except ValueError as exc:
        _raise_http_error(exc)
    job = result.job
    if result.created:
        try:
            enqueue_poster_job(job.id)
        except Exception as exc:  # noqa: BLE001
            mark_job_enqueue_failed(session, job_id=job.id, reason=str(exc))
            raise HTTPException(status_code=503, detail="任务队列暂不可用，请稍后重试") from exc
    return serialize_job(job)


@router.post("/posters/{poster_id}/regenerate", response_model=JobRunResponse, status_code=status.HTTP_202_ACCEPTED)
def regenerate_poster_endpoint(poster_id: str, session: Session = Depends(get_session)) -> JobRunResponse:
    try:
        result = create_regenerate_poster_job(session, poster_id=poster_id)
    except ValueError as exc:
        _raise_http_error(exc)
    job = result.job
    if result.created:
        try:
            enqueue_poster_job(job.id)
        except Exception as exc:  # noqa: BLE001
            mark_job_enqueue_failed(session, job_id=job.id, reason=str(exc))
            raise HTTPException(status_code=503, detail="任务队列暂不可用，请稍后重试") from exc
    return serialize_job(job)


@router.get("/posters/{poster_id}/download")
def download_poster_endpoint(
    poster_id: str,
    variant: ImageVariantName = Query(default="original"),
    session: Session = Depends(get_session),
) -> FileResponse:
    poster = session.get(PosterVariant, poster_id)
    if poster is None:
        raise HTTPException(status_code=404, detail="海报不存在")
    storage = LocalStorage()
    try:
        path, media_type = storage.resolve_for_variant(
            poster.storage_path,
            variant,
            fallback_media_type=poster.mime_type,
        )
    except ValueError as exc:
        raise HTTPException(status_code=404, detail="海报文件不存在") from exc
    if not path.exists():
        raise HTTPException(status_code=404, detail="海报文件不存在")
    filename = build_variant_filename(
        f"{poster.kind.value}{Path(poster.storage_path).suffix or '.png'}",
        variant=variant,
        resolved_suffix=path.suffix,
    )
    return FileResponse(path, media_type=media_type, filename=filename)


@router.get("/source-assets/{asset_id}/download")
def download_source_asset_endpoint(
    asset_id: str,
    variant: ImageVariantName = Query(default="original"),
    session: Session = Depends(get_session),
) -> FileResponse:
    asset = session.get(SourceAsset, asset_id)
    if asset is None:
        raise HTTPException(status_code=404, detail="源图不存在")
    storage = LocalStorage()
    try:
        path, media_type = storage.resolve_for_variant(
            asset.storage_path,
            variant,
            fallback_media_type=asset.mime_type,
        )
    except ValueError as exc:
        raise HTTPException(status_code=404, detail="源图文件不存在") from exc
    if not path.exists():
        raise HTTPException(status_code=404, detail="源图文件不存在")
    filename = build_variant_filename(asset.original_filename, variant=variant, resolved_suffix=path.suffix)
    return FileResponse(path, media_type=media_type, filename=filename)


@router.delete("/source-assets/{asset_id}", response_model=ProductDetailResponse)
def delete_source_asset_endpoint(
    asset_id: str,
    session: Session = Depends(get_session),
) -> ProductDetailResponse:
    try:
        product = delete_reference_image(session, asset_id=asset_id)
    except ValueError as exc:
        _raise_http_error(exc)
    return serialize_product_detail(product)


@router.get("/products/{product_id}/history", response_model=ProductHistoryResponse)
def get_product_history_endpoint(product_id: str, session: Session = Depends(get_session)) -> ProductHistoryResponse:
    try:
        history = get_product_history(session, product_id)
    except ValueError as exc:
        _raise_http_error(exc)
    return ProductHistoryResponse(
        copy_sets=[serialize_copy_set(item) for item in history["copy_sets"]],
        poster_variants=[serialize_poster_variant(item) for item in history["poster_variants"]],
        jobs=[serialize_job(item) for item in history["jobs"]],
    )
