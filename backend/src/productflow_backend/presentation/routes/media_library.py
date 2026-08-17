from __future__ import annotations

from fastapi import APIRouter, Depends, Header, Query, Response, status
from fastapi.responses import FileResponse
from sqlalchemy.orm import Session

from productflow_backend.application.media_library.contracts import MediaLibrarySourceType
from productflow_backend.application.media_library.organization import (
    create_media_library_folder,
    create_media_library_tag,
    delete_media_library_folder,
    delete_media_library_tag,
    move_media_library_assets,
    rename_media_library_folder,
    rename_media_library_tag,
    set_media_library_asset_tags,
)
from productflow_backend.application.media_library.queries import (
    get_media_library_asset,
    get_media_library_bootstrap,
    list_media_library_assets,
)
from productflow_backend.application.media_library.service import (
    archive_media_library_asset,
    collect_media_library_assets_to_product,
    restore_media_library_asset,
    save_media_library_asset_from_product,
    save_media_library_asset_from_session,
)
from productflow_backend.application.media_library.workflow import (
    list_workflow_media_library_assets,
    remove_workflow_media_library_asset,
    sync_workflow_media_library_assets,
)
from productflow_backend.domain.enums import MediaVerificationStatus
from productflow_backend.domain.errors import ConflictError, NotFoundError
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.image_variants import ImageVariantName, serve_image_variant
from productflow_backend.presentation.schemas.media_library import (
    MediaLibraryAssetListResponse,
    MediaLibraryAssetResponse,
    MediaLibraryBootstrapResponse,
    MediaLibraryCollectBatchToProductRequest,
    MediaLibraryFolderResponse,
    MediaLibraryMoveAssetsRequest,
    MediaLibraryNameRequest,
    MediaLibraryRenameFolderRequest,
    MediaLibraryRenameTagRequest,
    MediaLibrarySaveFromProductRequest,
    MediaLibrarySaveFromSessionRequest,
    MediaLibrarySetTagsRequest,
    MediaLibraryTagResponse,
    WorkflowMediaLibraryAssetListResponse,
    WorkflowMediaLibraryAssetResponse,
    WorkflowMediaLibrarySyncRequest,
    serialize_media_library_asset,
)
from productflow_backend.presentation.schemas.products import (
    ProductImageAssetResponse,
    serialize_product_image_asset,
)

router = APIRouter(
    prefix="/api/media-library",
    tags=["media-library"],
    dependencies=[Depends(require_admin)],
)


def _serialize_workflow_media_library_assets(
    workflow_id: str,
    records,
) -> WorkflowMediaLibraryAssetListResponse:
    return WorkflowMediaLibraryAssetListResponse(
        workflow_id=workflow_id,
        items=[
            WorkflowMediaLibraryAssetResponse(
                asset=serialize_media_library_asset(record.asset),
                product_image_asset_id=record.product_image_asset_id,
                linked_at=record.linked_at,
            )
            for record in records
        ],
    )


@router.get("", response_model=MediaLibraryAssetListResponse)
def list_media_library_endpoint(
    limit: int = Query(default=20, ge=1, le=100),
    cursor: str | None = None,
    include_archived: bool = Query(default=False),
    q: str | None = Query(default=None, max_length=255),
    source_type: MediaLibrarySourceType | None = Query(default=None),
    folder_id: str | None = Query(default=None, max_length=36),
    tag: str | None = Query(default=None, max_length=80),
    session: Session = Depends(get_session),
) -> MediaLibraryAssetListResponse:
    page = list_media_library_assets(
        session,
        limit=limit,
        cursor=cursor,
        include_archived=include_archived,
        search=q,
        source_type=source_type,
        folder_id=folder_id,
        tag=tag,
    )
    return MediaLibraryAssetListResponse(
        items=[serialize_media_library_asset(item) for item in page.items],
        next_cursor=page.next_cursor,
    )


@router.get("/bootstrap", response_model=MediaLibraryBootstrapResponse)
def media_library_bootstrap_endpoint(session: Session = Depends(get_session)) -> MediaLibraryBootstrapResponse:
    bootstrap = get_media_library_bootstrap(session)
    return MediaLibraryBootstrapResponse(
        total_count=bootstrap.total_count,
        active_count=bootstrap.active_count,
        archived_count=bootstrap.archived_count,
        unorganized_count=bootstrap.unorganized_count,
        folders=[MediaLibraryFolderResponse(id=id_, name=name, count=count) for id_, name, count in bootstrap.folders],
        tags=[MediaLibraryTagResponse(id=id_, name=name, count=count) for id_, name, count in bootstrap.tags],
    )


