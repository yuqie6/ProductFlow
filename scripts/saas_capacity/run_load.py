#!/usr/bin/env python3
"""Run one sustained L1/L2 mixed read, SSE, and image-generation round."""

import argparse
import asyncio
import csv
import hashlib
import json
import random
import time
from collections import Counter
from dataclasses import dataclass, field
from datetime import datetime
from pathlib import Path
from typing import Any

import aiohttp
import psycopg

from capacity_events import merge_events, prompt_matches_prefix, snapshot_events, validate_event
from capacity_identity import verify_identity


READ_ROUTES = ("products", "media", "session_status", "quota")
READ_WEIGHTS = (40, 30, 20, 10)
TERMINAL = {"succeeded", "failed", "unknown", "cancelled"}
BROWSER_ORIGIN = "http://127.0.0.1:30182"
GENERATION_SLOTS = 3


def percentile(values: list[float], fraction: float) -> float | None:
    if not values:
        return None
    ordered = sorted(values)
    index = min(len(ordered) - 1, max(0, int(round((len(ordered) - 1) * fraction))))
    return ordered[index]


@dataclass
class RequestRecord:
    route: str
    merchant: str
    status: int
    latency_ms: float
    error: str = ""
    phase: str = "sample"


def summarize_reads(read_records: list[RequestRecord]) -> dict[str, dict[str, Any]]:
    """Report measured reads only; retain warmup counts for audit."""
    by_route: dict[str, dict[str, Any]] = {}
    for route in READ_ROUTES:
        route_records = [record for record in read_records if record.route == route]
        sample_records = [record for record in route_records if record.phase == "sample"]
        successful = [record for record in sample_records if record.status == 200 and not record.error]
        by_route[route] = {
            "completed": len(successful),
            "expected_4xx": 0,
            "unexpected_failures": sum(record.status != 200 or bool(record.error) for record in sample_records),
            "p50_ms": percentile([record.latency_ms for record in successful], 0.50),
            "p95_ms": percentile([record.latency_ms for record in successful], 0.95),
            "p99_ms": percentile([record.latency_ms for record in successful], 0.99),
            "max_ms": max((record.latency_ms for record in successful), default=None),
            "warmup_requests": sum(record.phase == "warmup" for record in route_records),
            "sample_requests": sum(record.phase == "sample" for record in route_records),
        }
    return by_route


@dataclass
class SSEStats:
    merchant: str
    frames: int = 0
    duplicate_frames: int = 0
    bytes_received: int = 0
    first_frame_at: float | None = None
    frame_hashes: list[str] = field(default_factory=list)
    status_changes: int = 0
    observation_latencies_ms: list[float] = field(default_factory=list)
    connections_opened: int = 0
    connections_closed: int = 0
    reconnects: int = 0
    idle_closures: int = 0
    premature_closures: int = 0
    coverage_gaps_ms: list[float] = field(default_factory=list)
    error: str = ""

    def record(self, payload: dict[str, Any], received_epoch: float, db_rows: dict[str, dict[str, Any]]) -> None:
        encoded = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode("utf-8")
        digest = hashlib.sha256(encoded).hexdigest()
        if self.first_frame_at is None:
            self.first_frame_at = received_epoch
        if self.frame_hashes and self.frame_hashes[-1] == digest:
            self.duplicate_frames += 1
        self.frame_hashes.append(digest)
        self.frames += 1
        for task in payload.get("generation_tasks", []) or []:
            task_id = task.get("id")
            row = db_rows.get(task_id)
            if row is None or task.get("status") not in TERMINAL | {"queued", "running"}:
                continue
            self.status_changes += 1
            # The API does not expose PostgreSQL commit time. This is a
            # persisted-task-timestamp proxy only.
            updated_at = row.get("updated_at")
            wire_timestamp = task.get("progress_updated_at") or task.get("finished_at") or task.get("started_at")
            if wire_timestamp:
                try:
                    updated_at = datetime.fromisoformat(str(wire_timestamp).replace("Z", "+00:00"))
                except ValueError:
                    pass
            if updated_at is not None:
                self.observation_latencies_ms.append(max(0.0, received_epoch - updated_at.timestamp()) * 1000)


