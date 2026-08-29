"""add unknown to jobstatus

Revision ID: 20260829_0095
Revises: 20260827_0094
Create Date: 2026-08-29

连续生图任务在 provider 副作用无法证明时写 JobStatus.UNKNOWN。
PostgreSQL jobstatus 枚举原先只有 queued/running/succeeded/failed/cancelled。
"""

from __future__ import annotations

from alembic import op

revision = "20260829_0095"
down_revision = "20260827_0094"
branch_labels = None
depends_on = None


def upgrade() -> None:
    bind = op.get_bind()
    if bind.dialect.name == "postgresql":
        with op.get_context().autocommit_block():
            op.execute("ALTER TYPE jobstatus ADD VALUE IF NOT EXISTS 'unknown'")


def downgrade() -> None:
    raise RuntimeError("jobstatus 枚举值 unknown 不能安全移除")