@router.post("/folders", response_model=MediaLibraryFolderResponse)
def create_media_library_folder_endpoint(
    payload: MediaLibraryNameRequest,
    response: Response,
    session: Session = Depends(get_session),
) -> MediaLibraryFolderResponse:
    result = create_media_library_folder(session, name=payload.name)
    response.status_code = status.HTTP_201_CREATED if result.created else status.HTTP_200_OK
    return MediaLibraryFolderResponse(id=result.folder.id, name=result.folder.name)


@router.patch("/folders/{folder_id}", response_model=MediaLibraryFolderResponse)
def rename_media_library_folder_endpoint(
    folder_id: str,
    payload: MediaLibraryRenameFolderRequest,
    session: Session = Depends(get_session),
):
    folder = rename_media_library_folder(
        session,
        folder_id=folder_id,
        expected_name=payload.expected_name,
        name=payload.name,
    )
    return MediaLibraryFolderResponse(id=folder.id, name=folder.name)


@router.delete("/folders/{folder_id}")
def delete_media_library_folder_endpoint(folder_id: str, session: Session = Depends(get_session)):
    return {"folder_id": folder_id, "unorganized_count": delete_media_library_folder(session, folder_id=folder_id)}


@router.post("/tags", response_model=MediaLibraryTagResponse)
def create_media_library_tag_endpoint(
    payload: MediaLibraryNameRequest,
    response: Response,
    session: Session = Depends(get_session),
) -> MediaLibraryTagResponse:
    result = create_media_library_tag(session, name=payload.name)
    response.status_code = status.HTTP_201_CREATED if result.created else status.HTTP_200_OK
    return MediaLibraryTagResponse(id=result.tag.id, name=result.tag.name)


@router.patch("/tags/{tag_id}", response_model=MediaLibraryTagResponse)
def rename_media_library_tag_endpoint(
    tag_id: str,
    payload: MediaLibraryRenameTagRequest,
    session: Session = Depends(get_session),
):
    tag = rename_media_library_tag(
        session,
        tag_id=tag_id,
        expected_name=payload.expected_name,
        name=payload.name,
    )
    return MediaLibraryTagResponse(id=tag.id, name=tag.name)


@router.delete("/tags/{tag_id}")
def delete_media_library_tag_endpoint(tag_id: str, session: Session = Depends(get_session)):
    return {"tag_id": tag_id, "removed_assignment_count": delete_media_library_tag(session, tag_id=tag_id)}


@router.post("/organize/move", response_model=list[MediaLibraryAssetResponse])
def move_media_library_assets_endpoint(
    payload: MediaLibraryMoveAssetsRequest,
    session: Session = Depends(get_session),
):
    assets = move_media_library_assets(
        session,
        asset_ids=payload.asset_ids,
        folder_id=payload.folder_id,
        expected_revision=payload.expected_revisions,
    )
    return [serialize_media_library_asset(asset) for asset in assets]


@router.post("/organize/tags", response_model=list[MediaLibraryAssetResponse])
def set_media_library_tags_endpoint(
    payload: MediaLibrarySetTagsRequest,
    session: Session = Depends(get_session),
):
    assets = set_media_library_asset_tags(
        session,
        asset_ids=payload.asset_ids,
        tag_names=payload.tag_names,
        expected_revision=payload.expected_revisions,
    )
    return [serialize_media_library_asset(asset) for asset in assets]


@router.post("/from-session", response_model=MediaLibraryAssetResponse)
def save_from_session_endpoint(
    payload: MediaLibrarySaveFromSessionRequest,
    response: Response,
    session: Session = Depends(get_session),
) -> MediaLibraryAssetResponse:
    result = save_media_library_asset_from_session(
        session,
        image_session_asset_id=payload.image_session_asset_id,
    )
    response.status_code = status.HTTP_201_CREATED if result.created else status.HTTP_200_OK
    return serialize_media_library_asset(result.asset)


@router.post("/from-product", response_model=MediaLibraryAssetResponse)
def save_from_product_endpoint(
    payload: MediaLibrarySaveFromProductRequest,
    response: Response,
    session: Session = Depends(get_session),
) -> MediaLibraryAssetResponse:
    result = save_media_library_asset_from_product(
        session,
        product_image_asset_id=payload.product_image_asset_id,
    )
    response.status_code = status.HTTP_201_CREATED if result.created else status.HTTP_200_OK
    return serialize_media_library_asset(result.asset)


