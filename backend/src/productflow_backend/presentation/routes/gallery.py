from __future__ import annotations

from fastapi import APIRouter, Depends, Response, status
from sqlalchemy.orm import Session

from productflow_backend.application.gallery import list_gallery_entries, save_generated_asset_to_gallery
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.errors import raise_value_error_as_http
from productflow_backend.presentation.schemas.gallery import (
    GalleryEntryListResponse,
    GalleryEntryResponse,
    SaveGalleryEntryRequest,
    serialize_gallery_entry,
)

router = APIRouter(prefix="/api/gallery", tags=["gallery"], dependencies=[Depends(require_admin)])


@router.get("", response_model=GalleryEntryListResponse)
def list_gallery_entries_endpoint(session: Session = Depends(get_session)) -> GalleryEntryListResponse:
    items = list_gallery_entries(session)
    return GalleryEntryListResponse(items=[serialize_gallery_entry(item) for item in items])


@router.post("", response_model=GalleryEntryResponse, status_code=status.HTTP_201_CREATED)
def save_gallery_entry_endpoint(
    payload: SaveGalleryEntryRequest,
    response: Response,
    session: Session = Depends(get_session),
) -> GalleryEntryResponse:
    try:
        result = save_generated_asset_to_gallery(
            session,
            image_session_asset_id=payload.image_session_asset_id,
        )
    except ValueError as exc:
        raise_value_error_as_http(exc)
    if not result.created:
        response.status_code = status.HTTP_200_OK
    return serialize_gallery_entry(result.entry)
