"""remove legacy product workflow nodes

Revision ID: 20260424_0010
Revises: 20260424_0009
Create Date: 2026-04-24
"""

from alembic import op

revision = "20260424_0010"
down_revision = "20260424_0009"
branch_labels = None
depends_on = None


SUPPORTED_NODE_TYPES = ("product_context", "reference_image", "copy_generation", "image_generation")


def _node_type_text_expr() -> str:
    bind = op.get_bind()
    if bind.dialect.name == "postgresql":
        return "node_type::text"
    return "CAST(node_type AS TEXT)"


def _supported_node_type_list() -> str:
    return ", ".join(f"'{node_type}'" for node_type in SUPPORTED_NODE_TYPES)


def upgrade() -> None:
    bind = op.get_bind()
    node_type_text = _node_type_text_expr()

    if bind.dialect.name == "postgresql":
        with op.get_context().autocommit_block():
            op.execute("ALTER TYPE workflownodetype ADD VALUE IF NOT EXISTS 'reference_image'")

    # Some pre-clarification databases used `image_upload` as the image-slot node.
    # Keep the slot data, but move it to the only supported current value before
    # removing the remaining unsupported workflow nodes.
    op.execute(
        "UPDATE workflow_nodes SET node_type = 'reference_image' "
        f"WHERE {node_type_text} = 'image_upload'"
    )

    unsupported_nodes = (
        "SELECT id FROM workflow_nodes "
        f"WHERE {node_type_text} NOT IN ({_supported_node_type_list()})"
    )
    op.execute(
        "DELETE FROM workflow_edges "
        f"WHERE source_node_id IN ({unsupported_nodes}) OR target_node_id IN ({unsupported_nodes})"
    )
    op.execute(f"DELETE FROM workflow_node_runs WHERE node_id IN ({unsupported_nodes})")
    op.execute(f"DELETE FROM workflow_nodes WHERE id IN ({unsupported_nodes})")


def downgrade() -> None:
    # Deleted unsupported workflow nodes cannot be reconstructed safely.
    pass
