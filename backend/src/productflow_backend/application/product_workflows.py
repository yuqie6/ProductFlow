from __future__ import annotations

import logging
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from sqlalchemy import delete, select, update
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application import product_workflow_graph
from productflow_backend.application.contracts import PosterGenerationInput, ProductInput, ReferenceImageInput
from productflow_backend.application.time import now_utc
from productflow_backend.application.use_cases import update_copy_set
from productflow_backend.config import get_runtime_settings, normalize_image_size
from productflow_backend.domain.enums import (
    CopyStatus,
    PosterKind,
    SourceAssetKind,
    WorkflowNodeStatus,
    WorkflowNodeType,
    WorkflowRunStatus,
)
from productflow_backend.infrastructure.db.models import (
    CopySet,
    CreativeBrief,
    PosterVariant,
    Product,
    ProductWorkflow,
    SourceAsset,
    WorkflowEdge,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRun,
)
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.image.base import infer_extension
from productflow_backend.infrastructure.image.factory import get_image_provider
from productflow_backend.infrastructure.poster.renderer import PosterRenderer
from productflow_backend.infrastructure.storage import LocalStorage
from productflow_backend.infrastructure.text.factory import get_text_provider

logger = logging.getLogger(__name__)


@dataclass(frozen=True, slots=True)
class WorkflowRunKickoff:
    workflow: ProductWorkflow
    run_id: str
    created: bool


def get_or_create_product_workflow(session: Session, product_id: str) -> ProductWorkflow:
    existing = product_workflow_graph.get_active_workflow(session, product_id)
    if existing is not None:
        if _normalize_product_context_singleton(session, existing):
            session.commit()
            session.expire_all()
            return product_workflow_graph.get_active_workflow(
                session, product_id
            ) or product_workflow_graph.get_workflow_or_raise(session, existing.id)
        return existing

    product = product_workflow_graph.get_product_or_raise(session, product_id)
    workflow = ProductWorkflow(
        product_id=product.id,
        title=product_workflow_graph.DEFAULT_WORKFLOW_TITLE,
        active=True,
    )
    session.add(workflow)
    session.flush()

    nodes_by_key: dict[str, WorkflowNode] = {}
    for spec in product_workflow_graph.default_node_specs(product):
        key = str(spec.pop("key"))
        node = WorkflowNode(workflow_id=workflow.id, **spec)
        session.add(node)
        nodes_by_key[key] = node
    session.flush()
    for edge in product_workflow_graph.default_edges(nodes_by_key, workflow.id):
        session.add(edge)
    try:
        session.commit()
    except IntegrityError:
        session.rollback()
        existing = product_workflow_graph.get_active_workflow(session, product_id)
        if existing is not None:
            return existing
        raise
    session.expire_all()
    return product_workflow_graph.get_active_workflow(
        session, product_id
    ) or product_workflow_graph.get_workflow_or_raise(session, workflow.id)


def create_workflow_node(
    session: Session,
    *,
    product_id: str,
    node_type: WorkflowNodeType,
    title: str,
    position_x: int,
    position_y: int,
    config_json: dict[str, Any] | None,
) -> ProductWorkflow:
    workflow = get_or_create_product_workflow(session, product_id)
    if node_type == WorkflowNodeType.PRODUCT_CONTEXT and any(
        node.node_type == WorkflowNodeType.PRODUCT_CONTEXT for node in workflow.nodes
    ):
        raise ValueError("商品资料节点已存在")
    node = WorkflowNode(
        workflow_id=workflow.id,
        node_type=node_type,
        title=title.strip() or product_workflow_graph.default_title_for_type(node_type),
        position_x=position_x,
        position_y=position_y,
        config_json=config_json or {},
    )
    session.add(node)
    workflow.updated_at = now_utc()
    session.commit()
    session.expire_all()
    return product_workflow_graph.get_workflow_or_raise(session, workflow.id)


def update_workflow_node(
    session: Session,
    *,
    node_id: str,
    title: str | None,
    position_x: int | None,
    position_y: int | None,
    config_json: dict[str, Any] | None,
) -> ProductWorkflow:
    node = product_workflow_graph.get_node_or_raise(session, node_id)
    if title is not None:
        node.title = title.strip() or product_workflow_graph.default_title_for_type(node.node_type)
    if position_x is not None:
        node.position_x = position_x
    if position_y is not None:
        node.position_y = position_y
    if config_json is not None:
        node.config_json = config_json
    node.workflow.updated_at = now_utc()
    session.commit()
    session.expire_all()
    return product_workflow_graph.get_workflow_or_raise(session, node.workflow_id)


