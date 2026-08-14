from __future__ import annotations

import json
from collections.abc import Iterator

from fastapi import APIRouter, Depends, Header, Query, status
from fastapi.responses import StreamingResponse
from sqlalchemy.orm import Session

from productflow_backend.application.agent_conversations import mark_agent_conversation_completed_for_draft
from productflow_backend.application.product_workflows import (
    bind_v2_reference_node_asset,
    cancel_v2_workflow_node_run,
    cancel_v2_workflow_run,
    create_v2_reference_node,
    create_v2_workflow_edge,
    create_workflow_folder,
    delete_v2_workflow_edge,
    delete_v2_workflow_node,
    dissolve_workflow_folder,
    duplicate_v2_workflow_node,
    get_v2_workflow_node_detail,
    get_v2_workflow_node_run,
    get_v2_workflow_run,
    list_v2_workflow_node_runs,
    list_v2_workflow_runs,
    rename_workflow_folder,
    retry_v2_workflow_run,
    set_workflow_folder_members,
    submit_v2_workflow_node_run,
    submit_v2_workflow_run,
    translate_workflow_folder,
    update_v2_image_node,
    update_v2_prompt_node,
    update_v2_reference_node,
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
    CreateReferenceWorkflowNodeV2Request,
    CreateWorkflowDraftRequest,
    CreateWorkflowEdgeV2Request,
    CreateWorkflowFolderRequest,
    DuplicateWorkflowNodeV2Request,
    MaterializeWorkflowDraftRequest,
    RenameWorkflowFolderRequest,
    SetWorkflowFolderMembersRequest,
    SubmitWorkflowNodeRunV2Response,
    SubmitWorkflowRunV2Response,
    TranslateWorkflowFolderRequest,
    UpdateImageWorkflowNodeV2Request,
    UpdatePromptWorkflowNodeV2Request,
    UpdateReferenceWorkflowNodeV2Request,
    UpdateWorkflowNodeLayoutRequest,
    UpdateWorkflowNodeV2Request,
    WorkflowCanvasMutationResponse,
    WorkflowDraftResponse,
    WorkflowMaterializationResponse,
    WorkflowNodeDetailV2Response,
    WorkflowNodeRunListV2Response,
    WorkflowNodeRunV2Response,
    WorkflowRunDetailV2Response,
    WorkflowRunListV2Response,
    serialize_active_v2_workflow,
    serialize_canvas_mutation,
    serialize_materialization,
    serialize_product_workflow_v2,
    serialize_reference_binding,
    serialize_workflow_draft,
    serialize_workflow_node_detail_v2,
    serialize_workflow_node_run_v2,
    serialize_workflow_run_v2,
    to_workflow_node_positions,
)

router = APIRouter(prefix="/api/v2", tags=["workflow-drafts"], dependencies=[Depends(require_admin)])


@router.get("/products/{product_id}/workflow", response_model=ActiveProductWorkflowV2Response)
def get_active_v2_workflow_endpoint(
    product_id: str,
    session: Session = Depends(get_session),
) -> ActiveProductWorkflowV2Response:
    return serialize_active_v2_workflow(get_active_v2_workflow_snapshot(session, product_id=product_id))


@router.get(
    "/products/{product_id}/workflows/{workflow_id}/nodes/{node_id}",
    response_model=WorkflowNodeDetailV2Response,
)
def get_v2_workflow_node_detail_endpoint(
    product_id: str,
    workflow_id: str,
    node_id: str,
    session: Session = Depends(get_session),
) -> WorkflowNodeDetailV2Response:
    return serialize_workflow_node_detail_v2(
        get_v2_workflow_node_detail(
            session,
            product_id=product_id,
            workflow_id=workflow_id,
            node_id=node_id,
        )
    )


