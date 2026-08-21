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
from productflow_backend.application.agent.workflow_runs import inspect_agent_global_workflow_runs
from productflow_backend.application.product_workflow.run_state import WORKFLOW_CANCELLED_REASON
from productflow_backend.application.product_workflow.v2_runs import (
    retry_v2_workflow_run,
    retryable_node_run_ids,
    submit_v2_workflow_run,
    validate_retry_workflow_run,
)
from productflow_backend.application.product_workflow.graph_draft_persist import persist_confirmed_draft_graph
from productflow_backend.application.workflow_drafts.materialization import materialize_workflow_draft
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
    WorkflowNodeStatus,
    WorkflowRunStatus,
)
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.agent_service import AgentServiceToolStep, AgentServiceTurnState
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentWorkflowRunRequest,
    WorkflowGraphRun,
    WorkflowRun,
)


def _create_requestable_workspace(
    db_session,
    *,
    name: str = "执行请求测试商品",
    idempotency_key: str = "workflow-run-request-workspace",
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
        source_turn_id="request-test-turn",
        source_artifact_step_id="request-test-artifact",
    )
    confirm_workflow_draft_revision(
        db_session,
        product_id=workspace.product.id,
        draft_id=workspace.workflow_draft.id,
        expected_draft_version=1,
    )
    materialized = materialize_workflow_draft(
        db_session,
        product_id=workspace.product.id,
        draft_id=workspace.workflow_draft.id,
        expected_draft_version=1,
        expected_workflow_revision=0,
        idempotency_key="request-test-materialization",
    )
    return workspace, materialized.workflow


def test_global_agent_can_inspect_recent_runs_for_selected_workflows(db_session) -> None:
    first, first_workflow = _create_requestable_workspace(db_session)
    _, second_workflow = _create_requestable_workspace(
        db_session,
        name="执行请求测试商品 2",
        idempotency_key="workflow-run-request-workspace-2",
    )
    global_conversation = db_session.scalar(
        select(AgentConversation).where(
            AgentConversation.session_id == first.conversation.session_id,
            AgentConversation.scope_type == AgentConversationScope.GLOBAL,
        )
    )
    assert global_conversation is not None

    submitted = submit_v2_workflow_run(
        db_session,
        product_id=first.product.id,
        workflow_id=first_workflow.id,
    )
    inspected = inspect_agent_global_workflow_runs(
        db_session,
        conversation_id=global_conversation.id,
        workflow_ids=[second_workflow.id, first_workflow.id],
        limit=1,
    )

    assert [item["workflow_id"] for item in inspected] == [second_workflow.id, first_workflow.id]
    assert inspected[0]["product_name"] == "执行请求测试商品 2"
    assert inspected[0]["runs"] == []
    assert inspected[1]["runs"][0]["id"] == submitted.run.id
    assert inspected[1]["runs"][0]["status"] == WorkflowRunStatus.RUNNING
    assert inspected[1]["runs"][0]["node_status_counts"]


def test_agent_workflow_run_request_waits_for_confirmation_and_reuses_run_chain(db_session) -> None:
    workspace, workflow = _create_requestable_workspace(db_session)
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
    assert request.workflow_run_id is None
    persisted_task = db_session.get(type(task), task.id)
    assert persisted_task.status == AgentTaskStatus.AWAITING_CONFIRMATION
    assert persisted_task.workflow_id == workflow.id
    assert db_session.scalar(select(func.count()).select_from(WorkflowRun)) == 0

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
    assert confirmed.workflow_run_id is not None
    assert db_session.get(WorkflowRun, confirmed.workflow_run_id) is not None
    assert confirmed.workflow_run is not None
    assert confirmed.workflow_run.status == WorkflowRunStatus.RUNNING
    assert db_session.get(type(task), task.id).workflow_id == workflow.id
    assert confirmed.workflow_run.progress_metadata == {
        "run_scope": "workflow",
        "requested_by": "agent",
        "agent_workflow_run_request_id": request.id,
        "agent_task_id": task.id,
    }
    assert db_session.scalar(select(func.count()).select_from(WorkflowRun)) == 1
    assert db_session.get(type(task), task.id).status == AgentTaskStatus.RUNNING

    run = db_session.get(WorkflowRun, confirmed.workflow_run_id)
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
    assert confirmed_replay.workflow_run_id == confirmed.workflow_run_id
    assert db_session.scalar(select(func.count()).select_from(WorkflowRun)) == 1


def test_global_agent_workflow_run_request_targets_explicit_product_and_reuses_run_chain(db_session) -> None:
    workspace, workflow = _create_requestable_workspace(db_session)
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
    assert confirmed.workflow_run_id is not None
    assert confirmed.workflow_run is not None
    assert confirmed.workflow_run.workflow_id == workflow.id
    assert confirmed.workflow_run.status == WorkflowRunStatus.RUNNING
    assert get_agent_workflow_run_request(
        db_session,
        product_id=None,
        conversation_id=global_conversation.id,
        task_id=task.id,
    ).product_id == workspace.product.id
    assert db_session.get(type(task), task.id).status == AgentTaskStatus.RUNNING


