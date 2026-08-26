from __future__ import annotations

from fastapi import APIRouter, Body, Depends, File, Form, Query, Request, UploadFile, status
from sqlalchemy.orm import Session

from productflow_backend.application.local_image_edits.service import (
    adopt_local_image_edit_result,
    cancel_local_image_edit_task,
    create_local_image_edit_task,
    get_local_image_edit_capability,
    get_local_image_edit_task,
    list_local_image_edit_tasks,
    retry_local_image_edit_task,
    revert_local_image_edit_adoption,
    submit_queued_local_image_edit,
    update_local_image_edit_task,
)
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.infrastructure.storage import LocalStorage
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.schemas.local_image_edits import (
    AdoptLocalImageEditRequest,
    LocalImageEditCapabilityResponse,
    LocalImageEditRevisionRequest,
    LocalImageEditTaskListResponse,
    LocalImageEditTaskResponse,
    SubmitLocalImageEditRequest,
    parse_local_image_edit_draft,
    serialize_local_image_edit_capability,
    serialize_local_image_edit_task,
)
from productflow_backend.presentation.upload_validation import read_validated_image_upload

router = APIRouter(prefix="/api/v3", tags=["local-image-edits"], dependencies=[Depends(require_admin)])


@router.get("/local-image-edits/capability", response_model=LocalImageEditCapabilityResponse)
def get_local_image_edit_capability_endpoint() -> LocalImageEditCapabilityResponse:
    return serialize_local_image_edit_capability(get_local_image_edit_capability())


@router.get(
    "/products/{product_id}/image-edits",
    response_model=LocalImageEditTaskListResponse,
)
def list_local_image_edit_tasks_endpoint(
    product_id: str,
    limit: int = Query(default=50, ge=1, le=100),
    session: Session = Depends(get_session),
) -> LocalImageEditTaskListResponse:
    tasks = list_local_image_edit_tasks(session, product_id=product_id, limit=limit)
    return LocalImageEditTaskListResponse(
        items=[serialize_local_image_edit_task(item, include_audit=False) for item in tasks]
    )


@router.get(
    "/products/{product_id}/image-edits/{task_id}",
    response_model=LocalImageEditTaskResponse,
)
def get_local_image_edit_task_endpoint(
    product_id: str,
    task_id: str,
    session: Session = Depends(get_session),
) -> LocalImageEditTaskResponse:
    return serialize_local_image_edit_task(
        get_local_image_edit_task(session, product_id=product_id, task_id=task_id)
    )


@router.post(
    "/products/{product_id}/image-edits",
    response_model=LocalImageEditTaskResponse,
    status_code=status.HTTP_201_CREATED,
)
async def create_local_image_edit_task_endpoint(
    product_id: str,
    request: Request,
    source_asset_id: str = Form(...),
    operation: str = Form(...),
    mask: UploadFile = File(...),
    mask_geometry_json: str | None = Form(None),
    reference_asset_ids_json: str | None = Form(None),
    instruction: str | None = Form(None),
    source_text: str | None = Form(None),
    replacement_text: str | None = Form(None),
    target_node_id: str | None = Form(None),
    session: Session = Depends(get_session),
) -> LocalImageEditTaskResponse:
    await _reject_noncanonical_form_fields(request)
    validated_mask = await read_validated_image_upload(mask, fallback_filename="local-edit-mask.png")
    draft = _parse_form_draft(
        operation=operation,
        instruction=instruction,
        source_text=source_text,
        replacement_text=replacement_text,
        mask_geometry_json=mask_geometry_json,
        reference_asset_ids_json=reference_asset_ids_json,
    )
    task = create_local_image_edit_task(
        session,
        product_id=product_id,
        source_asset_id=source_asset_id,
        draft=draft,
        mask_png_bytes=validated_mask.content,
        target_node_id=target_node_id,
        storage=LocalStorage(),
    )
    return serialize_local_image_edit_task(get_local_image_edit_task(session, product_id=product_id, task_id=task.id))


@router.patch(
    "/products/{product_id}/image-edits/{task_id}",
    response_model=LocalImageEditTaskResponse,
)
async def update_local_image_edit_task_endpoint(
    product_id: str,
    task_id: str,
    request: Request,
    expected_revision: int = Form(..., ge=1),
    operation: str = Form(...),
    mask: UploadFile | None = File(None),
    mask_geometry_json: str | None = Form(None),
    reference_asset_ids_json: str | None = Form(None),
    instruction: str | None = Form(None),
    source_text: str | None = Form(None),
    replacement_text: str | None = Form(None),
    session: Session = Depends(get_session),
) -> LocalImageEditTaskResponse:
    await _reject_noncanonical_form_fields(request)
    existing = get_local_image_edit_task(session, product_id=product_id, task_id=task_id)
    validated_mask = (
        await read_validated_image_upload(mask, fallback_filename="local-edit-mask.png")
        if mask is not None
        else None
    )
    draft = _parse_form_draft(
        operation=operation,
        instruction=instruction,
        source_text=source_text,
        replacement_text=replacement_text,
        mask_geometry_json=mask_geometry_json,
        reference_asset_ids_json=reference_asset_ids_json,
    )
    update_local_image_edit_task(
        session,
        task_id=existing.id,
        expected_revision=expected_revision,
        draft=draft,
        mask_png_bytes=validated_mask.content if validated_mask is not None else None,
        storage=LocalStorage(),
    )
    return serialize_local_image_edit_task(get_local_image_edit_task(session, product_id=product_id, task_id=task_id))


