from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.infrastructure.db.models import Product, ProductFactSetVersion


@dataclass(frozen=True, slots=True)
class ProductSummarySnapshot:
    id: str
    name: str
    category: str | None
    price: str | None
    source_note: str | None


@dataclass(frozen=True, slots=True)
class FactSetSnapshot:
    id: str
    product_id: str
    version: int
    facts: tuple[dict[str, Any], ...]


@dataclass(frozen=True, slots=True)
class ProductSourceSnapshot:
    """Resolved product_source input used by both projection and runtime.

    ``source_product_id`` is the effective source.  For a legacy node whose
    config predates the explicit binding fields, it is the graph owner.  An
    explicit ``null`` remains unbound and therefore has no effective source.
    """

    source_product_id: str | None
    fact_set_version_id: str | None
    source_product: ProductSummarySnapshot | None
    fact_set_version: FactSetSnapshot | None
    facts: tuple[dict[str, Any], ...]
    legacy_fallback: bool = False


def resolve_product_source(
    session: Session,
    *,
    graph_product_id: str,
    config: dict[str, object] | None,
) -> ProductSourceSnapshot:
    """Resolve one product_source config to the exact facts it provides.

    Missing ``source_product_id`` is intentionally treated as the historical
    config={} shape.  An explicit null means an intentionally empty/manual
    source and must not fall back to the graph owner.
    """

    payload = config or {}
    has_source_binding = "source_product_id" in payload
    raw_source_id = payload.get("source_product_id")
    if raw_source_id is not None and (not isinstance(raw_source_id, str) or not raw_source_id.strip()):
        raise BusinessValidationError("商品资料节点的 source_product_id 无效")
    source_product_id = raw_source_id.strip() if isinstance(raw_source_id, str) else None
    if not has_source_binding:
        source_product_id = graph_product_id

    if source_product_id is None:
        if payload.get("fact_set_version_id") is not None:
            raise BusinessValidationError("未绑定商品的商品资料节点不能绑定 fact_set_version_id")
        return ProductSourceSnapshot(
            source_product_id=None,
            fact_set_version_id=None,
            source_product=None,
            fact_set_version=None,
            facts=(),
        )

    product = session.get(Product, source_product_id)
    if product is None:
        raise BusinessValidationError("商品资料节点绑定的商品不存在")

    raw_fact_set_id = payload.get("fact_set_version_id")
    if raw_fact_set_id is not None and (not isinstance(raw_fact_set_id, str) or not raw_fact_set_id.strip()):
        raise BusinessValidationError("商品资料节点的 fact_set_version_id 无效")
    fact_set_id = raw_fact_set_id.strip() if isinstance(raw_fact_set_id, str) else None
    fact_set: ProductFactSetVersion | None
    if fact_set_id is not None:
        fact_set = session.scalar(
            select(ProductFactSetVersion).where(
                ProductFactSetVersion.id == fact_set_id,
                ProductFactSetVersion.product_id == source_product_id,
            )
        )
        if fact_set is None:
            raise BusinessValidationError("fact_set_version_id 不属于绑定商品")
    else:
        fact_set = (
            session.get(ProductFactSetVersion, product.current_fact_set_version_id)
            if product.current_fact_set_version_id
            else None
        )
        if fact_set is not None and fact_set.product_id != source_product_id:
            raise BusinessValidationError("商品当前事实版本不属于该商品")

    facts = _facts_from_set(fact_set)
    return ProductSourceSnapshot(
        source_product_id=source_product_id,
        fact_set_version_id=fact_set.id if fact_set is not None else None,
        source_product=_product_summary(product),
        fact_set_version=_fact_set_snapshot(fact_set, facts) if fact_set is not None else None,
        facts=facts,
        legacy_fallback=not has_source_binding,
    )


def validate_product_source_configs(session: Session, *, graph_product_id: str, graph) -> None:
    """Validate product_source references before graph rows are persisted."""

    for node in graph.nodes:
        if getattr(node.node_type, "value", node.node_type) != "product_source":
            continue
        resolve_product_source(session, graph_product_id=graph_product_id, config=node.config)


