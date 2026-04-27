from __future__ import annotations

from pathlib import Path
from typing import Any

from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.application.contracts import ReferenceImageInput
from productflow_backend.config import normalize_image_generation_size
from productflow_backend.domain.enums import (
    PosterKind,
    SourceAssetKind,
    WorkflowNodeType,
)
from productflow_backend.infrastructure.db.models import (
    PosterVariant,
    Product,
    ProductWorkflow,
    SourceAsset,
    WorkflowNode,
)
from productflow_backend.infrastructure.storage import LocalStorage


def _find_source_asset(product: Product) -> SourceAsset | None:
    return next((asset for asset in product.source_assets if asset.kind == SourceAssetKind.ORIGINAL_IMAGE), None)


def _product_context_values(product: Product, node: WorkflowNode | None = None) -> dict[str, str | None]:
    config = node.config_json if node is not None else {}
    return {
        "name": _configured_text(config, "name", fallback=product.name) or product.name,
        "category": _configured_text(config, "category", fallback=product.category),
        "price": _configured_text(config, "price", fallback=str(product.price) if product.price is not None else None),
        "source_note": _configured_text(config, "source_note", fallback=product.source_note),
    }


def _empty_product_context() -> dict[str, str | None]:
    return {"name": None, "category": None, "price": None, "source_note": None}


def _effective_product_context(workflow: ProductWorkflow, target_node_id: str) -> dict[str, str | None]:
    incoming_context_nodes = [
        node
        for edge in workflow.edges
        for node in workflow.nodes
        if edge.target_node_id == target_node_id
        and edge.source_node_id == node.id
        and node.node_type == WorkflowNodeType.PRODUCT_CONTEXT
    ]
    if not incoming_context_nodes:
        return _empty_product_context()

    product = workflow.product
    fallback_context = _product_context_values(product)
    node = sorted(incoming_context_nodes, key=lambda item: item.last_run_at or item.updated_at, reverse=True)[0]
    output = node.output_json or {}
    return {
        "name": _configured_text(
            node.config_json,
            "name",
            fallback=_output_text(output, "name", fallback=fallback_context["name"]),
        ),
        "category": _configured_text(
            node.config_json,
            "category",
            fallback=_output_text(output, "category", fallback=fallback_context["category"]),
        ),
        "price": _configured_text(
            node.config_json,
            "price",
            fallback=_output_text(output, "price", fallback=fallback_context["price"]),
        ),
        "source_note": _configured_text(
            node.config_json,
            "source_note",
            fallback=_output_text(output, "source_note", fallback=fallback_context["source_note"]),
        ),
    }


def _configured_text(config: dict[str, Any], key: str, *, fallback: str | None = None) -> str | None:
    if key not in config:
        return fallback
    value = config.get(key)
    if value is None:
        return None
    if isinstance(value, str):
        return value.strip() or None
    return str(value)


def _output_text(output: dict[str, Any], key: str, *, fallback: str | None = None) -> str | None:
    value = output.get(key)
    if isinstance(value, str):
        return value.strip() or None
    return fallback


def _source_asset_ids_from_config(config: dict[str, Any]) -> list[str]:
    raw = config.get("source_asset_ids")
    if isinstance(raw, str):
        return [raw]
    if isinstance(raw, list):
        return [item for item in raw if isinstance(item, str)]
    single = config.get("source_asset_id")
    return [single] if isinstance(single, str) else []


def _optional_config_text(config: dict[str, Any], key: str) -> str | None:
    value = config.get(key)
    if not isinstance(value, str):
        return None
    normalized = value.strip()
    return normalized or None


def _poster_kind_from_config(config: dict[str, Any]) -> PosterKind:
    raw = config.get("poster_kind")
    if raw is None:
        return PosterKind.MAIN_IMAGE
    try:
        return PosterKind(str(raw))
    except ValueError as exc:
        raise ValueError("生图节点包含不支持的图片类型") from exc


def _image_size_from_config(config: dict[str, Any]) -> str | None:
    raw = config.get("size")
    if raw is None or (isinstance(raw, str) and not raw.strip()):
        return None
    return normalize_image_generation_size(raw, label="生图尺寸")


def _image_tool_options_from_config(config: dict[str, Any]) -> dict[str, Any] | None:
    raw = config.get("tool_options")
    if not isinstance(raw, dict):
        return None
    normalized = {
        str(key): value
        for key, value in raw.items()
        if value is not None and not (isinstance(value, str) and not value.strip())
    }
    return normalized or None


