"""canonicalize schema-v2 workflow edge handles

Revision ID: 20260820_0070
Revises: 20260820_0069
Create Date: 2026-08-20
"""

from __future__ import annotations

from alembic import op

revision = "20260820_0070"
down_revision = "20260820_0069"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.execute(
        """
        UPDATE workflow_edges AS edge
        SET source_handle = CASE
                WHEN source_node.node_type = 'product_context' THEN 'facts'
                WHEN source_node.node_type = 'reference_image' THEN 'asset'
                WHEN source_node.node_type = 'prompt_generation' THEN 'prompt'
                WHEN source_node.node_type = 'image_generation' THEN 'image'
                ELSE edge.source_handle
            END,
            target_handle = CASE
                WHEN target_node.node_type = 'prompt_generation'
                     AND source_node.node_type = 'product_context' THEN 'facts'
                WHEN target_node.node_type = 'prompt_generation'
                     AND source_node.node_type = 'reference_image' THEN 'reference'
                WHEN target_node.node_type = 'image_generation'
                     AND source_node.node_type = 'product_context' THEN 'facts'
                WHEN target_node.node_type = 'image_generation'
                     AND source_node.node_type = 'prompt_generation' THEN 'prompt'
                WHEN target_node.node_type = 'image_generation'
                     AND source_node.node_type IN ('reference_image', 'image_generation') THEN 'reference'
                ELSE edge.target_handle
            END
        FROM workflow_nodes AS source_node, workflow_nodes AS target_node
        WHERE source_node.id = edge.source_node_id
          AND target_node.id = edge.target_node_id
          AND source_node.schema_version = 2
          AND target_node.schema_version = 2
          AND (
              (source_node.node_type = 'product_context'
               AND target_node.node_type IN ('prompt_generation', 'image_generation'))
              OR (source_node.node_type = 'reference_image'
                  AND target_node.node_type IN ('prompt_generation', 'image_generation'))
              OR (source_node.node_type = 'prompt_generation'
                  AND target_node.node_type = 'image_generation')
              OR (source_node.node_type = 'image_generation'
                  AND target_node.node_type = 'image_generation')
          )
        """
    )


def downgrade() -> None:
    pass
