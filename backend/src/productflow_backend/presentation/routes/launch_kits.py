from __future__ import annotations

from fastapi import APIRouter, Depends, Query, status
from fastapi.responses import PlainTextResponse
from sqlalchemy.orm import Session

from productflow_backend.application.launch_kit.edits import save_launch_kit_manual_edits
from productflow_backend.application.launch_kit.exporting import launch_kit_export_filename, render_launch_kit_markdown
from productflow_backend.application.launch_kit.feedback import save_launch_kit_feedback
from productflow_backend.application.launch_kit.generation import (
    create_launch_kit_generation_task,
    execute_launch_kit_generation_task,
    submit_launch_kit_generation_task,
)
from productflow_backend.application.launch_kit.mutations import create_launch_kit
from productflow_backend.application.launch_kit.payloads import (
    SellerFeedbackPayload,
    SourceReferencePayload,
    StoreProfilePayload,
)
from productflow_backend.application.launch_kit.playbooks import ensure_starter_category_playbooks
from productflow_backend.application.launch_kit.query import get_launch_kit, list_launch_kits
from productflow_backend.application.launch_kit.store_profile import get_store_profile_payload, save_store_profile
from productflow_backend.config import get_runtime_settings
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.schemas.launch_kits import (
    LaunchKitCreateRequest,
    LaunchKitDetailResponse,
    LaunchKitFeedbackRequest,
    LaunchKitListResponse,
    LaunchKitManualEditsRequest,
    LaunchKitStatusResponse,
    StoreProfileResponse,
    StoreProfileUpdateRequest,
    serialize_launch_kit_detail,
    serialize_launch_kit_summary,
    serialize_launch_kit_task,
)

router = APIRouter(prefix="/api/launch-kits", tags=["launch-kits"], dependencies=[Depends(require_admin)])


@router.get("", response_model=LaunchKitListResponse)
def list_launch_kits_endpoint(
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=20, ge=1, le=100),
    session: Session = Depends(get_session),
) -> LaunchKitListResponse:
    ensure_starter_category_playbooks(session)
    items, total = list_launch_kits(session, page=page, page_size=page_size)
    return LaunchKitListResponse(
        items=[serialize_launch_kit_summary(item) for item in items],
        total=total,
        page=page,
        page_size=page_size,
    )


@router.post("", response_model=LaunchKitDetailResponse, status_code=status.HTTP_201_CREATED)
def create_launch_kit_endpoint(
    payload: LaunchKitCreateRequest,
    session: Session = Depends(get_session),
) -> LaunchKitDetailResponse:
    ensure_starter_category_playbooks(session)
    references = None
    if payload.source_references is not None:
        references = SourceReferencePayload(
            product_name=payload.product_name,
            pasted_reference_text=payload.source_references.pasted_reference_text,
            reference_urls=payload.source_references.reference_urls,
            notes=payload.source_references.notes,
        )
    launch_kit = create_launch_kit(
        session,
        product_name=payload.product_name,
        category_key=payload.category_key,
        target_platforms=payload.target_platforms,
        source_references=references,
    )
    return serialize_launch_kit_detail(launch_kit)


@router.get("/store-profile", response_model=StoreProfileResponse)
def get_store_profile_endpoint(session: Session = Depends(get_session)) -> StoreProfileResponse:
    return StoreProfileResponse.model_validate(get_store_profile_payload(session).model_dump(mode="json"))


@router.put("/store-profile", response_model=StoreProfileResponse)
def update_store_profile_endpoint(
    payload: StoreProfileUpdateRequest,
    session: Session = Depends(get_session),
) -> StoreProfileResponse:
    profile = save_store_profile(
        session,
        profile=StoreProfilePayload(**payload.model_dump(mode="json")),
    )
    return StoreProfileResponse.model_validate(profile.model_dump(mode="json"))


@router.get("/{launch_kit_id}", response_model=LaunchKitDetailResponse)
def get_launch_kit_endpoint(launch_kit_id: str, session: Session = Depends(get_session)) -> LaunchKitDetailResponse:
    return serialize_launch_kit_detail(get_launch_kit(session, launch_kit_id))


@router.post(
    "/{launch_kit_id}/generate",
    response_model=LaunchKitDetailResponse,
    status_code=status.HTTP_202_ACCEPTED,
)
def generate_launch_kit_endpoint(
    launch_kit_id: str,
    session: Session = Depends(get_session),
) -> LaunchKitDetailResponse:
    if get_runtime_settings().launch_kit_inline_generation:
        task = create_launch_kit_generation_task(session, launch_kit_id=launch_kit_id)
        execute_launch_kit_generation_task(task.id, session_factory=get_session_factory())
        session.expire_all()
        return serialize_launch_kit_detail(get_launch_kit(session, launch_kit_id))

    launch_kit = submit_launch_kit_generation_task(session, launch_kit_id=launch_kit_id)
    return serialize_launch_kit_detail(launch_kit)


@router.post("/{launch_kit_id}/feedback", response_model=LaunchKitDetailResponse)
def save_launch_kit_feedback_endpoint(
    launch_kit_id: str,
    payload: LaunchKitFeedbackRequest,
    session: Session = Depends(get_session),
) -> LaunchKitDetailResponse:
    launch_kit = save_launch_kit_feedback(
        session,
        launch_kit_id=launch_kit_id,
        feedback=SellerFeedbackPayload(
            used=payload.used,
            edited=payload.edited,
            would_reuse=payload.would_reuse,
            would_pay=payload.would_pay,
            notes=payload.notes,
            metrics=payload.metrics,
        ),
    )
    return serialize_launch_kit_detail(launch_kit)


@router.patch("/{launch_kit_id}/manual-edits", response_model=LaunchKitDetailResponse)
def save_launch_kit_manual_edits_endpoint(
    launch_kit_id: str,
    payload: LaunchKitManualEditsRequest,
    session: Session = Depends(get_session),
) -> LaunchKitDetailResponse:
    launch_kit = save_launch_kit_manual_edits(
        session,
        launch_kit_id=launch_kit_id,
        platform_blocks=[item.model_dump(mode="json") for item in payload.platform_blocks],
    )
    return serialize_launch_kit_detail(launch_kit)


@router.get("/{launch_kit_id}/exports/markdown", response_class=PlainTextResponse)
def export_launch_kit_markdown_endpoint(
    launch_kit_id: str,
    session: Session = Depends(get_session),
) -> PlainTextResponse:
    launch_kit = get_launch_kit(session, launch_kit_id)
    markdown = render_launch_kit_markdown(launch_kit)
    filename = launch_kit_export_filename(launch_kit)
    return PlainTextResponse(
        markdown,
        media_type="text/markdown; charset=utf-8",
        headers={"Content-Disposition": f'attachment; filename="{filename}"'},
    )


@router.get("/{launch_kit_id}/status", response_model=LaunchKitStatusResponse)
def get_launch_kit_status_endpoint(
    launch_kit_id: str,
    session: Session = Depends(get_session),
) -> LaunchKitStatusResponse:
    launch_kit = get_launch_kit(session, launch_kit_id)
    latest_task = max(launch_kit.tasks, key=lambda task: task.created_at, default=None)
    return LaunchKitStatusResponse(
        id=launch_kit.id,
        status=launch_kit.status,
        latest_task=serialize_launch_kit_task(latest_task) if latest_task else None,
        updated_at=launch_kit.updated_at,
    )