@router.post(
    "/products/{product_id}/image-edits/{task_id}/submit",
    response_model=LocalImageEditTaskResponse,
    status_code=status.HTTP_202_ACCEPTED,
)
def submit_local_image_edit_task_endpoint(
    product_id: str,
    task_id: str,
    payload: SubmitLocalImageEditRequest,
    session: Session = Depends(get_session),
) -> LocalImageEditTaskResponse:
    result = submit_queued_local_image_edit(
        session,
        product_id=product_id,
        task_id=task_id,
        idempotency_key=payload.idempotency_key,
    )
    return serialize_local_image_edit_task(
        get_local_image_edit_task(session, product_id=product_id, task_id=result.task.id)
    )


@router.post(
    "/products/{product_id}/image-edits/{task_id}/cancel",
    response_model=LocalImageEditTaskResponse,
)
def cancel_local_image_edit_task_endpoint(
    product_id: str,
    task_id: str,
    payload: LocalImageEditRevisionRequest | None = Body(default=None),
    session: Session = Depends(get_session),
) -> LocalImageEditTaskResponse:
    _require_product_task(session, product_id=product_id, task_id=task_id)
    cancel_local_image_edit_task(
        session,
        task_id=task_id,
        expected_revision=payload.expected_revision if payload is not None else None,
    )
    return serialize_local_image_edit_task(get_local_image_edit_task(session, product_id=product_id, task_id=task_id))


@router.post(
    "/products/{product_id}/image-edits/{task_id}/retry",
    response_model=LocalImageEditTaskResponse,
    status_code=status.HTTP_202_ACCEPTED,
)
def retry_local_image_edit_task_endpoint(
    product_id: str,
    task_id: str,
    payload: LocalImageEditRevisionRequest | None = Body(default=None),
    session: Session = Depends(get_session),
) -> LocalImageEditTaskResponse:
    _require_product_task(session, product_id=product_id, task_id=task_id)
    result = retry_local_image_edit_task(
        session,
        task_id=task_id,
        expected_revision=payload.expected_revision if payload is not None else None,
    )
    return serialize_local_image_edit_task(
        get_local_image_edit_task(session, product_id=product_id, task_id=result.task.id)
    )


@router.post(
    "/products/{product_id}/image-edits/{task_id}/adopt",
    response_model=LocalImageEditTaskResponse,
)
def adopt_local_image_edit_task_endpoint(
    product_id: str,
    task_id: str,
    payload: AdoptLocalImageEditRequest,
    session: Session = Depends(get_session),
) -> LocalImageEditTaskResponse:
    _require_product_task(session, product_id=product_id, task_id=task_id)
    adopt_local_image_edit_result(
        session,
        task_id=task_id,
        expected_current_artifact_id=payload.expected_current_artifact_id,
    )
    return serialize_local_image_edit_task(get_local_image_edit_task(session, product_id=product_id, task_id=task_id))


@router.post(
    "/products/{product_id}/image-edits/{task_id}/adoptions/{adoption_event_id}/revert",
    response_model=LocalImageEditTaskResponse,
)
def revert_local_image_edit_adoption_endpoint(
    product_id: str,
    task_id: str,
    adoption_event_id: str,
    payload: AdoptLocalImageEditRequest,
    session: Session = Depends(get_session),
) -> LocalImageEditTaskResponse:
    _require_product_task(session, product_id=product_id, task_id=task_id)
    revert_local_image_edit_adoption(
        session,
        adoption_event_id=adoption_event_id,
        expected_current_artifact_id=payload.expected_current_artifact_id,
        task_id=task_id,
    )
    return serialize_local_image_edit_task(get_local_image_edit_task(session, product_id=product_id, task_id=task_id))


def _parse_form_draft(
    *,
    operation: str,
    instruction: str | None,
    source_text: str | None,
    replacement_text: str | None,
    mask_geometry_json: str | None,
    reference_asset_ids_json: str | None,
):
    return parse_local_image_edit_draft(
        operation=operation,
        instruction=instruction,
        source_text=source_text,
        replacement_text=replacement_text,
        mask_geometry_json=mask_geometry_json,
        reference_asset_ids_json=reference_asset_ids_json,
    )


def _require_product_task(session: Session, *, product_id: str, task_id: str):
    return get_local_image_edit_task(session, product_id=product_id, task_id=task_id)


async def _reject_noncanonical_form_fields(request: Request) -> None:
    form = await request.form()
    aliases = {"mask_geometry", "reference_asset_ids"}
    present = sorted(aliases.intersection(form.keys()))
    if present:
        raise BusinessValidationError(f"局部编辑只接受正式字段: {', '.join(present)}")


__all__ = ["router"]
