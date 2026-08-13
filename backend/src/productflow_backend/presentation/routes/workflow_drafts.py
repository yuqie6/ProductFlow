from __future__ import annotations

import json
from collections.abc import Iterator

from fastapi import APIRouter, Depends, Header, Query, status
from fastapi.responses import StreamingResponse
from sqlalchemy.orm import Session

from productflow_backend.application.agent_conversations import mark_agent_conversation_completed_for_draft
from productflow_backend.application.product_workflows import (
    bind_v2_reference_node_asset,
    create_workflow_folder,
    dissolve_workflow_folder,
    get_v2_workflow_node_run,
    rename_workflow_folder,
    set_workflow_folder_members,
    submit_v2_workflow_node_run,
    translate_workflow_folder,
    update_workflow_node_layout,
)
from productflow_backend.application.workflow_drafts.materialization import (
    get_active_v2_workflow_snapshot,
    list_workflow_reveal_events,
    materialize_workflow_draft,
)
from productflow_backend.application.workflow_drafts.service import (
    append_workflow_draft_revision,
    confirm_workflow_draft_revision,
    create_workflow_draft,
    get_workflow_draft_or_raise,
)
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.infrastructure.db.models import WorkflowRevealEvent
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.schemas.workflow_drafts import (
    ActiveProductWorkflowV2Response,
    AppendWorkflowDraftRevisionRequest,
    BindWorkflowReferenceAssetRequest,
    BindWorkflowReferenceAssetResponse,
    ConfirmWorkflowDraftRequest,
    CreateWorkflowDraftRequest,
    CreateWorkflowFolderRequest,
    MaterializeWorkflowDraftRequest,
    RenameWorkflowFolderRequest,
    SetWorkflowFolderMembersRequest,
    SubmitWorkflowNodeRunV2Response,
    TranslateWorkflowFolderRequest,
    UpdateWorkflowNodeLayoutRequest,
    WorkflowCanvasMutationResponse,
    WorkflowDraftResponse,
    WorkflowMaterializationResponse,
    WorkflowNodeRunV2Response,
    serialize_active_v2_workflow,
    serialize_canvas_mutation,
    serialize_materialization,
    serialize_reference_binding,
    serialize_workflow_draft,
    serialize_workflow_node_run_v2,
    to_workflow_node_positions,
)

router = APIRouter(prefix="/api/v2", tags=["workflow-drafts"], dependencies=[Depends(require_admin)])


@router.get("/products/{product_id}/workflow", response_model=ActiveProductWorkflowV2Response)
def get_active_v2_workflow_endpoint(
    product_id: str,
    session: Session = Depends(get_session),
) -> ActiveProductWorkflowV2Response:
    return serialize_active_v2_workflow(get_active_v2_workflow_snapshot(session, product_id=product_id))


@router.patch(
    "/products/{product_id}/workflows/{workflow_id}/reference-nodes/{node_id}",
    response_model=BindWorkflowReferenceAssetResponse,
)
def bind_v2_reference_node_asset_endpoint(
    product_id: str,
    workflow_id: str,
    node_id: str,
    payload: BindWorkflowReferenceAssetRequest,
    session: Session = Depends(get_session),
) -> BindWorkflowReferenceAssetResponse:
    result = bind_v2_reference_node_asset(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        node_id=node_id,
        asset_id=payload.asset_id,
        expected_workflow_revision=payload.expected_workflow_revision,
        expected_bound_asset_id=payload.expected_bound_asset_id,
    )
    return serialize_reference_binding(result)


@router.post(
    "/products/{product_id}/workflows/{workflow_id}/folders",
    response_model=WorkflowCanvasMutationResponse,
    status_code=status.HTTP_201_CREATED,
)
def create_workflow_folder_endpoint(
    product_id: str,
    workflow_id: str,
    payload: CreateWorkflowFolderRequest,
    session: Session = Depends(get_session),
) -> WorkflowCanvasMutationResponse:
    return serialize_canvas_mutation(
        create_workflow_folder(
            session,
            product_id=product_id,
            workflow_id=workflow_id,
            title=payload.title,
            node_ids=payload.node_ids,
            expected_edit_version=payload.expected_edit_version,
        )
    )


@router.patch(
    "/products/{product_id}/workflows/{workflow_id}/folders/{folder_id}",
    response_model=WorkflowCanvasMutationResponse,
)
def rename_workflow_folder_endpoint(
    product_id: str,
    workflow_id: str,
    folder_id: str,
    payload: RenameWorkflowFolderRequest,
    session: Session = Depends(get_session),
) -> WorkflowCanvasMutationResponse:
    return serialize_canvas_mutation(
        rename_workflow_folder(
            session,
            product_id=product_id,
            workflow_id=workflow_id,
            folder_id=folder_id,
            title=payload.title,
            expected_edit_version=payload.expected_edit_version,
        )
    )


