from __future__ import annotations

import json
import os
from collections.abc import Iterator
from contextlib import contextmanager
from pathlib import Path
from uuid import uuid4

import pytest
import sqlalchemy as sa
from alembic.config import Config
from sqlalchemy.engine import URL, make_url
from test_migrations_database_constraints import _insert_gallery_migration_fixture

from alembic import command
from productflow_backend.config import get_settings

LIVE_GALLERY_MIGRATION_SWITCH = "PRODUCTFLOW_RUN_LIVE_GALLERY_MIGRATION"

pytestmark = [
    pytest.mark.live_dependencies,
    pytest.mark.skipif(
        os.getenv(LIVE_GALLERY_MIGRATION_SWITCH) != "1",
        reason=f"set {LIVE_GALLERY_MIGRATION_SWITCH}=1 to run the PostgreSQL gallery migration gate",
    ),
]


@contextmanager
def _temporary_postgres_database(base_database_url: str) -> Iterator[URL]:
    try:
        base_url = make_url(base_database_url)
    except Exception:
        pytest.fail("DATABASE_URL must be a valid SQLAlchemy URL", pytrace=False)
    if base_url.get_backend_name() != "postgresql":
        pytest.fail("DATABASE_URL must point to PostgreSQL for the live gallery migration gate", pytrace=False)

    database_name = f"productflow_live_gallery_{uuid4().hex}"
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
                    assert connection.scalar(
                        sa.text("SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = :database_name)"),
                        {"database_name": database_name},
                    ) is False
        finally:
            maintenance_engine.dispose()


def test_product_gallery_explorer_migration_round_trips_postgresql(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    base_database_url = os.getenv("DATABASE_URL", "").strip()
    if not base_database_url:
        pytest.fail("DATABASE_URL must be provided by the development environment", pytrace=False)

    with _temporary_postgres_database(base_database_url) as database_url:
        with monkeypatch.context() as environment:
            environment.setenv("DATABASE_URL", database_url.render_as_string(hide_password=False))
            environment.setenv("STORAGE_ROOT", str(tmp_path / "storage"))
            get_settings.cache_clear()
            backend_dir = Path(__file__).resolve().parents[1]
            config = Config(str(backend_dir / "alembic.ini"))
            config.set_main_option("script_location", str(backend_dir / "alembic"))

            command.upgrade(config, "20260812_0034")
            engine = sa.create_engine(database_url, future=True)
            with engine.begin() as connection:
                _insert_gallery_migration_fixture(connection)
            engine.dispose()

            command.upgrade(config, "20260812_0035")
            engine = sa.create_engine(database_url, future=True)
            inspector = sa.inspect(engine)
            assert "product_asset_folders" in inspector.get_table_names()
            assert "uq_workflow_image_generation_records_result_asset_id" in {
                item["name"] for item in inspector.get_unique_constraints("workflow_image_generation_records")
            }
            foreign_keys = {
                tuple(item["constrained_columns"]): item
                for item in inspector.get_foreign_keys("product_image_assets")
            }
            assert foreign_keys[("user_folder_id",)]["options"]["ondelete"] == "SET NULL"
            with engine.connect() as connection:
                assert connection.scalar(
                    sa.text("SELECT image_type_key FROM product_image_assets WHERE id = 'asset-generated'")
                ) == "hero"
                prepared_json = connection.scalar(
                    sa.text("SELECT prepared_json FROM agent_tool_mutations WHERE id = 'mutation-gallery'")
                )
                if isinstance(prepared_json, str):
                    prepared_json = json.loads(prepared_json)
                assert prepared_json["operation"] == "rename_asset"
            engine.dispose()

            command.downgrade(config, "20260812_0034")
            engine = sa.create_engine(database_url, future=True)
            assert "product_asset_folders" not in sa.inspect(engine).get_table_names()
            with engine.connect() as connection:
                assert connection.scalar(sa.text("SELECT COUNT(*) FROM media_objects")) == 2
            engine.dispose()

            command.upgrade(config, "20260812_0035")
            engine = sa.create_engine(database_url, future=True)
            with engine.connect() as connection:
                assert connection.scalar(
                    sa.text("SELECT image_type_key FROM product_image_assets WHERE id = 'asset-generated'")
                ) == "hero"
                assert connection.scalar(sa.text("SELECT COUNT(*) FROM media_objects")) == 2
            engine.dispose()
            get_settings.cache_clear()
