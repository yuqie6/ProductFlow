"""allow creative_brief and visual_system graph artifacts

Revision ID: 20260822_0083
Revises: 20260822_0082
Create Date: 2026-08-22
"""

from __future__ import annotations

from alembic import op

revision = "20260822_0083"
down_revision = "20260822_0082"
branch_labels = None
depends_on = None


def upgrade() -> None:
    _replace_artifact_type_check(
        "artifact_type IN ('creative_brief', 'visual_system', 'prompt', 'image')",
    )


def downgrade() -> None:
    _replace_artifact_type_check("artifact_type IN ('prompt', 'image')")


def _replace_artifact_type_check(expression: str) -> None:
    with op.batch_alter_table("workflow_graph_artifacts") as batch_op:
        batch_op.drop_constraint("ck_workflow_graph_artifacts_type", type_="check")
        batch_op.create_check_constraint("ck_workflow_graph_artifacts_type", expression)
