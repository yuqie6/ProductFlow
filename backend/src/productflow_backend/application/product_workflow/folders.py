from __future__ import annotations

from collections.abc import Iterable, Sequence
from dataclasses import dataclass

from sqlalchemy import func, select
from sqlalchemy.orm import Session

from productflow_backend.application.time import now_utc
from productflow_backend.application.workflow_drafts.materialization import v2_workflow_query
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    ProductWorkflow,
    WorkflowFolder,
    WorkflowNode,
    new_id,
)

V2_WORKFLOW_SCHEMA_VERSION = 2


@dataclass(frozen=True, slots=True)
class WorkflowNodePosition:
    node_id: str
    position_x: int
    position_y: int


@dataclass(frozen=True, slots=True)
class WorkflowCanvasMutationResult:
    workflow: ProductWorkflow
    changed: bool
    dissolved_folder_ids: tuple[str, ...]


def create_workflow_folder(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    title: str,
    node_ids: Sequence[str],
    expected_edit_version: int,
) -> WorkflowCanvasMutationResult:
    def mutate(workflow: ProductWorkflow) -> tuple[bool, set[str]]:
        normalized_title = _normalize_folder_title(title)
        normalized_node_ids = _unique_node_ids(node_ids, require_non_empty=True)
        nodes = _lock_nodes(
            session,
            workflow=workflow,
            node_ids=normalized_node_ids,
        )
        source_folder_ids = {node.folder_id for node in nodes if node.folder_id is not None}
        max_sort_order = session.scalar(
            select(func.max(WorkflowFolder.sort_order)).where(
                WorkflowFolder.workflow_id == workflow.id
            )
        )
        sort_order = (max_sort_order if max_sort_order is not None else -1) + 1
        folder = WorkflowFolder(
            workflow_id=workflow.id,
            folder_key=f"folder-{new_id()}",
            title=normalized_title,
            sort_order=sort_order,
        )
        session.add(folder)
        session.flush()
        changed_at = now_utc()
        for node in nodes:
            node.folder_id = folder.id
            node.updated_at = changed_at
        dissolved = _delete_empty_folders(
            session,
            workflow_id=workflow.id,
            folder_ids=source_folder_ids,
        )
        return True, dissolved

    return _run_canvas_mutation(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        expected_edit_version=expected_edit_version,
        mutate=mutate,
    )


def rename_workflow_folder(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    folder_id: str,
    title: str,
    expected_edit_version: int,
) -> WorkflowCanvasMutationResult:
    def mutate(workflow: ProductWorkflow) -> tuple[bool, set[str]]:
        folder = _lock_folder(session, workflow=workflow, folder_id=folder_id)
        normalized_title = _normalize_folder_title(title)
        if folder.title == normalized_title:
            return False, set()
        folder.title = normalized_title
        folder.updated_at = now_utc()
        return True, set()

    return _run_canvas_mutation(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        expected_edit_version=expected_edit_version,
        mutate=mutate,
    )


def set_workflow_folder_members(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    folder_id: str,
    node_ids: Sequence[str],
    expected_edit_version: int,
) -> WorkflowCanvasMutationResult:
    def mutate(workflow: ProductWorkflow) -> tuple[bool, set[str]]:
        folder = _lock_folder(session, workflow=workflow, folder_id=folder_id)
        normalized_node_ids = _unique_node_ids(node_ids, require_non_empty=False)
        desired_nodes = _lock_nodes(
            session,
            workflow=workflow,
            node_ids=normalized_node_ids,
        )
        current_nodes = list(
            session.scalars(
                select(WorkflowNode)
                .where(
                    WorkflowNode.workflow_id == workflow.id,
                    WorkflowNode.folder_id == folder.id,
                )
                .order_by(WorkflowNode.id)
                .with_for_update()
            )
        )
        current_ids = {node.id for node in current_nodes}
        desired_ids = {node.id for node in desired_nodes}
        if current_ids == desired_ids:
            return False, set()

        changed_at = now_utc()
        desired_by_id = {node.id: node for node in desired_nodes}
        source_folder_ids = {
            node.folder_id
            for node in desired_nodes
            if node.folder_id is not None and node.folder_id != folder.id
        }
        for node in current_nodes:
            if node.id not in desired_ids:
                node.folder_id = None
                node.updated_at = changed_at
        for node_id in sorted(desired_ids):
            node = desired_by_id[node_id]
            if node.folder_id != folder.id:
                node.folder_id = folder.id
                node.updated_at = changed_at

        dissolved = _delete_empty_folders(
            session,
            workflow_id=workflow.id,
            folder_ids=source_folder_ids,
        )
        if not desired_ids:
            session.delete(folder)
            dissolved.add(folder.id)
        return True, dissolved

    return _run_canvas_mutation(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        expected_edit_version=expected_edit_version,
        mutate=mutate,
    )


