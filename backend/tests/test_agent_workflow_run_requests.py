from __future__ import annotations

from datetime import UTC, datetime

import pytest
from helpers import _make_demo_image_bytes
from sqlalchemy import func, select
from workflow_draft_helpers import make_workflow_draft_payload

from productflow_backend.application.agent.control import synchronize_agent_turn_state
from productflow_backend.application.agent.conversations import bind_harness_turn, reserve_agent_turn
from productflow_backend.application.agent.product_intake import AgentProductSelectionV1
from productflow_backend.application.agent.product_workspaces import create_agent_product_workspace
from productflow_backend.application.agent.tasks import create_agent_task, get_agent_task_or_raise
from productflow_backend.application.agent.workflow_run_requests import (
    cancel_agent_workflow_run_request,
    confirm_agent_workflow_run_request,
    create_agent_global_workflow_run_request,
    create_agent_workflow_run_request,
    get_agent_workflow_run_request,
    prepare_agent_global_workflow_run_request,
    prepare_agent_workflow_run_request,
)
from productflow_backend.application.agent.workflow_runs import (
    inspect_agent_global_workflow_runs,
    list_agent_workflow_runs,
)
from productflow_backend.application.product_workflow.graph_draft_persist import persist_confirmed_draft_graph
from productflow_backend.application.product_workflow.graph_runs import submit_graph_run
from productflow_backend.application.workflow_drafts.service import (
    append_workflow_draft_revision,
    confirm_workflow_draft_revision,
)
from productflow_backend.domain.enums import (
    AgentConversationScope,
    AgentTaskStatus,
    AgentToolStepKind,
    AgentToolStepStatus,
    AgentTurnStatus,
    AgentWorkflowRunRequestStatus,
    GraphRunScope,
    WorkflowNodeStatus,
    WorkflowRunStatus,
)
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.agent_service import AgentServiceToolStep, AgentServiceTurnState
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentWorkflowRunRequest,
    WorkflowGraphRun,
)


def _silence_graph_run_enqueue(monkeypatch) -> None:
    monkeypatch.setattr(
        "productflow_backend.application.agent.workflow_run_requests.enqueue_graph_run",
        lambda run_id: None,
    )
    monkeypatch.setattr(
        "productflow_backend.application.product_workflow.graph_runs.enqueue_graph_run",
        lambda run_id: None,
    )


def test_global_agent_can_inspect_recent_runs_for_selected_workflows(db_session, monkeypatch) -> None:
    _silence_graph_run_enqueue(monkeypatch)
    first, first_graph = _create_v3_requestable_workspace(db_session)
    _, second_graph = _create_v3_requestable_workspace(
        db_session,
        name="执行请求测试商品 2",
        idempotency_key="v3-workflow-run-request-workspace-2",
    )
    global_conversation = db_session.scalar(
        select(AgentConversation).where(
            AgentConversation.session_id == first.conversation.session_id,
            AgentConversation.scope_type == AgentConversationScope.GLOBAL,
        )
    )
    assert global_conversation is not None

    submitted = submit_graph_run(
        db_session,
        product_id=first.product.id,
        graph_id=first_graph.id,
        scope=GraphRunScope.GRAPH,
    )
    inspected = inspect_agent_global_workflow_runs(
        db_session,
        conversation_id=global_conversation.id,
        workflow_ids=[second_graph.id, first_graph.id],
        limit=1,
    )

    assert [item["workflow_id"] for item in inspected] == [second_graph.id, first_graph.id]
    assert inspected[0]["product_name"] == "执行请求测试商品 2"
    assert inspected[0]["runs"] == []
    assert inspected[1]["runs"][0]["id"] == submitted.run.id
    assert inspected[1]["runs"][0]["status"] == WorkflowRunStatus.RUNNING
    assert inspected[1]["runs"][0]["node_status_counts"]


