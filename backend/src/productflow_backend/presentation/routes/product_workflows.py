from __future__ import annotations

from fastapi import APIRouter, Depends, File, Form, HTTPException, UploadFile, status
from sqlalchemy.orm import Session

from productflow_backend.application.product_workflows import (
    create_workflow_edge,
    create_workflow_node,
    delete_workflow_edge,
    delete_workflow_node,
    get_or_create_product_workflow,
    mark_workflow_run_enqueue_failed,
    start_product_workflow_run,
    update_workflow_copy_set,
    update_workflow_node,
    upload_workflow_node_image,
)
from productflow_backend.infrastructure.queue import enqueue_workflow_run
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.schemas.product_workflows import (
    CreateWorkflowEdgeRequest,
    CreateWorkflowNodeRequest,
    ProductWorkflowResponse,
    RunWorkflowRequest,
    UpdateWorkflowCopySetRequest,
    UpdateWorkflowNodeRequest,
    serialize_product_workflow,
)
from productflow_backend.presentation.upload_validation import read_validated_image_upload

router = APIRouter(prefix="/api", tags=["product-workflows"], dependencies=[Depends(require_admin)])


def _raise_http_error(exc: ValueError) -> None:
    detail = str(exc)
    if detail.endswith("不存在"):
        raise HTTPException(status_code=404, detail=detail) from exc
    raise HTTPException(status_code=400, detail=detail) from exc


@router.get("/products/{product_id}/workflow", response_model=ProductWorkflowResponse)
def get_product_workflow_endpoint(product_id: str, session: Session = Depends(get_session)) -> ProductWorkflowResponse:
    try:
        workflow = get_or_create_product_workflow(session, product_id)
    except ValueError as exc:
        _raise_http_error(exc)
    return serialize_product_workflow(workflow)


@router.post(
    "/products/{product_id}/workflow/nodes",
    response_model=ProductWorkflowResponse,
    status_code=status.HTTP_201_CREATED,
)
def create_workflow_node_endpoint(
    product_id: str,
    payload: CreateWorkflowNodeRequest,
    session: Session = Depends(get_session),
) -> ProductWorkflowResponse:
    try:
        workflow = create_workflow_node(
            session,
            product_id=product_id,
            node_type=payload.node_type,
            title=payload.title,
            position_x=payload.position_x,
            position_y=payload.position_y,
            config_json=payload.config_json,
        )
    except ValueError as exc:
        _raise_http_error(exc)
    return serialize_product_workflow(workflow)


@router.patch("/workflow-nodes/{node_id}", response_model=ProductWorkflowResponse)
def update_workflow_node_endpoint(
    node_id: str,
    payload: UpdateWorkflowNodeRequest,
    session: Session = Depends(get_session),
) -> ProductWorkflowResponse:
    try:
        workflow = update_workflow_node(
            session,
            node_id=node_id,
            title=payload.title,
            position_x=payload.position_x,
            position_y=payload.position_y,
            config_json=payload.config_json,
        )
    except ValueError as exc:
        _raise_http_error(exc)
    return serialize_product_workflow(workflow)


@router.patch("/workflow-nodes/{node_id}/copy", response_model=ProductWorkflowResponse)
def update_workflow_copy_set_endpoint(
    node_id: str,
    payload: UpdateWorkflowCopySetRequest,
    session: Session = Depends(get_session),
) -> ProductWorkflowResponse:
    try:
        workflow = update_workflow_copy_set(
            session,
            node_id=node_id,
            title=payload.title,
            selling_points=payload.selling_points,
            poster_headline=payload.poster_headline,
            cta=payload.cta,
        )
    except ValueError as exc:
        _raise_http_error(exc)
    return serialize_product_workflow(workflow)


@router.post("/workflow-nodes/{node_id}/image", response_model=ProductWorkflowResponse)
async def upload_workflow_node_image_endpoint(
    node_id: str,
    image: UploadFile = File(...),
    role: str | None = Form(default=None),
    label: str | None = Form(default=None),
    session: Session = Depends(get_session),
) -> ProductWorkflowResponse:
    validated = await read_validated_image_upload(image, fallback_filename="workflow-image.bin")
    try:
        workflow = upload_workflow_node_image(
            session,
            node_id=node_id,
            image_bytes=validated.content,
            filename=validated.filename,
            content_type=validated.mime_type,
            role=role,
            label=label,
        )
    except ValueError as exc:
        _raise_http_error(exc)
    return serialize_product_workflow(workflow)


@router.post(
    "/products/{product_id}/workflow/edges",
    response_model=ProductWorkflowResponse,
    status_code=status.HTTP_201_CREATED,
)
def create_workflow_edge_endpoint(
    product_id: str,
    payload: CreateWorkflowEdgeRequest,
    session: Session = Depends(get_session),
) -> ProductWorkflowResponse:
    try:
        workflow = create_workflow_edge(
            session,
            product_id=product_id,
            source_node_id=payload.source_node_id,
            target_node_id=payload.target_node_id,
            source_handle=payload.source_handle,
            target_handle=payload.target_handle,
        )
    except ValueError as exc:
        _raise_http_error(exc)
    return serialize_product_workflow(workflow)


@router.delete("/workflow-edges/{edge_id}", response_model=ProductWorkflowResponse)
def delete_workflow_edge_endpoint(edge_id: str, session: Session = Depends(get_session)) -> ProductWorkflowResponse:
    try:
        workflow = delete_workflow_edge(session, edge_id=edge_id)
    except ValueError as exc:
        _raise_http_error(exc)
    return serialize_product_workflow(workflow)


@router.delete("/workflow-nodes/{node_id}", response_model=ProductWorkflowResponse)
def delete_workflow_node_endpoint(node_id: str, session: Session = Depends(get_session)) -> ProductWorkflowResponse:
    try:
        workflow = delete_workflow_node(session, node_id=node_id)
    except ValueError as exc:
        _raise_http_error(exc)
    return serialize_product_workflow(workflow)


@router.post("/products/{product_id}/workflow/run", response_model=ProductWorkflowResponse)
def run_product_workflow_endpoint(
    product_id: str,
    payload: RunWorkflowRequest | None = None,
    session: Session = Depends(get_session),
) -> ProductWorkflowResponse:
    try:
        kickoff = start_product_workflow_run(
            session,
            product_id=product_id,
            start_node_id=payload.start_node_id if payload else None,
        )
    except ValueError as exc:
        _raise_http_error(exc)
    response = serialize_product_workflow(kickoff.workflow)
    if kickoff.created:
        try:
            enqueue_workflow_run(kickoff.run_id)
        except Exception as exc:
            mark_workflow_run_enqueue_failed(
                session,
                run_id=kickoff.run_id,
                reason="任务队列暂不可用，请稍后重试",
            )
            raise HTTPException(status_code=503, detail="任务队列暂不可用，请稍后重试") from exc
    return response
