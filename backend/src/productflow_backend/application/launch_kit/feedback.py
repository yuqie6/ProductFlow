from __future__ import annotations

from datetime import UTC, datetime

from sqlalchemy.orm import Session

from productflow_backend.application.launch_kit.payloads import SellerFeedbackPayload
from productflow_backend.application.launch_kit.query import get_launch_kit
from productflow_backend.infrastructure.db.models import LaunchKit


def save_launch_kit_feedback(
    session: Session,
    *,
    launch_kit_id: str,
    feedback: SellerFeedbackPayload,
) -> LaunchKit:
    launch_kit = get_launch_kit(session, launch_kit_id)
    now = datetime.now(UTC)
    launch_kit.seller_feedback_json = feedback.model_dump(mode="json")
    if feedback.used:
        launch_kit.used_at = now
    launch_kit.updated_at = now
    session.commit()
    session.expire_all()
    return get_launch_kit(session, launch_kit_id)