def test_agent_workflow_run_request_waits_for_confirmation_and_reuses_run_chain(db_session, monkeypatch) -> None:
    _silence_graph_run_enqueue(monkeypatch)
    workspace, workflow = _create_v3_requestable_workspace(db_session)
    task = create_agent_task(
        db_session,
        session_id=workspace.conversation.session_id,
        title="执行商品工作流",
        goal="执行当前商品的完整工作流",
        conversation_id=workspace.conversation.id,
    )

    prepared = prepare_agent_workflow_run_request(
        db_session,
        conversation_id=workspace.conversation.id,
        expected_workflow_revision=workflow.revision,
        task_id=task.id,
    )
    assert prepared.workflow_id == workflow.id
    assert prepared.runnable_node_count > 0

    request = create_agent_workflow_run_request(
        db_session,
        conversation_id=workspace.conversation.id,
        expected_workflow_revision=prepared.workflow_revision,
        workflow_id=prepared.workflow_id,
        source_step_id="run-request-step-1",
        idempotency_key="run-request-key-1",
        task_id=task.id,
    )
    assert request.status == AgentWorkflowRunRequestStatus.AWAITING_CONFIRMATION
    assert request.graph_run_id is None
    persisted_task = db_session.get(type(task), task.id)
    assert persisted_task.status == AgentTaskStatus.AWAITING_CONFIRMATION
    assert db_session.scalar(select(func.count()).select_from(WorkflowGraphRun)) == 0

    replay = create_agent_workflow_run_request(
        db_session,
        conversation_id=workspace.conversation.id,
        expected_workflow_revision=prepared.workflow_revision,
        workflow_id=prepared.workflow_id,
        source_step_id="run-request-step-1",
        idempotency_key="run-request-key-1",
        task_id=task.id,
    )
    assert replay.id == request.id
    assert db_session.scalar(select(func.count()).select_from(AgentWorkflowRunRequest)) == 1

    confirmed = confirm_agent_workflow_run_request(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        request_id=request.id,
    )
    assert confirmed.status == AgentWorkflowRunRequestStatus.CONFIRMED
    assert confirmed.graph_run_id is not None
    assert db_session.get(WorkflowGraphRun, confirmed.graph_run_id) is not None
    assert confirmed.graph_run is not None
    assert confirmed.graph_run.status == WorkflowRunStatus.RUNNING
    assert db_session.scalar(select(func.count()).select_from(WorkflowGraphRun)) == 1
    assert db_session.get(type(task), task.id).status == AgentTaskStatus.RUNNING

    run = db_session.get(WorkflowGraphRun, confirmed.graph_run_id)
    assert run is not None
    run.status = WorkflowRunStatus.SUCCEEDED
    run.finished_at = datetime.now(UTC)
    db_session.commit()

    synchronized_task = get_agent_task_or_raise(db_session, task.id)
    assert synchronized_task.status == AgentTaskStatus.SUCCEEDED
    assert get_agent_workflow_run_request(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
    ).status == AgentWorkflowRunRequestStatus.SUCCEEDED

    confirmed_replay = confirm_agent_workflow_run_request(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        request_id=request.id,
    )
    assert confirmed_replay.graph_run_id == confirmed.graph_run_id
    assert db_session.scalar(select(func.count()).select_from(WorkflowGraphRun)) == 1


