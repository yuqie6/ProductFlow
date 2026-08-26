from __future__ import annotations

import pytest
from helpers import _make_demo_image_bytes
from sqlalchemy import func, select

from productflow_backend.application.product_facts import stage_product_fact_set
from productflow_backend.application.product_workflow.graph_commands import (
    apply_graph_change_set,
    get_active_workflow_graph,
    load_applied_graph,
    redo_last_graph_change_set,
    stage_apply_graph_change_set,
    stage_new_workflow_graph,
    undo_last_graph_change_set,
)
from productflow_backend.application.product_workflow.graph_contracts import (
    CreateNodeOp,
    RenameNodeOp,
    WorkflowChangeSet,
)
from productflow_backend.application.product_workflow.graph_queries import project_workflow_graph
from productflow_backend.application.product_workflow.graph_runs import load_graph_sources
from productflow_backend.application.product_workflow.graph_template import (
    DirectCreateImageType,
    build_direct_create_template,
)
from productflow_backend.application.products import create_canonical_product_with_assets
from productflow_backend.domain.enums import GraphConfigStatus, GraphHistoryKind, GraphNodeType
from productflow_backend.domain.errors import BusinessValidationError, ConflictError
from productflow_backend.infrastructure.db.models import WorkflowDraft, WorkflowGraph, WorkflowOperationGroup


def _create_product_with_assets(db_session, *, name: str = "直接创建商品"):
    return create_canonical_product_with_assets(
        db_session,
        name=name,
        category="工业收纳",
        price="299.00",
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "product.png", "image/png")],
    )


def test_stage_new_workflow_graph_persists_revision_and_operation_group(db_session) -> None:
    creation = _create_product_with_assets(db_session)
    change_set = build_direct_create_template(
        image_types=[DirectCreateImageType(key="hero", quantity=2, order=0, title="首屏海报图")],
        reference_asset_ids=[creation.created_assets[0].id],
        product_title=creation.product.name,
    )
    result = stage_new_workflow_graph(
        db_session,
        product_id=creation.product.id,
        change_set=change_set,
        title=creation.product.name,
    )
    db_session.commit()

    graph = db_session.get(WorkflowGraph, result.graph.id)
    assert graph is not None
    assert graph.schema_version == 3
    assert graph.revision == 1
    assert graph.active is True
    applied = load_applied_graph(db_session, graph)
    assert {node.node_type for node in applied.nodes} >= {
        GraphNodeType.PRODUCT_SOURCE,
        GraphNodeType.IMAGE_ASSET,
        GraphNodeType.PROMPT_GENERATION,
        GraphNodeType.IMAGE_GENERATION,
        GraphNodeType.VISUAL_SYSTEM,
        GraphNodeType.CREATIVE_BRIEF,
    }
    assert all(len(node.id) == 36 for node in applied.nodes)
    asset_nodes = [node for node in applied.nodes if node.node_type == GraphNodeType.IMAGE_ASSET]
    assert len(asset_nodes) == 1
    assert asset_nodes[0].bound_asset_id == creation.created_assets[0].id
    assert any(edge.source_node_id == asset_nodes[0].id for edge in applied.edges)

    projection = project_workflow_graph(db_session, graph)
    unused_assets = [node for node in projection.nodes if node.node_type == GraphNodeType.IMAGE_ASSET]
    assert unused_assets[0].unused is False
    assert unused_assets[0].config_status == GraphConfigStatus.READY
    visual = next(node for node in projection.nodes if node.node_type == GraphNodeType.VISUAL_SYSTEM)
    assert visual.config_status == GraphConfigStatus.INCOMPLETE
    assert projection.last_operation_group_id is not None
    operation_count = db_session.scalar(select(func.count()).select_from(WorkflowOperationGroup))
    assert operation_count == 1
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraft)) == 0


def test_apply_graph_change_set_conflicts_on_stale_revision(db_session) -> None:
    creation = _create_product_with_assets(db_session, name="冲突商品")
    change_set = build_direct_create_template(
        image_types=[DirectCreateImageType(key="detail", quantity=1, order=0)],
        reference_asset_ids=[creation.created_assets[0].id],
    )
    created = stage_new_workflow_graph(db_session, product_id=creation.product.id, change_set=change_set)
    db_session.commit()
    brief = next(node for node in created.applied.nodes if node.node_type == GraphNodeType.CREATIVE_BRIEF)
    apply_graph_change_set(
        db_session,
        product_id=creation.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=1,
            summary="改名",
            operations=[RenameNodeOp(node_ref=brief.id, title="新创作要求")],
        ),
    )
    with pytest.raises(ConflictError, match="revision"):
        apply_graph_change_set(
            db_session,
            product_id=creation.product.id,
            graph_id=created.graph.id,
            change_set=WorkflowChangeSet(
                base_graph_revision=1,
                summary="过期改名",
                operations=[RenameNodeOp(node_ref=brief.id, title="不会写入")],
            ),
        )
    graph = get_active_workflow_graph(db_session, product_id=creation.product.id)
    assert graph is not None
    assert graph.revision == 2
    applied = load_applied_graph(db_session, graph)
    assert applied.node(brief.id).title == "新创作要求"


