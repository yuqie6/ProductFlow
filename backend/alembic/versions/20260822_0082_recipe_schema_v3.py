"""recipe versions store schema-v3 graph fragments

Revision ID: 20260822_0082
Revises: 20260822_0081
Create Date: 2026-08-22
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260822_0082"
down_revision = "20260822_0081"
branch_labels = None
depends_on = None


def upgrade() -> None:
    _assert_no_incompatible_recipe_versions(keep_schema_version=3)
    _replace_schema_version_check(equals=3)


def downgrade() -> None:
    _assert_no_incompatible_recipe_versions(keep_schema_version=1)
    _replace_schema_version_check(equals=1)


def _assert_no_incompatible_recipe_versions(*, keep_schema_version: int) -> None:
    bind = op.get_bind()
    incompatible = bind.execute(
        sa.text(
            """
            SELECT id
            FROM workflow_recipe_versions
            WHERE schema_version <> :keep
            ORDER BY id
            LIMIT 1
            """
        ),
        {"keep": keep_schema_version},
    ).first()
    if incompatible is not None:
        raise RuntimeError(
            f"存在 schema_version != {keep_schema_version} 的用户配方；"
            "升级或降级已停止且不会删除配方、版本或 Draft seed"
        )


def _replace_schema_version_check(*, equals: int) -> None:
    with op.batch_alter_table("workflow_recipe_versions") as batch_op:
        batch_op.drop_constraint("ck_workflow_recipe_versions_schema_version", type_="check")
        batch_op.create_check_constraint(
            "ck_workflow_recipe_versions_schema_version",
            f"schema_version = {equals}",
        )
