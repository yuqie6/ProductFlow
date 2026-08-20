from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass
from typing import Any, Literal

from pydantic import ValidationError

from productflow_backend.application.media_objects import VerifiedImageMetadata
from productflow_backend.application.workflow_drafts.contracts import DeliverySpec
from productflow_backend.domain.enums import JobStatus
from productflow_backend.domain.errors import BusinessValidationError

DELIVERY_RENDITION_SPEC_SCHEMA_VERSION = 1
DELIVERY_RENDITION_QUALITY_LEVELS = (95, 90, 85, 80, 75, 70, 60, 50, 40, 30, 20, 10, 5, 1)

DELIVERY_FORMAT_MIME_TYPES = {
    "png": "image/png",
    "jpeg": "image/jpeg",
    "webp": "image/webp",
}
DELIVERY_FORMAT_EXTENSIONS = {
    "png": ".png",
    "jpeg": ".jpg",
    "webp": ".webp",
}

DeliveryRenditionStatus = Literal[
    JobStatus.QUEUED,
    JobStatus.RUNNING,
    JobStatus.SUCCEEDED,
    JobStatus.FAILED,
]


@dataclass(frozen=True, slots=True)
class NormalizedDeliverySpec:
    spec: DeliverySpec
    payload: dict[str, Any]
    spec_hash: str


@dataclass(frozen=True, slots=True)
class RenderedDeliveryRendition:
    bytes_data: bytes
    metadata: VerifiedImageMetadata


def normalize_delivery_spec(value: DeliverySpec | dict[str, Any]) -> NormalizedDeliverySpec:
    try:
        spec = value if isinstance(value, DeliverySpec) else DeliverySpec.model_validate(value)
    except ValidationError as exc:
        raise BusinessValidationError("DeliverySpec 不符合 schema") from exc
    payload = spec.model_dump(mode="json")
    canonical = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return NormalizedDeliverySpec(
        spec=spec,
        payload=payload,
        spec_hash=hashlib.sha256(canonical).hexdigest(),
    )
