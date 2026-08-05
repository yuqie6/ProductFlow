from __future__ import annotations

from datetime import UTC, datetime

from sqlalchemy import event

from productflow_backend.application.product_workflow.artifacts import (
    source_asset_for_poster_variant,
)
from productflow_backend.application.product_workflow.context import poster_variant_ids_from_output
from productflow_backend.domain.enums import PosterKind, SourceAssetKind, WorkflowNodeStatus, WorkflowNodeType
from productflow_backend.infrastructure.db.models import (
    CopySet,
    PosterVariant,
    Product,
    ProductWorkflow,
    SourceAsset,
    WorkflowNode,
)


def test_poster_variant_ids_from_output_prefers_canonical_key() -> None:
    assert poster_variant_ids_from_output({"generated_poster_variant_ids": ["poster-1"]}) == ["poster-1"]
    assert poster_variant_ids_from_output({"poster_variant_ids": ["legacy-1"]}) == ["legacy-1"]
    assert poster_variant_ids_from_output(
        {"generated_poster_variant_ids": ["poster-1"], "poster_variant_ids": ["legacy-1"]}
    ) == ["poster-1"]
    assert poster_variant_ids_from_output(
        {"generated_poster_variant_ids": [1, None], "poster_variant_ids": "legacy"}
    ) == []
    assert poster_variant_ids_from_output({"poster_variant_ids": "legacy-1"}) == ["legacy-1"]


def test_source_asset_for_poster_variant_is_column_only_and_does_not_flush(db_session) -> None:
    product = Product(name="lineage test product")
    workflow = ProductWorkflow(product=product, title="lineage test workflow")
    copy_set = CopySet(
        product=product,
        provider_name="test",
        model_name="test",
        prompt_version="test",
    )
    poster = PosterVariant(
        product=product,
        copy_set=copy_set,
        kind=PosterKind.PROMO_POSTER,
        template_name="test",
        mime_type="image/png",
        storage_path="posters/poster.png",
        width=1,
        height=1,
    )
    db_session.add_all([product, workflow, copy_set, poster])
    db_session.flush()

    column_asset = SourceAsset(
        product=product,
        kind=SourceAssetKind.REFERENCE_IMAGE,
        original_filename="column.png",
        mime_type="image/png",
        storage_path="references/column.png",
        source_poster_variant_id=poster.id,
        created_at=datetime(2026, 8, 6, 0, 0, 1, tzinfo=UTC),
    )
    legacy_asset = SourceAsset(
        product=product,
        kind=SourceAssetKind.REFERENCE_IMAGE,
        original_filename="legacy.png",
        mime_type="image/png",
        storage_path="references/legacy.png",
        created_at=datetime(2026, 8, 6, 0, 0, 2, tzinfo=UTC),
    )
    node = WorkflowNode(
        workflow=workflow,
        node_type=WorkflowNodeType.IMAGE_GENERATION,
        title="historical output",
        status=WorkflowNodeStatus.SUCCEEDED,
        output_json={
            "generated_poster_variant_ids": [poster.id],
            "filled_source_asset_ids": [legacy_asset.id],
        },
    )
    db_session.add_all([column_asset, legacy_asset, node])
    db_session.commit()

    flush_count = 0

    def record_flush(*_args) -> None:
        nonlocal flush_count
        flush_count += 1

    event.listen(db_session, "before_flush", record_flush)
    try:
        found = source_asset_for_poster_variant(db_session, workflow=workflow, poster_variant_id=poster.id)
        assert found is not None
        assert found.id == column_asset.id

        missing = source_asset_for_poster_variant(db_session, workflow=workflow, poster_variant_id="missing-poster")
        assert missing is None
    finally:
        event.remove(db_session, "before_flush", record_flush)

    assert flush_count == 0
    db_session.refresh(legacy_asset)
    assert legacy_asset.source_poster_variant_id is None