class _IncomingContext:
    def __init__(self) -> None:
        self.copy_set_id: str | None = None
        self.image_asset_ids: list[str] = []
        self.poster_variant_ids: list[str] = []
        self.text_contexts: list[str] = []
        self.text_sources: list[dict[str, str]] = []

    def append_text(self, *, node: WorkflowNode, label: str, text: str) -> None:
        normalized = text.strip()
        if not normalized or normalized in self.text_contexts:
            return
        self.text_contexts.append(normalized)
        self.text_sources.append(
            {
                "node_id": node.id,
                "node_type": node.node_type.value,
                "node_title": node.title,
                "label": label,
                "text": normalized,
            }
        )


def _collect_incoming_context(workflow: ProductWorkflow, node_id: str) -> _IncomingContext:
    context = _IncomingContext()
    ordered_edges = sorted(
        [edge for edge in workflow.edges if edge.target_node_id == node_id],
        key=lambda item: (item.created_at, item.id),
    )
    incoming_sources = list(dict.fromkeys(edge.source_node_id for edge in ordered_edges))
    nodes_by_id = {node.id: node for node in workflow.nodes}
    candidates = [nodes_by_id[source_id] for source_id in incoming_sources if source_id in nodes_by_id]
    for candidate in candidates:
        output = candidate.output_json or {}
        if context.copy_set_id is None and isinstance(output.get("copy_set_id"), str):
            context.copy_set_id = output["copy_set_id"]
        if candidate.node_type in {WorkflowNodeType.REFERENCE_IMAGE, WorkflowNodeType.IMAGE_GENERATION}:
            for key in ("source_asset_ids", "image_asset_ids", "reference_asset_ids"):
                raw_ids = output.get(key)
                if isinstance(raw_ids, list):
                    context.image_asset_ids.extend(item for item in raw_ids if isinstance(item, str))
                elif isinstance(raw_ids, str):
                    context.image_asset_ids.append(raw_ids)
            raw_poster_ids = output.get("poster_variant_ids")
            if isinstance(raw_poster_ids, list):
                context.poster_variant_ids.extend(item for item in raw_poster_ids if isinstance(item, str))
            elif isinstance(raw_poster_ids, str):
                context.poster_variant_ids.append(raw_poster_ids)
            raw_images = output.get("images")
            images = raw_images if isinstance(raw_images, list) else []
            for image in images:
                if not isinstance(image, dict):
                    continue
                label = str(image.get("label") or image.get("filename") or candidate.title)
                role = str(image.get("role") or "参考图")
                filename = str(image.get("filename") or "")
                suffix = f"，文件：{filename}" if filename else ""
                context.append_text(node=candidate, label="参考图", text=f"参考图：{label}（角色：{role}{suffix}）")
        if candidate.node_type == WorkflowNodeType.COPY_GENERATION:
            title = _output_text(output, "title")
            poster_headline = _output_text(output, "poster_headline")
            cta = _output_text(output, "cta")
            selling_points = output.get("selling_points")
            point_text = ""
            if isinstance(selling_points, list):
                point_text = "；".join(
                    item.strip() for item in selling_points if isinstance(item, str) and item.strip()
                )
            copy_parts = [
                f"标题：{title}" if title else "",
                f"主标题：{poster_headline}" if poster_headline else "",
                f"卖点：{point_text}" if point_text else "",
                f"CTA：{cta}" if cta else "",
            ]
            context.append_text(
                node=candidate,
                label="文案",
                text="；".join(part for part in copy_parts if part),
            )
        elif candidate.node_type == WorkflowNodeType.PRODUCT_CONTEXT:
            product_source_asset_ids = _source_asset_ids_from_config(output)
            if not product_source_asset_ids:
                product_source = _find_source_asset(workflow.product)
                if product_source is not None:
                    product_source_asset_ids = [product_source.id]
            context.image_asset_ids.extend(product_source_asset_ids)
            product_source_assets = [
                asset for asset in workflow.product.source_assets if asset.id in product_source_asset_ids
            ]
            if product_source_assets:
                image_labels = "、".join(asset.original_filename or "商品原图" for asset in product_source_assets)
                context.append_text(node=candidate, label="商品图", text=f"商品图：{image_labels}")
            product_context = _product_context_values(workflow.product, candidate)
            product_parts = [
                f"商品：{product_context['name']}" if product_context["name"] else "",
                f"类目：{product_context['category']}" if product_context["category"] else "",
                f"价格：{product_context['price']}" if product_context["price"] else "",
                f"描述：{product_context['source_note']}" if product_context["source_note"] else "",
            ]
            context.append_text(
                node=candidate,
                label="商品资料",
                text="；".join(part for part in product_parts if part),
            )
        else:
            summary = _output_text(output, "summary")
            if summary:
                context.append_text(node=candidate, label="摘要", text=summary)
    context.image_asset_ids = list(dict.fromkeys(context.image_asset_ids))
    context.poster_variant_ids = list(dict.fromkeys(context.poster_variant_ids))
    return context


