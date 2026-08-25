from __future__ import annotations

import pytest
from helpers import _make_demo_image_bytes

from productflow_backend.application.agent.tools import (
    APPLY_GRAPH_TOOL_NAME,
    apply_agent_graph_change_set_tool,
    get_agent_contract,
    propose_agent_graph_change_set_tool,
    reconcile_agent_graph_change_set_tool,
    validate_agent_workflow_draft,
)
from productflow_backend.application.agent.workbenches import ensure_agent_workbench_bootstrap
from productflow_backend.application.product_workflow.graph_commands import (
    apply_graph_change_set,
    load_applied_graph,
    undo_last_graph_change_set,
)
from productflow_backend.application.product_workflow.graph_contracts import RenameNodeOp, WorkflowChangeSet
from productflow_backend.application.product_workflow.graph_direct_create import create_product_with_direct_graph
from productflow_backend.application.product_workflow.graph_proposals import (
    confirm_graph_proposal,
    discard_graph_proposal,
)
from productflow_backend.application.product_workflow.graph_queries import project_workflow_graph
from productflow_backend.application.product_workflow.graph_template import DirectCreateImageType
from productflow_backend.domain.enums import AgentToolMutationStatus, GraphActorType, GraphNodeType
from productflow_backend.domain.errors import BusinessValidationError, ConflictError
from productflow_backend.infrastructure.db.models import AgentToolMutation