@router.patch(
    "/products/{product_id}/workflows/{workflow_id}/nodes/{node_id}",
    response_model=WorkflowCanvasMutationResponse,
)
def update_v2_workflow_node_endpoint(
    product_id: str,
    workflow_id: str,
    node_id: str,
    payload: UpdateWorkflowNodeV2Request,
    session: Session = Depends(get_session),
) -> WorkflowCanvasMutationResponse:
    if isinstance(payload, UpdateReferenceWorkflowNodeV2Request):
        result = update_v2_reference_node(
            session,
            product_id=product_id,
            workflow_id=workflow_id,
            node_id=node_id,
            expected_edit_version=payload.expected_edit_version,
            title=payload.title,
            role=payload.role,
            label=payload.label,
        )
    elif isinstance(payload, UpdatePromptWorkflowNodeV2Request):
        result = update_v2_prompt_node(
            session,
            product_id=product_id,
            workflow_id=workflow_id,
            node_id=node_id,
            expected_edit_version=payload.expected_edit_version,
            expected_prompt_artifact_version_id=payload.expected_prompt_artifact_version_id,
            title=payload.title,
            payload=payload.prompt_payload,
        )
    elif isinstance(payload, UpdateImageWorkflowNodeV2Request):
        result = update_v2_image_node(
            session,
            product_id=product_id,
            workflow_id=workflow_id,
            node_id=node_id,
            expected_edit_version=payload.expected_edit_version,
            title=payload.title,
            variation_instruction=payload.variation_instruction,
            generation_spec=payload.generation_spec,
            delivery_spec=payload.delivery_spec,
        )
    else:
        raise BusinessValidationError("不支持的 schema-v2 节点编辑请求")
    return serialize_canvas_mutation(result)


@router.post(
    "/products/{product_id}/workflows/{workflow_id}/reference-nodes",
    response_model=WorkflowCanvasMutationResponse,
    status_code=status.HTTP_201_CREATED,
)
def create_v2_reference_node_endpoint(
    product_id: str,
    workflow_id: str,
    payload: CreateReferenceWorkflowNodeV2Request,
    session: Session = Depends(get_session),
) -> WorkflowCanvasMutationResponse:
    return serialize_canvas_mutation(
        create_v2_reference_node(
            session,
            product_id=product_id,
            workflow_id=workflow_id,
            expected_edit_version=payload.expected_edit_version,
            title=payload.title,
            role=payload.role,
            label=payload.label,
            position_x=payload.position_x,
            position_y=payload.position_y,
            folder_id=payload.folder_id,
        )
    )


@router.post(
    "/products/{product_id}/workflows/{workflow_id}/nodes/{node_id}/duplicate",
    response_model=WorkflowCanvasMutationResponse,
    status_code=status.HTTP_201_CREATED,
)
def duplicate_v2_workflow_node_endpoint(
    product_id: str,
    workflow_id: str,
    node_id: str,
    payload: DuplicateWorkflowNodeV2Request,
    session: Session = Depends(get_session),
) -> WorkflowCanvasMutationResponse:
    return serialize_canvas_mutation(
        duplicate_v2_workflow_node(
            session,
            product_id=product_id,
            workflow_id=workflow_id,
            node_id=node_id,
            expected_edit_version=payload.expected_edit_version,
        )
    )


@router.delete(
    "/products/{product_id}/workflows/{workflow_id}/nodes/{node_id}",
    response_model=WorkflowCanvasMutationResponse,
)
def delete_v2_workflow_node_endpoint(
    product_id: str,
    workflow_id: str,
    node_id: str,
    expected_edit_version: int = Query(ge=0),
    session: Session = Depends(get_session),
) -> WorkflowCanvasMutationResponse:
    return serialize_canvas_mutation(
        delete_v2_workflow_node(
            session,
            product_id=product_id,
            workflow_id=workflow_id,
            node_id=node_id,
            expected_edit_version=expected_edit_version,
        )
    )


@router.post(
    "/products/{product_id}/workflows/{workflow_id}/edges",
    response_model=WorkflowCanvasMutationResponse,
    status_code=status.HTTP_201_CREATED,
)
def create_v2_workflow_edge_endpoint(
    product_id: str,
    workflow_id: str,
    payload: CreateWorkflowEdgeV2Request,
    session: Session = Depends(get_session),
) -> WorkflowCanvasMutationResponse:
    return serialize_canvas_mutation(
        create_v2_workflow_edge(
            session,
            product_id=product_id,
            workflow_id=workflow_id,
            source_node_id=payload.source_node_id,
            target_node_id=payload.target_node_id,
            expected_edit_version=payload.expected_edit_version,
        )
    )


