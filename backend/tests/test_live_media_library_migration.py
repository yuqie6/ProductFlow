from __future__ import annotations

import os
from collections.abc import Iterator
from concurrent.futures import ThreadPoolExecutor
from contextlib import contextmanager
from datetime import UTC, datetime
from hashlib import sha256
from pathlib import Path
from threading import Barrier
from uuid import uuid4

import pytest
import sqlalchemy as sa
from alembic.config import Config
from sqlalchemy.engine import URL, make_url

from alembic import command
from productflow_backend.application.media_library.backfill import (
    capture_gallery_snapshot,
    run_gallery_backfill,
    verify_gallery_backfill,
)
from productflow_backend.application.media_library.service import (
    collect_media_library_asset_to_product,
    save_media_library_asset_from_product,
)
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import ImageSessionAssetKind, MediaVerificationStatus
from productflow_backend.infrastructure.db.models import (
    ImageGalleryEntry,
    ImageSession,
    ImageSessionAsset,
    MediaLibraryAsset,
    MediaObject,
    Product,
    ProductImageAsset,
)
from productflow_backend.infrastructure.db.session import get_engine, get_session_factory
from productflow_backend.infrastructure.storage import LocalStorage

LIVE_MEDIA_LIBRARY_MIGRATION_SWITCH = "PRODUCTFLOW_RUN_LIVE_MEDIA_LIBRARY_MIGRATION"

pytestmark = [
    pytest.mark.live_dependencies,
    pytest.mark.skipif(
        os.getenv(LIVE_MEDIA_LIBRARY_MIGRATION_SWITCH) != "1",
        reason=(
            f"set {LIVE_MEDIA_LIBRARY_MIGRATION_SWITCH}=1 "
            "to run the PostgreSQL media library migration gate"
        ),
    ),
]


@contextmanager
def _temporary_postgres_database(base_database_url: str) -> Iterator[URL]:
    base_url = make_url(base_database_url)
    if base_url.get_backend_name() != "postgresql":
        pytest.fail("DATABASE_URL must point to PostgreSQL for the media library gate", pytrace=False)
    database_name = f"productflow_live_media_library_{uuid4().hex}"
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
    try:
        if cached_engine is not None:
            cached_engine.dispose()
    finally:
        get_engine.cache_clear()
        get_settings.cache_clear()


def test_live_media_library_backfill_on_postgresql(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    base_database_url = os.getenv("DATABASE_URL", "").strip()
    if not base_database_url:
        pytest.fail("DATABASE_URL must be provided by the development environment", pytrace=False)

    with _temporary_postgres_database(base_database_url) as database_url:
        with monkeypatch.context() as environment:
            storage_root = tmp_path / "storage"
            media_path = storage_root / "media" / "live-library.png"
            media_path.parent.mkdir(parents=True)
            media_path.write_bytes(b"live library media")
            environment.setenv("DATABASE_URL", database_url.render_as_string(hide_password=False))
            environment.setenv("STORAGE_ROOT", str(storage_root))
            environment.setenv("ADMIN_ACCESS_KEY", "live-media-library-admin-key")
            environment.setenv("SESSION_SECRET", "live-media-library-session-secret")
            _reset_database_state()

            backend_dir = Path(__file__).resolve().parents[1]
            config = Config(str(backend_dir / "alembic.ini"))
            config.set_main_option("script_location", str(backend_dir / "alembic"))
            command.upgrade(config, "head")
            _reset_database_state()

            session_factory = get_session_factory()
            with session_factory() as session:
                image_session = ImageSession(title="Live library backfill")
                media = MediaObject(
                    storage_path="media/live-library.png",
                    mime_type="image/png",
                    byte_size=len(b"live library media"),
                    width=1,
                    height=1,
                    sha256=sha256(b"live library media").hexdigest(),
                    verification_status=MediaVerificationStatus.VERIFIED,
                    verified_at=datetime.now(UTC),
                )
                session.add(image_session)
                session.flush()
                asset = ImageSessionAsset(
                    session=image_session,
                    kind=ImageSessionAssetKind.GENERATED_IMAGE,
                    original_filename="live-library.png",
                    mime_type="image/png",
                    storage_path="media/live-library.png",
                    media_object=media,
                )
                session.add(asset)
                session.flush()
                entry = ImageGalleryEntry(
                    id="live-gallery-entry",
                    image_session_asset_id=asset.id,
                )
                session.add(entry)
                session.commit()
                entry_id = entry.id

            with session_factory() as snapshot_session:
                snapshot = capture_gallery_snapshot(snapshot_session)
            assert snapshot.gallery_count == 1
            with session_factory() as session:
                summary = run_gallery_backfill(
                    session,
                    storage=LocalStorage(root=storage_root),
                    apply=True,
                )
                assert summary.created == 1
                verified = verify_gallery_backfill(
                    session,
                    storage=LocalStorage(root=storage_root),
                    snapshot=snapshot,
                )
                assert verified == 1
                session.expire_all()
                library_asset = session.get(MediaLibraryAsset, entry_id)
                assert library_asset is not None
                assert library_asset.source_type == "legacy_gallery"


def test_live_collect_library_asset_to_product_is_concurrency_safe(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    base_database_url = os.getenv("DATABASE_URL", "").strip()
    if not base_database_url:
        pytest.fail("DATABASE_URL must be provided by the development environment", pytrace=False)

    with _temporary_postgres_database(base_database_url) as database_url:
        with monkeypatch.context() as environment:
            storage_root = tmp_path / "storage"
            environment.setenv("DATABASE_URL", database_url.render_as_string(hide_password=False))
            environment.setenv("STORAGE_ROOT", str(storage_root))
            environment.setenv("ADMIN_ACCESS_KEY", "live-collect-admin-key")
            environment.setenv("SESSION_SECRET", "live-collect-session-secret")
            _reset_database_state()

            backend_dir = Path(__file__).resolve().parents[1]
            config = Config(str(backend_dir / "alembic.ini"))
            config.set_main_option("script_location", str(backend_dir / "alembic"))
            command.upgrade(config, "head")
            _reset_database_state()

            session_factory = get_session_factory()
            with session_factory() as session:
                product = Product(name="Live collect product")
                media = MediaObject(
                    storage_path="media/collect.png",
                    mime_type="image/png",
                    byte_size=4,
                    width=1,
                    height=1,
                    sha256=sha256(b"data").hexdigest(),
                    verification_status=MediaVerificationStatus.VERIFIED,
                    verified_at=datetime.now(UTC),
                )
                session.add(product)
                session.flush()
                source_asset = ProductImageAsset(
                    product=product,
                    media_object=media,
                    origin_type="upload",
                    display_name="collect.png",
                    original_filename="collect.png",
                )
                session.add(source_asset)
                session.commit()
                product_id = product.id
                library_asset = save_media_library_asset_from_product(
                    session,
                    product_image_asset_id=source_asset.id,
                ).asset
                library_asset_id = library_asset.id

            barrier = Barrier(2)

            def collect_once() -> None:
                barrier.wait(timeout=5)
                with session_factory() as session:
                    collect_media_library_asset_to_product(
                        session,
                        product_id=product_id,
                        library_asset_id=library_asset_id,
                    )

            with ThreadPoolExecutor(max_workers=2) as executor:
                futures = [executor.submit(collect_once), executor.submit(collect_once)]
                for future in futures:
                    future.result(timeout=10)

            with session_factory() as session:
                count = session.scalar(
                    sa.select(sa.func.count())
                    .select_from(ProductImageAsset)
                    .where(ProductImageAsset.source_library_asset_id == library_asset_id)
                )
                assert count == 1