def _live_workbench(db_session):
    created = create_product_with_direct_graph(
        db_session,
        name="提案商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "ref.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    bootstrap = ensure_agent_workbench_bootstrap(
        db_session,
        product_id=created.product.id,
        idempotency_key="proposal-workbench",
    )
    return created, bootstrap


def test_live_graph_hides_covering_draft_and_exposes_graph_tools(db_session) -> None:
    created, bootstrap = _live_workbench(db_session)
    contract = get_agent_contract(db_session, bootstrap.conversation.id)
    assert contract["has_live_graph"] is True
    assert "apply_graph_change_set_v1" in contract["system_prompt"]
    assert "propose_graph_change_set_v1" in contract["system_prompt"]
    assert "确认和取消只在画布上" in contract["system_prompt"]
    assert "discard_graph_proposal_v1" not in contract["system_prompt"]
    assert "不得调用 propose_workflow_draft" in contract["system_prompt"]
    with pytest.raises(ConflictError, match="不再使用 WorkflowDraft"):
        validate_agent_workflow_draft(
            db_session,
            conversation_id=bootstrap.conversation.id,
            value={},
        )
    del created


def test_single_agent_edit_applies_through_graph_command_and_undoes(db_session) -> None:
    created, bootstrap = _live_workbench(db_session)
    prompt = next(node for node in created.projection.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    result = apply_agent_graph_change_set_tool(
        db_session,
        conversation_id=bootstrap.conversation.id,
        idempotency_key="apply-rename-1",
        change_set={
            "base_graph_revision": created.graph.revision,
            "summary": "改提示词标题",
            "operations": [{"op": "rename_node", "node_ref": prompt.id, "title": "Agent 改名"}],
        },
    )
    assert result["applied"] is True
    live = load_applied_graph(db_session, created.graph)
    assert live.node(prompt.id).title == "Agent 改名"
    undo_last_graph_change_set(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
    )
    restored = load_applied_graph(db_session, created.graph)
    assert restored.node(prompt.id).title == prompt.title


def test_multi_node_proposal_is_unapplied_until_confirm_and_discard_leaves_zero(db_session) -> None:
    created, bootstrap = _live_workbench(db_session)
    prompt = next(node for node in created.projection.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    image = next(node for node in created.projection.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION)
    proposed = propose_agent_graph_change_set_tool(
        db_session,
        conversation_id=bootstrap.conversation.id,
        idempotency_key="propose-batch-1",
        change_set={
            "base_graph_revision": created.graph.revision,
            "summary": "批量改名",
            "operations": [
                {"op": "rename_node", "node_ref": prompt.id, "title": "预览提示词"},
                {"op": "rename_node", "node_ref": image.id, "title": "预览生图"},
            ],
        },
    )
    assert proposed["applied"] is False
    assert proposed["pending_confirmation"] is True
    live = load_applied_graph(db_session, created.graph)
    assert live.node(prompt.id).title == prompt.title
    projection = project_workflow_graph(db_session, created.graph)
    assert projection.pending_proposal is not None
    assert projection.pending_proposal.stale is False
    assert set(projection.pending_proposal.changed_node_ids) == {prompt.id, image.id}

    discard_graph_proposal(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        proposal_id=proposed["proposal_id"],
    )
    after_discard = project_workflow_graph(db_session, created.graph)
    assert after_discard.pending_proposal is None
    assert load_applied_graph(db_session, created.graph).node(prompt.id).title == prompt.title

    proposed_again = propose_agent_graph_change_set_tool(
        db_session,
        conversation_id=bootstrap.conversation.id,
        idempotency_key="propose-batch-2",
        change_set={
            "base_graph_revision": created.graph.revision,
            "summary": "再次批量改名",
            "operations": [
                {"op": "rename_node", "node_ref": prompt.id, "title": "确认提示词"},
                {"op": "rename_node", "node_ref": image.id, "title": "确认生图"},
            ],
        },
    )
    confirm_graph_proposal(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        proposal_id=proposed_again["proposal_id"],
    )
    confirmed = load_applied_graph(db_session, created.graph)
    assert confirmed.node(prompt.id).title == "确认提示词"
    assert confirmed.node(image.id).title == "确认生图"
    assert project_workflow_graph(db_session, created.graph).pending_proposal is None


def test_concurrent_same_node_write_returns_structured_conflict(db_session) -> None:
    created, bootstrap = _live_workbench(db_session)
    prompt = next(node for node in created.projection.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    base_revision = created.graph.revision
    apply_graph_change_set(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=base_revision,
            summary="人先改",
            actor_type=GraphActorType.USER,
            operations=[RenameNodeOp(node_ref=prompt.id, title="人改的标题")],
        ),
    )
    with pytest.raises(ConflictError, match="图 revision 已变化"):
        apply_agent_graph_change_set_tool(
            db_session,
            conversation_id=bootstrap.conversation.id,
            idempotency_key="apply-stale-1",
            change_set={
                "base_graph_revision": base_revision,
                "summary": "Agent 后写",
                "operations": [{"op": "rename_node", "node_ref": prompt.id, "title": "被覆盖"}],
            },
        )
    live = load_applied_graph(db_session, created.graph)
    assert live.node(prompt.id).title == "人改的标题"


def test_agent_immediate_apply_rejects_multi_operation_change_set(db_session) -> None:
    created, bootstrap = _live_workbench(db_session)
    prompt = next(node for node in created.projection.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    image = next(node for node in created.projection.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION)
    with pytest.raises(BusinessValidationError, match="立即写入只接受一条"):
        apply_agent_graph_change_set_tool(
            db_session,
            conversation_id=bootstrap.conversation.id,
            idempotency_key="apply-multi-1",
            change_set={
                "base_graph_revision": created.graph.revision,
                "summary": "一次改两个标题",
                "operations": [
                    {"op": "rename_node", "node_ref": prompt.id, "title": "一"},
                    {"op": "rename_node", "node_ref": image.id, "title": "二"},
                ],
            },
        )
    live = load_applied_graph(db_session, created.graph)
    assert live.node(prompt.id).title == prompt.title
    assert live.node(image.id).title == image.title


def test_agent_graph_apply_replays_same_idempotency_key(db_session) -> None:
    created, bootstrap = _live_workbench(db_session)
    prompt = next(node for node in created.projection.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    change_set = {
        "base_graph_revision": created.graph.revision,
        "summary": "幂等改名",
        "operations": [{"op": "rename_node", "node_ref": prompt.id, "title": "幂等标题"}],
    }
    first = apply_agent_graph_change_set_tool(
        db_session,
        conversation_id=bootstrap.conversation.id,
        idempotency_key="apply-replay-1",
        change_set=change_set,
    )
    second = apply_agent_graph_change_set_tool(
        db_session,
        conversation_id=bootstrap.conversation.id,
        idempotency_key="apply-replay-1",
        change_set=change_set,
    )
    assert second == first
    live = load_applied_graph(db_session, created.graph)
    assert live.node(prompt.id).title == "幂等标题"
    assert live.revision == first["revision"]


def test_agent_graph_apply_rejects_different_request_hash(db_session) -> None:
    created, bootstrap = _live_workbench(db_session)
    prompt = next(node for node in created.projection.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    apply_agent_graph_change_set_tool(
        db_session,
        conversation_id=bootstrap.conversation.id,
        idempotency_key="apply-hash-1",
        change_set={
            "base_graph_revision": created.graph.revision,
            "summary": "第一次改名",
            "operations": [{"op": "rename_node", "node_ref": prompt.id, "title": "第一次"}],
        },
    )
    with pytest.raises(ConflictError, match="同一工具 idempotency key"):
        apply_agent_graph_change_set_tool(
            db_session,
            conversation_id=bootstrap.conversation.id,
            idempotency_key="apply-hash-1",
            change_set={
                "base_graph_revision": created.graph.revision + 1,
                "summary": "第二次改名",
                "operations": [{"op": "rename_node", "node_ref": prompt.id, "title": "第二次"}],
            },
        )


def test_agent_graph_apply_unknown_is_queryable_by_key(db_session) -> None:
    created, bootstrap = _live_workbench(db_session)
    prompt = next(node for node in created.projection.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    change_set = {
        "base_graph_revision": created.graph.revision,
        "summary": "未知结果",
        "operations": [{"op": "rename_node", "node_ref": prompt.id, "title": "未知标题"}],
    }
    apply_agent_graph_change_set_tool(
        db_session,
        conversation_id=bootstrap.conversation.id,
        idempotency_key="apply-unknown-1",
        change_set=change_set,
    )
    from sqlalchemy import select

    mutation = db_session.scalar(
        select(AgentToolMutation).where(
            AgentToolMutation.conversation_id == bootstrap.conversation.id,
            AgentToolMutation.tool_name == APPLY_GRAPH_TOOL_NAME,
            AgentToolMutation.idempotency_key == "apply-unknown-1",
        )
    )
    assert mutation is not None
    mutation.status = AgentToolMutationStatus.UNKNOWN
    mutation.result_json = None
    db_session.commit()
    with pytest.raises(ConflictError, match="副作用账本"):
        apply_agent_graph_change_set_tool(
            db_session,
            conversation_id=bootstrap.conversation.id,
            idempotency_key="apply-unknown-1",
            change_set=change_set,
        )
    reconciled = reconcile_agent_graph_change_set_tool(
        db_session,
        conversation_id=bootstrap.conversation.id,
        idempotency_key="apply-unknown-1",
        change_set=change_set,
        tool_name=APPLY_GRAPH_TOOL_NAME,
    )
    assert reconciled.state == "unknown"
