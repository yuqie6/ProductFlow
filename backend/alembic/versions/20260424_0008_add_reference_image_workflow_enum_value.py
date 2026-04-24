"""add reference image workflow node enum value"""

from alembic import op

revision = "20260424_0008"
down_revision = "20260424_0007"
branch_labels = None
depends_on = None


def upgrade() -> None:
    bind = op.get_bind()
    if bind.dialect.name == "postgresql":
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
