"""add global media library organization drafts

Revision ID: 20260817_0054
Revises: 20260817_0053
Create Date: 2026-08-17
"""

from __future__ import annotations

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

revision = "20260817_0054"
down_revision = "20260817_0053"
branch_labels = None
depends_on = None

LIBRARY_ORGANIZATION_DRAFT_STATUS = postgresql.ENUM(
    "awaiting_confirmation",
    "confirmed",
    "failed",
    "cancelled",
    name="libraryorganizationdraftstatus",
    create_type=False,
)

DRAFT_CURRENT_REVISION_FK = "fk_library_organization_drafts_current_revision_id"
DRAFT_CONFIRMED_REVISION_FK = "fk_library_organization_drafts_confirmed_revision_id"
TURN_REVISION_FK = "fk_agent_turn_projections_library_organization_draft_revision_id"
TURN_REVISION_UNIQUE = "uq_agent_turn_projections_library_organization_draft_revision_id"


def _alter_existing_table(table_name: str):
    bind = op.get_bind()
    return op.batch_alter_table(table_name, recreate="always" if bind.dialect.name == "sqlite" else "auto")


def upgrade() -> None:
    LIBRARY_ORGANIZATION_DRAFT_STATUS.create(op.get_bind(), checkfirst=True)
    op.create_table(
        "library_organization_drafts",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("conversation_id", sa.String(length=36), nullable=False),
        sa.Column("status", LIBRARY_ORGANIZATION_DRAFT_STATUS, nullable=False),
        sa.Column("current_revision_id", sa.String(length=36), nullable=True),
        sa.Column("confirmed_revision_id", sa.String(length=36), nullable=True),
        sa.Column("confirmation_idempotency_key", sa.String(length=200), nullable=True),
        sa.Column("confirmation_request_hash", sa.String(length=64), nullable=True),
        sa.Column("confirmation_result_json", sa.JSON(), nullable=True),
        sa.Column("confirmed_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            "(confirmation_idempotency_key IS NULL AND confirmation_request_hash IS NULL) OR "
            "(confirmation_idempotency_key IS NOT NULL AND length(confirmation_idempotency_key) > 0 "
            "AND confirmation_request_hash IS NOT NULL AND length(confirmation_request_hash) = 64)",
            name="ck_library_organization_drafts_confirmation_pair",
        ),
        sa.ForeignKeyConstraint(
            ["conversation_id"],
            ["agent_conversations.id"],
            name="fk_library_organization_drafts_conversation_id",
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("conversation_id", name="uq_library_organization_drafts_conversation_id"),
    )
    op.create_index(
        "ix_library_organization_drafts_status_updated",
        "library_organization_drafts",
        ["status", "updated_at", "id"],
    )

    op.create_table(
        "library_organization_draft_revisions",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("draft_id", sa.String(length=36), nullable=False),
        sa.Column("version", sa.Integer(), nullable=False),
        sa.Column("schema_version", sa.Integer(), nullable=False),
        sa.Column("payload_json", sa.JSON(), nullable=False),
        sa.Column("payload_hash", sa.String(length=64), nullable=False),
        sa.Column("source_turn_id", sa.String(length=120), nullable=True),
        sa.Column("source_artifact_step_id", sa.String(length=120), nullable=True),
        sa.Column("confirmed_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint("version > 0", name="ck_library_organization_draft_revisions_positive_version"),
        sa.CheckConstraint("schema_version = 1", name="ck_library_organization_draft_revisions_schema_version"),
        sa.CheckConstraint("length(payload_hash) = 64", name="ck_library_organization_draft_revisions_payload_hash"),
        sa.CheckConstraint(
            "(source_turn_id IS NULL AND source_artifact_step_id IS NULL) OR "
            "(source_turn_id IS NOT NULL AND source_artifact_step_id IS NOT NULL)",
            name="ck_library_organization_draft_revisions_artifact_origin_pair",
        ),
        sa.ForeignKeyConstraint(
            ["draft_id"],
            ["library_organization_drafts.id"],
            name="fk_library_organization_draft_revisions_draft_id",
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "draft_id",
            "version",
            name="uq_library_organization_draft_revisions_draft_version",
        ),
        sa.UniqueConstraint(
            "draft_id",
            "source_turn_id",
            "source_artifact_step_id",
            name="uq_library_organization_draft_revisions_artifact_origin",
        ),
    )

    with _alter_existing_table("library_organization_drafts") as batch_op:
        batch_op.create_foreign_key(
            DRAFT_CURRENT_REVISION_FK,
            "library_organization_draft_revisions",
            ["current_revision_id"],
            ["id"],
            ondelete="SET NULL",
        )
        batch_op.create_foreign_key(
            DRAFT_CONFIRMED_REVISION_FK,
            "library_organization_draft_revisions",
            ["confirmed_revision_id"],
            ["id"],
            ondelete="SET NULL",
        )

    with _alter_existing_table("agent_turn_projections") as batch_op:
        batch_op.add_column(sa.Column("library_organization_draft_revision_id", sa.String(length=36), nullable=True))
        batch_op.create_foreign_key(
            TURN_REVISION_FK,
            "library_organization_draft_revisions",
            ["library_organization_draft_revision_id"],
            ["id"],
            ondelete="SET NULL",
        )
        batch_op.create_unique_constraint(TURN_REVISION_UNIQUE, ["library_organization_draft_revision_id"])


def downgrade() -> None:
    with _alter_existing_table("agent_turn_projections") as batch_op:
        batch_op.drop_constraint(TURN_REVISION_UNIQUE, type_="unique")
        batch_op.drop_constraint(TURN_REVISION_FK, type_="foreignkey")
        batch_op.drop_column("library_organization_draft_revision_id")

    with _alter_existing_table("library_organization_drafts") as batch_op:
        batch_op.drop_constraint(DRAFT_CONFIRMED_REVISION_FK, type_="foreignkey")
        batch_op.drop_constraint(DRAFT_CURRENT_REVISION_FK, type_="foreignkey")

    op.drop_table("library_organization_draft_revisions")
    op.drop_index(
        "ix_library_organization_drafts_status_updated",
        table_name="library_organization_drafts",
    )
    op.drop_table("library_organization_drafts")
    bind = op.get_bind()
    if bind.dialect.name == "postgresql":
        LIBRARY_ORGANIZATION_DRAFT_STATUS.drop(bind, checkfirst=True)
