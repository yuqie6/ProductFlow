from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from sqlalchemy import delete
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from productflow_backend.application import product_workflow_graph
from productflow_backend.application.canvas_templates import CanvasTemplateNodeSpec, get_builtin_canvas_template
from productflow_backend.application.image_generation_core import normalize_image_generation_tool_options
from productflow_backend.application.product_workflow_artifacts import (
    fill_reference_node,
    image_asset_output,
    source_asset_for_poster_variant,
)
from productflow_backend.application.product_workflow_context import image_size_from_config, optional_config_text
from productflow_backend.application.product_workflow_templates import materialize_canvas_template_graph
from productflow_backend.application.time import now_utc
from productflow_backend.application.use_cases import update_copy_set
from productflow_backend.domain.durable_generation_tasks import WORKFLOW_RUN_GENERATION_TASK_CONTRACT
from productflow_backend.domain.enums import (
    SourceAssetKind,
    WorkflowNodeStatus,
    WorkflowNodeType,
)
from productflow_backend.domain.errors import BusinessError, BusinessValidationError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    CopySet,
    PosterVariant,
    ProductWorkflow,
    SourceAsset,
    WorkflowEdge,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRun,
)
from productflow_backend.infrastructure.image.base import infer_extension
from productflow_backend.infrastructure.storage import LocalStorage

NODE_GROUP_TEMPLATE_COLLISION_NODE_WIDTH = 248
NODE_GROUP_TEMPLATE_COLLISION_NODE_HEIGHT = 248
NODE_GROUP_TEMPLATE_COLLISION_GAP = 32


@dataclass(frozen=True, slots=True)
class _NodeBounds:
    left: int
    top: int
    right: int
    bottom: int


def _node_bounds(position_x: int, position_y: int) -> _NodeBounds:
    return _NodeBounds(
        left=position_x,
        top=position_y,
        right=position_x + NODE_GROUP_TEMPLATE_COLLISION_NODE_WIDTH,
        bottom=position_y + NODE_GROUP_TEMPLATE_COLLISION_NODE_HEIGHT,
    )


def _node_bounds_overlap(first: _NodeBounds, second: _NodeBounds) -> bool:
    return (
        first.left < second.right
        and first.right > second.left
        and first.top < second.bottom
        and first.bottom > second.top
    )


def _node_group_template_offsets(
    *,
    template_nodes: tuple[CanvasTemplateNodeSpec, ...],
    existing_nodes: list[WorkflowNode],
    position_x: int,
    position_y: int,
) -> tuple[int, int]:
    min_x = min(node.position_x for node in template_nodes)
    min_y = min(node.position_y for node in template_nodes)
    position_x_offset = position_x - min_x
    position_y_offset = position_y - min_y
    existing_bounds = [_node_bounds(node.position_x, node.position_y) for node in existing_nodes]

    for _ in range(len(existing_bounds) + 1):
        template_bounds = [
            _node_bounds(node.position_x + position_x_offset, node.position_y + position_y_offset)
            for node in template_nodes
        ]
        overlapping_existing_bounds = [
            existing
            for existing in existing_bounds
            if any(_node_bounds_overlap(template_bound, existing) for template_bound in template_bounds)
        ]
        if not overlapping_existing_bounds:
            return position_x_offset, position_y_offset
        position_y_offset = max(bound.bottom for bound in overlapping_existing_bounds) + (
            NODE_GROUP_TEMPLATE_COLLISION_GAP - min_y
        )

    return position_x_offset, position_y_offset


def _single_product_context_node(workflow: ProductWorkflow) -> WorkflowNode:
    product_context_nodes = [node for node in workflow.nodes if node.node_type == WorkflowNodeType.PRODUCT_CONTEXT]
    if len(product_context_nodes) != 1:
        raise BusinessValidationError("节点组模板需要当前画布中的商品资料节点")
    return product_context_nodes[0]


