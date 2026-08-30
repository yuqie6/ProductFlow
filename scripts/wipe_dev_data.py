#!/usr/bin/env python3
"""Wipe ProductFlow *business* data in the live DATABASE_URL database.

Keeps provider_profiles, provider_bindings, app_settings, and alembic_version.
tables to $STORAGE_ROOT/settings-keep.sql before truncating anything else.
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
import uuid
from pathlib import Path
from urllib.parse import unquote, urlparse

KEEP_TABLES = ("app_settings", "provider_profiles", "provider_bindings", "alembic_version")
LOCAL_HOSTS = {"localhost", "127.0.0.1", "::1", "postgres"}


def require_local_host(kind: str, host: str) -> None:
    name = (host or "").split("%")[0].lower()
    if name not in LOCAL_HOSTS:
        raise SystemExit(f"{kind} host {host!r} is not local; refuse to wipe")


def repo_root() -> Path:
    return Path(__file__).resolve().parents[1]


def postgres_dsn(raw: str) -> tuple[str, dict[str, str | int]]:
    normalized = (
        raw.replace("postgresql+psycopg://", "postgresql://", 1)
        .replace("postgresql+psycopg2://", "postgresql://", 1)
    )
    u = urlparse(normalized)
    kw = {
        "host": u.hostname or "localhost",
        "port": u.port or 5432,
        "dbname": u.path.lstrip("/"),
        "user": u.username or "",
        "password": unquote(u.password or ""),
    }
    return normalized, kw


def connect(kw: dict[str, str | int]):
    import psycopg

    return psycopg.connect(
        host=kw["host"],
        port=kw["port"],
        dbname=kw["dbname"],
        user=kw["user"],
        password=kw["password"],
        autocommit=True,
    )


def settings_dump_path() -> Path:
    root = os.environ.get("STORAGE_ROOT", str(repo_root() / "storage-dev"))
    path = Path(root)
    if not path.is_absolute():
        path = repo_root() / path
    path.mkdir(parents=True, exist_ok=True)
    return path / "settings-keep.sql"


def dump_settings(kw: dict[str, str | int], dest: Path) -> None:
    url, _ = postgres_dsn(os.environ["DATABASE_URL"])
    cmd = [
        "pg_dump",
        "--data-only",
        "--column-inserts",
        "--no-owner",
        "--no-acl",
        url,
    ]
    for name in KEEP_TABLES:
        cmd.extend(["-t", f"public.{name}"])
    try:
        result = subprocess.run(cmd, check=False, capture_output=True, text=True)
    except FileNotFoundError:
        result = None
    if result is not None and result.returncode == 0 and result.stdout.strip():
        dest.write_text(result.stdout)
        return
    # Fallback: COPY as SQL INSERTs via psycopg.
    conn = connect(kw)
    cur = conn.cursor()
    chunks = ["BEGIN;\n"]
    for table in reversed(KEEP_TABLES):
        chunks.append(f"DELETE FROM {table};\n")
    for table in KEEP_TABLES:
        cur.execute(f"SELECT * FROM {table}")
        cols = [desc.name for desc in cur.description]
        col_sql = ", ".join(f'"{c}"' for c in cols)
        for row in cur.fetchall():
            placeholders = ", ".join(["%s"] * len(row))
            mog = cur.mogrify(
                f'INSERT INTO {table} ({col_sql}) VALUES ({placeholders});\n',
                row,
            )
            chunks.append(mog.decode() if isinstance(mog, bytes) else mog)
    chunks.append("COMMIT;\n")
    dest.write_text("".join(chunks))
    conn.close()


def restore_settings(kw: dict[str, str | int], src: Path) -> None:
    sql = src.read_text()
    conn = connect(kw)
    cur = conn.cursor()
    cur.execute(sql)
    conn.close()


def truncate_business(kw: dict[str, str | int]) -> list[str]:
    conn = connect(kw)
    cur = conn.cursor()
    cur.execute(
        """
        SELECT tablename FROM pg_tables
        WHERE schemaname = 'public'
        ORDER BY 1
        """
    )
    tables = [r[0] for r in cur.fetchall()]
    drop = [t for t in tables if t not in KEEP_TABLES]
    if drop:
        quoted = ", ".join(f'"{t}"' for t in drop)
        cur.execute(f"TRUNCATE {quoted} RESTART IDENTITY CASCADE")
    conn.close()
    return drop


def seed_from_env(kw: dict[str, str | int]) -> str | None:
    base_url = (os.environ.get("AGENT_PROVIDER_BASE_URL") or "").strip()
    api_key = (os.environ.get("AGENT_PROVIDER_API_KEY") or "").strip()
    model = (os.environ.get("AGENT_PROVIDER_MODEL") or "").strip()
    if not base_url or not api_key or not model:
        return None
    conn = connect(kw)
    cur = conn.cursor()
    cur.execute(
        """
        SELECT COUNT(*) FROM provider_profiles
        WHERE archived_at IS NULL AND COALESCE(api_key, '') <> ''
        """
    )
    if cur.fetchone()[0]:
        conn.close()
        return "already_present"
    profile_id = str(uuid.uuid4())
    caps = json.dumps(["text_responses", "image_responses", "image_images"])
    models = json.dumps({"prompt": model, "agent": model})
    cur.execute(
        """
        INSERT INTO provider_profiles (
            id, name, provider_type, base_url, api_key,
            capabilities_json, default_models_json, config_json,
            enabled, archived_at, created_at, updated_at
        ) VALUES (
            %s, %s, 'openai_compatible', %s, %s,
            %s, %s, '{}',
            TRUE, NULL, NOW(), NOW()
        )
        """,
        (profile_id, "本地网关", base_url, api_key, caps, models),
    )
    settings = json.dumps({"model": model})
    agent_cfg: dict[str, str] = {}
    for key, env_key in (
        ("reasoning_effort", "AGENT_PROVIDER_REASONING_EFFORT"),
        ("reasoning_summary", "AGENT_PROVIDER_REASONING_SUMMARY"),
        ("text_verbosity", "AGENT_PROVIDER_TEXT_VERBOSITY"),
        ("service_tier", "AGENT_PROVIDER_SERVICE_TIER"),
    ):
        value = (os.environ.get(env_key) or "").strip()
        if value:
            agent_cfg[key] = value
    for purpose, kind, cfg in (
        ("prompt", "openai", {}),
        ("agent", "openai", agent_cfg),
    ):
        cur.execute(
            """
            UPDATE provider_bindings
            SET provider_kind = %s,
                provider_profile_id = %s,
                model_settings_json = %s,
                config_json = %s,
                updated_at = NOW()
            WHERE purpose = %s
            """,
            (kind, profile_id, settings, json.dumps(cfg), purpose),
        )
        if cur.rowcount == 0:
            cur.execute(
                """
                INSERT INTO provider_bindings (
                    id, purpose, provider_kind, provider_profile_id,
                    model_settings_json, config_json, created_at, updated_at
                ) VALUES (%s, %s, %s, %s, %s, %s, NOW(), NOW())
                """,
                (str(uuid.uuid4()), purpose, kind, profile_id, settings, json.dumps(cfg)),
            )
    conn.close()
    return profile_id


def wipe_files() -> None:
    root = repo_root()
    for path in (
        root / "agent-service" / "data" / "runs",
        root / "agent-service" / "data" / "sessions",
        root / "agent-service" / "data" / "workspaces",
        root / "agent-service" / "data" / "conversations",
    ):
        if path.exists():
            shutil.rmtree(path)
    storage = Path(os.environ.get("STORAGE_ROOT", str(root / "storage-dev")))
    if not storage.is_absolute():
        storage = root / storage
    media = storage / "media"
    if media.exists():
        shutil.rmtree(media)
    media.mkdir(parents=True, exist_ok=True)
    logs = storage / "logs"
    logs.mkdir(parents=True, exist_ok=True)
    for log in logs.glob("productflow-*.log*"):
        log.unlink(missing_ok=True)


def flush_redis() -> None:
    url = os.environ.get("REDIS_URL")
    if not url:
        return
    host = urlparse(url).hostname or ""
    require_local_host("REDIS_URL", host)
    subprocess.run(["redis-cli", "-u", url, "FLUSHDB"], check=False, capture_output=True)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--seed-from-env",
        action="store_true",
        help="If no real provider profile exists, create one from AGENT_PROVIDER_*",
    )
    parser.add_argument(
        "--skip-files",
        action="store_true",
        help="Do not delete Agent snapshots or storage-dev/media",
    )
    parser.add_argument(
        "--yes",
        action="store_true",
        help="Required to truncate business tables (seed-from-env does not need this)",
    )
    args = parser.parse_args()
    raw = os.environ.get("DATABASE_URL")
    if not raw:
        print("DATABASE_URL is required", file=sys.stderr)
        return 1
    _, kw = postgres_dsn(raw)
    require_local_host("DATABASE_URL", str(kw["host"]))
    dump_path = settings_dump_path()
    if args.seed_from_env:
        seeded = seed_from_env(kw)
        print(f"seed_from_env={seeded}")
        dump_settings(kw, dump_path)
        print(f"settings dumped to {dump_path}")
        return 0
    if not args.yes:
        print("refusing to truncate without --yes", file=sys.stderr)
        return 2
    dump_settings(kw, dump_path)
    print(f"settings dumped to {dump_path}")
    dropped = truncate_business(kw)
    print(f"truncated {len(dropped)} tables; kept {', '.join(KEEP_TABLES)}")
    flush_redis()
    if not args.skip_files:
        wipe_files()
        print("redis FLUSHDB; agent snapshots and media removed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
