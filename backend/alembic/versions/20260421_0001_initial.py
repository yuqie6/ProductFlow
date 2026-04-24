"""initial schema"""

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

revision = "20260421_0001"
down_revision = None
branch_labels = None
depends_on = None


source_asset_kind = postgresql.ENUM(
    "original_image",
    "reference_image",
    "processed_product_image",
    name="sourceassetkind",
    create_type=False,
)
job_kind = postgresql.ENUM("copy_generation", "poster_generation", name="jobkind", create_type=False)
job_status = postgresql.ENUM("queued", "running", "succeeded", "failed", name="jobstatus", create_type=False)
copy_status = postgresql.ENUM("draft", "confirmed", name="copystatus", create_type=False)
poster_kind = postgresql.ENUM("main_image", "promo_poster", name="posterkind", create_type=False)


def upgrade() -> None:
    bind = op.get_bind()
    source_asset_kind.create(bind, checkfirst=True)
    job_kind.create(bind, checkfirst=True)
    job_status.create(bind, checkfirst=True)
    copy_status.create(bind, checkfirst=True)
    poster_kind.create(bind, checkfirst=True)

    op.create_table(
        "products",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("name", sa.String(length=255), nullable=False),
        sa.Column("category", sa.String(length=120), nullable=True),
        sa.Column("price", sa.Numeric(10, 2), nullable=True),
        sa.Column("source_note", sa.Text(), nullable=True),
        sa.Column("current_confirmed_copy_set_id", sa.String(length=36), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.PrimaryKeyConstraint("id"),
    )

    op.create_table(
        "creative_briefs",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("payload", sa.JSON(), nullable=False),
        sa.Column("provider_name", sa.String(length=50), nullable=False),
        sa.Column("model_name", sa.String(length=100), nullable=False),
        sa.Column("prompt_version", sa.String(length=32), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["product_id"], ["products.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
    )

    op.create_table(
        "copy_sets",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("creative_brief_id", sa.String(length=36), nullable=True),
        sa.Column("status", copy_status, nullable=False),
        sa.Column("title", sa.Text(), nullable=False),
        sa.Column("selling_points", sa.JSON(), nullable=False),
        sa.Column("poster_headline", sa.Text(), nullable=False),
        sa.Column("cta", sa.Text(), nullable=False),
        sa.Column("model_title", sa.Text(), nullable=False),
        sa.Column("model_selling_points", sa.JSON(), nullable=False),
        sa.Column("model_poster_headline", sa.Text(), nullable=False),
        sa.Column("model_cta", sa.Text(), nullable=False),
        sa.Column("provider_name", sa.String(length=50), nullable=False),
        sa.Column("model_name", sa.String(length=100), nullable=False),
        sa.Column("prompt_version", sa.String(length=32), nullable=False),
        sa.Column("edited_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("confirmed_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["creative_brief_id"], ["creative_briefs.id"], ondelete="SET NULL"),
        sa.ForeignKeyConstraint(["product_id"], ["products.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
    )

    if bind.dialect.name != "sqlite":
        op.create_foreign_key(
            "fk_products_current_confirmed_copy_set_id",
            "products",
            "copy_sets",
            ["current_confirmed_copy_set_id"],
            ["id"],
            ondelete="SET NULL",
        )

    op.create_table(
        "source_assets",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("kind", source_asset_kind, nullable=False),
        sa.Column("original_filename", sa.String(length=255), nullable=False),
        sa.Column("mime_type", sa.String(length=100), nullable=False),
        sa.Column("storage_path", sa.String(length=500), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["product_id"], ["products.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
    )

    op.create_table(
        "poster_variants",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("copy_set_id", sa.String(length=36), nullable=False),
        sa.Column("kind", poster_kind, nullable=False),
        sa.Column("template_name", sa.String(length=100), nullable=False),
        sa.Column("mime_type", sa.String(length=50), nullable=False),
        sa.Column("storage_path", sa.String(length=500), nullable=False),
        sa.Column("width", sa.Integer(), nullable=False),
        sa.Column("height", sa.Integer(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["copy_set_id"], ["copy_sets.id"], ondelete="CASCADE"),
        sa.ForeignKeyConstraint(["product_id"], ["products.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
    )

    op.create_table(
        "job_runs",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("kind", job_kind, nullable=False),
        sa.Column("status", job_status, nullable=False),
        sa.Column("target_poster_kind", poster_kind, nullable=True),
        sa.Column("failure_reason", sa.Text(), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("started_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("finished_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("copy_set_id", sa.String(length=36), nullable=True),
        sa.Column("poster_variant_id", sa.String(length=36), nullable=True),
        sa.Column("attempts", sa.Integer(), nullable=False),
        sa.Column("is_retryable", sa.Boolean(), nullable=False),
        sa.ForeignKeyConstraint(["copy_set_id"], ["copy_sets.id"], ondelete="SET NULL"),
        sa.ForeignKeyConstraint(["poster_variant_id"], ["poster_variants.id"], ondelete="SET NULL"),
        sa.ForeignKeyConstraint(["product_id"], ["products.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
    )


def downgrade() -> None:
    op.drop_table("job_runs")
    op.drop_table("poster_variants")
    op.drop_table("source_assets")
    bind = op.get_bind()
    if bind.dialect.name != "sqlite":
        op.drop_constraint("fk_products_current_confirmed_copy_set_id", "products", type_="foreignkey")
    op.drop_table("copy_sets")
    op.drop_table("creative_briefs")
    op.drop_table("products")

    poster_kind.drop(bind, checkfirst=True)
    copy_status.drop(bind, checkfirst=True)
    job_status.drop(bind, checkfirst=True)
    job_kind.drop(bind, checkfirst=True)
    source_asset_kind.drop(bind, checkfirst=True)