@router.put(
    "/products/{product_id}/workflows/{workflow_id}/folders/{folder_id}/members",
    response_model=WorkflowCanvasMutationResponse,
)
def set_workflow_folder_members_endpoint(
    product_id: str,
    workflow_id: str,
    folder_id: str,
    payload: SetWorkflowFolderMembersRequest,
    session: Session = Depends(get_session),
) -> WorkflowCanvasMutationResponse:
    return serialize_canvas_mutation(
        set_workflow_folder_members(
            session,
            product_id=product_id,
            workflow_id=workflow_id,
            folder_id=folder_id,
            node_ids=payload.node_ids,
            expected_edit_version=payload.expected_edit_version,
        )
    )


@router.delete(
    "/products/{product_id}/workflows/{workflow_id}/folders/{folder_id}",
    response_model=WorkflowCanvasMutationResponse,
)
def dissolve_workflow_folder_endpoint(
    product_id: str,
    workflow_id: str,
    folder_id: str,
    expected_edit_version: int = Query(ge=0),
    session: Session = Depends(get_session),
) -> WorkflowCanvasMutationResponse:
    return serialize_canvas_mutation(
        dissolve_workflow_folder(
            session,
            product_id=product_id,
            workflow_id=workflow_id,
            folder_id=folder_id,
            expected_edit_version=expected_edit_version,
        )
    )


@router.post(
    "/products/{product_id}/workflows/{workflow_id}/folders/{folder_id}/translate",
    response_model=WorkflowCanvasMutationResponse,
)
def translate_workflow_folder_endpoint(
    product_id: str,
    workflow_id: str,
    folder_id: str,
    payload: TranslateWorkflowFolderRequest,
    session: Session = Depends(get_session),
) -> WorkflowCanvasMutationResponse:
    return serialize_canvas_mutation(
        translate_workflow_folder(
            session,
            product_id=product_id,
            workflow_id=workflow_id,
            folder_id=folder_id,
            delta_x=payload.delta_x,
            delta_y=payload.delta_y,
            expected_edit_version=payload.expected_edit_version,
        )
    )


@router.patch(
    "/products/{product_id}/workflows/{workflow_id}/layout",
    response_model=WorkflowCanvasMutationResponse,
)
def update_workflow_node_layout_endpoint(
    product_id: str,
    workflow_id: str,
    payload: UpdateWorkflowNodeLayoutRequest,
    session: Session = Depends(get_session),
) -> WorkflowCanvasMutationResponse:
    return serialize_canvas_mutation(
        update_workflow_node_layout(
            session,
            product_id=product_id,
            workflow_id=workflow_id,
            positions=to_workflow_node_positions(payload.positions),
            expected_edit_version=payload.expected_edit_version,
        )
    )


@router.post(
    "/workflow-nodes/{node_id}/run",
    response_model=SubmitWorkflowNodeRunV2Response,
    status_code=status.HTTP_202_ACCEPTED,
)
def submit_v2_workflow_node_run_endpoint(
    node_id: str,
    session: Session = Depends(get_session),
) -> SubmitWorkflowNodeRunV2Response:
    submission = submit_v2_workflow_node_run(session, node_id=node_id)
    return SubmitWorkflowNodeRunV2Response(
        created=submission.created,
        node_run=serialize_workflow_node_run_v2(submission.node_run),
    )


@router.get(
    "/workflow-node-runs/{node_run_id}",
    response_model=WorkflowNodeRunV2Response,
)
def get_v2_workflow_node_run_endpoint(
    node_run_id: str,
    session: Session = Depends(get_session),
) -> WorkflowNodeRunV2Response:
    return serialize_workflow_node_run_v2(
        get_v2_workflow_node_run(session, node_run_id=node_run_id)
    )


@router.post(
    "/products/{product_id}/workflow-drafts",
    response_model=WorkflowDraftResponse,
    status_code=status.HTTP_201_CREATED,
)
def create_workflow_draft_endpoint(
    product_id: str,
    payload: CreateWorkflowDraftRequest,
    session: Session = Depends(get_session),
) -> WorkflowDraftResponse:
    draft = create_workflow_draft(
        session,
        product_id=product_id,
        payload=payload.payload,
        ready_for_confirmation=payload.ready_for_confirmation,
        source_turn_id=payload.source_turn_id,
        source_artifact_step_id=payload.source_artifact_step_id,
    )
    return serialize_workflow_draft(draft)