def product_source_snapshot_from_dict(payload: dict[str, Any] | None) -> ProductSourceSnapshot | None:
    if not payload:
        return None
    product_payload = payload.get("source_product")
    source_product = (
        ProductSummarySnapshot(
            id=str(product_payload["id"]),
            name=str(product_payload.get("name") or ""),
            category=product_payload.get("category"),
            price=product_payload.get("price"),
            source_note=product_payload.get("source_note"),
        )
        if isinstance(product_payload, dict) and product_payload.get("id")
        else None
    )
    fact_payload = payload.get("fact_set_version")
    facts = tuple(dict(item) for item in payload.get("facts") or () if isinstance(item, dict))
    fact_set = (
        FactSetSnapshot(
            id=str(fact_payload["id"]),
            product_id=str(fact_payload.get("product_id") or payload.get("source_product_id") or ""),
            version=int(fact_payload.get("version") or 0),
            facts=tuple(dict(item) for item in fact_payload.get("facts") or facts if isinstance(item, dict)),
        )
        if isinstance(fact_payload, dict) and fact_payload.get("id")
        else None
    )
    if fact_set is not None and not facts:
        facts = fact_set.facts
    return ProductSourceSnapshot(
        source_product_id=payload.get("source_product_id"),
        fact_set_version_id=payload.get("fact_set_version_id"),
        source_product=source_product,
        fact_set_version=fact_set,
        facts=facts,
        legacy_fallback=bool(payload.get("legacy_fallback")),
    )


def merge_runtime_facts(
    facts: tuple[dict[str, Any], ...],
    product_source: ProductSourceSnapshot | None,
) -> tuple[dict[str, Any], ...]:
    """Keep stored facts, and fill identity keys from the bound product when missing."""

    product = product_source.source_product if product_source is not None else None
    if product is None:
        return facts
    existing = {str(item.get("key") or "").casefold() for item in facts}
    identity = (
        ("product_name", product.name),
        ("category", product.category),
        ("price", product.price),
        ("source_note", product.source_note),
    )
    extras = tuple(
        {"key": key, "value": value}
        for key, value in identity
        if value is not None and str(value).strip() and key.casefold() not in existing
    )
    return extras + facts


def product_source_snapshot_to_dict(snapshot: ProductSourceSnapshot | None) -> dict[str, Any] | None:
    if snapshot is None:
        return None
    product = snapshot.source_product
    fact_set = snapshot.fact_set_version
    return {
        "source_product_id": snapshot.source_product_id,
        "fact_set_version_id": snapshot.fact_set_version_id,
        "source_product": (
            {
                "id": product.id,
                "name": product.name,
                "category": product.category,
                "price": product.price,
                "source_note": product.source_note,
            }
            if product is not None
            else None
        ),
        "fact_set_version": (
            {
                "id": fact_set.id,
                "product_id": fact_set.product_id,
                "version": fact_set.version,
                "facts": list(fact_set.facts),
            }
            if fact_set is not None
            else None
        ),
        "facts": list(snapshot.facts),
        "legacy_fallback": snapshot.legacy_fallback,
    }


def _product_summary(product: Product) -> ProductSummarySnapshot:
    return ProductSummarySnapshot(
        id=product.id,
        name=product.name,
        category=product.category,
        price=str(product.price) if product.price is not None else None,
        source_note=product.source_note,
    )


def _fact_set_snapshot(
    fact_set: ProductFactSetVersion,
    facts: tuple[dict[str, Any], ...],
) -> FactSetSnapshot:
    return FactSetSnapshot(
        id=fact_set.id,
        product_id=fact_set.product_id,
        version=fact_set.version,
        facts=facts,
    )


def _facts_from_set(fact_set: ProductFactSetVersion | None) -> tuple[dict[str, Any], ...]:
    if fact_set is None:
        return ()
    facts = fact_set.payload_json.get("facts")
    if not isinstance(facts, list):
        return ()
    return tuple(dict(fact) for fact in facts if isinstance(fact, dict))
