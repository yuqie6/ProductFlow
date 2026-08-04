from __future__ import annotations

from collections.abc import Iterator
from contextlib import contextmanager
from pathlib import Path

from sqlalchemy import event
from sqlalchemy.orm import Session

from productflow_backend.application.product_workflow.context import reference_image_inputs_for_copy
from productflow_backend.domain.enums import SourceAssetKind, WorkflowNodeType
from productflow_backend.infrastructure.db.models import (
    Product,
    ProductWorkflow,
    SourceAsset,
    WorkflowEdge,
    WorkflowNode,
)
from productflow_backend.infrastructure.storage import LocalStorage


@contextmanager
def _record_source_asset_selects(session: Session) -> Iterator[list[str]]:
    statements: list[str] = []
    bind = session.get_bind()

    def record_source_asset_select(
        connection,
        cursor,
        statement: str,
        parameters,
        context,
        executemany,
    ) -> None:
        normalized = " ".join(statement.lower().split())
        if normalized.startswith("select source_assets.id") and "from source_assets" in normalized:
            statements.append(normalized)

    event.listen(bind, "before_cursor_execute", record_source_asset_select)
    try:
        yield statements
    finally:
        event.remove(bind, "before_cursor_execute", record_source_asset_select)


def _persist_reference_asset(
    session: Session,
    *,
    product: Product,
    asset_id: str,
    filename: str,
) -> SourceAsset:
    asset = SourceAsset(
        id=asset_id,
        product=product,
        kind=SourceAssetKind.REFERENCE_IMAGE,
        original_filename=filename,
        mime_type="image/png",
        storage_path=f"products/{product.id}/reference/{filename}",
    )
    session.add(asset)
    return asset


def _persist_workflow_with_references(
    session: Session,
    *,
    product: Product,
    reference_configs: list[tuple[str, dict, dict | None]],
) -> tuple[ProductWorkflow, WorkflowNode]:
    workflow = ProductWorkflow(product=product, title="参考素材批量查询测试")
    copy_node = WorkflowNode(
        workflow=workflow,
        node_type=WorkflowNodeType.COPY_GENERATION,
        title="目标文案",
        config_json={},
    )
    session.add_all([workflow, copy_node])
    for title, config_json, output_json in reference_configs:
        reference_node = WorkflowNode(
            workflow=workflow,
            node_type=WorkflowNodeType.REFERENCE_IMAGE,
            title=title,
            config_json=config_json,
            output_json=output_json,
        )
        session.add(reference_node)
        session.add(
            WorkflowEdge(
                workflow=workflow,
                source_node=reference_node,
                target_node=copy_node,
            )
        )
    session.flush()
    return workflow, copy_node


def test_reference_image_inputs_batch_query_preserves_order_filtering_and_metadata(
    configured_env: Path,
    db_session: Session,
) -> None:
    product = Product(id="product-main", name="主商品")
    other_product = Product(id="product-other", name="其它商品")
    db_session.add_all([product, other_product])
    first_asset = _persist_reference_asset(
        db_session,
        product=product,
        asset_id="asset-first",
        filename="first.png",
    )
    shared_asset = _persist_reference_asset(
        db_session,
        product=product,
        asset_id="asset-shared",
        filename="shared.png",
    )
    later_asset = _persist_reference_asset(
        db_session,
        product=product,
        asset_id="asset-later",
        filename="later.png",
    )
    foreign_asset = _persist_reference_asset(
        db_session,
        product=other_product,
        asset_id="asset-foreign",
        filename="foreign.png",
    )
    db_session.flush()
    workflow, copy_node = _persist_workflow_with_references(
        db_session,
        product=product,
        reference_configs=[
            (
                "第一参考节点",
                {
                    "source_asset_ids": [first_asset.id, shared_asset.id, "asset-missing"],
                    "role": "  style  ",
                    "label": "  第一组  ",
                },
                {"source_asset_ids": [first_asset.id]},
            ),
            (
                "第二参考节点",
                {
                    "source_asset_ids": [shared_asset.id, later_asset.id, foreign_asset.id],
                    "role": "subject",
                },
                None,
            ),
        ],
    )

    with _record_source_asset_selects(db_session) as source_asset_selects:
        inputs = reference_image_inputs_for_copy(
            db_session,
            workflow=workflow,
            node_id=copy_node.id,
            storage=LocalStorage(configured_env),
        )

    assert len(source_asset_selects) == 1
    assert [item.filename for item in inputs] == ["first.png", "shared.png", "later.png"]
    assert [(item.role, item.label) for item in inputs] == [
        ("style", "第一组"),
        ("style", "第一组"),
        ("subject", "第二参考节点"),
    ]
    assert [item.path for item in inputs] == [
        (configured_env / first_asset.storage_path).resolve(),
        (configured_env / shared_asset.storage_path).resolve(),
        (configured_env / later_asset.storage_path).resolve(),
    ]
    assert all(item.mime_type == "image/png" for item in inputs)


def test_reference_image_inputs_use_one_query_for_one_reference_node(
    configured_env: Path,
    db_session: Session,
) -> None:
    product = Product(id="product-single", name="单参考商品")
    db_session.add(product)
    asset = _persist_reference_asset(
        db_session,
        product=product,
        asset_id="asset-single",
        filename="single.png",
    )
    db_session.flush()
    workflow, copy_node = _persist_workflow_with_references(
        db_session,
        product=product,
        reference_configs=[("单参考节点", {"source_asset_ids": [asset.id]}, None)],
    )

    with _record_source_asset_selects(db_session) as source_asset_selects:
        inputs = reference_image_inputs_for_copy(
            db_session,
            workflow=workflow,
            node_id=copy_node.id,
            storage=LocalStorage(configured_env),
        )

    assert len(source_asset_selects) == 1
    assert [item.filename for item in inputs] == ["single.png"]


def test_reference_image_inputs_skip_query_when_reference_nodes_have_no_asset_ids(
    configured_env: Path,
    db_session: Session,
) -> None:
    product = Product(id="product-empty", name="空参考商品")
    db_session.add(product)
    db_session.flush()
    workflow, copy_node = _persist_workflow_with_references(
        db_session,
        product=product,
        reference_configs=[("空参考节点", {"role": "style"}, {"summary": "no asset"})],
    )

    with _record_source_asset_selects(db_session) as source_asset_selects:
        inputs = reference_image_inputs_for_copy(
            db_session,
            workflow=workflow,
            node_id=copy_node.id,
            storage=LocalStorage(configured_env),
        )

    assert source_asset_selects == []
    assert inputs == []
