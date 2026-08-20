from __future__ import annotations

import argparse
import json
import signal
from collections.abc import Callable, Sequence
from dataclasses import asdict
from threading import Event
from time import monotonic
from typing import Any

from productflow_backend.application.agent.sync import recover_unfinished_agent_turn_syncs
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

DEFAULT_WATCH_INTERVAL_SECONDS = 1.0


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="运行可靠异步投递调度器")
    parser.add_argument("--limit", type=int, default=100, help="每轮最多处理多少条待投递记录")
    parser.add_argument(
        "--watch",
        action="store_true",
        help="持续运行 recovery 和 dispatch loop；收到 SIGINT/SIGTERM 后退出",
    )
    parser.add_argument(
        "--interval",
        type=float,
        default=DEFAULT_WATCH_INTERVAL_SECONDS,
        help="watch 模式两轮之间的等待秒数",
    )
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
        "workflow_unknown": workflow.unknown_runs,
        "image_session": image_session.enqueued_tasks,
        "image_session_unknown": image_session.unknown_tasks,
        "agent": agent.enqueued_turns,
        "rendition": rendition.enqueued_jobs,
    }


def _run_once(*, limit: int) -> dict[str, Any]:
    recovery = _recover_business_state()
    summary = run_async_dispatcher_once(
        enqueue=enqueue_async_dispatch,
        limit=max(1, limit),
    )
    return {"dispatch": asdict(summary), "recovery": recovery}


def _print_summary(summary: dict[str, Any]) -> None:
    print(json.dumps(summary, ensure_ascii=False, sort_keys=True), flush=True)


def _validate_watch_interval(parser: argparse.ArgumentParser, interval: float) -> float:
    if interval <= 0 or interval > 3600:
        parser.error("--interval 必须大于 0 且不超过 3600 秒")
    return interval


def _run_watch(
    *,
    limit: int,
    interval_seconds: float,
    stop_event: Event | None = None,
    run_once: Callable[[], dict[str, Any]] | None = None,
    wait: Callable[[float], bool] | None = None,
) -> None:
    stop = stop_event or Event()
    cycle = run_once or (lambda: _run_once(limit=limit))
    wait_for_stop = wait or stop.wait
    while not stop.is_set():
        started = monotonic()
        try:
            _print_summary(cycle())
        except Exception:  # noqa: BLE001
            # A transient database or broker outage must not terminate the
            # resident scanner; the next cycle will retry the durable rows.
            print(json.dumps({"error": "dispatcher cycle failed"}, ensure_ascii=False), flush=True)
        remaining = max(0.0, interval_seconds - (monotonic() - started))
        if wait_for_stop(remaining):
            return


def main(argv: Sequence[str] | None = None) -> int:
    parser = _parser()
    args = parser.parse_args(argv)
    interval_seconds = _validate_watch_interval(parser, args.interval)
    if args.watch:
        stop = Event()
        previous_handlers: dict[signal.Signals, Any] = {}

        def request_stop(signum: int, _frame: Any) -> None:
            del signum
            stop.set()

        try:
            for signum in (signal.SIGINT, signal.SIGTERM):
                previous_handlers[signum] = signal.signal(signum, request_stop)
            _run_watch(limit=max(1, args.limit), interval_seconds=interval_seconds, stop_event=stop)
        finally:
            for signum, handler in previous_handlers.items():
                signal.signal(signum, handler)
        return 0

    _print_summary(_run_once(limit=args.limit))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
