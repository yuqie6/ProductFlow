"""Durable generation 任务合同：队列身份、可安全重入阶段，以及 unknown 与 failed 的分界。"""

from __future__ import annotations

from collections.abc import Sequence
from dataclasses import dataclass
from enum import StrEnum
from typing import Any

from productflow_backend.domain.enums import (
    JobStatus,
    LocalImageEditTaskStatus,
    WorkflowNodeStatus,
    WorkflowRunStatus,
)

QUEUE_UNAVAILABLE_DETAIL = "任务队列暂不可用，请稍后重试"
WORKFLOW_PROVIDER_EFFECT_SAFE_REQUEUE_PHASES = frozenset({"claimed", "prepared"})
WORKFLOW_PROVIDER_EFFECT_CALL_PHASE = "provider_call"
WORKFLOW_PROVIDER_EFFECT_RESULT_PHASE = "provider_result_received"
WORKFLOW_PROVIDER_EFFECT_UNKNOWN_DETAIL = (
    "工作流供应商请求结果未知，系统未自动重试。请检查供应商记录后重新发起工作流。"
)
WORKFLOW_PROVIDER_EFFECT_UNKNOWN_PHASE = "unknown_provider_effect"
IMAGE_SESSION_PROVIDER_EFFECT_UNKNOWN_DETAIL = (
    "图片供应商请求结果未知，系统未自动重试。请检查供应商记录后重新发起生成。"
)
IMAGE_SESSION_PROVIDER_EFFECT_UNKNOWN_PHASE = "unknown_provider_effect"


class WorkflowRunDeliveryState(StrEnum):
    NONE = "none"
    RUNNING = "running"
    QUEUED = "queued"


@dataclass(frozen=True, slots=True)
class DurableGenerationTaskContract:
    """DB 耐久生成工作的共享可执行合同。

    合同描述现有业务模型，不替换它们。商品工作流运行保留 run/node-run 拆分，
    连续 image-session 任务保留任务级 queued/running 状态。
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


IMAGE_SESSION_GENERATION_TASK_CONTRACT = DurableGenerationTaskContract(
    name="image_session_generation_task",
    durable_model_name="ImageSessionGenerationTask",
    actor_name="run_image_session_generation_task",
    active_statuses=(JobStatus.QUEUED, JobStatus.RUNNING),
    queued_statuses=(JobStatus.QUEUED,),
    running_statuses=(JobStatus.RUNNING,),
    terminal_statuses=(JobStatus.SUCCEEDED, JobStatus.FAILED, JobStatus.UNKNOWN, JobStatus.CANCELLED),
    execution_queued_statuses=(JobStatus.QUEUED,),
    execution_running_statuses=(JobStatus.RUNNING,),
    status_snapshot_source="ImageSessionStatusSnapshot",
    recovery_entrypoint="recover_unfinished_image_session_generation_tasks",
)

# 图运行把 UNKNOWN 列为终态；无法证明的 provider effect 不得改成 FAILED。
GRAPH_RUN_GENERATION_TASK_CONTRACT = DurableGenerationTaskContract(
    name="workflow_graph_run",
    durable_model_name="WorkflowGraphRun",
    actor_name="run_workflow_graph_run",
    active_statuses=(WorkflowRunStatus.RUNNING,),
    queued_statuses=(),
    running_statuses=(WorkflowRunStatus.RUNNING,),
    terminal_statuses=(
        WorkflowRunStatus.SUCCEEDED,
        WorkflowRunStatus.FAILED,
        WorkflowRunStatus.CANCELLED,
        WorkflowRunStatus.UNKNOWN,
    ),
    execution_queued_statuses=(WorkflowNodeStatus.QUEUED,),
    execution_running_statuses=(WorkflowNodeStatus.RUNNING,),
    status_snapshot_source="WorkflowGraphRun",
    recovery_entrypoint="execute_graph_run",
)

# DeliverySpec 派生是确定性工作，没有 provider 未知态。
DELIVERY_RENDITION_TASK_CONTRACT = DurableGenerationTaskContract(
    name="delivery_rendition_job",
    durable_model_name="DeliveryRenditionJob",
    actor_name="run_delivery_rendition_job",
    active_statuses=(JobStatus.QUEUED, JobStatus.RUNNING),
    queued_statuses=(JobStatus.QUEUED,),
    running_statuses=(JobStatus.RUNNING,),
    terminal_statuses=(JobStatus.SUCCEEDED, JobStatus.FAILED),
    execution_queued_statuses=(JobStatus.QUEUED,),
    execution_running_statuses=(JobStatus.RUNNING,),
    status_snapshot_source="DeliveryRenditionJob",
    recovery_entrypoint="recover_unfinished_delivery_rendition_jobs",
)

LOCAL_IMAGE_EDIT_TASK_CONTRACT = DurableGenerationTaskContract(
    name="local_image_edit_task",
    durable_model_name="LocalImageEditTask",
    actor_name="run_local_image_edit_task",
    active_statuses=(LocalImageEditTaskStatus.QUEUED, LocalImageEditTaskStatus.RUNNING),
    queued_statuses=(LocalImageEditTaskStatus.QUEUED,),
    running_statuses=(LocalImageEditTaskStatus.RUNNING,),
    terminal_statuses=(
        LocalImageEditTaskStatus.SUCCEEDED,
        LocalImageEditTaskStatus.FAILED,
        LocalImageEditTaskStatus.CANCELLED,
        LocalImageEditTaskStatus.UNKNOWN,
    ),
    execution_queued_statuses=(LocalImageEditTaskStatus.QUEUED,),
    execution_running_statuses=(LocalImageEditTaskStatus.RUNNING,),
    status_snapshot_source="LocalImageEditTask",
    recovery_entrypoint="execute_local_image_edit_task",
)


def classify_workflow_run_delivery(
    run_status: WorkflowRunStatus | str,
    node_run_statuses: Sequence[WorkflowNodeStatus | str],
) -> WorkflowRunDeliveryState:
    """投递层视图。节点全终态但 run 仍 RUNNING 时仍视为 queued，不能据此猜 failed。"""

    if not GRAPH_RUN_GENERATION_TASK_CONTRACT.is_active(run_status):
        return WorkflowRunDeliveryState.NONE

    statuses = tuple(node_run_statuses)
    if any(GRAPH_RUN_GENERATION_TASK_CONTRACT.execution_is_running(status) for status in statuses):
        return WorkflowRunDeliveryState.RUNNING
    if any(GRAPH_RUN_GENERATION_TASK_CONTRACT.execution_is_queued(status) for status in statuses):
        return WorkflowRunDeliveryState.QUEUED
    if statuses and all(
        GRAPH_RUN_GENERATION_TASK_CONTRACT.has_status(
            status,
            (WorkflowNodeStatus.SUCCEEDED, WorkflowNodeStatus.FAILED, WorkflowNodeStatus.UNKNOWN),
        )
        for status in statuses
    ):
        return WorkflowRunDeliveryState.QUEUED
    return WorkflowRunDeliveryState.NONE


def assert_actor_uses_durable_generation_contract(
    contract: DurableGenerationTaskContract,
    actor: Any,
) -> None:
    """生成 worker actor 绕过耐久重试合同时立刻失败。"""

    max_retries = getattr(actor, "options", {}).get("max_retries")
    if max_retries != contract.actor_max_retries:
        raise RuntimeError(
            f"{contract.actor_name} must use max_retries={contract.actor_max_retries}; "
            "application execution owns durable failure state"
        )
