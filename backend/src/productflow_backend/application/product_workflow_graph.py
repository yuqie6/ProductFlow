from __future__ import annotations

from collections import deque
from typing import Any

from sqlalchemy import select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.domain.enums import WorkflowNodeType
from productflow_backend.domain.errors import BusinessValidationError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    Product,
    ProductWorkflow,
    WorkflowEdge,
    WorkflowNode,
    WorkflowRun,
)

DEFAULT_WORKFLOW_TITLE = "商品创意工作流"
DEFAULT_IMAGE_SIZE = "1024x1024"


def workflow_query():
    return select(ProductWorkflow).options(
        selectinload(ProductWorkflow.product).selectinload(Product.source_assets),
        selectinload(ProductWorkflow.product).selectinload(Product.creative_briefs),
        selectinload(ProductWorkflow.product).selectinload(Product.copy_sets),
        selectinload(ProductWorkflow.product).selectinload(Product.poster_variants),
        selectinload(ProductWorkflow.product).selectinload(Product.confirmed_copy_set),
        selectinload(ProductWorkflow.nodes),
        selectinload(ProductWorkflow.edges),
        selectinload(ProductWorkflow.runs).selectinload(WorkflowRun.node_runs),
    )


def get_product_or_raise(session: Session, product_id: str) -> Product:
    product = session.scalar(
        select(Product)
        .options(
            selectinload(Product.source_assets),
            selectinload(Product.creative_briefs),
            selectinload(Product.copy_sets),
            selectinload(Product.poster_variants),
            selectinload(Product.confirmed_copy_set),
        )
        .where(Product.id == product_id)
    )
    if product is None:
        raise NotFoundError("商品不存在")
    return product


def get_workflow_or_raise(session: Session, workflow_id: str) -> ProductWorkflow:
    workflow = session.scalar(workflow_query().where(ProductWorkflow.id == workflow_id))
    if workflow is None:
        raise NotFoundError("工作流不存在")
    return workflow


def get_active_workflow(session: Session, product_id: str) -> ProductWorkflow | None:
    return session.scalar(
        workflow_query().where(ProductWorkflow.product_id == product_id, ProductWorkflow.active.is_(True))
    )


def get_node_or_raise(session: Session, node_id: str) -> WorkflowNode:
    node = session.get(WorkflowNode, node_id)
    if node is None:
        raise NotFoundError("工作流节点不存在")
    return node


def get_edge_or_raise(session: Session, edge_id: str) -> WorkflowEdge:
    edge = session.get(WorkflowEdge, edge_id)
    if edge is None:
        raise NotFoundError("工作流连线不存在")
    return edge


def default_node_specs(product: Product) -> list[dict[str, Any]]:
    return [
        {
            "key": "context",
            "node_type": WorkflowNodeType.PRODUCT_CONTEXT,
            "title": "商品",
            "position_x": 40,
            "position_y": 120,
            "config_json": {},
        },
        {
            "key": "copy",
            "node_type": WorkflowNodeType.COPY_GENERATION,
            "title": "文案",
            "position_x": 320,
            "position_y": 80,
            "config_json": {"instruction": f"围绕 {product.name} 生成一版适合商品图的文案"},
        },
        {
            "key": "image",
            "node_type": WorkflowNodeType.IMAGE_GENERATION,
            "title": "生图",
            "position_x": 620,
            "position_y": 100,
            "config_json": {
                "instruction": "结合商品和文案生成商品图",
                "size": DEFAULT_IMAGE_SIZE,
            },
        },
        {
            "key": "reference",
            "node_type": WorkflowNodeType.REFERENCE_IMAGE,
            "title": "参考图",
            "position_x": 920,
            "position_y": 120,
            "config_json": {"role": "reference", "label": "生成结果槽位"},
        },
    ]


def default_edges(nodes_by_key: dict[str, WorkflowNode], workflow_id: str) -> list[WorkflowEdge]:
    pairs = [
        ("context", "copy"),
        ("context", "image"),
        ("copy", "image"),
        ("image", "reference"),
    ]
    return [
        WorkflowEdge(
            workflow_id=workflow_id,
            source_node_id=nodes_by_key[source].id,
            target_node_id=nodes_by_key[target].id,
            source_handle="output",
            target_handle="input",
        )
        for source, target in pairs
    ]


def default_title_for_type(node_type: WorkflowNodeType) -> str:
    return {
        WorkflowNodeType.PRODUCT_CONTEXT: "商品",
        WorkflowNodeType.REFERENCE_IMAGE: "参考图",
        WorkflowNodeType.COPY_GENERATION: "文案",
        WorkflowNodeType.IMAGE_GENERATION: "生图",
    }[node_type]


def topological_nodes(workflow: ProductWorkflow) -> list[WorkflowNode]:
    nodes = {node.id: node for node in workflow.nodes}
    incoming_count = {node_id: 0 for node_id in nodes}
    outgoing: dict[str, list[str]] = {node_id: [] for node_id in nodes}
    for edge in workflow.edges:
        if edge.source_node_id not in nodes or edge.target_node_id not in nodes:
            raise BusinessValidationError("工作流连线引用了不存在的节点")
        outgoing[edge.source_node_id].append(edge.target_node_id)
        incoming_count[edge.target_node_id] += 1

    queue = deque(
        sorted(
            [node_id for node_id, count in incoming_count.items() if count == 0],
            key=lambda item: nodes[item].position_x,
        )
    )
    ordered: list[WorkflowNode] = []
    while queue:
        node_id = queue.popleft()
        ordered.append(nodes[node_id])
        for target_id in outgoing[node_id]:
            incoming_count[target_id] -= 1
            if incoming_count[target_id] == 0:
                queue.append(target_id)
    if len(ordered) != len(nodes):
        raise ValueError("工作流不能包含循环依赖")
    return ordered


def latest_workflow_runs(workflow: ProductWorkflow, limit: int = 10) -> list[WorkflowRun]:
    return sorted(workflow.runs, key=lambda item: item.started_at, reverse=True)[:limit]
