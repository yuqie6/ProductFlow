"""drop retired schema-v2 online graph tables

Revision ID: 20260821_0080
Revises: 20260821_0079
Create Date: 2026-08-21

V1 工作流 DAG 与 schema-v2 共用 product_workflows。schema-v2 从未上生产。
V1 生产历史已经在 legacy_workflow_archives / legacy_canvas_agent_archives /
legacy_user_template_archives。本 revision 删除在线 DAG 表；copy/poster/source_assets/
user_canvas_templates 源表和 archive 表保留。legacy_retirement 审计在表不存在时跳过 DAG 计数。
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260821_0080"
down_revision = "20260821_0079"
branch_labels = None
depends_on = None

_V2_TABLES = (
    "workflow_image_generation_references",
    "workflow_image_generation_records",
    "image_prompt_artifact_version_references",
    "image_prompt_artifact_versions",
    "image_prompt_artifacts",
    "visual_exceptions",
    "workflow_provider_effects",
    "workflow_node_runs",
    "workflow_runs",
    "workflow_reveal_events",
    "workflow_materialization_keys",
    "workflow_materializations",
    "workflow_edges",
    "workflow_nodes",
    "workflow_folders",
    "product_workflows",
)


def upgrade() -> None:
    op.execute(sa.text("DELETE FROM agent_workflow_run_requests WHERE graph_id IS NULL"))
    op.execute(sa.text("UPDATE workflow_drafts SET status = 'confirmed' WHERE status = 'materializing'"))

    with op.batch_alter_table("agent_workflow_run_requests") as batch_op:
        batch_op.drop_constraint("ck_agent_workflow_run_requests_v2_or_v3", type_="check")
        batch_op.drop_index("ix_agent_workflow_run_requests_workflow_run_id")
        batch_op.drop_index("ix_agent_workflow_run_requests_source_run_id")
        batch_op.drop_constraint("fk_agent_workflow_run_requests_workflow_id", type_="foreignkey")
        batch_op.drop_constraint("fk_agent_workflow_run_requests_workflow_run_id", type_="foreignkey")
        batch_op.drop_constraint("fk_agent_workflow_run_requests_source_run_id", type_="foreignkey")
        batch_op.drop_column("workflow_id")
        batch_op.drop_column("workflow_run_id")
        batch_op.drop_column("source_run_id")
        batch_op.alter_column("graph_id", existing_type=sa.String(length=36), nullable=False)
        batch_op.create_check_constraint(
            "ck_agent_workflow_run_requests_graph_required",
            "graph_id IS NOT NULL",
        )

    with op.batch_alter_table("workflow_drafts") as batch_op:
        batch_op.drop_constraint("fk_workflow_drafts_final_workflow_id", type_="foreignkey")
        batch_op.drop_column("final_workflow_id")

    with op.batch_alter_table("agent_tasks") as batch_op:
        batch_op.drop_constraint("fk_agent_tasks_workflow_id", type_="foreignkey")
        batch_op.drop_column("workflow_id")

    with op.batch_alter_table("workflow_draft_recipe_seeds") as batch_op:
        batch_op.drop_constraint("ck_workflow_draft_recipe_seeds_base_workflow", type_="check")
        batch_op.drop_constraint("fk_workflow_draft_recipe_seeds_base_workflow_id", type_="foreignkey")
        batch_op.drop_column("base_workflow_id")
        batch_op.drop_column("base_workflow_revision")

    with op.batch_alter_table("workflow_nodes") as batch_op:
        batch_op.drop_constraint(
            "fk_workflow_nodes_current_prompt_artifact_version_id",
            type_="foreignkey",
        )

    bind = op.get_bind()
    if bind.dialect.name == "sqlite":
        op.execute(sa.text("PRAGMA foreign_keys=OFF"))
    for table_name in _V2_TABLES:
        op.drop_table(table_name)
    if bind.dialect.name == "sqlite":
        op.execute(sa.text("PRAGMA foreign_keys=ON"))


def downgrade() -> None:
    raise RuntimeError("schema-v2 在线图表已删除，不能降级")