def test_global_agent_workflow_run_request_targets_explicit_product_and_reuses_run_chain(
    db_session,
    monkeypatch,
) -> None:
    _silence_graph_run_enqueue(monkeypatch)
    workspace, workflow = _create_v3_requestable_workspace(db_session)
    global_conversation = db_session.scalar(
        select(AgentConversation).where(
            AgentConversation.session_id == workspace.conversation.session_id,
            AgentConversation.scope_type == AgentConversationScope.GLOBAL,
        )
    )
    assert global_conversation is not None
    task = create_agent_task(
        db_session,
        session_id=global_conversation.session_id,
        title="从全局执行商品工作流",
        goal="执行商品 A 的主图工作流",
        conversation_id=global_conversation.id,
    )

    prepared = prepare_agent_global_workflow_run_request(
        db_session,
        conversation_id=global_conversation.id,
        product_id=workspace.product.id,
        workflow_id=workflow.id,
        expected_workflow_revision=workflow.revision,
        task_id=task.id,
    )
    request = create_agent_global_workflow_run_request(
        db_session,
        conversation_id=global_conversation.id,
        product_id=workspace.product.id,
        expected_workflow_revision=prepared.workflow_revision,
        workflow_id=prepared.workflow_id,
        source_step_id="global-run-request-step",
        idempotency_key="global-run-request-key",
        task_id=task.id,
    )

    assert request.conversation_id == global_conversation.id
    assert request.product_id == workspace.product.id
    assert request.status == AgentWorkflowRunRequestStatus.AWAITING_CONFIRMATION
    assert db_session.get(type(task), task.id).status == AgentTaskStatus.AWAITING_CONFIRMATION

    confirmed = confirm_agent_workflow_run_request(
        db_session,
        product_id=None,
        conversation_id=global_conversation.id,
        request_id=request.id,
    )
    assert confirmed.status == AgentWorkflowRunRequestStatus.CONFIRMED
    assert confirmed.graph_run_id is not None
    assert confirmed.graph_run is not None
    assert confirmed.graph_run.graph_id == workflow.id
    assert confirmed.graph_run.status == WorkflowRunStatus.RUNNING
    assert get_agent_workflow_run_request(
        db_session,
        product_id=None,
        conversation_id=global_conversation.id,
        task_id=task.id,
    ).product_id == workspace.product.id
    assert db_session.get(type(task), task.id).status == AgentTaskStatus.RUNNING


def test_global_agent_turn_projects_workflow_run_request_for_dock_confirmation(db_session) -> None:
    workspace, workflow = _create_v3_requestable_workspace(db_session)
    global_conversation = db_session.scalar(
        select(AgentConversation).where(
            AgentConversation.session_id == workspace.conversation.session_id,
            AgentConversation.scope_type == AgentConversationScope.GLOBAL,
        )
    )
    assert global_conversation is not None
    task = create_agent_task(
        db_session,
        session_id=global_conversation.session_id,
        title="等待全局执行确认",
        goal="等待人工确认后执行商品 A 的工作流",
        conversation_id=global_conversation.id,
    )
    projection = reserve_agent_turn(
        db_session,
        product_id=None,
        conversation_id=global_conversation.id,
        input_text="执行商品 A 的工作流",
        input_asset_ids=[],
        idempotency_key="global-run-request-turn",
        task_id=task.id,
    ).projection
    projection = bind_harness_turn(
        db_session,
        product_id=None,
        conversation_id=global_conversation.id,
        projection_id=projection.id,
        harness_turn_id="global-run-request-harness-turn",
        status=AgentTurnStatus.RUNNING,
    )
    request = create_agent_global_workflow_run_request(
        db_session,
        conversation_id=global_conversation.id,
        product_id=workspace.product.id,
        expected_workflow_revision=workflow.revision,
        workflow_id=workflow.id,
        source_step_id="global-run-request-tool-step",
        idempotency_key="global-run-request-turn-key",
        task_id=task.id,
    )

    projected = synchronize_agent_turn_state(
        db_session,
        product_id=None,
        conversation_id=global_conversation.id,
        projection_id=projection.id,
        state=AgentServiceTurnState(
            api_version="v1alpha1",
            run_id=task.harness_run_id,
            turn_id=projection.harness_turn_id or "",
            status=AgentTurnStatus.SUCCEEDED,
            tool_steps=[
                AgentServiceToolStep(
                    step_id=request.source_step_id,
                    kind=AgentToolStepKind.REQUEST_WORKFLOW_RUN,
                    summary="等待人工确认执行指定商品工作流",
                    status=AgentToolStepStatus.SUCCEEDED,
                )
            ],
            created_at=datetime.now(UTC),
            updated_at=datetime.now(UTC),
            finished_at=datetime.now(UTC),
        ),
    )

    assert projected.status == AgentTurnStatus.AWAITING_CONFIRMATION
    assert projected.workflow_run_request_id == request.id
    assert db_session.get(type(task), task.id).status == AgentTaskStatus.AWAITING_CONFIRMATION


