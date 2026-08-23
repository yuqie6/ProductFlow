from __future__ import annotations

import json

from fastapi import APIRouter, Depends, File, Form, UploadFile, status
from pydantic import ValidationError
from sqlalchemy.orm import Session

from productflow_backend.application.agent.product_intake import AGENT_PRODUCT_IMAGE_TYPE_KEYS
from productflow_backend.application.product_workflow.graph_commands import (
    apply_graph_change_set,
    create_empty_workflow_graph,
    redo_last_graph_change_set,
    undo_last_graph_change_set,
)
from productflow_backend.application.product_workflow.graph_contracts import WorkflowChangeSet
from productflow_backend.application.product_workflow.graph_direct_create import create_product_with_direct_graph
from productflow_backend.application.product_workflow.graph_draft_persist import persist_confirmed_draft_graph
from productflow_backend.application.product_workflow.graph_proposals import (
    confirm_graph_proposal,
    discard_graph_proposal,
)
from productflow_backend.application.product_workflow.graph_queries import (
    get_active_graph_projection,
    get_graph_projection,
)
from productflow_backend.application.product_workflow.graph_runs import (
    cancel_graph_run,
    get_graph_run,
    list_graph_runs,
    retry_graph_run,
    submit_graph_run,
)
from productflow_backend.application.product_workflow.graph_template import (
    DirectCreateImageType,
    resolve_template_generation_spec,
)
from productflow_backend.domain.enums import GraphActorType, GraphRunScope
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.domain.graph_catalog import graph_catalog_document
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.schemas.graphs import (
    DirectCreateImageTypeRequest,
    DirectCreateProductResponse,
    DraftGraphPersistResponse,
    GraphCatalogResponse,
    GraphProjectionResponse,
    GraphRunListResponse,
    GraphRunRequest,
    GraphRunResponse,
    PersistDraftGraphRequest,
    serialize_direct_create,
    serialize_graph_catalog,
    serialize_graph_projection,
    serialize_graph_run,
)
from productflow_backend.presentation.upload_validation import (
    read_validated_image_upload,
    validate_reference_image_count,
)

router = APIRouter(prefix="/api/v3", tags=["workflow-graphs"], dependencies=[Depends(require_admin)])


@router.get("/node-catalog", response_model=GraphCatalogResponse)
def get_node_catalog_endpoint() -> GraphCatalogResponse:
    return serialize_graph_catalog(graph_catalog_document())


@router.post("/products", response_model=DirectCreateProductResponse, status_code=status.HTTP_201_CREATED)
async def create_product_with_direct_graph_endpoint(
    name: str = Form(...),
    images: list[UploadFile] = File(...),
    image_types: str = Form(...),
    category: str | None = Form(default=None),
    price: str | None = Form(default=None),
    source_note: str | None = Form(default=None),
    generation_spec: str | None = Form(default=None),
    session: Session = Depends(get_session),
) -> DirectCreateProductResponse:
    validate_reference_image_count(len(images))
    image_payloads: list[tuple[bytes, str, str]] = []
    for image in images:
        validated = await read_validated_image_upload(image, fallback_filename="reference.bin")
        image_payloads.append((validated.content, validated.filename, validated.mime_type))
    result = create_product_with_direct_graph(
        session,
        name=name,
        category=category,
        price=price,
        source_note=source_note,
        image_uploads=image_payloads,
        image_types=_parse_image_types(image_types),
        generation_spec=_parse_generation_spec(generation_spec),
    )
    return serialize_direct_create(
        product=result.product,
        created_assets=result.created_assets,
        projection=result.projection,
    )


@router.post(
    "/products/{product_id}/workflows",
    response_model=GraphProjectionResponse,
    status_code=status.HTTP_201_CREATED,
)
def create_empty_workflow_graph_endpoint(
    product_id: str,
    session: Session = Depends(get_session),
) -> GraphProjectionResponse:
    graph = create_empty_workflow_graph(session, product_id=product_id)
    return serialize_graph_projection(get_graph_projection(session, product_id=product_id, graph_id=graph.id))


