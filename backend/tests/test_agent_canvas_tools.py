from __future__ import annotations

import pytest
from helpers import _make_demo_image_bytes

from productflow_backend.application.agent.graph_tools import (
    apply_agent_graph_change_set_tool,
    cancel_agent_workflow_run_tool,
    canvas_focus_for_turns,
    discard_agent_graph_proposal_tool,
    focus_agent_canvas_items_tool,
    get_agent_node_detail,
    propose_agent_graph_change_set_tool,
    reconcile_agent_canvas_focus_tool,
    reconcile_agent_graph_proposal_discard_tool,
    reconcile_agent_workflow_run_cancel_tool,
)
from productflow_backend.application.agent.workbenches import ensure_agent_workbench_bootstrap
from productflow_backend.application.product_workflow.graph_direct_create import create_product_with_direct_graph
from productflow_backend.application.product_workflow.graph_queries import project_workflow_graph
from productflow_backend.application.product_workflow.graph_runs import submit_graph_run
from productflow_backend.application.product_workflow.graph_template import DirectCreateImageType
from productflow_backend.domain.enums import AgentTurnStatus, GraphNodeType, GraphRunScope, WorkflowRunStatus
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import AgentTurnProjection


def _live_workbench(db_session):
    created = create_product_with_direct_graph(
        db_session,
        name="画布工具商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "ref.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    bootstrap = ensure_agent_workbench_bootstrap(
        db_session,
        product_id=created.product.id,
        idempotency_key="canvas-tools-workbench",
    )
    return created, bootstrap


def test_pin_change_set_creates_bound_image_asset_without_reference_edge(db_session) -> None:
    created, bootstrap = _live_workbench(db_session)
    image = next(node for node in created.projection.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION)
    asset_id = created.created_assets[0].id
    before_ids = {node.id for node in created.projection.nodes}
    before_edge_ids = {edge.id for edge in created.projection.edges}
    result = apply_agent_graph_change_set_tool(
        db_session,
        conversation_id=bootstrap.conversation.id,
        idempotency_key="pin-asset-1",
        change_set={
            "base_graph_revision": created.graph.revision,
            "summary": "固定为图片素材",
            "operations": [
                {
                    "op": "create_node",
                    "client_ref": "pin-asset-1",
                    "node_type": "image_asset",
                    "title": "图片素材",
                    "position_x": image.position_x + 48,
                    "position_y": image.position_y + 48,
                    "config": {},
                    "bound_asset_id": asset_id,
                }
            ],
        },
    )
    assert result["applied"] is True
    live = project_workflow_graph(db_session, created.graph)
    pinned = next(node for node in live.nodes if node.id not in before_ids)
    assert pinned.node_type == GraphNodeType.IMAGE_ASSET
    assert pinned.bound_asset_id == asset_id
    assert {edge.id for edge in live.edges} == before_edge_ids


def test_get_node_detail_returns_config_edges_and_artifact_summary(db_session) -> None:
    created, bootstrap = _live_workbench(db_session)
    image = next(node for node in created.projection.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION)
    detail = get_agent_node_detail(
        db_session,
        conversation_id=bootstrap.conversation.id,
        node_id=image.id,
    )
    assert detail["accepted"] is True
    assert detail["node"]["id"] == image.id
    assert detail["node"]["node_type"] == "image_generation"
    assert detail["node"]["config_status"] == image.config_status.value
    assert isinstance(detail["node"]["config"], dict)
    assert any(edge["role"] == "prompt" for edge in detail["node"]["incoming"])
    assert "current_artifact" in detail["node"]
    with pytest.raises(NotFoundError):
        get_agent_node_detail(
            db_session,
            conversation_id=bootstrap.conversation.id,
            node_id="missing-node",
        )


def test_discard_workflow_proposal_tool_clears_pending_overlay(db_session) -> None:
    created, bootstrap = _live_workbench(db_session)
    prompt = next(node for node in created.projection.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    image = next(node for node in created.projection.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION)
    with pytest.raises(ConflictError):
        discard_agent_graph_proposal_tool(
            db_session,
            conversation_id=bootstrap.conversation.id,
            idempotency_key="discard-none",
        )
    proposed = propose_agent_graph_change_set_tool(
        db_session,
        conversation_id=bootstrap.conversation.id,
        idempotency_key="propose-for-discard",
        change_set={
            "base_graph_revision": created.graph.revision,
            "summary": "批量改名",
            "operations": [
                {"op": "rename_node", "node_ref": prompt.id, "title": "预览提示词"},
                {"op": "rename_node", "node_ref": image.id, "title": "预览生图"},
            ],
        },
    )
    discarded = discard_agent_graph_proposal_tool(
        db_session,
        conversation_id=bootstrap.conversation.id,
        idempotency_key="discard-pending",
        proposal_id=proposed["proposal_id"],
    )
    assert discarded["discarded"] is True
    assert discarded["proposal_id"] == proposed["proposal_id"]
    replay = discard_agent_graph_proposal_tool(
        db_session,
        conversation_id=bootstrap.conversation.id,
        idempotency_key="discard-pending",
        proposal_id=proposed["proposal_id"],
    )
    assert replay == discarded
    assert project_workflow_graph(db_session, created.graph).pending_proposal is None
    reconciled = reconcile_agent_graph_proposal_discard_tool(
        db_session,
        conversation_id=bootstrap.conversation.id,
        idempotency_key="discard-pending",
        proposal_id=proposed["proposal_id"],
    )
    assert reconciled.state == "applied"


def test_cancel_workflow_run_tool_cancels_running_graph(db_session) -> None:
    created, bootstrap = _live_workbench(db_session)
    submission = submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.GRAPH,
        enqueue=lambda _run_id: None,
    )
    cancelled = cancel_agent_workflow_run_tool(
        db_session,
        conversation_id=bootstrap.conversation.id,
        run_id=submission.run.id,
        idempotency_key="cancel-run-1",
    )
    assert cancelled["cancelled"] is True
    assert cancelled["run_id"] == submission.run.id
    assert cancelled["status"] == WorkflowRunStatus.CANCELLED.value
    replay = cancel_agent_workflow_run_tool(
        db_session,
        conversation_id=bootstrap.conversation.id,
        run_id=submission.run.id,
        idempotency_key="cancel-run-1",
    )
    assert replay == cancelled
    reconciled = reconcile_agent_workflow_run_cancel_tool(
        db_session,
        conversation_id=bootstrap.conversation.id,
        run_id=submission.run.id,
        idempotency_key="cancel-run-1",
    )
    assert reconciled.state == "applied"


def test_focus_canvas_items_records_bounded_request_for_turn_projection(db_session) -> None:
    created, bootstrap = _live_workbench(db_session)
    node_id = created.projection.nodes[0].id
    with pytest.raises(BusinessValidationError):
        focus_agent_canvas_items_tool(
            db_session,
            conversation_id=bootstrap.conversation.id,
            node_ids=[],
            idempotency_key="focus-empty",
        )
    turn = AgentTurnProjection(
        conversation_id=bootstrap.conversation.id,
        harness_turn_id="canvas-focus-turn",
        idempotency_key="canvas-focus-turn",
        request_hash="c" * 64,
        input_text="看这个节点",
        input_asset_ids_json=[],
        status=AgentTurnStatus.RUNNING,
    )
    db_session.add(turn)
    db_session.commit()
    focused = focus_agent_canvas_items_tool(
        db_session,
        conversation_id=bootstrap.conversation.id,
        node_ids=[node_id],
        idempotency_key="focus-node-1",
    )
    assert focused["accepted"] is True
    assert focused["node_ids"] == [node_id]
    assert focused["request_id"]
    replay = focus_agent_canvas_items_tool(
        db_session,
        conversation_id=bootstrap.conversation.id,
        node_ids=[node_id],
        idempotency_key="focus-node-1",
    )
    assert replay["request_id"] == focused["request_id"]
    mapping = canvas_focus_for_turns(db_session, [turn])
    assert mapping[turn.id]["request_id"] == focused["request_id"]
    assert mapping[turn.id]["node_ids"] == [node_id]
    reconciled = reconcile_agent_canvas_focus_tool(
        db_session,
        conversation_id=bootstrap.conversation.id,
        node_ids=[node_id],
        idempotency_key="focus-node-1",
    )
    assert reconciled.state == "applied"
    with pytest.raises(NotFoundError):
        focus_agent_canvas_items_tool(
            db_session,
            conversation_id=bootstrap.conversation.id,
            node_ids=["missing-node"],
            idempotency_key="focus-missing",
        )
