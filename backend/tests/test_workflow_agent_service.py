from __future__ import annotations

from datetime import UTC, datetime
from pathlib import Path

import pytest
import sqlalchemy as sa
from alembic.config import Config
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes
from sqlalchemy import func, select
from workflow_draft_helpers import make_workflow_draft_payload

from alembic import command
from productflow_backend.application.agent_conversations import (
    attach_agent_workflow_draft_artifact,
    bind_harness_turn,
    create_agent_conversation,
    project_agent_turn_state,
    reserve_agent_turn,
)
from productflow_backend.application.agent_sync import (
    execute_agent_turn_sync,
    recover_unfinished_agent_turn_syncs,
)
from productflow_backend.application.agent_tools import (
    apply_agent_asset_rename,
    get_agent_contract,
    get_agent_product_context,
    inspect_agent_product_assets,
    list_agent_product_assets,
    prepare_agent_asset_rename,
    read_agent_product_asset_content,
    reconcile_agent_asset_rename,
)
from productflow_backend.application.use_cases import create_canonical_product
from productflow_backend.application.workflow_drafts.service import create_workflow_draft
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import (
    AgentConversationStatus,
    AgentTurnStatus,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.agent_service import (
    AgentServiceArtifact,
    AgentServiceQuestion,
    AgentServiceQuestionOption,
    AgentServiceTurnState,
)
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentToolMutation,
    AgentTurnProjection,
    WorkflowDraftRevision,
)


def _create_product_and_draft(
    db_session,
    *,
    name: str = "Agent 测试商品",
    image_count: int = 1,
):
    product = create_canonical_product(
        db_session,
        name=name,
        category="工业收纳",
        price="299.00",
        source_note="橙蓝色刀具收纳套装",
        image_uploads=[
            (_make_demo_image_bytes(), f"{name}-{index}.png", "image/png")
            for index in range(image_count)
        ],
    )
    asset = product.image_assets[0]
    payload = make_workflow_draft_payload(reference_asset_id=asset.id)
    draft = create_workflow_draft(
        db_session,
        product_id=product.id,
        payload=payload,
        ready_for_confirmation=False,
    )
    return product, asset, draft, payload


def test_conversation_and_turn_reservation_are_product_scoped_and_idempotent(db_session) -> None:
    product, asset, draft, _ = _create_product_and_draft(db_session)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    repeated_conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    assert repeated_conversation.id == conversation.id
    assert conversation.harness_run_id == conversation.id

    reservation = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="  请先核对商品资料  ",
        input_asset_ids=[asset.id],
        idempotency_key="turn-key-1",
    )
    assert reservation.created is True
    assert reservation.projection.input_text == "请先核对商品资料"
    assert reservation.projection.input_asset_ids_json == [asset.id]
    assert len(reservation.projection.request_hash) == 64

    repeated = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="请先核对商品资料",
        input_asset_ids=[asset.id],
        idempotency_key="turn-key-1",
    )
    assert repeated.created is False
    assert repeated.projection.id == reservation.projection.id
    assert db_session.scalar(select(func.count()).select_from(AgentTurnProjection)) == 1

    with pytest.raises(ConflictError, match="不同"):
        reserve_agent_turn(
            db_session,
            product_id=product.id,
            conversation_id=conversation.id,
            input_text="换成另一条请求",
            input_asset_ids=[asset.id],
            idempotency_key="turn-key-1",
        )

    other_product, other_asset, _, _ = _create_product_and_draft(db_session, name="其他商品")
    with pytest.raises(BusinessValidationError, match="当前商品"):
        reserve_agent_turn(
            db_session,
            product_id=product.id,
            conversation_id=conversation.id,
            input_text="查看跨商品图片",
            input_asset_ids=[other_asset.id],
            idempotency_key="turn-key-cross-product",
        )
    with pytest.raises(NotFoundError):
        reserve_agent_turn(
            db_session,
            product_id=other_product.id,
            conversation_id=conversation.id,
            input_text="尝试切换商品作用域",
            input_asset_ids=[],
            idempotency_key="turn-key-cross-conversation",
        )