class DBProbe:
    def __init__(self, db_url: str):
        self.connection = psycopg.connect(db_url, autocommit=True)
        self.lock = asyncio.Lock()

    async def task_rows(self, task_ids: set[str]) -> dict[str, dict[str, Any]]:
        if not task_ids:
            return {}
        async with self.lock:
            return await asyncio.to_thread(self._task_rows, list(task_ids))

    def _task_rows(self, task_ids: list[str]) -> dict[str, dict[str, Any]]:
        with self.connection.cursor() as cursor:
            cursor.execute(
                """
                SELECT t.id, t.status, t.prompt,
                       COALESCE(t.progress_updated_at, t.finished_at, t.started_at, s.updated_at, t.created_at) AS updated_at,
                       t.created_at, t.started_at, t.finished_at,
                       d.sent_at
                FROM image_session_generation_tasks t
                JOIN image_sessions s ON s.id = t.session_id
                LEFT JOIN async_dispatches d
                  ON d.actor_name = 'run_image_session_generation_task' AND d.aggregate_id = t.id
                WHERE t.id = ANY(%s)
                """,
                (task_ids,),
            )
            columns = [item.name for item in cursor.description]
            return {str(row[0]): dict(zip(columns, row)) for row in cursor.fetchall()}

    async def close(self) -> None:
        await asyncio.to_thread(self.connection.close)


def extract_tasks(payload: dict[str, Any]) -> list[dict[str, Any]]:
    tasks = payload.get("generation_tasks")
    if isinstance(tasks, list):
        return [item for item in tasks if isinstance(item, dict)]
    return []


def merchant_prefix(index: int, kind: str) -> str:
    return f"cap0908-{kind}-{index:02d}-"


async def login(client: aiohttp.ClientSession, base_url: str, merchant: dict[str, Any]) -> str:
    async with client.post(
        f"{base_url}/api/auth/session",
        json={"email": merchant["email"], "password": merchant["password"]},
    ) as response:
        body = await response.read()
        if response.status != 200:
            raise RuntimeError(f"login {merchant['email']} status={response.status} body={body[:200]!r}")
        cookie = response.cookies.get("session")
        if cookie is None:
            raise RuntimeError(f"login {merchant['email']} did not return session cookie")
        return cookie.value


async def validate_response(route: str, index: int, body: bytes) -> None:
    payload = json.loads(body or b"{}")
    if route == "products":
        for item in payload.get("items", []):
            if not str(item.get("id", "")).startswith(merchant_prefix(index, "p")):
                raise RuntimeError(f"product cross-merchant row for merchant {index}: {item.get('id')}")
    elif route == "media":
        for item in payload.get("items", []):
            if not str(item.get("id", "")).startswith(merchant_prefix(index, "a")):
                raise RuntimeError(f"asset cross-merchant row for merchant {index}: {item.get('id')}")
    elif route == "session_status":
        if payload.get("id") != f"cap0908-s-{index:02d}":
            raise RuntimeError(f"session cross-merchant row for merchant {index}: {payload.get('id')}")
    elif route == "quota":
        if payload.get("merchant_id") != f"cap0908-m-{index:02d}":
            raise RuntimeError(f"quota cross-merchant row for merchant {index}: {payload.get('merchant_id')}")


async def one_read(
    client: aiohttp.ClientSession,
    base_url: str,
    merchant_index: int,
    cookies: list[str],
    route: str,
    records: list[RequestRecord],
    phase: str,
) -> None:
    paths = {
        "products": f"/api/v2/products?page={random.randint(1, 20)}&page_size=50",
        "media": "/api/media-library?limit=50",
        "session_status": f"/api/image-sessions/cap0908-s-{merchant_index:02d}/status",
        "quota": f"/api/merchants/cap0908-m-{merchant_index:02d}/quota",
    }
    started = time.perf_counter()
    status = 0
    error = ""
    try:
        async with client.get(
            f"{base_url}{paths[route]}",
            headers={"Cookie": f"session={cookies[merchant_index]}"},
        ) as response:
            status = response.status
            body = await response.read()
            if status == 200:
                await validate_response(route, merchant_index, body)
            else:
                # Every mixed-workload read is legitimate. 4xx is therefore
                # unexpected; expected_4xx is reserved for negative probes.
                error = body[:200].decode("utf-8", errors="replace")
    except Exception as exc:  # noqa: BLE001 - connection failures are outcomes.
        error = repr(exc)
    records.append(RequestRecord(route, f"cap0908-m-{merchant_index:02d}", status, (time.perf_counter() - started) * 1000, error, phase))