def update_workflow_copy_set(
    session: Session,
    *,
    node_id: str,
    title: str | None,
    selling_points: list[str] | None,
    poster_headline: str | None,
    cta: str | None,
) -> ProductWorkflow:
    node = product_workflow_graph.get_node_or_raise(session, node_id)
    if node.node_type != WorkflowNodeType.COPY_GENERATION:
        raise ValueError("只有文案节点可以编辑文案")
    workflow_id = node.workflow_id
    workflow = product_workflow_graph.get_workflow_or_raise(session, workflow_id)
    copy_set_id = (node.output_json or {}).get("copy_set_id")
    if not isinstance(copy_set_id, str) or not copy_set_id:
        raise ValueError("文案节点还没有生成文案")

    copy_set = session.get(CopySet, copy_set_id)
    if copy_set is None or copy_set.product_id != workflow.product_id:
        raise ValueError("文案版本不存在")

    copy_set = update_copy_set(
        session,
        copy_set_id=copy_set.id,
        title=title,
        selling_points=selling_points,
        poster_headline=poster_headline,
        cta=cta,
    )
    node = product_workflow_graph.get_node_or_raise(session, node_id)
    output = dict(node.output_json or {})
    output.update(
        {
            "copy_set_id": copy_set.id,
            "creative_brief_id": copy_set.creative_brief_id,
            "title": copy_set.title,
            "poster_headline": copy_set.poster_headline,
            "selling_points": copy_set.selling_points,
            "cta": copy_set.cta,
            "manual_edit": True,
            "summary": f"文案：{copy_set.poster_headline}",
        }
    )
    node.output_json = output
    node.workflow.updated_at = now_utc()
    node.workflow.product.updated_at = now_utc()
    session.commit()
    session.expire_all()
    return product_workflow_graph.get_workflow_or_raise(session, workflow_id)


def upload_workflow_node_image(
    session: Session,
    *,
    node_id: str,
    image_bytes: bytes,
    filename: str,
    content_type: str,
    role: str | None = None,
    label: str | None = None,
    storage: LocalStorage | None = None,
) -> ProductWorkflow:
    """把上传图存为商品参考图，并绑定到参考图节点输出。"""
    node = product_workflow_graph.get_node_or_raise(session, node_id)
    if node.node_type != WorkflowNodeType.REFERENCE_IMAGE:
        raise ValueError("只有参考图节点可以上传图片")
    workflow = product_workflow_graph.get_workflow_or_raise(session, node.workflow_id)
    storage = storage or LocalStorage()
    relative_path = storage.save_reference_upload(workflow.product_id, filename, image_bytes)
    asset = SourceAsset(
        product_id=workflow.product_id,
        kind=SourceAssetKind.REFERENCE_IMAGE,
        original_filename=filename,
        mime_type=content_type or "application/octet-stream",
        storage_path=relative_path,
    )
    session.add(asset)
    session.flush()

    config = dict(node.config_json or {})
    if role is not None:
        config["role"] = role.strip() or "reference"
    if label is not None:
        config["label"] = label.strip() or filename
    config["source_asset_ids"] = [asset.id]
    node.config_json = config
    node.output_json = _image_asset_output(
        [asset],
        summary="已替换参考图",
        role=_optional_config_text(config, "role"),
        label=_optional_config_text(config, "label"),
    )
    node.status = WorkflowNodeStatus.SUCCEEDED
    node.failure_reason = None
    node.last_run_at = now_utc()
    workflow.updated_at = now_utc()
    workflow.product.updated_at = now_utc()
    session.commit()
    session.expire_all()
    return product_workflow_graph.get_workflow_or_raise(session, workflow.id)


def create_workflow_edge(
    session: Session,
    *,
    product_id: str,
    source_node_id: str,
    target_node_id: str,
    source_handle: str | None = None,
    target_handle: str | None = None,
) -> ProductWorkflow:
    workflow = get_or_create_product_workflow(session, product_id)
    nodes = {node.id for node in workflow.nodes}
    if source_node_id == target_node_id:
        raise ValueError("工作流连线不能连接到自身")
    if source_node_id not in nodes or target_node_id not in nodes:
        raise ValueError("工作流连线节点不属于当前商品")
    edge = WorkflowEdge(
        workflow_id=workflow.id,
        source_node_id=source_node_id,
        target_node_id=target_node_id,
        source_handle=source_handle,
        target_handle=target_handle,
    )
    session.add(edge)
    workflow.updated_at = now_utc()
    session.flush()
    session.expire(workflow, ["nodes", "edges"])
    refreshed = product_workflow_graph.get_workflow_or_raise(session, workflow.id)
    try:
        product_workflow_graph.topological_nodes(refreshed)
    except ValueError:
        session.rollback()
        raise
    session.commit()
    session.expire_all()
    return product_workflow_graph.get_workflow_or_raise(session, workflow.id)


def delete_workflow_edge(session: Session, *, edge_id: str) -> ProductWorkflow:
    edge = product_workflow_graph.get_edge_or_raise(session, edge_id)
    workflow_id = edge.workflow_id
    edge.workflow.updated_at = now_utc()
    session.delete(edge)
    session.commit()
    session.expire_all()
    return product_workflow_graph.get_workflow_or_raise(session, workflow_id)