@pytest.mark.parametrize(
    ("terminal_status", "conversation_status"),
    [
        (AgentTurnStatus.FAILED, AgentConversationStatus.FAILED),
        (AgentTurnStatus.CANCELED, AgentConversationStatus.CANCELED),
        (AgentTurnStatus.UNKNOWN, AgentConversationStatus.UNKNOWN),
    ],
)
def test_failed_canceled_or_unknown_turn_does_not_close_conversation(
    db_session,
    terminal_status: AgentTurnStatus,
    conversation_status: AgentConversationStatus,
) -> None:
    product, _, draft, _ = _create_product_and_draft(db_session)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    first = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="执行第一轮",
        input_asset_ids=[],
        idempotency_key=f"first-{terminal_status.value}",
    ).projection
    first = bind_harness_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=first.id,
        harness_turn_id=f"harness-{terminal_status.value}",
        status=AgentTurnStatus.RUNNING,
    )
    first = project_agent_turn_state(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=first.id,
        harness_turn_id=first.harness_turn_id or "",
        status=terminal_status,
        output_text=None,
        error_text="终态测试",
        question_json=None,
        finished_at=datetime.now(UTC),
    )
    assert first.conversation.status == conversation_status

    second = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="继续下一轮",
        input_asset_ids=[],
        idempotency_key=f"second-{terminal_status.value}",
    )
    assert second.created is True
    assert second.projection.conversation.status == AgentConversationStatus.COLLECTING


def test_turn_projection_and_artifact_sync_converge_without_storing_artifact_body(db_session) -> None:
    product, asset, draft, payload = _create_product_and_draft(db_session)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    projection = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="生成可确认的工作流草案",
        input_asset_ids=[asset.id],
        idempotency_key="artifact-turn",
    ).projection
    projection = bind_harness_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-turn-1",
        status=AgentTurnStatus.RUNNING,
    )
    assert projection.status == AgentTurnStatus.RUNNING

    projection = project_agent_turn_state(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-turn-1",
        status=AgentTurnStatus.AWAITING_CONFIRMATION,
        output_text="草案已生成，请确认。",
        error_text=None,
        question_json=None,
        finished_at=datetime.now(UTC),
    )
    assert projection.conversation.status == AgentConversationStatus.AWAITING_CONFIRMATION

    projection = attach_agent_workflow_draft_artifact(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-turn-1",
        artifact_name="propose_workflow_draft",
        artifact_step_id="artifact-step-1",
        artifact_value=payload,
    )
    revision_id = projection.workflow_draft_revision_id
    assert revision_id is not None
    assert projection.artifact_name == "propose_workflow_draft"
    assert projection.artifact_step_id == "artifact-step-1"
    assert not hasattr(projection, "artifact_value")
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraftRevision)) == 2

    repeated = attach_agent_workflow_draft_artifact(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id="harness-turn-1",
        artifact_name="propose_workflow_draft",
        artifact_step_id="artifact-step-1",
        artifact_value=payload,
    )
    assert repeated.workflow_draft_revision_id == revision_id
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraftRevision)) == 2

    changed_payload = {**payload, "confirmation_summary": "内容不同但仍符合 schema 的草案"}
    with pytest.raises(ConflictError, match="不能写入不同"):
        attach_agent_workflow_draft_artifact(
            db_session,
            product_id=product.id,
            conversation_id=conversation.id,
            projection_id=projection.id,
            harness_turn_id="harness-turn-1",
            artifact_name="propose_workflow_draft",
            artifact_step_id="artifact-step-1",
            artifact_value=changed_payload,
        )


