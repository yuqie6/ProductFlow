from __future__ import annotations

import pytest

from productflow_backend.application.queue_submission import enqueue_or_mark_failed
from productflow_backend.domain.durable_generation_tasks import (
    DELIVERY_RENDITION_TASK_CONTRACT,
    GRAPH_RUN_GENERATION_TASK_CONTRACT,
    IMAGE_SESSION_GENERATION_TASK_CONTRACT,
    QUEUE_UNAVAILABLE_DETAIL,
    WORKFLOW_RUN_GENERATION_TASK_CONTRACT,
    WorkflowRunDeliveryState,
    assert_actor_uses_durable_generation_contract,
    classify_workflow_run_delivery,
)
from productflow_backend.domain.enums import JobStatus, WorkflowNodeStatus, WorkflowRunStatus
from productflow_backend.domain.errors import QueueUnavailableError


def test_durable_generation_task_contract_keeps_workflow_and_image_models_separate() -> None:
    assert WORKFLOW_RUN_GENERATION_TASK_CONTRACT.durable_model_name == "WorkflowRun"
    assert IMAGE_SESSION_GENERATION_TASK_CONTRACT.durable_model_name == "ImageSessionGenerationTask"

    assert WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_active(WorkflowRunStatus.RUNNING)
    assert WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_terminal(WorkflowRunStatus.SUCCEEDED)
    assert WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_terminal(WorkflowRunStatus.CANCELLED)
    assert WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_queued(WorkflowNodeStatus.QUEUED)
    assert WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_running(WorkflowNodeStatus.RUNNING)

    assert IMAGE_SESSION_GENERATION_TASK_CONTRACT.is_active(JobStatus.QUEUED)
    assert IMAGE_SESSION_GENERATION_TASK_CONTRACT.is_active(JobStatus.RUNNING)
    assert IMAGE_SESSION_GENERATION_TASK_CONTRACT.is_queued(JobStatus.QUEUED)
    assert IMAGE_SESSION_GENERATION_TASK_CONTRACT.is_running(JobStatus.RUNNING)
    assert IMAGE_SESSION_GENERATION_TASK_CONTRACT.is_terminal(JobStatus.FAILED)
    assert IMAGE_SESSION_GENERATION_TASK_CONTRACT.is_terminal(JobStatus.CANCELLED)
    assert DELIVERY_RENDITION_TASK_CONTRACT.durable_model_name == "DeliveryRenditionJob"
    assert DELIVERY_RENDITION_TASK_CONTRACT.is_active(JobStatus.QUEUED)
    assert DELIVERY_RENDITION_TASK_CONTRACT.is_active(JobStatus.RUNNING)
    assert DELIVERY_RENDITION_TASK_CONTRACT.is_terminal(JobStatus.SUCCEEDED)
    assert DELIVERY_RENDITION_TASK_CONTRACT.is_terminal(JobStatus.FAILED)


@pytest.mark.parametrize(
    ("run_status", "node_run_statuses", "expected"),
    [
        (WorkflowRunStatus.SUCCEEDED, [WorkflowNodeStatus.QUEUED], WorkflowRunDeliveryState.NONE),
        (WorkflowRunStatus.CANCELLED, [WorkflowNodeStatus.RUNNING], WorkflowRunDeliveryState.NONE),
        (WorkflowRunStatus.RUNNING, [], WorkflowRunDeliveryState.NONE),
        (WorkflowRunStatus.RUNNING, [WorkflowNodeStatus.FAILED], WorkflowRunDeliveryState.QUEUED),
        (
            WorkflowRunStatus.RUNNING,
            [WorkflowNodeStatus.SUCCEEDED, WorkflowNodeStatus.FAILED],
            WorkflowRunDeliveryState.QUEUED,
        ),
        (WorkflowRunStatus.RUNNING, [WorkflowNodeStatus.QUEUED], WorkflowRunDeliveryState.QUEUED),
        (WorkflowRunStatus.RUNNING, [WorkflowNodeStatus.SUCCEEDED], WorkflowRunDeliveryState.QUEUED),
        (
            WorkflowRunStatus.RUNNING,
            [WorkflowNodeStatus.QUEUED, WorkflowNodeStatus.RUNNING],
            WorkflowRunDeliveryState.RUNNING,
        ),
        ("running", ["succeeded", "succeeded"], WorkflowRunDeliveryState.QUEUED),
    ],
)
def test_classify_workflow_run_delivery(
    run_status: WorkflowRunStatus | str,
    node_run_statuses: list[WorkflowNodeStatus | str],
    expected: WorkflowRunDeliveryState,
) -> None:
    assert classify_workflow_run_delivery(run_status, node_run_statuses) == expected


def test_durable_generation_task_contract_matches_worker_actor_retry_policy(configured_env) -> None:
    from productflow_backend.workers import (
        run_delivery_rendition_job,
        run_image_session_generation_task,
        run_product_workflow_node_run,
        run_product_workflow_run,
        run_workflow_graph_run,
    )

    assert_actor_uses_durable_generation_contract(
        WORKFLOW_RUN_GENERATION_TASK_CONTRACT,
        run_product_workflow_run,
    )
    assert_actor_uses_durable_generation_contract(
        WORKFLOW_RUN_GENERATION_TASK_CONTRACT,
        run_product_workflow_node_run,
    )
    assert_actor_uses_durable_generation_contract(
        IMAGE_SESSION_GENERATION_TASK_CONTRACT,
        run_image_session_generation_task,
    )
    assert_actor_uses_durable_generation_contract(
        DELIVERY_RENDITION_TASK_CONTRACT,
        run_delivery_rendition_job,
    )
    assert_actor_uses_durable_generation_contract(
        GRAPH_RUN_GENERATION_TASK_CONTRACT,
        run_workflow_graph_run,
    )


def test_enqueue_or_mark_failed_uses_shared_durable_generation_failure_detail() -> None:
    marked: list[tuple[str, str]] = []

    def fail_enqueue(_: str) -> None:
        raise RuntimeError("redis unavailable")

    with pytest.raises(QueueUnavailableError) as exc_info:
        enqueue_or_mark_failed(
            "durable-task-id",
            enqueue=fail_enqueue,
            mark_failed=lambda task_id, reason: marked.append((task_id, reason)),
        )

    assert str(exc_info.value) == QUEUE_UNAVAILABLE_DETAIL
    assert marked == [("durable-task-id", QUEUE_UNAVAILABLE_DETAIL)]
