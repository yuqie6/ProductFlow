#!/usr/bin/env python3
"""Exercise Redis outage plus in-flight worker/API restart on the dedicated stack."""

import argparse
import asyncio
import json
import subprocess
import time
from collections import Counter
from pathlib import Path
from typing import Any

import aiohttp
import psycopg

from capacity_events import merge_events, snapshot_events, validate_event
from capacity_identity import verify_identity
from river_evidence import image_session_river_join, image_session_river_projection
from run_contention import quota_report


BROWSER_ORIGIN = "http://127.0.0.1:30182"
TERMINAL = {"succeeded", "failed", "unknown", "cancelled"}


def event_matches_prompt(event_prompt: object, expected_prompt: str) -> bool:
    value = str(event_prompt or "")
    return value == expected_prompt or expected_prompt in value.splitlines()


def provider_trace_report(
    source: str | Path,
    destination: str | Path,
    expected_prompts: list[str],
) -> dict[str, Any]:
    """Bind each fault task to exactly one complete mock-provider call."""
    snapshot = snapshot_events(source, destination, expected_prompts)
    events = merge_events(destination)
    expected = list(dict.fromkeys(expected_prompts))
    by_prompt: dict[str, list[str]] = {prompt: [] for prompt in expected}
    invalid_events: list[str] = []
    unknown_events: list[str] = []
    for request_id, event in events.items():
        try:
            validate_event(event)
        except RuntimeError as error:
            invalid_events.append(str(error))
        matches = [prompt for prompt in expected if event_matches_prompt(event.get("prompt"), prompt)]
        if len(matches) == 1:
            by_prompt[matches[0]].append(str(request_id))
        else:
            unknown_events.append(str(request_id))
    duplicate_prompts = {prompt: request_ids for prompt, request_ids in by_prompt.items() if len(request_ids) != 1}
    lifecycle_line_count_pass = snapshot["line_count"] == snapshot["request_count"] * 4
    return {
        "status": "measurable",
        "pass": bool(expected)
        and snapshot["request_count"] == len(expected)
        and lifecycle_line_count_pass
        and not invalid_events
        and not unknown_events
        and not duplicate_prompts,
        "snapshot": snapshot,
        "expected_prompt_count": len(expected),
        "by_prompt": by_prompt,
        "duplicate_prompts": duplicate_prompts,
        "unknown_request_ids": unknown_events,
        "invalid_events": invalid_events,
        "lifecycle_line_count_pass": lifecycle_line_count_pass,
    }


def docker(*args: str) -> str:
    return subprocess.check_output(["docker", *args], text=True, stderr=subprocess.STDOUT).strip()