def test_agent_read_tools_are_bounded_and_rename_is_reconcilable(db_session) -> None:
    product, asset, draft, _ = _create_product_and_draft(db_session, image_count=3)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )

    contract = get_agent_contract(db_session, conversation.id)
    assert contract["conversation_id"] == conversation.id
    assert contract["product_id"] == product.id
    assert contract["workflow_draft_id"] == draft.id
    assert contract["current_draft_version"] == 1
    assert contract["workflow_draft_schema"]["additionalProperties"] is False

    context = get_agent_product_context(db_session, conversation.id)
    assert context["product"]["name"] == product.name
    assert context["workflow_draft"]["version"] == 1
    assert "storage_path" not in str(context)

    first_page = list_agent_product_assets(
        db_session,
        conversation_id=conversation.id,
        limit=1,
    )
    assert len(first_page.items) == 1
    assert first_page.next_cursor is not None
    assert "storage_path" not in first_page.items[0]
    assert "url" not in first_page.items[0]
    second_page = list_agent_product_assets(
        db_session,
        conversation_id=conversation.id,
        after=first_page.next_cursor,
        limit=1,
    )
    assert second_page.items[0]["id"] != first_page.items[0]["id"]

    inspected = inspect_agent_product_assets(
        db_session,
        conversation_id=conversation.id,
        asset_ids=[asset.id],
    )
    assert inspected[0]["id"] == asset.id
    assert inspected[0]["mime_type"] == "image/png"
    content = read_agent_product_asset_content(
        db_session,
        conversation_id=conversation.id,
        asset_id=asset.id,
    )
    assert content.content == _make_demo_image_bytes()
    assert content.media_type == "image/png"

    prepared = prepare_agent_asset_rename(
        db_session,
        conversation_id=conversation.id,
        asset_id=asset.id,
        target_display_name="  商品正面参考图  ",
    )
    before = reconcile_agent_asset_rename(
        db_session,
        conversation_id=conversation.id,
        idempotency_key="rename-1",
        asset_id=prepared.asset_id,
        expected_display_name=prepared.expected_display_name,
        target_display_name=prepared.target_display_name,
    )
    assert before.state == "not_applied"

    renamed = apply_agent_asset_rename(
        db_session,
        conversation_id=conversation.id,
        idempotency_key="rename-1",
        asset_id=prepared.asset_id,
        expected_display_name=prepared.expected_display_name,
        target_display_name=prepared.target_display_name,
    )
    assert renamed == {
        "asset_id": asset.id,
        "display_name": "商品正面参考图",
        "applied": True,
    }
    assert apply_agent_asset_rename(
        db_session,
        conversation_id=conversation.id,
        idempotency_key="rename-1",
        asset_id=prepared.asset_id,
        expected_display_name=prepared.expected_display_name,
        target_display_name=prepared.target_display_name,
    ) == renamed
    assert db_session.scalar(select(func.count()).select_from(AgentToolMutation)) == 1

    after = reconcile_agent_asset_rename(
        db_session,
        conversation_id=conversation.id,
        idempotency_key="rename-1",
        asset_id=prepared.asset_id,
        expected_display_name=prepared.expected_display_name,
        target_display_name=prepared.target_display_name,
    )
    assert after.state == "applied"
    assert after.result == renamed
    ambiguous = reconcile_agent_asset_rename(
        db_session,
        conversation_id=conversation.id,
        idempotency_key="rename-without-ledger",
        asset_id=prepared.asset_id,
        expected_display_name=prepared.expected_display_name,
        target_display_name=prepared.target_display_name,
    )
    assert ambiguous.state == "conflict"

    with pytest.raises(ConflictError, match="不同"):
        apply_agent_asset_rename(
            db_session,
            conversation_id=conversation.id,
            idempotency_key="rename-1",
            asset_id=prepared.asset_id,
            expected_display_name=prepared.expected_display_name,
            target_display_name="另一名称",
        )


