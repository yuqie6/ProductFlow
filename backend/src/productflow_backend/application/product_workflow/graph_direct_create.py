"""跳过 Draft，一次事务创建商品、参考图和完整 schema-v3 图。"""

from __future__ import annotations

from dataclasses import dataclass

from sqlalchemy.orm import Session

from productflow_backend.application.product_facts import product_metadata_facts, stage_product_fact_set
from productflow_backend.application.product_images.assets import get_product_image_assets_by_ids
from productflow_backend.application.product_intake import delivery_preset_spec_for_key
from productflow_backend.application.product_workflow.graph_commands import stage_new_workflow_graph
from productflow_backend.application.product_workflow.graph_queries import GraphProjection, project_workflow_graph
from productflow_backend.application.product_workflow.graph_template import (
    DirectCreateImageType,
    build_direct_create_template,
)
from productflow_backend.application.products import get_product_detail, stage_canonical_product_with_assets
from productflow_backend.application.storage_compensation import compensate_storage_writes
from productflow_backend.infrastructure.db.models import Product, ProductImageAsset, WorkflowGraph
from productflow_backend.infrastructure.storage import LocalStorage


@dataclass(frozen=True, slots=True)
class DirectCreateResult:
    product: Product
    created_assets: list[ProductImageAsset]
    graph: WorkflowGraph
    projection: GraphProjection


def create_product_with_direct_graph(
    session: Session,
    *,
    name: str,
    category: str | None,
    price: str | None,
    source_note: str | None,
    image_uploads: list[tuple[bytes, str, str]],
    image_types: list[DirectCreateImageType],
    storage: LocalStorage | None = None,
    generation_spec: dict | None = None,
    delivery_preset_key: str | None = None,
) -> DirectCreateResult:
    """Create a product, its reference assets, and the preset v3 graph in one transaction."""

    delivery_spec = delivery_preset_spec_for_key(delivery_preset_key)
    delivery_spec_json = delivery_spec.model_dump(mode="json") if delivery_spec is not None else None
    storage = storage or LocalStorage()
    with compensate_storage_writes(session) as storage_writes:
        creation = stage_canonical_product_with_assets(
            session,
            name=name,
            category=category,
            price=price,
            source_note=source_note,
            image_uploads=image_uploads,
            storage=storage,
            storage_writes=storage_writes,
        )
        creation.product.cover_image_asset_id = creation.created_assets[0].id
        fact_set = stage_product_fact_set(
            session,
            product=creation.product,
            facts=product_metadata_facts(creation.product),
        )
        change_set = build_direct_create_template(
            image_types=image_types,
            reference_asset_ids=[asset.id for asset in creation.created_assets],
            product_title=creation.product.name,
            source_product_id=creation.product.id,
            fact_set_version_id=fact_set.id,
            source_note=source_note,
            generation_spec=generation_spec,
            delivery_spec=delivery_spec_json,
        )
        command = stage_new_workflow_graph(
            session,
            product_id=creation.product.id,
            change_set=change_set,
            title=creation.product.name,
        )
        product_id = creation.product.id
        asset_ids = [asset.id for asset in creation.created_assets]
        graph_id = command.graph.id
        # Graph Command 只 flush；本函数一次 commit 商品、参考图和完整 v3 图。
        session.commit()
    session.expire_all()
    product = get_product_detail(session, product_id)
    graph = session.get(WorkflowGraph, graph_id)
    assert graph is not None
    return DirectCreateResult(
        product=product,
        created_assets=get_product_image_assets_by_ids(session, product_id=product_id, asset_ids=asset_ids),
        graph=graph,
        projection=project_workflow_graph(session, graph),
    )