def test_agent_workflow_run_request_rejects_stale_revision_and_can_cancel_before_confirmation(db_session) -> None:
    workspace, workflow = _create_v3_requestable_workspace(db_session)
    request = create_agent_workflow_run_request(
        db_session,
        conversation_id=workspace.conversation.id,
        expected_workflow_revision=workflow.revision,
        workflow_id=workflow.id,
        source_step_id="run-request-step-stale",
        idempotency_key="run-request-key-stale",
    )
    workflow.revision += 1
    db_session.commit()

    with pytest.raises(ConflictError, match="发生变化"):
        confirm_agent_workflow_run_request(
            db_session,
            product_id=workspace.product.id,
            conversation_id=workspace.conversation.id,
            request_id=request.id,
        )
    assert db_session.scalar(select(func.count()).select_from(WorkflowGraphRun)) == 0

    cancelled = cancel_agent_workflow_run_request(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        request_id=request.id,
    )
    assert cancelled.status == AgentWorkflowRunRequestStatus.CANCELLED
    assert cancelled.graph_run_id is None
    assert get_agent_workflow_run_request(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
    ).status == AgentWorkflowRunRequestStatus.CANCELLED


def test_agent_turn_projects_workflow_run_request_as_human_confirmation(db_session, monkeypatch) -> None:
    _silence_graph_run_enqueue(monkeypatch)
    workspace, workflow = _create_v3_requestable_workspace(db_session)
    task = create_agent_task(
        db_session,
        session_id=workspace.conversation.session_id,
        title="等待执行确认",
        goal="等待人工确认后执行工作流",
        conversation_id=workspace.conversation.id,
    )
    projection = reserve_agent_turn(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        input_text="执行当前工作流",
        input_asset_ids=[],
        idempotency_key="run-request-turn",
        task_id=task.id,
    ).projection
    projection = bind_harness_turn(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        projection_id=projection.id,
        harness_turn_id="run-request-harness-turn",
        status=AgentTurnStatus.RUNNING,
    )
    request = create_agent_workflow_run_request(
        db_session,
        conversation_id=workspace.conversation.id,
        expected_workflow_revision=workflow.revision,
        workflow_id=workflow.id,
        source_step_id="run-request-tool-step",
        idempotency_key="run-request-turn-key",
        task_id=task.id,
    )

    projected = synchronize_agent_turn_state(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        projection_id=projection.id,
        state=AgentServiceTurnState(
            api_version="v1alpha1",
            run_id=task.harness_run_id,
            turn_id=projection.harness_turn_id or "",
            status=AgentTurnStatus.SUCCEEDED,
            tool_steps=[
                AgentServiceToolStep(
                    step_id=request.source_step_id,
                    kind=AgentToolStepKind.REQUEST_WORKFLOW_RUN,
                    summary="等待人工确认执行工作流",
                    status=AgentToolStepStatus.SUCCEEDED,
                )
            ],
            created_at=datetime.now(UTC),
            updated_at=datetime.now(UTC),
            finished_at=datetime.now(UTC),
        ),
    )
    assert projected.status == AgentTurnStatus.AWAITING_CONFIRMATION
    assert projected.workflow_run_request_id == request.id
    assert db_session.get(type(task), task.id).status == AgentTaskStatus.AWAITING_CONFIRMATION

    confirmed = confirm_agent_workflow_run_request(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        request_id=request.id,
    )
    assert confirmed.status == AgentWorkflowRunRequestStatus.CONFIRMED
    assert db_session.get(type(projected), projected.id).status == AgentTurnStatus.SUCCEEDED
    assert db_session.get(type(task), task.id).status == AgentTaskStatus.RUNNING