def dissolve_workflow_folder(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    folder_id: str,
    expected_edit_version: int,
) -> WorkflowCanvasMutationResult:
    def mutate(workflow: ProductWorkflow) -> tuple[bool, set[str]]:
        folder = _lock_folder(session, workflow=workflow, folder_id=folder_id)
        members = list(
            session.scalars(
                select(WorkflowNode)
                .where(
                    WorkflowNode.workflow_id == workflow.id,
                    WorkflowNode.folder_id == folder.id,
                )
                .order_by(WorkflowNode.id)
                .with_for_update()
            )
        )
        changed_at = now_utc()
        for node in members:
            node.folder_id = None
            node.updated_at = changed_at
        session.delete(folder)
        return True, {folder.id}

    return _run_canvas_mutation(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        expected_edit_version=expected_edit_version,
        mutate=mutate,
    )


def translate_workflow_folder(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    folder_id: str,
    delta_x: int,
    delta_y: int,
    expected_edit_version: int,
) -> WorkflowCanvasMutationResult:
    def mutate(workflow: ProductWorkflow) -> tuple[bool, set[str]]:
        folder = _lock_folder(session, workflow=workflow, folder_id=folder_id)
        if delta_x == 0 and delta_y == 0:
            return False, set()
        members = list(
            session.scalars(
                select(WorkflowNode)
                .where(
                    WorkflowNode.workflow_id == workflow.id,
                    WorkflowNode.folder_id == folder.id,
                )
                .order_by(WorkflowNode.id)
                .with_for_update()
            )
        )
        if not members:
            raise ConflictError("文件夹没有成员，请刷新画布")
        changed_at = now_utc()
        for node in members:
            node.position_x += delta_x
            node.position_y += delta_y
            node.updated_at = changed_at
        return True, set()

    return _run_canvas_mutation(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        expected_edit_version=expected_edit_version,
        mutate=mutate,
    )


def update_workflow_node_layout(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    positions: Sequence[WorkflowNodePosition],
    expected_edit_version: int,
) -> WorkflowCanvasMutationResult:
    def mutate(workflow: ProductWorkflow) -> tuple[bool, set[str]]:
        if not positions:
            raise BusinessValidationError("节点位置列表不能为空")
        node_ids = [position.node_id for position in positions]
        normalized_node_ids = _unique_node_ids(node_ids, require_non_empty=True)
        nodes = _lock_nodes(
            session,
            workflow=workflow,
            node_ids=normalized_node_ids,
        )
        nodes_by_id = {node.id: node for node in nodes}
        changed_at = now_utc()
        changed = False
        for position in positions:
            node = nodes_by_id[position.node_id]
            if node.position_x == position.position_x and node.position_y == position.position_y:
                continue
            node.position_x = position.position_x
            node.position_y = position.position_y
            node.updated_at = changed_at
            changed = True
        return changed, set()

    return _run_canvas_mutation(
        session,
        product_id=product_id,
        workflow_id=workflow_id,
        expected_edit_version=expected_edit_version,
        mutate=mutate,
    )


