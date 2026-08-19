from __future__ import annotations

from datetime import timedelta

from fastapi.testclient import TestClient
from helpers import _login
from sqlalchemy import select
from test_agent_sessions import _create_workspace

from productflow_backend.application.agent_conversations import reserve_agent_turn
from productflow_backend.application.agent_execution import (
    append_agent_turn_checkpoint,
    claim_agent_turn_execution,
    heartbeat_agent_turn_execution,
    recover_expired_agent_turn_executions,
    release_agent_turn_execution,
)
from productflow_backend.application.agent_sync import recover_unfinished_agent_turn_syncs
from productflow_backend.application.agent_tasks import (
    create_agent_task,
    list_agent_tasks,
    pause_agent_task,
    resume_agent_task,
)
from productflow_backend.application.agent_tools import (
    inspect_agent_global_products,
    list_agent_global_products,
)
from productflow_backend.application.time import now_utc
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import (
    AgentCheckpointKind,
    AgentConversationScope,
    AgentExecutionPhase,
    AgentTaskStatus,
    AgentTurnStatus,
)
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentPageContextSnapshot,
    AgentSession,
    AgentTask,
    AgentTurnCheckpoint,
    AgentTurnExecution,
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


def test_task_list_uses_cursor_and_session_summary(db_session) -> None:
    workspace = _create_workspace(db_session, key="task-cursor")
    session_id = workspace.conversation.session_id
    tasks = [
        create_agent_task(
            db_session,
            session_id=session_id,
            conversation_id=workspace.conversation.id,
            title=f"任务 {index}",
            goal=f"检查第 {index} 个任务",
        )
        for index in range(3)
    ]

    first = list_agent_tasks(db_session, session_id=session_id, limit=1)
    assert [task.id for task in first.items] == [tasks[-1].id]
    assert first.next_cursor is not None
    second = list_agent_tasks(
        db_session,
        session_id=session_id,
        limit=1,
        after=first.next_cursor,
    )
    assert [task.id for task in second.items] == [tasks[-2].id]
    assert second.next_cursor is not None
    third = list_agent_tasks(
        db_session,
        session_id=session_id,
        limit=1,
        after=second.next_cursor,
    )
    assert [task.id for task in third.items] == [tasks[-3].id]
    assert third.next_cursor is None
    agent_session = db_session.get(AgentSession, session_id)
    assert agent_session is not None
    assert agent_session.summary is not None
    assert "任务 3 个" in agent_session.summary


def test_task_can_pause_before_first_turn_and_resume_idempotently(db_session) -> None:
    workspace = _create_workspace(db_session, key="task-pause-before-turn")
    task = create_agent_task(
        db_session,
        session_id=workspace.conversation.session_id,
        conversation_id=workspace.conversation.id,
        title="可暂停任务",
        goal="等待用户确认后再开始检查",
    )

    paused = pause_agent_task(db_session, task_id=task.id)
    assert paused.status == AgentTaskStatus.PAUSED
    assert paused.waiting_reason == "user_paused"
    session = db_session.get(AgentSession, workspace.conversation.session_id)
    assert session is not None
    assert "未完成 1 个" in (session.summary or "")

    recovery_enqueued: list[str] = []
    recovery = recover_unfinished_agent_turn_syncs(enqueue=recovery_enqueued.append)
    assert recovery.recovered_task_turns == 0
    assert recovery_enqueued == []

    resumed = resume_agent_task(db_session, task_id=task.id)
    assert resumed.task.status == AgentTaskStatus.QUEUED
    assert resumed.projection_id is not None
    projection = db_session.get(AgentTurnProjection, resumed.projection_id)
    assert projection is not None
    assert projection.task_id == task.id
    assert projection.idempotency_key == f"initial:{workspace.conversation.id}:{task.id}"

    repeated = resume_agent_task(db_session, task_id=task.id)
    assert repeated.projection_id is None
    assert repeated.task.status == AgentTaskStatus.QUEUED


