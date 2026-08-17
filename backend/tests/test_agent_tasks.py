from __future__ import annotations

from fastapi.testclient import TestClient
from helpers import _login
from sqlalchemy import select
from test_agent_sessions import _create_workspace

from productflow_backend.application.agent_conversations import reserve_agent_turn
from productflow_backend.application.agent_sync import recover_unfinished_agent_turn_syncs
from productflow_backend.application.agent_tasks import (
    create_agent_task,
    list_agent_tasks,
)
from productflow_backend.application.agent_tools import (
    inspect_agent_global_products,
    list_agent_global_products,
)
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import AgentConversationScope, AgentTaskStatus, AgentTurnStatus
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentPageContextSnapshot,
    AgentTask,
    AgentTurnProjection,
)
from productflow_backend.presentation.api import create_app


def test_tasks_have_independent_harness_runs_and_share_a_session(db_session) -> None:
    workspace = _create_workspace(db_session, key="task-independent-runs")
    session_id = workspace.conversation.session_id

    first = create_agent_task(
        db_session,
        session_id=session_id,
        conversation_id=workspace.conversation.id,
        title="商品 A 主图",
        goal="检查商品 A 的主图素材",
    )
    second = create_agent_task(
        db_session,
        session_id=session_id,
        conversation_id=workspace.conversation.id,
        title="商品 A 场景图",
        goal="整理商品 A 的场景图",
    )

    assert first.id != second.id
    assert first.harness_run_id != second.harness_run_id
    assert first.session_id == second.session_id == session_id
    assert [task.id for task in list_agent_tasks(db_session, session_id=session_id).items] == [
        second.id,
        first.id,
    ]


def test_agent_recovery_creates_one_initial_turn_for_queued_task(db_session) -> None:
    workspace = _create_workspace(db_session, key="task-initial-recovery")
    task = create_agent_task(
        db_session,
        session_id=workspace.conversation.session_id,
        conversation_id=workspace.conversation.id,
        title="浏览器关闭后的任务",
        goal="检查商品 A 的主图素材",
    )

    enqueued: list[str] = []
    summary = recover_unfinished_agent_turn_syncs(enqueue=enqueued.append)

    assert summary.pending_turns == 1
    assert summary.enqueued_turns == 1
    assert summary.recovered_task_turns == 1
    assert enqueued
    db_session.expire_all()
    task = db_session.get(AgentTask, task.id)
    assert task is not None
    assert task.current_turn_id == enqueued[0]
    projection = db_session.get(AgentTurnProjection, enqueued[0])
    assert projection is not None
    assert projection.task_id == task.id
    assert projection.input_text == task.goal
    assert projection.idempotency_key == f"initial:{workspace.conversation.id}:{task.id}"

    second_enqueued: list[str] = []
    second = recover_unfinished_agent_turn_syncs(enqueue=second_enqueued.append)
    assert second.pending_turns == 1
    assert second.enqueued_turns == 1
    assert second.recovered_task_turns == 0
    assert second_enqueued == enqueued


def test_global_agent_can_list_and_inspect_products_with_active_workflow_summary(db_session) -> None:
    first = _create_workspace(db_session, key="global-products-first")
    second = _create_workspace(db_session, key="global-products-second")
    global_conversation = db_session.scalar(
        select(AgentConversation).where(
            AgentConversation.session_id == first.conversation.session_id,
            AgentConversation.scope_type == AgentConversationScope.GLOBAL,
        )
    )
    assert global_conversation is not None

    page = list_agent_global_products(
        db_session,
        conversation_id=global_conversation.id,
        query="Agent Session 商品",
        limit=1,
    )
    assert len(page.items) == 1
    assert page.next_cursor is not None

    next_page = list_agent_global_products(
        db_session,
        conversation_id=global_conversation.id,
        query="Agent Session 商品",
        cursor=page.next_cursor,
        limit=1,
    )
    assert len(next_page.items) == 1
    assert {page.items[0]["id"], next_page.items[0]["id"]} == {
        first.product.id,
        second.product.id,
    }

    inspected = inspect_agent_global_products(
        db_session,
        conversation_id=global_conversation.id,
        product_ids=[first.product.id, second.product.id],
    )
    assert [item["id"] for item in inspected] == [first.product.id, second.product.id]
    assert all(item["active_workflow"] is None for item in inspected)


