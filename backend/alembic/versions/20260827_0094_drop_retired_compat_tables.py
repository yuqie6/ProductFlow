"""drop retired compatibility tables and draft lineage columns

Revision ID: 20260827_0094
Revises: 20260825_0093
Create Date: 2026-08-27

ADR 0010: 主线不再保留 WorkflowDraft、legacy archive、cutover gate 和旧源表。
已部署库直接删列删表；空库 upgrade head 到达同一 schema。不能降级。
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260827_0094"
down_revision = "20260825_0093"
branch_labels = None
depends_on = None

_PRODUCT_IMAGE_ORIGIN_VALUES = (
    "upload",
    "workflow_generation",
    "image_session_attach",
    "local_edit",
)
_MEDIA_LIBRARY_SOURCE_TYPES = (
    "image_session_generated",
    "product_asset",
    "direct_upload",
)
_RETIRED_TABLES = (
    "workflow_draft_legacy_archive_seeds",
    "workflow_draft_recipe_seeds",
    "legacy_workflow_archive_assets",
    "workflow_draft_revisions",
    "workflow_drafts",
    "legacy_workflow_archives",
    "legacy_canvas_agent_archives",
    "legacy_user_template_archives",
    "legacy_cutover_gates",
    "media_library_cutover_gates",
    "image_gallery_entries",
    "source_assets",
    "poster_variants",
    "copy_sets",
    "creative_briefs",
    "user_canvas_templates",
)


def _alter_existing_table(table_name: str):
    bind = op.get_bind()
    return op.batch_alter_table(table_name, recreate="always" if bind.dialect.name == "sqlite" else "auto")


def _in_clause(values: tuple[str, ...]) -> str:
    return ", ".join(f"'{value}'" for value in values)


def _table_exists(table_name: str) -> bool:
    return table_name in sa.inspect(op.get_bind()).get_table_names()


def _column_names(table_name: str) -> set[str]:
    inspector = sa.inspect(op.get_bind())
    return {column["name"] for column in inspector.get_columns(table_name)}


def _fk_name_for_column(table_name: str, column_name: str) -> str | None:
    inspector = sa.inspect(op.get_bind())
    for foreign_key in inspector.get_foreign_keys(table_name):
        if column_name in (foreign_key.get("constrained_columns") or []):
            return foreign_key.get("name")
    return None


def _unique_name_for_column(table_name: str, column_name: str, preferred_name: str | None = None) -> str | None:
    inspector = sa.inspect(op.get_bind())
    matched: str | None = None
    for unique in inspector.get_unique_constraints(table_name):
        columns = unique.get("column_names") or []
        name = unique.get("name")
        if preferred_name and name == preferred_name:
            return name
        if columns == [column_name] and name:
            matched = name
    return matched


def _unique_index_name_for_column(
    table_name: str,
    column_name: str,
    preferred_name: str | None = None,
) -> str | None:
    inspector = sa.inspect(op.get_bind())
    matched: str | None = None
    for index in inspector.get_indexes(table_name):
        if not index.get("unique"):
            continue
        name = index.get("name")
        columns = index.get("column_names") or []
        if preferred_name and name == preferred_name:
            return name
        if columns == [column_name] and name:
            matched = name
    return matched


def _check_names(table_name: str) -> set[str]:
    inspector = sa.inspect(op.get_bind())
    return {check["name"] for check in inspector.get_check_constraints(table_name) if check.get("name")}


def _drop_fk_unique_and_column(
    table_name: str,
    column_name: str,
    *,
    fk_name: str | None = None,
    unique_name: str | None = None,
    drop_checks: tuple[str, ...] = (),
    create_checks: tuple[tuple[str, str], ...] = (),
) -> None:
    if not _table_exists(table_name):
        return
    columns = _column_names(table_name)
    if column_name not in columns:
        for check_name, expression in create_checks:
            if check_name in _check_names(table_name):
                continue
            with _alter_existing_table(table_name) as batch_op:
                batch_op.create_check_constraint(check_name, expression)
        return

    resolved_fk = _fk_name_for_column(table_name, column_name) or fk_name
    resolved_unique = _unique_name_for_column(table_name, column_name, unique_name)
    resolved_unique_index = None
    if resolved_unique is None:
        resolved_unique_index = _unique_index_name_for_column(table_name, column_name, unique_name)
    existing_checks = _check_names(table_name)

    with _alter_existing_table(table_name) as batch_op:
        for check_name in drop_checks:
            if check_name in existing_checks:
                batch_op.drop_constraint(check_name, type_="check")
        if resolved_unique is not None:
            batch_op.drop_constraint(resolved_unique, type_="unique")
        elif resolved_unique_index is not None:
            batch_op.drop_index(resolved_unique_index)
        if resolved_fk:
            batch_op.drop_constraint(resolved_fk, type_="foreignkey")
        batch_op.drop_column(column_name)
        for check_name, expression in create_checks:
            batch_op.create_check_constraint(check_name, expression)


def _rewrite_media_library_source_type() -> None:
    if not _table_exists("media_library_assets"):
        return
    op.execute(
        sa.text("UPDATE media_library_assets SET source_type = 'direct_upload' WHERE source_type = 'legacy_gallery'")
    )
    existing_checks = _check_names("media_library_assets")
    bind = op.get_bind()
    new_expression = f"source_type IN ({_in_clause(_MEDIA_LIBRARY_SOURCE_TYPES)})"
    if bind.dialect.name == "postgresql":
        if "ck_media_library_assets_source_type" in existing_checks:
            op.drop_constraint("ck_media_library_assets_source_type", "media_library_assets", type_="check")
        op.create_check_constraint(
            "ck_media_library_assets_source_type",
            "media_library_assets",
            new_expression,
        )
        return
    with _alter_existing_table("media_library_assets") as batch_op:
        if "ck_media_library_assets_source_type" in existing_checks:
            batch_op.drop_constraint("ck_media_library_assets_source_type", type_="check")
        batch_op.create_check_constraint("ck_media_library_assets_source_type", new_expression)


def _rewrite_product_image_origin_type() -> None:
    if not _table_exists("product_image_assets"):
        return
    op.execute(sa.text("UPDATE product_image_assets SET origin_type = 'upload' WHERE origin_type = 'legacy_import'"))
    bind = op.get_bind()
    values = _in_clause(_PRODUCT_IMAGE_ORIGIN_VALUES)
    if bind.dialect.name == "postgresql":
        op.execute(sa.text("ALTER TABLE product_image_assets ALTER COLUMN origin_type TYPE varchar"))
        op.execute(sa.text("DROP TYPE IF EXISTS productimageorigintype"))
        op.execute(sa.text(f"CREATE TYPE productimageorigintype AS ENUM ({values})"))
        op.execute(
            sa.text(
                "ALTER TABLE product_image_assets "
                "ALTER COLUMN origin_type TYPE productimageorigintype "
                "USING origin_type::productimageorigintype"
            )
        )
        return

    inspector = sa.inspect(bind)
    origin_checks = [
        check["name"]
        for check in inspector.get_check_constraints("product_image_assets")
        if check.get("name")
        and ("legacy_import" in (check.get("sqltext") or "") or str(check["name"]).lower() == "productimageorigintype")
    ]
    with _alter_existing_table("product_image_assets") as batch_op:
        for check_name in origin_checks:
            batch_op.drop_constraint(check_name, type_="check")
        batch_op.create_check_constraint(
            "ck_product_image_assets_origin_type",
            f"origin_type IN ({values})",
        )


def _drop_products_copy_set_pointer() -> None:
    if not _table_exists("products"):
        return
    if "current_confirmed_copy_set_id" not in _column_names("products"):
        return
    fk_name = _fk_name_for_column("products", "current_confirmed_copy_set_id")
    with _alter_existing_table("products") as batch_op:
        if fk_name:
            batch_op.drop_constraint(fk_name, type_="foreignkey")
        batch_op.drop_column("current_confirmed_copy_set_id")


def _drop_table_if_exists(table_name: str) -> None:
    bind = op.get_bind()
    if bind.dialect.name == "postgresql":
        op.execute(sa.text(f'DROP TABLE IF EXISTS "{table_name}" CASCADE'))
        return
    op.execute(sa.text(f'DROP TABLE IF EXISTS "{table_name}"'))


def upgrade() -> None:
    _rewrite_media_library_source_type()
    _rewrite_product_image_origin_type()
    _drop_products_copy_set_pointer()

    _drop_fk_unique_and_column(
        "agent_conversations",
        "workflow_draft_id",
        fk_name="fk_agent_conversations_workflow_draft_id",
        unique_name="uq_agent_conversations_workflow_draft_id",
        drop_checks=("ck_agent_conversations_scope_fields",),
        create_checks=(
            (
                "ck_agent_conversations_scope_fields",
                "(scope_type = 'product_workflow' AND product_id IS NOT NULL) OR "
                "(scope_type = 'global' AND product_id IS NULL)",
            ),
        ),
    )
    _drop_fk_unique_and_column(
        "agent_tasks",
        "workflow_draft_id",
        fk_name="fk_agent_tasks_workflow_draft_id",
    )
    _drop_fk_unique_and_column(
        "agent_turn_projections",
        "workflow_draft_revision_id",
        fk_name="fk_agent_turn_projections_workflow_draft_revision_id",
        unique_name="uq_agent_turn_projections_workflow_draft_revision_id",
    )
    _drop_fk_unique_and_column(
        "visual_system_versions",
        "source_draft_revision_id",
        fk_name="fk_visual_system_versions_source_draft_revision_id",
        unique_name="uq_visual_system_versions_source_draft_revision_id",
    )
    _drop_fk_unique_and_column(
        "product_fact_set_versions",
        "source_draft_revision_id",
        fk_name="fk_product_fact_set_versions_source_draft_revision_id",
        unique_name="uq_product_fact_set_versions_source_draft_revision_id",
    )
    _drop_fk_unique_and_column(
        "workflow_graphs",
        "source_draft_revision_id",
        fk_name="fk_workflow_graphs_source_draft_revision_id",
    )

    bind = op.get_bind()
    if bind.dialect.name == "sqlite":
        op.execute(sa.text("PRAGMA foreign_keys=OFF"))
    for table_name in _RETIRED_TABLES:
        _drop_table_if_exists(table_name)
    if bind.dialect.name == "sqlite":
        op.execute(sa.text("PRAGMA foreign_keys=ON"))


def downgrade() -> None:
    raise RuntimeError("已删除的兼容表和 Draft 列不能降级")
