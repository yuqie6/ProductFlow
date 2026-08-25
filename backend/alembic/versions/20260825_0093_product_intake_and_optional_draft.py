"""product owns intake; product conversations no longer require a WorkflowDraft

Revision ID: 20260825_0093
Revises: 20260825_0092
Create Date: 2026-08-25
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260825_0093"
down_revision = "20260825_0092"
branch_labels = None
depends_on = None


def _alter_existing_table(table_name: str):
    bind = op.get_bind()
    return op.batch_alter_table(table_name, recreate="always" if bind.dialect.name == "sqlite" else "auto")


def upgrade() -> None:
    with _alter_existing_table("products") as batch_op:
        batch_op.add_column(sa.Column("intake_schema_version", sa.Integer(), nullable=True))
        batch_op.add_column(sa.Column("intake_json", sa.JSON(), nullable=True))
        batch_op.create_check_constraint(
            "ck_products_intake_pair",
            "(intake_schema_version IS NULL AND intake_json IS NULL) OR "
            "(intake_schema_version = 1 AND intake_json IS NOT NULL)",
        )
    op.execute(
        sa.text(
            """
            UPDATE products
            SET intake_schema_version = (
                SELECT workflow_drafts.intake_schema_version
                FROM workflow_drafts
                WHERE workflow_drafts.product_id = products.id
                  AND workflow_drafts.intake_json IS NOT NULL
                ORDER BY workflow_drafts.updated_at DESC, workflow_drafts.id DESC
                LIMIT 1
            ),
            intake_json = (
                SELECT workflow_drafts.intake_json
                FROM workflow_drafts
                WHERE workflow_drafts.product_id = products.id
                  AND workflow_drafts.intake_json IS NOT NULL
                ORDER BY workflow_drafts.updated_at DESC, workflow_drafts.id DESC
                LIMIT 1
            )
            WHERE EXISTS (
                SELECT 1
                FROM workflow_drafts
                WHERE workflow_drafts.product_id = products.id
                  AND workflow_drafts.intake_json IS NOT NULL
            )
            """
        )
    )
    with _alter_existing_table("agent_conversations") as batch_op:
        batch_op.drop_constraint("ck_agent_conversations_scope_fields", type_="check")
        batch_op.create_check_constraint(
            "ck_agent_conversations_scope_fields",
            "(scope_type = 'product_workflow' AND product_id IS NOT NULL) "
            "OR (scope_type = 'global' AND product_id IS NULL AND workflow_draft_id IS NULL)",
        )


def downgrade() -> None:
    with _alter_existing_table("agent_conversations") as batch_op:
        batch_op.drop_constraint("ck_agent_conversations_scope_fields", type_="check")
        batch_op.create_check_constraint(
            "ck_agent_conversations_scope_fields",
            "(scope_type = 'product_workflow' AND product_id IS NOT NULL AND workflow_draft_id IS NOT NULL) "
            "OR (scope_type = 'global' AND product_id IS NULL AND workflow_draft_id IS NULL)",
        )
    with _alter_existing_table("products") as batch_op:
        batch_op.drop_constraint("ck_products_intake_pair", type_="check")
        batch_op.drop_column("intake_json")
        batch_op.drop_column("intake_schema_version")
