from __future__ import annotations

import sys
from pathlib import Path

import dramatiq

from productflow_backend.application.product_workflows import execute_product_workflow_run
from productflow_backend.application.use_cases import execute_copy_job, execute_poster_job
from productflow_backend.config import get_runtime_settings
from productflow_backend.infrastructure.queue import (
    get_broker,
    recover_unfinished_jobs,
    recover_unfinished_workflow_runs,
)

get_broker()


@dramatiq.actor(max_retries=0)
def run_copy_generation(job_id: str) -> None:
    """文案生成 worker：执行失败时自调度重试。"""
    if execute_copy_job(job_id):
        run_copy_generation.send_with_options(args=(job_id,), delay=get_runtime_settings().job_retry_delay_ms)


@dramatiq.actor(max_retries=0)
def run_poster_generation(job_id: str) -> None:
    """海报生成 worker：执行失败时自调度重试。"""
    if execute_poster_job(job_id):
        run_poster_generation.send_with_options(args=(job_id,), delay=get_runtime_settings().job_retry_delay_ms)


@dramatiq.actor(max_retries=0)
def run_product_workflow_run(workflow_run_id: str) -> None:
    """商品工作流 worker：执行边界内部负责把失败落库。"""
    execute_product_workflow_run(workflow_run_id)


def _running_under_dramatiq_cli() -> bool:
    return any(Path(arg).name == "dramatiq" for arg in sys.argv)


if _running_under_dramatiq_cli():
    recover_unfinished_jobs(reset_stale_running=True)
    recover_unfinished_workflow_runs(reset_stale_running=True)