async def read_pump(
    client: aiohttp.ClientSession,
    base_url: str,
    cookies: list[str],
    clients: int,
    rate: float,
    warmup_end: float,
    window_end: float,
    records: list[RequestRecord],
) -> None:
    next_request = time.monotonic()
    pending: set[asyncio.Task[None]] = set()
    while time.monotonic() < window_end:
        now = time.monotonic()
        phase = "warmup" if now < warmup_end else "sample"
        route = random.choices(READ_ROUTES, weights=READ_WEIGHTS, k=1)[0]
        merchant_index = random.randrange(clients) % len(cookies)
        pending.add(asyncio.create_task(one_read(client, base_url, merchant_index, cookies, route, records, phase)))
        pending = {task for task in pending if not task.done()}
        next_request += 1 / rate
        await asyncio.sleep(max(0.0, next_request - time.monotonic()))
    if pending:
        await asyncio.gather(*pending, return_exceptions=True)


async def sse_reader(
    client: aiohttp.ClientSession,
    base_url: str,
    merchant_index: int,
    cookie: str,
    stats: SSEStats,
    db_probe: DBProbe,
    task_ids: set[str],
    active_until: float,
    stop_at: float,
) -> None:
    path = f"/api/image-sessions/cap0908-s-{merchant_index:02d}/events"
    last_closed_at: float | None = None
    retry_delay = 0.1
    while time.monotonic() < stop_at:
        try:
            async with client.get(
                f"{base_url}{path}",
                headers={"Cookie": f"session={cookie}"},
                timeout=aiohttp.ClientTimeout(total=None, sock_read=None),
            ) as response:
                last_payload: dict[str, Any] | None = None
                connected_at = time.monotonic()
                if last_closed_at is not None:
                    stats.coverage_gaps_ms.append(max(0.0, connected_at - last_closed_at) * 1000)
                stats.connections_opened += 1
                if response.status != 200:
                    stats.error = f"SSE status={response.status}"
                else:
                    async for raw_line in response.content:
                        if time.monotonic() >= stop_at:
                            return
                        stats.bytes_received += len(raw_line)
                        line = raw_line.decode("utf-8", errors="replace").strip()
                        if not line.startswith("data: "):
                            continue
                        payload = json.loads(line[6:])
                        last_payload = payload
                        received_epoch = time.time()
                        rows = await db_probe.task_rows(set(task_ids))
                        stats.record(payload, received_epoch, rows)
                stats.connections_closed += 1
                closed_at = time.monotonic()
                last_closed_at = closed_at
                if closed_at >= stop_at:
                    return
                # The server deliberately closes after an idle snapshot. A
                # successor may already be queued by the time EOF is observed;
                # consulting the mutable task set here misclassifies that close.
                if last_payload is not None and last_payload.get("has_active_generation_task") is False:
                    stats.idle_closures += 1
                else:
                    stats.premature_closures += 1
                    stats.error = stats.error or "SSE closed without a final idle snapshot"
                stats.reconnects += 1
        except asyncio.CancelledError:
            raise
        except Exception as exc:  # noqa: BLE001 - disconnects are measured.
            stats.error = repr(exc)
            last_closed_at = time.monotonic()
            if last_closed_at >= stop_at:
                return
            stats.reconnects += 1
        await asyncio.sleep(min(retry_delay, max(0.0, stop_at - time.monotonic())))


async def submit_generation(
    client: aiohttp.ClientSession,
    base_url: str,
    cookies: list[str],
    merchant_index: int,
    prompt: str,
    base_asset_id: str | None,
    phase: str,
    records: list[RequestRecord],
) -> str | None:
    started = time.perf_counter()
    status = 0
    error = ""
    task_id: str | None = None
    try:
        request_body: dict[str, Any] = {"prompt": prompt, "size": "64x64", "generation_count": 1}
        if base_asset_id:
            request_body["base_asset_id"] = base_asset_id
        async with client.post(
            f"{base_url}/api/image-sessions/cap0908-s-{merchant_index:02d}/generate",
            headers={"Cookie": f"session={cookies[merchant_index]}"},
            json=request_body,
        ) as response:
            status = response.status
            body = await response.read()
            if status == 202:
                tasks = extract_tasks(json.loads(body))
                matches = [item for item in tasks if str(item.get("prompt") or "") == prompt]
                if len(matches) == 1 and matches[0].get("id"):
                    # The response is a recent-task projection. Match the
                    # unique prompt so concurrent dispatcher/readback timing
                    # cannot associate this HTTP request with an older task.
                    task_id = str(matches[0]["id"])
                elif len(matches) > 1:
                    error = f"accepted response had non-unique prompt match count={len(matches)}"
                else:
                    error = "accepted response had no unique task for prompt"
            else:
                error = body[:200].decode("utf-8", errors="replace")
    except Exception as exc:  # noqa: BLE001 - accepted failures remain evidence.
        error = repr(exc)
    records.append(RequestRecord("generation_slot", f"cap0908-m-{merchant_index:02d}", status, (time.perf_counter() - started) * 1000, error, phase))
    return task_id


