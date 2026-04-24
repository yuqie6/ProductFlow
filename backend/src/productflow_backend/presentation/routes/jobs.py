from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException
from sqlalchemy.orm import Session

from productflow_backend.application.use_cases import get_job
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.schemas.jobs import JobRunResponse, serialize_job

router = APIRouter(prefix="/api/jobs", tags=["jobs"], dependencies=[Depends(require_admin)])


@router.get("/{job_id}", response_model=JobRunResponse)
def get_job_detail(job_id: str, session: Session = Depends(get_session)) -> JobRunResponse:
    try:
        return serialize_job(get_job(session, job_id))
    except ValueError as exc:
        raise HTTPException(status_code=404, detail=str(exc)) from exc