def test_running_task_cannot_be_paused(db_session) -> None:
    workspace = _create_workspace(db_session, key="task-pause-running")
    task = create_agent_task(
        db_session,
        session_id=workspace.conversation.session_id,
        conversation_id=workspace.conversation.id,
        title="运行中任务",
        goal="验证运行中任务不能伪装成已暂停",
    )
    reservation = reserve_agent_turn(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        task_id=task.id,
        input_text="开始运行",
        input_asset_ids=[],
        idempotency_key="pause-running-turn",
    )

    try:
        pause_agent_task(db_session, task_id=task.id)
    except ConflictError as exc:
        assert "需要先取消" in str(exc)
    else:
        raise AssertionError("expected a running task to reject pause")
    assert db_session.get(AgentTurnProjection, reservation.projection.id).status == AgentTurnStatus.QUEUED


def test_agent_turn_execution_claims_are_fenced_and_idempotent(db_session) -> None:
    workspace = _create_workspace(db_session, key="agent-execution-lease")
    reservation = reserve_agent_turn(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        input_text="执行一次可恢复检查",
        input_asset_ids=[],
        idempotency_key="execution-lease-turn",
    )

    first = claim_agent_turn_execution(
        db_session,
        conversation_id=workspace.conversation.id,
        task_id=None,
        idempotency_key=reservation.projection.idempotency_key,
        harness_turn_id="harness-turn-1",
        owner_id="agent-instance-1",
    )
    repeated = claim_agent_turn_execution(
        db_session,
        conversation_id=workspace.conversation.id,
        task_id=None,
        idempotency_key=reservation.projection.idempotency_key,
        harness_turn_id="harness-turn-1",
        owner_id="agent-instance-1",
    )
    assert repeated.lease_token == first.lease_token
    assert repeated.attempt == first.attempt == 1

    try:
        claim_agent_turn_execution(
            db_session,
            conversation_id=workspace.conversation.id,
            task_id=None,
            idempotency_key=reservation.projection.idempotency_key,
            harness_turn_id="harness-turn-1",
            owner_id="agent-instance-2",
        )
    except ConflictError:
        pass
    else:
        raise AssertionError("an active execution lease must fence a second owner")

    heartbeat = heartbeat_agent_turn_execution(
        db_session,
        conversation_id=workspace.conversation.id,
        execution_id=first.execution_id,
        owner_id=first.owner_id,
        lease_token=first.lease_token,
        phase=AgentExecutionPhase.TOOL,
    )
    assert heartbeat.phase == AgentExecutionPhase.TOOL
    intent = append_agent_turn_checkpoint(
        db_session,
        conversation_id=workspace.conversation.id,
        execution_id=first.execution_id,
        owner_id=first.owner_id,
        lease_token=first.lease_token,
        sequence=1,
        kind=AgentCheckpointKind.TOOL_EFFECT_INTENT,
        payload={"operation": "workflow_run_request", "idempotency_key": "effect-1"},
    )
    repeated_intent = append_agent_turn_checkpoint(
        db_session,
        conversation_id=workspace.conversation.id,
        execution_id=first.execution_id,
        owner_id=first.owner_id,
        lease_token=first.lease_token,
        sequence=1,
        kind=AgentCheckpointKind.TOOL_EFFECT_INTENT,
        payload={"operation": "workflow_run_request", "idempotency_key": "effect-1"},
    )
    assert repeated_intent.id == intent.id
    result_checkpoint = append_agent_turn_checkpoint(
        db_session,
        conversation_id=workspace.conversation.id,
        execution_id=first.execution_id,
        owner_id=first.owner_id,
        lease_token=first.lease_token,
        sequence=2,
        kind=AgentCheckpointKind.TOOL_EFFECT_RESULT,
        payload={"result": "applied"},
    )
    assert result_checkpoint.sequence == 2
    stored_checkpoints = db_session.query(AgentTurnCheckpoint).filter_by(execution_id=first.execution_id).all()
    assert [checkpoint.sequence for checkpoint in stored_checkpoints] == [1, 2]
    assert release_agent_turn_execution(
        db_session,
        conversation_id=workspace.conversation.id,
        execution_id=first.execution_id,
        owner_id=first.owner_id,
        lease_token=first.lease_token,
    )
    expired = db_session.get(AgentTurnExecution, first.execution_id)
    assert expired is not None
    expired.phase = AgentExecutionPhase.CLAIMED
    expired.owner_id = first.owner_id
    expired.lease_token = first.lease_token
    expired.lease_expires_at = now_utc() - timedelta(seconds=1)
    db_session.commit()

    second = claim_agent_turn_execution(
        db_session,
        conversation_id=workspace.conversation.id,
        task_id=None,
        idempotency_key=reservation.projection.idempotency_key,
        harness_turn_id="harness-turn-1",
        owner_id="agent-instance-2",
    )
    assert second.attempt == 2
    assert second.fencing_token == 2
    assert second.lease_token != first.lease_token


