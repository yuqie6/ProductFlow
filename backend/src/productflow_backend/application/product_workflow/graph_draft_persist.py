"""Draft 确认后一次事务物化完整 schema-v3 图。商品路径已退休该入口。"""

from __future__ import annotations

from dataclasses import dataclass

from sqlalchemy.orm import Session

from productflow_backend.application.product_workflow.graph_queries import GraphProjection
from productflow_backend.application.workflow_drafts.service import PRODUCT_WORKFLOW_DRAFT_RETIRED
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.db.models import WorkflowGraph


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
    """商品路径不再物化 WorkflowDraft。"""
    del session, product_id, draft_id, expected_draft_version
    raise ConflictError(PRODUCT_WORKFLOW_DRAFT_RETIRED)
