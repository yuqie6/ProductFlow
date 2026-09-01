#!/usr/bin/env python3
"""Repeatable, read-only HTTP performance and contract gates for the workbench."""
from __future__ import annotations

import argparse
import json
import math
import os
import random
import statistics
import subprocess
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass
from http.cookiejar import CookieJar
from pathlib import Path
from typing import Any, Callable
from urllib.error import HTTPError
from urllib.parse import quote
from urllib.request import HTTPCookieProcessor, Request, build_opener, urlopen


class GateFailure(RuntimeError):
    """A fixture or response violated a contract needed by this benchmark."""


@dataclass
class Client:
    base: str
    admin_key: str

    def __post_init__(self) -> None:
        self.base = self.base.rstrip("/")
        self._jar = CookieJar()
        self._opener = build_opener(HTTPCookieProcessor(self._jar))
        self._cookie_header = ""

    def login(self) -> None:
        request = Request(
            self.base + "/api/auth/session",
            data=json.dumps({"admin_key": self.admin_key}).encode("utf-8"),
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        try:
            with self._opener.open(request, timeout=10) as response:
                if response.status != 200:
                    raise RuntimeError(f"login {self.base} -> HTTP {response.status}")
                response.read()
        except HTTPError as exc:
            raise RuntimeError(f"login {self.base} -> HTTP {exc.code}") from exc
        except Exception as exc:  # noqa: BLE001
            raise RuntimeError(f"login {self.base} failed: {exc}") from exc
        self._cookie_header = "; ".join(f"{cookie.name}={cookie.value}" for cookie in self._jar)
        if not self._cookie_header:
            raise RuntimeError(f"login {self.base} returned no session cookie")

    def timed(self, path: str, timeout: float = 30.0) -> dict[str, Any]:
        request = Request(
            self.base + path,
            headers={"Accept": "application/json", "Cookie": self._cookie_header},
            method="GET",
        )
        started = time.perf_counter()
        try:
            with urlopen(request, timeout=timeout) as response:
                body = response.read()
                return {
                    "ok": 200 <= response.status < 300,
                    "status": response.status,
                    "ms": (time.perf_counter() - started) * 1000,
                    "bytes": len(body),
                    "body": body,
                    "error": None,
                }
        except HTTPError as exc:
            body = exc.read() if exc.fp else b""
            return {
                "ok": False,
                "status": exc.code,
                "ms": (time.perf_counter() - started) * 1000,
                "bytes": len(body),
                "body": b"",
                "error": f"HTTP {exc.code}",
            }
        except Exception as exc:  # noqa: BLE001
            return {
                "ok": False,
                "status": 0,
                "ms": (time.perf_counter() - started) * 1000,
                "bytes": 0,
                "body": b"",
                "error": str(exc),
            }


def percentile(samples: list[float], p: float) -> float:
    if not samples:
        return 0.0
    ordered = sorted(samples)
    rank = max(1, math.ceil(len(ordered) * p / 100))
    return ordered[min(len(ordered) - 1, rank - 1)]


def summarize(name: str, stack: str, path: str, rows: list[dict[str, Any]]) -> dict[str, Any]:
    successful = [row for row in rows if row["ok"]]
    times = [float(row["ms"]) for row in successful]
    sizes = [int(row["bytes"]) for row in successful]
    result = {
        "name": name,
        "stack": stack,
        "path": path,
        "n": len(rows),
        "ok": len(successful),
        "p50_ms": round(percentile(times, 50), 2) if times else None,
        "p95_ms": round(percentile(times, 95), 2) if times else None,
        "p99_ms": round(percentile(times, 99), 2) if times else None,
        "mean_ms": round(statistics.fmean(times), 2) if times else None,
        "min_ms": round(min(times), 2) if times else None,
        "max_ms": round(max(times), 2) if times else None,
        "min_bytes": min(sizes) if sizes else None,
        "max_bytes": max(sizes) if sizes else None,
        "error": next((row["error"] for row in rows if row["error"]), None),
    }
    print(
        f"{stack:5} {name:27} n={result['ok']}/{result['n']} "
        f"p50={result['p50_ms']} p95={result['p95_ms']} p99={result['p99_ms']} "
        f"bytes={result['min_bytes']}..{result['max_bytes']} err={result['error']}"
    )
    return result


def run_serial(
    client: Client,
    path: str,
    warmup: int,
    samples: int,
    validator: Callable[[bytes], Any] | None = None,
) -> tuple[list[dict[str, Any]], Any]:
    for _ in range(warmup):
        row = client.timed(path)
        if not row["ok"]:
            raise GateFailure(f"warmup failed for {path}: {row['error']}")
        if validator is not None:
            validator(row["body"])
    rows: list[dict[str, Any]] = []
    observed = None
    for _ in range(samples):
        row = client.timed(path)
        if row["ok"] and validator is not None:
            value = validator(row["body"])
            if observed is None:
                observed = value
        rows.append(row)
    return rows, observed


def run_parallel(client: Client, path: str, samples: int, concurrency: int) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    remaining = samples
    while remaining > 0:
        batch_size = min(remaining, concurrency)
        with ThreadPoolExecutor(max_workers=batch_size) as pool:
            futures = [pool.submit(client.timed, path) for _ in range(batch_size)]
            for future in as_completed(futures):
                rows.append(future.result())
        remaining -= batch_size
    return rows


def run_sequence(
    client: Client,
    current_path: str,
    runs_path: str,
    warmup: int,
    samples: int,
    runs_validator: Callable[[bytes], Any],
) -> list[dict[str, Any]]:
    for _ in range(warmup):
        current = client.timed(current_path)
        runs = client.timed(runs_path)
        if not current["ok"] or not runs["ok"]:
            raise GateFailure(f"workbench warmup failed: {current['error'] or runs['error']}")
        runs_validator(runs["body"])
    rows: list[dict[str, Any]] = []
    for _ in range(samples):
        started = time.perf_counter()
        current = client.timed(current_path)
        runs = client.timed(runs_path)
        row = {
            "ok": current["ok"] and runs["ok"],
            "status": runs["status"] if not current["ok"] else current["status"],
            "ms": (time.perf_counter() - started) * 1000,
            "bytes": current["bytes"] + runs["bytes"],
            "body": b"",
            "error": current["error"] or runs["error"],
        }
        if row["ok"]:
            runs_validator(runs["body"])
        rows.append(row)
    return rows


def run_interleaved(
    left: Client,
    right: Client,
    left_path: str,
    right_path: str,
    warmup: int,
    samples: int,
    rng: random.Random,
) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    for index in range(warmup):
        clients = [(left, left_path), (right, right_path)]
        if index % 2:
            clients.reverse()
        for client, path in clients:
            row = client.timed(path)
            if not row["ok"]:
                raise GateFailure(f"A/B warmup failed for {path}: {row['error']}")
    left_rows: list[dict[str, Any]] = []
    right_rows: list[dict[str, Any]] = []
    for _ in range(samples):
        clients = [(left, left_path, left_rows), (right, right_path, right_rows)]
        rng.shuffle(clients)
        for client, path, target in clients:
            target.append(client.timed(path))
    return left_rows, right_rows


def validate_summary(body: bytes) -> str:
    try:
        payload = json.loads(body)
    except json.JSONDecodeError as exc:
        raise GateFailure(f"GraphRun summary is not JSON: {exc}") from exc
    items = payload.get("items") if isinstance(payload, dict) else None
    if not isinstance(items, list):
        raise GateFailure("GraphRun summary has no items array")
    if len(items) > 20:
        raise GateFailure(f"GraphRun summary returned {len(items)} items; expected <=20")
    forbidden = {
        "snapshot",
        "snapshot_json",
        "compiled_context",
        "compiled_context_json",
        "input_trace",
        "output",
        "output_json",
    }

    def visit(value: Any, location: str) -> None:
        if isinstance(value, dict):
            leaked = forbidden.intersection(value)
            if leaked:
                raise GateFailure(f"GraphRun summary leaked {sorted(leaked)} at {location}")
            for key, child in value.items():
                visit(child, f"{location}.{key}")
        elif isinstance(value, list):
            for index, child in enumerate(value):
                visit(child, f"{location}[{index}]")

    visit(payload, "summary")
    if not items:
        raise GateFailure("GraphRun summary fixture has no runs")
    run_id = items[0].get("id")
    if not isinstance(run_id, str) or not run_id:
        raise GateFailure("GraphRun summary first item has no id")
    return run_id


def validate_detail(body: bytes) -> None:
    try:
        payload = json.loads(body)
    except json.JSONDecodeError as exc:
        raise GateFailure(f"GraphRun detail is not JSON: {exc}") from exc
    nodes = payload.get("node_runs") if isinstance(payload, dict) else None
    if not isinstance(nodes, list) or not nodes:
        raise GateFailure("GraphRun detail fixture has no node_runs")
    required = {"compiled_context", "input_trace", "output"}
    missing = required.difference(nodes[0])
    if missing:
        raise GateFailure(f"GraphRun detail omitted expected fields: {sorted(missing)}")


def encoded(value: str) -> str:
    return quote(value, safe="")


def check(
    checks: list[dict[str, Any]],
    name: str,
    condition: bool,
    detail: str,
) -> None:
    checks.append({"name": name, "ok": condition, "detail": detail})
    print(f"[{'PASS' if condition else 'FAIL'}] {name}: {detail}")


def check_summary_gate(
    checks: list[dict[str, Any]],
    name: str,
    result: dict[str, Any],
    max_p95_ms: float,
    max_bytes: int | None = None,
) -> None:
    p95 = result["p95_ms"]
    check(
        checks,
        name + ".errors",
        result["ok"] == result["n"],
        f"ok={result['ok']}/{result['n']}",
    )
    check(
        checks,
        name + ".p95",
        p95 is not None and p95 <= max_p95_ms,
        f"p95={p95}ms <= {max_p95_ms}ms",
    )
    if max_bytes is not None:
        max_seen = result["max_bytes"]
        check(
            checks,
            name + ".payload",
            max_seen is not None and max_seen <= max_bytes,
            f"max={max_seen}B <= {max_bytes}B",
        )


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--go-base",
        default=os.environ.get("HTTP_GATE_GO_BASE", f"http://127.0.0.1:{os.environ.get('APP_PORT', '29280')}"),
    )
    parser.add_argument("--go-product", default=os.environ.get("HTTP_GATE_GO_PRODUCT"))
    parser.add_argument("--go-graph", default=os.environ.get("HTTP_GATE_GO_GRAPH"))
    parser.add_argument("--main-base", default=os.environ.get("HTTP_GATE_MAIN_BASE"))
    parser.add_argument("--main-product", default=os.environ.get("HTTP_GATE_MAIN_PRODUCT"))
    parser.add_argument("--reference-go-base", default=os.environ.get("HTTP_GATE_REFERENCE_GO_BASE"))
    parser.add_argument("--reference-go-product", default=os.environ.get("HTTP_GATE_REFERENCE_GO_PRODUCT"))
    parser.add_argument("--reference-go-graph", default=os.environ.get("HTTP_GATE_REFERENCE_GO_GRAPH"))
    parser.add_argument("--warmup", type=int, default=int(os.environ.get("HTTP_GATE_WARMUP", "10")))
    parser.add_argument("--samples", type=int, default=int(os.environ.get("HTTP_GATE_SAMPLES", "100")))
    parser.add_argument("--concurrency", type=int, default=int(os.environ.get("HTTP_GATE_CONCURRENCY", "20")))
    parser.add_argument("--seed", type=int, default=int(os.environ.get("HTTP_GATE_SEED", "1")))
    parser.add_argument("--runs-p95-max-ms", type=float, default=30.0)
    parser.add_argument("--runs-max-bytes", type=int, default=24 * 1024)
    parser.add_argument("--detail-p95-max-ms", type=float, default=float(os.environ.get("HTTP_GATE_DETAIL_P95_MAX_MS", "100")))
    parser.add_argument("--detail-max-bytes", type=int, default=int(os.environ.get("HTTP_GATE_DETAIL_MAX_BYTES", str(512 * 1024))))
    parser.add_argument("--sequence-p95-max-ms", type=float, default=30.0)
    parser.add_argument("--product-list-p95-max-ms", type=float, default=15.0)
    parser.add_argument("--product-list-concurrent-p95-max-ms", type=float, default=80.0)
    parser.add_argument("--out", type=Path, default=Path(os.environ["HTTP_GATE_OUT"]) if os.environ.get("HTTP_GATE_OUT") else None)
    args = parser.parse_args()

    admin_key = os.environ.get("ADMIN_ACCESS_KEY")
    missing = []
    for name, value in (("--go-product", args.go_product), ("--go-graph", args.go_graph), ("ADMIN_ACCESS_KEY", admin_key)):
        if not value:
            missing.append(name)
    if missing:
        parser.error("missing " + ", ".join(missing) + "; use HTTP_GATE_* env vars")
    if bool(args.main_base) != bool(args.main_product):
        parser.error("--main-base and --main-product must be provided together")
    reference_values = (args.reference_go_base, args.reference_go_product, args.reference_go_graph)
    if any(reference_values) and not all(reference_values):
        parser.error("--reference-go-base, --reference-go-product, and --reference-go-graph must be provided together")
    if args.warmup < 0 or args.samples < 1 or args.concurrency < 1:
        parser.error("warmup >= 0, samples >= 1, and concurrency >= 1 are required")

    go = Client(args.go_base, admin_key)
    go.login()
    main_client = None
    if args.main_base:
        main_client = Client(args.main_base, admin_key)
        main_client.login()
    reference_client = None
    if args.reference_go_base:
        reference_client = Client(args.reference_go_base, admin_key)
        reference_client.login()

    product_list_20 = "/api/v2/products?page=1&page_size=20"
    product_list_100 = "/api/v2/products?page=1&page_size=100"
    current_path = f"/api/v3/products/{encoded(args.go_product)}/workflows/current"
    runs_path = f"/api/v3/products/{encoded(args.go_product)}/workflows/{encoded(args.go_graph)}/runs"
    reference_current_path = None
    reference_runs_path = None
    if reference_client is not None:
        reference_current_path = f"/api/v3/products/{encoded(args.reference_go_product)}/workflows/current"
        reference_runs_path = f"/api/v3/products/{encoded(args.reference_go_product)}/workflows/{encoded(args.reference_go_graph)}/runs"

    results: list[dict[str, Any]] = []
    checks: list[dict[str, Any]] = []

    summary_rows, first_run_id = run_serial(go, runs_path, args.warmup, args.samples, validate_summary)
    results.append(summarize("graph_run_summary", "go", runs_path, summary_rows))
    if not first_run_id:
        raise GateFailure("summary benchmark did not expose a run id")
    detail_path = f"/api/v3/products/{encoded(args.go_product)}/workflows/{encoded(args.go_graph)}/runs/{encoded(first_run_id)}"
    detail_rows, _ = run_serial(go, detail_path, args.warmup, args.samples, validate_detail)
    results.append(summarize("graph_run_detail", "go", detail_path, detail_rows))
    check_summary_gate(checks, "graph_run_summary", results[-2], args.runs_p95_max_ms, args.runs_max_bytes)
    check(checks, "graph_run_summary.schema", True, "forbidden detail fields absent")
    check_summary_gate(checks, "graph_run_detail", results[-1], args.detail_p95_max_ms, args.detail_max_bytes)
    check(checks, "graph_run_detail.contract", results[-1]["ok"] == results[-1]["n"], "detail retains compiled_context/input_trace/output")

    sequence_rows = run_sequence(go, current_path, runs_path, args.warmup, args.samples, validate_summary)
    results.append(summarize("workbench_current_then_runs", "go", current_path + " + " + runs_path, sequence_rows))
    check_summary_gate(checks, "workbench_current_then_runs", results[-1], args.sequence_p95_max_ms)

    list20_rows, _ = run_serial(go, product_list_20, args.warmup, args.samples)
    results.append(summarize("product_list_p20", "go", product_list_20, list20_rows))
    check_summary_gate(checks, "product_list_p20", results[-1], args.product_list_p95_max_ms)

    list100_rows, _ = run_serial(go, product_list_100, args.warmup, args.samples)
    results.append(summarize("product_list_p100", "go", product_list_100, list100_rows))
    check_summary_gate(checks, "product_list_p100", results[-1], args.product_list_p95_max_ms)

    concurrent_rows = run_parallel(go, product_list_20, args.samples, args.concurrency)
    results.append(summarize("product_list_p20_concurrent", "go", product_list_20, concurrent_rows))
    check_summary_gate(checks, "product_list_p20_concurrent", results[-1], args.product_list_concurrent_p95_max_ms)

    if reference_client is not None:
        rng = random.Random(args.seed)
        comparisons = [
            ("workbench_graph", current_path, reference_current_path),
            ("run_history", runs_path, reference_runs_path),
        ]
        for name, go_path, reference_path in comparisons:
            go_rows, reference_rows = run_interleaved(
                go, reference_client, go_path, reference_path, args.warmup, args.samples, rng
            )
            results.append(summarize(name, "go", go_path, go_rows))
            results.append(summarize(name, "reference", reference_path, reference_rows))
    else:
        print("[SKIP] Go A/B comparison: set HTTP_GATE_REFERENCE_GO_BASE, HTTP_GATE_REFERENCE_GO_PRODUCT, and HTTP_GATE_REFERENCE_GO_GRAPH")

    if main_client is not None:
        rng = random.Random(args.seed)
        main_current_path = f"/api/products/{encoded(args.main_product)}/workflow"
        main_runs_path = f"/api/products/{encoded(args.main_product)}/workflow/status"
        comparisons = [
            ("workbench_graph", current_path, main_current_path),
            ("run_history", runs_path, main_runs_path),
            ("product_list_p20", product_list_20, product_list_20.replace("/api/v2", "/api")),
            ("product_list_p100", product_list_100, product_list_100.replace("/api/v2", "/api")),
        ]
        for name, go_path, main_path in comparisons:
            go_rows, main_rows = run_interleaved(go, main_client, go_path, main_path, args.warmup, args.samples, rng)
            results.append(summarize(name, "go", go_path, go_rows))
            results.append(summarize(name, "main", main_path, main_rows))
        main_concurrent = run_parallel(main_client, "/api/products?page=1&page_size=20", args.samples, args.concurrency)
        results.append(summarize("product_list_p20_concurrent", "main", "/api/products?page=1&page_size=20", main_concurrent))
    else:
        print("[SKIP] A/B comparison: set HTTP_GATE_MAIN_BASE and HTTP_GATE_MAIN_PRODUCT to enable the legacy reference")

    def detected_commit() -> str:
        try:
            root = Path(__file__).resolve().parents[1]
            commit = subprocess.run(
                ["git", "rev-parse", "HEAD"],
                cwd=root,
                check=True,
                capture_output=True,
                text=True,
            ).stdout.strip()
            dirty = subprocess.run(
                ["git", "status", "--porcelain"],
                cwd=root,
                check=True,
                capture_output=True,
                text=True,
            ).stdout.strip()
            return commit + ("-dirty" if dirty else "")
        except (OSError, subprocess.CalledProcessError):
            return "unknown"

    report = {
        "version": 1,
        "when": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
        "go_base": args.go_base,
        "main_base": args.main_base,
        "reference_go_base": args.reference_go_base,
        "service_commits": {
            "go": os.environ.get("HTTP_GATE_GO_COMMIT", detected_commit()),
            "main": os.environ.get("HTTP_GATE_MAIN_COMMIT") if args.main_base else None,
            "reference_go": os.environ.get("HTTP_GATE_REFERENCE_GO_COMMIT") if args.reference_go_base else None,
        },
        "fixture": {
            "go_product": args.go_product,
            "go_graph": args.go_graph,
            "main_product": args.main_product,
            "reference_go_product": args.reference_go_product,
            "reference_go_graph": args.reference_go_graph,
        },
        "warmup": args.warmup,
        "samples": args.samples,
        "concurrency": args.concurrency,
        "thresholds": {
            "runs_p95_max_ms": args.runs_p95_max_ms,
            "runs_max_bytes": args.runs_max_bytes,
            "detail_p95_max_ms": args.detail_p95_max_ms,
            "detail_max_bytes": args.detail_max_bytes,
            "sequence_p95_max_ms": args.sequence_p95_max_ms,
            "product_list_p95_max_ms": args.product_list_p95_max_ms,
            "product_list_concurrent_p95_max_ms": args.product_list_concurrent_p95_max_ms,
        },
        "checks": checks,
        "results": results,
    }
    if args.out is not None:
        args.out.parent.mkdir(parents=True, exist_ok=True)
        args.out.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
        print(f"wrote {args.out}")
    return 0 if all(check["ok"] for check in checks) else 1


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (GateFailure, RuntimeError) as exc:
        print(f"[FAIL] {exc}", file=sys.stderr)
        raise SystemExit(1)
