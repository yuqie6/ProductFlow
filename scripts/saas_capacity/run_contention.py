#!/usr/bin/env python3
"""Measure generation-slot contention and preserve accept/dispatch/provider timestamps."""

import argparse
import asyncio
import json
import statistics
import time
from decimal import Decimal
from pathlib import Path
from typing import Any

import aiohttp
import psycopg

from capacity_events import merge_events, snapshot_events, validate_event
from capacity_identity import verify_identity


TERMINAL = {"succeeded", "failed", "unknown", "cancelled"}
BROWSER_ORIGIN = "http://127.0.0.1:30182"


class DBProbe:
    def __init__(self, db_url: str):
        self.connection = psycopg.connect(db_url, autocommit=True)

    def rows(self, task_ids: list[str]) -> dict[str, dict[str, Any]]:
        if not task_ids:
            return {}
        with self.connection.cursor() as cursor:
            cursor.execute(
                """
                SELECT t.id, t.status, t.prompt, t.created_at, t.started_at, t.finished_at,
                       t.session_id, s.merchant_id,
                       COALESCE(t.progress_updated_at, t.finished_at, t.started_at, s.updated_at, t.created_at) AS updated_at,
                       d.sent_at, d.attempts AS dispatch_attempts
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

    def tasks_by_session_prompts(self, pairs: list[tuple[str, str]]) -> dict[tuple[str, str], list[str]]:
        """Find tasks after an accepted response omitted them from its projection.

        The generate response is a bounded session snapshot, so a 202 does not
        guarantee that the newly-created task is present in that response. The
        pair is constrained by the authenticated session and exact prompt; a
        missing or ambiguous pair stays unresolved instead of being guessed.
        """
        if not pairs:
            return {}
        values = ", ".join(["(%s, %s)"] * len(pairs))
        parameters = [value for pair in pairs for value in pair]
        with self.connection.cursor() as cursor:
            cursor.execute(
                f"""
                SELECT t.id, t.session_id, t.prompt
                FROM image_session_generation_tasks t
                WHERE (t.session_id, t.prompt) IN (VALUES {values})
                ORDER BY t.created_at DESC, t.id DESC
                """,
                parameters,
            )
            result: dict[tuple[str, str], list[str]] = {}
            for task_id, session_id, prompt in cursor.fetchall():
                result.setdefault((str(session_id), str(prompt)), []).append(str(task_id))
            return result

    def quota_summary(self, task_ids: list[str]) -> list[dict[str, Any]]:
        """Return exact event, hold, and final-account evidence for each task."""
        if not task_ids:
            return []
        with self.connection.cursor() as cursor:
            cursor.execute(
                """
                SELECT t.id, t.status, s.merchant_id,
                       qa.available_units AS account_available_units,
                       qa.reserved_units AS account_reserved_units,
                       (
                           SELECT COALESCE(json_agg(json_build_object(
                               'event_type', e.event_type,
                               'amount_units', e.amount_units,
                               'available_after', e.available_after,
                               'reserved_after', e.reserved_after,
                               'hold_id', e.hold_id,
                               'idempotency_key', e.idempotency_key
                           ) ORDER BY e.created_at, e.id), '[]'::json)
                           FROM merchant_quota_events e
                           WHERE e.merchant_id = s.merchant_id
                             AND e.idempotency_key LIKE '%%' || t.id || '%%'
                       ) AS quota_events,
                       (
	                           SELECT COALESCE(json_agg(json_build_object(
	                               'id', h.id,
	                               'amount_units', h.amount_units,
                               'status', h.status,
                               'settled_units', h.settled_units,
                               'idempotency_key', h.idempotency_key
                           ) ORDER BY h.created_at, h.id), '[]'::json)
                           FROM merchant_quota_holds h
                           WHERE h.merchant_id = s.merchant_id
                             AND h.idempotency_key LIKE '%%' || t.id || '%%'
                       ) AS quota_holds
                FROM image_session_generation_tasks t
                JOIN image_sessions s ON s.id = t.session_id
                LEFT JOIN merchant_quota_accounts qa ON qa.merchant_id = s.merchant_id
                WHERE t.id = ANY(%s)
                ORDER BY t.created_at, t.id
                """,
                (task_ids,),
            )
            columns = [item.name for item in cursor.description]
            return [dict(zip(columns, row)) for row in cursor.fetchall()]

    def quota_account_summary(self, merchant_ids: list[str]) -> list[dict[str, Any]]:
        if not merchant_ids:
            return []
        with self.connection.cursor() as cursor:
            cursor.execute(
                """
                SELECT a.merchant_id, a.available_units, a.reserved_units,
                       (
                           SELECT count(*)
                           FROM merchant_quota_events e
                           WHERE e.merchant_id = a.merchant_id AND e.event_type = 'adjust'
                       ) AS adjust_event_count,
                       (
                           SELECT COALESCE(sum(e.amount_units), 0)
                           FROM merchant_quota_events e
                           WHERE e.merchant_id = a.merchant_id AND e.event_type = 'adjust'
                       ) AS adjust_units,
                       (
                           SELECT COALESCE(sum(COALESCE(h.settled_units, 0)), 0)
                           FROM merchant_quota_holds h
                           WHERE h.merchant_id = a.merchant_id AND h.status = 'settled'
                       ) AS consumed_units,
                       (
                           SELECT COALESCE(sum(h.amount_units), 0)
                           FROM merchant_quota_holds h
                           WHERE h.merchant_id = a.merchant_id
                             AND h.status IN ('reserved', 'pending_reconciliation')
                       ) AS outstanding_units
                FROM merchant_quota_accounts a
                WHERE a.merchant_id = ANY(%s)
                ORDER BY a.merchant_id
                """,
                (merchant_ids,),
            )
            columns = [item.name for item in cursor.description]
            return [dict(zip(columns, row)) for row in cursor.fetchall()]

    def duplicate_events(self, task_ids: list[str]) -> list[dict[str, Any]]:
        if not task_ids:
            return []
        with self.connection.cursor() as cursor:
            cursor.execute(
                """
                SELECT t.id, e.event_type, e.idempotency_key, count(*) AS copies
                FROM image_session_generation_tasks t
                JOIN merchant_quota_events e
                  ON e.idempotency_key LIKE '%%' || t.id || '%%'
                WHERE t.id = ANY(%s)
                GROUP BY t.id, e.event_type, e.idempotency_key
                HAVING count(*) > 1
                ORDER BY t.id, e.event_type
                """,
                (task_ids,),
            )
            columns = [item.name for item in cursor.description]
            return [dict(zip(columns, row)) for row in cursor.fetchall()]

    def close(self) -> None:
        self.connection.close()


def provider_events(path: str) -> dict[str, dict[str, Any]]:
    events = merge_events(path)
    for event in events.values():
        validate_event(event)
    return events


def submission_task_id(item: dict[str, Any]) -> str | None:
    """Return the DB-bound task id while preserving the original API id field."""
    task_id = item.get("reconciled_task_id") or item.get("task_id")
    return str(task_id) if task_id else None


def reconcile_submissions(
    db_probe: DBProbe,
    submissions: list[dict[str, Any]],
    merchants: list[dict[str, Any]],
) -> dict[str, int]:
    """Bind accepted requests whose bounded response snapshot omitted the task.

    The API response is evidence in its own right. We keep its original
    ``task_id`` (or lack of one), then use the authenticated session and exact
    prompt to associate the already-created DB row. No request is repeated.
    """
    accepted = [item for item in submissions if int(item.get("status") or 0) == 202]
    pairs = [
        (
            str(merchants[int(item["merchant_index"])]["image_session_id"]),
            str(item["prompt"]),
        )
        for item in accepted
    ]
    candidates = db_probe.tasks_by_session_prompts(pairs)
    counts = {
        "accepted": 0,
        "response_snapshot_task_ids": 0,
        "reconciled": 0,
        "unresolved": 0,
        "ambiguous": 0,
    }
    for item in submissions:
        response_task_id = item.get("task_id")
        item["api_response_task_id"] = response_task_id
        item["api_response_task_present"] = bool(response_task_id)
        if int(item.get("status") or 0) != 202:
            item["reconciled_task_id"] = None
            item["reconciliation"] = "not_accepted"
            continue
        counts["accepted"] += 1
        if response_task_id:
            counts["response_snapshot_task_ids"] += 1
        session_id = str(merchants[int(item["merchant_index"])]["image_session_id"])
        prompt = str(item["prompt"])
        pair_candidates = candidates.get((session_id, prompt), [])
        if response_task_id:
            if str(response_task_id) in pair_candidates:
                item["reconciled_task_id"] = str(response_task_id)
                item["reconciliation"] = "response_snapshot"
                counts["reconciled"] += 1
            else:
                item["reconciled_task_id"] = None
                item["reconciliation"] = "response_task_not_in_session_prompt_pair"
                counts["unresolved"] += 1
        elif len(pair_candidates) == 1:
            item["reconciled_task_id"] = pair_candidates[0]
            item["reconciliation"] = "db_session_prompt"
            counts["reconciled"] += 1
        elif not pair_candidates:
            item["reconciled_task_id"] = None
            item["reconciliation"] = "accepted_db_task_missing"
            counts["unresolved"] += 1
        else:
            item["reconciled_task_id"] = None
            item["reconciliation"] = "accepted_db_task_ambiguous"
            counts["ambiguous"] += 1
            counts["unresolved"] += 1
    return counts


def task_ids_for_label(submissions: list[dict[str, Any]], label: str) -> list[str]:
    return [
        task_id
        for item in submissions
        if item.get("label") == label
        for task_id in [submission_task_id(item)]
        if task_id
    ]


def json_rows(value: Any) -> list[dict[str, Any]]:
    if isinstance(value, str):
        try:
            value = json.loads(value)
        except json.JSONDecodeError:
            return []
    return [item for item in (value or []) if isinstance(item, dict)]


def quota_report(
    task_ids: list[str],
    rows: list[dict[str, Any]],
	account_rows: list[dict[str, Any]] | None = None,
	opening_balance_units: int | None = None,
	expected_adjust_units_per_merchant: int | None = None,
	expected_quota_units: int = 1,
	expected_merchant_ids: list[str] | None = None,
) -> dict[str, Any]:
    by_id = {str(row["id"]): row for row in rows}
    incomplete: list[str] = []
    for task_id in task_ids:
        row = by_id.get(str(task_id))
        if row is None:
            incomplete.append(str(task_id))
            continue
        status = str(row.get("status") or "")
        expected_terminal = {
            "succeeded": {"settle"},
            "failed": {"settle", "release", "mark_unknown"},
            "cancelled": {"release", "mark_unknown"},
            "unknown": {"mark_unknown"},
        }.get(status, set())
        events = json_rows(row.get("quota_events"))
        holds = json_rows(row.get("quota_holds"))
        reserve_events = [item for item in events if item.get("event_type") == "reserve"]
        terminal_events = [item for item in events if item.get("event_type") in expected_terminal]
        hold = holds[0] if len(holds) == 1 else {}
        terminal_type = str(terminal_events[0].get("event_type")) if len(terminal_events) == 1 else ""
        expected_hold_status = {
            "succeeded": "settled",
            "failed": "settled" if any(item.get("event_type") == "settle" for item in terminal_events) else "released",
            "cancelled": "pending_reconciliation" if terminal_type == "mark_unknown" else "released",
            "unknown": "pending_reconciliation",
        }.get(status)
        settled_units = hold.get("settled_units")
        hold_amount = hold.get("amount_units")
        terminal_amount = terminal_events[0].get("amount_units") if len(terminal_events) == 1 else None
        hold_id = hold.get("id")
        snapshots_are_numeric = all(
            isinstance(item.get(key), (int, float))
            for item in events
            for key in ("available_after", "reserved_after", "amount_units")
        )
        lifecycle_valid = (
            status in TERMINAL
            and len(reserve_events) == 1
            and len(terminal_events) == 1
            and len(holds) == 1
            and reserve_events[0].get("amount_units") == expected_quota_units
            and hold_amount == expected_quota_units
            and terminal_amount == expected_quota_units
            and hold_id
            and all(item.get("hold_id") == hold_id for item in events)
            and hold.get("status") == expected_hold_status
            and snapshots_are_numeric
            and (
                (terminal_type == "settle" and settled_units == expected_quota_units)
                or (terminal_type != "settle" and settled_units is None)
            )
        )
        if not lifecycle_valid:
            incomplete.append(str(task_id))
    account_reconciliation: dict[str, Any] = {
        "status": "unmeasurable",
        "pass": False,
        "rows": account_rows or [],
    }
    account_evidence_requested = any(
        value is not None
        for value in (account_rows, opening_balance_units, expected_adjust_units_per_merchant, expected_merchant_ids)
    )
    if account_rows is not None and opening_balance_units is not None and expected_adjust_units_per_merchant is not None:
        account_incomplete: list[str] = []
        observed_merchant_ids = [str(account.get("merchant_id") or "") for account in account_rows]
        if expected_merchant_ids is not None:
            expected_ids = [str(item) for item in expected_merchant_ids]
            account_incomplete.extend(sorted(set(expected_ids) - set(observed_merchant_ids)))
            account_incomplete.extend(sorted(set(observed_merchant_ids) - set(expected_ids)))
            if len(observed_merchant_ids) != len(set(observed_merchant_ids)):
                account_incomplete.append("duplicate merchant account rows")
        for account in account_rows:
            merchant_id = str(account.get("merchant_id") or "")
            available = account.get("available_units")
            reserved = account.get("reserved_units")
            adjust_count = account.get("adjust_event_count")
            adjust_units = account.get("adjust_units")
            consumed = account.get("consumed_units")
            outstanding = account.get("outstanding_units")
            expected_available = opening_balance_units + int(adjust_units or 0) - int(consumed or 0) - int(outstanding or 0)
            arithmetic_pass = (
                # PostgreSQL SUM(bigint) is numeric, decoded by psycopg as
                # Decimal. Keep the exact aggregate instead of using float.
                all(
                    isinstance(value, (int, float, Decimal)) and value == int(value)
                    for value in (available, reserved, adjust_count, adjust_units, consumed, outstanding)
                )
                and int(adjust_count) == expected_adjust_units_per_merchant
                and int(adjust_units) == expected_adjust_units_per_merchant
                and int(available) == expected_available
                and int(reserved) == int(outstanding)
                and int(available) + int(reserved) + int(consumed) == opening_balance_units + int(adjust_units)
            )
            if not arithmetic_pass:
                account_incomplete.append(merchant_id)
        account_reconciliation = {
            "status": "measurable",
            "pass": len(account_rows) > 0 and expected_merchant_ids is not None and not account_incomplete,
            "incomplete_merchant_ids": account_incomplete,
            "opening_balance_units": opening_balance_units,
            "expected_adjust_units_per_merchant": expected_adjust_units_per_merchant,
            "rows": account_rows,
        }
    return {
        "expected_task_count": len(task_ids),
        "observed_task_count": len(rows),
        "semantic_complete_task_count": len(task_ids) - len(incomplete),
        "incomplete_task_ids": incomplete,
        "pass": len(rows) == len(task_ids) and not incomplete and (account_reconciliation["pass"] if account_evidence_requested else True),
        "lifecycle_complete": len(rows) == len(task_ids) and not incomplete,
        "account_reconciliation": account_reconciliation,
        "rows": rows,
    }


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
            raise RuntimeError("login response did not contain session cookie")
        return cookie.value


async def submit(
    client: aiohttp.ClientSession,
    base_url: str,
    cookie: str,
    session_id: str,
    prompt: str,
    base_asset_id: str | None = None,
) -> tuple[str | None, float | None, int, str]:
    request_body: dict[str, Any] = {"prompt": prompt, "size": "64x64", "generation_count": 1}
    if base_asset_id:
        request_body["base_asset_id"] = base_asset_id
    try:
        async with client.post(
            f"{base_url}/api/image-sessions/{session_id}/generate",
            headers={"Cookie": f"session={cookie}"},
            json=request_body,
        ) as response:
            body = await response.read()
            if response.status != 202:
                return None, None, response.status, body[:200].decode("utf-8", errors="replace")
            accepted_at = time.time()
            payload = json.loads(body)
            tasks = payload.get("generation_tasks", [])
            if not tasks:
                return None, accepted_at, response.status, "accepted response had no task"
            matches = [item for item in tasks if str(item.get("prompt") or "") == prompt]
            if len(matches) != 1 or not matches[0].get("id"):
                return None, accepted_at, response.status, f"accepted response had unique prompt matches={len(matches)}"
            return str(matches[0]["id"]), accepted_at, response.status, ""
    except Exception as exc:  # noqa: BLE001 - preserve the failure in the report.
        return None, None, 0, repr(exc)


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


async def wait_terminal(client: aiohttp.ClientSession, base_url: str, cookie: str, session_id: str, task_ids: list[str], timeout: float, db_probe: DBProbe) -> dict[str, dict[str, Any]]:
    end = time.monotonic() + timeout
    remaining = set(task_ids)
    latest: dict[str, dict[str, Any]] = {}
    while remaining and time.monotonic() < end:
        async with client.get(f"{base_url}/api/image-sessions/{session_id}/status", headers={"Cookie": f"session={cookie}"}) as response:
            if response.status == 200:
                payload = await response.json()
                for task in payload.get("generation_tasks", []):
                    task_id = task.get("id")
                    if task_id in remaining:
                        latest[task_id] = task
                        if task.get("status") in TERMINAL:
                            remaining.remove(task_id)
        rows = db_probe.rows(list(remaining))
        for task_id, row in rows.items():
            if row.get("status") in TERMINAL:
                latest[task_id] = row
                remaining.remove(task_id)
        if remaining:
            await asyncio.sleep(0.5)
    return latest


def provider_event_for_task(row: dict[str, Any], events: dict[str, dict[str, Any]]) -> tuple[dict[str, Any], int]:
    task_prompt = str(row.get("prompt") or "").strip()
    marker = f"Current user request:\n{task_prompt}\n"
    matches = [
        event
        for event in events.values()
        if task_prompt and marker in str(event.get("prompt") or "")
    ]
    return (matches[0] if len(matches) == 1 else {}), len(matches)


def phase_metrics(
    task_ids: list[str],
    rows: dict[str, dict[str, Any]],
    events: dict[str, dict[str, Any]],
    submissions: dict[str, dict[str, Any]],
) -> list[dict[str, Any]]:
    result = []
    for task_id in task_ids:
        row = rows.get(task_id, {})
        event, event_matches = provider_event_for_task(row, events)
        submission = submissions.get(task_id, {})
        accepted_at = submission.get("accepted_at")
        sent = row.get("sent_at")
        started = event.get("started_epoch")
        finished_provider = event.get("finished_epoch")
        finished_db = row.get("finished_at")
        result.append(
            {
                "task_id": task_id,
                "api_response_task_id": submission.get("api_response_task_id", submission.get("task_id")),
                "reconciliation": submission.get("reconciliation"),
                "status": row.get("status"),
                "prompt": row.get("prompt"),
                "accepted_at_epoch": accepted_at,
                "accepted_at_observation": "submit_response" if accepted_at is not None else "missing_in_original_report",
                "created_at": row.get("created_at").isoformat() if row.get("created_at") else None,
                "last_sent_at": sent.isoformat() if sent else None,
                "dispatch_attempts": row.get("dispatch_attempts"),
                "first_dispatch_observation": "unmeasured: sent_at is overwritten on redispatch",
                "started_at": row.get("started_at").isoformat() if row.get("started_at") else None,
                "finished_at": finished_db.isoformat() if finished_db else None,
                "provider_request_id": event.get("request_id"),
                "accept_to_last_dispatch_ms": ((sent.timestamp() - accepted_at) * 1000) if sent and accepted_at else None,
                "last_dispatch_to_provider_ms": ((started - sent.timestamp()) * 1000) if started and sent else None,
                "provider_to_persist_ms": ((finished_db.timestamp() - finished_provider) * 1000) if finished_db and finished_provider else None,
                "accept_to_provider_ms": ((started - accepted_at) * 1000) if started and accepted_at else None,
                "provider_event_matches": event_matches,
            }
        )
    return result


def summarize_wait(items: list[dict[str, Any]], expected_count: int) -> dict[str, Any]:
    waits = [item["accept_to_provider_ms"] for item in items if item.get("accept_to_provider_ms") is not None]
    terminal_count = sum(item.get("status") in TERMINAL for item in items)
    unique_provider_count = sum(item.get("provider_event_matches") == 1 for item in items)
    return {
        "count": len(items),
        "expected_count": expected_count,
        "terminal_count": terminal_count,
        "provider_started": len(waits),
        "unique_provider_events": unique_provider_count,
        "missing_observations": expected_count - len(waits),
        "p50_ms": statistics.quantiles(waits, n=2, method="inclusive")[0] if len(waits) > 1 else (waits[0] if waits else None),
        "p95_ms": sorted(waits)[min(len(waits) - 1, int(round((len(waits) - 1) * 0.95)))] if waits else None,
        "max_ms": max(waits, default=None),
    }


def contention_pass(output: dict[str, Any]) -> bool:
    short = output["short"]
    return bool(
        output["quota_coverage"]["pass"]
        and not output["duplicate_quota_events"]
        and output["submission_reconciliation"]["unresolved"] == 0
        and short["a_accepted_by_http"] == short["a_reconciled_task_ids"] == 100
        and short["a_terminal_count"] == short["a_throughput_succeeded"] == 100
        and short["b_accepted_by_http"] == short["b_reconciled_task_ids"] == 20
        and short["target_b_p95_pass"]
        and output["provider_attribution_pass"]
    )


def repair_existing(args: argparse.Namespace) -> dict[str, Any]:
    """Repair an earlier report without issuing another generation request."""
    identity = verify_identity(Path(__file__).resolve().parents[2], Path(args.identity))
    manifest = json.loads(Path(args.manifest).read_text(encoding="utf-8"))
    if manifest.get("source_identity_sha256") != identity["source_identity_sha256"]:
        raise RuntimeError("fixture manifest source identity does not match the running tools")
    source_path = Path(args.repair_input)
    if source_path.resolve() == Path(args.out).resolve():
        raise RuntimeError("repair output must be separate from the historical input")
    if Path(args.out).exists():
        raise RuntimeError(f"capacity output already exists: {args.out}")
    output = json.loads(source_path.read_text(encoding="utf-8"))
    submissions = output.get("submissions", [])
    db_probe = DBProbe(args.db_url)
    reconciliation = reconcile_submissions(db_probe, submissions, manifest["merchants"])
    a_tasks = task_ids_for_label(submissions, "A")
    b_tasks = task_ids_for_label(submissions, "B")
    long_tasks = task_ids_for_label(submissions, "LONG-A") + task_ids_for_label(submissions, "LONG-B")
    short_ids = a_tasks + b_tasks
    all_task_ids = short_ids + long_tasks
    rows = db_probe.rows(all_task_ids)
    event_snapshot_path = Path(args.out).with_name("contention-provider-events.jsonl")
    event_snapshot = snapshot_events(
        args.provider_events,
        event_snapshot_path,
        ("capacity-A-short-", "capacity-B-short-", "capacity-long-"),
    )
    events = provider_events(str(event_snapshot_path))
    submission_by_task = {
        str(submission_task_id(item)): item
        for item in submissions
        if submission_task_id(item)
    }
    short_items = phase_metrics(short_ids, rows, events, submission_by_task)
    a_items = [item for item in short_items if str(item.get("prompt") or "").startswith("capacity-A-short")]
    b_items = [item for item in short_items if str(item.get("prompt") or "").startswith("capacity-B-short")]
    long_items = phase_metrics(long_tasks, rows, events, submission_by_task)
    duplicates = db_probe.duplicate_events(all_task_ids)
    quota = quota_report(
        all_task_ids,
        db_probe.quota_summary(all_task_ids),
        db_probe.quota_account_summary([item["merchant_id"] for item in manifest["merchants"]]),
        int(manifest["opening_balance_units"]),
        int(manifest["quota_adjust_units_per_merchant"]),
        expected_merchant_ids=[item["merchant_id"] for item in manifest["merchants"]],
    )
    db_probe.close()

    a_accepted = sum(int(item.get("status") or 0) == 202 for item in submissions if item.get("label") == "A")
    b_accepted = sum(int(item.get("status") or 0) == 202 for item in submissions if item.get("label") == "B")
    a_response_snapshot = sum(bool(item.get("api_response_task_id")) for item in submissions if item.get("label") == "A")
    b_response_snapshot = sum(bool(item.get("api_response_task_id")) for item in submissions if item.get("label") == "B")
    old_short = output.get("short", {})
    output["short"] = {
        "a_attempted": 100,
        "a_submitted": a_accepted,
        "a_accepted_by_http": a_accepted,
        "a_response_snapshot_task_ids": a_response_snapshot,
        "a_reconciled_task_ids": len(a_tasks),
        "a_unresolved_task_ids": a_accepted - len(a_tasks),
        "b_attempted": 20,
        "b_submitted": b_accepted,
        "b_accepted_by_http": b_accepted,
        "b_response_snapshot_task_ids": b_response_snapshot,
        "b_reconciled_task_ids": len(b_tasks),
        "b_unresolved_task_ids": b_accepted - len(b_tasks),
        "b_submit_interval_seconds": old_short.get("b_submit_interval_seconds", 5),
        "elapsed_seconds": old_short.get("elapsed_seconds"),
        "b_wait": summarize_wait(b_items, 20),
        "a_throughput_succeeded": sum(item.get("status") == "succeeded" for item in a_items),
        "a_terminal_count": sum(item.get("status") in TERMINAL for item in a_items),
        "a_provider_observed_once": sum(item.get("provider_event_matches") == 1 for item in a_items),
        "a_phase_metrics": a_items,
        "b_phase_metrics": b_items,
        "target_b_p95_wait_ms": 10000,
        "all_b_terminal": len(b_tasks) == 20 and len(b_items) == 20 and all(item.get("status") in TERMINAL for item in b_items),
        "all_b_provider_observed_once": len(b_tasks) == 20 and len(b_items) == 20 and all(item.get("provider_event_matches") == 1 for item in b_items),
        "target_b_p95_pass": (
            len(b_tasks) == 20
            and all(item.get("status") in TERMINAL for item in b_items)
            and all(item.get("provider_event_matches") == 1 for item in b_items)
            and summarize_wait(b_items, 20)["provider_started"] == 20
            and summarize_wait(b_items, 20)["p95_ms"] is not None
            and summarize_wait(b_items, 20)["p95_ms"] <= 10000
        ),
    }
    output["long_non_preemptive"] = {
        "tasks": long_items,
        "interpretation": "The 15s three-slot case is reported separately; the short-job <=10s target is not applied to this non-preemptive scenario.",
    }
    output["duplicate_quota_events"] = duplicates
    output["quota_duplicate_check_pass"] = not duplicates
    output["quota_coverage"] = quota
    output["submission_reconciliation"] = reconciliation
    output["repair"] = {
        "source": str(source_path),
        "method": "read-only DB binding by authenticated session and exact prompt",
        "requests_reissued": 0,
        "api_response_snapshot_missing_tasks_preserved": True,
    }
    output["source_identity_sha256"] = identity["source_identity_sha256"]
    output["provider_event_snapshot"] = event_snapshot
    output["provider_attribution_pass"] = bool(short_items + long_items) and all(
        item.get("provider_event_matches") == 1 for item in short_items + long_items
    )
    output["pass"] = contention_pass(output)
    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(output, indent=2, default=str) + "\n", encoding="utf-8")
    print(json.dumps({
        "out": str(out),
        "a_accepted": a_accepted,
        "a_response_snapshot_task_ids": a_response_snapshot,
        "a_reconciled_task_ids": len(a_tasks),
        "b_p95_ms": output["short"]["b_wait"]["p95_ms"],
        "quota_pass": quota["pass"],
    }, sort_keys=True))
    return output


async def run(args: argparse.Namespace) -> dict[str, Any]:
    identity = verify_identity(Path(__file__).resolve().parents[2], Path(args.identity))
    manifest = json.loads(Path(args.manifest).read_text(encoding="utf-8"))
    if manifest.get("source_identity_sha256") != identity["source_identity_sha256"]:
        raise RuntimeError("fixture manifest source identity does not match the running tools")
    if Path(args.out).exists():
        raise RuntimeError(f"capacity output already exists: {args.out}")
    merchants = manifest["merchants"]
    db_probe = DBProbe(args.db_url)
    timeout = aiohttp.ClientTimeout(total=30)
    connector = aiohttp.TCPConnector(limit=0, limit_per_host=0, enable_cleanup_closed=True)
    async with aiohttp.ClientSession(timeout=timeout, connector=connector, headers={"Origin": BROWSER_ORIGIN}) as client:
        cookies = [await login(client, args.base_url, merchant) for merchant in merchants]
        base_assets = latest_generated_assets(args.db_url, [merchant["image_session_id"] for merchant in merchants])
        if any(merchant["image_session_id"] not in base_assets for merchant in merchants[:2]):
            raise RuntimeError("contention requires an existing generated base image for both A and B; prepare these outside the measured workload")
        submissions: list[dict[str, Any]] = []

        async def send(label: str, index: int, merchant_index: int, long: bool = False) -> None:
            prompt = f"capacity-{label}-{'long' if long else 'short'}-{index:03d}"
            task_id, accepted_at, status, error = await submit(
                client,
                args.base_url,
                cookies[merchant_index],
                merchants[merchant_index]["image_session_id"],
                prompt,
                base_assets.get(merchants[merchant_index]["image_session_id"]),
            )
            submissions.append({"label": label, "merchant_index": merchant_index, "index": index, "prompt": prompt, "task_id": task_id, "accepted_at": accepted_at, "status": status, "error": error})

        start_short = time.monotonic()
        await asyncio.gather(*(send("A", index, 0) for index in range(100)))
        for index in range(20):
            await send("B", index, 1)
            if index < 19:
                await asyncio.sleep(5)
        reconciliation = reconcile_submissions(db_probe, submissions, merchants)
        a_tasks = task_ids_for_label(submissions, "A")
        b_tasks = task_ids_for_label(submissions, "B")
        short_ids = a_tasks + b_tasks
        await asyncio.gather(
            wait_terminal(client, args.base_url, cookies[0], merchants[0]["image_session_id"], a_tasks, 300, db_probe),
            wait_terminal(client, args.base_url, cookies[1], merchants[1]["image_session_id"], b_tasks, 300, db_probe),
        )
        short_elapsed = time.monotonic() - start_short

        for index in range(3):
            prompt = f"capacity-long-long-{index:03d}"
            task_id, accepted_at, status, error = await submit(
                client,
                args.base_url,
                cookies[0],
                merchants[0]["image_session_id"],
                prompt,
                base_assets.get(merchants[0]["image_session_id"]),
            )
            submissions.append({"label": "LONG-A", "merchant_index": 0, "index": index, "prompt": prompt, "task_id": task_id, "accepted_at": accepted_at, "status": status, "error": error})
        await asyncio.sleep(0.5)
        long_b_id, long_b_accepted, status, error = await submit(
            client,
            args.base_url,
            cookies[1],
            merchants[1]["image_session_id"],
            "capacity-long-B-000",
            base_assets.get(merchants[1]["image_session_id"]),
        )
        submissions.append({"label": "LONG-B", "merchant_index": 1, "index": 0, "prompt": "capacity-long-B-000", "task_id": long_b_id, "accepted_at": long_b_accepted, "status": status, "error": error})
        reconciliation = reconcile_submissions(db_probe, submissions, merchants)
        long_tasks = task_ids_for_label(submissions, "LONG-A") + task_ids_for_label(submissions, "LONG-B")
        await asyncio.gather(
            wait_terminal(client, args.base_url, cookies[0], merchants[0]["image_session_id"], task_ids_for_label(submissions, "LONG-A"), 180, db_probe),
            wait_terminal(client, args.base_url, cookies[1], merchants[1]["image_session_id"], task_ids_for_label(submissions, "LONG-B"), 180, db_probe),
        )

    rows = db_probe.rows(short_ids + long_tasks)
    event_snapshot_path = Path(args.out).with_name("contention-provider-events.jsonl")
    event_snapshot = snapshot_events(
        args.provider_events,
        event_snapshot_path,
        ("capacity-A-short-", "capacity-B-short-", "capacity-long-"),
    )
    short_events = provider_events(str(event_snapshot_path))
    submission_by_task = {
        str(submission_task_id(item)): item
        for item in submissions
        if submission_task_id(item)
    }
    short_items = phase_metrics(short_ids, rows, short_events, submission_by_task)
    b_items = [item for item in short_items if str(item.get("prompt") or "").startswith("capacity-B-short")]
    a_items = [item for item in short_items if str(item.get("prompt") or "").startswith("capacity-A-short")]
    long_items = phase_metrics(long_tasks, rows, short_events, submission_by_task)
    duplicates = db_probe.duplicate_events(short_ids + long_tasks)
    quota_rows = db_probe.quota_summary(short_ids + long_tasks)
    quota = quota_report(
        short_ids + long_tasks,
        quota_rows,
        db_probe.quota_account_summary([item["merchant_id"] for item in manifest["merchants"]]),
        int(manifest["opening_balance_units"]),
        int(manifest["quota_adjust_units_per_merchant"]),
        expected_merchant_ids=[item["merchant_id"] for item in manifest["merchants"]],
    )
    db_probe.close()
    a_accepted = sum(int(item.get("status") or 0) == 202 for item in submissions if item.get("label") == "A")
    b_accepted = sum(int(item.get("status") or 0) == 202 for item in submissions if item.get("label") == "B")
    a_response_snapshot = sum(bool(item.get("api_response_task_id")) for item in submissions if item.get("label") == "A")
    b_response_snapshot = sum(bool(item.get("api_response_task_id")) for item in submissions if item.get("label") == "B")
    output = {
        "fixture": manifest["fixture_version"],
        "commit": manifest["commit"],
        "source_identity_sha256": identity["source_identity_sha256"],
        "short": {
            "a_attempted": 100,
            "a_submitted": a_accepted,
            "a_accepted_by_http": a_accepted,
            "a_response_snapshot_task_ids": a_response_snapshot,
            "a_reconciled_task_ids": len(a_tasks),
            "a_unresolved_task_ids": a_accepted - len(a_tasks),
            "b_attempted": 20,
            "b_submitted": b_accepted,
            "b_accepted_by_http": b_accepted,
            "b_response_snapshot_task_ids": b_response_snapshot,
            "b_reconciled_task_ids": len(b_tasks),
            "b_unresolved_task_ids": b_accepted - len(b_tasks),
            "b_submit_interval_seconds": 5,
            "elapsed_seconds": short_elapsed,
            "b_wait": summarize_wait(b_items, 20),
            "a_throughput_succeeded": sum(item.get("status") == "succeeded" for item in a_items),
            "a_terminal_count": sum(item.get("status") in TERMINAL for item in a_items),
            "a_provider_observed_once": sum(item.get("provider_event_matches") == 1 for item in a_items),
            "a_phase_metrics": a_items,
            "b_phase_metrics": b_items,
            "target_b_p95_wait_ms": 10000,
            "all_b_terminal": len(b_tasks) == 20 and len(b_items) == 20 and all(item.get("status") in TERMINAL for item in b_items),
            "all_b_provider_observed_once": len(b_tasks) == 20 and len(b_items) == 20 and all(item.get("provider_event_matches") == 1 for item in b_items),
            "target_b_p95_pass": (
                len(b_tasks) == 20
                and all(item.get("status") in TERMINAL for item in b_items)
                and all(item.get("provider_event_matches") == 1 for item in b_items)
                and summarize_wait(b_items, 20)["provider_started"] == 20
                and summarize_wait(b_items, 20)["p95_ms"] is not None
                and summarize_wait(b_items, 20)["p95_ms"] <= 10000
            ),
        },
        "long_non_preemptive": {
            "tasks": long_items,
            "interpretation": "The 15s three-slot case is reported separately; the short-job <=10s target is not applied to this non-preemptive scenario.",
        },
        "duplicate_quota_events": duplicates,
        "quota_duplicate_check_pass": not duplicates,
        "quota_coverage": quota,
        "submission_reconciliation": reconciliation,
        "provider_event_snapshot": event_snapshot,
        "submissions": submissions,
        "provider_delay_seconds": 2,
        "provider_long_delay_seconds": 15,
    }
    output["provider_attribution_pass"] = bool(short_items + long_items) and all(
        item.get("provider_event_matches") == 1 for item in short_items + long_items
    )
    output["pass"] = contention_pass(output)
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
    parser.add_argument("--provider-events", required=True)
    parser.add_argument("--identity", required=True)
    parser.add_argument("--out", required=True)
    parser.add_argument(
        "--repair-input",
        help="repair an existing report from DB evidence without issuing new requests",
    )
    return parser.parse_args()


if __name__ == "__main__":
    parsed_args = parse_args()
    if parsed_args.repair_input:
        result = repair_existing(parsed_args)
    else:
        result = asyncio.run(run(parsed_args))
    raise SystemExit(0 if result.get("pass") else 1)
