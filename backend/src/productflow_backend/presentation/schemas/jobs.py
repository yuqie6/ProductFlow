from __future__ import annotations

from datetime import datetime

from pydantic import BaseModel

from productflow_backend.domain.enums import JobKind, JobStatus, PosterKind
from productflow_backend.infrastructure.db.models import JobRun


class JobRunResponse(BaseModel):
    id: str
    product_id: str
    kind: JobKind
    status: JobStatus
    target_poster_kind: PosterKind | None = None
    failure_reason: str | None = None
    attempts: int
    created_at: datetime
    started_at: datetime | None = None
    finished_at: datetime | None = None
    copy_set_id: str | None = None
    poster_variant_id: str | None = None


def serialize_job(job: JobRun) -> JobRunResponse:
    return JobRunResponse(
        id=job.id,
        product_id=job.product_id,
        kind=job.kind,
        status=job.status,
        target_poster_kind=job.target_poster_kind,
        failure_reason=job.failure_reason,
        attempts=job.attempts,
        created_at=job.created_at,
        started_at=job.started_at,
        finished_at=job.finished_at,
        copy_set_id=job.copy_set_id,
        poster_variant_id=job.poster_variant_id,
    )