def test_agent_turn_execution_does_not_claim_a_confirmation_turn(db_session) -> None:
    workspace = _create_workspace(db_session, key="agent-execution-confirmation")
    reservation = reserve_agent_turn(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        input_text="已完成草案",
        input_asset_ids=[],
        idempotency_key="execution-confirmation-turn",
    )
    projection = db_session.get(AgentTurnProjection, reservation.projection.id)
    assert projection is not None
    projection.status = AgentTurnStatus.AWAITING_CONFIRMATION
    db_session.commit()

    try:
        claim_agent_turn_execution(
            db_session,
            conversation_id=workspace.conversation.id,
            task_id=None,
            idempotency_key=projection.idempotency_key,
            harness_turn_id="confirmation-harness-turn",
            owner_id="agent-instance-1",
        )
    except ConflictError:
        pass
    else:
        raise AssertionError("a confirmation Turn must not acquire an execution lease")


def test_expired_agent_execution_requeues_unstarted_turn_and_closes_active_turn_unknown(db_session) -> None:
    queued_workspace = _create_workspace(db_session, key="agent-execution-requeue")
    queued_reservation = reserve_agent_turn(
        db_session,
        product_id=queued_workspace.product.id,
        conversation_id=queued_workspace.conversation.id,
        input_text="尚未开始",
        input_asset_ids=[],
        idempotency_key="execution-requeue-turn",
    )
    queued_lease = claim_agent_turn_execution(
        db_session,
        conversation_id=queued_workspace.conversation.id,
        task_id=None,
        idempotency_key=queued_reservation.projection.idempotency_key,
        harness_turn_id="queued-harness-turn",
        owner_id="agent-instance-1",
    )

    active_workspace = _create_workspace(db_session, key="agent-execution-unknown")
    active_reservation = reserve_agent_turn(
        db_session,
        product_id=active_workspace.product.id,
        conversation_id=active_workspace.conversation.id,
        input_text="已经开始",
        input_asset_ids=[],
        idempotency_key="execution-unknown-turn",
    )
    active_projection = db_session.get(AgentTurnProjection, active_reservation.projection.id)
    assert active_projection is not None
    active_projection.status = AgentTurnStatus.RUNNING
    active_projection.tool_steps_json = [
        {"step_id": "step-1", "kind": "inspect_context", "summary": "读取上下文", "status": "running"}
    ]
    active_lease = claim_agent_turn_execution(
        db_session,
        conversation_id=active_workspace.conversation.id,
        task_id=None,
        idempotency_key=active_reservation.projection.idempotency_key,
        harness_turn_id="active-harness-turn",
        owner_id="agent-instance-1",
    )
    active_execution = db_session.get(AgentTurnExecution, active_lease.execution_id)
    queued_execution = db_session.get(AgentTurnExecution, queued_lease.execution_id)
    assert active_execution is not None and queued_execution is not None
    active_fencing_token = active_execution.fencing_token
    active_execution.phase = AgentExecutionPhase.TOOL
    active_execution.lease_expires_at = now_utc() - timedelta(seconds=1)
    queued_execution.lease_expires_at = now_utc() - timedelta(seconds=1)
    db_session.commit()

    summary = recover_expired_agent_turn_executions(db_session)

    assert summary.requeued == 1
    assert summary.unknown == 1
    db_session.expire_all()
    recovered_queued = db_session.get(AgentTurnProjection, queued_reservation.projection.id)
    recovered_active = db_session.get(AgentTurnProjection, active_reservation.projection.id)
    recovered_active_execution = db_session.get(AgentTurnExecution, active_lease.execution_id)
    assert recovered_queued is not None and recovered_queued.status == AgentTurnStatus.QUEUED
    assert recovered_active is not None and recovered_active.status == AgentTurnStatus.UNKNOWN
    assert recovered_active.tool_steps_json[0]["status"] == "unknown"
    recovery_checkpoint = db_session.scalar(
        select(AgentTurnCheckpoint).where(AgentTurnCheckpoint.execution_id == active_lease.execution_id)
    )
    assert recovery_checkpoint is not None
    assert recovery_checkpoint.kind == AgentCheckpointKind.TERMINAL
    assert recovery_checkpoint.payload_json["status"] == AgentTurnStatus.UNKNOWN.value
    assert recovered_active_execution is not None
    assert recovered_active_execution.fencing_token == active_fencing_token + 1