def latest_generated_assets(db_url: str, session_ids: list[str]) -> dict[str, str]:
    if not session_ids:
        return {}
    with psycopg.connect(db_url, autocommit=True) as connection:
        with connection.cursor() as cursor:
            cursor.execute(
                """
                SELECT DISTINCT ON (session_id) session_id, id
                FROM image_session_assets
                WHERE session_id = ANY(%s) AND kind = 'generated_image'
                ORDER BY session_id, created_at DESC, id DESC
                """,
                (session_ids,),
            )
            return {str(row[0]): str(row[1]) for row in cursor.fetchall()}


async def wait_one_terminal(task_id: str, db_probe: DBProbe, timeout: float = 180) -> dict[str, Any] | None:
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        row = (await db_probe.task_rows({task_id})).get(task_id)
        if row and row.get("status") in TERMINAL:
            return row
        await asyncio.sleep(0.2)
    return (await db_probe.task_rows({task_id})).get(task_id)


def provider_prompt_finish_epoch(path: str | None, prompt: str) -> float | None:
    if not path:
        return None
    event_path = Path(path)
    if not event_path.exists():
        return None
    try:
        merged = merge_events(event_path)
    except OSError:
        return None
    finish_times = [
        float(event["finished_epoch"])
        for event in merged.values()
        if prompt_matches_prefix(str(event.get("prompt", "")), prompt)
        and isinstance(event.get("finished_epoch"), (int, float))
        and isinstance(event.get("responded_epoch"), (int, float))
    ]
    return max(finish_times, default=None)


async def wait_provider_finished(path: str | None, prompt: str, timeout: float = 30) -> float | None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        finish_epoch = await asyncio.to_thread(provider_prompt_finish_epoch, path, prompt)
        if finish_epoch is not None:
            return finish_epoch
        await asyncio.sleep(0.2)
    return await asyncio.to_thread(provider_prompt_finish_epoch, path, prompt)


