from __future__ import annotations

from fastapi import APIRouter, Depends, Query, status
from sqlalchemy.orm import Session

from productflow_backend.application.product_workflow.graph_queries import get_graph_projection
from productflow_backend.application.workflow_recipes.service import (
    append_workflow_recipe_version,
    apply_workflow_recipe,
    archive_workflow_recipe,
    create_workflow_recipe,
    get_workflow_recipe_or_raise,
    list_workflow_recipes,
    preview_workflow_recipe,
)
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.schemas.graphs import serialize_graph_projection
from productflow_backend.presentation.schemas.workflow_recipes import (
    AppendWorkflowRecipeVersionRequest,
    ApplyWorkflowRecipeRequest,
    CreateWorkflowRecipeRequest,
    PreviewWorkflowRecipeRequest,
    WorkflowRecipeApplicationResponse,
    WorkflowRecipeArchiveResponse,
    WorkflowRecipePreviewResponse,
    WorkflowRecipeResponse,
    WorkflowRecipeSummaryResponse,
    serialize_workflow_recipe,
    serialize_workflow_recipe_application,
    serialize_workflow_recipe_archive,
    serialize_workflow_recipe_preview,
    serialize_workflow_recipe_summary,
)

router = APIRouter(prefix="/api/v2", tags=["workflow-recipes"], dependencies=[Depends(require_admin)])
v3_router = APIRouter(prefix="/api/v3", tags=["workflow-recipes"], dependencies=[Depends(require_admin)])


@router.get("/workflow-recipes", response_model=list[WorkflowRecipeSummaryResponse])
@v3_router.get("/workflow-recipes", response_model=list[WorkflowRecipeSummaryResponse])
def list_workflow_recipes_endpoint(
    include_archived: bool = Query(default=False),
    session: Session = Depends(get_session),
) -> list[WorkflowRecipeSummaryResponse]:
    return [
        serialize_workflow_recipe_summary(recipe)
        for recipe in list_workflow_recipes(
            session,
            include_archived=include_archived,
        )
    ]


@router.get("/workflow-recipes/{recipe_id}", response_model=WorkflowRecipeResponse)
@v3_router.get("/workflow-recipes/{recipe_id}", response_model=WorkflowRecipeResponse)
def get_workflow_recipe_endpoint(
    recipe_id: str,
    session: Session = Depends(get_session),
) -> WorkflowRecipeResponse:
    return serialize_workflow_recipe(get_workflow_recipe_or_raise(session, recipe_id=recipe_id))


def _create_recipe(
    *,
    product_id: str,
    workflow_id: str,
    payload: CreateWorkflowRecipeRequest,
    session: Session,
) -> WorkflowRecipeResponse:
    return serialize_workflow_recipe(
        create_workflow_recipe(
            session,
            product_id=product_id,
            workflow_id=workflow_id,
            source_type=payload.source_type,
            group_id=payload.group_id,
            node_ids=payload.node_ids,
            expected_graph_revision=payload.expected_graph_revision,
            title=payload.title,
            description=payload.description,
            preferred_visual_system_version_id=payload.preferred_visual_system_version_id,
        )
    )


def _append_recipe(
    *,
    product_id: str,
    workflow_id: str,
    recipe_id: str,
    payload: AppendWorkflowRecipeVersionRequest,
    session: Session,
) -> WorkflowRecipeResponse:
    return serialize_workflow_recipe(
        append_workflow_recipe_version(
            session,
            recipe_id=recipe_id,
            expected_recipe_version=payload.expected_recipe_version,
            product_id=product_id,
            workflow_id=workflow_id,
            source_type=payload.source_type,
            group_id=payload.group_id,
            node_ids=payload.node_ids,
            expected_graph_revision=payload.expected_graph_revision,
            title=payload.title,
            description=payload.description,
            preferred_visual_system_version_id=payload.preferred_visual_system_version_id,
        )
    )


