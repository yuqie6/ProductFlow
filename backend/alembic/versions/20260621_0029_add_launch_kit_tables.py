"""add launch kit tables

Revision ID: 20260621_0029
Revises: 20260513_0028
Create Date: 2026-06-21
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260621_0029"
down_revision = "20260513_0028"
branch_labels = None
depends_on = None


def enum_values(*values: str) -> sa.Enum:
    return sa.Enum(*values, native_enum=False)


def upgrade() -> None:
    op.create_table(
        "launch_kits",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("target_platforms_json", sa.JSON(), nullable=False),
        sa.Column("category_key", sa.String(length=80), nullable=False),
        sa.Column("buyer_angle_key", sa.String(length=80), nullable=True),
        sa.Column("status", enum_values("draft", "generating", "ready", "failed", "archived"), nullable=False),
        sa.Column("source_references_json", sa.JSON(), nullable=False),
        sa.Column("generated_summary_json", sa.JSON(), nullable=True),
        sa.Column("selected_angle_json", sa.JSON(), nullable=True),
        sa.Column("export_snapshot_json", sa.JSON(), nullable=True),
        sa.Column("used_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("seller_feedback_json", sa.JSON(), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["product_id"], ["products.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index("ix_launch_kits_product_id", "launch_kits", ["product_id"])
    op.create_index("ix_launch_kits_status", "launch_kits", ["status"])
    op.create_index("ix_launch_kits_category_key", "launch_kits", ["category_key"])
    op.create_index("ix_launch_kits_updated_at", "launch_kits", ["updated_at"])

    op.create_table(
        "launch_kit_generation_tasks",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("launch_kit_id", sa.String(length=36), nullable=False),
        sa.Column("status", enum_values("queued", "running", "succeeded", "failed", "cancelled"), nullable=False),
        sa.Column(
            "progress_stage",
            enum_values(
                "extracting_facts",
                "applying_playbook",
                "applying_store_profile",
                "generating_angles",
                "generating_copy",
                "planning_images",
                "scoring",
                "exporting_optional_snapshot",
            ),
            nullable=True,
        ),
        sa.Column("attempt_count", sa.Integer(), nullable=False),
        sa.Column("failure_category", sa.String(length=80), nullable=True),
        sa.Column("failure_detail", sa.Text(), nullable=True),
        sa.Column("is_retryable", sa.Boolean(), nullable=False),
        sa.Column("is_cancelable", sa.Boolean(), nullable=False),
        sa.Column("provider_metadata_json", sa.JSON(), nullable=False),
        sa.Column("started_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("progress_updated_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("finished_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["launch_kit_id"], ["launch_kits.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index(
        "ix_launch_kit_generation_tasks_launch_kit_id",
        "launch_kit_generation_tasks",
        ["launch_kit_id"],
    )
    op.create_index("ix_launch_kit_generation_tasks_status", "launch_kit_generation_tasks", ["status"])
    op.create_index(
        "ix_launch_kit_generation_tasks_progress_stage",
        "launch_kit_generation_tasks",
        ["progress_stage"],
    )

    op.create_table(
        "launch_kit_variants",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("launch_kit_id", sa.String(length=36), nullable=False),
        sa.Column(
            "kind",
            enum_values("title", "description", "image_plan", "hashtag", "hook", "full_kit"),
            nullable=False,
        ),
        sa.Column("platform", enum_values("shopee", "tiktok_shop", "both"), nullable=False),
        sa.Column("content_json", sa.JSON(), nullable=False),
        sa.Column("score_json", sa.JSON(), nullable=False),
        sa.Column("selected", sa.Boolean(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["launch_kit_id"], ["launch_kits.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index("ix_launch_kit_variants_launch_kit_id", "launch_kit_variants", ["launch_kit_id"])
    op.create_index("ix_launch_kit_variants_kind_platform", "launch_kit_variants", ["kind", "platform"])
    op.create_index("ix_launch_kit_variants_selected", "launch_kit_variants", ["selected"])

    op.create_table(
        "launch_quality_scores",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("launch_kit_id", sa.String(length=36), nullable=False),
        sa.Column("score_json", sa.JSON(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["launch_kit_id"], ["launch_kits.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index("ix_launch_quality_scores_launch_kit_id", "launch_quality_scores", ["launch_kit_id"])

    op.create_table(
        "launch_kit_exports",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("launch_kit_id", sa.String(length=36), nullable=False),
        sa.Column("export_type", enum_values("markdown", "images_zip", "platform_text", "checklist"), nullable=False),
        sa.Column("storage_path", sa.String(length=500), nullable=True),
        sa.Column("status", enum_values("ready", "failed"), nullable=False),
        sa.Column("failure_reason", sa.Text(), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["launch_kit_id"], ["launch_kits.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index("ix_launch_kit_exports_launch_kit_id", "launch_kit_exports", ["launch_kit_id"])
    op.create_index("ix_launch_kit_exports_status", "launch_kit_exports", ["status"])

    op.create_table(
        "category_playbooks",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("key", sa.String(length=80), nullable=False),
        sa.Column("display_name", sa.String(length=120), nullable=False),
        sa.Column("schema_version", sa.Integer(), nullable=False),
        sa.Column("playbook_json", sa.JSON(), nullable=False),
        sa.Column("active", sa.Boolean(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index("uq_category_playbooks_key", "category_playbooks", ["key"], unique=True)
    op.create_index("ix_category_playbooks_active", "category_playbooks", ["active"])

    op.create_table(
        "store_profiles",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("schema_version", sa.Integer(), nullable=False),
        sa.Column("profile_json", sa.JSON(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.PrimaryKeyConstraint("id"),
    )


def downgrade() -> None:
    op.drop_table("store_profiles")
    op.drop_index("ix_category_playbooks_active", table_name="category_playbooks")
    op.drop_index("uq_category_playbooks_key", table_name="category_playbooks")
    op.drop_table("category_playbooks")
    op.drop_index("ix_launch_kit_exports_status", table_name="launch_kit_exports")
    op.drop_index("ix_launch_kit_exports_launch_kit_id", table_name="launch_kit_exports")
    op.drop_table("launch_kit_exports")
    op.drop_index("ix_launch_quality_scores_launch_kit_id", table_name="launch_quality_scores")
    op.drop_table("launch_quality_scores")
    op.drop_index("ix_launch_kit_variants_selected", table_name="launch_kit_variants")
    op.drop_index("ix_launch_kit_variants_kind_platform", table_name="launch_kit_variants")
    op.drop_index("ix_launch_kit_variants_launch_kit_id", table_name="launch_kit_variants")
    op.drop_table("launch_kit_variants")
    op.drop_index("ix_launch_kit_generation_tasks_progress_stage", table_name="launch_kit_generation_tasks")
    op.drop_index("ix_launch_kit_generation_tasks_status", table_name="launch_kit_generation_tasks")
    op.drop_index("ix_launch_kit_generation_tasks_launch_kit_id", table_name="launch_kit_generation_tasks")
    op.drop_table("launch_kit_generation_tasks")
    op.drop_index("ix_launch_kits_updated_at", table_name="launch_kits")
    op.drop_index("ix_launch_kits_category_key", table_name="launch_kits")
    op.drop_index("ix_launch_kits_status", table_name="launch_kits")
    op.drop_index("ix_launch_kits_product_id", table_name="launch_kits")
    op.drop_table("launch_kits")
