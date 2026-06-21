from __future__ import annotations

from sqlalchemy import desc, func, select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.domain.errors import NotFoundError
from productflow_backend.infrastructure.db.models import LaunchKit


def launch_kit_detail_query():
    return select(LaunchKit).options(
        selectinload(LaunchKit.product),
        selectinload(LaunchKit.tasks),
        selectinload(LaunchKit.variants),
        selectinload(LaunchKit.quality_scores),
        selectinload(LaunchKit.exports),
    )


def get_launch_kit(session: Session, launch_kit_id: str) -> LaunchKit:
    launch_kit = session.scalar(launch_kit_detail_query().where(LaunchKit.id == launch_kit_id))
    if launch_kit is None:
        raise NotFoundError("Launch kit does not exist")
    return launch_kit


def list_launch_kits(session: Session, *, page: int, page_size: int) -> tuple[list[LaunchKit], int]:
    page = max(page, 1)
    page_size = min(max(page_size, 1), 100)
    start = (page - 1) * page_size
    total = session.scalar(select(func.count()).select_from(LaunchKit)) or 0
    items = session.scalars(
        select(LaunchKit)
        .options(selectinload(LaunchKit.product), selectinload(LaunchKit.tasks), selectinload(LaunchKit.quality_scores))
        .order_by(desc(LaunchKit.updated_at))
        .offset(start)
        .limit(page_size)
    ).all()
    return list(items), total