class DBProbe:
    def __init__(self, db_url: str):
        self.connection = psycopg.connect(db_url, autocommit=True)

    def task(self, task_id: str) -> dict[str, Any]:
        with self.connection.cursor() as cursor:
            cursor.execute(
                f"""
                SELECT t.id, t.status, t.prompt, t.billing_seq, s.merchant_id,
                       t.created_at, t.started_at, t.finished_at,
                       COALESCE(t.progress_updated_at, t.finished_at, t.started_at, s.updated_at, t.created_at) AS updated_at,
                       {image_session_river_projection()}
                FROM image_session_generation_tasks t
                JOIN image_sessions s ON s.id = t.session_id
                {image_session_river_join()}
                WHERE t.id = %s
                """,
                (task_id,),
            )
            row = cursor.fetchone()
            if row is None:
                return {}
            columns = [item.name for item in cursor.description]
            return dict(zip(columns, row))

    def quota_evidence(self, task_id: str) -> dict[str, Any]:
        task = self.task(task_id)
        merchant_id = task.get("merchant_id")
        pattern = f"%{task_id}%"
        with self.connection.cursor() as cursor:
            cursor.execute(
                """
                SELECT event_type, idempotency_key, amount_units, available_after, reserved_after,
                       hold_id, count(*) OVER (PARTITION BY event_type, idempotency_key) AS copies
                FROM merchant_quota_events
                WHERE merchant_id = %s AND idempotency_key LIKE %s
                ORDER BY idempotency_key, event_type
                """,
                (merchant_id, pattern),
            )
            columns = [item.name for item in cursor.description]
            events = [dict(zip(columns, row)) for row in cursor.fetchall()]
            cursor.execute(
                """
                SELECT id, idempotency_key, status, amount_units, settled_units
                FROM merchant_quota_holds
                WHERE merchant_id = %s AND idempotency_key LIKE %s
                ORDER BY idempotency_key
                """,
                (merchant_id, pattern),
            )
            hold_columns = [item.name for item in cursor.description]
            holds = [dict(zip(hold_columns, row)) for row in cursor.fetchall()]
        event_counts = Counter(str(item["event_type"]) for item in events)
        duplicates = [
            {"event_type": item["event_type"], "idempotency_key": item["idempotency_key"], "copies": int(item["copies"])}
            for item in events
            if int(item["copies"]) > 1
        ]
        status = str(task.get("status") or "")
        exact_report = quota_report(
            [task_id],
            [{"id": task_id, "status": status, "quota_events": events, "quota_holds": holds}],
            expected_quota_units=1,
        )
        return {
            "task_id": task_id,
            "task_status": status,
            "event_counts": dict(event_counts),
            "events": events,
            "holds": holds,
            "duplicates": duplicates,
            "semantic_complete": exact_report["lifecycle_complete"],
            "exact_quota_report": exact_report,
        }

    def close(self) -> None:
        self.connection.close()


async def login(client: aiohttp.ClientSession, base_url: str, merchant: dict[str, Any]) -> str:
    async with client.post(
        f"{base_url}/api/auth/session",
        json={"email": merchant["email"], "password": merchant["password"]},
    ) as response:
        body = await response.read()
        if response.status != 200:
            raise RuntimeError(f"login failed status={response.status} body={body[:200]!r}")
        cookie = response.cookies.get("session")
        if cookie is None:
            raise RuntimeError("login cookie missing")
        return cookie.value


def latest_generated_asset(db_url: str, session_id: str) -> str | None:
    with psycopg.connect(db_url, autocommit=True) as connection:
        with connection.cursor() as cursor:
            cursor.execute(
                """
                SELECT id FROM image_session_assets
                WHERE session_id = %s AND kind = 'generated_image'
                ORDER BY created_at DESC, id DESC LIMIT 1
                """,
                (session_id,),
            )
            row = cursor.fetchone()
            return str(row[0]) if row else None


async def submit(
    client: aiohttp.ClientSession,
    base_url: str,
    cookie: str,
    session_id: str,
    prompt: str,
    base_asset_id: str | None = None,
) -> tuple[str, float, int, str]:
    request_body: dict[str, Any] = {"prompt": prompt, "size": "64x64", "generation_count": 1}
    if base_asset_id:
        request_body["base_asset_id"] = base_asset_id
    async with client.post(
        f"{base_url}/api/image-sessions/{session_id}/generate",
        headers={"Cookie": f"session={cookie}"},
        json=request_body,
    ) as response:
        body = await response.read()
        accepted_at = time.time()
        if response.status != 202:
            raise RuntimeError(f"generate status={response.status} body={body[:200]!r}")
        payload = json.loads(body)
        tasks = payload.get("generation_tasks", [])
        if not tasks:
            raise RuntimeError("generate accepted without a task")
        matches = [item for item in tasks if str(item.get("prompt") or "") == prompt]
        if len(matches) != 1 or not matches[0].get("id"):
            raise RuntimeError(f"generate accepted with unique prompt matches={len(matches)}")
        return str(matches[0]["id"]), accepted_at, response.status, body.decode("utf-8")