async def generation_slot(
    client: aiohttp.ClientSession,
    base_url: str,
    db_url: str,
    cookies: list[str],
    merchant_index: int,
    label: str,
    round_number: int,
    start_gate: asyncio.Event,
    ready: asyncio.Event,
    ready_slots: set[int],
    provider_barrier: asyncio.Barrier,
    window_end: dict[str, float],
    sample_start: dict[str, float],
    active_task_ids: dict[int, set[str]],
    preflight_task_ids: list[str],
    window_task_ids: list[str],
    records: list[RequestRecord],
    db_probe: DBProbe,
    provider_events_path: str | None,
    provider_wait_failures: list[str],
    provider_wait_trace: list[dict[str, Any]],
) -> None:
    session_id = f"cap0908-s-{merchant_index:02d}"
    slot_active_task_ids = active_task_ids[merchant_index]
    base_assets = await asyncio.to_thread(latest_generated_assets, db_url, [session_id])
    sequence = 0
    current_task: str | None = None
    current_prompt: str | None = None
    retry_until = time.monotonic() + 30

    # A clean fixture has no generated session asset yet. Prime each slot
    # before the timed window so every successor can carry a valid base asset.
    if not base_assets:
        prime_task = await submit_generation(
            client,
            base_url,
            cookies,
            merchant_index,
            f"capacity-{label}-round-{round_number}-prime-slot-{merchant_index:02d}",
            None,
            "preflight",
            records,
        )
        if prime_task is not None:
            preflight_task_ids.append(prime_task)
            await wait_one_terminal(prime_task, db_probe)
            prime_prompt = f"capacity-{label}-round-{round_number}-prime-slot-{merchant_index:02d}"
            finish_epoch = await wait_provider_finished(provider_events_path, prime_prompt)
            provider_wait_trace.append({
                "slot": merchant_index,
                "prompt": prime_prompt,
                "finish_epoch": finish_epoch,
                "observed_epoch": time.time(),
            })
            if finish_epoch is None:
                provider_wait_failures.append(prime_prompt)
            base_assets.update(await asyncio.to_thread(latest_generated_assets, db_url, [session_id]))

    # The barrier ensures all three slots are actually accepted before the
    # mixed read/SSE window starts.
    while current_task is None and time.monotonic() < retry_until:
        sequence += 1
        current_prompt = f"capacity-{label}-round-{round_number}-slot-{merchant_index:02d}-{sequence:04d}"
        current_task = await submit_generation(
            client,
            base_url,
            cookies,
            merchant_index,
            current_prompt,
            base_assets.get(session_id),
            "warmup",
            records,
        )
        if current_task is None:
            current_prompt = None
            await asyncio.sleep(0.5)
    if current_task is None:
        return
    slot_active_task_ids.add(current_task)
    window_task_ids.append(current_task)
    ready_slots.add(merchant_index)
    ready.set()
    await start_gate.wait()

    async def submit_successor() -> tuple[str | None, str | None]:
        nonlocal sequence
        if time.monotonic() >= window_end["value"]:
            return None, None
        phase = "sample" if time.monotonic() >= sample_start["value"] else "warmup"
        retry_until = min(window_end["value"], time.monotonic() + 30)
        while time.monotonic() < retry_until:
            sequence += 1
            prompt = f"capacity-{label}-round-{round_number}-slot-{merchant_index:02d}-{sequence:04d}"
            task_id = await submit_generation(
                client,
                base_url,
                cookies,
                merchant_index,
                prompt,
                base_assets.get(session_id),
                phase,
                records,
            )
            if task_id:
                slot_active_task_ids.add(task_id)
                window_task_ids.append(task_id)
                return task_id, prompt
            await asyncio.sleep(0.5)
        return None, None

    while current_task is not None:
        await wait_one_terminal(current_task, db_probe)
        if current_prompt:
            finish_epoch = await wait_provider_finished(provider_events_path, current_prompt)
            provider_wait_trace.append({
                "slot": merchant_index,
                "prompt": current_prompt,
                "finish_epoch": finish_epoch,
                "observed_epoch": time.time(),
            })
            if finish_epoch is None:
                provider_wait_failures.append(current_prompt)
        slot_active_task_ids.discard(current_task)
        base_assets.update(await asyncio.to_thread(latest_generated_assets, db_url, [session_id]))
        # Advance the three slots as a batch. The fixed dispatcher can start a
        # newly queued task before the prior task's provider event is flushed,
        # so the batch barrier makes the measured provider overlap explicit:
        # all three prior requests must have finished before any successor is
        # submitted.
        if time.monotonic() >= window_end["value"]:
            await provider_barrier.abort()
            return
        try:
            await provider_barrier.wait()
        except asyncio.BrokenBarrierError:
            return
        current_task, current_prompt = await submit_successor()
        if current_task is None:
            await provider_barrier.abort()
            return


def provider_summary(path: str | None, prefix: str) -> dict[str, Any]:
    merged = merge_events(path) if path else {}
    events = [item for item in merged.values() if prompt_matches_prefix(str(item.get("prompt", "")), prefix)]
    if not events:
        raise RuntimeError(f"no provider events matched {prefix!r} in {path}")
    execution_boundaries: list[tuple[float, int]] = []
    http_boundaries: list[tuple[float, int]] = []
    completed = 0
    http_received = 0
    http_completed = 0
    invalid_intervals: list[str] = []
    for event in events:
        request_id = str(event.get("request_id"))
        try:
            validate_event(event)
        except RuntimeError as error:
            if "negative or non-monotonic" not in str(error):
                raise
            invalid_intervals.append(request_id)
            continue
        received = event.get("received_monotonic")
        started = event.get("started_monotonic")
        finished = event.get("finished_monotonic")
        responded = event.get("responded_monotonic")
        execution_boundaries.extend(((float(started), 1), (float(finished), -1)))
        http_boundaries.extend(((float(received), 1), (float(responded), -1)))
        completed += 1
        http_received += 1
        http_completed += 1
    if invalid_intervals:
        raise RuntimeError(f"provider event contains negative or non-monotonic intervals: {invalid_intervals[:5]}")

    active = 0
    execution_maximum = 0
    for _, delta in sorted(execution_boundaries, key=lambda item: (item[0], item[1])):
        active += delta
        execution_maximum = max(execution_maximum, active)
    http_active = 0
    http_maximum = 0
    for _, delta in sorted(http_boundaries, key=lambda item: (item[0], item[1])):
        http_active += delta
        http_maximum = max(http_maximum, http_active)
    return {
        "event_file": path,
        "prompt_prefix": prefix,
        "requests_started": len(events),
        "requests_completed": completed,
        "execution_max_concurrency": execution_maximum,
        "http_requests_received": http_received,
        "http_responses_completed": http_completed,
        "http_incomplete_responses": http_received - http_completed,
        "http_max_concurrency": http_maximum,
        "expected_concurrency": GENERATION_SLOTS,
    }