def test_internal_agent_routes_require_service_token_and_never_need_browser_session(
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    product, asset, draft, _ = _create_product_and_draft(db_session)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    internal_token = "agent-internal-token-with-at-least-32-characters"
    monkeypatch.setenv("AGENT_SERVICE_INTERNAL_TOKEN", internal_token)
    get_settings.cache_clear()
    client = TestClient(create_app())
    contract_path = f"/api/internal/v1/agent-conversations/{conversation.id}/contract"

    assert client.get(contract_path).status_code == 401
    _login(client)
    assert client.get(contract_path).status_code == 401
    assert client.get(
        contract_path,
        headers={"Authorization": "Bearer wrong-token"},
    ).status_code == 401

    headers = {"Authorization": f"Bearer {internal_token}"}
    contract = client.get(contract_path, headers=headers)
    assert contract.status_code == 200, contract.text
    assert contract.json()["conversation_id"] == conversation.id
    assets = client.get(
        f"/api/internal/v1/agent-conversations/{conversation.id}/assets?limit=10",
        headers=headers,
    )
    assert assets.status_code == 200, assets.text
    assert assets.json()["items"][0]["id"] == asset.id
    assert "storage_path" not in assets.text
    assert "url" not in assets.text
    content = client.get(
        f"/api/internal/v1/agent-conversations/{conversation.id}/assets/{asset.id}/content",
        headers=headers,
    )
    assert content.status_code == 200
    assert content.headers["content-type"] == "image/png"
    assert content.content == _make_demo_image_bytes()

    prepared = client.post(
        f"/api/internal/v1/agent-conversations/{conversation.id}/asset-renames/prepare",
        headers=headers,
        json={"asset_id": asset.id, "target_display_name": "正面参考图"},
    )
    assert prepared.status_code == 200, prepared.text
    applied = client.post(
        f"/api/internal/v1/agent-conversations/{conversation.id}/asset-renames",
        headers={**headers, "Idempotency-Key": "internal-rename-1"},
        json=prepared.json(),
    )
    assert applied.status_code == 200, applied.text
    assert applied.json()["display_name"] == "正面参考图"
    reconciled = client.post(
        f"/api/internal/v1/agent-conversations/{conversation.id}/asset-renames/reconcile",
        headers={**headers, "Idempotency-Key": "internal-rename-1"},
        json=prepared.json(),
    )
    assert reconciled.status_code == 200, reconciled.text
    assert reconciled.json()["state"] == "applied"


class _FakeAgentGateway:
    def __init__(self) -> None:
        self.run_id = ""
        self.turn_id = "harness-turn-api"
        self.status = AgentTurnStatus.REQUIRES_INPUT
        self.start_calls: list[dict] = []
        self.event_cursors: list[int] = []

    def state(self) -> AgentServiceTurnState:
        question = (
            AgentServiceQuestion(
                id="question-1",
                header="图片文字",
                question="图片中的文字使用哪种语言？",
                options=[AgentServiceQuestionOption(label="中文")],
            )
            if self.status == AgentTurnStatus.REQUIRES_INPUT
            else None
        )
        return AgentServiceTurnState(
            api_version="v1alpha1",
            run_id=self.run_id,
            turn_id=self.turn_id,
            status=self.status,
            question=question,
            created_at=datetime.now(UTC),
            updated_at=datetime.now(UTC),
        )

    def start_turn(self, **kwargs) -> AgentServiceTurnState:
        self.start_calls.append(kwargs)
        return self.state()

    def get_turn(self, **_kwargs) -> AgentServiceTurnState:
        return self.state()

    def answer_question(self, **_kwargs) -> AgentServiceTurnState:
        self.status = AgentTurnStatus.QUEUED
        return self.state()

    def resume_turn(self, **_kwargs) -> AgentServiceTurnState:
        self.status = AgentTurnStatus.RUNNING
        return self.state()

    def cancel_turn(self, **_kwargs) -> AgentServiceTurnState:
        self.status = AgentTurnStatus.CANCELED
        return self.state()

    async def stream_turn_events(self, **kwargs):
        self.event_cursors.append(kwargs["after"])
        yield b'id: 8\nevent: text.delta\ndata: {"delta":"ok"}\n\n'


class _ArtifactAgentGateway(_FakeAgentGateway):
    def __init__(self, payload: dict) -> None:
        super().__init__()
        self.payload = payload
        self.status = AgentTurnStatus.AWAITING_CONFIRMATION

    def state(self) -> AgentServiceTurnState:
        return AgentServiceTurnState(
            api_version="v1alpha1",
            run_id=self.run_id,
            turn_id=self.turn_id,
            status=self.status,
            artifact=AgentServiceArtifact(
                name="propose_workflow_draft",
                value=self.payload,
                step_id="worker-artifact-step",
            ),
            output="草案已准备完成",
            created_at=datetime.now(UTC),
            updated_at=datetime.now(UTC),
            finished_at=datetime.now(UTC),
        )


def test_public_agent_routes_keep_session_scope_idempotency_question_and_sse(
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app
    from productflow_backend.presentation.routes import agent_conversations as agent_routes

    product, asset, draft, _ = _create_product_and_draft(db_session)
    other_product, _, _, _ = _create_product_and_draft(db_session, name="公开 API 其他商品")
    gateway = _FakeAgentGateway()
    enqueued: list[str] = []
    monkeypatch.setattr(agent_routes, "_agent_gateway_or_raise", lambda: gateway)
    monkeypatch.setattr(agent_routes, "enqueue_agent_turn_sync", enqueued.append)
    client = TestClient(create_app())
    collection_path = f"/api/v2/products/{product.id}/agent-conversations"

    unauthorized = client.post(collection_path, json={"workflow_draft_id": draft.id})
    assert unauthorized.status_code == 401
    _login(client)
    created = client.post(collection_path, json={"workflow_draft_id": draft.id})
    assert created.status_code == 201, created.text
    conversation = created.json()
    gateway.run_id = conversation["harness_run_id"]

    turn_path = f"{collection_path}/{conversation['id']}/turns"
    submitted = client.post(
        turn_path,
        json={
            "input_text": "请完善工作流",
            "asset_ids": [asset.id],
            "idempotency_key": "public-turn-1",
        },
    )
    assert submitted.status_code == 202, submitted.text
    body = submitted.json()
    assert body["created"] is True
    assert body["turn"]["status"] == "requires_input"
    assert body["turn"]["question"]["id"] == "question-1"
    projection_id = body["turn"]["id"]
    assert gateway.start_calls[0]["idempotency_key"] == projection_id
    assert gateway.start_calls[0]["asset_ids"] == [asset.id]
    assert enqueued == [projection_id]

    repeated = client.post(
        turn_path,
        json={
            "input_text": "请完善工作流",
            "asset_ids": [asset.id],
            "idempotency_key": "public-turn-1",
        },
    )
    assert repeated.status_code == 202, repeated.text
    assert repeated.json()["created"] is False
    assert repeated.json()["turn"]["id"] == projection_id
    assert len(gateway.start_calls) == 1

    cross_scope = client.get(
        f"/api/v2/products/{other_product.id}/agent-conversations/{conversation['id']}"
    )
    assert cross_scope.status_code == 404

    answered = client.post(
        f"{turn_path}/{projection_id}/questions/question-1/answer",
        json={"option": 0},
    )
    assert answered.status_code == 200, answered.text
    assert answered.json()["status"] == "queued"
    assert answered.json()["resume_required"] is True

    def fail_enqueue(_: str) -> None:
        raise RuntimeError("queue unavailable")

    monkeypatch.setattr(agent_routes, "enqueue_agent_turn_sync", fail_enqueue)
    failed_resume = client.post(f"{turn_path}/{projection_id}/resume")
    assert failed_resume.status_code == 503, failed_resume.text
    persisted_after_failed_enqueue = client.get(f"{turn_path}/{projection_id}")
    assert persisted_after_failed_enqueue.status_code == 200
    assert persisted_after_failed_enqueue.json()["status"] == "running"
    assert persisted_after_failed_enqueue.json()["resume_required"] is False
    assert "无法入队" in persisted_after_failed_enqueue.json()["sync_error"]

    monkeypatch.setattr(agent_routes, "enqueue_agent_turn_sync", enqueued.append)
    resumed = client.post(f"{turn_path}/{projection_id}/resume")
    assert resumed.status_code == 200, resumed.text
    assert resumed.json()["status"] == "running"
    assert resumed.json()["resume_required"] is False
    assert resumed.json()["sync_error"] is None

    events = client.get(
        f"{turn_path}/{projection_id}/events?after=3",
        headers={"Last-Event-ID": "7"},
    )
    assert events.status_code == 200, events.text
    assert events.headers["content-type"].startswith("text/event-stream")
    assert events.text == 'id: 8\nevent: text.delta\ndata: {"delta":"ok"}\n\n'
    assert gateway.event_cursors == [7]

    canceled = client.post(f"{turn_path}/{projection_id}/cancel")
    assert canceled.status_code == 200, canceled.text
    assert canceled.json()["status"] == "canceled"


def test_sync_worker_recovers_unbound_turn_and_idempotently_attaches_artifact(db_session) -> None:
    product, asset, draft, payload = _create_product_and_draft(db_session)
    conversation = create_agent_conversation(
        db_session,
        product_id=product.id,
        workflow_draft_id=draft.id,
    )
    projection = reserve_agent_turn(
        db_session,
        product_id=product.id,
        conversation_id=conversation.id,
        input_text="浏览器关闭后继续生成草案",
        input_asset_ids=[asset.id],
        idempotency_key="worker-turn-1",
    ).projection
    gateway = _ArtifactAgentGateway(payload)
    gateway.run_id = conversation.harness_run_id
    delayed: list[tuple[str, int]] = []

    recovery_enqueued: list[str] = []
    summary = recover_unfinished_agent_turn_syncs(enqueue=recovery_enqueued.append)
    assert summary.pending_turns == 1
    assert summary.enqueued_turns == 1
    assert recovery_enqueued == [projection.id]

    execute_agent_turn_sync(
        projection.id,
        gateway=gateway,
        enqueue_later=lambda target_id, delay_ms: delayed.append((target_id, delay_ms)),
    )
    db_session.expire_all()
    projection = db_session.get(AgentTurnProjection, projection.id)
    assert projection is not None
    assert projection.harness_turn_id == gateway.turn_id
    assert projection.status == AgentTurnStatus.AWAITING_CONFIRMATION
    assert projection.workflow_draft_revision_id is not None
    assert projection.sync_error is None
    assert delayed == []
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraftRevision)) == 2

    execute_agent_turn_sync(
        projection.id,
        gateway=gateway,
        enqueue_later=lambda target_id, delay_ms: delayed.append((target_id, delay_ms)),
    )
    db_session.expire_all()
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraftRevision)) == 2
    assert recover_unfinished_agent_turn_syncs(enqueue=recovery_enqueued.append).pending_turns == 0

    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)
    confirmed = client.post(
        f"/api/v2/products/{product.id}/workflow-drafts/{draft.id}/confirm",
        json={"expected_draft_version": 2},
    )
    assert confirmed.status_code == 200, confirmed.text
    db_session.expire_all()
    refreshed_conversation = db_session.get(AgentConversation, conversation.id)
    assert refreshed_conversation is not None
    assert refreshed_conversation.status == AgentConversationStatus.COMPLETED