def test_agent_can_request_workflow_run_retry(db_session, monkeypatch) -> None:
    _silence_graph_run_enqueue(monkeypatch)
    workspace, workflow = _create_v3_requestable_workspace(db_session)
    submission = submit_graph_run(
        db_session,
        product_id=workspace.product.id,
        graph_id=workflow.id,
        scope=GraphRunScope.GRAPH,
    )
    run = submission.run
    run.status = WorkflowRunStatus.FAILED
    run.is_retryable = True
    for node_run in run.node_runs:
        node_run.status = WorkflowNodeStatus.FAILED
        node_run.failure_reason = "generation_failed"
    db_session.commit()

    prepared = prepare_agent_workflow_run_request(
        db_session,
        conversation_id=workspace.conversation.id,
        expected_workflow_revision=workflow.revision,
        source_run_id=run.id,
    )
    assert prepared.source_graph_run_id == run.id
    assert prepared.workflow_id == workflow.id

    request = create_agent_workflow_run_request(
        db_session,
        conversation_id=workspace.conversation.id,
        expected_workflow_revision=workflow.revision,
        workflow_id=workflow.id,
        source_step_id="step-retry-1",
        idempotency_key="request-retry-1",
        source_run_id=run.id,
    )
    assert request.source_graph_run_id == run.id
    assert request.status == AgentWorkflowRunRequestStatus.AWAITING_CONFIRMATION

    confirmed = confirm_agent_workflow_run_request(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        request_id=request.id,
    )
    assert confirmed.status == AgentWorkflowRunRequestStatus.CONFIRMED
    assert confirmed.graph_run_id is not None
    assert confirmed.graph_run_id != run.id
    new_run = db_session.get(WorkflowGraphRun, confirmed.graph_run_id)
    assert new_run is not None
    assert new_run.id != run.id
    assert confirmed.source_graph_run_id == run.id


def test_global_agent_can_request_workflow_run_retry(db_session, monkeypatch) -> None:
    _silence_graph_run_enqueue(monkeypatch)
    workspace, workflow = _create_v3_requestable_workspace(db_session)
    submission = submit_graph_run(
        db_session,
        product_id=workspace.product.id,
        graph_id=workflow.id,
        scope=GraphRunScope.GRAPH,
    )
    run = submission.run
    run.status = WorkflowRunStatus.FAILED
    run.is_retryable = True
    for node_run in run.node_runs:
        node_run.status = WorkflowNodeStatus.FAILED
        node_run.failure_reason = "generation_failed"
    db_session.commit()

    global_conversation = db_session.scalar(
        select(AgentConversation).where(
            AgentConversation.session_id == workspace.conversation.session_id,
            AgentConversation.scope_type == AgentConversationScope.GLOBAL,
        )
    )
    assert global_conversation is not None

    prepared = prepare_agent_global_workflow_run_request(
        db_session,
        conversation_id=global_conversation.id,
        product_id=workspace.product.id,
        workflow_id=workflow.id,
        expected_workflow_revision=workflow.revision,
        source_run_id=run.id,
    )
    assert prepared.source_graph_run_id == run.id

    request = create_agent_global_workflow_run_request(
        db_session,
        conversation_id=global_conversation.id,
        product_id=workspace.product.id,
        expected_workflow_revision=workflow.revision,
        workflow_id=workflow.id,
        source_step_id="step-global-retry-1",
        idempotency_key="request-global-retry-1",
        source_run_id=run.id,
    )
    assert request.source_graph_run_id == run.id

    confirmed = confirm_agent_workflow_run_request(
        db_session,
        product_id=None,
        conversation_id=global_conversation.id,
        request_id=request.id,
    )
    assert confirmed.status == AgentWorkflowRunRequestStatus.CONFIRMED
    assert confirmed.graph_run_id is not None
    new_run = db_session.get(WorkflowGraphRun, confirmed.graph_run_id)
    assert new_run is not None
    assert new_run.id != run.id
    assert confirmed.source_graph_run_id == run.id


