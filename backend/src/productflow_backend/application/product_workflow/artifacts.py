from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.application.contracts import BlocksCopyContent, CopyBlock, CopyPayloadV2
from productflow_backend.application.copy_payloads import (
    copy_payload_to_output,
    validate_copy_payload,
)
from productflow_backend.application.product_workflow.context import optional_config_text
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import (
    CopyStatus,
    SourceAssetKind,
    WorkflowNodeStatus,
)
from productflow_backend.infrastructure.db.models import (
    CopySet,
    Product,
    ProductWorkflow,
    SourceAsset,
    WorkflowNode,
)


@dataclass(frozen=True, slots=True)
class GeneratedWorkflowImage:
    target_index: int
    content: bytes
    width: int
    height: int
    template_name: str
    mime_type: str
    provider_name: str | None = None
    model_name: str | None = None
    provider_response_id: str | None = None
    provider_response_status: str | None = None
    provider_output_json: dict[str, Any] | None = None


def create_context_copy_set(
    session: Session,
    *,
    product: Product,
    product_context: dict[str, str | None],
    node: WorkflowNode,
) -> CopySet:
    instruction = optional_config_text(node.config_json, "instruction")
    product_name = product_context["name"] or "自由创作"
    source_note = product_context["source_note"]
    blocks = [
        CopyBlock(id="product-name", role="subject", label="商品", text=product_name),
        *[
            CopyBlock(id=f"context-{index}", role="context", label=f"上下文 {index}", text=item, priority=index)
            for index, item in enumerate(
                [
                    source_note,
                    product_context["category"],
                    instruction,
                ],
                start=1,
            )
            if item
        ],
    ]
    structured_payload = CopyPayloadV2(
        purpose="workflow_context",
        summary=instruction or product_name,
        content=BlocksCopyContent(blocks=blocks),
    )
    copy_set = CopySet(
        product_id=product.id,
        creative_brief_id=None,
        status=CopyStatus.DRAFT,
        structured_payload=structured_payload.model_dump(mode="json"),
        model_structured_payload=structured_payload.model_dump(mode="json"),
        provider_name="workflow_context",
        model_name="product_context",
        prompt_version="v1",
    )
    session.add(copy_set)
    session.flush()
    product.updated_at = now_utc()
    return copy_set


def image_asset_output(
    assets: list[SourceAsset],
    *,
    summary: str,
    role: str | None = None,
    label: str | None = None,
) -> dict[str, Any]:
    return {
        "source_asset_ids": [asset.id for asset in assets],
        "image_asset_ids": [asset.id for asset in assets],
        "images": [
            {
                "source_asset_id": asset.id,
                "filename": asset.original_filename,
                "mime_type": asset.mime_type,
                "role": role,
                "label": label,
            }
            for asset in assets
        ],
        "role": role,
        "label": label,
        "summary": summary,
    }


def copy_node_output(
    copy_set: CopySet,
    *,
    creative_brief_id: str | None,
    manual_edit: bool = False,
) -> dict[str, Any]:
    if not isinstance(copy_set.structured_payload, dict):
        raise ValueError("文案版本缺少 structured_payload")
    structured_payload = validate_copy_payload(copy_set.structured_payload)
    output: dict[str, Any] = {
        "copy_set_id": copy_set.id,
        "creative_brief_id": creative_brief_id,
        **copy_payload_to_output(structured_payload),
    }
    if manual_edit:
        output["manual_edit"] = True
    return output


def source_asset_for_poster_variant(
    session: Session,
    *,
    workflow: ProductWorkflow,
    poster_variant_id: str,
) -> SourceAsset | None:
    """Find the newest column-backed reference SourceAsset for a workflow poster."""
    return session.scalar(
        select(SourceAsset)
        .where(
            SourceAsset.product_id == workflow.product_id,
            SourceAsset.kind == SourceAssetKind.REFERENCE_IMAGE,
            SourceAsset.source_poster_variant_id == poster_variant_id,
        )
        .order_by(SourceAsset.created_at.desc(), SourceAsset.id.desc())
    )


def fill_reference_node(
    node: WorkflowNode,
    asset: SourceAsset,
    *,
    source_poster_variant_id: str | None = None,
) -> None:
    config = dict(node.config_json or {})
    config["source_asset_ids"] = [asset.id]
    config.setdefault("role", "reference")
    config.setdefault("label", node.title)
    if source_poster_variant_id:
        config["source_poster_variant_id"] = source_poster_variant_id
    else:
        config.pop("source_poster_variant_id", None)
    node.config_json = config
    node.output_json = image_asset_output(
        [asset],
        summary="已填充参考图",
        role=optional_config_text(config, "role"),
        label=optional_config_text(config, "label"),
    )
    if source_poster_variant_id:
        node.output_json["source_poster_variant_id"] = source_poster_variant_id
    node.status = WorkflowNodeStatus.SUCCEEDED
    node.failure_reason = None
    node.last_run_at = now_utc()