def _active_workflow_run(workflow: ProductWorkflow) -> WorkflowRun | None:
    return next(
        (
            run
            for run in sorted(workflow.runs, key=lambda item: item.started_at, reverse=True)
            if WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_active(run.status)
        ),
        None,
    )


def _normalize_node_config(node_type: WorkflowNodeType, config_json: dict[str, Any] | None) -> dict[str, Any]:
    config = dict(config_json or {})
    if node_type == WorkflowNodeType.IMAGE_GENERATION:
        normalized_size = image_size_from_config(config)
        if normalized_size is not None:
            config["size"] = normalized_size
        if "tool_options" in config:
            raw_tool_options = config.get("tool_options")
            config["tool_options"] = normalize_image_generation_tool_options(
                raw_tool_options if isinstance(raw_tool_options, dict) else None
            )
    return config


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
        raise BusinessValidationError("商品资料节点已存在")
    node = WorkflowNode(
        workflow_id=workflow.id,
        node_type=node_type,
        title=title.strip() or product_workflow_graph.default_title_for_type(node_type),
        position_x=position_x,
        position_y=position_y,
        config_json=_normalize_node_config(node_type, config_json),
    )
    session.add(node)
    workflow.updated_at = now_utc()
    session.commit()
    session.expire_all()
    return product_workflow_graph.get_workflow_or_raise(session, workflow.id)


def apply_node_group_template_to_workflow(
    session: Session,
    *,
    product_id: str,
    template_key: str,
    position_x: int,
    position_y: int,
) -> ProductWorkflow:
    template = get_builtin_canvas_template(template_key.strip())
    if template.kind != "node_group":
        raise BusinessValidationError("画布内只能添加节点组模板，完整画布模板请在创建商品时选择")
    workflow = product_workflow_graph.get_active_workflow(session, product_id)
    if workflow is None:
        product_workflow_graph.get_product_or_raise(session, product_id)
        raise BusinessValidationError("需要先创建或打开画布后才能添加节点组")
    external_source_nodes = {"existing_product_context": _single_product_context_node(workflow)}
    position_x_offset, position_y_offset = _node_group_template_offsets(
        template_nodes=template.nodes,
        existing_nodes=list(workflow.nodes),
        position_x=position_x,
        position_y=position_y,
    )
    materialize_canvas_template_graph(
        session,
        workflow=workflow,
        template=template,
        position_x_offset=position_x_offset,
        position_y_offset=position_y_offset,
        external_source_nodes_by_template_source=external_source_nodes,
    )
    workflow.updated_at = now_utc()
    session.flush()
    session.expire(workflow, ["nodes", "edges"])
    refreshed = product_workflow_graph.get_workflow_or_raise(session, workflow.id)
    try:
        product_workflow_graph.topological_nodes(refreshed)
    except BusinessError:
        session.rollback()
        raise
    except ValueError as exc:
        session.rollback()
        raise BusinessValidationError(str(exc)) from exc
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
        node.config_json = _normalize_node_config(node.node_type, config_json)
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
        raise BusinessValidationError("只有文案节点可以编辑文案")
    workflow_id = node.workflow_id
    workflow = product_workflow_graph.get_workflow_or_raise(session, workflow_id)
    copy_set_id = (node.output_json or {}).get("copy_set_id")
    if not isinstance(copy_set_id, str) or not copy_set_id:
        raise BusinessValidationError("文案节点还没有生成文案")

    copy_set = session.get(CopySet, copy_set_id)
    if copy_set is None or copy_set.product_id != workflow.product_id:
        raise NotFoundError("文案版本不存在")

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
        raise BusinessValidationError("只有参考图节点可以上传图片")
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
    config.pop("source_poster_variant_id", None)
    node.config_json = config
    node.output_json = image_asset_output(
        [asset],
        summary="已替换参考图",
        role=optional_config_text(config, "role"),
        label=optional_config_text(config, "label"),
    )
    node.status = WorkflowNodeStatus.SUCCEEDED
    node.failure_reason = None
    node.last_run_at = now_utc()
    workflow.updated_at = now_utc()
    workflow.product.updated_at = now_utc()
    session.commit()
    session.expire_all()
    return product_workflow_graph.get_workflow_or_raise(session, workflow.id)


