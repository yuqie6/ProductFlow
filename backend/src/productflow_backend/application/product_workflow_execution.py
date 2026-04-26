from __future__ import annotations

import logging
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from sqlalchemy import update
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from productflow_backend.application import product_workflow_graph
from productflow_backend.application.contracts import PosterGenerationInput, ProductInput, ReferenceImageInput
from productflow_backend.application.product_workflow_artifacts import (
    _copy_node_output,
    _create_context_copy_set,
    _fill_reference_node,
    _GeneratedWorkflowImage,
    _image_asset_output,
)
from productflow_backend.application.product_workflow_context import (
    _collect_incoming_context,
    _downstream_reference_nodes,
    _effective_product_context,
    _find_source_asset,
    _image_instruction_with_context,
    _image_size_from_config,
    _instruction_with_upstream_text,
    _optional_config_text,
    _poster_kind_from_config,
    _product_context_values,
    _reference_assets_for_image_generation,
    _reference_image_inputs_for_copy,
    _source_asset_ids_from_config,
)
from productflow_backend.application.product_workflow_dependencies import (
    PosterRendererFactory,
    WorkflowExecutionDependencies,
    default_workflow_execution_dependencies,
)
from productflow_backend.application.product_workflow_mutations import get_or_create_product_workflow
from productflow_backend.application.product_workflow_query import WorkflowQueryService
from productflow_backend.application.time import now_utc
from productflow_backend.config import get_runtime_settings
from productflow_backend.domain.enums import (
    CopyStatus,
    PosterKind,
    SourceAssetKind,
    WorkflowNodeStatus,
    WorkflowNodeType,
    WorkflowRunStatus,
)
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.domain.workflow_rules import (
    WorkflowRuleEdge,
    WorkflowRuleNode,
    selected_node_execution_plan,
    should_execute_missing_upstream,
)
from productflow_backend.infrastructure.db.models import (
    CopySet,
    CreativeBrief,
    PosterVariant,
    Product,
    ProductWorkflow,
    SourceAsset,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRun,
)
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.image.base import ImageProvider, infer_extension
from productflow_backend.infrastructure.storage import LocalStorage

logger = logging.getLogger(__name__)


def get_text_provider():
    """Resolve through the facade so existing test monkeypatch targets keep working."""

    from productflow_backend.application import product_workflows

    return product_workflows.get_text_provider()


def get_image_provider():
    """Resolve through the facade so existing test monkeypatch targets keep working."""

    from productflow_backend.application import product_workflows

    return product_workflows.get_image_provider()


def _get_execute_node():
    """Resolve through the facade so existing test monkeypatch targets keep working."""

    from productflow_backend.application import product_workflows

    return product_workflows._execute_node


@dataclass(frozen=True, slots=True)
class WorkflowRunKickoff:
    workflow: ProductWorkflow
    run_id: str
    created: bool
    should_enqueue: bool


def _active_workflow_run(workflow: ProductWorkflow) -> WorkflowRun | None:
    return next(
        (
            run
            for run in sorted(workflow.runs, key=lambda item: item.started_at, reverse=True)
            if run.status == WorkflowRunStatus.RUNNING
        ),
        None,
    )