def delete_workflow_node(session: Session, *, node_id: str) -> ProductWorkflow:
    node = product_workflow_graph.get_node_or_raise(session, node_id)
    workflow = product_workflow_graph.get_workflow_or_raise(session, node.workflow_id)
    if _active_workflow_run(workflow) is not None or node.status in {
        WorkflowNodeStatus.QUEUED,
        WorkflowNodeStatus.RUNNING,
    }:
        raise ValueError("运行中，稍后删除")

    workflow_id = workflow.id
    workflow.updated_at = now_utc()
    session.execute(
        delete(WorkflowEdge).where(
            (WorkflowEdge.workflow_id == workflow_id)
            & ((WorkflowEdge.source_node_id == node.id) | (WorkflowEdge.target_node_id == node.id))
        )
    )
    session.execute(delete(WorkflowNodeRun).where(WorkflowNodeRun.node_id == node.id))
    session.delete(node)
    session.commit()
    session.expire_all()
    return product_workflow_graph.get_workflow_or_raise(session, workflow_id)


def _active_workflow_run(workflow: ProductWorkflow) -> WorkflowRun | None:
    return next(
        (
            run
            for run in sorted(workflow.runs, key=lambda item: item.started_at, reverse=True)
            if run.status == WorkflowRunStatus.RUNNING
        ),
        None,
    )


def start_product_workflow_run(
    session: Session,
    *,
    product_id: str,
    start_node_id: str | None = None,
) -> WorkflowRunKickoff:
    workflow = get_or_create_product_workflow(session, product_id)
    active_run = _active_workflow_run(workflow)
    if active_run is not None:
        return WorkflowRunKickoff(workflow=workflow, run_id=active_run.id, created=False)

    ordered_nodes = product_workflow_graph.topological_nodes(workflow)
    node_ids_to_run = _node_ids_to_run(session, workflow, start_node_id)
    if not node_ids_to_run:
        raise ValueError("工作流没有可运行节点")

    run = WorkflowRun(workflow_id=workflow.id, status=WorkflowRunStatus.RUNNING)
    logger.info(
        "创建商品工作流运行: product_id=%s workflow_id=%s start_node_id=%s",
        product_id,
        workflow.id,
        start_node_id,
    )
    session.add(run)
    session.flush()
    for node in ordered_nodes:
        if node.id not in node_ids_to_run:
            continue
        node.status = WorkflowNodeStatus.QUEUED
        node.failure_reason = None
        node.last_run_at = now_utc()
        session.add(
            WorkflowNodeRun(
                workflow_run_id=run.id,
                node_id=node.id,
                status=WorkflowNodeStatus.QUEUED,
            )
        )
    workflow.updated_at = now_utc()
    try:
        session.commit()
    except IntegrityError:
        session.rollback()
        workflow = product_workflow_graph.get_workflow_or_raise(session, workflow.id)
        active_run = _active_workflow_run(workflow)
        if active_run is not None:
            return WorkflowRunKickoff(workflow=workflow, run_id=active_run.id, created=False)
        raise
    session.expire_all()
    return WorkflowRunKickoff(
        workflow=product_workflow_graph.get_workflow_or_raise(session, workflow.id),
        run_id=run.id,
        created=True,
    )


def run_product_workflow(
    session: Session,
    *,
    product_id: str,
    start_node_id: str | None = None,
) -> ProductWorkflow:
    kickoff = start_product_workflow_run(session, product_id=product_id, start_node_id=start_node_id)
    if kickoff.created:
        execute_product_workflow_run(kickoff.run_id)
        session.expire_all()
        return product_workflow_graph.get_workflow_or_raise(session, kickoff.workflow.id)
    return kickoff.workflow


def execute_product_workflow_run(run_id: str) -> None:
    session_factory = get_session_factory()
    session = session_factory()
    try:
        try:
            _execute_product_workflow_run(session, run_id=run_id)
        except Exception as exc:  # noqa: BLE001
            session.rollback()
            _mark_workflow_run_failed(
                session,
                run_id=run_id,
                failed_node_id=None,
                reason=str(exc)[:1000],
            )
    finally:
        session.close()


def mark_workflow_run_enqueue_failed(session: Session, *, run_id: str, reason: str) -> None:
    """Mark a just-created workflow run failed when its durable queue message cannot be sent."""

    _mark_workflow_run_failed(
        session,
        run_id=run_id,
        failed_node_id=None,
        reason=reason[:1000],
    )


