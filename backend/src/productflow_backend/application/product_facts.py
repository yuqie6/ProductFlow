from __future__ import annotations

import hashlib
import json
from typing import Any

from sqlalchemy import func, select
from sqlalchemy.orm import Session

from productflow_backend.application.products import (
    _normalize_optional_text,
    _normalize_price,
    normalize_product_name,
)
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import ProductFactSourceType, ProductFactStatus
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import Product, ProductFactSetVersion


def get_product_facts(session: Session, *, product_id: str) -> Product:
    product = session.scalar(select(Product).where(Product.id == product_id))
    if product is None:
        raise NotFoundError("商品不存在")
    return product


def update_product_facts(
    session: Session,
    *,
    product_id: str,
    expected_fact_set_version_id: str | None = None,
    expected_fact_version: int | None = None,
    expected_version_provided: bool = False,
    name: str | None = None,
    category: str | None = None,
    price: str | None = None,
    source_note: str | None = None,
    facts: list[dict[str, Any]] | None = None,
    fields_set: set[str] | None = None,
) -> Product:
    product = session.scalar(select(Product).where(Product.id == product_id).with_for_update())
    if product is None:
        raise NotFoundError("商品不存在")
    fields = fields_set or set()
    current = (
        session.get(ProductFactSetVersion, product.current_fact_set_version_id)
        if product.current_fact_set_version_id
        else None
    )
    if current is not None and not expected_version_provided:
        raise ConflictError("保存商品资料必须携带当前 fact version")
    if expected_version_provided:
        if (
            expected_fact_set_version_id is not None
            and product.current_fact_set_version_id != expected_fact_set_version_id
        ):
            raise ConflictError("商品资料 fact version 已变化，请刷新后重试")
        if expected_fact_version is not None and (current is None or current.version != expected_fact_version):
            raise ConflictError("商品资料 fact version 已变化，请刷新后重试")
        if expected_fact_set_version_id is None and expected_fact_version is None and current is not None:
            raise ConflictError("商品资料 fact version 已变化，请刷新后重试")

    if "name" in fields:
        product.name = normalize_product_name(name or "")
    if "category" in fields:
        product.category = _normalize_optional_text(category, field_name="类目", max_length=120)
    if "price" in fields:
        product.price = _normalize_price(price)
    if "source_note" in fields:
        product.source_note = _normalize_optional_text(source_note, field_name="备注", max_length=4000)

    fact_payload = list(_facts_from_set(current)) if facts is None else facts
    stage_product_fact_set(session, product=product, facts=fact_payload)
    product.updated_at = now_utc()
    session.commit()
    session.expire_all()
    return get_product_facts(session, product_id=product.id)


def stage_product_fact_set(
    session: Session,
    *,
    product: Product,
    facts: list[dict[str, Any]],
) -> ProductFactSetVersion:
    """创建并选中一个不可变 fact 版本，不拥有事务。"""

    normalized = [normalize_fact_payload(fact) for fact in facts]
    keys = [str(fact["key"]).casefold() for fact in normalized]
    if len(keys) != len(set(keys)):
        raise BusinessValidationError("商品事实 key 不能重复")
    payload = {
        "schema_version": 1,
        "source_product_id": product.id,
        "facts": normalized,
    }
    next_version = (session.scalar(select(func.max(ProductFactSetVersion.version)).where(
        ProductFactSetVersion.product_id == product.id
    )) or 0) + 1
    fact_set = ProductFactSetVersion(
        product_id=product.id,
        version=next_version,
        payload_json=payload,
        payload_hash=_json_hash(payload),
    )
    session.add(fact_set)
    session.flush()
    product.current_fact_set_version_id = fact_set.id
    return fact_set


def product_metadata_facts(product: Product) -> list[dict[str, Any]]:
    values = (
        ("product_name", product.name),
        ("category", product.category),
        ("price", str(product.price) if product.price is not None else None),
        ("source_note", product.source_note),
    )
    return [
        {"key": key, "value": value}
        for key, value in values
        if value is not None and str(value).strip()
    ]


def normalize_fact_payload(payload: dict[str, Any]) -> dict[str, Any]:
    key = str(payload.get("key") or "").strip()
    if not key or len(key) > 120:
        raise BusinessValidationError("商品事实 key 不能为空且不能超过 120 个字符")
    return {
        "key": key,
        "value": payload.get("value"),
        "source_type": str(payload.get("source_type") or ProductFactSourceType.USER.value),
        "status": str(payload.get("status") or ProductFactStatus.CONFIRMED.value),
        "requires_confirmation": bool(payload.get("requires_confirmation", False)),
        "evidence_asset_ids": list(payload.get("evidence_asset_ids") or []),
        "conflicts": list(payload.get("conflicts") or []),
    }


def product_facts_from_product(product: Product) -> tuple[dict[str, Any], ...]:
    return _facts_from_set(product.current_fact_set_version)


def _facts_from_set(fact_set: ProductFactSetVersion | None) -> tuple[dict[str, Any], ...]:
    if fact_set is None:
        return ()
    facts = fact_set.payload_json.get("facts")
    if not isinstance(facts, list):
        return ()
    return tuple(normalize_fact_payload(fact) for fact in facts if isinstance(fact, dict))


def _json_hash(payload: dict[str, Any]) -> str:
    canonical = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(canonical).hexdigest()
