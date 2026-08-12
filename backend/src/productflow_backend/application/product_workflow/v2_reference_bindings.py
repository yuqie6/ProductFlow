from __future__ import annotations

from dataclasses import dataclass

from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import WorkflowNodeStatus, WorkflowNodeType
from productflow_backend.domain.errors import ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    Product,
    ProductImageAsset,
    ProductWorkflow,
    WorkflowEdge,
    WorkflowNode,
    WorkflowNodeRun,
)

V2_WORKFLOW_SCHEMA_VERSION = 2
_STALE_NODE_TYPES = {WorkflowNodeType.PROMPT_GENERATION, WorkflowNodeType.IMAGE_GENERATION}
_ACTIVE_NODE_STATUSES = {WorkflowNodeStatus.QUEUED, WorkflowNodeStatus.RUNNING}


@dataclass(frozen=True, slots=True)
class V2ReferenceBindingResult:
    reference_node: WorkflowNode
    previous_asset_id: str | None
    affected_node_ids: tuple[str, ...]
    changed: bool


def bind_v2_reference_node_asset(
    session: Session,
    *,
    product_id: str,
    workflow_id: str,
    node_id: str,
    asset_id: str,
    expected_workflow_revision: int,
    expected_bound_asset_id: str | None,
) -> V2ReferenceBindingResult:
    product = session.scalar(select(Product).where(Product.id == product_id).with_for_update())
    if product is None:
        raise NotFoundError("商品不存在")
    workflow = session.scalar(
        select(ProductWorkflow)
        .where(ProductWorkflow.id == workflow_id, ProductWorkflow.product_id == product_id)
        .with_for_update()
    )
    if workflow is None:
        raise NotFoundError("商品工作流不存在")
    if workflow.schema_version != V2_WORKFLOW_SCHEMA_VERSION:
        raise ConflictError("再次引用只支持 schema-v2 工作流")
    if not workflow.active:
        raise ConflictError("只能修改 active schema-v2 工作流")
    if workflow.revision != expected_workflow_revision:
        raise ConflictError("工作流 revision 已变化")

    reference_node = session.scalar(
        select(WorkflowNode)
        .where(WorkflowNode.id == node_id, WorkflowNode.workflow_id == workflow.id)
        .with_for_update()
    )
    if reference_node is None:
        raise NotFoundError("工作流节点不存在")
    if reference_node.schema_version != V2_WORKFLOW_SCHEMA_VERSION:
        raise ConflictError("再次引用只支持 schema-v2 节点")
    if reference_node.node_type != WorkflowNodeType.REFERENCE_IMAGE:
        raise ConflictError("当前节点不是参考图节点")
    if reference_node.bound_image_asset_id != expected_bound_asset_id:
        raise ConflictError("参考图节点绑定已被其他操作修改")
    asset = session.scalar(
        select(ProductImageAsset)
        .where(ProductImageAsset.id == asset_id, ProductImageAsset.product_id == product_id)
        .with_for_update()
    )
    if asset is None:
        raise NotFoundError("商品图片不存在")

    previous_asset_id = reference_node.bound_image_asset_id
    if previous_asset_id == asset.id:
        session.commit()
        return V2ReferenceBindingResult(
            reference_node=reference_node,
            previous_asset_id=previous_asset_id,
            affected_node_ids=(),
            changed=False,
        )

    affected_ids = _reachable_stale_node_ids(
        session,
        workflow_id=workflow.id,
        source_node_id=reference_node.id,
    )
    affected_nodes = []
    if affected_ids:
        affected_nodes = list(
            session.scalars(
                select(WorkflowNode)
                .where(WorkflowNode.id.in_(affected_ids))
                .order_by(WorkflowNode.id)
                .with_for_update()
            )
        )
        if {node.id for node in affected_nodes} != affected_ids:
            raise ConflictError("工作流下游节点已变化")
        active_runs = list(
            session.scalars(
                select(WorkflowNodeRun)
                .where(
                    WorkflowNodeRun.node_id.in_(sorted(affected_ids)),
                    WorkflowNodeRun.status.in_(_ACTIVE_NODE_STATUSES),
                )
                .order_by(WorkflowNodeRun.node_id, WorkflowNodeRun.id)
                .with_for_update()
            )
        )
        if active_runs:
            raise ConflictError("受影响的提示词或图片节点正在运行")

    changed_at = now_utc()
    reference_node.bound_image_asset_id = asset.id
    reference_node.updated_at = changed_at
    for node in affected_nodes:
        node.status = WorkflowNodeStatus.IDLE
        node.failure_reason = None
        node.updated_at = changed_at
        if node.node_type == WorkflowNodeType.PROMPT_GENERATION:
            output = dict(node.output_json) if isinstance(node.output_json, dict) else {}
            superseded = {
                value
                for value in output.get("superseded_reference_asset_ids", [])
                if isinstance(value, str) and value
            }
            if previous_asset_id is not None:
                superseded.add(previous_asset_id)
            output["references_stale"] = True
            output["superseded_reference_asset_ids"] = sorted(superseded)
            node.output_json = output
    workflow.updated_at = changed_at
    session.commit()
    return V2ReferenceBindingResult(
        reference_node=reference_node,
        previous_asset_id=previous_asset_id,
        affected_node_ids=tuple(sorted(affected_ids)),
        changed=True,
    )


def _reachable_stale_node_ids(
    session: Session,
    *,
    workflow_id: str,
    source_node_id: str,
) -> set[str]:
    edges = list(
        session.scalars(
            select(WorkflowEdge)
            .where(WorkflowEdge.workflow_id == workflow_id)
            .order_by(WorkflowEdge.edge_key, WorkflowEdge.id)
        )
    )
    outgoing: dict[str, list[str]] = {}
    for edge in edges:
        outgoing.setdefault(edge.source_node_id, []).append(edge.target_node_id)
    seen: set[str] = set()
    queue = list(outgoing.get(source_node_id, []))
    while queue:
        node_id = queue.pop(0)
        if node_id in seen:
            continue
        seen.add(node_id)
        queue.extend(outgoing.get(node_id, []))
    if not seen:
        return set()
    return set(
        session.scalars(
            select(WorkflowNode.id).where(
                WorkflowNode.id.in_(seen),
                WorkflowNode.workflow_id == workflow_id,
                WorkflowNode.node_type.in_(_STALE_NODE_TYPES),
            )
        )
    )


__all__ = ["V2ReferenceBindingResult", "bind_v2_reference_node_asset"]
