"""repair reference image workflow node enum value

Revision ID: 20260424_0009
Revises: 20260424_0008
Create Date: 2026-04-24
"""

from alembic import op

revision = "20260424_0009"
down_revision = "20260424_0008"
branch_labels = None
depends_on = None


def upgrade() -> None:
    bind = op.get_bind()
    if bind.dialect.name != "postgresql":
        return

    # Some development/live databases already ran an older 0008 revision that
    # added `image_upload` but did not add the final `reference_image` value.
    # Add the final value in an autocommit block so it can be used immediately
    # by the data repair statement below.
    with op.get_context().autocommit_block():
        op.execute("ALTER TYPE workflownodetype ADD VALUE IF NOT EXISTS 'reference_image'")

    op.execute(
        "UPDATE workflow_nodes SET node_type = 'reference_image' "
        "WHERE node_type::text = 'image_upload'"
    )


def downgrade() -> None:
    # PostgreSQL enum values cannot be safely removed without rebuilding the type.
    # Keep downgrade as a no-op so existing workflow rows remain readable.
    pass
