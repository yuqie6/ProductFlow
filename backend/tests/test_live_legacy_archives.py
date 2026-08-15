from __future__ import annotations

import os
from collections.abc import Iterator
from contextlib import contextmanager
from datetime import UTC, datetime
from pathlib import Path
from uuid import uuid4

import pytest
import sqlalchemy as sa
from alembic.config import Config
from sqlalchemy.engine import URL, make_url
from sqlalchemy.exc import IntegrityError

from alembic import command
from productflow_backend.application.legacy_retirement.audit import (
    CURRENT_ARCHIVE_PROFILE,
    audit_legacy_retirement,
)
from productflow_backend.application.legacy_retirement.contracts import canonical_sha256
from productflow_backend.config import get_settings
from productflow_backend.infrastructure.db.session import get_engine, get_session_factory

LIVE_LEGACY_ARCHIVE_SWITCH = "PRODUCTFLOW_RUN_LIVE_LEGACY_ARCHIVES"

pytestmark = [
    pytest.mark.live_dependencies,
    pytest.mark.skipif(
        os.getenv(LIVE_LEGACY_ARCHIVE_SWITCH) != "1",
        reason=f"set {LIVE_LEGACY_ARCHIVE_SWITCH}=1 to run the PostgreSQL legacy archive gate",
    ),
]


@contextmanager
def _temporary_postgres_database(base_database_url: str) -> Iterator[URL]:
    base_url = make_url(base_database_url)
    if base_url.get_backend_name() != "postgresql":
        pytest.fail("DATABASE_URL must point to PostgreSQL for the legacy archive gate", pytrace=False)

    database_name = f"productflow_live_legacy_archive_{uuid4().hex}"
    maintenance_engine = sa.create_engine(
        base_url.set(database="postgres"),
        future=True,
        isolation_level="AUTOCOMMIT",
        pool_pre_ping=True,
    )
    quoted_name = maintenance_engine.dialect.identifier_preparer.quote(database_name)
    created = False
    try:
        with maintenance_engine.connect() as connection:
            connection.exec_driver_sql(f"CREATE DATABASE {quoted_name} TEMPLATE template0")
        created = True
        yield base_url.set(database=database_name)
    finally:
        try:
            if created:
                with maintenance_engine.connect() as connection:
                    connection.execute(
                        sa.text(
                            "SELECT pg_terminate_backend(pid) FROM pg_stat_activity "
                            "WHERE datname = :database_name AND pid <> pg_backend_pid()"
                        ),
                        {"database_name": database_name},
                    ).all()
                    connection.exec_driver_sql(f"DROP DATABASE IF EXISTS {quoted_name}")
        finally:
            maintenance_engine.dispose()


def _reset_database_state() -> None:
    cached_engine = get_engine() if get_engine.cache_info().currsize else None
    get_session_factory.cache_clear()
    if cached_engine is not None:
        cached_engine.dispose()
    get_engine.cache_clear()
    get_settings.cache_clear()


def test_legacy_archive_contract_on_postgresql(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    base_database_url = os.getenv("DATABASE_URL", "").strip()
    if not base_database_url:
        pytest.fail("DATABASE_URL must be provided by the development environment", pytrace=False)

    with _temporary_postgres_database(base_database_url) as database_url:
        with monkeypatch.context() as environment:
            storage_root = tmp_path / "storage"
            storage_root.mkdir()
            environment.setenv("DATABASE_URL", database_url.render_as_string(hide_password=False))
            environment.setenv("STORAGE_ROOT", str(storage_root))
            _reset_database_state()

            backend_dir = Path(__file__).resolve().parents[1]
            config = Config(str(backend_dir / "alembic.ini"))
            config.set_main_option("script_location", str(backend_dir / "alembic"))
            command.upgrade(config, "head")

            engine = sa.create_engine(database_url, future=True)
            now = datetime(2026, 8, 15, 8, 0, tzinfo=UTC)
            payload = {"schema_version": 1, "nodes": [], "edges": [], "runs": []}
            with engine.begin() as connection:
                connection.execute(
                    sa.text(
                        "INSERT INTO products (id, name, created_at, updated_at) "
                        "VALUES ('product-live-archive', 'PostgreSQL 归档商品', :now, :now)"
                    ),
                    {"now": now},
                )
                connection.execute(
                    sa.text(
                        "INSERT INTO media_objects "
                        "(id, storage_path, mime_type, verification_status, created_at) "
                        "VALUES ('media-live-archive', 'media/live-archive.png', 'image/png', "
                        "'legacy_pending', :now)"
                    ),
                    {"now": now},
                )
                connection.execute(
                    sa.text(
                        "INSERT INTO product_image_assets "
                        "(id, product_id, media_object_id, origin_type, display_name, original_filename, "
                        "created_at, updated_at) VALUES "
                        "('asset-live-archive', 'product-live-archive', 'media-live-archive', 'upload', "
                        "'归档参考图', 'live-archive.png', :now, :now)"
                    ),
                    {"now": now},
                )
                connection.execute(
                    sa.text(
                        "INSERT INTO legacy_workflow_archives "
                        "(id, source_profile, legacy_workflow_id, product_id, source_title, source_updated_at, "
                        "archive_schema_version, payload_json, source_fingerprint_sha256, payload_sha256, "
                        "node_count, edge_count, run_count, node_run_count, asset_count, created_at) VALUES "
                        "('archive-live', 'legacy_canvas_agent_20260518_0032', 'workflow-live-old', "
                        "'product-live-archive', 'PostgreSQL 旧工作流', :now, 1, CAST(:payload AS JSON), "
                        ":source_hash, :payload_hash, 0, 0, 0, 0, 1, :now)"
                    ),
                    {
                        "now": now,
                        "payload": '{"edges":[],"nodes":[],"runs":[],"schema_version":1}',
                        "source_hash": "a" * 64,
                        "payload_hash": canonical_sha256(payload),
                    },
                )
                connection.execute(
                    sa.text(
                        "INSERT INTO legacy_workflow_archive_assets "
                        "(id, archive_id, product_image_asset_id, role, legacy_source_type, legacy_source_id, "
                        "created_at) VALUES ('archive-live-asset', 'archive-live', 'asset-live-archive', "
                        "'reference', 'source_asset', 'source-live-old', :now)"
                    ),
                    {"now": now},
                )

            report = audit_legacy_retirement(engine, storage_root=storage_root, generated_at=now)
            assert report.source.schema_profile == CURRENT_ARCHIVE_PROFILE
            assert report.source.read_only_enforced is True
            assert report.source.table_counts["legacy_workflow_archives"] == 1
            assert {key: value for key, value in report.integrity_counts.items() if value} == {}
            assert report.ready_for_archive is True

            with pytest.raises(IntegrityError):
                with engine.begin() as connection:
                    connection.execute(
                        sa.text("DELETE FROM product_image_assets WHERE id = 'asset-live-archive'")
                    )
            engine.dispose()
            _reset_database_state()

        _reset_database_state()
