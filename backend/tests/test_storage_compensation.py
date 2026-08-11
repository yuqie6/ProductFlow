from __future__ import annotations

import logging
from collections.abc import Iterator
from contextlib import contextmanager
from pathlib import Path

import pytest
from helpers import _make_demo_image_bytes

from productflow_backend.application.image_sessions import (
    add_image_session_reference_images,
    attach_image_session_asset_to_product,
    create_image_session,
    delete_image_session,
    delete_image_session_reference_image,
)
from productflow_backend.application.product_workflow.mutations import (
    bind_workflow_node_image,
    get_or_create_product_workflow,
    upload_workflow_node_image,
)
from productflow_backend.application.use_cases import (
    add_reference_images,
    create_product,
    delete_product,
    delete_reference_image,
)
from productflow_backend.domain.enums import (
    ImageSessionAssetKind,
    PosterKind,
    SourceAssetKind,
    WorkflowNodeStatus,
    WorkflowNodeType,
    WorkflowRunStatus,
)
from productflow_backend.infrastructure.db.models import (
    CopySet,
    ImageSessionAsset,
    PosterVariant,
    Product,
    SourceAsset,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRun,
)
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

    def _save(self, method_name: str, *args, **kwargs) -> str:
        self.save_calls += 1
        if self.fail_save_at == self.save_calls:
            raise OSError("injected save failure")
        path = getattr(self.inner, method_name)(*args, **kwargs)
        self.saved_paths.append(path)
        return path

    def save_product_upload(self, product_id: str, filename: str, content: bytes) -> str:
        return self._save("save_product_upload", product_id, filename, content)

    def save_reference_upload(self, product_id: str, filename: str, content: bytes) -> str:
        return self._save("save_reference_upload", product_id, filename, content)

    def save_generated_image(
        self,
        product_id: str,
        poster_kind: str,
        content: bytes,
        suffix: str = ".png",
    ) -> str:
        return self._save("save_generated_image", product_id, poster_kind, content, suffix=suffix)

    def save_media_image(self, media_id: str, filename: str, content: bytes) -> str:
        return self._save("save_media_image", media_id, filename, content)

    def save_image_session_reference(self, session_id: str, filename: str, content: bytes) -> str:
        return self._save("save_image_session_reference", session_id, filename, content)

    def save_image_session_generated(self, session_id: str, content: bytes, suffix: str = ".png") -> str:
        return self._save("save_image_session_generated", session_id, content, suffix=suffix)

    def delete_image_with_variants(self, relative_path: str) -> None:
        self.deleted_paths.append(relative_path)
        if self.fail_delete:
            raise OSError("injected delete failure")
        self.inner.delete_image_with_variants(relative_path)

    def delete_product_tree(self, product_id: str) -> None:
        if self.fail_delete:
            raise OSError("injected delete failure")
        self.inner.delete_product_tree(product_id)

    def delete_image_session_tree(self, session_id: str) -> None:
        if self.fail_delete:
            raise OSError("injected delete failure")
        self.inner.delete_image_session_tree(session_id)


def _assert_saved_files_are_gone(storage: FailingStorage) -> None:
    assert storage.deleted_paths == list(reversed(storage.saved_paths))
    assert all(not storage.inner.resolve(path).exists() for path in storage.saved_paths)


def _create_product(db_session):
    return create_product(
        db_session,
        name="补偿测试商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="product.png",
        content_type="image/png",
    )


def test_product_create_cleans_files_when_second_save_fails(configured_env: Path, db_session) -> None:
    storage = FailingStorage(configured_env)
    storage.fail_save_at = 3

    with pytest.raises(OSError, match="injected save failure"):
        create_product(
            db_session,
            name="第二张失败",
            category=None,
            price=None,
            source_note=None,
            image_bytes=_make_demo_image_bytes(),
            filename="product.png",
            content_type="image/png",
            reference_image_uploads=[
                (_make_demo_image_bytes(), "reference-1.png", "image/png"),
                (_make_demo_image_bytes(), "reference-2.png", "image/png"),
            ],
            storage=storage,
        )

    assert db_session.query(Product).count() == 0
    _assert_saved_files_are_gone(storage)