def test_undo_last_graph_change_set_restores_previous_title(db_session) -> None:
    creation = _create_product_with_assets(db_session, name="撤销商品")
    change_set = build_direct_create_template(
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
        reference_asset_ids=[creation.created_assets[0].id],
    )
    created = stage_new_workflow_graph(db_session, product_id=creation.product.id, change_set=change_set)
    db_session.commit()
    brief = next(node for node in created.applied.nodes if node.node_type == GraphNodeType.CREATIVE_BRIEF)
    apply_graph_change_set(
        db_session,
        product_id=creation.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=1,
            summary="改名",
            operations=[RenameNodeOp(node_ref=brief.id, title="新创作要求")],
        ),
    )
    undone = undo_last_graph_change_set(
        db_session,
        product_id=creation.product.id,
        graph_id=created.graph.id,
    )
    applied = load_applied_graph(db_session, undone.graph)
    assert applied.node(brief.id).title == brief.title
    assert undone.graph.revision == 3
    assert GraphHistoryKind(undone.operation_group.history_kind) == GraphHistoryKind.UNDO
    undone_projection = project_workflow_graph(db_session, undone.graph)
    assert undone_projection.can_undo is False
    assert undone_projection.can_redo is True


def test_redo_last_graph_change_set_restores_undone_edit(db_session) -> None:
    creation = _create_product_with_assets(db_session, name="重做商品")
    change_set = build_direct_create_template(
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
        reference_asset_ids=[creation.created_assets[0].id],
    )
    created = stage_new_workflow_graph(db_session, product_id=creation.product.id, change_set=change_set)
    db_session.commit()
    brief = next(node for node in created.applied.nodes if node.node_type == GraphNodeType.CREATIVE_BRIEF)
    apply_graph_change_set(
        db_session,
        product_id=creation.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=1,
            summary="改名",
            operations=[RenameNodeOp(node_ref=brief.id, title="新创作要求")],
        ),
    )
    undo_last_graph_change_set(
        db_session,
        product_id=creation.product.id,
        graph_id=created.graph.id,
    )
    with pytest.raises(ConflictError, match="没有可撤销"):
        undo_last_graph_change_set(
            db_session,
            product_id=creation.product.id,
            graph_id=created.graph.id,
        )
    redone = redo_last_graph_change_set(
        db_session,
        product_id=creation.product.id,
        graph_id=created.graph.id,
    )
    applied = load_applied_graph(db_session, redone.graph)
    assert applied.node(brief.id).title == "新创作要求"
    assert GraphHistoryKind(redone.operation_group.history_kind) == GraphHistoryKind.REDO
    redone_projection = project_workflow_graph(db_session, redone.graph)
    assert redone_projection.can_undo is True
    assert redone_projection.can_redo is False


def test_new_edit_after_undo_clears_redo(db_session) -> None:
    creation = _create_product_with_assets(db_session, name="清空重做商品")
    change_set = build_direct_create_template(
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
        reference_asset_ids=[creation.created_assets[0].id],
    )
    created = stage_new_workflow_graph(db_session, product_id=creation.product.id, change_set=change_set)
    db_session.commit()
    brief = next(node for node in created.applied.nodes if node.node_type == GraphNodeType.CREATIVE_BRIEF)
    apply_graph_change_set(
        db_session,
        product_id=creation.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=1,
            summary="改名",
            operations=[RenameNodeOp(node_ref=brief.id, title="新创作要求")],
        ),
    )
    undo_last_graph_change_set(
        db_session,
        product_id=creation.product.id,
        graph_id=created.graph.id,
    )
    apply_graph_change_set(
        db_session,
        product_id=creation.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=3,
            summary="另改名",
            operations=[RenameNodeOp(node_ref=brief.id, title="另一标题")],
        ),
    )
    with pytest.raises(ConflictError, match="没有可重做"):
        redo_last_graph_change_set(
            db_session,
            product_id=creation.product.id,
            graph_id=created.graph.id,
        )


