"""add workflow agent conversation projections

Revision ID: 20260812_0034
Revises: 20260812_0033
Create Date: 2026-08-12
"""

from __future__ import annotations

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

revision = "20260812_0034"
down_revision = "20260812_0033"
branch_labels = None
depends_on = None

AGENT_CONVERSATION_STATUS = postgresql.ENUM(
    "collecting",
    "awaiting_confirmation",
    "completed",
    "failed",
    "canceled",
    "unknown",
    name="agentconversationstatus",
    create_type=False,
)
AGENT_TURN_STATUS = postgresql.ENUM(
    "queued",
    "running",
    "requires_input",
    "awaiting_confirmation",
    "succeeded",
    "failed",
    "cancel_requested",
    "canceled",
    "unknown",
    name="agentturnstatus",
    create_type=False,
)
AGENT_TOOL_MUTATION_STATUS = postgresql.ENUM(
    "prepared",
    "applied",
    "failed",
    "unknown",
    name="agenttoolmutationstatus",
    create_type=False,
)


def _create_enums() -> None:
    bind = op.get_bind()
    AGENT_CONVERSATION_STATUS.create(bind, checkfirst=True)
    AGENT_TURN_STATUS.create(bind, checkfirst=True)
    AGENT_TOOL_MUTATION_STATUS.create(bind, checkfirst=True)


def upgrade() -> None:
    _create_enums()
    op.create_table(
        "agent_conversations",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("workflow_draft_id", sa.String(length=36), nullable=False),
        sa.Column("harness_run_id", sa.String(length=120), nullable=False),
        sa.Column("status", AGENT_CONVERSATION_STATUS, nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(
            ["product_id"],
            ["products.id"],
            name="fk_agent_conversations_product_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["workflow_draft_id"],
            ["workflow_drafts.id"],
            name="fk_agent_conversations_workflow_draft_id",
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "workflow_draft_id",
            name="uq_agent_conversations_workflow_draft_id",
        ),
        sa.UniqueConstraint(
            "harness_run_id",
            name="uq_agent_conversations_harness_run_id",
        ),
    )
    op.create_index(
        "ix_agent_conversations_product_status",
        "agent_conversations",
        ["product_id", "status"],
    )

    op.create_table(
        "agent_turn_projections",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("conversation_id", sa.String(length=36), nullable=False),
        sa.Column("harness_turn_id", sa.String(length=120), nullable=True),
        sa.Column("idempotency_key", sa.String(length=200), nullable=False),
        sa.Column("request_hash", sa.String(length=64), nullable=False),
        sa.Column("input_text", sa.Text(), nullable=False),
        sa.Column("input_asset_ids_json", sa.JSON(), nullable=False),
        sa.Column("status", AGENT_TURN_STATUS, nullable=False),
        sa.Column("resume_required", sa.Boolean(), nullable=False),
        sa.Column("output_text", sa.Text(), nullable=True),
        sa.Column("error_text", sa.Text(), nullable=True),
        sa.Column("question_json", sa.JSON(), nullable=True),
        sa.Column("artifact_name", sa.String(length=120), nullable=True),
        sa.Column("artifact_step_id", sa.String(length=120), nullable=True),
        sa.Column("workflow_draft_revision_id", sa.String(length=36), nullable=True),
        sa.Column("sync_error", sa.Text(), nullable=True),
        sa.Column("finished_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            "length(request_hash) = 64",
            name="ck_agent_turn_projections_request_hash",
        ),
        sa.ForeignKeyConstraint(
            ["conversation_id"],
            ["agent_conversations.id"],
            name="fk_agent_turn_projections_conversation_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["workflow_draft_revision_id"],
            ["workflow_draft_revisions.id"],
            name="fk_agent_turn_projections_workflow_draft_revision_id",
            ondelete="SET NULL",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "conversation_id",
            "idempotency_key",
            name="uq_agent_turn_projections_conversation_key",
        ),
        sa.UniqueConstraint(
            "harness_turn_id",
            name="uq_agent_turn_projections_harness_turn_id",
        ),
        sa.UniqueConstraint(
            "workflow_draft_revision_id",
            name="uq_agent_turn_projections_workflow_draft_revision_id",
        ),
    )
    op.create_index(
        "ix_agent_turn_projections_conversation_created",
        "agent_turn_projections",
        ["conversation_id", "created_at", "id"],
    )
    op.create_index(
        "ix_agent_turn_projections_status",
        "agent_turn_projections",
        ["status"],
    )

    op.create_table(
        "agent_tool_mutations",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("conversation_id", sa.String(length=36), nullable=False),
        sa.Column("tool_name", sa.String(length=120), nullable=False),
        sa.Column("idempotency_key", sa.String(length=200), nullable=False),
        sa.Column("request_hash", sa.String(length=64), nullable=False),
        sa.Column("asset_id", sa.String(length=36), nullable=False),
        sa.Column("expected_display_name", sa.String(length=255), nullable=False),
        sa.Column("target_display_name", sa.String(length=255), nullable=False),
        sa.Column("status", AGENT_TOOL_MUTATION_STATUS, nullable=False),
        sa.Column("result_json", sa.JSON(), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            "length(request_hash) = 64",
            name="ck_agent_tool_mutations_request_hash",
        ),
        sa.ForeignKeyConstraint(
            ["conversation_id"],
            ["agent_conversations.id"],
            name="fk_agent_tool_mutations_conversation_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["asset_id"],
            ["product_image_assets.id"],
            name="fk_agent_tool_mutations_asset_id",
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "conversation_id",
            "tool_name",
            "idempotency_key",
            name="uq_agent_tool_mutations_conversation_tool_key",
        ),
    )
    op.create_index(
        "ix_agent_tool_mutations_asset_id",
        "agent_tool_mutations",
        ["asset_id"],
    )


def downgrade() -> None:
    op.drop_index("ix_agent_tool_mutations_asset_id", table_name="agent_tool_mutations")
    op.drop_table("agent_tool_mutations")
    op.drop_index("ix_agent_turn_projections_status", table_name="agent_turn_projections")
    op.drop_index(
        "ix_agent_turn_projections_conversation_created",
        table_name="agent_turn_projections",
    )
    op.drop_table("agent_turn_projections")
    op.drop_index("ix_agent_conversations_product_status", table_name="agent_conversations")
    op.drop_table("agent_conversations")

    bind = op.get_bind()
    if bind.dialect.name == "postgresql":
        AGENT_TOOL_MUTATION_STATUS.drop(bind, checkfirst=True)
        AGENT_TURN_STATUS.drop(bind, checkfirst=True)
        AGENT_CONVERSATION_STATUS.drop(bind, checkfirst=True)
