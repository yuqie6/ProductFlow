"""SQL fragments for binding capacity evidence to native River jobs."""

from __future__ import annotations


RIVER_TASK_KIND = "productflow_task"
IMAGE_SESSION_ACTOR = "run_image_session_generation_task"


def image_session_river_projection(job_alias: str = "r") -> str:
    """Project the native River lifecycle fields without renaming their meaning."""
    return f"""
                       {job_alias}.state AS river_state,
                       {job_alias}.attempt AS river_attempt,
                       {job_alias}.attempted_at AS river_attempted_at,
                       {job_alias}.finalized_at AS river_finalized_at"""


def image_session_river_join(
    job_alias: str = "r",
    task_alias: str = "t",
    session_alias: str = "s",
) -> str:
    """Join exactly one business execution to its immutable River identity."""
    return f"""
                LEFT JOIN river_job {job_alias}
                  ON {job_alias}.kind = '{RIVER_TASK_KIND}'
                 AND {job_alias}.args ->> 'actor' = '{IMAGE_SESSION_ACTOR}'
                 AND {job_alias}.args ->> 'aggregate_id' = {task_alias}.id
                 AND {job_alias}.args ->> 'merchant_id' = {session_alias}.merchant_id
                 AND {job_alias}.args ->> 'execution_id' = {task_alias}.queue_execution_id"""