def test_global_agent_turn_projects_workflow_run_request_for_dock_confirmation(db_session) -> None:
    workspace, workflow = _create_requestable_workspace(db_session)
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
    workspace, workflow = _create_requestable_workspace(db_session)
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
    assert db_session.scalar(select(func.count()).select_from(WorkflowRun)) == 0

    cancelled = cancel_agent_workflow_run_request(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        request_id=request.id,
    )
    assert cancelled.status == AgentWorkflowRunRequestStatus.CANCELLED
    assert cancelled.workflow_run_id is None
    assert get_agent_workflow_run_request(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
    ).status == AgentWorkflowRunRequestStatus.CANCELLED


def test_agent_turn_projects_workflow_run_request_as_human_confirmation(db_session) -> None:
    workspace, workflow = _create_requestable_workspace(db_session)
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


def test_agent_can_request_workflow_run_retry(db_session) -> None:
    workspace, workflow = _create_requestable_workspace(db_session)
    submission = submit_v2_workflow_run(
        db_session,
        product_id=workspace.product.id,
        workflow_id=workflow.id,
    )
    run = submission.run
    run.status = WorkflowRunStatus.FAILED
    run.is_retryable = True
    for node_run in run.node_runs:
        node_run.status = WorkflowRunStatus.FAILED
        node_run.failure_reason = "generation_failed"
    db_session.commit()

    prepared = prepare_agent_workflow_run_request(
        db_session,
        conversation_id=workspace.conversation.id,
        expected_workflow_revision=workflow.revision,
        source_run_id=run.id,
    )
    assert prepared.source_run_id == run.id
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
    assert request.source_run_id == run.id
    assert request.status == AgentWorkflowRunRequestStatus.AWAITING_CONFIRMATION

    confirmed = confirm_agent_workflow_run_request(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        request_id=request.id,
    )
    assert confirmed.status == AgentWorkflowRunRequestStatus.CONFIRMED
    assert confirmed.workflow_run_id is not None
    assert confirmed.workflow_run_id != run.id
    new_run = db_session.get(WorkflowRun, confirmed.workflow_run_id)
    assert new_run is not None
    assert new_run.progress_metadata.get("source_run_id") == run.id
    assert new_run.progress_metadata.get("manual_retry") is True


def test_global_agent_can_request_workflow_run_retry(db_session) -> None:
    workspace, workflow = _create_requestable_workspace(db_session)
    submission = submit_v2_workflow_run(
        db_session,
        product_id=workspace.product.id,
        workflow_id=workflow.id,
    )
    run = submission.run
    run.status = WorkflowRunStatus.FAILED
    run.is_retryable = True
    for node_run in run.node_runs:
        node_run.status = WorkflowRunStatus.FAILED
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
    assert prepared.source_run_id == run.id

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
    assert request.source_run_id == run.id

    confirmed = confirm_agent_workflow_run_request(
        db_session,
        product_id=None,
        conversation_id=global_conversation.id,
        request_id=request.id,
    )
    assert confirmed.status == AgentWorkflowRunRequestStatus.CONFIRMED
    assert confirmed.workflow_run_id is not None
    new_run = db_session.get(WorkflowRun, confirmed.workflow_run_id)
    assert new_run is not None
    assert new_run.progress_metadata.get("source_run_id") == run.id



def test_retryable_node_rule_excludes_cancelled_reason_failures(db_session) -> None:
    """Single-owner rule: only non-cancelled failed nodes are marked for retry."""
    workspace, workflow = _create_requestable_workspace(db_session)
    submission = submit_v2_workflow_run(
        db_session,
        product_id=workspace.product.id,
        workflow_id=workflow.id,
    )
    run = submission.run
    run.status = WorkflowRunStatus.FAILED
    run.is_retryable = True
    node_runs = list(run.node_runs)
    assert node_runs
    for node_run in node_runs:
        node_run.status = WorkflowNodeStatus.FAILED
        node_run.failure_reason = "generation_failed"
    node_runs[0].failure_reason = WORKFLOW_CANCELLED_REASON
    db_session.commit()

    assert retryable_node_run_ids(run) == {node_run.node_id for node_run in node_runs[1:]}


def test_retry_node_rule_agrees_between_validate_and_execute(db_session) -> None:
    """The retry set announced by validation always equals the set that re-runs."""
    workspace, workflow = _create_requestable_workspace(db_session)
    submission = submit_v2_workflow_run(
        db_session,
        product_id=workspace.product.id,
        workflow_id=workflow.id,
    )
    run = submission.run
    run.status = WorkflowRunStatus.FAILED
    run.is_retryable = True
    node_runs = list(run.node_runs)
    assert node_runs
    for node_run in node_runs:
        node_run.status = WorkflowNodeStatus.FAILED
        node_run.failure_reason = "generation_failed"
    db_session.commit()

    workflow_v, ordered = validate_retry_workflow_run(
        db_session,
        product_id=workspace.product.id,
        workflow_id=workflow.id,
        run_id=run.id,
    )
    assert workflow_v.id == workflow.id
    expected = {node_run.node_id for node_run in node_runs}
    assert set(ordered) == expected

    retried = retry_v2_workflow_run(
        db_session,
        product_id=workspace.product.id,
        workflow_id=workflow.id,
        run_id=run.id,
    )
    new_run = retried.run
    assert {node_run.node_id for node_run in new_run.node_runs} == expected
    assert new_run.progress_metadata.get("source_run_id") == run.id
    assert new_run.progress_metadata.get("manual_retry") is True


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
    assert request.workflow_id is None
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
