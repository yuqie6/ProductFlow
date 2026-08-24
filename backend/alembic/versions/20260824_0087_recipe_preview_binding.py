"""persist recipe preview binding and semantic application audit fields

Revision ID: 20260824_0087
Revises: 20260824_0086
Create Date: 2026-08-24
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260824_0087"
down_revision = "20260824_0086"
branch_labels = None
depends_on = None


def _schema_version_check() -> str:
    return "schema_version IN (1, 2)"


def _preview_v2_check() -> str:
    return (
        "(schema_version = 1) OR ("
        "schema_version = 2 "
        "AND preview_graph_revision IS NOT NULL "
        "AND preview_graph_revision >= 0 "
        "AND preview_digest IS NOT NULL "
        "AND length(preview_digest) = 64 "
        "AND updated_node_ids_json IS NOT NULL "
        "AND required_bindings_json IS NOT NULL"
        ")"
    )


def upgrade() -> None:
    with op.batch_alter_table("workflow_recipe_applications") as batch_op:
        batch_op.drop_constraint("ck_workflow_recipe_applications_schema_version", type_="check")
        batch_op.add_column(sa.Column("preview_graph_revision", sa.Integer(), nullable=True))
        batch_op.add_column(sa.Column("preview_digest", sa.String(length=64), nullable=True))
        batch_op.add_column(sa.Column("updated_node_ids_json", sa.JSON(), nullable=True))
        batch_op.add_column(sa.Column("required_bindings_json", sa.JSON(), nullable=True))
        batch_op.create_check_constraint(
            "ck_workflow_recipe_applications_schema_version",
            _schema_version_check(),
        )
        batch_op.create_check_constraint(
            "ck_workflow_recipe_applications_preview_v2",
            _preview_v2_check(),
        )


def downgrade() -> None:
    bind = op.get_bind()
    referenced = bind.execute(
        sa.text(
            "SELECT 1 FROM workflow_recipe_applications "
            "WHERE schema_version = 2 LIMIT 1"
        )
    ).first()
    if referenced is not None:
        raise RuntimeError("schema2 recipe application 已存在，不能 downgrade 丢失 preview 审计")

    with op.batch_alter_table("workflow_recipe_applications") as batch_op:
        batch_op.drop_constraint("ck_workflow_recipe_applications_preview_v2", type_="check")
        batch_op.drop_constraint("ck_workflow_recipe_applications_schema_version", type_="check")
        batch_op.drop_column("required_bindings_json")
        batch_op.drop_column("updated_node_ids_json")
        batch_op.drop_column("preview_digest")
        batch_op.drop_column("preview_graph_revision")
        batch_op.create_check_constraint(
            "ck_workflow_recipe_applications_schema_version",
            "schema_version = 1",
        )