def write_window_marker(path: Path, payload: dict[str, Any]) -> None:
    if path.exists():
        raise RuntimeError(f"load window marker already exists: {path}")
    temporary = path.with_name(f".{path.name}.tmp")
    temporary.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    temporary.replace(path)


async def run_round(args: argparse.Namespace) -> dict[str, Any]:
    repo_root = Path(__file__).resolve().parents[2]
    identity = verify_identity(repo_root, Path(args.identity))
    manifest = json.loads(Path(args.manifest).read_text(encoding="utf-8"))
    if manifest.get("source_identity_sha256") != identity["source_identity_sha256"]:
        raise RuntimeError("fixture manifest source identity does not match the running tools")
    merchants = manifest["merchants"]
    timeout = aiohttp.ClientTimeout(total=30)
    connector = aiohttp.TCPConnector(limit=0, limit_per_host=0, enable_cleanup_closed=True)
    out_dir = Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)
    sample_path = out_dir / f"{args.label.lower()}-round-{args.round}-requests.csv"
    summary_path = out_dir / f"{args.label.lower()}-round-{args.round}.json"
    provider_event_path = out_dir / f"{args.label.lower()}-round-{args.round}-provider-events.jsonl"
    for path in (sample_path, summary_path, provider_event_path):
        if path.exists():
            raise RuntimeError(f"capacity output already exists: {path}")
    records: list[RequestRecord] = []
    random.seed(908000 + args.round + (0 if args.label == "L1" else 100))
    stats: list[SSEStats] = []
    preflight_task_ids: list[str] = []
    window_task_ids: list[str] = []
    generation_records: list[RequestRecord] = []
    generation_rows: dict[str, dict[str, Any]] = {}
    generation_errors: list[str] = []
    provider_wait_failures: list[str] = []
    provider_wait_trace: list[dict[str, Any]] = []
    provider_prefix = f"capacity-{args.label}-round-{args.round}-"
    window_file = Path(args.window_file)

    async with aiohttp.ClientSession(timeout=timeout, connector=connector, headers={"Origin": BROWSER_ORIGIN}) as client:
        cookies = [await login(client, args.base_url, merchant) for merchant in merchants]
        db_probe = DBProbe(args.db_url)
        active_task_ids = {index: set() for index in range(GENERATION_SLOTS)}
        ready_slots: set[int] = set()
        ready_events = [asyncio.Event() for _ in range(GENERATION_SLOTS)]
        start_gate = asyncio.Event()
        provider_barrier = asyncio.Barrier(GENERATION_SLOTS)
        window_end = {"value": 0.0}
        sample_start = {"value": 0.0}
        slot_tasks = [
            asyncio.create_task(
                generation_slot(
                    client,
                    args.base_url,
                    args.db_url,
                    cookies,
                    index,
                    args.label,
                    args.round,
                    start_gate,
                    ready_events[index],
                    ready_slots,
                    provider_barrier,
                    window_end,
                    sample_start,
                    active_task_ids,
                    preflight_task_ids,
                    window_task_ids,
                    generation_records,
                    db_probe,
                    args.provider_events,
                    provider_wait_failures,
                    provider_wait_trace,
                )
            )
            for index in range(GENERATION_SLOTS)
        ]
        try:
            await asyncio.wait_for(asyncio.gather(*(event.wait() for event in ready_events)), timeout=35)
        except asyncio.TimeoutError as exc:
            for task in slot_tasks:
                task.cancel()
            await asyncio.gather(*slot_tasks, return_exceptions=True)
            await db_probe.close()
            raise RuntimeError(f"only {len(ready_slots)}/{GENERATION_SLOTS} generation slots became active") from exc

        window_start = time.monotonic()
        window_start_epoch = time.time()
        sample_start["value"] = window_start + args.warmup
        window_end["value"] = sample_start["value"] + args.duration
        stop_at = window_end["value"] + 5
        write_window_marker(
            window_file,
            {
                "label": args.label,
                "round": args.round,
                "window_start_epoch": window_start_epoch,
                "window_end_epoch": window_start_epoch + args.warmup + args.duration,
                "sample_start_epoch": window_start_epoch + args.warmup,
                "sample_end_epoch": window_start_epoch + args.warmup + args.duration,
            },
        )

        # Keep all logical SSE clients on sessions that have a live generation
        # task. Read requests still cover all ten merchants.
        stats = [SSEStats(f"cap0908-m-{index % GENERATION_SLOTS:02d}") for index in range(args.clients)]
        sse_tasks = [
            asyncio.create_task(
                sse_reader(
                    client,
                    args.base_url,
                    index % GENERATION_SLOTS,
                    cookies[index % GENERATION_SLOTS],
                    item,
                    db_probe,
                    active_task_ids[index % GENERATION_SLOTS],
                    window_end["value"],
                    stop_at,
                )
            )
            for index, item in enumerate(stats)
        ]
        start_gate.set()
        read_task = asyncio.create_task(
            read_pump(
                client,
                args.base_url,
                cookies,
                args.clients,
                args.rate,
                sample_start["value"],
                window_end["value"],
                records,
            )
        )
        task_results = await asyncio.gather(read_task, *slot_tasks, return_exceptions=True)
        generation_errors = [repr(result) for result in task_results[1:] if isinstance(result, BaseException)]
        await asyncio.sleep(max(0.0, stop_at - time.monotonic()))
        for task in sse_tasks:
            task.cancel()
        await asyncio.gather(*sse_tasks, return_exceptions=True)
        generation_rows = await db_probe.task_rows(set(preflight_task_ids + window_task_ids))
        await db_probe.close()

    records.extend(generation_records)
    provider_event_snapshot = snapshot_events(args.provider_events, provider_event_path, (provider_prefix,))
    provider = provider_summary(str(provider_event_path), provider_prefix)
    expected_task_ids = preflight_task_ids + window_task_ids
    provider_events = merge_events(provider_event_path)
    missing_task_rows = [
        str(task_id) for task_id in expected_task_ids if str(task_id) not in generation_rows
    ]
    provider_attribution_issues: list[dict[str, Any]] = []
    for task_id, row in generation_rows.items():
        prompt = str(row.get("prompt") or "")
        if not prompt:
            provider_attribution_issues.append({"task_id": str(task_id), "matches": 0, "reason": "task prompt missing"})
            continue
        matches = [
            event for event in provider_events.values()
            if prompt_matches_prefix(str(event.get("prompt") or ""), prompt)
        ]
        if len(matches) != 1:
            provider_attribution_issues.append({"task_id": str(task_id), "matches": len(matches)})
    provider_evidence_complete = (
        not generation_errors
        and not provider_wait_failures
        and not missing_task_rows
        and len(expected_task_ids) == len(set(expected_task_ids))
        and provider["requests_started"] == len(expected_task_ids)
        and provider["requests_completed"] == len(expected_task_ids)
        and provider["http_incomplete_responses"] == 0
        and not provider_attribution_issues
    )
    with sample_path.open("w", newline="", encoding="utf-8") as stream:
        writer = csv.DictWriter(stream, fieldnames=["route", "merchant", "status", "latency_ms", "error", "phase"])
        writer.writeheader()
        for record in records:
            writer.writerow(record.__dict__)

    read_records = [record for record in records if record.route in READ_ROUTES]
    by_route = summarize_reads(read_records)
    completed = sum(item["completed"] for item in by_route.values())
    unexpected = sum(item["unexpected_failures"] for item in by_route.values())
    sse_summary = [
        {
            "merchant": item.merchant,
            "frames": item.frames,
            "duplicate_frames": item.duplicate_frames,
            "bytes_received": item.bytes_received,
            "first_frame": item.first_frame_at,
            "proxy_observation_p95_ms": percentile(item.observation_latencies_ms, 0.95),
            "observations": len(item.observation_latencies_ms),
            "connections_opened": item.connections_opened,
            "connections_closed": item.connections_closed,
            "reconnects": item.reconnects,
            "idle_closures": item.idle_closures,
            "premature_closures": item.premature_closures,
            "max_coverage_gap_ms": max(item.coverage_gaps_ms, default=0.0),
            "error": item.error,
        }
        for item in stats
    ]
    sse_observation_failures = [
        {
            "merchant": item.merchant,
            "connections_opened": item.connections_opened,
            "frames": item.frames,
            "premature_closures": item.premature_closures,
            "error": item.error,
        }
        for item in stats
        if item.connections_opened == 0 or item.frames == 0 or item.premature_closures or item.error
    ]
    sse_observation_complete = bool(stats) and not sse_observation_failures
    round_evidence_complete = provider_evidence_complete and sse_observation_complete
    status_counts = Counter(str(row.get("status")) for row in generation_rows.values())
    summary = {
        "label": args.label,
        "round": args.round,
        "clients": args.clients,
        "rate_per_second": args.rate,
        "warmup_seconds": args.warmup,
        "sample_seconds": args.duration,
        "read_requests_completed": completed,
        "read_requests_expected_4xx": 0,
        "read_requests_unexpected_failures": unexpected,
        "read_requests_unexpected_failure_rate": (unexpected / (completed + unexpected)) if completed + unexpected else None,
        "routes": by_route,
        "read_requests_by_phase": {
            "warmup": sum(record.phase == "warmup" for record in read_records),
            "sample": sum(record.phase == "sample" for record in read_records),
        },
        "generation_setup": {
            "slots_expected": GENERATION_SLOTS,
            "slots_ready": len(ready_slots),
            "preflight_tasks_accepted": len(preflight_task_ids),
            "tasks_accepted": len(window_task_ids),
            "all_tasks_accepted": len(preflight_task_ids) + len(window_task_ids),
            "unique_task_ids": len(set(preflight_task_ids + window_task_ids)),
            "duplicate_task_ids": len(preflight_task_ids + window_task_ids) - len(set(preflight_task_ids + window_task_ids)),
            "task_statuses": dict(status_counts),
            "unresolved_tasks": len(missing_task_rows),
            "errors": generation_errors,
            "provider_wait_failures": provider_wait_failures,
            "provider_wait_trace": provider_wait_trace,
            "records": [record.__dict__ for record in generation_records],
        },
        "provider": {
            **provider,
            "event_snapshot": provider_event_snapshot,
            "attribution_issues": provider_attribution_issues,
        },
        "round_evidence": {
            "status": "complete" if round_evidence_complete else "incomplete",
            "provider_complete": provider_evidence_complete,
            "sse_complete": sse_observation_complete,
            "sse_failures": sse_observation_failures,
            "missing_task_rows": missing_task_rows,
            "provider_wait_failures": provider_wait_failures,
            "generation_errors": generation_errors,
        },
        "sse": {
            "connections": len(stats),
            "connections_opened": sum(item.connections_opened for item in stats),
            "connections_closed": sum(item.connections_closed for item in stats),
            "reconnects": sum(item.reconnects for item in stats),
            "idle_closures": sum(item.idle_closures for item in stats),
            "premature_closures": sum(item.premature_closures for item in stats),
            "max_coverage_gap_ms": max((max(item.coverage_gaps_ms, default=0.0) for item in stats), default=0.0),
            "duplicate_frames": sum(item.duplicate_frames for item in stats),
            "errors": sum(bool(item.error) for item in stats),
            "clients": sse_summary,
            "commit_to_observation": {
                "status": "unmeasurable",
                "reason": "the API exposes no PostgreSQL commit timestamp; proxy uses persisted task progress/terminal timestamp sampled after the SSE frame",
                "proxy_p95_ms": percentile([value for item in stats for value in item.observation_latencies_ms], 0.95),
            },
        },
        "requests_csv": str(sample_path),
        "fixture": manifest["fixture_version"],
        "commit": manifest["commit"],
        "source_identity_sha256": identity["source_identity_sha256"],
        "load_window": {
            "marker": str(window_file),
            "window_start_epoch": window_start_epoch,
            "window_end_epoch": window_start_epoch + args.warmup + args.duration,
            "sample_start_epoch": window_start_epoch + args.warmup,
            "sample_end_epoch": window_start_epoch + args.warmup + args.duration,
        },
    }
    summary_path.write_text(json.dumps(summary, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(summary, sort_keys=True))
    if not round_evidence_complete:
        raise RuntimeError("capacity round evidence is incomplete; see round_evidence in the written summary")
    return summary


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-url", default="http://127.0.0.1:30182")
    parser.add_argument("--db-url", required=True)
    parser.add_argument("--manifest", required=True)
    parser.add_argument("--label", choices=["L1", "L2"], required=True)
    parser.add_argument("--round", type=int, required=True)
    parser.add_argument("--clients", type=int, required=True)
    parser.add_argument("--rate", type=float, required=True)
    parser.add_argument("--warmup", type=float, default=60)
    parser.add_argument("--duration", type=float, default=300)
    parser.add_argument("--out-dir", required=True)
    parser.add_argument("--provider-events", required=True)
    parser.add_argument("--identity", required=True)
    parser.add_argument("--window-file", required=True)
    return parser.parse_args()


if __name__ == "__main__":
    asyncio.run(run_round(parse_args()))