def _execute_product_workflow_run(session: Session, *, run_id: str) -> None:
    run = session.get(WorkflowRun, run_id)
    if run is None:
        return
    if run.status != WorkflowRunStatus.RUNNING:
        return
    workflow = product_workflow_graph.get_workflow_or_raise(session, run.workflow_id)
    ordered_nodes = product_workflow_graph.topological_nodes(workflow)
    run_node_ids = {node_run.node_id for node_run in run.node_runs}
    node_runs_by_node_id = {node_run.node_id: node_run for node_run in run.node_runs}
    if any(node_run.status == WorkflowNodeStatus.RUNNING for node_run in run.node_runs):
        return

    for ordered_node in ordered_nodes:
        if ordered_node.id not in run_node_ids:
            continue
        node = product_workflow_graph.get_node_or_raise(session, ordered_node.id)
        node_run = node_runs_by_node_id.get(node.id)
        if node_run is None:
            continue
        session.refresh(node_run)
        if node_run.status == WorkflowNodeStatus.RUNNING:
            return
        if node_run.status != WorkflowNodeStatus.QUEUED:
            continue
        if not _claim_workflow_node_run(session, node_run_id=node_run.id, node_id=node.id):
            return
        node = product_workflow_graph.get_node_or_raise(session, ordered_node.id)
        node_run = session.get(WorkflowNodeRun, node_run.id)
        if node_run is None:
            return
        try:
            logger.info(
                "开始执行工作流节点: run_id=%s node_id=%s node_type=%s",
                run_id,
                node.id,
                node.node_type.value,
            )
            output = _execute_node(session, workflow_id=workflow.id, node=node)
        except Exception as exc:  # noqa: BLE001
            session.rollback()
            _mark_workflow_run_failed(
                session,
                run_id=run_id,
                failed_node_id=ordered_node.id,
                reason=str(exc)[:1000],
            )
            return

        node.output_json = output
        node.status = WorkflowNodeStatus.SUCCEEDED
        node.failure_reason = None
        node.last_run_at = now_utc()
        node_run.status = WorkflowNodeStatus.SUCCEEDED
        node_run.output_json = output
        node_run.copy_set_id = output.get("copy_set_id")
        poster_ids = output.get("poster_variant_ids") if isinstance(output.get("poster_variant_ids"), list) else []
        node_run.poster_variant_id = poster_ids[0] if poster_ids else output.get("poster_variant_id")
        node_run.image_session_asset_id = output.get("image_session_asset_id")
        node_run.finished_at = now_utc()
        workflow.updated_at = now_utc()
        session.commit()
        logger.info("工作流节点执行成功: run_id=%s node_id=%s", run_id, node.id)

    persisted_run = session.scalar(
        select(WorkflowRun).options(selectinload(WorkflowRun.node_runs)).where(WorkflowRun.id == run_id)
    )
    if (
        persisted_run is not None
        and persisted_run.status == WorkflowRunStatus.RUNNING
        and persisted_run.node_runs
        and all(node_run.status == WorkflowNodeStatus.SUCCEEDED for node_run in persisted_run.node_runs)
    ):
        persisted_run.status = WorkflowRunStatus.SUCCEEDED
        persisted_run.finished_at = now_utc()
        logger.info("工作流运行成功: run_id=%s workflow_id=%s", run_id, persisted_run.workflow_id)
    session.commit()


def _claim_workflow_node_run(session: Session, *, node_run_id: str, node_id: str) -> bool:
    """Atomically claim one queued node run so duplicate Dramatiq messages do not execute it twice."""

    now = now_utc()
    result = session.execute(
        update(WorkflowNodeRun)
        .where(
            WorkflowNodeRun.id == node_run_id,
            WorkflowNodeRun.status == WorkflowNodeStatus.QUEUED,
        )
        .values(status=WorkflowNodeStatus.RUNNING, started_at=now)
    )
    if result.rowcount != 1:
        session.rollback()
        return False
    session.execute(
        update(WorkflowNode)
        .where(WorkflowNode.id == node_id)
        .values(status=WorkflowNodeStatus.RUNNING, failure_reason=None, last_run_at=now)
    )
    session.commit()
    return True


def _mark_workflow_run_failed(
    session: Session,
    *,
    run_id: str,
    failed_node_id: str | None,
    reason: str,
) -> None:
    persisted_run = session.get(WorkflowRun, run_id)
    if persisted_run is None:
        return
    now = now_utc()
    if failed_node_id is not None:
        failed_node = product_workflow_graph.get_node_or_raise(session, failed_node_id)
        failed_node.status = WorkflowNodeStatus.FAILED
        failed_node.failure_reason = reason
        failed_node.last_run_at = now
    for node_run in persisted_run.node_runs:
        if node_run.node_id == failed_node_id:
            node_run.status = WorkflowNodeStatus.FAILED
            node_run.failure_reason = reason
            node_run.finished_at = now
        elif node_run.status == WorkflowNodeStatus.QUEUED:
            skipped_node = session.get(WorkflowNode, node_run.node_id)
            if skipped_node is not None:
                skipped_node.status = WorkflowNodeStatus.IDLE
                skipped_node.failure_reason = None
            node_run.status = WorkflowNodeStatus.FAILED
            node_run.failure_reason = "上游节点失败"
            node_run.finished_at = now
    logger.warning("工作流运行失败: run_id=%s failed_node_id=%s reason=%s", run_id, failed_node_id, reason)
    persisted_run.status = WorkflowRunStatus.FAILED
    persisted_run.failure_reason = reason
    persisted_run.finished_at = now
    persisted_run.workflow.updated_at = now
    session.commit()