def test_bound_asset_must_belong_to_product(db_session) -> None:
    owner = _create_product_with_assets(db_session, name="资产所有者")
    other = _create_product_with_assets(db_session, name="其他商品")
    change_set = build_direct_create_template(
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
        reference_asset_ids=[other.created_assets[0].id],
    )
    with pytest.raises(BusinessValidationError, match="不属于该商品"):
        stage_new_workflow_graph(db_session, product_id=owner.product.id, change_set=change_set)


def test_multiple_product_sources_resolve_their_own_products_and_fact_versions(db_session) -> None:
    owner = _create_product_with_assets(db_session, name="主商品")
    other = _create_product_with_assets(db_session, name="搭配商品")
    owner_facts = stage_product_fact_set(
        db_session,
        product=owner.product,
        facts=[{"key": "material", "value": "steel"}],
    )
    other_facts = stage_product_fact_set(
        db_session,
        product=other.product,
        facts=[{"key": "material", "value": "wood"}],
    )
    db_session.commit()

    created = stage_new_workflow_graph(
        db_session,
        product_id=owner.product.id,
        change_set=build_direct_create_template(
            image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
            reference_asset_ids=[owner.created_assets[0].id],
            source_product_id=owner.product.id,
            fact_set_version_id=owner_facts.id,
        ),
    )
    db_session.commit()
    added = apply_graph_change_set(
        db_session,
        product_id=owner.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=created.graph.revision,
            summary="增加搭配商品资料",
            operations=[
                CreateNodeOp(
                    client_ref="secondary-product",
                    node_type=GraphNodeType.PRODUCT_SOURCE,
                    title="搭配商品",
                    config={
                        "source_product_id": other.product.id,
                        "fact_set_version_id": other_facts.id,
                    },
                )
            ],
        ),
    )
    sources = load_graph_sources(db_session, added.graph, added.applied)
    product_sources = [
        (node, sources[node.id])
        for node in added.applied.nodes
        if node.node_type == GraphNodeType.PRODUCT_SOURCE
    ]
    assert len(product_sources) == 2
    assert {
        (record.product_source.source_product.name, record.facts[0]["value"])
        for _, record in product_sources
        if record.product_source is not None and record.product_source.source_product is not None
    } == {("主商品", "steel"), ("搭配商品", "wood")}

    with pytest.raises(BusinessValidationError, match="不属于绑定商品"):
        apply_graph_change_set(
            db_session,
            product_id=owner.product.id,
            graph_id=added.graph.id,
            change_set=WorkflowChangeSet(
                base_graph_revision=added.graph.revision,
                summary="错误绑定事实版本",
                operations=[
                    CreateNodeOp(
                        client_ref="invalid-product",
                        node_type=GraphNodeType.PRODUCT_SOURCE,
                        title="错误资料",
                        config={
                            "source_product_id": owner.product.id,
                            "fact_set_version_id": other_facts.id,
                        },
                    )
                ],
            ),
        )


def test_stage_apply_graph_change_set_failure_keeps_outer_uncommitted_work(db_session) -> None:
    creation = _create_product_with_assets(db_session, name="savepoint商品")
    change_set = build_direct_create_template(
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
        reference_asset_ids=[creation.created_assets[0].id],
    )
    created = stage_new_workflow_graph(db_session, product_id=creation.product.id, change_set=change_set)
    db_session.commit()
    brief = next(node for node in created.applied.nodes if node.node_type == GraphNodeType.CREATIVE_BRIEF)
    creation.product.name = "外层事务应保留"
    db_session.flush()

    with pytest.raises(ConflictError, match="revision"):
        stage_apply_graph_change_set(
            db_session,
            product_id=creation.product.id,
            graph_id=created.graph.id,
            change_set=WorkflowChangeSet(
                base_graph_revision=0,
                summary="过期改名",
                operations=[RenameNodeOp(node_ref=brief.id, title="不会写入")],
            ),
        )

    assert creation.product.name == "外层事务应保留"
    db_session.commit()
    graph = get_active_workflow_graph(db_session, product_id=creation.product.id)
    assert graph is not None
    applied = load_applied_graph(db_session, graph)
    assert applied.node(brief.id).title != "不会写入"
    reloaded = db_session.get(type(creation.product), creation.product.id)
    assert reloaded is not None
    assert reloaded.name == "外层事务应保留"
