from __future__ import annotations

from typing import Any, NoReturn

from sqlalchemy import select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.workflow_drafts.contracts import WorkflowDraftPayloadV1
from productflow_backend.domain.errors import ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    Product,
    WorkflowDraft,
    WorkflowDraftLegacyArchiveSeed,
    WorkflowDraftRecipeSeed,
    WorkflowDraftRevision,
)

PRODUCT_WORKFLOW_DRAFT_RETIRED = "商品路径不再使用 WorkflowDraft"


def workflow_draft_query():
    return select(WorkflowDraft).options(
        selectinload(WorkflowDraft.revisions).selectinload(WorkflowDraftRevision.fact_set_version),
        selectinload(WorkflowDraft.revisions).selectinload(WorkflowDraftRevision.visual_system_version),
        selectinload(WorkflowDraft.current_revision),
        selectinload(WorkflowDraft.recipe_seed).selectinload(WorkflowDraftRecipeSeed.recipe_version),
        selectinload(WorkflowDraft.legacy_archive_seed).selectinload(WorkflowDraftLegacyArchiveSeed.workflow_archive),
        selectinload(WorkflowDraft.legacy_archive_seed).selectinload(
            WorkflowDraftLegacyArchiveSeed.canvas_agent_archive
        ),
        selectinload(WorkflowDraft.legacy_archive_seed).selectinload(
            WorkflowDraftLegacyArchiveSeed.user_template_archive
        ),
    )


def get_workflow_draft_or_raise(session: Session, *, product_id: str, draft_id: str) -> WorkflowDraft:
    draft = session.scalar(
        workflow_draft_query().where(WorkflowDraft.id == draft_id, WorkflowDraft.product_id == product_id)
    )
    if draft is None:
        _get_product_or_raise(session, product_id)
        raise NotFoundError("WorkflowDraft 不存在")
    return draft


def create_workflow_draft(
    session: Session,
    *,
    product_id: str,
    payload: WorkflowDraftPayloadV1 | dict[str, Any],
    ready_for_confirmation: bool,
    source_turn_id: str | None = None,
    source_artifact_step_id: str | None = None,
) -> WorkflowDraft:
    del session, product_id, payload, ready_for_confirmation, source_turn_id, source_artifact_step_id
    raise ConflictError(PRODUCT_WORKFLOW_DRAFT_RETIRED)


def append_workflow_draft_revision(
    session: Session,
    *,
    product_id: str,
    draft_id: str,
    expected_draft_version: int,
    payload: WorkflowDraftPayloadV1 | dict[str, Any],
    ready_for_confirmation: bool,
    source_turn_id: str | None = None,
    source_artifact_step_id: str | None = None,
    commit: bool = True,
) -> WorkflowDraft:
    del session, product_id, draft_id, expected_draft_version, payload
    del ready_for_confirmation, source_turn_id, source_artifact_step_id, commit
    raise ConflictError(PRODUCT_WORKFLOW_DRAFT_RETIRED)


def confirm_workflow_draft_revision(
    session: Session,
    *,
    product_id: str,
    draft_id: str,
    expected_draft_version: int,
    commit: bool = True,
) -> WorkflowDraft:
    del session, product_id, draft_id, expected_draft_version, commit
    raise ConflictError(PRODUCT_WORKFLOW_DRAFT_RETIRED)


def persist_confirmed_draft_graph(
    session: Session,
    *,
    product_id: str,
    draft_id: str,
    expected_draft_version: int,
) -> NoReturn:
    del session, product_id, draft_id, expected_draft_version
    raise ConflictError(PRODUCT_WORKFLOW_DRAFT_RETIRED)


def _get_product_or_raise(session: Session, product_id: str) -> Product:
    product = session.scalar(select(Product).where(Product.id == product_id))
    if product is None:
        raise NotFoundError("商品不存在")
    return product


__all__ = [
    "PRODUCT_WORKFLOW_DRAFT_RETIRED",
    "append_workflow_draft_revision",
    "confirm_workflow_draft_revision",
    "create_workflow_draft",
    "get_workflow_draft_or_raise",
    "persist_confirmed_draft_graph",
    "workflow_draft_query",
]
