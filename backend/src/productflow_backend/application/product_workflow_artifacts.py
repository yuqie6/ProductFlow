from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.application.product_workflow_context import _optional_config_text
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import (
    CopyStatus,
    SourceAssetKind,
    WorkflowNodeStatus,
    WorkflowNodeType,
)
from productflow_backend.infrastructure.db.models import (
    CopySet,
    Product,
    ProductWorkflow,
    SourceAsset,
    WorkflowNode,
)


@dataclass(frozen=True, slots=True)
class _GeneratedWorkflowImage:
    target_index: int
    content: bytes
    width: int
    height: int
    template_name: str
    mime_type: str


def _create_context_copy_set(
    session: Session,
    *,
    product: Product,
    product_context: dict[str, str | None],
    node: WorkflowNode,
) -> CopySet:
    instruction = _optional_config_text(node.config_json, "instruction")
    product_name = product_context["name"] or "自由创作"
    source_note = product_context["source_note"]
    selling_points = [
        item
        for item in [
            source_note,
            product_context["category"],
            instruction,
        ]
        if item
    ][:3]
    while len(selling_points) < 3:
        selling_points.append(instruction or product_name)
    headline = instruction or product_name
    title = product_name
    cta = ""
    copy_set = CopySet(
        product_id=product.id,
        creative_brief_id=None,
        status=CopyStatus.DRAFT,
        title=title,
        selling_points=selling_points,
        poster_headline=headline[:500],
        cta=cta,
        model_title=title,
        model_selling_points=selling_points,
        model_poster_headline=headline[:500],
        model_cta=cta,
        provider_name="workflow_context",
        model_name="product_context",
        prompt_version="v1",
    )
    session.add(copy_set)
    session.flush()
    product.updated_at = now_utc()
    return copy_set


def _image_asset_output(
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


def _copy_node_output(
    copy_set: CopySet,
    *,
    creative_brief_id: str | None,
    manual_edit: bool = False,
) -> dict[str, Any]:
    output: dict[str, Any] = {
        "copy_set_id": copy_set.id,
        "creative_brief_id": creative_brief_id,
        "title": copy_set.title,
        "poster_headline": copy_set.poster_headline,
        "selling_points": copy_set.selling_points,
        "cta": copy_set.cta,
        "summary": f"文案：{copy_set.poster_headline}",
    }
    if manual_edit:
        output["manual_edit"] = True
    return output


def _source_asset_for_poster_variant(
    session: Session,
    *,
    workflow: ProductWorkflow,
    poster_variant_id: str,
) -> SourceAsset | None:
    """Find the reference SourceAsset that was created alongside a workflow poster."""
    asset = session.scalar(
        select(SourceAsset)
        .where(
            SourceAsset.product_id == workflow.product_id,
            SourceAsset.kind == SourceAssetKind.REFERENCE_IMAGE,
            SourceAsset.source_poster_variant_id == poster_variant_id,
        )
        .order_by(SourceAsset.created_at.desc())
    )
    if asset is not None:
        return asset

    for node in workflow.nodes:
        if node.node_type != WorkflowNodeType.IMAGE_GENERATION:
            continue
        output = node.output_json or {}
        raw_poster_ids = output.get("generated_poster_variant_ids")
        raw_source_asset_ids = output.get("filled_source_asset_ids")
        poster_ids = (
            [item for item in raw_poster_ids if isinstance(item, str)] if isinstance(raw_poster_ids, list) else []
        )
        source_asset_ids = (
            [item for item in raw_source_asset_ids if isinstance(item, str)]
            if isinstance(raw_source_asset_ids, list)
            else []
        )
        for poster_id, source_asset_id in zip(poster_ids, source_asset_ids, strict=False):
            if poster_id != poster_variant_id:
                continue
            asset = session.get(SourceAsset, source_asset_id)
            if (
                asset is not None
                and asset.product_id == workflow.product_id
                and asset.kind == SourceAssetKind.REFERENCE_IMAGE
            ):
                asset.source_poster_variant_id = poster_variant_id
                session.flush()
                return asset
    for node in workflow.nodes:
        if node.node_type != WorkflowNodeType.REFERENCE_IMAGE:
            continue
        output = node.output_json or {}
        if output.get("source_poster_variant_id") != poster_variant_id:
            continue
        raw_source_asset_ids = output.get("source_asset_ids")
        source_asset_ids = (
            [item for item in raw_source_asset_ids if isinstance(item, str)]
            if isinstance(raw_source_asset_ids, list)
            else []
        )
        source_asset_id = source_asset_ids[0] if source_asset_ids else None
        if source_asset_id is None:
            continue
        asset = session.get(SourceAsset, source_asset_id)
        if (
            asset is not None
            and asset.product_id == workflow.product_id
            and asset.kind == SourceAssetKind.REFERENCE_IMAGE
        ):
            asset.source_poster_variant_id = poster_variant_id
            session.flush()
            return asset
    return None


def _fill_reference_node(
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
    node.output_json = _image_asset_output(
        [asset],
        summary="已填充参考图",
        role=_optional_config_text(config, "role"),
        label=_optional_config_text(config, "label"),
    )
    if source_poster_variant_id:
        node.output_json["source_poster_variant_id"] = source_poster_variant_id
    node.status = WorkflowNodeStatus.SUCCEEDED
    node.failure_reason = None
    node.last_run_at = now_utc()
