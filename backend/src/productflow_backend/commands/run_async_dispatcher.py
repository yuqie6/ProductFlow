from __future__ import annotations

import argparse
import json
from collections.abc import Sequence
from dataclasses import asdict

from productflow_backend.application.agent_sync import recover_unfinished_agent_turn_syncs
from productflow_backend.application.async_delivery import (
    run_async_dispatcher_once,
    stage_async_dispatch_for_actor,
)
from productflow_backend.application.durable_recovery import (
    recover_unfinished_delivery_rendition_jobs,
    recover_unfinished_image_session_generation_tasks,
    recover_unfinished_workflow_runs,
)
from productflow_backend.domain.durable_generation_tasks import (
    DELIVERY_RENDITION_TASK_CONTRACT,
    IMAGE_SESSION_GENERATION_TASK_CONTRACT,
    WORKFLOW_RUN_GENERATION_TASK_CONTRACT,
)
from productflow_backend.infrastructure.queue import enqueue_async_dispatch


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="运行一轮可靠异步投递调度器")
    parser.add_argument("--limit", type=int, default=100, help="每轮最多处理多少条待投递记录")
    return parser


def _recover_business_state() -> dict[str, int]:
    workflow = recover_unfinished_workflow_runs(
        stage_dispatch=lambda session, run_id: stage_async_dispatch_for_actor(
            session,
            WORKFLOW_RUN_GENERATION_TASK_CONTRACT.actor_name,
            run_id,
        ),
        reset_stale_running=True,
    )
    image_session = recover_unfinished_image_session_generation_tasks(
        stage_dispatch=lambda session, task_id: stage_async_dispatch_for_actor(
            session,
            IMAGE_SESSION_GENERATION_TASK_CONTRACT.actor_name,
            task_id,
        ),
        reset_stale_running=True,
    )
    agent = recover_unfinished_agent_turn_syncs(
        stage_dispatch=lambda session, projection_id: stage_async_dispatch_for_actor(
            session,
            "run_agent_turn_sync",
            projection_id,
        )
    )
    rendition = recover_unfinished_delivery_rendition_jobs(
        stage_dispatch=lambda session, job_id: stage_async_dispatch_for_actor(
            session,
            DELIVERY_RENDITION_TASK_CONTRACT.actor_name,
            job_id,
        ),
        reset_stale_running=True,
    )
    return {
        "workflow": workflow.enqueued_runs,
        "image_session": image_session.enqueued_tasks,
        "agent": agent.enqueued_turns,
        "rendition": rendition.enqueued_jobs,
    }


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    recovery = _recover_business_state()
    summary = run_async_dispatcher_once(
        enqueue=enqueue_async_dispatch,
        limit=max(1, args.limit),
    )
    print(
        json.dumps(
            {"dispatch": asdict(summary), "recovery": recovery},
            ensure_ascii=False,
            sort_keys=True,
        )
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
