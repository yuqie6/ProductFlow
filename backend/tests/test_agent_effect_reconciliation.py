from __future__ import annotations

from fastapi.testclient import TestClient
from helpers import _login
from sqlalchemy import select

from productflow_backend.application.agent import effect_reconciliation as agent_effect_reconciliation
from productflow_backend.application.agent.conversations import reserve_agent_turn
from productflow_backend.application.agent.product_workspaces import (
    AgentProductWorkspaceReconcileResult,
    create_agent_product_draft_workspace_from_global_conversation,
)
from productflow_backend.application.agent.sessions import create_agent_session
from productflow_backend.domain.enums import AgentCheckpointKind, AgentExecutionPhase, AgentTurnStatus
from productflow_backend.infrastructure.db.models import (
    AgentTurnCheckpoint,
    AgentTurnEffectReconciliation,
    AgentTurnExecution,
)
from productflow_backend.presentation.api import create_app


def _unknown_global_turn(db_session, *, tool_call_id: str, product_name: str, idempotency_key: str):
    agent_session = create_agent_session(db_session, title="对账测试 Session")
    conversation = agent_session.conversations[0]
    reservation = reserve_agent_turn(
        db_session,
        product_id=None,
        conversation_id=conversation.id,
        input_text="恢复未知副作用",
        input_asset_ids=[],
        idempotency_key=f"turn-{tool_call_id}",
    )
    projection = reservation.projection
    projection.status = AgentTurnStatus.UNKNOWN
    execution = AgentTurnExecution(
        turn_projection_id=projection.id,
        harness_turn_id="unknown-effect-harness-turn",
        attempt=1,
        fencing_token=1,
        phase=AgentExecutionPhase.TERMINAL,
    )
    db_session.add(execution)
    db_session.flush()
    db_session.add(
        AgentTurnCheckpoint(
            turn_projection_id=projection.id,
            execution_id=execution.id,
            attempt=1,
            fencing_token=1,
            sequence=1,
            kind=AgentCheckpointKind.TOOL_EFFECT_INTENT,
            payload_json={
                "tool_name": "create_product_workspace_v1",
                "tool_call_id": tool_call_id,
                "idempotency_key": idempotency_key,
                "product_name": product_name,
            },
        )
    )
    db_session.commit()
    return conversation, projection


def test_unknown_effect_reconciliation_is_durable_and_does_not_replay_failed_lookup(db_session) -> None:
    conversation, projection = _unknown_global_turn(
        db_session,
        tool_call_id="workspace-tool-1",
        product_name="未创建商品",
        idempotency_key="workspace-key-1",
    )

    first = agent_effect_reconciliation.reconcile_agent_turn_effect(
        db_session,
        product_id=None,
        conversation_id=conversation.id,
        projection_id=projection.id,
        tool_call_id="workspace-tool-1",
    )
    assert first.effect_result == "failed"
    assert first.reconciliation_state == "not_applied"

    second = agent_effect_reconciliation.reconcile_agent_turn_effect(
        db_session,
        product_id=None,
        conversation_id=conversation.id,
        projection_id=projection.id,
        tool_call_id="workspace-tool-1",
    )
    assert second.id == first.id
    assert second.effect_result == "failed"
    assert second.reconciliation_state == "not_applied"
    assert db_session.scalar(
        select(AgentTurnEffectReconciliation).where(
            AgentTurnEffectReconciliation.turn_projection_id == projection.id,
            AgentTurnEffectReconciliation.tool_call_id == "workspace-tool-1",
        )
    ) is not None
    assert len(
        list(
            db_session.scalars(
                select(AgentTurnEffectReconciliation).where(
                    AgentTurnEffectReconciliation.turn_projection_id == projection.id
                )
            )
        )
    ) == 1


def test_unknown_effect_can_be_reconciled_again_after_business_ledger_appears(
    db_session,
    monkeypatch,
) -> None:
    conversation, projection = _unknown_global_turn(
        db_session,
        tool_call_id="workspace-tool-2",
        product_name="可恢复商品",
        idempotency_key="workspace-key-2",
    )
    original_reconcile = agent_effect_reconciliation.reconcile_agent_product_draft_workspace_from_global_conversation
    monkeypatch.setattr(
        agent_effect_reconciliation,
        "reconcile_agent_product_draft_workspace_from_global_conversation",
        lambda *args, **kwargs: AgentProductWorkspaceReconcileResult(
            state="unknown",
            detail="第一次查询时聚合尚不可读",
        ),
    )

    first = agent_effect_reconciliation.reconcile_agent_turn_effect(
        db_session,
        product_id=None,
        conversation_id=conversation.id,
        projection_id=projection.id,
        tool_call_id="workspace-tool-2",
    )
    assert first.effect_result == "unknown"
    assert first.reconciliation_state == "unknown"

    create_agent_product_draft_workspace_from_global_conversation(
        db_session,
        global_conversation_id=conversation.id,
        name="可恢复商品",
        idempotency_key="workspace-key-2",
    )
    monkeypatch.setattr(
        agent_effect_reconciliation,
        "reconcile_agent_product_draft_workspace_from_global_conversation",
        original_reconcile,
    )

    second = agent_effect_reconciliation.reconcile_agent_turn_effect(
        db_session,
        product_id=None,
        conversation_id=conversation.id,
        projection_id=projection.id,
        tool_call_id="workspace-tool-2",
    )
    assert second.id == first.id
    assert second.effect_result == "applied"
    assert second.reconciliation_state == "applied"
    assert second.result_json is not None
    assert second.result_json["product_name"] == "可恢复商品"
    assert db_session.scalar(
        select(AgentTurnEffectReconciliation).where(
            AgentTurnEffectReconciliation.turn_projection_id == projection.id,
            AgentTurnEffectReconciliation.tool_call_id == "workspace-tool-2",
        )
    ).effect_result == "applied"


def test_unknown_effect_reconciliation_http_contract_is_idempotent(db_session, monkeypatch) -> None:
    conversation, projection = _unknown_global_turn(
        db_session,
        tool_call_id="workspace-tool-http",
        product_name="HTTP 对账商品",
        idempotency_key="workspace-key-http",
    )
    monkeypatch.setattr(
        agent_effect_reconciliation,
        "reconcile_agent_product_draft_workspace_from_global_conversation",
        lambda *args, **kwargs: AgentProductWorkspaceReconcileResult(
            state="unknown",
            detail="待人工对账",
        ),
    )

    client = TestClient(create_app())
    _login(client)
    response = client.post(
        f"/api/v2/agent-conversations/{conversation.id}/turns/{projection.id}/effect-reconciliation",
        json={"tool_call_id": "workspace-tool-http"},
    )

    assert response.status_code == 200, response.text
    assert response.json()["effect_result"] == "unknown"
    assert response.json()["reconciliation_state"] == "unknown"
    assert response.json()["projection_id"] == projection.id