def _reference_assets_for_image_generation(
    session: Session,
    workflow: ProductWorkflow,
    incoming_source_asset_ids: list[str],
    incoming_poster_variant_ids: list[str],
) -> list[SourceAsset]:
    product = workflow.product
    assets: list[SourceAsset] = []
    if incoming_source_asset_ids:
        fetched = list(session.scalars(select(SourceAsset).where(SourceAsset.id.in_(incoming_source_asset_ids))))
        assets.extend(asset for asset in fetched if asset.product_id == product.id)
    if incoming_poster_variant_ids:
        posters = list(session.scalars(select(PosterVariant).where(PosterVariant.id.in_(incoming_poster_variant_ids))))
        for poster in posters:
            assets.append(
                SourceAsset(
                    id=poster.id,
                    product_id=poster.product_id,
                    kind=SourceAssetKind.REFERENCE_IMAGE,
                    original_filename=f"{poster.kind.value}.png",
                    mime_type=poster.mime_type,
                    storage_path=poster.storage_path,
                )
            )
    return list({asset.storage_path: asset for asset in assets}.values())


def _reference_image_inputs_for_copy(
    session: Session,
    *,
    workflow: ProductWorkflow,
    node_id: str,
    storage: LocalStorage,
) -> list[ReferenceImageInput]:
    nodes_by_id = {node.id: node for node in workflow.nodes}
    reference_nodes = [
        nodes_by_id[edge.source_node_id]
        for edge in workflow.edges
        if edge.target_node_id == node_id
        and edge.source_node_id in nodes_by_id
        and nodes_by_id[edge.source_node_id].node_type == WorkflowNodeType.REFERENCE_IMAGE
    ]
    inputs: list[ReferenceImageInput] = []
    seen_asset_ids: set[str] = set()
    for reference_node in reference_nodes:
        asset_ids = list(
            dict.fromkeys(
                [
                    *_source_asset_ids_from_config(reference_node.config_json or {}),
                    *_source_asset_ids_from_config(reference_node.output_json or {}),
                ]
            )
        )
        if not asset_ids:
            continue
        assets = list(session.scalars(select(SourceAsset).where(SourceAsset.id.in_(asset_ids))))
        role = _optional_config_text(reference_node.config_json or {}, "role")
        label = _optional_config_text(reference_node.config_json or {}, "label") or reference_node.title
        for asset in assets:
            if asset.product_id != workflow.product_id or asset.id in seen_asset_ids:
                continue
            seen_asset_ids.add(asset.id)
            inputs.append(
                ReferenceImageInput(
                    path=Path(storage.resolve(asset.storage_path)),
                    mime_type=asset.mime_type,
                    filename=asset.original_filename,
                    role=role,
                    label=label,
                )
            )
    return inputs


def _downstream_reference_nodes(workflow: ProductWorkflow, node_id: str) -> list[WorkflowNode]:
    target_ids = list(dict.fromkeys(edge.target_node_id for edge in workflow.edges if edge.source_node_id == node_id))
    nodes_by_id = {node.id: node for node in workflow.nodes}
    return [
        nodes_by_id[target_id]
        for target_id in target_ids
        if target_id in nodes_by_id and nodes_by_id[target_id].node_type == WorkflowNodeType.REFERENCE_IMAGE
    ]


def _image_instruction_with_context(node: WorkflowNode, text_contexts: list[str]) -> str | None:
    instruction = _optional_config_text(node.config_json, "instruction")
    compact_contexts = [item for item in text_contexts if item and item != instruction][:8]
    if not compact_contexts:
        return instruction
    joined = "；".join(compact_contexts)
    if instruction:
        return f"{instruction}\n上游文本上下文：{joined}"
    return f"上游文本上下文：{joined}"


def _instruction_with_upstream_text(instruction: str | None, incoming_context: _IncomingContext) -> str | None:
    compact_contexts = [item for item in incoming_context.text_contexts if item and item != instruction][:8]
    if not compact_contexts:
        return instruction
    joined = "；".join(compact_contexts)
    if instruction:
        return f"{instruction}\n上游文本上下文：{joined}"
    return f"上游文本上下文：{joined}"
