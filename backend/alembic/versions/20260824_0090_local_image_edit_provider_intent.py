"""persist the immutable provider capability intent for local image edits

Revision ID: 20260824_0090
Revises: 20260824_0089
Create Date: 2026-08-24
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260824_0090"
down_revision = "20260824_0089"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.add_column(
        "local_image_edit_tasks",
        sa.Column("requested_provider_name", sa.String(length=80), nullable=True),
    )
    op.add_column(
        "local_image_edit_tasks",
        sa.Column("requested_local_edit_mode", sa.String(length=32), nullable=True),
    )
    op.execute(
        sa.text(
            "UPDATE local_image_edit_tasks "
            "SET requested_provider_name = 'legacy_snapshot_missing', "
            "requested_local_edit_mode = 'legacy_snapshot_missing' "
            "WHERE request_hash IS NOT NULL"
        )
    )
    with op.batch_alter_table("local_image_edit_tasks") as batch_op:
        batch_op.create_check_constraint(
            "ck_local_image_edit_tasks_provider_intent",
            "request_hash IS NULL OR "
            "(requested_provider_name IS NOT NULL AND requested_local_edit_mode IS NOT NULL)",
        )


def downgrade() -> None:
    bind = op.get_bind()
    referenced = bind.execute(
        sa.text(
            "SELECT 1 FROM local_image_edit_tasks "
            "WHERE requested_provider_name IS NOT NULL "
            "OR requested_local_edit_mode IS NOT NULL LIMIT 1"
        )
    ).first()
    if referenced is not None:
        raise RuntimeError("已有局部编辑 provider 能力快照，不能 downgrade 丢失请求审计")

    with op.batch_alter_table("local_image_edit_tasks") as batch_op:
        batch_op.drop_constraint("ck_local_image_edit_tasks_provider_intent", type_="check")
        batch_op.drop_column("requested_local_edit_mode")
        batch_op.drop_column("requested_provider_name")