def _create_v3_requestable_workspace(
    db_session,
    *,
    name: str = "v3 执行请求商品",
    idempotency_key: str = "v3-workflow-run-request-workspace",
):
    selection = AgentProductSelectionV1.model_validate(
        {
            "schema_version": 1,
            "image_types": [{"key": "hero", "quantity": 2, "order": 0}],
        }
    )
    workspace = create_agent_product_workspace(
        db_session,
        name=name,
        selection=selection,
        image_uploads=[(_make_demo_image_bytes(), "reference.png", "image/png")],
        idempotency_key=idempotency_key,
    )
    append_workflow_draft_revision(
        db_session,
        product_id=workspace.product.id,
        draft_id=workspace.workflow_draft.id,
        expected_draft_version=0,
        payload=make_workflow_draft_payload(reference_asset_id=workspace.created_assets[0].id),
        ready_for_confirmation=True,
        source_turn_id="v3-request-test-turn",
        source_artifact_step_id="v3-request-test-artifact",
    )
    confirm_workflow_draft_revision(
        db_session,
        product_id=workspace.product.id,
        draft_id=workspace.workflow_draft.id,
        expected_draft_version=1,
    )
    persisted = persist_confirmed_draft_graph(
        db_session,
        product_id=workspace.product.id,
        draft_id=workspace.workflow_draft.id,
        expected_draft_version=1,
    )
    return workspace, persisted.graph


def test_agent_workflow_run_request_confirms_v3_graph(db_session, monkeypatch) -> None:
    monkeypatch.setattr(
        "productflow_backend.application.agent.workflow_run_requests.enqueue_graph_run",
        lambda run_id: None,
    )
    workspace, graph = _create_v3_requestable_workspace(db_session)
    prepared = prepare_agent_workflow_run_request(
        db_session,
        conversation_id=workspace.conversation.id,
        expected_workflow_revision=graph.revision,
    )
    assert prepared.workflow_id == graph.id
    assert prepared.graph_id == graph.id
    assert prepared.runnable_node_count > 0

    request = create_agent_workflow_run_request(
        db_session,
        conversation_id=workspace.conversation.id,
        expected_workflow_revision=prepared.workflow_revision,
        workflow_id=prepared.workflow_id,
        source_step_id="v3-run-request-step",
        idempotency_key="v3-run-request-key",
    )
    assert request.graph_id == graph.id
    assert request.status == AgentWorkflowRunRequestStatus.AWAITING_CONFIRMATION

    confirmed = confirm_agent_workflow_run_request(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        request_id=request.id,
    )
    assert confirmed.status == AgentWorkflowRunRequestStatus.CONFIRMED
    assert confirmed.graph_run_id is not None
    graph_run = db_session.get(WorkflowGraphRun, confirmed.graph_run_id)
    assert graph_run is not None
    assert graph_run.status == WorkflowRunStatus.RUNNING
    assert graph_run.graph_id == graph.id


def test_list_agent_workflow_runs_returns_v3_graph_runs(db_session) -> None:
    workspace, graph = _create_v3_requestable_workspace(
        db_session,
        name="v3 运行列表商品",
        idempotency_key="v3-workflow-run-list-workspace",
    )
    submission = submit_graph_run(
        db_session,
        product_id=workspace.product.id,
        graph_id=graph.id,
        scope=GraphRunScope.GRAPH,
        enqueue=lambda _run_id: None,
    )
    page = list_agent_workflow_runs(db_session, conversation_id=workspace.conversation.id, limit=5)
    assert page.workflow_id == graph.id
    assert page.workflow_revision == graph.revision
    assert [run.id for run in page.graph_runs] == [submission.run.id]
    assert page.graph_runs[0].status == WorkflowRunStatus.RUNNING


_create_requestable_workspace = _create_v3_requestable_workspace