@router.delete(
    "/products/{product_id}/workflows/{workflow_id}/edges/{edge_id}",
    response_model=WorkflowCanvasMutationResponse,
)
def delete_v2_workflow_edge_endpoint(
    product_id: str,
    workflow_id: str,
    edge_id: str,
    expected_edit_version: int = Query(ge=0),
    session: Session = Depends(get_session),
) -> WorkflowCanvasMutationResponse:
    return serialize_canvas_mutation(
        delete_v2_workflow_edge(
            session,
            product_id=product_id,
            workflow_id=workflow_id,
            edge_id=edge_id,
            expected_edit_version=expected_edit_version,
        )
    )


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
    "/products/{product_id}/workflows/{workflow_id}/runs",
    response_model=SubmitWorkflowRunV2Response,
    status_code=status.HTTP_202_ACCEPTED,
)
def submit_v2_workflow_run_endpoint(
    product_id: str,
    workflow_id: str,
    session: Session = Depends(get_session),
) -> SubmitWorkflowRunV2Response:
    submission = submit_v2_workflow_run(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
    )
    return SubmitWorkflowRunV2Response(
        created=submission.created,
        workflow_run=serialize_workflow_run_v2(submission.run),
        workflow=serialize_product_workflow_v2(submission.run.workflow),
    )


@router.get(
    "/products/{product_id}/workflows/{workflow_id}/runs",
    response_model=WorkflowRunListV2Response,
)
def list_v2_workflow_runs_endpoint(
    product_id: str,
    workflow_id: str,
    limit: int = Query(default=20, ge=1, le=50),
    session: Session = Depends(get_session),
) -> WorkflowRunListV2Response:
    result = list_v2_workflow_runs(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        limit=limit,
    )
    return WorkflowRunListV2Response(
        items=[serialize_workflow_run_v2(run) for run in result.runs],
        workflow=serialize_product_workflow_v2(result.workflow),
    )


@router.get(
    "/products/{product_id}/workflows/{workflow_id}/runs/{run_id}",
    response_model=WorkflowRunDetailV2Response,
)
def get_v2_workflow_run_endpoint(
    product_id: str,
    workflow_id: str,
    run_id: str,
    session: Session = Depends(get_session),
) -> WorkflowRunDetailV2Response:
    run = get_v2_workflow_run(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        run_id=run_id,
    )
    return WorkflowRunDetailV2Response(
        workflow_run=serialize_workflow_run_v2(run),
        workflow=serialize_product_workflow_v2(run.workflow),
    )


@router.post(
    "/products/{product_id}/workflows/{workflow_id}/runs/{run_id}/cancel",
    response_model=WorkflowRunDetailV2Response,
)
def cancel_v2_workflow_run_endpoint(
    product_id: str,
    workflow_id: str,
    run_id: str,
    session: Session = Depends(get_session),
) -> WorkflowRunDetailV2Response:
    run = cancel_v2_workflow_run(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        run_id=run_id,
    )
    return WorkflowRunDetailV2Response(
        workflow_run=serialize_workflow_run_v2(run),
        workflow=serialize_product_workflow_v2(run.workflow),
    )


@router.post(
    "/products/{product_id}/workflows/{workflow_id}/runs/{run_id}/retry",
    response_model=SubmitWorkflowRunV2Response,
    status_code=status.HTTP_202_ACCEPTED,
)
def retry_v2_workflow_run_endpoint(
    product_id: str,
    workflow_id: str,
    run_id: str,
    session: Session = Depends(get_session),
) -> SubmitWorkflowRunV2Response:
    submission = retry_v2_workflow_run(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        run_id=run_id,
    )
    return SubmitWorkflowRunV2Response(
        created=submission.created,
        workflow_run=serialize_workflow_run_v2(submission.run),
        workflow=serialize_product_workflow_v2(submission.run.workflow),
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


@router.get(
    "/workflow-nodes/{node_id}/runs",
    response_model=WorkflowNodeRunListV2Response,
)
def list_v2_workflow_node_runs_endpoint(
    node_id: str,
    limit: int = Query(default=20, ge=1, le=50),
    session: Session = Depends(get_session),
) -> WorkflowNodeRunListV2Response:
    return WorkflowNodeRunListV2Response(
        items=[
            serialize_workflow_node_run_v2(node_run)
            for node_run in list_v2_workflow_node_runs(session, node_id=node_id, limit=limit)
        ]
    )


@router.post(
    "/workflow-node-runs/{node_run_id}/cancel",
    response_model=WorkflowNodeRunV2Response,
)
def cancel_v2_workflow_node_run_endpoint(
    node_run_id: str,
    session: Session = Depends(get_session),
) -> WorkflowNodeRunV2Response:
    return serialize_workflow_node_run_v2(
        cancel_v2_workflow_node_run(session, node_run_id=node_run_id)
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