def test_agent_projection_tables_are_removed_without_touching_existing_rows(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    database_path = tmp_path / "workflow-agent-migration.db"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(tmp_path / "storage"))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "20260812_0033")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    now = "2026-08-12 12:00:00"
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO products (id, name, created_at, updated_at) "
                "VALUES ('product-agent', '迁移保留商品', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_drafts "
                "(id, product_id, status, created_at, updated_at) "
                "VALUES ('draft-agent', 'product-agent', 'collecting', :now, :now)"
            ),
            {"now": now},
        )

    command.upgrade(config, "head")
    inspector = sa.inspect(engine)
    assert {
        "agent_conversations",
        "agent_turn_projections",
        "agent_tool_mutations",
    }.issubset(inspector.get_table_names())
    conversation_fks = {
        foreign_key["name"]: foreign_key
        for foreign_key in inspector.get_foreign_keys("agent_conversations")
    }
    assert conversation_fks["fk_agent_conversations_product_id"]["options"]["ondelete"] == "CASCADE"
    assert conversation_fks["fk_agent_conversations_workflow_draft_id"]["options"]["ondelete"] == "CASCADE"
    turn_constraints = {constraint["name"] for constraint in inspector.get_unique_constraints("agent_turn_projections")}
    assert "uq_agent_turn_projections_conversation_key" in turn_constraints
    assert "uq_agent_turn_projections_harness_turn_id" in turn_constraints

    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO agent_conversations "
                "(id, product_id, workflow_draft_id, harness_run_id, status, created_at, updated_at) "
                "VALUES ('conversation-agent', 'product-agent', 'draft-agent', 'run-agent', "
                "'collecting', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO agent_turn_projections "
                "(id, conversation_id, idempotency_key, request_hash, input_text, "
                "input_asset_ids_json, status, resume_required, created_at, updated_at) "
                "VALUES ('turn-agent', 'conversation-agent', 'key-agent', :hash, '核对商品', "
                "'[]', 'queued', 0, :now, :now)"
            ),
            {"hash": "a" * 64, "now": now},
        )
    with pytest.raises(sa.exc.IntegrityError):
        with engine.begin() as connection:
            connection.execute(
                sa.text(
                    "INSERT INTO agent_turn_projections "
                    "(id, conversation_id, idempotency_key, request_hash, input_text, "
                    "input_asset_ids_json, status, resume_required, created_at, updated_at) "
                    "VALUES ('turn-agent-2', 'conversation-agent', 'key-agent', :hash, '不同请求', "
                    "'[]', 'queued', 0, :now, :now)"
                ),
                {"hash": "b" * 64, "now": now},
            )

    engine.dispose()
    command.downgrade(config, "20260812_0033")
    engine = sa.create_engine(f"sqlite:///{database_path}")
    inspector = sa.inspect(engine)
    assert "agent_conversations" not in inspector.get_table_names()
    with engine.connect() as connection:
        assert connection.scalar(sa.text("SELECT name FROM products WHERE id = 'product-agent'")) == "迁移保留商品"
        assert connection.scalar(sa.text("SELECT id FROM workflow_drafts WHERE id = 'draft-agent'")) == "draft-agent"
    engine.dispose()

    command.upgrade(config, "head")
    get_settings.cache_clear()