def _node_ids_to_run(session: Session, workflow: ProductWorkflow, start_node_id: str | None) -> set[str]:
    if start_node_id is None:
        return {node.id for node in workflow.nodes}
    nodes_by_id = {node.id: node for node in workflow.nodes}
    if start_node_id not in nodes_by_id:
        raise ValueError("工作流节点不属于当前商品")
    incoming: dict[str, list[str]] = defaultdict(list)
    for edge in workflow.edges:
        incoming[edge.target_node_id].append(edge.source_node_id)

    selected: set[str] = set()

    def include_missing_required_upstream(node_id: str) -> None:
        for source_id in incoming[node_id]:
            source_node = nodes_by_id.get(source_id)
            target_node = nodes_by_id[node_id]
            if source_node is None:
                raise ValueError("工作流连线引用了不存在的节点")
            if _node_has_reusable_output(session, workflow, source_node, target_node=target_node):
                continue
            if not _should_execute_missing_upstream(source_node, target_node):
                continue
            if source_id in selected:
                continue
            include_missing_required_upstream(source_id)
            selected.add(source_id)

    include_missing_required_upstream(start_node_id)
    selected.add(start_node_id)
    return selected


def _node_has_reusable_output(
    session: Session,
    workflow: ProductWorkflow,
    node: WorkflowNode,
    *,
    target_node: WorkflowNode | None = None,
) -> bool:
    if node.node_type == WorkflowNodeType.PRODUCT_CONTEXT:
        return True
    if node.status != WorkflowNodeStatus.SUCCEEDED:
        return False
    output = node.output_json or {}
    if node.node_type == WorkflowNodeType.REFERENCE_IMAGE:
        return _node_has_valid_reference_assets(session, workflow.product_id, node)
    if node.node_type == WorkflowNodeType.COPY_GENERATION:
        copy_set_id = output.get("copy_set_id")
        if not isinstance(copy_set_id, str):
            return False
        copy_set = session.get(CopySet, copy_set_id)
        return copy_set is not None and copy_set.product_id == workflow.product_id
    if node.node_type == WorkflowNodeType.IMAGE_GENERATION:
        if target_node is not None and target_node.node_type == WorkflowNodeType.REFERENCE_IMAGE:
            return _image_generation_filled_reference_target(
                session,
                workflow=workflow,
                image_node=node,
                reference_node=target_node,
            )
        poster_ids = output.get("poster_variant_ids")
        filled_ids = output.get("filled_source_asset_ids")
        source_asset_ids = _source_asset_ids_from_config(output)
        if isinstance(filled_ids, list):
            source_asset_ids.extend(item for item in filled_ids if isinstance(item, str))
        has_source_assets = _valid_source_asset_ids(session, workflow.product_id, source_asset_ids)
        has_posters = False
        if isinstance(poster_ids, list):
            posters = session.scalars(select(PosterVariant).where(PosterVariant.id.in_(poster_ids))).all()
            has_posters = any(poster.product_id == workflow.product_id for poster in posters)
        return has_source_assets or has_posters
    return False


def _image_generation_filled_reference_target(
    session: Session,
    *,
    workflow: ProductWorkflow,
    image_node: WorkflowNode,
    reference_node: WorkflowNode,
) -> bool:
    """Return whether an image node's previous output satisfies a specific reference slot edge."""
    output = image_node.output_json or {}
    filled_reference_node_ids = output.get("filled_reference_node_ids")
    output_names_target = (
        isinstance(filled_reference_node_ids, list) and reference_node.id in filled_reference_node_ids
    )
    target_has_assets = _node_has_valid_reference_assets(session, workflow.product_id, reference_node)
    if output_names_target:
        return target_has_assets
    # Older outputs may not name filled reference nodes. The target slot itself is still authoritative: if it
    # already exposes a live first-class image artifact, the upstream image node does not need to rerun.
    return target_has_assets


def _node_has_valid_reference_assets(session: Session, product_id: str, node: WorkflowNode) -> bool:
    asset_ids = list(
        dict.fromkeys(
            [
                *_source_asset_ids_from_config(node.output_json or {}),
                *_source_asset_ids_from_config(node.config_json or {}),
            ]
        )
    )
    return _valid_source_asset_ids(session, product_id, asset_ids)


def _valid_source_asset_ids(session: Session, product_id: str, asset_ids: list[str]) -> bool:
    if not asset_ids:
        return False
    assets = session.scalars(select(SourceAsset).where(SourceAsset.id.in_(asset_ids))).all()
    return any(asset.product_id == product_id for asset in assets)


def _should_execute_missing_upstream(source_node: WorkflowNode, target_node: WorkflowNode) -> bool:
    if source_node.node_type == WorkflowNodeType.PRODUCT_CONTEXT:
        return False
    if source_node.node_type == WorkflowNodeType.REFERENCE_IMAGE:
        return bool(_source_asset_ids_from_config(source_node.config_json or {}))
    if source_node.node_type == WorkflowNodeType.COPY_GENERATION:
        return target_node.node_type in {WorkflowNodeType.COPY_GENERATION, WorkflowNodeType.IMAGE_GENERATION}
    if source_node.node_type == WorkflowNodeType.IMAGE_GENERATION:
        return target_node.node_type in {WorkflowNodeType.REFERENCE_IMAGE, WorkflowNodeType.IMAGE_GENERATION}
    return False


