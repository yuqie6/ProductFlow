"""drop legacy job runs

Revision ID: 20260428_0017
Revises: 20260428_0016
Create Date: 2026-04-28
"""

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

revision = "20260428_0017"
down_revision = "20260428_0016"
branch_labels = None
depends_on = None


job_kind = postgresql.ENUM("copy_generation", "poster_generation", name="jobkind", create_type=False)
job_status = postgresql.ENUM("queued", "running", "succeeded", "failed", name="jobstatus", create_type=False)
poster_kind = postgresql.ENUM("main_image", "promo_poster", name="posterkind", create_type=False)


def upgrade() -> None:
    bind = op.get_bind()
    op.drop_index("uq_job_runs_one_active_per_product_kind", table_name="job_runs")
    op.drop_table("job_runs")
    if bind.dialect.name != "sqlite":
        job_kind.drop(bind, checkfirst=True)


def downgrade() -> None:
    bind = op.get_bind()
    if bind.dialect.name != "sqlite":
        job_kind.create(bind, checkfirst=True)
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
    op.create_index(
        "uq_job_runs_one_active_per_product_kind",
        "job_runs",
        ["product_id", "kind"],
        unique=True,
        postgresql_where=sa.text("status IN ('queued', 'running')"),
        sqlite_where=sa.text("status IN ('queued', 'running')"),
    )
