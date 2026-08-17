from __future__ import annotations

import os
from collections.abc import Iterator
from contextlib import contextmanager
from pathlib import Path
from uuid import uuid4

import pytest
import sqlalchemy as sa
from alembic.config import Config
from sqlalchemy.engine import URL, make_url
from sqlalchemy.orm import Session

from alembic import command
from productflow_backend.application.image_sessions import delete_image_session
from productflow_backend.config import get_settings
from productflow_backend.infrastructure.storage import LocalStorage

LIVE_IMAGE_SESSION_MEDIA_MIGRATION_SWITCH = "PRODUCTFLOW_RUN_LIVE_IMAGE_SESSION_MEDIA_MIGRATION"

pytestmark = [
    pytest.mark.live_dependencies,
    pytest.mark.skipif(
        os.getenv(LIVE_IMAGE_SESSION_MEDIA_MIGRATION_SWITCH) != "1",
        reason=(
            f"set {LIVE_IMAGE_SESSION_MEDIA_MIGRATION_SWITCH}=1 "
            "to run the PostgreSQL ImageSession media migration gate"
        ),
    ),
]


@contextmanager
def _temporary_postgres_database(base_database_url: str) -> Iterator[URL]:
    base_url = make_url(base_database_url)
    if base_url.get_backend_name() != "postgresql":
        pytest.fail("DATABASE_URL must point to PostgreSQL for the migration gate", pytrace=False)

    database_name = f"productflow_live_session_media_{uuid4().hex}"
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


def test_image_session_media_authority_migration_on_postgresql(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    base_database_url = os.getenv("DATABASE_URL", "").strip()
    if not base_database_url:
        pytest.fail("DATABASE_URL must be provided by the development environment", pytrace=False)

    with _temporary_postgres_database(base_database_url) as database_url:
        with monkeypatch.context() as environment:
            storage_root = tmp_path / "storage"
            shared_media_path = storage_root / "media" / "live-session.png"
            shared_media_path.parent.mkdir(parents=True)
            shared_media_path.write_bytes(b"shared media remains product-owned")
            environment.setenv("DATABASE_URL", database_url.render_as_string(hide_password=False))
            environment.setenv("STORAGE_ROOT", str(storage_root))
            environment.setenv("ADMIN_ACCESS_KEY", "live-migration-admin-key")
            environment.setenv("SESSION_SECRET", "live-migration-session-secret")
            get_settings.cache_clear()

            backend_dir = Path(__file__).resolve().parents[1]
            config = Config(str(backend_dir / "alembic.ini"))
            config.set_main_option("script_location", str(backend_dir / "alembic"))
            command.upgrade(config, "20260816_0042")

            engine = sa.create_engine(database_url, future=True)
            try:
                with engine.begin() as connection:
                    connection.execute(
                        sa.text(
                            "INSERT INTO image_sessions (id, title, created_at, updated_at) VALUES "
                            "('session-live-media', 'Live media migration', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)"
                        )
                    )
                    connection.execute(
                        sa.text(
                            "INSERT INTO media_objects "
                            "(id, storage_path, mime_type, verification_status, created_at) "
                            "VALUES ('media-live-session', 'media/live-session.png', 'image/png', "
                            "'legacy_pending', CURRENT_TIMESTAMP)"
                        )
                    )
                    connection.execute(
                        sa.text(
                            "INSERT INTO image_session_assets "
                            "(id, session_id, kind, original_filename, mime_type, storage_path, "
                            "media_object_id, created_at) VALUES "
                            "('asset-live-session', 'session-live-media', 'reference_upload', "
                            "'reference.png', 'image/jpeg', 'media/stale-carrier.jpg', "
                            "'media-live-session', CURRENT_TIMESTAMP)"
                        )
                    )
                    connection.execute(
                        sa.text(
                            "INSERT INTO products (id, name, created_at, updated_at) VALUES "
                            "('product-live-session', 'Shared media product', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)"
                        )
                    )
                    connection.execute(
                        sa.text(
                            "INSERT INTO product_image_assets "
                            "(id, product_id, media_object_id, origin_type, display_name, original_filename, "
                            "source_image_session_asset_id, created_at, updated_at) VALUES "
                            "('product-asset-live-session', 'product-live-session', 'media-live-session', "
                            "'image_session_attach', 'Shared reference', 'reference.png', "
                            "'asset-live-session', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)"
                        )
                    )
            finally:
                engine.dispose()

            with pytest.raises(RuntimeError, match="carrier drift"):
                command.upgrade(config, "head")

            engine = sa.create_engine(database_url, future=True)
            try:
                pre_repair_inspector = sa.inspect(engine)
                pre_repair_column = next(
                    column
                    for column in pre_repair_inspector.get_columns("image_session_assets")
                    if column["name"] == "media_object_id"
                )
                assert pre_repair_column["nullable"] is True
                with engine.begin() as connection:
                    assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == "20260816_0042"
                    assert connection.execute(
                        sa.text(
                            "SELECT storage_path, mime_type FROM image_session_assets "
                            "WHERE id = 'asset-live-session'"
                        )
                    ).one() == ("media/stale-carrier.jpg", "image/jpeg")
                    connection.execute(
                        sa.text(
                            "UPDATE image_session_assets SET storage_path = 'media/live-session.png', "
                            "mime_type = 'image/png' WHERE id = 'asset-live-session'"
                        )
                    )
            finally:
                engine.dispose()

            command.upgrade(config, "head")

            engine = sa.create_engine(database_url, future=True)
            try:
                inspector = sa.inspect(engine)
                media_column = next(
                    column
                    for column in inspector.get_columns("image_session_assets")
                    if column["name"] == "media_object_id"
                )
                assert media_column["nullable"] is False
                assert {
                    foreign_key["name"]
                    for foreign_key in inspector.get_foreign_keys("image_session_assets")
                } >= {"fk_image_session_assets_media_object_id"}
                with engine.connect() as connection:
                    assert connection.scalar(sa.text("SELECT version_num FROM alembic_version")) == "20260817_0050"
                    assert connection.scalar(
                        sa.text(
                            "SELECT media_object_id FROM image_session_assets "
                            "WHERE id = 'asset-live-session'"
                        )
                    ) == "media-live-session"

                with Session(engine) as session:
                    delete_image_session(
                        session,
                        image_session_id="session-live-media",
                        storage=LocalStorage(root=storage_root),
                    )
                with engine.connect() as connection:
                    assert connection.scalar(
                        sa.text("SELECT COUNT(*) FROM image_sessions WHERE id = 'session-live-media'")
                    ) == 0
                    assert connection.scalar(
                        sa.text("SELECT COUNT(*) FROM media_objects WHERE id = 'media-live-session'")
                    ) == 1
                    assert connection.scalar(
                        sa.text(
                            "SELECT source_image_session_asset_id FROM product_image_assets "
                            "WHERE id = 'product-asset-live-session'"
                        )
                    ) is None
                assert shared_media_path.exists()
            finally:
                engine.dispose()
                get_settings.cache_clear()
