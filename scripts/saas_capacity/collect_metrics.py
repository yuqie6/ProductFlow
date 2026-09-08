#!/usr/bin/env python3
"""Collect per-round Docker, PostgreSQL, Redis, and storage evidence."""

import argparse
import csv
import json
import re
import subprocess
import time
from datetime import datetime, timezone
from pathlib import Path
import psycopg

from capacity_identity import verify_identity


CONTAINERS = [
    "pf-capacity-0908-postgres",
    "pf-capacity-0908-redis",
    "pf-capacity-0908-mock",
    "pf-capacity-0908-api",
    "pf-capacity-0908-worker",
    "pf-capacity-0908-dispatcher",
]


def iso_now() -> str:
    return datetime.now(timezone.utc).isoformat()


def parse_number(value: str) -> float | None:
    try:
        return float(value.strip().rstrip("%"))
    except (AttributeError, ValueError):
        return None


def parse_bytes(value: str) -> int | None:
    value = value.strip()
    match = re.match(r"^([0-9.]+)\s*([KMGT]?i?B)$", value, re.IGNORECASE)
    if not match:
        return None
    number = float(match.group(1))
    unit = match.group(2).lower()
    factors = {"b": 1, "kb": 1000, "kib": 1024, "mb": 1000**2, "mib": 1024**2, "gb": 1000**3, "gib": 1024**3, "tb": 1000**4, "tib": 1024**4}
    return int(number * factors[unit]) if unit in factors else None


def parse_key_values(text: str) -> dict[str, int]:
    result: dict[str, int] = {}
    for line in text.splitlines():
        key, _, value = line.partition(" ")
        if not key or not value.strip():
            continue
        try:
            result[key] = int(value.strip())
        except ValueError:
            continue
    return result


def parse_cgroup_mounts(text: str) -> dict[str, Path]:
    mounts: dict[str, Path] = {}
    for line in text.splitlines():
        fields = line.split()
        if len(fields) < 4 or fields[2] != "cgroup":
            continue
        options = set(fields[3].split(","))
        for subsystem in ("memory", "cpu", "cpuacct"):
            if subsystem in options and subsystem not in mounts:
                mounts[subsystem] = Path(fields[1])
    return mounts


def parse_cgroup_paths(text: str) -> dict[str, str]:
    paths: dict[str, str] = {}
    for line in text.splitlines():
        hierarchy, _, relative = line.partition(":")
        if not relative or ":" not in relative:
            continue
        subsystems, _, cgroup_path = relative.partition(":")
        for subsystem in subsystems.split(","):
            if subsystem in {"memory", "cpu", "cpuacct"}:
                paths[subsystem] = cgroup_path
    return paths


def parse_cgroup_sample(memory_stat: str, memory_limit: str, cpu_quota: str, cpu_period: str, cpu_stat: str) -> dict[str, int | float]:
    memory_values = parse_key_values(memory_stat)
    rss_bytes = memory_values.get("total_rss", memory_values.get("rss"))
    memory_usage_bytes = memory_values.get("total_cache", 0) + (rss_bytes or 0)
    try:
        limit_bytes = int(memory_limit.strip())
        quota = int(cpu_quota.strip())
        period = int(cpu_period.strip())
    except ValueError as error:
        raise RuntimeError("cgroup v1 limit file is not numeric") from error
    if rss_bytes is None or limit_bytes <= 0 or quota <= 0 or period <= 0:
        raise RuntimeError("cgroup v1 lacks bounded RSS or CPU limit")
    cpu_values = parse_key_values(cpu_stat)
    if "nr_throttled" not in cpu_values or "throttled_time" not in cpu_values:
        raise RuntimeError("cgroup v1 cpu.stat lacks throttling counters")
    return {
        "rss_bytes": rss_bytes,
        "memory_usage_bytes": memory_usage_bytes,
        "memory_limit_bytes": limit_bytes,
        "cpu_limit_cores": quota / period,
        "cpu_nr_throttled": cpu_values["nr_throttled"],
        "cpu_throttled_usec": cpu_values["throttled_time"] / 1000,
    }