def wait_for_terminal(probe: DBProbe, task_id: str, timeout: float) -> dict[str, Any]:
    deadline = time.monotonic() + timeout
    latest: dict[str, Any] = {}
    while time.monotonic() < deadline:
        latest = probe.task(task_id)
        if latest.get("status") in TERMINAL:
            return latest
        time.sleep(1)
    latest["timed_out"] = True
    return latest


async def wait_running(probe: DBProbe, task_id: str, timeout: float) -> dict[str, Any]:
    deadline = time.monotonic() + timeout
    latest: dict[str, Any] = {}
    while time.monotonic() < deadline:
        latest = await asyncio.to_thread(probe.task, task_id)
        if latest.get("status") == "running" or latest.get("started_at") is not None:
            return latest
        await asyncio.sleep(0.2)
    return latest


async def run(args: argparse.Namespace) -> dict[str, Any]:
    identity = verify_identity(Path(__file__).resolve().parents[2], Path(args.identity))
    manifest = json.loads(Path(args.manifest).read_text(encoding="utf-8"))
    if manifest.get("source_identity_sha256") != identity["source_identity_sha256"]:
        raise RuntimeError("fixture manifest source identity does not match the running tools")
    if Path(args.out).exists():
        raise RuntimeError(f"capacity output already exists: {args.out}")
    merchant = manifest["merchants"][0]
    db = DBProbe(args.db_url)
    timeout = aiohttp.ClientTimeout(total=30)
    pre_fault: list[dict[str, Any]] = []
    outage_started = None
    outage_recovered = None
    restart_results: dict[str, str] = {}
    precondition: dict[str, Any] = {}
    try:
        async with aiohttp.ClientSession(timeout=timeout, headers={"Origin": BROWSER_ORIGIN}) as client:
            cookie = await login(client, args.base_url, merchant)
            base_asset_id = latest_generated_asset(args.db_url, merchant["image_session_id"])

            # Fill all three provider slots with 15s work, wait until they are
            # in flight, then accept a fourth task before the fault begins.
            long_submissions = await asyncio.gather(
                *(
                    submit(client, args.base_url, cookie, merchant["image_session_id"], f"capacity-long-fault-slot-{index:03d}", base_asset_id)
                    for index in range(3)
                )
            )
            long_ids = [item[0] for item in long_submissions]
            running_before = await asyncio.gather(*(wait_running(db, task_id, 30) for task_id in long_ids))
            queued_id, queued_accepted_at, queued_status, queued_body = await submit(
                client,
                args.base_url,
                cookie,
                merchant["image_session_id"],
                "capacity-fault-redis-queued-before-outage",
                base_asset_id,
            )
            queued_before_fault = await asyncio.to_thread(db.task, queued_id)
            accepted_pre_fault = [
                {
                    "task_id": item[0],
                    "prompt": f"capacity-long-fault-slot-{index:03d}",
                    "accepted_at_epoch": item[1],
                    "status_at_fault": running_before[index].get("status"),
                    "row_at_fault": {key: str(value) for key, value in running_before[index].items()},
                }
                for index, item in enumerate(long_submissions)
            ]
            accepted_pre_fault.append(
                {
                    "task_id": queued_id,
                    "prompt": "capacity-fault-redis-queued-before-outage",
                    "accepted_at_epoch": queued_accepted_at,
                    "status_at_fault": queued_before_fault.get("status"),
                    "row_at_fault": {key: str(value) for key, value in queued_before_fault.items()},
                }
            )
            pre_fault = accepted_pre_fault
            precondition = {
                "long_task_count": len(long_ids),
                "long_task_statuses": [item.get("status") for item in running_before],
                "queued_task_status": queued_before_fault.get("status"),
                "long_tasks_running": len(long_ids) == 3 and all(item.get("status") == "running" for item in running_before),
                "queued_task_queued": queued_before_fault.get("status") == "queued",
            }
            precondition["pass"] = bool(precondition["long_tasks_running"] and precondition["queued_task_queued"])
            if not precondition["pass"]:
                result = {
                    "fixture": manifest["fixture_version"],
                    "commit": manifest["commit"],
                    "source_identity_sha256": identity["source_identity_sha256"],
                    "status": "unmeasurable",
                    "reason": "fault precondition did not observe three running long tasks and one queued task",
                    "precondition": precondition,
                    "redis_outage": {"pre_fault_accepted_tasks": pre_fault},
                    "restart": {
                        "performed_while_redis_down": False,
                        "performed_while_long_tasks_in_flight": bool(precondition["long_tasks_running"]),
                    },
                    "provider_attribution": {"status": "unmeasurable", "pass": False},
                    "pass": False,
                }
                out = Path(args.out)
                out.parent.mkdir(parents=True, exist_ok=True)
                out.write_text(json.dumps(result, indent=2, default=str) + "\n", encoding="utf-8")
                print(json.dumps(result, sort_keys=True, default=str))
                return result

            outage_started = time.time()
            docker("stop", "pf-capacity-0908-redis")
            # Restart both processes while the three long tasks are still in
            # flight and Redis is down; this keeps recovery evidence tied to
            # work accepted before the fault.
            restart_results["worker"] = docker("restart", "pf-capacity-0908-worker")
            restart_results["api"] = docker("restart", "pf-capacity-0908-api")
            await asyncio.sleep(args.redis_outage_seconds)
            docker("start", "pf-capacity-0908-redis")
            for _ in range(60):
                try:
                    if docker("exec", "pf-capacity-0908-redis", "redis-cli", "ping") == "PONG":
                        outage_recovered = time.time()
                        break
                except subprocess.CalledProcessError:
                    pass
                await asyncio.sleep(1)
            if outage_recovered is None:
                raise RuntimeError("dedicated Redis did not recover")

            recovered_rows = await asyncio.gather(*(asyncio.to_thread(wait_for_terminal, db, task_id, 240) for task_id in long_ids + [queued_id]))
            post_recovery_id, post_recovery_accepted_at, _, post_recovery_body = await submit(
                client,
                args.base_url,
                cookie,
                merchant["image_session_id"],
                "capacity-fault-post-recovery-validation",
                latest_generated_asset(args.db_url, merchant["image_session_id"]),
            )
            post_recovery_row = await asyncio.to_thread(wait_for_terminal, db, post_recovery_id, 180)
    finally:
        db.close()

    pre_fault_rows = [
        {"task_id": item["task_id"], "row": {key: str(value) for key, value in row.items()}}
        for item, row in zip(pre_fault, recovered_rows if "recovered_rows" in locals() else [{} for _ in pre_fault])
    ]
    pre_fault_ledger = [db_evidence for db_evidence in []]
    # Reopen briefly after the async client has closed so the persisted task
    # and ledger evidence is captured from the same post-fault database state.
    evidence_db = DBProbe(args.db_url)
    try:
        for item in pre_fault:
            item["post_fault_row"] = {key: str(value) for key, value in evidence_db.task(item["task_id"]).items()}
        pre_fault_ledger = [evidence_db.quota_evidence(item["task_id"]) for item in pre_fault]
        post_ledger = evidence_db.quota_evidence(post_recovery_id)
    finally:
        evidence_db.close()
    duplicate_events = [item for evidence in pre_fault_ledger + [post_ledger] for item in evidence["duplicates"]]
    accepted_recovered = all(item.get("post_fault_row", {}).get("status") in TERMINAL for item in pre_fault)
    accepted_succeeded = all(item.get("post_fault_row", {}).get("status") == "succeeded" for item in pre_fault)
    ledger_complete = all(evidence["semantic_complete"] for evidence in pre_fault_ledger + [post_ledger])
    post_recovery_status = str(post_recovery_row.get("status") or "")
    post_recovery_terminal = post_recovery_status in TERMINAL
    post_recovery_succeeded = post_recovery_status == "succeeded"
    expected_provider_prompts = [item["prompt"] for item in pre_fault] + ["capacity-fault-post-recovery-validation"]
    provider_snapshot_path = Path(args.out).with_name("fault-provider-events.jsonl")
    try:
        provider_attribution = provider_trace_report(args.provider_events, provider_snapshot_path, expected_provider_prompts)
    except (OSError, RuntimeError, ValueError, json.JSONDecodeError) as error:
        provider_attribution = {
            "status": "unmeasurable",
            "pass": False,
            "error": str(error),
            "expected_prompt_count": len(expected_provider_prompts),
        }
    effect_attribution = {
        "task_count": len(pre_fault) + 1,
        "provider_prompt_count": provider_attribution.get("expected_prompt_count", 0),
        "pre_fault_semantic_complete": all(evidence["semantic_complete"] for evidence in pre_fault_ledger),
        "post_recovery_semantic_complete": post_ledger["semantic_complete"],
        "post_recovery_terminal": post_recovery_terminal,
        "post_recovery_succeeded": post_recovery_succeeded,
        "provider_trace_pass": provider_attribution.get("pass", False),
    }
    output = {
        "fixture": manifest["fixture_version"],
        "commit": manifest["commit"],
        "source_identity_sha256": identity["source_identity_sha256"],
        "precondition": precondition,
        "redis_outage": {
            "container": "pf-capacity-0908-redis",
            "stopped_at_epoch": outage_started,
            "recovered_at_epoch": outage_recovered,
            "actual_seconds": (outage_recovered - outage_started) if outage_recovered and outage_started else None,
            "requested_seconds": args.redis_outage_seconds,
            "pre_fault_accepted_tasks": pre_fault,
            "post_fault_rows": pre_fault_rows,
            "tasks": recovered_rows if "recovered_rows" in locals() else [],
        },
        "restart": {
            "worker": "pf-capacity-0908-worker",
            "api": "pf-capacity-0908-api",
            "performed_while_redis_down": True,
            "performed_while_long_tasks_in_flight": bool(precondition.get("long_tasks_running")),
            "commands": restart_results,
        },
        "post_recovery_validation": {
            "task_id": post_recovery_id,
            "accepted_at_epoch": post_recovery_accepted_at,
            "accepted_body_bytes": len(post_recovery_body),
            "row": {key: str(value) for key, value in post_recovery_row.items()},
        },
        "quota_ledger": {
            "pre_fault": pre_fault_ledger,
            "post_recovery": post_ledger,
            "duplicate_events": duplicate_events,
            "semantic_complete": ledger_complete,
        },
        "provider_attribution": provider_attribution,
        "effect_attribution": effect_attribution,
        "redis_flushall_used": False,
        "accepted_tasks_recovered": accepted_recovered,
        "accepted_tasks_all_succeeded": accepted_succeeded,
        "post_recovery_terminal": post_recovery_terminal,
        "post_recovery_succeeded": post_recovery_succeeded,
        "pass": precondition.get("pass", False)
        and accepted_recovered
        and accepted_succeeded
        and ledger_complete
        and post_recovery_terminal
        and post_recovery_succeeded
        and provider_attribution.get("pass", False)
        and not duplicate_events,
    }
    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(output, indent=2, default=str) + "\n", encoding="utf-8")
    print(json.dumps(output, sort_keys=True, default=str))
    return output


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-url", default="http://127.0.0.1:30182")
    parser.add_argument("--db-url", required=True)
    parser.add_argument("--manifest", required=True)
    parser.add_argument("--identity", required=True)
    parser.add_argument("--provider-events", required=True)
    parser.add_argument("--redis-outage-seconds", type=int, default=30)
    parser.add_argument("--out", required=True)
    return parser.parse_args()


if __name__ == "__main__":
    result = asyncio.run(run(parse_args()))
    raise SystemExit(0 if result.get("pass") else 1)