@router.post("/collect", response_model=list[ProductImageAssetResponse])
def collect_to_product_endpoint(
    payload: MediaLibraryCollectBatchToProductRequest,
    idempotency_key: str = Header(alias="Idempotency-Key", min_length=1, max_length=200),
    session: Session = Depends(get_session),
) -> list[ProductImageAssetResponse]:
    results = collect_media_library_assets_to_product(
        session,
        product_id=payload.product_id,
        library_asset_ids=payload.media_library_asset_ids,
        idempotency_key=idempotency_key,
    )
    return [serialize_product_image_asset(result.asset) for result in results]


@router.get(
    "/workflows/{workflow_id}/media-library",
    response_model=WorkflowMediaLibraryAssetListResponse,
)
def list_workflow_media_library_endpoint(
    workflow_id: str,
    product_id: str = Query(..., min_length=1, max_length=36),
    limit: int = Query(default=100, ge=1, le=100),
    session: Session = Depends(get_session),
) -> WorkflowMediaLibraryAssetListResponse:
    records = list_workflow_media_library_assets(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        limit=limit,
    )
    return _serialize_workflow_media_library_assets(workflow_id, records)


@router.post(
    "/workflows/{workflow_id}/media-library/sync",
    response_model=WorkflowMediaLibraryAssetListResponse,
)
def sync_workflow_media_library_endpoint(
    workflow_id: str,
    payload: WorkflowMediaLibrarySyncRequest,
    product_id: str = Query(..., min_length=1, max_length=36),
    session: Session = Depends(get_session),
) -> WorkflowMediaLibraryAssetListResponse:
    records = sync_workflow_media_library_assets(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        media_library_asset_ids=payload.media_library_asset_ids,
    )
    return _serialize_workflow_media_library_assets(workflow_id, records)


@router.delete(
    "/workflows/{workflow_id}/media-library/{media_library_asset_id}",
    status_code=status.HTTP_204_NO_CONTENT,
)
def remove_workflow_media_library_endpoint(
    workflow_id: str,
    media_library_asset_id: str,
    product_id: str = Query(..., min_length=1, max_length=36),
    session: Session = Depends(get_session),
) -> Response:
    remove_workflow_media_library_asset(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        media_library_asset_id=media_library_asset_id,
    )
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.get("/{asset_id}", response_model=MediaLibraryAssetResponse)
def get_media_library_endpoint(
    asset_id: str,
    session: Session = Depends(get_session),
) -> MediaLibraryAssetResponse:
    return serialize_media_library_asset(get_media_library_asset(session, asset_id=asset_id))


@router.get("/{asset_id}/download")
def download_media_library_asset_endpoint(
    asset_id: str,
    variant: ImageVariantName = Query(default="original"),
    session: Session = Depends(get_session),
) -> FileResponse:
    asset = get_media_library_asset(session, asset_id=asset_id)
    media = asset.media_object
    if media is None:
        raise NotFoundError("素材库媒体文件不存在")
    if media.verification_status != MediaVerificationStatus.VERIFIED:
        raise ConflictError("素材库媒体尚未通过核验")
    return serve_image_variant(
        storage_path=media.storage_path,
        original_filename=asset.original_filename,
        mime_type=media.mime_type,
        variant=variant,
        missing_file_detail="素材库媒体文件不存在",
    )


@router.post("/{asset_id}/archive", response_model=MediaLibraryAssetResponse)
def archive_media_library_endpoint(
    asset_id: str,
    expected_revision: int | None = Query(default=None, ge=1),
    session: Session = Depends(get_session),
) -> MediaLibraryAssetResponse:
    return serialize_media_library_asset(
        archive_media_library_asset(session, asset_id=asset_id, expected_revision=expected_revision)
    )


@router.post("/{asset_id}/restore", response_model=MediaLibraryAssetResponse)
def restore_media_library_endpoint(
    asset_id: str,
    expected_revision: int | None = Query(default=None, ge=1),
    session: Session = Depends(get_session),
) -> MediaLibraryAssetResponse:
    return serialize_media_library_asset(
        restore_media_library_asset(session, asset_id=asset_id, expected_revision=expected_revision)
    )