@router.post(
    "/products/{product_id}/workflows/{workflow_id}/recipes",
    response_model=WorkflowRecipeResponse,
    status_code=status.HTTP_201_CREATED,
)
@v3_router.post(
    "/products/{product_id}/workflows/{workflow_id}/recipes",
    response_model=WorkflowRecipeResponse,
    status_code=status.HTTP_201_CREATED,
)
def create_workflow_recipe_endpoint(
    product_id: str,
    workflow_id: str,
    payload: CreateWorkflowRecipeRequest,
    session: Session = Depends(get_session),
) -> WorkflowRecipeResponse:
    return _create_recipe(
        product_id=product_id,
        workflow_id=workflow_id,
        payload=payload,
        session=session,
    )


@router.post(
    "/products/{product_id}/workflows/{workflow_id}/recipes/{recipe_id}/versions",
    response_model=WorkflowRecipeResponse,
    status_code=status.HTTP_201_CREATED,
)
@v3_router.post(
    "/products/{product_id}/workflows/{workflow_id}/recipes/{recipe_id}/versions",
    response_model=WorkflowRecipeResponse,
    status_code=status.HTTP_201_CREATED,
)
def append_workflow_recipe_version_endpoint(
    product_id: str,
    workflow_id: str,
    recipe_id: str,
    payload: AppendWorkflowRecipeVersionRequest,
    session: Session = Depends(get_session),
) -> WorkflowRecipeResponse:
    return _append_recipe(
        product_id=product_id,
        workflow_id=workflow_id,
        recipe_id=recipe_id,
        payload=payload,
        session=session,
    )


@router.post(
    "/products/{product_id}/workflow-recipes/{recipe_id}/preview",
    response_model=WorkflowRecipePreviewResponse,
)
@v3_router.post(
    "/products/{product_id}/workflow-recipes/{recipe_id}/preview",
    response_model=WorkflowRecipePreviewResponse,
)
def preview_workflow_recipe_endpoint(
    product_id: str,
    recipe_id: str,
    payload: PreviewWorkflowRecipeRequest,
    session: Session = Depends(get_session),
) -> WorkflowRecipePreviewResponse:
    return serialize_workflow_recipe_preview(
        preview_workflow_recipe(
            session,
            product_id=product_id,
            recipe_id=recipe_id,
            expected_recipe_version=payload.expected_recipe_version,
        )
    )


@router.post(
    "/products/{product_id}/workflow-recipes/{recipe_id}/apply",
    response_model=WorkflowRecipeApplicationResponse,
    status_code=status.HTTP_201_CREATED,
)
@v3_router.post(
    "/products/{product_id}/workflow-recipes/{recipe_id}/apply",
    response_model=WorkflowRecipeApplicationResponse,
    status_code=status.HTTP_201_CREATED,
)
def apply_workflow_recipe_endpoint(
    product_id: str,
    recipe_id: str,
    payload: ApplyWorkflowRecipeRequest,
    session: Session = Depends(get_session),
) -> WorkflowRecipeApplicationResponse:
    result = apply_workflow_recipe(
        session,
        product_id=product_id,
        recipe_id=recipe_id,
        expected_recipe_version=payload.expected_recipe_version,
        expected_graph_revision=payload.expected_graph_revision,
        preview_digest=payload.preview_digest,
        idempotency_key=payload.idempotency_key,
    )
    projection = get_graph_projection(
        session,
        product_id=product_id,
        graph_id=result.graph.id,
    )
    return serialize_workflow_recipe_application(
        result,
        serialize_graph_projection(projection),
    )


@router.delete("/workflow-recipes/{recipe_id}", response_model=WorkflowRecipeArchiveResponse)
@v3_router.delete("/workflow-recipes/{recipe_id}", response_model=WorkflowRecipeArchiveResponse)
def archive_workflow_recipe_endpoint(
    recipe_id: str,
    expected_recipe_version: int = Query(ge=1),
    session: Session = Depends(get_session),
) -> WorkflowRecipeArchiveResponse:
    return serialize_workflow_recipe_archive(
        archive_workflow_recipe(
            session,
            recipe_id=recipe_id,
            expected_recipe_version=expected_recipe_version,
        )
    )


__all__ = ["router", "v3_router"]