def docker_stats() -> tuple[dict[str, dict[str, object]], str | None]:
    command = ["docker", "stats", "--no-stream", "--format", "{{json .}}", *CONTAINERS]
    try:
        output = subprocess.check_output(command, text=True, stderr=subprocess.STDOUT)
    except (subprocess.CalledProcessError, OSError) as error:
        return {}, f"docker stats failed: {error}"
    result: dict[str, dict[str, object]] = {}
    for line in output.splitlines():
        try:
            item = json.loads(line)
        except json.JSONDecodeError:
            continue
        memory_parts = str(item.get("MemUsage", "")).split(" / ", 1)
        memory_bytes = parse_bytes(memory_parts[0]) if memory_parts else None
        memory_limit_bytes = parse_bytes(memory_parts[1]) if len(memory_parts) == 2 else None
        result[str(item.get("Name", ""))] = {
            "cpu_percent": parse_number(str(item.get("CPUPerc", ""))),
            "memory_bytes": memory_bytes,
            "memory_limit_bytes": memory_limit_bytes,
            "memory_display": memory_parts[0] if memory_parts else "",
            "memory_percent": parse_number(str(item.get("MemPerc", ""))),
            "net_io": str(item.get("NetIO", "")),
            "block_io": str(item.get("BlockIO", "")),
        }
    missing = sorted(set(CONTAINERS) - set(result))
    return result, (f"docker stats missing containers: {','.join(missing)}" if missing else None)


def docker_limits(expected_image_name: str | None = None, expected_image_id: str | None = None) -> tuple[dict[str, dict[str, object]], str | None]:
    try:
        raw = subprocess.check_output(
            [
                "docker",
                "inspect",
                "--format",
                "{{.Name}}\t{{.State.Pid}}\t{{.HostConfig.NanoCpus}}\t{{.HostConfig.Memory}}\t{{.Config.Image}}\t{{.Image}}",
                *CONTAINERS,
            ],
            text=True,
            stderr=subprocess.STDOUT,
        )
    except (subprocess.CalledProcessError, OSError) as error:
        return {}, f"docker inspect limits failed: {error}"
    result: dict[str, dict[str, object]] = {}
    for line in raw.splitlines():
        name, pid, nano_cpus, memory, image_name, image_id = (line.split("\t") + ["", "", "", "", "", ""])[:6]
        result[name.lstrip("/")] = {
            "pid": int(pid) if pid.isdigit() else None,
            "nano_cpus": int(nano_cpus) if nano_cpus.isdigit() else None,
            "memory_bytes": int(memory) if memory.isdigit() else None,
            "image_name": image_name,
            "image_id": image_id,
        }
    missing = sorted(set(CONTAINERS) - set(result))
    errors = [f"docker inspect missing containers: {','.join(missing)}"] if missing else []
    if expected_image_name or expected_image_id:
        for name in ("pf-capacity-0908-api", "pf-capacity-0908-worker", "pf-capacity-0908-dispatcher"):
            item = result.get(name)
            if item is None:
                continue
            if expected_image_name and item.get("image_name") != expected_image_name:
                errors.append(f"{name}: image name differs from frozen identity")
            if expected_image_id and item.get("image_id") != expected_image_id:
                errors.append(f"{name}: image id differs from frozen identity")
    return result, ("; ".join(errors) if errors else None)


def cgroup_v1_sample(limits: dict[str, dict[str, object]]) -> tuple[dict[str, dict[str, object]], str | None]:
    try:
        mounts = parse_cgroup_mounts(Path("/proc/mounts").read_text(encoding="utf-8"))
    except OSError as error:
        return {}, f"cgroup v1 mount table unavailable: {error}"
    if "memory" not in mounts or "cpu" not in mounts:
        return {}, "cgroup v1 memory/cpu mounts unavailable; resource observation refused"
    result: dict[str, dict[str, object]] = {}
    errors: list[str] = []
    for name in CONTAINERS:
        item = limits.get(name, {})
        pid = item.get("pid")
        if not isinstance(pid, int) or pid <= 0:
            errors.append(f"{name}: container pid unavailable")
            continue
        try:
            paths = parse_cgroup_paths(Path(f"/proc/{pid}/cgroup").read_text(encoding="utf-8"))
            memory_path = mounts["memory"] / paths["memory"].lstrip("/")
            cpu_path = mounts["cpu"] / paths.get("cpu", paths.get("cpuacct", "")).lstrip("/")
            sample = parse_cgroup_sample(
                (memory_path / "memory.stat").read_text(encoding="utf-8"),
                (memory_path / "memory.limit_in_bytes").read_text(encoding="utf-8"),
                (cpu_path / "cpu.cfs_quota_us").read_text(encoding="utf-8"),
                (cpu_path / "cpu.cfs_period_us").read_text(encoding="utf-8"),
                (cpu_path / "cpu.stat").read_text(encoding="utf-8"),
            )
            sample["memory_usage_bytes"] = int((memory_path / "memory.usage_in_bytes").read_text(encoding="utf-8").strip())
            configured_cpu = item.get("nano_cpus")
            configured_memory = item.get("memory_bytes")
            if not isinstance(configured_cpu, int) or not isinstance(configured_memory, int):
                raise RuntimeError("Docker configured CPU/memory limit is missing")
            if sample["memory_limit_bytes"] != configured_memory:
                raise RuntimeError(
                    f"memory cgroup limit {sample['memory_limit_bytes']} != Docker limit {configured_memory}"
                )
            if abs(float(sample["cpu_limit_cores"]) - configured_cpu / 1_000_000_000) > 0.000001:
                raise RuntimeError("CPU cgroup limit differs from Docker configured limit")
            result[name] = sample
        except (KeyError, OSError, RuntimeError) as error:
            errors.append(f"{name}: {error}")
    return result, ("; ".join(errors) if errors else None)