def test_turn_reservation_persists_task_and_bounded_page_context(db_session) -> None:
    workspace = _create_workspace(db_session, key="task-context")
    task = create_agent_task(
        db_session,
        session_id=workspace.conversation.session_id,
        conversation_id=workspace.conversation.id,
        title="检查页面上下文",
        goal="验证 Task 和页面快照的关联",
    )
    context = {
        "route": f"/products/{workspace.product.id}",
        "page_type": "product_workbench",
        "product_id": workspace.product.id,
        "workflow_id": None,
        "selected_asset_ids": [],
        "visible_asset_ids": [],
        "filters": {"tab": "agent"},
        "workflow_revision": 2,
        "library_revision": 3,
        "captured_at": "2026-08-17T12:00:00+00:00",
    }

    reservation = reserve_agent_turn(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        input_text="检查当前商品素材",
        input_asset_ids=[],
        idempotency_key="task-context-turn",
        task_id=task.id,
        page_context=context,
    )

    assert reservation.created is True
    assert reservation.projection.task_id is not None
    task = db_session.get(AgentTask, reservation.projection.task_id)
    assert task is not None
    assert task.status == AgentTaskStatus.QUEUED
    snapshot = db_session.get(
        AgentPageContextSnapshot,
        reservation.projection.page_context_snapshot_id,
    )
    assert snapshot is not None
    assert snapshot.task_id == task.id
    assert snapshot.turn_id == reservation.projection.id
    assert snapshot.route == context["route"]
    assert snapshot.filters_json == {"tab": "agent"}
    assert len(snapshot.digest) == 64

    duplicate = reserve_agent_turn(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        input_text="检查当前商品素材",
        input_asset_ids=[],
        idempotency_key="task-context-turn",
        task_id=task.id,
        page_context={**context, "captured_at": "2026-08-17T12:00:01+00:00"},
    )
    assert duplicate.created is False
    assert duplicate.projection.id == reservation.projection.id
    assert db_session.query(AgentTask).count() == 1


def test_page_context_snapshot_can_belong_to_a_legacy_conversation_turn(db_session) -> None:
    workspace = _create_workspace(db_session, key="legacy-page-context")
    reservation = reserve_agent_turn(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        input_text="查看当前页面",
        input_asset_ids=[],
        idempotency_key="legacy-page-context-turn",
        page_context={
            "route": f"/products/{workspace.product.id}",
            "page_type": "product_workbench",
            "product_id": workspace.product.id,
            "workflow_id": None,
            "selected_asset_ids": [],
            "visible_asset_ids": [],
            "filters": {},
            "workflow_revision": None,
            "library_revision": None,
            "captured_at": "2026-08-17T12:00:00+00:00",
        },
    )

    assert reservation.projection.task_id is None
    assert reservation.projection.page_context_snapshot_id is not None
    snapshot = db_session.get(AgentPageContextSnapshot, reservation.projection.page_context_snapshot_id)
    assert snapshot is not None
    assert snapshot.task_id is None


def test_one_task_rejects_a_second_active_turn(db_session) -> None:
    workspace = _create_workspace(db_session, key="task-serial-turns")
    task = create_agent_task(
        db_session,
        session_id=workspace.conversation.session_id,
        conversation_id=workspace.conversation.id,
        title="串行任务",
        goal="同一个目标按顺序执行",
    )
    reserve_agent_turn(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        task_id=task.id,
        input_text="执行第一步",
        input_asset_ids=[],
        idempotency_key="serial-turn-1",
    )

    try:
        reserve_agent_turn(
            db_session,
            product_id=workspace.product.id,
            conversation_id=workspace.conversation.id,
            task_id=task.id,
            input_text="同时执行第二步",
            input_asset_ids=[],
            idempotency_key="serial-turn-2",
        )
    except ConflictError as exc:
        assert str(exc) == "当前 Agent Task 仍有未结束的 Turn"
    else:
        raise AssertionError("expected one active Turn per Agent Task")


