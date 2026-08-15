from __future__ import annotations

import logging
from collections.abc import Iterator
from contextlib import contextmanager
from pathlib import Path

import pytest
from helpers import _make_demo_image_bytes

from productflow_backend.application.image_sessions import (
    add_image_session_reference_images,
    create_image_session,
    delete_image_session,
    delete_image_session_reference_image,
)
from productflow_backend.application.use_cases import (
    add_canonical_product_images,
    create_canonical_product,
    delete_product,
)
from productflow_backend.domain.enums import ImageSessionAssetKind
from productflow_backend.infrastructure.db.models import ImageSessionAsset, MediaObject, Product, ProductImageAsset
from productflow_backend.infrastructure.storage import LocalStorage


@contextmanager
def _capture_storage_logs(caplog: pytest.LogCaptureFixture) -> Iterator[None]:
    logger = logging.getLogger("productflow_backend.application.storage_compensation")
    attached = caplog.handler not in logger.handlers
    was_disabled = logger.disabled
    if attached:
        logger.addHandler(caplog.handler)
    logger.disabled = False
    try:
        with caplog.at_level(logging.ERROR, logger=logger.name):
            yield
    finally:
        logger.disabled = was_disabled
        if attached:
            logger.removeHandler(caplog.handler)


class FailingStorage:
    def __init__(self, root: Path) -> None:
        self.inner = LocalStorage(root)
        self.fail_save_at: int | None = None
        self.fail_delete = False
        self.save_calls = 0
        self.saved_paths: list[str] = []
        self.deleted_paths: list[str] = []

    def __getattr__(self, name: str):
        return getattr(self.inner, name)

    def save_media_image(self, media_id: str, filename: str, content: bytes) -> str:
        self.save_calls += 1
        if self.fail_save_at == self.save_calls:
            raise OSError("injected save failure")
        path = self.inner.save_media_image(media_id, filename, content)
        self.saved_paths.append(path)
        return path

    def delete_image_with_variants(self, relative_path: str) -> None:
        self.deleted_paths.append(relative_path)
        if self.fail_delete:
            raise OSError("injected delete failure")
        self.inner.delete_image_with_variants(relative_path)


def _assert_saved_files_are_gone(storage: FailingStorage) -> None:
    assert storage.deleted_paths == list(reversed(storage.saved_paths))
    assert all(not storage.inner.resolve(path).exists() for path in storage.saved_paths)


def _create_product(db_session, *, storage=None):
    return create_canonical_product(
        db_session,
        name="补偿测试商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "product.png", "image/png")],
        storage=storage,
    )


def test_product_create_cleans_files_when_later_save_fails(configured_env: Path, db_session) -> None:
    storage = FailingStorage(configured_env)
    storage.fail_save_at = 2

    with pytest.raises(OSError, match="injected save failure"):
        create_canonical_product(
            db_session,
            name="第二张失败",
            category=None,
            price=None,
            source_note=None,
            image_uploads=[
                (_make_demo_image_bytes(), "first.png", "image/png"),
                (_make_demo_image_bytes(), "second.png", "image/png"),
            ],
            storage=storage,
        )

    assert db_session.query(Product).count() == 0
    assert db_session.query(ProductImageAsset).count() == 0
    assert db_session.query(MediaObject).count() == 0
    _assert_saved_files_are_gone(storage)


def test_product_image_commit_failure_rolls_back_and_cleans_file(
    configured_env: Path,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    product = _create_product(db_session)
    storage = FailingStorage(configured_env)
    original_asset_count = db_session.query(ProductImageAsset).count()
    original_media_count = db_session.query(MediaObject).count()

    monkeypatch.setattr(db_session, "commit", lambda: (_ for _ in ()).throw(RuntimeError("injected commit failure")))
    with pytest.raises(RuntimeError, match="injected commit failure"):
        add_canonical_product_images(
            db_session,
            product_id=product.id,
            image_uploads=[(_make_demo_image_bytes(), "reference.png", "image/png")],
            storage=storage,
        )

    assert db_session.query(ProductImageAsset).count() == original_asset_count
    assert db_session.query(MediaObject).count() == original_media_count
    _assert_saved_files_are_gone(storage)


def test_image_session_reference_commit_failure_cleans_files(
    configured_env: Path,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    image_session = create_image_session(db_session, title="参考图失败")
    storage = FailingStorage(configured_env)
    monkeypatch.setattr(db_session, "commit", lambda: (_ for _ in ()).throw(RuntimeError("injected commit failure")))

    with pytest.raises(RuntimeError, match="injected commit failure"):
        add_image_session_reference_images(
            db_session,
            image_session_id=image_session.id,
            reference_image_uploads=[
                (_make_demo_image_bytes(), "reference-1.png", "image/png"),
                (_make_demo_image_bytes(), "reference-2.png", "image/png"),
            ],
            storage=storage,
        )

    assert db_session.query(ImageSessionAsset).filter_by(session_id=image_session.id).count() == 0
    _assert_saved_files_are_gone(storage)


def test_storage_delete_failures_keep_committed_database_results(
    configured_env: Path,
    db_session,
    caplog: pytest.LogCaptureFixture,
) -> None:
    product = _create_product(db_session)
    failing_storage = FailingStorage(configured_env)
    failing_storage.fail_delete = True

    with _capture_storage_logs(caplog):
        delete_product(db_session, product_id=product.id, storage=failing_storage)

    assert db_session.get(Product, product.id) is None
    assert f"product_id={product.id}" in caplog.text


def test_image_session_delete_failures_keep_committed_database_results(
    configured_env: Path,
    db_session,
    caplog: pytest.LogCaptureFixture,
) -> None:
    image_session = create_image_session(db_session, title="删除失败")
    added = add_image_session_reference_images(
        db_session,
        image_session_id=image_session.id,
        reference_image_uploads=[(_make_demo_image_bytes(), "reference.png", "image/png")],
    )
    reference_asset = next(asset for asset in added.assets if asset.kind == ImageSessionAssetKind.REFERENCE_UPLOAD)
    failing_storage = FailingStorage(configured_env)
    failing_storage.fail_delete = True

    with _capture_storage_logs(caplog):
        delete_image_session_reference_image(
            db_session,
            image_session_id=image_session.id,
            asset_id=reference_asset.id,
            storage=failing_storage,
        )

    assert db_session.get(ImageSessionAsset, reference_asset.id) is None
    assert f"image_session_asset_id={reference_asset.id}" in caplog.text

    caplog.clear()
    add_image_session_reference_images(
        db_session,
        image_session_id=image_session.id,
        reference_image_uploads=[(_make_demo_image_bytes(), "remaining.png", "image/png")],
    )
    with _capture_storage_logs(caplog):
        delete_image_session(db_session, image_session_id=image_session.id, storage=failing_storage)

    assert db_session.get(type(image_session), image_session.id) is None
    assert "media_object_id=" in caplog.text


def test_local_storage_cleans_original_when_variant_warming_fails(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    storage = LocalStorage(configured_env)
    monkeypatch.setattr(
        storage,
        "_warm_image_variants",
        lambda relative_path: (_ for _ in ()).throw(RuntimeError("injected variant failure")),
    )

    with pytest.raises(RuntimeError, match="injected variant failure"):
        storage.save_media_image(
            "00000000-0000-0000-0000-000000000001",
            "product.png",
            _make_demo_image_bytes(),
        )

    assert not any(path.is_file() for path in (configured_env / "media").rglob("*"))