def redis_info(port: int) -> tuple[dict[str, object], str | None]:
    info: dict[str, object] = {}
    errors: list[str] = []
    commands = [
        ["redis-cli", "-h", "127.0.0.1", "-p", str(port), "INFO", "memory"],
        ["redis-cli", "-h", "127.0.0.1", "-p", str(port), "INFO", "stats"],
        ["redis-cli", "-h", "127.0.0.1", "-p", str(port), "INFO", "clients"],
        ["redis-cli", "-h", "127.0.0.1", "-p", str(port), "INFO", "commandstats"],
        ["redis-cli", "-h", "127.0.0.1", "-p", str(port), "LATENCY", "LATEST"],
    ]
    for command in commands:
        try:
            output = subprocess.check_output(command, text=True, stderr=subprocess.STDOUT)
        except (subprocess.CalledProcessError, OSError) as error:
            errors.append(f"{' '.join(command[4:])}: {error}")
            continue
        for line in output.splitlines():
            if not line or line.startswith("#") or ":" not in line:
                continue
            key, value = line.split(":", 1)
            if key.startswith("cmdstat_"):
                match = re.search(r"calls=([0-9]+).*usec_per_call=([0-9.]+)", value)
                if match:
                    info[key] = {"calls": int(match.group(1)), "usec_per_call": float(match.group(2))}
            else:
                try:
                    info[key] = int(value)
                except ValueError:
                    info[key] = value
    missing = sorted({"used_memory", "evicted_keys", "blocked_clients"} - info.keys())
    if missing:
        errors.append(f"Redis metrics missing: {', '.join(missing)}")
    return info, ("; ".join(errors) if errors else None)


def postgres_info(connection: psycopg.Connection | None) -> dict[str, object]:
    if connection is None:
        return {"error": "postgres connection unavailable"}
    result: dict[str, object] = {}
    try:
        with connection.cursor() as cursor:
            cursor.execute("SELECT count(*) FROM pg_stat_activity")
            result["connections_total"] = cursor.fetchone()[0]
            cursor.execute("SELECT count(*) FROM pg_stat_activity WHERE state = 'active'")
            result["connections_active"] = cursor.fetchone()[0]
            cursor.execute("SHOW max_connections")
            result["max_connections"] = int(cursor.fetchone()[0])
        connection.commit()
    except psycopg.Error as error:
        connection.rollback()
        result["error"] = str(error)
    return result


def storage_usage(path: Path) -> tuple[int | None, str | None]:
    try:
        # The database/Redis bind mounts are root-owned by their containers;
        # measure the accessible application/media volume without turning a
        # host permission detail into a fabricated zero or failed full sample.
        output = subprocess.check_output(
            ["du", "-sb", "--exclude=postgres", "--exclude=redis", str(path)],
            text=True,
            stderr=subprocess.STDOUT,
        )
        return int(output.split()[0]), None
    except (subprocess.CalledProcessError, OSError, ValueError, IndexError) as error:
        return None, f"storage usage failed: {error}"


