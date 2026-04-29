from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from sqlalchemy import desc, select
from sqlalchemy.orm import Session, load_only, selectinload

from productflow_backend.domain.enums import WorkflowNodeType
from productflow_backend.domain.errors import NotFoundError
from productflow_backend.domain.workflow_rules import WorkflowRuleEdge, WorkflowRuleNode, topological_node_ids
from productflow_backend.infrastructure.db.models import (
    Product,
    ProductWorkflow,
    WorkflowEdge,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRun,
)

DEFAULT_WORKFLOW_TITLE = "商品创意工作流"
DEFAULT_IMAGE_SIZE = "1024x1024"


@dataclass(frozen=True, slots=True)
class ProductWorkflowStatusSnapshot:
    workflow: ProductWorkflow
    nodes: list[WorkflowNode]
    runs: list[WorkflowRun]


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


def workflow_status_query():
    return select(ProductWorkflow).options(
        load_only(
            ProductWorkflow.id,
            ProductWorkflow.product_id,
            ProductWorkflow.title,
            ProductWorkflow.active,
            ProductWorkflow.created_at,
            ProductWorkflow.updated_at,
        ),
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


def get_active_workflow_status(session: Session, product_id: str) -> ProductWorkflowStatusSnapshot:
    workflow = session.scalar(
        workflow_status_query().where(ProductWorkflow.product_id == product_id, ProductWorkflow.active.is_(True))
    )
    if workflow is None:
        get_product_or_raise(session, product_id)
        raise NotFoundError("工作流不存在")
    nodes = list(
        session.scalars(
            select(WorkflowNode)
            .options(
                load_only(
                    WorkflowNode.id,
                    WorkflowNode.workflow_id,
                    WorkflowNode.status,
                    WorkflowNode.failure_reason,
                    WorkflowNode.last_run_at,
                    WorkflowNode.updated_at,
                )
            )
            .where(WorkflowNode.workflow_id == workflow.id)
            .order_by(WorkflowNode.position_x, WorkflowNode.position_y, WorkflowNode.created_at)
        )
    )
    runs = list(
        session.scalars(
            select(WorkflowRun)
            .options(
                load_only(
                    WorkflowRun.id,
                    WorkflowRun.workflow_id,
                    WorkflowRun.status,
                    WorkflowRun.started_at,
                    WorkflowRun.finished_at,
                    WorkflowRun.failure_reason,
                ),
                selectinload(WorkflowRun.node_runs).load_only(
                    WorkflowNodeRun.id,
                    WorkflowNodeRun.workflow_run_id,
                    WorkflowNodeRun.node_id,
                    WorkflowNodeRun.status,
                    WorkflowNodeRun.failure_reason,
                    WorkflowNodeRun.started_at,
                    WorkflowNodeRun.finished_at,
                ),
            )
            .where(WorkflowRun.workflow_id == workflow.id)
            .order_by(desc(WorkflowRun.started_at), desc(WorkflowRun.id))
            .limit(10)
        )
    )
    return ProductWorkflowStatusSnapshot(workflow=workflow, nodes=nodes, runs=runs)


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
    ordered_ids = topological_node_ids(
        [
            WorkflowRuleNode(
                id=node.id,
                node_type=node.node_type,
                position_x=node.position_x,
                config_json=node.config_json,
            )
            for node in workflow.nodes
        ],
        [
            WorkflowRuleEdge(source_node_id=edge.source_node_id, target_node_id=edge.target_node_id)
            for edge in workflow.edges
        ],
    )
    return [nodes[node_id] for node_id in ordered_ids]


def latest_workflow_runs(workflow: ProductWorkflow, limit: int = 10) -> list[WorkflowRun]:
    return sorted(workflow.runs, key=lambda item: item.started_at, reverse=True)[:limit]