def test_agent_task_api_lists_creates_and_renames(configured_env) -> None:
    from productflow_backend.infrastructure.db.session import get_session_factory

    factory = get_session_factory()
    with factory() as session:
        workspace = _create_workspace(session, key="task-api")
        session_id = workspace.conversation.session_id
        conversation_id = workspace.conversation.id

    client = TestClient(create_app())
    _login(client)
    create_response = client.post(
        "/api/v2/agent-tasks",
        json={
            "session_id": session_id,
            "conversation_id": conversation_id,
            "title": "检查工作流运行",
            "goal": "查看这个商品最近的 WorkflowRun",
        },
    )
    assert create_response.status_code == 201, create_response.text
    task = create_response.json()
    assert task["session_id"] == session_id
    assert task["conversation_id"] == conversation_id
    assert task["status"] == "queued"

    list_response = client.get(f"/api/v2/agent-tasks?session_id={session_id}")
    assert list_response.status_code == 200, list_response.text
    assert list_response.json()["items"][0]["id"] == task["id"]

    rename_response = client.patch(
        f"/api/v2/agent-tasks/{task['id']}",
        json={"title": "检查最近运行"},
    )
    assert rename_response.status_code == 200, rename_response.text
    assert rename_response.json()["title"] == "检查最近运行"

    cancel_response = client.post(f"/api/v2/agent-tasks/{task['id']}/cancel")
    assert cancel_response.status_code == 200, cancel_response.text
    assert cancel_response.json()["status"] == "canceled"


def test_canceling_an_unbound_task_turn_updates_conversation_status(db_session) -> None:
    workspace = _create_workspace(db_session, key="task-cancel-unbound")
    task = create_agent_task(
        db_session,
        session_id=workspace.conversation.session_id,
        conversation_id=workspace.conversation.id,
        title="取消排队任务",
        goal="验证排队 Turn 的取消状态一致",
    )
    reservation = reserve_agent_turn(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        task_id=task.id,
        input_text="取消我",
        input_asset_ids=[],
        idempotency_key="cancel-unbound-turn",
    )

    from productflow_backend.application.agent_tasks import cancel_agent_task

    cancel_agent_task(db_session, task_id=task.id)

    db_session.refresh(reservation.projection.conversation)
    assert reservation.projection.status == AgentTurnStatus.CANCELED
    assert reservation.projection.conversation.status.value == "canceled"


def test_internal_agent_task_contract_uses_task_run(configured_env, monkeypatch) -> None:
    from productflow_backend.infrastructure.db.session import get_session_factory

    factory = get_session_factory()
    with factory() as session:
        workspace = _create_workspace(session, key="task-contract")
        task = create_agent_task(
            session,
            session_id=workspace.conversation.session_id,
            conversation_id=workspace.conversation.id,
            title="任务运行契约",
            goal="验证 Task 使用独立 harness run",
        )

    internal_token = "agent-internal-token-with-at-least-32-characters"
    monkeypatch.setenv("AGENT_SERVICE_INTERNAL_TOKEN", internal_token)
    get_settings.cache_clear()
    client = TestClient(create_app())
    path = f"/api/internal/v1/agent-tasks/{task.id}/contract"

    assert client.get(path).status_code == 401
    response = client.get(path, headers={"Authorization": f"Bearer {internal_token}"})

    assert response.status_code == 200, response.text
    payload = response.json()
    assert payload["task_id"] == task.id
    assert payload["conversation_id"] == task.conversation_id
    assert payload["harness_run_id"] == task.harness_run_id
    assert payload["harness_run_id"] != task.conversation_id

    runs_path = f"/api/internal/v1/agent-conversations/{task.conversation_id}/workflow-runs?limit=5"
    runs_response = client.get(runs_path, headers={"Authorization": f"Bearer {internal_token}"})
    assert runs_response.status_code == 200, runs_response.text
    assert runs_response.json() == {
        "workflow_id": None,
        "workflow_revision": 0,
        "items": [],
    }
