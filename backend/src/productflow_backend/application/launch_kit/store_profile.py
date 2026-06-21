from __future__ import annotations

from datetime import UTC, datetime
from typing import Any

from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.application.launch_kit.payloads import StoreProfilePayload
from productflow_backend.infrastructure.db.models import StoreProfile


def get_store_profile_payload(session: Session) -> StoreProfilePayload:
    row = session.scalars(select(StoreProfile).order_by(StoreProfile.created_at.asc()).limit(1)).first()
    if row is None:
        return StoreProfilePayload()
    return StoreProfilePayload.model_validate(row.profile_json or {})


def get_store_profile_json(session: Session) -> dict[str, Any]:
    return get_store_profile_payload(session).model_dump(mode="json")


def save_store_profile(session: Session, *, profile: StoreProfilePayload) -> StoreProfilePayload:
    row = session.scalars(select(StoreProfile).order_by(StoreProfile.created_at.asc()).limit(1)).first()
    now = datetime.now(UTC)
    if row is None:
        row = StoreProfile(schema_version=profile.schema_version, profile_json=profile.model_dump(mode="json"))
        session.add(row)
    else:
        row.schema_version = profile.schema_version
        row.profile_json = profile.model_dump(mode="json")
        row.updated_at = now
    session.commit()
    return get_store_profile_payload(session)