def evaluate_window_coverage(
    sample_epochs: list[float],
    window_start_epoch: float | None,
    window_end_epoch: float | None,
    interval: float,
) -> dict[str, object]:
    if window_start_epoch is None or window_end_epoch is None:
        return {"status": "unmeasurable", "reason": "load window marker is missing", "sample_count": 0}
    samples = sorted(epoch for epoch in sample_epochs if window_start_epoch <= epoch <= window_end_epoch)
    if not samples:
        return {"status": "unmeasurable", "reason": "no resource sample overlaps load window", "sample_count": 0}
    # Docker stats plus the cgroup/Redis/Postgres probes take about 3.1s on
    # this stack; keep a 4s ceiling while still rejecting longer blind spots.
    tolerance = max(interval * 2, 4.0)
    first = samples[0]
    last = samples[-1]
    gaps = [second - first for first, second in zip(samples, samples[1:])]
    max_gap = max(gaps, default=window_end_epoch - window_start_epoch)
    edge_covered = first <= window_start_epoch + tolerance and last >= window_end_epoch - tolerance
    covered = edge_covered and max_gap <= tolerance
    if not edge_covered:
        reason = "resource samples do not span both load-window edges"
    elif max_gap > tolerance:
        reason = f"resource sample gap {max_gap:.3f}s exceeds {tolerance:.3f}s tolerance"
    else:
        reason = None
    return {
        "status": "covered" if covered else "unmeasurable",
        "reason": reason,
        "sample_count": len(samples),
        "first_sample_epoch": first,
        "last_sample_epoch": last,
        "max_gap_seconds": max_gap,
        "gap_tolerance_seconds": tolerance,
        "window_seconds": window_end_epoch - window_start_epoch,
    }


