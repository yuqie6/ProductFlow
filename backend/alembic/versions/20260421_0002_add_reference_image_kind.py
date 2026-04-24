"""add reference image source asset kind"""

from alembic import op

revision = "20260421_0002"
down_revision = "20260421_0001"
branch_labels = None
depends_on = None


def upgrade() -> None:
    # Current checked-in 0001 already includes this enum value. Keep this
    # migration idempotent so older dev databases that were created before the
    # enum cleanup can still move through the same revision chain safely.
    bind = op.get_bind()
    if bind.dialect.name == "postgresql":
        op.execute("ALTER TYPE sourceassetkind ADD VALUE IF NOT EXISTS 'reference_image'")


def downgrade() -> None:
    # PostgreSQL enum value removal is not safely reversible in-place.
    pass
