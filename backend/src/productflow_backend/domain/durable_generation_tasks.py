from __future__ import annotations

from collections.abc import Sequence
from dataclasses import dataclass
from enum import StrEnum
from typing import Any

from productflow_backend.domain.enums import JobStatus, WorkflowNodeStatus, WorkflowRunStatus

QUEUE_UNAVAILABLE_DETAIL = "任务队列暂不可用，请稍后重试"


@dataclass(frozen=True, slots=True)
class DurableGenerationTaskContract:
    """Shared executable contract for DB-durable generation work.

    The contract intentionally describes existing business models instead of replacing them. Product workflow runs keep
    their run/node-run split, while continuous image-session tasks keep their task-level queued/running state.
    """

    name: str
    durable_model_name: str
    actor_name: str
    active_statuses: tuple[StrEnum, ...]
    queued_statuses: tuple[StrEnum, ...]
    running_statuses: tuple[StrEnum, ...]
    terminal_statuses: tuple[StrEnum, ...]
    execution_queued_statuses: tuple[StrEnum, ...]
    execution_running_statuses: tuple[StrEnum, ...]
    status_snapshot_source: str
    recovery_entrypoint: str
    submit_capacity_entrypoint: str = "ensure_generation_capacity"
    worker_capacity_entrypoint: str = "generation_running_capacity_available"
    enqueue_failure_detail: str = QUEUE_UNAVAILABLE_DETAIL
    actor_max_retries: int = 0

    def status_values(self, statuses: Sequence[StrEnum]) -> tuple[str, ...]:
        return tuple(status.value for status in statuses)

    def has_status(self, status: StrEnum | str, statuses: Sequence[StrEnum]) -> bool:
        value = status.value if isinstance(status, StrEnum) else status
        return value in self.status_values(statuses)

    def is_active(self, status: StrEnum | str) -> bool:
        return self.has_status(status, self.active_statuses)

    def is_queued(self, status: StrEnum | str) -> bool:
        return self.has_status(status, self.queued_statuses)

    def is_running(self, status: StrEnum | str) -> bool:
        return self.has_status(status, self.running_statuses)

    def is_terminal(self, status: StrEnum | str) -> bool:
        return self.has_status(status, self.terminal_statuses)

    def execution_is_queued(self, status: StrEnum | str) -> bool:
        return self.has_status(status, self.execution_queued_statuses)

    def execution_is_running(self, status: StrEnum | str) -> bool:
        return self.has_status(status, self.execution_running_statuses)


WORKFLOW_RUN_GENERATION_TASK_CONTRACT = DurableGenerationTaskContract(
    name="product_workflow_run",
    durable_model_name="WorkflowRun",
    actor_name="run_product_workflow_run",
    active_statuses=(WorkflowRunStatus.RUNNING,),
    queued_statuses=(),
    running_statuses=(WorkflowRunStatus.RUNNING,),
    terminal_statuses=(WorkflowRunStatus.SUCCEEDED, WorkflowRunStatus.FAILED, WorkflowRunStatus.CANCELLED),
    execution_queued_statuses=(WorkflowNodeStatus.QUEUED,),
    execution_running_statuses=(WorkflowNodeStatus.RUNNING,),
    status_snapshot_source="ProductWorkflowStatusSnapshot",
    recovery_entrypoint="recover_unfinished_workflow_runs",
)

LAUNCH_KIT_GENERATION_TASK_CONTRACT = DurableGenerationTaskContract(
    name="launch_kit_generation_task",
    durable_model_name="LaunchKitGenerationTask",
    actor_name="run_launch_kit_generation_task",
    active_statuses=(JobStatus.QUEUED, JobStatus.RUNNING),
    queued_statuses=(JobStatus.QUEUED,),
    running_statuses=(JobStatus.RUNNING,),
    terminal_statuses=(JobStatus.SUCCEEDED, JobStatus.FAILED, JobStatus.CANCELLED),
    execution_queued_statuses=(JobStatus.QUEUED,),
    execution_running_statuses=(JobStatus.RUNNING,),
    status_snapshot_source="LaunchKitStatusResponse",
    recovery_entrypoint="recover_unfinished_launch_kit_generation_tasks",
)


IMAGE_SESSION_GENERATION_TASK_CONTRACT = DurableGenerationTaskContract(
    name="image_session_generation_task",
    durable_model_name="ImageSessionGenerationTask",
    actor_name="run_image_session_generation_task",
    active_statuses=(JobStatus.QUEUED, JobStatus.RUNNING),
    queued_statuses=(JobStatus.QUEUED,),
    running_statuses=(JobStatus.RUNNING,),
    terminal_statuses=(JobStatus.SUCCEEDED, JobStatus.FAILED, JobStatus.CANCELLED),
    execution_queued_statuses=(JobStatus.QUEUED,),
    execution_running_statuses=(JobStatus.RUNNING,),
    status_snapshot_source="ImageSessionStatusSnapshot",
    recovery_entrypoint="recover_unfinished_image_session_generation_tasks",
)


def assert_actor_uses_durable_generation_contract(
    contract: DurableGenerationTaskContract,
    actor: Any,
) -> None:
    """Fail fast when a generation worker actor bypasses the durable retry contract."""

    max_retries = getattr(actor, "options", {}).get("max_retries")
    if max_retries != contract.actor_max_retries:
        raise RuntimeError(
            f"{contract.actor_name} must use max_retries={contract.actor_max_retries}; "
            "application execution owns durable failure state"
        )