def bind_workflow_node_image(
    session: Session,
    *,
    node_id: str,
    source_asset_id: str | None = None,
    poster_variant_id: str | None = None,
    storage: LocalStorage | None = None,
) -> ProductWorkflow:
    """把已有商品图片绑定到参考图节点。

    SourceAsset 直接复用已有行；PosterVariant 优先复用同一次工作流生成时已经填充的 SourceAsset，
    找不到时再把海报文件复制成新的 reference SourceAsset。
    """
    if bool(source_asset_id) == bool(poster_variant_id):
        raise BusinessValidationError("请选择一张图片")

    node = product_workflow_graph.get_node_or_raise(session, node_id)
    if node.node_type != WorkflowNodeType.REFERENCE_IMAGE:
        raise BusinessValidationError("只有参考图节点可以填充图片")
    workflow = product_workflow_graph.get_workflow_or_raise(session, node.workflow_id)

    source_poster_variant_id: str | None = None
    if source_asset_id:
        asset = session.get(SourceAsset, source_asset_id)
        if asset is None or asset.product_id != workflow.product_id:
            raise NotFoundError("源图不存在")
        if asset.kind != SourceAssetKind.REFERENCE_IMAGE:
            raise BusinessValidationError("只能绑定参考图素材")
        if asset.source_poster_variant_id:
            poster = session.get(PosterVariant, asset.source_poster_variant_id)
            if poster is not None and poster.product_id == workflow.product_id:
                source_poster_variant_id = poster.id
    else:
        poster = session.get(PosterVariant, poster_variant_id)
        if poster is None or poster.product_id != workflow.product_id:
            raise NotFoundError("海报不存在")
        source_poster_variant_id = poster.id
        asset = source_asset_for_poster_variant(session, workflow=workflow, poster_variant_id=poster.id)
        if asset is None:
            storage = storage or LocalStorage()
            try:
                content = storage.resolve(poster.storage_path).read_bytes()
            except (OSError, ValueError) as exc:
                raise BusinessValidationError("海报文件不存在") from exc
            filename = f"poster-{poster.id}{infer_extension(poster.mime_type)}"
            reference_path = storage.save_reference_upload(workflow.product_id, filename, content)
            asset = SourceAsset(
                product_id=workflow.product_id,
                kind=SourceAssetKind.REFERENCE_IMAGE,
                original_filename=filename,
                mime_type=poster.mime_type,
                storage_path=reference_path,
                source_poster_variant_id=poster.id,
            )
            session.add(asset)
            session.flush()

    fill_reference_node(node, asset, source_poster_variant_id=source_poster_variant_id)
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
        raise BusinessValidationError("工作流连线不能连接到自身")
    if source_node_id not in nodes or target_node_id not in nodes:
        raise BusinessValidationError("工作流连线节点不属于当前商品")
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
    except BusinessError:
        session.rollback()
        raise
    except ValueError as exc:
        session.rollback()
        raise BusinessValidationError(str(exc)) from exc
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
    if (
        _active_workflow_run(workflow) is not None
        or WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_queued(node.status)
        or WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_running(node.status)
    ):
        raise BusinessValidationError("运行中，稍后删除")

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
    duplicate_ids = {node.id for node in product_nodes[1:]}
    if duplicate_ids:
        session.execute(
            delete(WorkflowEdge).where(
                (WorkflowEdge.workflow_id == workflow.id)
                & (WorkflowEdge.source_node_id.in_(duplicate_ids) | WorkflowEdge.target_node_id.in_(duplicate_ids))
            )
        )
        session.execute(delete(WorkflowNodeRun).where(WorkflowNodeRun.node_id.in_(duplicate_ids)))
        for duplicate in product_nodes[1:]:
            session.delete(duplicate)
        changed = True
    if changed:
        workflow.updated_at = now_utc()
    return changed
