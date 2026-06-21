from __future__ import annotations

from productflow_backend.domain.enums import JobStatus
from productflow_backend.infrastructure.db.models import LaunchKit, LaunchKitGenerationTask


def latest_generation_task(launch_kit: LaunchKit) -> LaunchKitGenerationTask | None:
    return max(launch_kit.tasks, key=lambda task: task.created_at, default=None)


def launch_kit_has_active_task(launch_kit: LaunchKit) -> bool:
    latest = latest_generation_task(launch_kit)
    return latest is not None and latest.status in {JobStatus.QUEUED, JobStatus.RUNNING}
