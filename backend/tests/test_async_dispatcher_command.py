from __future__ import annotations

from threading import Event

from productflow_backend.commands import run_async_dispatcher


def test_watch_runs_cycles_until_stop_requested(monkeypatch) -> None:
    stop = Event()
    summaries: list[dict[str, object]] = []
    cycles = 0

    def run_once() -> dict[str, object]:
        nonlocal cycles
        cycles += 1
        if cycles == 2:
            stop.set()
        return {"cycle": cycles}

    monkeypatch.setattr(run_async_dispatcher, "_print_summary", summaries.append)
    run_async_dispatcher._run_watch(
        limit=10,
        interval_seconds=0,
        stop_event=stop,
        run_once=run_once,
        wait=lambda _seconds: stop.is_set(),
    )

    assert summaries == [{"cycle": 1}, {"cycle": 2}]


def test_watch_cycle_failure_does_not_stop_the_scanner(monkeypatch) -> None:
    stop = Event()
    printed: list[dict[str, object]] = []
    cycles = 0

    def run_once() -> dict[str, object]:
        nonlocal cycles
        cycles += 1
        if cycles == 1:
            raise RuntimeError("temporary database outage")
        stop.set()
        return {"cycle": cycles}

    monkeypatch.setattr(run_async_dispatcher, "_print_summary", printed.append)
    run_async_dispatcher._run_watch(
        limit=10,
        interval_seconds=0,
        stop_event=stop,
        run_once=run_once,
        wait=lambda _seconds: stop.is_set(),
    )

    assert cycles == 2
    assert printed == [{"cycle": 2}]