@router.get(
    "/products/{product_id}/workflow-drafts/{draft_id}",
    response_model=WorkflowDraftResponse,
)
def get_workflow_draft_endpoint(
    product_id: str,
    draft_id: str,
    session: Session = Depends(get_session),
) -> WorkflowDraftResponse:
    return serialize_workflow_draft(
        get_workflow_draft_or_raise(session, product_id=product_id, draft_id=draft_id)
    )


@router.post(
    "/products/{product_id}/workflow-drafts/{draft_id}/revisions",
    response_model=WorkflowDraftResponse,
    status_code=status.HTTP_201_CREATED,
)
def append_workflow_draft_revision_endpoint(
    product_id: str,
    draft_id: str,
    payload: AppendWorkflowDraftRevisionRequest,
    session: Session = Depends(get_session),
) -> WorkflowDraftResponse:
    draft = append_workflow_draft_revision(
        session,
        product_id=product_id,
        draft_id=draft_id,
        expected_draft_version=payload.expected_draft_version,
        payload=payload.payload,
        ready_for_confirmation=payload.ready_for_confirmation,
        source_turn_id=payload.source_turn_id,
        source_artifact_step_id=payload.source_artifact_step_id,
    )
    return serialize_workflow_draft(draft)


@router.post(
    "/products/{product_id}/workflow-drafts/{draft_id}/confirm",
    response_model=WorkflowDraftResponse,
)
def confirm_workflow_draft_endpoint(
    product_id: str,
    draft_id: str,
    payload: ConfirmWorkflowDraftRequest,
    session: Session = Depends(get_session),
) -> WorkflowDraftResponse:
    draft = confirm_workflow_draft_revision(
        session,
        product_id=product_id,
        draft_id=draft_id,
        expected_draft_version=payload.expected_draft_version,
    )
    mark_agent_conversation_completed_for_draft(
        session,
        product_id=product_id,
        workflow_draft_id=draft_id,
    )
    return serialize_workflow_draft(draft)


@router.post(
    "/products/{product_id}/workflow-drafts/{draft_id}/materialize",
    response_model=WorkflowMaterializationResponse,
)
def materialize_workflow_draft_endpoint(
    product_id: str,
    draft_id: str,
    payload: MaterializeWorkflowDraftRequest,
    session: Session = Depends(get_session),
) -> WorkflowMaterializationResponse:
    result = materialize_workflow_draft(
        session,
        product_id=product_id,
        draft_id=draft_id,
        expected_draft_version=payload.expected_draft_version,
        expected_workflow_revision=payload.expected_workflow_revision,
        idempotency_key=payload.idempotency_key,
    )
    return serialize_materialization(result)


@router.get("/workflow-materializations/{materialization_id}/reveal-events")
def stream_workflow_reveal_events_endpoint(
    materialization_id: str,
    after: int = Query(default=0, ge=0),
    last_event_id: str | None = Header(default=None, alias="Last-Event-ID"),
    session: Session = Depends(get_session),
) -> StreamingResponse:
    cursor = max(after, _parse_last_event_id(last_event_id))
    events = list_workflow_reveal_events(
        session,
        materialization_id=materialization_id,
        after=cursor,
    )
    chunks = tuple(_serialize_reveal_event(event) for event in events)
    return StreamingResponse(
        _iter_sse_chunks(chunks),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "X-Accel-Buffering": "no",
        },
    )


def _parse_last_event_id(value: str | None) -> int:
    if value is None or not value.strip():
        return 0
    try:
        cursor = int(value)
    except ValueError as exc:
        raise BusinessValidationError("Last-Event-ID 必须是非负整数") from exc
    if cursor < 0:
        raise BusinessValidationError("Last-Event-ID 必须是非负整数")
    return cursor


def _serialize_reveal_event(event: WorkflowRevealEvent) -> str:
    payload = {
        "schema_version": 1,
        "materialization_id": event.materialization_id,
        "sequence": event.sequence,
        "kind": event.kind.value,
        "entity_type": event.entity_type,
        "entity_id": event.entity_id,
        "payload": event.payload_json,
        "created_at": event.created_at.isoformat(),
    }
    data = json.dumps(payload, ensure_ascii=False, separators=(",", ":"))
    return f"id: {event.sequence}\nevent: {event.kind.value}\ndata: {data}\n\n"


def _iter_sse_chunks(chunks: tuple[str, ...]) -> Iterator[str]:
    yield from chunks


__all__ = ["router"]