def _run_canvas_mutation(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    expected_edit_version: int,
    mutate,
) -> WorkflowCanvasMutationResult:
    try:
        workflow = session.scalar(
            select(ProductWorkflow)
            .where(
                ProductWorkflow.id == workflow_id,
                ProductWorkflow.product_id == product_id,
            )
            .with_for_update()
        )
        if workflow is None:
            raise NotFoundError("商品工作流不存在")
        if workflow.schema_version != V2_WORKFLOW_SCHEMA_VERSION:
            raise ConflictError("画布文件夹只支持 schema-v2 工作流")
        if not workflow.active:
            raise ConflictError("只能修改 active schema-v2 工作流")
        if workflow.edit_version != expected_edit_version:
            raise ConflictError("工作流 edit version 已变化，请刷新后重试")

        changed, dissolved_folder_ids = mutate(workflow)
        if changed:
            workflow.edit_version += 1
            workflow.updated_at = now_utc()
        session.commit()
        session.expire_all()
        return WorkflowCanvasMutationResult(
            workflow=_reload_workflow(session, workflow.id),
            changed=changed,
            dissolved_folder_ids=tuple(sorted(dissolved_folder_ids)),
        )
    except Exception:
        session.rollback()
        raise


def _reload_workflow(session: Session, workflow_id: str) -> ProductWorkflow:
    workflow = session.scalar(v2_workflow_query().where(ProductWorkflow.id == workflow_id))
    if workflow is None:
        raise NotFoundError("商品工作流不存在")
    return workflow


def _normalize_folder_title(title: str) -> str:
    normalized = title.strip()
    if not normalized:
        raise BusinessValidationError("文件夹名称不能为空")
    if len(normalized) > 255:
        raise BusinessValidationError("文件夹名称不能超过 255 个字符")
    return normalized


def _unique_node_ids(node_ids: Sequence[str], *, require_non_empty: bool) -> tuple[str, ...]:
    normalized = tuple(node_ids)
    if require_non_empty and not normalized:
        raise BusinessValidationError("文件夹必须至少包含一个节点")
    if len(normalized) != len(set(normalized)):
        raise BusinessValidationError("节点 ID 不能重复")
    return normalized


def _lock_nodes(
    session: Session,
    *,
    workflow: ProductWorkflow,
    node_ids: Sequence[str],
) -> list[WorkflowNode]:
    if not node_ids:
        return []
    nodes = list(
        session.scalars(
            select(WorkflowNode)
            .where(
                WorkflowNode.id.in_(node_ids),
                WorkflowNode.workflow_id == workflow.id,
            )
            .order_by(WorkflowNode.id)
            .with_for_update()
        )
    )
    if {node.id for node in nodes} != set(node_ids):
        raise NotFoundError("工作流节点不存在或不属于当前工作流")
    if any(node.schema_version != V2_WORKFLOW_SCHEMA_VERSION for node in nodes):
        raise ConflictError("画布文件夹只能包含 schema-v2 节点")
    return nodes


def _lock_folder(
    session: Session,
    *,
    workflow: ProductWorkflow,
    folder_id: str,
) -> WorkflowFolder:
    folder = session.scalar(
        select(WorkflowFolder)
        .where(
            WorkflowFolder.id == folder_id,
            WorkflowFolder.workflow_id == workflow.id,
        )
        .with_for_update()
    )
    if folder is None:
        raise NotFoundError("工作流文件夹不存在")
    return folder


def _delete_empty_folders(
    session: Session,
    *,
    workflow_id: str,
    folder_ids: Iterable[str],
) -> set[str]:
    candidate_ids = sorted(set(folder_ids))
    if not candidate_ids:
        return set()
    session.flush()
    folders = list(
        session.scalars(
            select(WorkflowFolder)
            .where(
                WorkflowFolder.workflow_id == workflow_id,
                WorkflowFolder.id.in_(candidate_ids),
            )
            .order_by(WorkflowFolder.id)
            .with_for_update()
        )
    )
    dissolved: set[str] = set()
    for folder in folders:
        has_member = session.scalar(
            select(WorkflowNode.id).where(WorkflowNode.folder_id == folder.id).limit(1)
        )
        if has_member is None:
            dissolved.add(folder.id)
            session.delete(folder)
    return dissolved


__all__ = [
    "WorkflowCanvasMutationResult",
    "WorkflowNodePosition",
    "create_workflow_folder",
    "dissolve_workflow_folder",
    "rename_workflow_folder",
    "set_workflow_folder_members",
    "translate_workflow_folder",
    "update_workflow_node_layout",
]