def _execute_node(session: Session, *, workflow_id: str, node: WorkflowNode) -> dict[str, Any]:
    workflow = product_workflow_graph.get_workflow_or_raise(session, workflow_id)
    product = workflow.product
    if node.node_type == WorkflowNodeType.PRODUCT_CONTEXT:
        return _execute_product_context(product, node)
    if node.node_type == WorkflowNodeType.REFERENCE_IMAGE:
        return _execute_reference_image(session, workflow=workflow, node=node)
    if node.node_type == WorkflowNodeType.COPY_GENERATION:
        return _execute_copy_generation(session, workflow=workflow, node=node)
    if node.node_type == WorkflowNodeType.IMAGE_GENERATION:
        return _execute_image_generation(session, workflow=workflow, node=node)
    raise ValueError("工作流节点类型不支持")


def _execute_product_context(product: Product, node: WorkflowNode) -> dict[str, Any]:
    context = _product_context_values(product, node)
    source = _find_source_asset(product)
    return {
        "product_id": product.id,
        "name": context["name"],
        "category": context["category"],
        "price": context["price"],
        "source_note": context["source_note"],
        "source_asset_id": source.id if source else None,
        "summary": "商品已读取。",
    }


def _execute_reference_image(session: Session, *, workflow: ProductWorkflow, node: WorkflowNode) -> dict[str, Any]:
    asset_ids = _source_asset_ids_from_config(node.config_json)
    assets = list(session.scalars(select(SourceAsset).where(SourceAsset.id.in_(asset_ids)))) if asset_ids else []
    assets = [asset for asset in assets if asset.product_id == workflow.product_id]
    if not assets:
        return _image_asset_output([], summary="参考图为空")
    return _image_asset_output(
        assets,
        summary=f"参考图 {len(assets)} 张",
        role=_optional_config_text(node.config_json, "role"),
        label=_optional_config_text(node.config_json, "label") or node.title,
    )


def _execute_copy_generation(session: Session, *, workflow: ProductWorkflow, node: WorkflowNode) -> dict[str, Any]:
    product = workflow.product
    product_context = _effective_product_context(workflow, node.id)
    existing_output = node.output_json or {}
    existing_copy_set_id = existing_output.get("copy_set_id")
    if existing_output.get("manual_edit") is True and isinstance(existing_copy_set_id, str):
        copy_set = session.get(CopySet, existing_copy_set_id)
        if copy_set is not None and copy_set.product_id == product.id:
            return _copy_node_output(copy_set, creative_brief_id=copy_set.creative_brief_id, manual_edit=True)

    source = _find_source_asset(product)
    if source is None:
        raise ValueError("商品缺少原始图片")
    storage = LocalStorage()
    product_input = ProductInput(
        name=product_context["name"] or product.name,
        category=product_context["category"],
        price=product_context["price"],
        source_note=product_context["source_note"],
        image_path=str(storage.resolve(source.storage_path)),
    )
    incoming_context = _collect_incoming_context(workflow, node.id)
    reference_images = _reference_image_inputs_for_copy(session, workflow=workflow, node_id=node.id, storage=storage)
    instruction = _instruction_with_upstream_text(
        _optional_config_text(node.config_json, "instruction"),
        incoming_context,
    )
    provider = get_text_provider()
    brief_payload, brief_model = provider.generate_brief(product_input)
    brief = CreativeBrief(
        product_id=product.id,
        payload=brief_payload.model_dump(),
        provider_name=provider.provider_name,
        model_name=brief_model,
        prompt_version=provider.prompt_version,
    )
    session.add(brief)
    session.flush()

    copy_payload, copy_model = provider.generate_copy(
        product_input,
        brief_payload,
        instruction=instruction,
        reference_images=reference_images,
    )
    copy_set = CopySet(
        product_id=product.id,
        creative_brief_id=brief.id,
        status=CopyStatus.DRAFT,
        title=copy_payload.title,
        selling_points=copy_payload.selling_points,
        poster_headline=copy_payload.poster_headline,
        cta=copy_payload.cta,
        model_title=copy_payload.title,
        model_selling_points=copy_payload.selling_points,
        model_poster_headline=copy_payload.poster_headline,
        model_cta=copy_payload.cta,
        provider_name=provider.provider_name,
        model_name=copy_model,
        prompt_version=provider.prompt_version,
    )
    session.add(copy_set)
    session.flush()
    product.updated_at = now_utc()
    output = _copy_node_output(copy_set, creative_brief_id=brief.id)
    output["instruction"] = instruction
    return output