def read_window_marker(path: Path | None) -> dict[str, float] | None:
    if path is None or not path.exists():
        return None
    payload = json.loads(path.read_text(encoding="utf-8"))
    start = float(payload["window_start_epoch"])
    end = float(payload["window_end_epoch"])
    if end <= start:
        raise RuntimeError("load window marker has non-positive duration")
    return {"window_start_epoch": start, "window_end_epoch": end}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--db-url", required=True)
    parser.add_argument("--redis-port", type=int, default=30184)
    parser.add_argument("--duration", type=float, default=900, help="maximum collector runtime")
    parser.add_argument("--interval", type=float, default=1)
    parser.add_argument("--out", required=True)
    parser.add_argument("--label", required=True)
    parser.add_argument("--storage-root")
    parser.add_argument("--window-file", type=Path)
    parser.add_argument("--startup-timeout", type=float, default=120)
    parser.add_argument("--grace", type=float, default=5)
    parser.add_argument("--identity", type=Path, required=True)
    args = parser.parse_args()
    repo_root = Path(__file__).resolve().parents[2]
    identity = verify_identity(repo_root, args.identity)
    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    csv_path = out.with_suffix(".csv")
    if out.exists() or csv_path.exists():
        raise RuntimeError(f"capacity metrics output already exists: {out}")
    samples: list[dict[str, object]] = []
    started = time.monotonic()
    image_identity = identity.get("image") or {}
    limits, limits_error = docker_limits(
        str(image_identity.get("name") or ""),
        str(image_identity.get("id") or ""),
    )
    connection: psycopg.Connection | None = None
    fatal_error: str | None = None
    writer_fields = [
        "timestamp", "timestamp_epoch", "label", "complete", "sample_errors", "container_count",
        "stack_rss_bytes", "stack_memory_usage_bytes", "stack_cpu_percent", "stack_cpu_throttled_usec",
        "actual_cpu_limit_cores", "actual_memory_limit_bytes", "pg_connections_total", "pg_connections_active", "pg_max_connections",
        "redis_used_memory", "redis_evicted_keys", "redis_blocked_clients", "redis_command_latency_usec",
        "storage_bytes", "containers_json", "cgroup_json", "limits_json", "redis_json",
    ]
    with csv_path.open("w", newline="", encoding="utf-8") as stream:
        writer = csv.DictWriter(stream, fieldnames=writer_fields)
        writer.writeheader()
        while True:
            elapsed = time.monotonic() - started
            try:
                window = read_window_marker(args.window_file)
            except (OSError, KeyError, TypeError, ValueError, json.JSONDecodeError) as error:
                fatal_error = f"load window marker invalid: {error}"
                break
            now_epoch = time.time()
            if window is not None:
                if now_epoch >= window["window_end_epoch"] + args.grace:
                    break
            elif args.window_file is not None:
                if elapsed >= args.startup_timeout:
                    fatal_error = "load window marker did not appear before startup timeout"
                    break
            elif elapsed >= args.duration:
                break
            if elapsed >= args.duration:
                fatal_error = f"collector reached maximum runtime before load window ended: {args.duration}s"
                break
            timestamp = iso_now()
            containers, docker_error = docker_stats()
            if connection is None or connection.closed:
                try:
                    connection = psycopg.connect(args.db_url, autocommit=False)
                except psycopg.Error:
                    connection = None
            pg = postgres_info(connection)
            redis, redis_error = redis_info(args.redis_port)
            storage_bytes, storage_error = storage_usage(Path(args.storage_root)) if args.storage_root else (None, "storage root not configured")
            cgroups, cgroup_error = cgroup_v1_sample(limits)
            errors = [item for item in [docker_error, limits_error, cgroup_error, pg.get("error"), redis_error, storage_error] if item]
            coverage_complete = len(containers) == len(CONTAINERS) and len(cgroups) == len(CONTAINERS) and all(
                containers[name].get("memory_bytes") is not None and containers[name].get("cpu_percent") is not None for name in CONTAINERS
            )
            coverage_complete = coverage_complete and all(
                all(key in cgroups[name] for key in ("rss_bytes", "memory_usage_bytes", "memory_limit_bytes", "cpu_limit_cores", "cpu_throttled_usec"))
                for name in CONTAINERS
                if name in cgroups
            )
            stack_rss = sum(int(cgroups[name]["rss_bytes"]) for name in CONTAINERS) if coverage_complete else None
            stack_memory_usage = sum(int(cgroups[name]["memory_usage_bytes"]) for name in CONTAINERS) if coverage_complete else None
            stack_cpu = sum(float(containers[name]["cpu_percent"]) for name in CONTAINERS) if coverage_complete else None
            stack_cpu_throttled = sum(float(cgroups[name]["cpu_throttled_usec"]) for name in CONTAINERS) if coverage_complete else None
            actual_cpu_limit = sum(float(cgroups[name]["cpu_limit_cores"]) for name in CONTAINERS) if coverage_complete else None
            actual_memory_limit = sum(int(cgroups[name]["memory_limit_bytes"]) for name in CONTAINERS) if coverage_complete else None
            command_latencies = [
                float(item.get("usec_per_call", 0))
                for key, item in redis.items()
                if key.startswith("cmdstat_") and isinstance(item, dict)
            ]
            complete = (
                not errors
                and coverage_complete
                and all(key in pg for key in ("connections_total", "connections_active", "max_connections"))
                and all(key in redis for key in ("used_memory", "evicted_keys", "blocked_clients"))
            )
            row = {
                "timestamp": timestamp,
                "timestamp_epoch": now_epoch,
                "label": args.label,
                "complete": complete,
                "sample_errors": json.dumps(errors, ensure_ascii=False),
                "container_count": len(containers),
                "stack_rss_bytes": stack_rss if complete else "",
                "stack_memory_usage_bytes": stack_memory_usage if complete else "",
                "stack_cpu_percent": stack_cpu if complete else "",
                "stack_cpu_throttled_usec": stack_cpu_throttled if complete else "",
                "actual_cpu_limit_cores": actual_cpu_limit if complete else "",
                "actual_memory_limit_bytes": actual_memory_limit if complete else "",
                "pg_connections_total": pg.get("connections_total", "") if complete else "",
                "pg_connections_active": pg.get("connections_active", "") if complete else "",
                "pg_max_connections": pg.get("max_connections", "") if complete else "",
                "redis_used_memory": redis.get("used_memory", "") if complete else "",
                "redis_evicted_keys": redis.get("evicted_keys", "") if complete else "",
                "redis_blocked_clients": redis.get("blocked_clients", "") if complete else "",
                "redis_command_latency_usec": max(command_latencies) if command_latencies and complete else "",
                "storage_bytes": storage_bytes if complete else "",
                "containers_json": json.dumps(containers, sort_keys=True),
                "cgroup_json": json.dumps(cgroups, sort_keys=True),
                "limits_json": json.dumps(limits, sort_keys=True),
                "redis_json": json.dumps(redis, sort_keys=True),
            }
            writer.writerow(row)
            stream.flush()
            samples.append(row)
            time.sleep(min(args.interval, max(0.0, args.duration - (time.monotonic() - started))))
    if connection is not None and not connection.closed:
        connection.close()
    try:
        window = read_window_marker(args.window_file)
    except (OSError, KeyError, TypeError, ValueError, json.JSONDecodeError):
        window = None
    try:
        measured_samples = [
            item
            for item in samples
            if window is None
            or (
                window["window_start_epoch"] <= float(item["timestamp_epoch"]) <= window["window_end_epoch"]
            )
        ]
    except (KeyError, TypeError, ValueError):
        measured_samples = []
    complete_samples = [item for item in measured_samples if item["complete"] is True or item["complete"] == "True"]
    rss_values = [int(item["stack_rss_bytes"]) for item in complete_samples if str(item["stack_rss_bytes"]).isdigit()]
    memory_values = [int(item["stack_memory_usage_bytes"]) for item in complete_samples if str(item["stack_memory_usage_bytes"]).isdigit()]
    cpu_values = [float(item["stack_cpu_percent"]) for item in complete_samples if str(item["stack_cpu_percent"]) not in {"", "None"}]
    throttled_values = [float(item["stack_cpu_throttled_usec"]) for item in complete_samples if str(item["stack_cpu_throttled_usec"]) not in {"", "None"}]
    actual_cpu_limits = [float(item["actual_cpu_limit_cores"]) for item in complete_samples if str(item["actual_cpu_limit_cores"]) not in {"", "None"}]
    actual_memory_limits = [int(item["actual_memory_limit_bytes"]) for item in complete_samples if str(item["actual_memory_limit_bytes"]).isdigit()]
    pg_values = [int(item["pg_connections_total"]) for item in complete_samples if str(item["pg_connections_total"]).isdigit()]
    max_connections = max((int(item["pg_max_connections"]) for item in complete_samples if str(item["pg_max_connections"]).isdigit()), default=None)
    storage_values = [int(item["storage_bytes"]) for item in complete_samples if str(item["storage_bytes"]).isdigit()]
    limit_cpu_cores = sum((int(item["nano_cpus"]) for item in limits.values() if item.get("nano_cpus")), 0) / 1_000_000_000
    limit_memory_bytes = sum((int(item["memory_bytes"]) for item in limits.values() if item.get("memory_bytes")), 0)
    sample_epochs = [float(item["timestamp_epoch"]) for item in samples]
    window_coverage = evaluate_window_coverage(
        sample_epochs,
        window["window_start_epoch"] if window else None,
        window["window_end_epoch"] if window else None,
        args.interval,
    )
    resource_status = (
        "measurable"
        if measured_samples and len(complete_samples) == len(measured_samples) and window_coverage["status"] == "covered" and fatal_error is None
        else "unmeasurable"
    )
    summary = {
        "label": args.label,
        "duration_seconds": time.monotonic() - started,
        "sample_count": len(samples),
        "window_sample_count": len(measured_samples),
        "complete_sample_count": len(complete_samples),
        "incomplete_sample_count": len(measured_samples) - len(complete_samples),
        "resource_observation_status": resource_status,
        "fatal_error": fatal_error,
        "source_identity_sha256": identity["source_identity_sha256"],
        "image_identity": image_identity,
        "cgroup_version": "v1",
        "peak_stack_rss_bytes": max(rss_values) if rss_values else None,
        "peak_stack_rss_gib": (max(rss_values) / (1024**3)) if rss_values else None,
        "peak_stack_memory_usage_bytes": max(memory_values) if memory_values else None,
        "peak_stack_memory_usage_gib": (max(memory_values) / (1024**3)) if memory_values else None,
        "peak_stack_cpu_percent_sum": max(cpu_values) if cpu_values else None,
        "peak_stack_cpu_throttled_usec": max(throttled_values) if throttled_values else None,
        "actual_cpu_limit_cores": max(actual_cpu_limits) if actual_cpu_limits else None,
        "actual_memory_limit_bytes": max(actual_memory_limits) if actual_memory_limits else None,
        "peak_pg_connections": max(pg_values) if pg_values else None,
        "pg_max_connections": max_connections,
        "peak_pg_connection_fraction": (max(pg_values) / max_connections) if pg_values and max_connections else None,
        "peak_storage_bytes": max(storage_values) if storage_values else None,
        "configured_cpu_limit_cores": limit_cpu_cores if limits else None,
        "configured_memory_limit_bytes": limit_memory_bytes if limits else None,
        "container_coverage": {"expected": len(CONTAINERS), "observed_min": min((int(item["container_count"]) for item in samples), default=0), "observed_max": max((int(item["container_count"]) for item in samples), default=0)},
        "load_window": window,
        "window_coverage": window_coverage,
        "limits_error": limits_error,
        "csv": str(csv_path),
    }
    out.write_text(json.dumps(summary, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(summary, sort_keys=True))
    return 0 if resource_status == "measurable" else 1


if __name__ == "__main__":
    raise SystemExit(main())
