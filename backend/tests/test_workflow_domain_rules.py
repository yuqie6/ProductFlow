from __future__ import annotations

import pytest

from productflow_backend.domain.enums import WorkflowNodeType
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.domain.workflow_rules import (
    WorkflowRuleEdge,
    WorkflowRuleNode,
    ready_workflow_node_ids,
    topological_node_ids,
)


def _node(node_id: str, node_type: WorkflowNodeType, *, x: int = 0) -> WorkflowRuleNode:
    return WorkflowRuleNode(id=node_id, node_type=node_type, position_x=x)


def test_topological_node_ids_orders_schema_v2_graph() -> None:
    nodes = [
        _node("context", WorkflowNodeType.PRODUCT_CONTEXT, x=0),
        _node("reference", WorkflowNodeType.REFERENCE_IMAGE, x=50),
        _node("prompt", WorkflowNodeType.PROMPT_GENERATION, x=100),
        _node("image", WorkflowNodeType.IMAGE_GENERATION, x=200),
    ]
    edges = [
        WorkflowRuleEdge("context", "prompt"),
        WorkflowRuleEdge("reference", "prompt"),
        WorkflowRuleEdge("prompt", "image"),
    ]

    ordered = topological_node_ids(nodes, edges)

    assert ordered.index("context") < ordered.index("prompt")
    assert ordered.index("reference") < ordered.index("prompt")
    assert ordered.index("prompt") < ordered.index("image")


def test_topological_node_ids_rejects_missing_nodes() -> None:
    nodes = [_node("prompt", WorkflowNodeType.PROMPT_GENERATION)]

    with pytest.raises(BusinessValidationError, match="工作流连线引用了不存在的节点"):
        topological_node_ids(nodes, [WorkflowRuleEdge("prompt", "missing")])


def test_topological_node_ids_rejects_cycle() -> None:
    nodes = [
        _node("prompt", WorkflowNodeType.PROMPT_GENERATION, x=100),
        _node("image", WorkflowNodeType.IMAGE_GENERATION, x=200),
    ]
    edges = [WorkflowRuleEdge("prompt", "image"), WorkflowRuleEdge("image", "prompt")]

    with pytest.raises(BusinessValidationError, match="工作流不能包含循环依赖"):
        topological_node_ids(nodes, edges)


def test_ready_workflow_node_ids_waits_for_prompt() -> None:
    nodes = [
        _node("prompt", WorkflowNodeType.PROMPT_GENERATION, x=100),
        _node("image", WorkflowNodeType.IMAGE_GENERATION, x=200),
    ]
    edges = [WorkflowRuleEdge("prompt", "image")]

    assert ready_workflow_node_ids(
        nodes=nodes,
        edges=edges,
        run_node_ids={"prompt", "image"},
        queued_node_ids={"prompt", "image"},
        succeeded_node_ids=set(),
    ) == ["prompt"]
    assert ready_workflow_node_ids(
        nodes=nodes,
        edges=edges,
        run_node_ids={"prompt", "image"},
        queued_node_ids={"image"},
        succeeded_node_ids={"prompt"},
    ) == ["image"]


def test_ready_workflow_node_ids_dispatches_parallel_images_after_prompt() -> None:
    nodes = [
        _node("prompt", WorkflowNodeType.PROMPT_GENERATION, x=100),
        _node("image-a", WorkflowNodeType.IMAGE_GENERATION, x=200),
        _node("image-b", WorkflowNodeType.IMAGE_GENERATION, x=250),
    ]
    edges = [WorkflowRuleEdge("prompt", "image-a"), WorkflowRuleEdge("prompt", "image-b")]

    ready = ready_workflow_node_ids(
        nodes=nodes,
        edges=edges,
        run_node_ids={"prompt", "image-a", "image-b"},
        queued_node_ids={"image-a", "image-b"},
        succeeded_node_ids={"prompt"},
    )

    assert ready == ["image-a", "image-b"]


def test_ready_workflow_node_ids_ignores_non_running_context_nodes() -> None:
    nodes = [
        _node("context", WorkflowNodeType.PRODUCT_CONTEXT, x=0),
        _node("reference", WorkflowNodeType.REFERENCE_IMAGE, x=50),
        _node("prompt", WorkflowNodeType.PROMPT_GENERATION, x=100),
    ]
    edges = [WorkflowRuleEdge("context", "prompt"), WorkflowRuleEdge("reference", "prompt")]

    ready = ready_workflow_node_ids(
        nodes=nodes,
        edges=edges,
        run_node_ids={"prompt"},
        queued_node_ids={"prompt"},
        succeeded_node_ids=set(),
    )

    assert ready == ["prompt"]