def _execute_image_generation(session: Session, *, workflow: ProductWorkflow, node: WorkflowNode) -> dict[str, Any]:
    product = workflow.product
    incoming_context = _collect_incoming_context(workflow, node.id)
    product_context = _effective_product_context(workflow, node.id)
    copy_set_id = _optional_config_text(node.config_json, "copy_set_id") or incoming_context.copy_set_id
    copy_set = session.get(CopySet, copy_set_id) if copy_set_id else product.confirmed_copy_set
    if copy_set is None or copy_set.product_id != product.id:
        copy_set = _create_context_copy_set(session, product=product, product_context=product_context, node=node)

    source = _find_source_asset(product)
    if source is None:
        raise ValueError("商品缺少原始图片")

    storage = LocalStorage()
    downstream_reference_nodes = _downstream_reference_nodes(workflow, node.id)

    reference_assets = _reference_assets_for_image_generation(
        session,
        workflow,
        incoming_context.image_asset_ids,
        incoming_context.poster_variant_ids,
    )
    render_input = PosterGenerationInput(
        product_name=product_context["name"] or product.name,
        category=product_context["category"],
        price=product_context["price"],
        source_note=product_context["source_note"],
        instruction=_image_instruction_with_context(node, incoming_context.text_contexts),
        image_size=_image_size_from_config(node.config_json),
        title=copy_set.title,
        selling_points=copy_set.selling_points,
        poster_headline=copy_set.poster_headline,
        cta=copy_set.cta,
        source_image=Path(storage.resolve(source.storage_path)),
        reference_images=[
            ReferenceImageInput(
                path=Path(storage.resolve(asset.storage_path)),
                mime_type=asset.mime_type,
                filename=asset.original_filename,
            )
            for asset in reference_assets
        ],
    )
    poster_ids: list[str] = []
    filled_source_asset_ids: list[str] = []
    filled_reference_node_ids: list[str] = []
    settings = get_runtime_settings()
    renderer = PosterRenderer()
    kind = _poster_kind_from_config(node.config_json)
    generation_targets = downstream_reference_nodes or [None]
    for target_index, target_node in enumerate(generation_targets, start=1):
        if settings.poster_generation_mode == "generated":
            image_provider = get_image_provider()
            generated_image, image_model = image_provider.generate_poster_image(render_input, kind)
            content = generated_image.bytes_data
            width = generated_image.width
            height = generated_image.height
            template_name = f"workflow:{image_provider.provider_name}:{generated_image.variant_label}:{image_model}"
            mime_type = generated_image.mime_type
        else:
            content = renderer.render(render_input, kind)
            width = 1080
            height = 1080 if kind == PosterKind.MAIN_IMAGE else 1440
            template_name = f"workflow:{'default-main' if kind == PosterKind.MAIN_IMAGE else 'default-promo'}"
            mime_type = "image/png"
        relative_path = storage.save_generated_image(
            product.id,
            f"workflow-{kind.value}-{target_index}",
            content,
            suffix=infer_extension(mime_type),
        )
        poster = PosterVariant(
            product_id=product.id,
            copy_set_id=copy_set.id,
            kind=kind,
            template_name=template_name,
            storage_path=relative_path,
            mime_type=mime_type,
            width=width,
            height=height,
        )
        session.add(poster)
        session.flush()
        poster_ids.append(poster.id)

        if target_node is not None:
            filename = f"reference-{target_index}{infer_extension(mime_type)}"
            reference_path = storage.save_reference_upload(product.id, filename, content)
            asset = SourceAsset(
                product_id=product.id,
                kind=SourceAssetKind.REFERENCE_IMAGE,
                original_filename=filename,
                mime_type=mime_type,
                storage_path=reference_path,
            )
            session.add(asset)
            session.flush()
            filled_source_asset_ids.append(asset.id)
            filled_reference_node_ids.append(target_node.id)
            _fill_reference_node(target_node, asset)
    product.updated_at = now_utc()
    return {
        "copy_set_id": copy_set.id,
        "poster_variant_ids": poster_ids,
        "image_asset_ids": incoming_context.image_asset_ids,
        "filled_source_asset_ids": filled_source_asset_ids,
        "filled_reference_node_ids": filled_reference_node_ids,
        "target_count": len(downstream_reference_nodes),
        "size": _image_size_from_config(node.config_json),
        "instruction": _optional_config_text(node.config_json, "instruction"),
        "summary": (
            f"已填充 {len(filled_reference_node_ids)} 个参考图"
            if filled_reference_node_ids
            else f"已生成 {len(poster_ids)} 张图片"
        ),
    }


def _normalize_product_context_singleton(session: Session, workflow: ProductWorkflow) -> bool:
    product_nodes = sorted(
        [node for node in workflow.nodes if node.node_type == WorkflowNodeType.PRODUCT_CONTEXT],
        key=lambda item: (item.created_at, item.position_x, item.position_y),
    )
    changed = False
    if not product_nodes:
        context = WorkflowNode(
            workflow_id=workflow.id,
            node_type=WorkflowNodeType.PRODUCT_CONTEXT,
            title="商品",
            position_x=40,
            position_y=120,
            config_json={},
        )
        session.add(context)
        session.flush()
        product_nodes = [context]
        changed = True
    keeper = product_nodes[0]
    duplicate_ids = {node.id for node in product_nodes[1:]}
    if duplicate_ids:
        session.execute(
            delete(WorkflowEdge).where(
                (WorkflowEdge.workflow_id == workflow.id)
                & (
                    WorkflowEdge.source_node_id.in_(duplicate_ids)
                    | WorkflowEdge.target_node_id.in_(duplicate_ids)
                )
            )
        )
        session.execute(delete(WorkflowNodeRun).where(WorkflowNodeRun.node_id.in_(duplicate_ids)))
        for duplicate in product_nodes[1:]:
            session.delete(duplicate)
        changed = True
    non_context_targets = {
        node.id
        for node in workflow.nodes
        if node.node_type in {WorkflowNodeType.COPY_GENERATION, WorkflowNodeType.IMAGE_GENERATION}
        and node.id not in duplicate_ids
    }
    existing_targets = {
        edge.target_node_id
        for edge in workflow.edges
        if edge.source_node_id == keeper.id and edge.target_node_id in non_context_targets
    }
    for target_id in sorted(non_context_targets - existing_targets):
        session.add(
            WorkflowEdge(
                workflow_id=workflow.id,
                source_node_id=keeper.id,
                target_node_id=target_id,
                source_handle="output",
                target_handle="input",
            )
        )
        changed = True
    if changed:
        workflow.updated_at = now_utc()
    return changed


