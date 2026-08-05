"""backfill poster/source lineage and add its foreign key

Revision ID: 20260806_0030
Revises: 20260627_0029
Create Date: 2026-08-06
"""

from __future__ import annotations

import json
from collections import defaultdict
from collections.abc import Mapping
from typing import Any

import sqlalchemy as sa

from alembic import op

revision = "20260806_0030"
down_revision = "20260627_0029"
branch_labels = None
depends_on = None

SOURCE_ASSET_LINEAGE_FK = "fk_source_assets_source_poster_variant_id"
SOURCE_ASSET_LINEAGE_INDEX = "ix_source_assets_source_poster_variant_id"
REFERENCE_IMAGE_KIND = "reference_image"


def _json_object(value: Any) -> dict[str, Any]:
    if isinstance(value, Mapping):
        return dict(value)
    if isinstance(value, str):
        try:
            parsed = json.loads(value)
        except (TypeError, ValueError):
            return {}
        return dict(parsed) if isinstance(parsed, Mapping) else {}
    return {}


def _string_list(value: Any) -> list[str]:
    if not isinstance(value, list):
        return []
    return [item for item in value if isinstance(item, str) and item]


def _poster_ids_from_output(output: Mapping[str, Any]) -> list[str]:
    generated_value = output.get("generated_poster_variant_ids")
    if isinstance(generated_value, list):
        return _string_list(generated_value)
    return _string_list(output.get("poster_variant_ids"))


def _lineage_pairs(node_type: str, output_value: Any) -> list[tuple[str, str]]:
    output = _json_object(output_value)
    if node_type == "image_generation":
        poster_ids = _poster_ids_from_output(output)
        source_asset_ids = _string_list(output.get("filled_source_asset_ids"))
        return list(zip(poster_ids, source_asset_ids, strict=False))
    if node_type == "reference_image":
        poster_id = output.get("source_poster_variant_id")
        source_asset_ids = _string_list(output.get("source_asset_ids"))
        if isinstance(poster_id, str) and poster_id and source_asset_ids:
            return [(poster_id, source_asset_ids[0])]
    return []


def _candidate_sort_key(asset: Mapping[str, Any]) -> tuple[str, str]:
    return (str(asset.get("created_at") or ""), str(asset["id"]))


