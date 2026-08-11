from __future__ import annotations

import json
from collections.abc import Iterator

from fastapi import APIRouter, Depends, Header, Query, status
from fastapi.responses import StreamingResponse
from sqlalchemy.orm import Session

from productflow_backend.application.product_workflows import (
    get_v2_workflow_node_run,
    submit_v2_workflow_node_run,
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
    ConfirmWorkflowDraftRequest,
    CreateWorkflowDraftRequest,
    MaterializeWorkflowDraftRequest,
    SubmitWorkflowNodeRunV2Response,
    WorkflowDraftResponse,
    WorkflowMaterializationResponse,
    WorkflowNodeRunV2Response,
    serialize_active_v2_workflow,
    serialize_materialization,
    serialize_workflow_draft,
    serialize_workflow_node_run_v2,
)

router = APIRouter(prefix="/api/v2", tags=["workflow-drafts"], dependencies=[Depends(require_admin)])


@router.get("/products/{product_id}/workflow", response_model=ActiveProductWorkflowV2Response)
def get_active_v2_workflow_endpoint(
    product_id: str,
    session: Session = Depends(get_session),
) -> ActiveProductWorkflowV2Response:
    return serialize_active_v2_workflow(get_active_v2_workflow_snapshot(session, product_id=product_id))


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
