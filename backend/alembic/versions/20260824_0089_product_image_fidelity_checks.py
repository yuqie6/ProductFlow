"""add immutable versioned product image fidelity checks

Revision ID: 20260824_0089
Revises: 20260824_0088
Create Date: 2026-08-24
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260824_0089"
down_revision = "20260824_0088"
branch_labels = None
depends_on = None


_OUTCOME_VALUES = "('pass', 'fail', 'not_applicable')"


def upgrade() -> None:
    op.create_table(
        "product_image_fidelity_checks",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("asset_id", sa.String(length=36), nullable=False),
        sa.Column("version", sa.Integer(), nullable=False),
        sa.Column("shape_fidelity", sa.String(length=20), nullable=False),
        sa.Column("color_material_fidelity", sa.String(length=20), nullable=False),
        sa.Column("logo_text_legibility", sa.String(length=20), nullable=False),
        sa.Column("text_policy_compliance", sa.String(length=20), nullable=False),
        sa.Column("notes", sa.Text(), nullable=True),
        sa.Column("checked_by", sa.String(length=80), nullable=False),
        sa.Column("idempotency_key", sa.String(length=120), nullable=False),
        sa.Column("request_hash", sa.String(length=64), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            "version >= 1",
            name="ck_product_image_fidelity_checks_positive_version",
        ),
        sa.CheckConstraint(
            f"shape_fidelity IN {_OUTCOME_VALUES}",
            name="ck_product_image_fidelity_checks_shape_outcome",
        ),
        sa.CheckConstraint(
            f"color_material_fidelity IN {_OUTCOME_VALUES}",
            name="ck_product_image_fidelity_checks_color_outcome",
        ),
        sa.CheckConstraint(
            f"logo_text_legibility IN {_OUTCOME_VALUES}",
            name="ck_product_image_fidelity_checks_logo_outcome",
        ),
        sa.CheckConstraint(
            f"text_policy_compliance IN {_OUTCOME_VALUES}",
            name="ck_product_image_fidelity_checks_text_policy_outcome",
        ),
        sa.CheckConstraint(
            "notes IS NULL OR length(notes) <= 4000",
            name="ck_product_image_fidelity_checks_notes_length",
        ),
        sa.CheckConstraint(
            "checked_by = 'administrator'",
            name="ck_product_image_fidelity_checks_checked_by_authority",
        ),
        sa.CheckConstraint(
            "length(idempotency_key) > 0",
            name="ck_product_image_fidelity_checks_idempotency_nonempty",
        ),
        sa.CheckConstraint(
            "length(request_hash) = 64",
            name="ck_product_image_fidelity_checks_request_hash",
        ),
        sa.ForeignKeyConstraint(
            ["product_id"],
            ["products.id"],
            name="fk_product_image_fidelity_checks_product_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["asset_id"],
            ["product_image_assets.id"],
            name="fk_product_image_fidelity_checks_asset_id",
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "asset_id",
            "version",
            name="uq_product_image_fidelity_checks_asset_version",
        ),
        sa.UniqueConstraint(
            "asset_id",
            "idempotency_key",
            name="uq_product_image_fidelity_checks_asset_idempotency",
        ),
    )
    op.create_index(
        "ix_product_image_fidelity_checks_asset_created",
        "product_image_fidelity_checks",
        ["asset_id", "created_at", "id"],
    )


def downgrade() -> None:
    bind = op.get_bind()
    referenced = bind.execute(
        sa.text("SELECT 1 FROM product_image_fidelity_checks LIMIT 1")
    ).first()
    if referenced is not None:
        raise RuntimeError("已有商品图片人工保真检查记录，不能 downgrade 丢失审计历史")

    op.drop_index(
        "ix_product_image_fidelity_checks_asset_created",
        table_name="product_image_fidelity_checks",
    )
    op.drop_table("product_image_fidelity_checks")