def test_product_create_cleans_files_when_template_initialization_fails(
    configured_env: Path,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    def fail_template(*args, **kwargs) -> None:
        raise RuntimeError("injected template failure")

    monkeypatch.setattr(
        "productflow_backend.application.use_cases.materialize_product_workflow_from_template",
        fail_template,
    )
    storage = FailingStorage(configured_env)

    with pytest.raises(RuntimeError, match="injected template failure"):
        create_product(
            db_session,
            name="模板失败",
            category=None,
            price=None,
            source_note=None,
            image_bytes=_make_demo_image_bytes(),
            filename="product.png",
            content_type="image/png",
            canvas_template_key="ecommerce-taobao-main-image-v1",
            storage=storage,
        )

    assert db_session.query(Product).count() == 0
    _assert_saved_files_are_gone(storage)


def test_reference_write_commit_failure_rolls_back_and_cleans_file(
    configured_env: Path,
    db_session,
    monkeypatch,
) -> None:
    product = _create_product(db_session)
    storage = FailingStorage(configured_env)

    def fail_commit() -> None:
        raise RuntimeError("injected commit failure")

    monkeypatch.setattr(db_session, "commit", fail_commit)
    with pytest.raises(RuntimeError, match="injected commit failure"):
        add_reference_images(
            db_session,
            product_id=product.id,
            reference_image_uploads=[(_make_demo_image_bytes(), "reference.png", "image/png")],
            storage=storage,
        )

    assert db_session.query(SourceAsset).filter_by(
        product_id=product.id,
        kind=SourceAssetKind.REFERENCE_IMAGE,
    ).count() == 0
    _assert_saved_files_are_gone(storage)


def test_workflow_upload_commit_failure_rolls_back_and_cleans_file(
    configured_env: Path,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    product = _create_product(db_session)
    workflow = get_or_create_product_workflow(db_session, product.id)
    reference_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.REFERENCE_IMAGE)
    storage = FailingStorage(configured_env)

    monkeypatch.setattr(db_session, "commit", lambda: (_ for _ in ()).throw(RuntimeError("injected commit failure")))
    with pytest.raises(RuntimeError, match="injected commit failure"):
        upload_workflow_node_image(
            db_session,
            node_id=reference_node.id,
            image_bytes=_make_demo_image_bytes(),
            filename="workflow-reference.png",
            content_type="image/png",
            storage=storage,
        )

    assert db_session.query(SourceAsset).filter_by(
        product_id=product.id,
        kind=SourceAssetKind.REFERENCE_IMAGE,
    ).count() == 0
    _assert_saved_files_are_gone(storage)


def test_bind_poster_commit_failure_rolls_back_and_cleans_copy(
    configured_env: Path,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    product = _create_product(db_session)
    workflow = get_or_create_product_workflow(db_session, product.id)
    reference_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.REFERENCE_IMAGE)
    copy_set = CopySet(
        product_id=product.id,
        structured_payload={},
        model_structured_payload={},
        provider_name="test",
        model_name="test",
        prompt_version="test",
    )
    db_session.add(copy_set)
    db_session.flush()
    poster_path = f"products/{product.id}/posters/manual.png"
    poster_file = configured_env / poster_path
    poster_file.parent.mkdir(parents=True, exist_ok=True)
    poster_file.write_bytes(_make_demo_image_bytes())
    poster = PosterVariant(
        product_id=product.id,
        copy_set_id=copy_set.id,
        kind=PosterKind.PROMO_POSTER,
        template_name="test",
        mime_type="image/png",
        storage_path=poster_path,
        width=100,
        height=100,
    )
    db_session.add(poster)
    db_session.commit()

    storage = FailingStorage(configured_env)
    monkeypatch.setattr(db_session, "commit", lambda: (_ for _ in ()).throw(RuntimeError("injected commit failure")))
    with pytest.raises(RuntimeError, match="injected commit failure"):
        bind_workflow_node_image(
            db_session,
            node_id=reference_node.id,
            poster_variant_id=poster.id,
            storage=storage,
        )

    assert db_session.query(SourceAsset).filter_by(
        product_id=product.id,
        source_poster_variant_id=poster.id,
    ).count() == 0
    _assert_saved_files_are_gone(storage)
    assert poster_file.exists()


def test_image_session_reference_write_commit_failure_cleans_files(
    configured_env: Path,
    db_session,
    monkeypatch,
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


def test_image_session_attach_commit_failure_cleans_copied_file(configured_env: Path, db_session, monkeypatch) -> None:
    image_session = create_image_session(db_session, title="写回失败")
    seed_storage = LocalStorage(configured_env)
    generated_path = seed_storage.save_image_session_generated(image_session.id, _make_demo_image_bytes())
    generated_asset = ImageSessionAsset(
        session_id=image_session.id,
        kind=ImageSessionAssetKind.GENERATED_IMAGE,
        original_filename="generated.png",
        mime_type="image/png",
        storage_path=generated_path,
    )
    db_session.add(generated_asset)
    product = _create_product(db_session)
    db_session.commit()

    storage = FailingStorage(configured_env)
    monkeypatch.setattr(db_session, "commit", lambda: (_ for _ in ()).throw(RuntimeError("injected commit failure")))
    with pytest.raises(RuntimeError, match="injected commit failure"):
        attach_image_session_asset_to_product(
            db_session,
            image_session_id=image_session.id,
            asset_id=generated_asset.id,
            target="reference",
            product_id=product.id,
            storage=storage,
        )

    assert db_session.query(SourceAsset).filter_by(
        product_id=product.id,
        kind=SourceAssetKind.REFERENCE_IMAGE,
    ).count() == 0
    _assert_saved_files_are_gone(storage)


def test_storage_delete_failures_keep_committed_database_results(
    configured_env: Path,
    db_session,
    caplog: pytest.LogCaptureFixture,
) -> None:
    product = _create_product(db_session)
    add_reference_images(
        db_session,
        product_id=product.id,
        reference_image_uploads=[(_make_demo_image_bytes(), "reference.png", "image/png")],
    )
    reference_asset = next(
        asset for asset in product.source_assets if asset.kind == SourceAssetKind.REFERENCE_IMAGE
    )
    failing_storage = FailingStorage(configured_env)
    failing_storage.fail_delete = True

    with _capture_storage_logs(caplog):
        delete_reference_image(db_session, asset_id=reference_asset.id, storage=failing_storage)

    assert db_session.get(SourceAsset, reference_asset.id) is None
    assert "source_asset_id=" in caplog.text

    product_to_delete = _create_product(db_session)
    caplog.clear()
    with _capture_storage_logs(caplog):
        delete_product(db_session, product_id=product_to_delete.id, storage=failing_storage)

    assert db_session.get(Product, product_to_delete.id) is None
    assert f"product_id={product_to_delete.id}" in caplog.text


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
    reference_asset = next(
        asset for asset in added.assets if asset.kind == ImageSessionAssetKind.REFERENCE_UPLOAD
    )
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

    def fail_warming(relative_path: str) -> None:
        raise RuntimeError("injected variant failure")

    monkeypatch.setattr(storage, "_warm_image_variants", fail_warming)
    with pytest.raises(RuntimeError, match="injected variant failure"):
        storage.save_product_upload("product-id", "product.png", _make_demo_image_bytes())

    source_root = configured_env / "products" / "product-id" / "source"
    assert not any(path.is_file() for path in source_root.rglob("*"))


def test_workflow_node_commit_failure_cleans_generated_artifact(
    configured_env: Path,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.application.product_workflow import execution as workflow_execution

    product = _create_product(db_session)
    workflow = get_or_create_product_workflow(db_session, product.id)
    image_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.IMAGE_GENERATION)
    run = WorkflowRun(workflow_id=workflow.id, status=WorkflowRunStatus.RUNNING)
    db_session.add(run)
    db_session.flush()
    node_run = WorkflowNodeRun(
        workflow_run_id=run.id,
        node_id=image_node.id,
        status=WorkflowNodeStatus.QUEUED,
    )
    db_session.add(node_run)
    db_session.commit()

    storage = FailingStorage(configured_env)
    monkeypatch.setattr(workflow_execution, "LocalStorage", lambda: storage)

    def fake_execute_node(
        session,
        *,
        workflow_id: str,
        node: WorkflowNode,
        dependencies=None,
        storage=None,
        storage_writes=None,
    ) -> dict:
        relative_path = storage.save_generated_image(workflow_id, "generated", _make_demo_image_bytes())
        storage_writes.track(storage, relative_path)
        return {}

    monkeypatch.setattr(workflow_execution, "_execute_node", fake_execute_node)
    original_commit = db_session.commit
    commit_calls = 0

    def fail_final_commit() -> None:
        nonlocal commit_calls
        commit_calls += 1
        if commit_calls == 2:
            raise RuntimeError("injected commit failure")
        original_commit()

    monkeypatch.setattr(db_session, "commit", fail_final_commit)
    workflow_execution._execute_workflow_node_run(
        db_session,
        node_run_id=node_run.id,
        schedule_after_finish=False,
    )

    _assert_saved_files_are_gone(storage)
