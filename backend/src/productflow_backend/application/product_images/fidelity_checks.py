from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass
from typing import Any

from sqlalchemy import desc, func, select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from productflow_backend.domain.enums import ProductImageFidelityOutcome
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    Product,
    ProductImageAsset,
    ProductImageFidelityCheck,
)

FIDELITY_CHECK_MAX_LIMIT = 100
FIDELITY_CHECK_ACTOR = "administrator"
FIDELITY_CHECK_NOTES_MAX_LENGTH = 4000


@dataclass(frozen=True, slots=True)
class ProductImageFidelityCheckList:
    product_id: str
    asset_id: str
    items: list[ProductImageFidelityCheck]
    latest_version: int


def create_product_image_fidelity_check(
    session: Session,
    *,
    product_id: str,
    asset_id: str,
    expected_latest_version: int,
    idempotency_key: str,
    shape_fidelity: ProductImageFidelityOutcome | str,
    color_material_fidelity: ProductImageFidelityOutcome | str,
    logo_text_legibility: ProductImageFidelityOutcome | str,
    text_policy_compliance: ProductImageFidelityOutcome | str,
    notes: str | None,
) -> ProductImageFidelityCheck:
    if expected_latest_version < 0:
        raise BusinessValidationError("expected_latest_version 不能小于 0")
    normalized_key = _normalize_idempotency_key(idempotency_key)
    normalized_notes = _normalize_notes(notes)
    outcomes = {
        "shape_fidelity": _normalize_outcome(shape_fidelity),
        "color_material_fidelity": _normalize_outcome(color_material_fidelity),
        "logo_text_legibility": _normalize_outcome(logo_text_legibility),
        "text_policy_compliance": _normalize_outcome(text_policy_compliance),
    }
    request_hash = _request_hash(
        product_id=product_id,
        asset_id=asset_id,
        expected_latest_version=expected_latest_version,
        shape_fidelity=outcomes["shape_fidelity"],
        color_material_fidelity=outcomes["color_material_fidelity"],
        logo_text_legibility=outcomes["logo_text_legibility"],
        text_policy_compliance=outcomes["text_policy_compliance"],
        notes=normalized_notes,
    )

    try:
        _get_product_and_asset_for_update(session, product_id=product_id, asset_id=asset_id)
        existing = _find_by_idempotency_key(session, asset_id=asset_id, idempotency_key=normalized_key)
        if existing is not None:
            if existing.request_hash != request_hash:
                raise ConflictError("相同 idempotency key 不能写入不同的人工保真检查内容")
            session.commit()
            return existing

        latest_version = _latest_version(session, asset_id=asset_id)
        if expected_latest_version != latest_version:
            raise ConflictError("商品图片人工保真检查版本已变化，请基于最新版本重试")

        check = ProductImageFidelityCheck(
            product_id=product_id,
            asset_id=asset_id,
            version=latest_version + 1,
            shape_fidelity=outcomes["shape_fidelity"],
            color_material_fidelity=outcomes["color_material_fidelity"],
            logo_text_legibility=outcomes["logo_text_legibility"],
            text_policy_compliance=outcomes["text_policy_compliance"],
            notes=normalized_notes,
            checked_by=FIDELITY_CHECK_ACTOR,
            idempotency_key=normalized_key,
            request_hash=request_hash,
        )
        session.add(check)
        session.commit()
        return check
    except IntegrityError:
        session.rollback()
        existing = _find_by_idempotency_key(session, asset_id=asset_id, idempotency_key=normalized_key)
        if existing is not None:
            if existing.request_hash != request_hash:
                raise ConflictError("相同 idempotency key 不能写入不同的人工保真检查内容") from None
            session.commit()
            return existing
        raise ConflictError("商品图片人工保真检查版本已变化，请基于最新版本重试") from None
    except Exception:
        session.rollback()
        raise