def _backfill_lineage(connection: sa.Connection) -> None:
    workflows = sa.table(
        "product_workflows",
        sa.column("id", sa.String(length=36)),
        sa.column("product_id", sa.String(length=36)),
    )
    nodes = sa.table(
        "workflow_nodes",
        sa.column("workflow_id", sa.String(length=36)),
        sa.column("node_type", sa.String(length=40)),
        sa.column("output_json", sa.JSON()),
    )
    posters = sa.table(
        "poster_variants",
        sa.column("id", sa.String(length=36)),
        sa.column("product_id", sa.String(length=36)),
        sa.column("created_at", sa.DateTime(timezone=True)),
    )
    source_assets = sa.table(
        "source_assets",
        sa.column("id", sa.String(length=36)),
        sa.column("product_id", sa.String(length=36)),
        sa.column("kind", sa.String(length=40)),
        sa.column("source_poster_variant_id", sa.String(length=36)),
        sa.column("created_at", sa.DateTime(timezone=True)),
    )

    poster_by_id = {
        row["id"]: dict(row)
        for row in connection.execute(sa.select(posters.c.id, posters.c.product_id, posters.c.created_at)).mappings()
    }
    source_by_id = {
        row["id"]: dict(row)
        for row in connection.execute(
            sa.select(
                source_assets.c.id,
                source_assets.c.product_id,
                source_assets.c.kind,
                source_assets.c.source_poster_variant_id,
                source_assets.c.created_at,
            )
        ).mappings()
    }

    for asset in source_by_id.values():
        current_poster_id = asset["source_poster_variant_id"]
        if current_poster_id is None:
            continue
        poster = poster_by_id.get(current_poster_id)
        if (
            poster is not None
            and poster["product_id"] == asset["product_id"]
            and asset["kind"] == REFERENCE_IMAGE_KIND
        ):
            continue
        connection.execute(
            source_assets.update()
            .where(source_assets.c.id == asset["id"])
            .values(source_poster_variant_id=None)
        )
        asset["source_poster_variant_id"] = None

    candidate_posters_by_source: dict[str, set[str]] = defaultdict(set)
    candidate_sources_by_poster: dict[str, set[str]] = defaultdict(set)
    node_query = (
        sa.select(nodes.c.node_type, nodes.c.output_json, workflows.c.product_id)
        .select_from(nodes.join(workflows, nodes.c.workflow_id == workflows.c.id))
        .where(
            sa.cast(nodes.c.node_type, sa.String(length=40)).in_(["image_generation", "reference_image"])
        )
    )
    for row in connection.execute(node_query).mappings():
        for poster_id, source_asset_id in _lineage_pairs(row["node_type"], row["output_json"]):
            poster = poster_by_id.get(poster_id)
            asset = source_by_id.get(source_asset_id)
            if (
                poster is None
                or asset is None
                or poster["product_id"] != row["product_id"]
                or asset["product_id"] != row["product_id"]
                or asset["kind"] != REFERENCE_IMAGE_KIND
            ):
                continue
            candidate_posters_by_source[source_asset_id].add(poster_id)
            candidate_sources_by_poster[poster_id].add(source_asset_id)

    ambiguous_source_assets = {
        source_asset_id
        for source_asset_id, poster_ids in candidate_posters_by_source.items()
        if len(poster_ids) > 1
    }
    existing_poster_ids = {
        asset["source_poster_variant_id"]
        for asset in source_by_id.values()
        if asset["source_poster_variant_id"] is not None
    }
    for poster_id, source_asset_ids in sorted(candidate_sources_by_poster.items()):
        if poster_id in existing_poster_ids:
            continue
        candidates = [
            source_by_id[source_asset_id]
            for source_asset_id in source_asset_ids
            if source_asset_id not in ambiguous_source_assets
            and source_by_id[source_asset_id]["source_poster_variant_id"] is None
        ]
        if not candidates:
            continue
        selected_asset = max(candidates, key=_candidate_sort_key)
        connection.execute(
            source_assets.update()
            .where(source_assets.c.id == selected_asset["id"])
            .values(source_poster_variant_id=poster_id)
        )
        selected_asset["source_poster_variant_id"] = poster_id
        existing_poster_ids.add(poster_id)


def _add_lineage_foreign_key() -> None:
    bind = op.get_bind()
    if bind.dialect.name == "sqlite":
        op.drop_index(SOURCE_ASSET_LINEAGE_INDEX, table_name="source_assets")
        with op.batch_alter_table("source_assets", recreate="always") as batch_op:
            batch_op.create_foreign_key(
                SOURCE_ASSET_LINEAGE_FK,
                "poster_variants",
                ["source_poster_variant_id"],
                ["id"],
                ondelete="SET NULL",
            )
        op.create_index(SOURCE_ASSET_LINEAGE_INDEX, "source_assets", ["source_poster_variant_id"])
        return
    op.create_foreign_key(
        SOURCE_ASSET_LINEAGE_FK,
        "source_assets",
        "poster_variants",
        ["source_poster_variant_id"],
        ["id"],
        ondelete="SET NULL",
    )


def _drop_lineage_foreign_key() -> None:
    bind = op.get_bind()
    if bind.dialect.name == "sqlite":
        op.drop_index(SOURCE_ASSET_LINEAGE_INDEX, table_name="source_assets")
        with op.batch_alter_table("source_assets", recreate="always") as batch_op:
            batch_op.drop_constraint(SOURCE_ASSET_LINEAGE_FK, type_="foreignkey")
        op.create_index(SOURCE_ASSET_LINEAGE_INDEX, "source_assets", ["source_poster_variant_id"])
        return
    op.drop_constraint(SOURCE_ASSET_LINEAGE_FK, "source_assets", type_="foreignkey")


def upgrade() -> None:
    connection = op.get_bind()
    _backfill_lineage(connection)
    _add_lineage_foreign_key()


def downgrade() -> None:
    _drop_lineage_foreign_key()
