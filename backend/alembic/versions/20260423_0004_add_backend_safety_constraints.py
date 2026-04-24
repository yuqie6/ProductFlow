"""add backend safety constraints"""

import sqlalchemy as sa

from alembic import op

revision = "20260423_0004"
down_revision = "20260423_0003"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_index(
        "uq_job_runs_one_active_per_product_kind",
        "job_runs",
        ["product_id", "kind"],
        unique=True,
        postgresql_where=sa.text("status IN ('queued', 'running')"),
        sqlite_where=sa.text("status IN ('queued', 'running')"),
    )
    op.create_index(
        "uq_image_session_rounds_generated_asset_id",
        "image_session_rounds",
        ["generated_asset_id"],
        unique=True,
    )
    op.create_index(
        "uq_source_assets_one_original_per_product",
        "source_assets",
        ["product_id"],
        unique=True,
        postgresql_where=sa.text("kind = 'original_image'"),
        sqlite_where=sa.text("kind = 'original_image'"),
    )


def downgrade() -> None:
    op.drop_index("uq_source_assets_one_original_per_product", table_name="source_assets")
    op.drop_index("uq_image_session_rounds_generated_asset_id", table_name="image_session_rounds")
    op.drop_index("uq_job_runs_one_active_per_product_kind", table_name="job_runs")