def list_product_image_fidelity_checks(
    session: Session,
    *,
    product_id: str,
    asset_id: str,
    limit: int = FIDELITY_CHECK_MAX_LIMIT,
) -> ProductImageFidelityCheckList:
    if limit < 1 or limit > FIDELITY_CHECK_MAX_LIMIT:
        raise BusinessValidationError(f"人工保真检查列表最多返回 {FIDELITY_CHECK_MAX_LIMIT} 条")
    _get_product_and_asset(session, product_id=product_id, asset_id=asset_id)
    items = list(
        session.scalars(
            select(ProductImageFidelityCheck)
            .where(
                ProductImageFidelityCheck.product_id == product_id,
                ProductImageFidelityCheck.asset_id == asset_id,
            )
            .order_by(
                desc(ProductImageFidelityCheck.version),
                desc(ProductImageFidelityCheck.created_at),
                desc(ProductImageFidelityCheck.id),
            )
            .limit(limit)
        ).all()
    )
    return ProductImageFidelityCheckList(
        product_id=product_id,
        asset_id=asset_id,
        items=items,
        latest_version=_latest_version(session, asset_id=asset_id),
    )


def _get_product_and_asset(
    session: Session,
    *,
    product_id: str,
    asset_id: str,
) -> tuple[Product, ProductImageAsset]:
    product = session.get(Product, product_id)
    if product is None:
        raise NotFoundError("商品不存在")
    asset = session.scalar(
        select(ProductImageAsset).where(
            ProductImageAsset.id == asset_id,
            ProductImageAsset.product_id == product_id,
        )
    )
    if asset is None:
        raise NotFoundError("商品图片不存在")
    return product, asset


def _get_product_and_asset_for_update(
    session: Session,
    *,
    product_id: str,
    asset_id: str,
) -> tuple[Product, ProductImageAsset]:
    product = session.scalar(select(Product).where(Product.id == product_id).with_for_update())
    if product is None:
        raise NotFoundError("商品不存在")
    asset = session.scalar(
        select(ProductImageAsset)
        .where(
            ProductImageAsset.id == asset_id,
            ProductImageAsset.product_id == product_id,
        )
        .with_for_update()
    )
    if asset is None:
        raise NotFoundError("商品图片不存在")
    return product, asset


def _find_by_idempotency_key(
    session: Session,
    *,
    asset_id: str,
    idempotency_key: str,
) -> ProductImageFidelityCheck | None:
    return session.scalar(
        select(ProductImageFidelityCheck).where(
            ProductImageFidelityCheck.asset_id == asset_id,
            ProductImageFidelityCheck.idempotency_key == idempotency_key,
        )
    )


def _latest_version(session: Session, *, asset_id: str) -> int:
    return int(
        session.scalar(
            select(func.max(ProductImageFidelityCheck.version)).where(
                ProductImageFidelityCheck.asset_id == asset_id,
            )
        )
        or 0
    )


def _normalize_idempotency_key(value: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError("idempotency_key 不能为空")
    return normalized


def _normalize_notes(value: str | None) -> str | None:
    if value is None:
        return None
    normalized = value.strip()
    if len(normalized) > FIDELITY_CHECK_NOTES_MAX_LENGTH:
        raise BusinessValidationError("人工保真检查 notes 不能超过 4000 个字符")
    return normalized or None


def _normalize_outcome(value: ProductImageFidelityOutcome | str) -> ProductImageFidelityOutcome:
    try:
        return ProductImageFidelityOutcome(value)
    except ValueError as exc:
        raise BusinessValidationError("人工保真检查结论无效") from exc


def _request_hash(
    *,
    product_id: str,
    asset_id: str,
    expected_latest_version: int,
    shape_fidelity: ProductImageFidelityOutcome,
    color_material_fidelity: ProductImageFidelityOutcome,
    logo_text_legibility: ProductImageFidelityOutcome,
    text_policy_compliance: ProductImageFidelityOutcome,
    notes: str | None,
) -> str:
    payload: dict[str, Any] = {
        "asset_id": asset_id,
        "color_material_fidelity": color_material_fidelity.value,
        "expected_latest_version": expected_latest_version,
        "logo_text_legibility": logo_text_legibility.value,
        "notes": notes,
        "product_id": product_id,
        "shape_fidelity": shape_fidelity.value,
        "text_policy_compliance": text_policy_compliance.value,
    }
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()