def test_before_model_checkpoint_blocks_expired_queued_turn_requeue(db_session) -> None:
    workspace = _create_workspace(db_session, key="agent-execution-before-model-checkpoint")
    reservation = reserve_agent_turn(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        input_text="模型边界已记录",
        input_asset_ids=[],
        idempotency_key="execution-before-model-checkpoint-turn",
    )
    lease = claim_agent_turn_execution(
        db_session,
        conversation_id=workspace.conversation.id,
        task_id=None,
        idempotency_key=reservation.projection.idempotency_key,
        harness_turn_id="before-model-harness-turn",
        owner_id="agent-instance-1",
    )
    append_agent_turn_checkpoint(
        db_session,
        conversation_id=workspace.conversation.id,
        execution_id=lease.execution_id,
        owner_id=lease.owner_id,
        lease_token=lease.lease_token,
        sequence=1,
        kind=AgentCheckpointKind.BEFORE_MODEL_REQUEST,
        payload={"attempt": lease.attempt, "fencing_token": lease.fencing_token},
    )
    execution = db_session.get(AgentTurnExecution, lease.execution_id)
    assert execution is not None
    execution.lease_expires_at = now_utc() - timedelta(seconds=1)
    db_session.commit()

    summary = recover_expired_agent_turn_executions(db_session)

    assert summary.requeued == 0
    assert summary.unknown == 1
    db_session.expire_all()
    recovered = db_session.get(AgentTurnProjection, reservation.projection.id)
    assert recovered is not None and recovered.status == AgentTurnStatus.UNKNOWN


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

    pause_response = client.post(f"/api/v2/agent-tasks/{task['id']}/pause")
    assert pause_response.status_code == 200, pause_response.text
    assert pause_response.json()["status"] == "paused"
    assert pause_response.json()["waiting_reason"] == "user_paused"

    resume_response = client.post(f"/api/v2/agent-tasks/{task['id']}/resume")
    assert resume_response.status_code == 200, resume_response.text
    assert resume_response.json()["status"] == "queued"
    assert resume_response.json()["current_turn_id"]

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

    runtime_context_path = f"/api/internal/v1/agent-conversations/{task.conversation_id}/runtime-context"
    runtime_context_response = client.get(
        runtime_context_path,
        params={"task_id": task.id},
        headers={"Authorization": f"Bearer {internal_token}"},
    )
    assert runtime_context_response.status_code == 200, runtime_context_response.text
    runtime_context = runtime_context_response.json()
    assert runtime_context["schema_version"] == 1
    assert runtime_context["session_id"] == task.session_id
    assert runtime_context["conversation_id"] == task.conversation_id
    assert runtime_context["task_id"] == task.id
    assert runtime_context["session_summary"]
    assert runtime_context["task_summary"] == task.summary