def _create_context_copy_set(
    session: Session,
    *,
    product: Product,
    product_context: dict[str, str | None],
    node: WorkflowNode,
) -> CopySet:
    instruction = _optional_config_text(node.config_json, "instruction")
    product_name = product_context["name"] or product.name
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
        selling_points.append(f"突出{product_name}的商品质感")
    headline = instruction or f"{product_name} 商品图"
    title = f"{product_name} 商品图文案"
    cta = "立即了解"
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


def _effective_product_context(workflow: ProductWorkflow, target_node_id: str) -> dict[str, str | None]:
    product = workflow.product
    context = _product_context_values(product)
    incoming_context_nodes = [
        node
        for edge in workflow.edges
        for node in workflow.nodes
        if edge.target_node_id == target_node_id
        and edge.source_node_id == node.id
        and node.node_type == WorkflowNodeType.PRODUCT_CONTEXT
    ]
    for node in sorted(incoming_context_nodes, key=lambda item: item.last_run_at or item.updated_at, reverse=True):
        output = node.output_json or {}
        context.update(
            {
                "name": _output_text(
                    output,
                    "name",
                    fallback=_configured_text(node.config_json, "name", fallback=context["name"]),
                )
                or product.name,
                "category": _output_text(
                    output,
                    "category",
                    fallback=_configured_text(node.config_json, "category", fallback=context["category"]),
                ),
                "price": _output_text(
                    output,
                    "price",
                    fallback=_configured_text(node.config_json, "price", fallback=context["price"]),
                ),
                "source_note": _output_text(
                    output,
                    "source_note",
                    fallback=_configured_text(node.config_json, "source_note", fallback=context["source_note"]),
                ),
            }
        )
        break
    return context


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
    return normalize_image_size(raw, label="生图尺寸")


class _IncomingContext:
    def __init__(self) -> None:
        self.copy_set_id: str | None = None
        self.image_asset_ids: list[str] = []
        self.poster_variant_ids: list[str] = []
        self.text_contexts: list[str] = []


def _collect_incoming_context(workflow: ProductWorkflow, node_id: str) -> _IncomingContext:
    context = _IncomingContext()
    incoming_sources = [edge.source_node_id for edge in workflow.edges if edge.target_node_id == node_id]
    candidates = [node for node in workflow.nodes if node.id in incoming_sources and node.output_json]
    for candidate in sorted(candidates, key=lambda item: item.last_run_at or item.updated_at, reverse=True):
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
        for key in ("text", "name", "source_note", "title", "poster_headline", "cta", "summary"):
            value = output.get(key)
            if isinstance(value, str) and value.strip():
                context.text_contexts.append(value.strip())
        selling_points = output.get("selling_points")
        if isinstance(selling_points, list):
            context.text_contexts.extend(
                item.strip() for item in selling_points if isinstance(item, str) and item.strip()
            )
    context.image_asset_ids = list(dict.fromkeys(context.image_asset_ids))
    context.poster_variant_ids = list(dict.fromkeys(context.poster_variant_ids))
    context.text_contexts = list(dict.fromkeys(context.text_contexts))
    return context


def _reference_assets_for_image_generation(
    session: Session,
    workflow: ProductWorkflow,
    incoming_source_asset_ids: list[str],
    incoming_poster_variant_ids: list[str],
) -> list[SourceAsset]:
    product = workflow.product
    source = _find_source_asset(product)
    assets: list[SourceAsset] = [source] if source else []
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


def _fill_reference_node(node: WorkflowNode, asset: SourceAsset) -> None:
    config = dict(node.config_json or {})
    config["source_asset_ids"] = [asset.id]
    config.setdefault("role", "reference")
    config.setdefault("label", node.title)
    node.config_json = config
    node.output_json = _image_asset_output(
        [asset],
        summary="已填充参考图",
        role=_optional_config_text(config, "role"),
        label=_optional_config_text(config, "label"),
    )
    node.status = WorkflowNodeStatus.SUCCEEDED
    node.failure_reason = None
    node.last_run_at = now_utc()


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


def latest_workflow_runs(workflow: ProductWorkflow, limit: int = 10) -> list[WorkflowRun]:
    return product_workflow_graph.latest_workflow_runs(workflow, limit=limit)
