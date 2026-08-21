from __future__ import annotations

from dataclasses import dataclass

from sqlalchemy import select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.product_workflow.draft_graph_adapter import (
    build_draft_initial_graph_change_set,
)
from productflow_backend.application.product_workflow.graph_commands import (
    get_active_workflow_graph,
    stage_new_workflow_graph,
)
from productflow_backend.application.product_workflow.graph_queries import GraphProjection, project_workflow_graph
from productflow_backend.application.time import now_utc
from productflow_backend.application.workflow_drafts.service import (
    parse_and_verify_draft_revision,
    validate_workflow_draft_reference_assets,
)
from productflow_backend.domain.enums import WorkflowDraftStatus
from productflow_backend.domain.errors import ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    Product,
    WorkflowDraft,
    WorkflowDraftRevision,
    WorkflowGraph,
)


@dataclass(frozen=True, slots=True)
class DraftGraphPersistResult:
    graph: WorkflowGraph
    projection: GraphProjection
    created: bool


def persist_confirmed_draft_graph(
    session: Session,
    *,
    product_id: str,
    draft_id: str,
    expected_draft_version: int,
) -> DraftGraphPersistResult:
    product = session.scalar(select(Product).where(Product.id == product_id).with_for_update())
    if product is None:
        raise NotFoundError("商品不存在")
    draft = session.scalar(
        select(WorkflowDraft)
        .where(WorkflowDraft.id == draft_id, WorkflowDraft.product_id == product_id)
        .with_for_update()
    )
    if draft is None:
        raise NotFoundError("WorkflowDraft 不存在")
    requested_revision = session.scalar(
        select(WorkflowDraftRevision)
        .options(selectinload(WorkflowDraftRevision.visual_system_version))
        .where(
            WorkflowDraftRevision.draft_id == draft.id,
            WorkflowDraftRevision.version == expected_draft_version,
        )
    )
    if requested_revision is None:
        raise ConflictError("WorkflowDraft expected version 不存在")
    if draft.current_revision_id != requested_revision.id:
        raise ConflictError("WorkflowDraft version 已变化，请基于最新 revision 物化")
    existing = session.scalar(
        select(WorkflowGraph).where(WorkflowGraph.source_draft_revision_id == requested_revision.id)
    )
    if existing is not None:
        return DraftGraphPersistResult(
            graph=existing,
            projection=project_workflow_graph(session, existing),
            created=False,
        )
    if draft.status != WorkflowDraftStatus.CONFIRMED or requested_revision.confirmed_at is None:
        raise ConflictError("只有 confirmed WorkflowDraft revision 可以物化")
    active = get_active_workflow_graph(session, product_id=product_id)
    if active is not None:
        raise ConflictError("商品已有 active schema-v3 工作流")

    artifact = parse_and_verify_draft_revision(requested_revision)
    validate_workflow_draft_reference_assets(session, product_id=product_id, artifact=artifact)
    change_set = build_draft_initial_graph_change_set(
        artifact,
        draft_revision_id=requested_revision.id,
        visual_system_version_id=requested_revision.visual_system_version_id,
        source_product_id=product_id,
        fact_set_version_id=requested_revision.fact_set_version.id if requested_revision.fact_set_version else None,
    )
    try:
        command = stage_new_workflow_graph(
            session,
            product_id=product_id,
            change_set=change_set,
            title=artifact.title,
            source_draft_revision_id=requested_revision.id,
        )
        draft.status = WorkflowDraftStatus.READY
        draft.updated_at = now_utc()
        session.commit()
    except Exception:
        session.rollback()
        raise
    session.expire_all()
    graph = session.get(WorkflowGraph, command.graph.id)
    assert graph is not None
    return DraftGraphPersistResult(
        graph=graph,
        projection=project_workflow_graph(session, graph),
        created=True,
    )
