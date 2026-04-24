"""add image session response metadata"""

import sqlalchemy as sa

from alembic import op

revision = "20260424_0005"
down_revision = "20260423_0004"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.add_column("image_session_rounds", sa.Column("provider_response_id", sa.String(length=128), nullable=True))
    op.add_column("image_session_rounds", sa.Column("previous_response_id", sa.String(length=128), nullable=True))
    op.add_column("image_session_rounds", sa.Column("image_generation_call_id", sa.String(length=128), nullable=True))
    op.add_column("image_session_rounds", sa.Column("provider_request_json", sa.JSON(), nullable=True))
    op.add_column("image_session_rounds", sa.Column("provider_output_json", sa.JSON(), nullable=True))


def downgrade() -> None:
    op.drop_column("image_session_rounds", "provider_output_json")
    op.drop_column("image_session_rounds", "provider_request_json")
    op.drop_column("image_session_rounds", "image_generation_call_id")
    op.drop_column("image_session_rounds", "previous_response_id")
    op.drop_column("image_session_rounds", "provider_response_id")