@router.get("/products/{product_id}/workflows/current", response_model=GraphProjectionResponse)
def get_current_workflow_graph_endpoint(
    product_id: str,
    session: Session = Depends(get_session),
) -> GraphProjectionResponse:
    return serialize_graph_projection(get_active_graph_projection(session, product_id=product_id))


@router.get("/products/{product_id}/workflows/{workflow_id}", response_model=GraphProjectionResponse)
def get_workflow_graph_endpoint(
    product_id: str,
    workflow_id: str,
    session: Session = Depends(get_session),
) -> GraphProjectionResponse:
    return serialize_graph_projection(get_graph_projection(session, product_id=product_id, graph_id=workflow_id))


@router.post(
    "/products/{product_id}/workflow-drafts/{draft_id}/graphs",
    response_model=DraftGraphPersistResponse,
)
def persist_confirmed_draft_graph_endpoint(
    product_id: str,
    draft_id: str,
    payload: PersistDraftGraphRequest,
    session: Session = Depends(get_session),
) -> DraftGraphPersistResponse:
    result = persist_confirmed_draft_graph(
        session,
        product_id=product_id,
        draft_id=draft_id,
        expected_draft_version=payload.expected_draft_version,
    )
    return DraftGraphPersistResponse(
        created=result.created,
        graph=serialize_graph_projection(result.projection),
    )


@router.post(
    "/products/{product_id}/workflows/{workflow_id}/changesets",
    response_model=GraphProjectionResponse,
)
def apply_workflow_change_set_endpoint(
    product_id: str,
    workflow_id: str,
    payload: WorkflowChangeSet,
    session: Session = Depends(get_session),
) -> GraphProjectionResponse:
    change_set = WorkflowChangeSet(
        base_graph_revision=payload.base_graph_revision,
        summary=payload.summary,
        actor_type=GraphActorType.USER,
        operations=payload.operations,
    )
    result = apply_graph_change_set(
        session,
        product_id=product_id,
        graph_id=workflow_id,
        change_set=change_set,
    )
    return serialize_graph_projection(
        get_graph_projection(session, product_id=product_id, graph_id=result.graph.id)
    )


@router.post(
    "/products/{product_id}/workflows/{workflow_id}/undo",
    response_model=GraphProjectionResponse,
)
def undo_workflow_change_set_endpoint(
    product_id: str,
    workflow_id: str,
    session: Session = Depends(get_session),
) -> GraphProjectionResponse:
    result = undo_last_graph_change_set(session, product_id=product_id, graph_id=workflow_id)
    return serialize_graph_projection(
        get_graph_projection(session, product_id=product_id, graph_id=result.graph.id)
    )


@router.post(
    "/products/{product_id}/workflows/{workflow_id}/redo",
    response_model=GraphProjectionResponse,
)
def redo_workflow_change_set_endpoint(
    product_id: str,
    workflow_id: str,
    session: Session = Depends(get_session),
) -> GraphProjectionResponse:
    result = redo_last_graph_change_set(session, product_id=product_id, graph_id=workflow_id)
    return serialize_graph_projection(
        get_graph_projection(session, product_id=product_id, graph_id=result.graph.id)
    )


@router.post(
    "/products/{product_id}/workflows/{workflow_id}/proposals/{proposal_id}/confirm",
    response_model=GraphProjectionResponse,
)
def confirm_graph_proposal_endpoint(
    product_id: str,
    workflow_id: str,
    proposal_id: str,
    session: Session = Depends(get_session),
) -> GraphProjectionResponse:
    result = confirm_graph_proposal(
        session,
        product_id=product_id,
        graph_id=workflow_id,
        proposal_id=proposal_id,
    )
    return serialize_graph_projection(
        get_graph_projection(session, product_id=product_id, graph_id=result.id)
    )


@router.post(
    "/products/{product_id}/workflows/{workflow_id}/proposals/{proposal_id}/discard",
    response_model=GraphProjectionResponse,
)
def discard_graph_proposal_endpoint(
    product_id: str,
    workflow_id: str,
    proposal_id: str,
    session: Session = Depends(get_session),
) -> GraphProjectionResponse:
    discard_graph_proposal(
        session,
        product_id=product_id,
        graph_id=workflow_id,
        proposal_id=proposal_id,
    )
    return serialize_graph_projection(
        get_graph_projection(session, product_id=product_id, graph_id=workflow_id)
    )


