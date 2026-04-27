from __future__ import annotations

from pydantic import BaseModel

from productflow_backend.application.admission import GenerationQueueOverview


class GenerationQueueOverviewResponse(BaseModel):
    active_count: int
    running_count: int
    queued_count: int
    max_concurrent_tasks: int


def serialize_generation_queue_overview(overview: GenerationQueueOverview) -> GenerationQueueOverviewResponse:
    return GenerationQueueOverviewResponse(
        active_count=overview.active_count,
        running_count=overview.running_count,
        queued_count=overview.queued_count,
        max_concurrent_tasks=overview.max_concurrent_tasks,
    )
