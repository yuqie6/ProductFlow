from __future__ import annotations

from sqlalchemy.orm import Session

from productflow_backend.application.launch_kit.payloads import SourceReferencePayload
from productflow_backend.application.launch_kit.playbooks import get_active_category_playbook
from productflow_backend.application.launch_kit.query import get_launch_kit
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.domain.launch_kits import LaunchKitPlatform, LaunchKitStatus
from productflow_backend.infrastructure.db.models import LaunchKit, Product


def _normalize_required(value: str, *, field_name: str, max_length: int) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError(f"{field_name} is required")
    if len(normalized) > max_length:
        raise BusinessValidationError(f"{field_name} must be at most {max_length} characters")
    return normalized


def _normalize_platforms(platforms: list[LaunchKitPlatform]) -> list[str]:
    if not platforms:
        raise BusinessValidationError("At least one target platform is required")
    values = [platform.value for platform in platforms]
    if LaunchKitPlatform.BOTH.value in values and len(values) > 1:
        return [LaunchKitPlatform.BOTH.value]
    return sorted(set(values))


def create_launch_kit(
    session: Session,
    *,
    product_name: str,
    category_key: str,
    target_platforms: list[LaunchKitPlatform],
    source_references: SourceReferencePayload | None = None,
) -> LaunchKit:
    category_key = _normalize_required(category_key, field_name="category_key", max_length=80)
    get_active_category_playbook(session, category_key)
    product = Product(
        name=_normalize_required(product_name, field_name="product_name", max_length=255),
        category=category_key,
    )
    session.add(product)
    session.flush()
    references = source_references or SourceReferencePayload(product_name=product.name)
    launch_kit = LaunchKit(
        product_id=product.id,
        target_platforms_json=_normalize_platforms(target_platforms),
        category_key=category_key,
        status=LaunchKitStatus.DRAFT,
        source_references_json=references.model_dump(mode="json"),
        generated_summary_json=None,
        selected_angle_json=None,
        export_snapshot_json=None,
        seller_feedback_json=None,
    )
    session.add(launch_kit)
    session.commit()
    session.expire_all()
    return get_launch_kit(session, launch_kit.id)