@router.post(
    "/products/{product_id}/workflows/{workflow_id}/runs",
    response_model=GraphRunResponse,
    status_code=status.HTTP_201_CREATED,
)
def submit_graph_run_endpoint(
    product_id: str,
    workflow_id: str,
    payload: GraphRunRequest,
    session: Session = Depends(get_session),
) -> GraphRunResponse:
    if payload.scope != GraphRunScope.GRAPH and not payload.node_id:
        raise BusinessValidationError("节点运行范围必须指定 node_id")
    submission = submit_graph_run(
        session,
        product_id=product_id,
        graph_id=workflow_id,
        scope=payload.scope,
        target_node_id=payload.node_id,
    )
    return serialize_graph_run(submission.run)


@router.get("/products/{product_id}/workflows/{workflow_id}/runs", response_model=GraphRunListResponse)
def list_graph_runs_endpoint(
    product_id: str,
    workflow_id: str,
    session: Session = Depends(get_session),
) -> GraphRunListResponse:
    runs = list_graph_runs(session, product_id=product_id, graph_id=workflow_id)
    return GraphRunListResponse(items=[serialize_graph_run(run) for run in runs])


@router.get(
    "/products/{product_id}/workflows/{workflow_id}/runs/{run_id}",
    response_model=GraphRunResponse,
)
def get_graph_run_endpoint(
    product_id: str,
    workflow_id: str,
    run_id: str,
    session: Session = Depends(get_session),
) -> GraphRunResponse:
    return serialize_graph_run(
        get_graph_run(session, product_id=product_id, graph_id=workflow_id, run_id=run_id)
    )


@router.post(
    "/products/{product_id}/workflows/{workflow_id}/runs/{run_id}/cancel",
    response_model=GraphRunResponse,
)
def cancel_graph_run_endpoint(
    product_id: str,
    workflow_id: str,
    run_id: str,
    session: Session = Depends(get_session),
) -> GraphRunResponse:
    return serialize_graph_run(
        cancel_graph_run(session, product_id=product_id, graph_id=workflow_id, run_id=run_id)
    )


@router.post(
    "/products/{product_id}/workflows/{workflow_id}/runs/{run_id}/retry",
    response_model=GraphRunResponse,
    status_code=status.HTTP_201_CREATED,
)
def retry_graph_run_endpoint(
    product_id: str,
    workflow_id: str,
    run_id: str,
    session: Session = Depends(get_session),
) -> GraphRunResponse:
    submission = retry_graph_run(
        session,
        product_id=product_id,
        graph_id=workflow_id,
        run_id=run_id,
    )
    return serialize_graph_run(submission.run)


def _parse_generation_spec(raw: str | None) -> dict | None:
    if raw is None or not raw.strip():
        return None
    try:
        payload = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise BusinessValidationError("出图设定必须是 JSON 对象") from exc
    if not isinstance(payload, dict):
        raise BusinessValidationError("出图设定必须是 JSON 对象")
    return resolve_template_generation_spec(payload)


def _parse_image_types(raw: str) -> list[DirectCreateImageType]:
    try:
        payload = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise BusinessValidationError("图片类型必须是 JSON 数组") from exc
    if not isinstance(payload, list):
        raise BusinessValidationError("图片类型必须是 JSON 数组")
    try:
        parsed = [DirectCreateImageTypeRequest.model_validate(item) for item in payload]
    except ValidationError as exc:
        raise BusinessValidationError("图片类型格式无效") from exc
    keys = [item.key for item in parsed]
    unknown = [key for key in keys if key not in AGENT_PRODUCT_IMAGE_TYPE_KEYS]
    if unknown:
        raise BusinessValidationError(f"不支持的图片类型: {', '.join(unknown)}")
    return [
        DirectCreateImageType(
            key=item.key,
            quantity=item.quantity,
            order=index,
            aspect_ratio=item.aspect_ratio,
        )
        for index, item in enumerate(parsed)
    ]