def _workflow_run_should_enqueue(run: WorkflowRun) -> bool:
    if run.status != WorkflowRunStatus.RUNNING:
        return False
    if any(node_run.status == WorkflowNodeStatus.RUNNING for node_run in run.node_runs):
        return False
    return any(node_run.status == WorkflowNodeStatus.QUEUED for node_run in run.node_runs) or (
        bool(run.node_runs) and all(node_run.status == WorkflowNodeStatus.SUCCEEDED for node_run in run.node_runs)
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
        return WorkflowRunKickoff(
            workflow=workflow,
            run_id=active_run.id,
            created=False,
            should_enqueue=_workflow_run_should_enqueue(active_run),
        )

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
            return WorkflowRunKickoff(
                workflow=workflow,
                run_id=active_run.id,
                created=False,
                should_enqueue=_workflow_run_should_enqueue(active_run),
            )
        raise
    session.expire_all()
    return WorkflowRunKickoff(
        workflow=product_workflow_graph.get_workflow_or_raise(session, workflow.id),
        run_id=run.id,
        created=True,
        should_enqueue=True,
    )


def run_product_workflow(
    session: Session,
    *,
    product_id: str,
    start_node_id: str | None = None,
    dependencies: WorkflowExecutionDependencies | None = None,
) -> ProductWorkflow:
    kickoff = start_product_workflow_run(session, product_id=product_id, start_node_id=start_node_id)
    if kickoff.created:
        execute_product_workflow_run(kickoff.run_id, dependencies=dependencies)
        session.expire_all()
        return product_workflow_graph.get_workflow_or_raise(session, kickoff.workflow.id)
    return kickoff.workflow


def execute_product_workflow_run(
    run_id: str,
    *,
    dependencies: WorkflowExecutionDependencies | None = None,
) -> None:
    session_factory = get_session_factory()
    session = session_factory()
    try:
        try:
            _execute_product_workflow_run(session, run_id=run_id, dependencies=dependencies)
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


def _execute_product_workflow_run(
    session: Session,
    *,
    run_id: str,
    dependencies: WorkflowExecutionDependencies | None = None,
) -> None:
    queries = WorkflowQueryService(session)
    run = session.get(WorkflowRun, run_id)
    if run is None:
        return
    if run.status != WorkflowRunStatus.RUNNING:
        return
    workflow = queries.get_workflow_or_raise(run.workflow_id)
    ordered_nodes = product_workflow_graph.topological_nodes(workflow)
    run_node_ids = {node_run.node_id for node_run in run.node_runs}
    node_runs_by_node_id = {node_run.node_id: node_run for node_run in run.node_runs}
    if any(node_run.status == WorkflowNodeStatus.RUNNING for node_run in run.node_runs):
        return

    for ordered_node in ordered_nodes:
        if ordered_node.id not in run_node_ids:
            continue
        node = queries.get_node_or_raise(ordered_node.id)
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
        node = queries.get_node_or_raise(ordered_node.id)
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
            if dependencies is None:
                output = _get_execute_node()(session, workflow_id=workflow.id, node=node)
            else:
                output = _execute_node(session, workflow_id=workflow.id, node=node, dependencies=dependencies)
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
        if isinstance(output.get("generated_poster_variant_ids"), list):
            poster_ids = output["generated_poster_variant_ids"]
        else:
            poster_ids = output.get("poster_variant_ids") if isinstance(output.get("poster_variant_ids"), list) else []
        node_run.poster_variant_id = poster_ids[0] if poster_ids else output.get("poster_variant_id")
        node_run.image_session_asset_id = output.get("image_session_asset_id")
        node_run.finished_at = now_utc()
        workflow.updated_at = now_utc()
        session.commit()
        logger.info("工作流节点执行成功: run_id=%s node_id=%s", run_id, node.id)

    persisted_run = queries.workflow_run_with_node_runs(run_id)
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
    rule_nodes = [
        WorkflowRuleNode(
            id=node.id,
            node_type=node.node_type,
            position_x=node.position_x,
            config_json=node.config_json,
        )
        for node in workflow.nodes
    ]
    rule_edges = [
        WorkflowRuleEdge(source_node_id=edge.source_node_id, target_node_id=edge.target_node_id)
        for edge in workflow.edges
    ]
    nodes_by_id = {node.id: node for node in workflow.nodes}
    if start_node_id not in nodes_by_id:
        raise BusinessValidationError("工作流节点不属于当前商品")
    reusable_edges: set[tuple[str, str]] = set()
    for edge in workflow.edges:
        source_node = nodes_by_id.get(edge.source_node_id)
        target_node = nodes_by_id.get(edge.target_node_id)
        if source_node is None or target_node is None:
            raise BusinessValidationError("工作流连线引用了不存在的节点")
        if _node_has_reusable_output(session, workflow, source_node, target_node=target_node):
            reusable_edges.add((edge.source_node_id, edge.target_node_id))
    return selected_node_execution_plan(
        nodes=rule_nodes,
        edges=rule_edges,
        start_node_id=start_node_id,
        reusable_edges=reusable_edges,
    )


def _node_has_reusable_output(
    session: Session,
    workflow: ProductWorkflow,
    node: WorkflowNode,
    *,
    target_node: WorkflowNode | None = None,
) -> bool:
    queries = WorkflowQueryService(session)
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
        return queries.copy_set_for_product(copy_set_id, workflow.product_id) is not None
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
            posters = queries.posters_by_ids(poster_ids)
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
    return WorkflowQueryService(session).has_any_source_asset_for_product(product_id, asset_ids)


def _should_execute_missing_upstream(source_node: WorkflowNode, target_node: WorkflowNode) -> bool:
    return should_execute_missing_upstream(
        WorkflowRuleNode(
            id=source_node.id,
            node_type=source_node.node_type,
            position_x=source_node.position_x,
            config_json=source_node.config_json,
        ),
        WorkflowRuleNode(
            id=target_node.id,
            node_type=target_node.node_type,
            position_x=target_node.position_x,
            config_json=target_node.config_json,
        ),
    )


def _execute_node(
    session: Session,
    *,
    workflow_id: str,
    node: WorkflowNode,
    dependencies: WorkflowExecutionDependencies | None = None,
) -> dict[str, Any]:
    workflow = product_workflow_graph.get_workflow_or_raise(session, workflow_id)
    product = workflow.product
    dependencies = dependencies or default_workflow_execution_dependencies()
    if node.node_type == WorkflowNodeType.PRODUCT_CONTEXT:
        return _execute_product_context(product, node)
    if node.node_type == WorkflowNodeType.REFERENCE_IMAGE:
        return _execute_reference_image(session, workflow=workflow, node=node)
    if node.node_type == WorkflowNodeType.COPY_GENERATION:
        return _execute_copy_generation(session, workflow=workflow, node=node, dependencies=dependencies)
    if node.node_type == WorkflowNodeType.IMAGE_GENERATION:
        return _execute_image_generation(session, workflow=workflow, node=node, dependencies=dependencies)
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
    assets = WorkflowQueryService(session).source_assets_by_ids(asset_ids)
    assets = [asset for asset in assets if asset.product_id == workflow.product_id]
    if not assets:
        return _image_asset_output([], summary="参考图为空")
    return _image_asset_output(
        assets,
        summary=f"参考图 {len(assets)} 张",
        role=_optional_config_text(node.config_json, "role"),
        label=_optional_config_text(node.config_json, "label") or node.title,
    )


def _execute_copy_generation(
    session: Session,
    *,
    workflow: ProductWorkflow,
    node: WorkflowNode,
    dependencies: WorkflowExecutionDependencies | None = None,
) -> dict[str, Any]:
    dependencies = dependencies or default_workflow_execution_dependencies()
    product = workflow.product
    product_context = _effective_product_context(workflow, node.id)
    has_product_context = any(value is not None for value in product_context.values())
    existing_output = node.output_json or {}
    existing_copy_set_id = existing_output.get("copy_set_id")
    if existing_output.get("manual_edit") is True and isinstance(existing_copy_set_id, str):
        copy_set = session.get(CopySet, existing_copy_set_id)
        if copy_set is not None and copy_set.product_id == product.id:
            return _copy_node_output(copy_set, creative_brief_id=copy_set.creative_brief_id, manual_edit=True)

    storage = LocalStorage()
    source = _find_source_asset(product) if has_product_context else None
    product_input = ProductInput(
        name=product_context["name"] or "自由创作",
        category=product_context["category"],
        price=product_context["price"],
        source_note=product_context["source_note"],
        image_path=str(storage.resolve(source.storage_path)) if source is not None else "",
    )
    incoming_context = _collect_incoming_context(workflow, node.id)
    reference_images = _reference_image_inputs_for_copy(session, workflow=workflow, node_id=node.id, storage=storage)
    instruction = _instruction_with_upstream_text(
        _optional_config_text(node.config_json, "instruction"),
        incoming_context,
    )
    provider = dependencies.text_provider()
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
    output["context_summary"] = {
        "product_context": product_context,
        "reference_image_count": len(reference_images),
        "upstream_text_count": len(incoming_context.text_contexts),
    }
    output["context_sources"] = incoming_context.text_sources[:8]
    return output


def _execute_image_generation(
    session: Session,
    *,
    workflow: ProductWorkflow,
    node: WorkflowNode,
    dependencies: WorkflowExecutionDependencies | None = None,
) -> dict[str, Any]:
    dependencies = dependencies or default_workflow_execution_dependencies()
    product = workflow.product
    incoming_context = _collect_incoming_context(workflow, node.id)
    product_context = _effective_product_context(workflow, node.id)
    downstream_reference_nodes = _downstream_reference_nodes(workflow, node.id)
    if not downstream_reference_nodes:
        raise ValueError("请先把生图节点连接到至少一个图片/参考图节点，再运行图片生成")

    linked_copy_set_id = _optional_config_text(node.config_json, "copy_set_id") or incoming_context.copy_set_id
    copy_set = session.get(CopySet, linked_copy_set_id) if linked_copy_set_id else None
    has_linked_copy_input = (
        linked_copy_set_id is not None and copy_set is not None and copy_set.product_id == product.id
    )
    if copy_set is None or copy_set.product_id != product.id:
        copy_set = _create_context_copy_set(session, product=product, product_context=product_context, node=node)

    storage = LocalStorage()
    reference_assets = _reference_assets_for_image_generation(
        session,
        workflow,
        incoming_context.image_asset_ids,
        incoming_context.poster_variant_ids,
    )
    render_input = PosterGenerationInput(
        copy_prompt_mode="copy" if has_linked_copy_input else "image_edit",
        product_name=product_context["name"] or "",
        category=product_context["category"],
        price=product_context["price"],
        source_note=product_context["source_note"],
        instruction=_image_instruction_with_context(node, incoming_context.text_contexts),
        image_size=_image_size_from_config(node.config_json),
        title=copy_set.title,
        selling_points=copy_set.selling_points,
        poster_headline=copy_set.poster_headline,
        cta=copy_set.cta,
        source_image=(Path(storage.resolve(reference_assets[0].storage_path)) if reference_assets else None),
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
    kind = _poster_kind_from_config(node.config_json)
    image_providers = (
        [dependencies.image_provider() for _ in downstream_reference_nodes]
        if settings.poster_generation_mode == "generated"
        else None
    )
    generated_images = _generate_workflow_images_concurrently(
        render_input=render_input,
        kind=kind,
        target_count=len(downstream_reference_nodes),
        poster_generation_mode=settings.poster_generation_mode,
        poster_font_path=settings.poster_font_path,
        image_providers=image_providers,
        renderer_factory=dependencies.poster_renderer,
    )
    for generated_image, target_node in zip(generated_images, downstream_reference_nodes, strict=True):
        content = generated_image.content
        mime_type = generated_image.mime_type
        relative_path = storage.save_generated_image(
            product.id,
            f"workflow-{kind.value}-{generated_image.target_index}",
            content,
            suffix=infer_extension(mime_type),
        )
        poster = PosterVariant(
            product_id=product.id,
            copy_set_id=copy_set.id,
            kind=kind,
            template_name=generated_image.template_name,
            storage_path=relative_path,
            mime_type=mime_type,
            width=generated_image.width,
            height=generated_image.height,
        )
        session.add(poster)
        session.flush()
        poster_ids.append(poster.id)

        filename = f"reference-{generated_image.target_index}{infer_extension(mime_type)}"
        reference_path = storage.save_reference_upload(product.id, filename, content)
        asset = SourceAsset(
            product_id=product.id,
            kind=SourceAssetKind.REFERENCE_IMAGE,
            original_filename=filename,
            mime_type=mime_type,
            storage_path=reference_path,
            source_poster_variant_id=poster.id,
        )
        session.add(asset)
        session.flush()
        filled_source_asset_ids.append(asset.id)
        filled_reference_node_ids.append(target_node.id)
        _fill_reference_node(target_node, asset, source_poster_variant_id=poster.id)
    product.updated_at = now_utc()
    return {
        "copy_set_id": copy_set.id,
        "generated_poster_variant_ids": poster_ids,
        "filled_source_asset_ids": filled_source_asset_ids,
        "filled_reference_node_ids": filled_reference_node_ids,
        "target_count": len(downstream_reference_nodes),
        "size": _image_size_from_config(node.config_json),
        "instruction": _optional_config_text(node.config_json, "instruction"),
        "context_summary": {
            "product_context": product_context,
            "copy_set_id": copy_set.id,
            "copy_prompt_mode": render_input.copy_prompt_mode,
            "upstream_text_count": len(incoming_context.text_contexts),
            "reference_image_count": len(incoming_context.image_asset_ids),
            "poster_variant_count": len(incoming_context.poster_variant_ids),
        },
        "context_sources": incoming_context.text_sources[:8],
        "summary": f"已填充 {len(filled_reference_node_ids)} 个参考图",
    }


def _generate_workflow_images_concurrently(
    *,
    render_input: PosterGenerationInput,
    kind: PosterKind,
    target_count: int,
    poster_generation_mode: str,
    poster_font_path: Path,
    image_providers: list[ImageProvider] | None,
    renderer_factory: PosterRendererFactory | None = None,
) -> list[_GeneratedWorkflowImage]:
    if target_count <= 0:
        return []
    dependencies = default_workflow_execution_dependencies()
    renderer_factory = renderer_factory or dependencies.poster_renderer

    def generate_one(target_index: int) -> _GeneratedWorkflowImage:
        if poster_generation_mode == "generated":
            if image_providers is None:
                raise RuntimeError("图片生成供应商未初始化")
            image_provider = image_providers[target_index - 1]
            generated_image, image_model = image_provider.generate_poster_image(render_input, kind)
            return _GeneratedWorkflowImage(
                target_index=target_index,
                content=generated_image.bytes_data,
                width=generated_image.width,
                height=generated_image.height,
                template_name=f"workflow:{image_provider.provider_name}:{generated_image.variant_label}:{image_model}",
                mime_type=generated_image.mime_type,
            )

        renderer = renderer_factory(poster_font_path)
        return _GeneratedWorkflowImage(
            target_index=target_index,
            content=renderer.render(render_input, kind),
            width=1080,
            height=1080 if kind == PosterKind.MAIN_IMAGE else 1440,
            template_name=f"workflow:{'default-main' if kind == PosterKind.MAIN_IMAGE else 'default-promo'}",
            mime_type="image/png",
        )

    if target_count == 1:
        return [generate_one(1)]
    with ThreadPoolExecutor(max_workers=target_count) as executor:
        return list(executor.map(generate_one, range(1, target_count + 1)))
