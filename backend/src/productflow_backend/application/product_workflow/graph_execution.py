from __future__ import annotations

import hashlib
import json
import logging
import re
from collections.abc import Callable
from dataclasses import replace
from typing import Any

from pydantic import ValidationError
from sqlalchemy import select, update
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, selectinload
from sqlalchemy.orm.attributes import flag_modified

from productflow_backend.application.agent.product_intake import (
    LISTING_LOOK_RULE,
    agent_product_image_type_option,
    image_type_family,
    image_type_generation_job,
    image_type_prompt_goal,
)
from productflow_backend.application.async_delivery import delivery_key_for_actor, stage_async_dispatch
from productflow_backend.application.delivery_renditions.service import create_delivery_rendition_job
from productflow_backend.application.product_images.assets import stage_product_image_asset
from productflow_backend.application.product_workflow.dependencies import (
    WorkflowExecutionDependencies,
    default_workflow_execution_dependencies,
)
from productflow_backend.application.product_workflow.graph_commands import load_applied_graph
from productflow_backend.application.product_workflow.graph_compiler import (
    ContextRuntimeInput,
    GraphRuntimeArtifacts,
    GraphSourceRecord,
    ImageRuntimeInput,
    PromptRuntimeInput,
    applied_graph_from_snapshot,
    artifacts_from_sources,
    compile_context_runtime,
    compile_image_runtime,
    compile_prompt_runtime,
    sources_from_snapshot,
    strip_v3_prompt_payload,
)
from productflow_backend.application.product_workflow.graph_runs import load_graph_sources
from productflow_backend.application.storage_compensation import StorageWriteCompensation
from productflow_backend.application.time import now_utc
from productflow_backend.application.workflow_drafts.contracts import (
    GenerationSpec,
    ImagePromptPayloadV1,
    PromptTextContent,
    VisualExceptionPlan,
    VisualSystemDraftPayload,
)
from productflow_backend.domain.durable_generation_tasks import DELIVERY_RENDITION_TASK_CONTRACT
from productflow_backend.domain.enums import (
    GraphArtifactType,
    GraphNodeType,
    JobStatus,
    ProductImageOriginType,
    WorkflowNodeStatus,
    WorkflowRunStatus,
)
from productflow_backend.domain.errors import BusinessValidationError, NotFoundError
from productflow_backend.domain.graph_catalog import catalog_visual_overlay
from productflow_backend.infrastructure.db.models import (
    Product,
    ProductImageAsset,
    WorkflowGraph,
    WorkflowGraphArtifact,
    WorkflowGraphNode,
    WorkflowGraphNodeRun,
    WorkflowGraphRun,
)
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.image.base import (
    WorkflowImageReference,
    WorkflowImageRequest,
    aspect_mismatch_message,
    image_dimensions_from_bytes,
    measured_aspect_matches_spec,
)
from productflow_backend.infrastructure.prompt.base import (
    ContextGenerationRequest,
    PromptGenerationRequest,
    PromptReferenceImage,
)
from productflow_backend.infrastructure.storage import LocalStorage

logger = logging.getLogger(__name__)
SUPPORTED_REFERENCE_MIME_TYPES = {"image/png", "image/jpeg", "image/webp"}
# ImagePromptPayloadV1 still requires images[].image_plan_key; stored v3 artifacts strip it.
V3_PROMPT_PROVIDER_PLAN_KEY = "output"


def execute_graph_run(
    run_id: str,
    *,
    dependencies: WorkflowExecutionDependencies | None = None,
    storage: LocalStorage | None = None,
) -> None:
    session = get_session_factory()()
    try:
        _execute_graph_run(session, run_id=run_id, dependencies=dependencies, storage=storage)
    except Exception:
        session.rollback()
        logger.exception("schema-v3 graph run failed: run_id=%s", run_id)
        _fail_run(session, run_id=run_id, reason="工作流运行失败")
    finally:
        session.close()


def _execute_graph_run(
    session: Session,
    *,
    run_id: str,
    dependencies: WorkflowExecutionDependencies | None,
    storage: LocalStorage | None,
) -> None:
    run = session.get(WorkflowGraphRun, run_id)
    if run is None or run.status != WorkflowRunStatus.RUNNING:
        return
    graph_row = session.get(WorkflowGraph, run.graph_id)
    if graph_row is None:
        raise NotFoundError("商品工作流不存在")
    product_id = graph_row.product_id
    resolved_dependencies = dependencies or default_workflow_execution_dependencies()
    resolved_storage = storage or LocalStorage()
    snapshot = run.snapshot_json
    graph = applied_graph_from_snapshot(snapshot)
    sources = sources_from_snapshot(snapshot)
    artifacts = artifacts_from_sources(sources)
    node_runs = session.scalars(
        select(WorkflowGraphNodeRun)
        .where(WorkflowGraphNodeRun.graph_run_id == run.id)
        .order_by(WorkflowGraphNodeRun.sort_order, WorkflowGraphNodeRun.id)
    ).all()
    for node_run in node_runs:
        session.refresh(run)
        if run.status in {WorkflowRunStatus.CANCELLED, WorkflowRunStatus.FAILED}:
            return
        session.refresh(node_run)
        if node_run.status != WorkflowNodeStatus.QUEUED:
            continue
        if not _claim_queued_node_run(session, node_run):
            continue
        try:
            artifacts = _execute_node_run(
                session,
                run=run,
                node_run=node_run,
                graph=graph,
                sources=sources,
                artifacts=artifacts,
                dependencies=resolved_dependencies,
                storage=resolved_storage,
                product_id=product_id,
            )
        except BusinessValidationError as exc:
            _fail_claimed_node(session, run_id=run.id, node_run_id=node_run.id, reason=str(exc))
            return
        except IntegrityError:
            logger.exception("schema-v3 graph node persist conflict: run_id=%s node_run_id=%s", run.id, node_run.id)
            _fail_claimed_node(session, run_id=run.id, node_run_id=node_run.id, reason="节点运行失败")
            return
        except Exception:
            logger.exception("schema-v3 graph node run failed: run_id=%s node_run_id=%s", run.id, node_run.id)
            _fail_claimed_node(session, run_id=run.id, node_run_id=node_run.id, reason="节点运行失败")
            return
    session.refresh(run)
    if run.status == WorkflowRunStatus.RUNNING:
        run.status = WorkflowRunStatus.SUCCEEDED
        run.finished_at = now_utc()
        session.commit()


def _execute_node_run(
    session: Session,
    *,
    run: WorkflowGraphRun,
    node_run: WorkflowGraphNodeRun,
    graph,
    sources,
    artifacts: GraphRuntimeArtifacts,
    dependencies: WorkflowExecutionDependencies,
    storage: LocalStorage,
    product_id: str,
) -> GraphRuntimeArtifacts:
    if node_run.node_id is None:
        raise BusinessValidationError("运行节点已从当前图中删除")
    applied_node = graph.node(node_run.node_id)
    if applied_node.node_type == GraphNodeType.CREATIVE_BRIEF:
        runtime = compile_context_runtime(graph, node_run.node_id, sources, artifacts)
        skipped = _complete_skipped_node_run(
            session,
            node_run=node_run,
            sources=sources,
            input_digest=runtime.input_digest,
            trace=_context_trace(runtime),
        )
        if skipped:
            return artifacts
        node_run.compiled_context_json = _context_trace(runtime)
        prompt_provider = dependencies.prompt_generation_provider()
        result = prompt_provider.generate_creative_brief(
            _to_context_request(
                runtime,
                title=applied_node.title,
                storage=storage,
                session=session,
                product_id=product_id,
            )
        )
        payload = result.payload.model_dump(mode="json")
        artifact = _persist_artifact(
            session,
            run=run,
            node_run=node_run,
            artifact_type=GraphArtifactType.CREATIVE_BRIEF,
            payload=payload,
            input_digest=runtime.input_digest,
            provider_name=prompt_provider.provider_name,
            provider_model=result.model,
        )
        _write_generated_config(
            session,
            graph_id=run.graph_id,
            graph_revision=run.graph_revision,
            node_id=node_run.node_id,
            updater=lambda config: {**config, **payload},
        )
        _refresh_artifact_digest_after_writeback(
            session,
            graph_id=run.graph_id,
            graph_revision=run.graph_revision,
            node_id=node_run.node_id,
            node_type=applied_node.node_type,
            artifact=artifact,
        )
        sources[node_run.node_id] = replace(
            sources.get(node_run.node_id, GraphSourceRecord()),
            brief=payload,
            current_artifact_id=artifact.id,
            current_artifact_type=GraphArtifactType.CREATIVE_BRIEF,
            current_artifact_payload=payload,
            current_input_digest=artifact.input_digest,
        )
        artifacts = artifacts.with_prompt(node_run.node_id, artifact_id=artifact.id, payload=payload)
    elif applied_node.node_type == GraphNodeType.VISUAL_SYSTEM:
        runtime = compile_context_runtime(graph, node_run.node_id, sources, artifacts)
        skipped = _complete_skipped_node_run(
            session,
            node_run=node_run,
            sources=sources,
            input_digest=runtime.input_digest,
            trace=_context_trace(runtime),
        )
        if skipped:
            return artifacts
        node_run.compiled_context_json = _context_trace(runtime)
        prompt_provider = dependencies.prompt_generation_provider()
        result = prompt_provider.generate_visual_overlay(
            _to_context_request(
                runtime,
                title=applied_node.title,
                storage=storage,
                session=session,
                product_id=product_id,
            )
        )
        dumped = result.payload.model_dump(mode="json")
        overlay = catalog_visual_overlay(dumped) or dumped
        artifact = _persist_artifact(
            session,
            run=run,
            node_run=node_run,
            artifact_type=GraphArtifactType.VISUAL_SYSTEM,
            payload=overlay,
            input_digest=runtime.input_digest,
            provider_name=prompt_provider.provider_name,
            provider_model=result.model,
        )
        _write_generated_config(
            session,
            graph_id=run.graph_id,
            graph_revision=run.graph_revision,
            node_id=node_run.node_id,
            updater=lambda config: {**config, "visual_overlay": overlay},
        )
        _refresh_artifact_digest_after_writeback(
            session,
            graph_id=run.graph_id,
            graph_revision=run.graph_revision,
            node_id=node_run.node_id,
            node_type=applied_node.node_type,
            artifact=artifact,
        )
        sources[node_run.node_id] = replace(
            sources.get(node_run.node_id, GraphSourceRecord()),
            visual_payload=overlay,
            current_artifact_id=artifact.id,
            current_artifact_type=GraphArtifactType.VISUAL_SYSTEM,
            current_artifact_payload=overlay,
            current_input_digest=artifact.input_digest,
        )
        artifacts = artifacts.with_prompt(node_run.node_id, artifact_id=artifact.id, payload=overlay)
    elif applied_node.node_type == GraphNodeType.PROMPT_GENERATION:
        runtime = compile_prompt_runtime(graph, node_run.node_id, sources, artifacts)
        skipped = _complete_skipped_node_run(
            session,
            node_run=node_run,
            sources=sources,
            input_digest=runtime.input_digest,
            trace=_prompt_context_trace(runtime),
        )
        if skipped:
            return artifacts
        node_run.compiled_context_json = _prompt_context_trace(runtime)
        prompt_provider = dependencies.prompt_generation_provider()
        result = prompt_provider.generate_prompt(
            _to_prompt_request(
                runtime,
                title=applied_node.title,
                storage=storage,
                session=session,
                product_id=product_id,
            )
        )
        payload_model = _apply_text_policy_to_prompt_payload(
            result.payload,
            text_policy=runtime.text_policy,
            keep_authored=_prompt_config_has_authored_text(runtime.prompt_config),
        )
        payload = strip_v3_prompt_payload(payload_model.model_dump(mode="json"))
        artifact = _persist_artifact(
            session,
            run=run,
            node_run=node_run,
            artifact_type=GraphArtifactType.PROMPT,
            payload=payload,
            input_digest=runtime.input_digest,
            provider_name=prompt_provider.provider_name,
            provider_model=result.model,
        )
        _write_generated_config(
            session,
            graph_id=run.graph_id,
            graph_revision=run.graph_revision,
            node_id=node_run.node_id,
            updater=lambda config: {**config, "prompt": payload},
        )
        _refresh_artifact_digest_after_writeback(
            session,
            graph_id=run.graph_id,
            graph_revision=run.graph_revision,
            node_id=node_run.node_id,
            node_type=applied_node.node_type,
            artifact=artifact,
        )
        artifacts = artifacts.with_prompt(node_run.node_id, artifact_id=artifact.id, payload=payload)
    elif applied_node.node_type == GraphNodeType.IMAGE_GENERATION:
        runtime = compile_image_runtime(graph, node_run.node_id, sources, artifacts)
        skipped = _complete_skipped_node_run(
            session,
            node_run=node_run,
            sources=sources,
            input_digest=runtime.input_digest,
            trace=_image_context_trace(runtime),
        )
        if skipped:
            return artifacts
        node_run.compiled_context_json = _image_context_trace(runtime)
        image_provider = dependencies.image_provider()
        spec = GenerationSpec.model_validate(runtime.generation_spec)
        image_result = image_provider.generate_workflow_image(
            _to_image_request(runtime, session=session, storage=storage, product_id=product_id)
        )
        generated = image_result.images[0]
        dimensions = image_dimensions_from_bytes(generated.bytes_data)
        measured_width, measured_height = dimensions or (0, 0)
        if dimensions is None or not measured_aspect_matches_spec(spec, measured_width, measured_height):
            raise BusinessValidationError(aspect_mismatch_message(spec, measured_width, measured_height))
        product = session.get(Product, product_id)
        if product is None:
            raise NotFoundError("商品不存在")
        storage_writes = StorageWriteCompensation()
        image_type_key = applied_node.config.get("image_type_key")
        asset = stage_product_image_asset(
            session,
            product=product,
            content=generated.bytes_data,
            filename=f"{applied_node.title}.png",
            expected_mime_type=generated.mime_type,
            display_name=applied_node.title,
            origin_type=ProductImageOriginType.WORKFLOW_GENERATION,
            storage=storage,
            storage_writes=storage_writes,
            image_type_key=image_type_key if isinstance(image_type_key, str) else None,
        )
        session.flush()
        payload = {
            "schema_version": 3,
            "product_image_asset_id": asset.id,
            "generation_spec": runtime.generation_spec,
            "prompt_artifact_id": runtime.prompt_artifact_id,
            "measured_output": {
                "mime_type": generated.mime_type,
                "provider_status": image_result.provider_status,
                "effective_parameters": image_result.effective_parameters,
                "requested_aspect_ratio": spec.aspect_ratio,
                "measured_width": measured_width,
                "measured_height": measured_height,
                "requested_quality": spec.quality_intent,
            },
        }
        artifact = _persist_artifact(
            session,
            run=run,
            node_run=node_run,
            artifact_type=GraphArtifactType.IMAGE,
            payload=payload,
            input_digest=runtime.input_digest,
            provider_name=image_provider.provider_name,
            provider_model=image_result.model,
            product_image_asset_id=asset.id,
        )
        artifacts = artifacts.with_image(node_run.node_id, artifact_id=artifact.id, asset_id=asset.id)
        _queue_delivery_rendition_after_image_success(
            session,
            node_id=node_run.node_id,
            source_asset_id=asset.id,
        )
        storage_writes.release()
    else:
        raise BusinessValidationError("不能运行该节点类型")
    node_run.status = WorkflowNodeStatus.SUCCEEDED
    node_run.finished_at = now_utc()
    node_run.output_json = {"artifact_id": artifact.id}
    session.commit()
    return artifacts


def _queue_delivery_rendition_after_image_success(
    session: Session,
    *,
    node_id: str,
    source_asset_id: str,
) -> None:
    live_node = session.get(WorkflowGraphNode, node_id)
    if live_node is None:
        return
    session.refresh(live_node)
    spec = live_node.config_json.get("delivery_spec") if isinstance(live_node.config_json, dict) else None
    if not spec:
        return
    try:
        creation = create_delivery_rendition_job(
            session,
            source_asset_id=source_asset_id,
            delivery_spec=spec,
        )
        if creation.created and creation.job.status == JobStatus.QUEUED:
            stage_async_dispatch(
                session,
                delivery_key=delivery_key_for_actor(
                    DELIVERY_RENDITION_TASK_CONTRACT.actor_name,
                    creation.job.id,
                ),
                actor_name=DELIVERY_RENDITION_TASK_CONTRACT.actor_name,
                aggregate_id=creation.job.id,
            )
    except Exception:
        logger.exception(
            "image success kept; delivery rendition queue failed: asset_id=%s node_id=%s",
            source_asset_id,
            node_id,
        )


def _persist_artifact(
    session: Session,
    *,
    run: WorkflowGraphRun,
    node_run: WorkflowGraphNodeRun,
    artifact_type: GraphArtifactType,
    payload: dict[str, Any],
    input_digest: str,
    provider_name: str,
    provider_model: str | None,
    product_image_asset_id: str | None = None,
) -> WorkflowGraphArtifact:
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True)
    payload_hash = hashlib.sha256(encoded.encode("utf-8")).hexdigest()
    artifact = session.scalar(
        select(WorkflowGraphArtifact).where(WorkflowGraphArtifact.node_run_id == node_run.id)
    )
    if artifact is None:
        artifact = WorkflowGraphArtifact(
            graph_id=run.graph_id,
            node_id=node_run.node_id,
            node_run_id=node_run.id,
            artifact_type=artifact_type,
            schema_version=3,
            graph_revision=run.graph_revision,
            payload_json=payload,
            payload_hash=payload_hash,
            input_digest=input_digest,
            product_image_asset_id=product_image_asset_id,
            provider_name=provider_name,
            provider_model=provider_model,
        )
        session.add(artifact)
    else:
        artifact.artifact_type = artifact_type
        artifact.schema_version = 3
        artifact.graph_revision = run.graph_revision
        artifact.payload_json = payload
        artifact.payload_hash = payload_hash
        artifact.input_digest = input_digest
        artifact.product_image_asset_id = product_image_asset_id
        artifact.provider_name = provider_name
        artifact.provider_model = provider_model
        flag_modified(artifact, "payload_json")
    session.flush()
    live_graph = session.get(WorkflowGraph, run.graph_id)
    if live_graph is not None and live_graph.revision == run.graph_revision:
        node = session.get(WorkflowGraphNode, node_run.node_id)
        if node is not None:
            node.current_artifact_id = artifact.id
            session.flush()
    return artifact


PROMPT_CONTEXT_DERIVED_PLACEHOLDER = "根据参考图、商品资料与图片类型生成"
NO_ON_IMAGE_TEXT_RULE = "画面中不得出现文字、数字、价格、Logo 或水印"
IDENTITY_SHARED_RULES = (
    "商品外形、结构、颜色和材质以参考图为准",
    "不要编造参考图和资料里没有的认证、Logo、价格或结构",
    "参考图只提供商品本体，必须按图种重新构图，禁止原图贴字交差",
    "不要极简大留白或浅灰空棚，也不要爆炸贴或满屏色块",
)
NO_CAPTION_ON_REFERENCE_RULE = "禁止把参考图原样放大缩小后只加一行字交差"
_AUTHORED_PROMPT_KEYS = (
    "composition",
    "content",
    "atmosphere",
    "text",
    "product_fidelity",
    "creative_boundary",
)


def _to_prompt_request(
    runtime: PromptRuntimeInput,
    *,
    title: str,
    session: Session,
    storage: LocalStorage,
    product_id: str,
) -> PromptGenerationRequest:
    visual, visual_exceptions = _prompt_visual_inputs(runtime)
    generate_from_context = _prompt_config_is_generation_seed(runtime.prompt_config)
    type_option = agent_product_image_type_option(runtime.image_type_key or "")
    text_languages = (runtime.text_language,) if runtime.text_language else ()
    image_type_key = runtime.image_type_key or "unspecified"
    return PromptGenerationRequest(
        image_type_key=image_type_key,
        image_plan_keys=(V3_PROMPT_PROVIDER_PLAN_KEY,),
        facts=runtime.product_facts,
        visual_system=visual,
        visual_exceptions=visual_exceptions,
        current_prompt=_prompt_from_runtime(runtime, title=title, generate_from_context=generate_from_context),
        text_languages=text_languages,
        reference_images=tuple(
            _load_prompt_reference(session, product_id=product_id, reference=reference, storage=storage)
            for reference in runtime.reference_images
        ),
        generate_from_context=generate_from_context,
        image_type_title=type_option.title if type_option else None,
        image_type_description=type_option.description if type_option else None,
        text_policy=runtime.text_policy,
        image_type_family=image_type_family(image_type_key),
        image_type_job=image_type_generation_job(image_type_key) or None,
    )


def _to_context_request(
    runtime: ContextRuntimeInput,
    *,
    title: str,
    session: Session,
    storage: LocalStorage,
    product_id: str,
) -> ContextGenerationRequest:
    current = runtime.current_config
    brief = None
    overlay = None
    if runtime.node_type == GraphNodeType.CREATIVE_BRIEF:
        brief = {
            key: current.get(key)
            for key in ("goal", "design_goals", "required_copy", "prohibitions")
            if current.get(key) not in (None, "", [])
        } or None
    else:
        raw = current.get("visual_overlay")
        overlay = catalog_visual_overlay(raw if isinstance(raw, dict) else None)
    return ContextGenerationRequest(
        facts=runtime.product_facts,
        reference_images=tuple(
            _load_prompt_reference(session, product_id=product_id, reference=reference, storage=storage)
            for reference in runtime.reference_images
        ),
        current_brief=brief,
        current_overlay=overlay,
        text_policy=runtime.text_policy,
        text_language=runtime.text_language,
        node_title=title,
        image_types=runtime.image_types,
    )


def _write_generated_config(
    session: Session,
    *,
    graph_id: str,
    graph_revision: int,
    node_id: str,
    updater: Callable[[dict[str, Any]], dict[str, Any]],
) -> None:
    live_graph = session.get(WorkflowGraph, graph_id)
    if live_graph is None or live_graph.revision != graph_revision:
        return
    node = session.get(WorkflowGraphNode, node_id)
    if node is None:
        return
    node.config_json = updater(dict(node.config_json or {}))
    flag_modified(node, "config_json")
    session.flush()


def _refresh_artifact_digest_after_writeback(
    session: Session,
    *,
    graph_id: str,
    graph_revision: int,
    node_id: str,
    node_type: GraphNodeType,
    artifact: WorkflowGraphArtifact,
) -> None:
    live_graph = session.get(WorkflowGraph, graph_id)
    if live_graph is None or live_graph.revision != graph_revision:
        return
    applied = load_applied_graph(session, live_graph)
    sources = load_graph_sources(session, live_graph, applied)
    runtime_artifacts = artifacts_from_sources(sources)
    if node_type in {GraphNodeType.CREATIVE_BRIEF, GraphNodeType.VISUAL_SYSTEM}:
        runtime = compile_context_runtime(applied, node_id, sources, runtime_artifacts)
    elif node_type == GraphNodeType.PROMPT_GENERATION:
        runtime = compile_prompt_runtime(applied, node_id, sources, runtime_artifacts)
    else:
        return
    artifact.input_digest = runtime.input_digest
    session.flush()


def _prompt_visual_inputs(
    runtime: PromptRuntimeInput,
) -> tuple[VisualSystemDraftPayload | None, tuple[dict[str, Any], ...]]:
    if runtime.visual_system is not None:
        try:
            return VisualSystemDraftPayload.model_validate(runtime.visual_system), ()
        except ValidationError as exc:
            if runtime.visual_system_version_id is not None:
                raise BusinessValidationError("视觉规范输入无效") from exc
    overlay = runtime.visual_overlay or runtime.visual_system
    if not isinstance(overlay, dict) or not overlay:
        if runtime.visual_system is not None:
            raise BusinessValidationError("视觉规范输入无效")
        return None, ()
    try:
        exceptions = _visual_exceptions_from_overlay(overlay)
    except ValidationError as overlay_error:
        raise BusinessValidationError("视觉覆盖输入无效") from overlay_error
    if not exceptions:
        raise BusinessValidationError("视觉覆盖输入无效")
    return None, exceptions


_OVERLAY_COLOR_ROLE_RE = re.compile(r"^[a-z0-9][a-z0-9_-]*$")


def _overlay_color_role(raw: str, index: int, used: set[str]) -> str:
    slug = re.sub(r"[^a-z0-9_-]+", "-", raw.strip().lower()).strip("-")
    candidate = slug[:80] if slug and _OVERLAY_COLOR_ROLE_RE.fullmatch(slug) else f"color-{index + 1}"
    if candidate in used:
        suffix = 2
        candidate = f"color-{index + 1}"
        while candidate in used:
            candidate = f"color-{index + 1}-{suffix}"
            suffix += 1
    used.add(candidate)
    return candidate


def _visual_exceptions_from_overlay(overlay: dict[str, Any]) -> tuple[dict[str, Any], ...]:
    overrides: list[dict[str, Any]] = []
    style = overlay.get("style")
    if isinstance(style, list):
        values = [item.strip() for item in style if isinstance(item, str) and item.strip()]
        if values:
            overrides.append({"field": "style", "value": values})
    colors = overlay.get("colors")
    if isinstance(colors, list):
        coerced: list[dict[str, str]] = []
        used_roles: set[str] = set()
        for index, item in enumerate(colors):
            if not isinstance(item, dict):
                continue
            value = item.get("value")
            if not isinstance(value, str) or not value.strip():
                continue
            raw_role = item.get("role")
            role_text = raw_role.strip() if isinstance(raw_role, str) else ""
            role = _overlay_color_role(role_text, index, used_roles)
            raw_label = item.get("label")
            label = raw_label.strip() if isinstance(raw_label, str) and raw_label.strip() else (role_text or role)
            coerced.append({"role": role, "value": value.strip(), "label": label})
        if coerced:
            overrides.append({"field": "colors", "value": coerced})
    prohibitions = overlay.get("prohibitions")
    if isinstance(prohibitions, list):
        values = [item.strip() for item in prohibitions if isinstance(item, str) and item.strip()]
        if values:
            overrides.append({"field": "prohibitions", "value": values})
    if not overrides:
        return ()
    exception = VisualExceptionPlan.model_validate(
        {
            "key": "graph-inline-overlay",
            "scope": {"type": "workflow"},
            "overrides": overrides,
            "reason": "工作流内联视觉覆盖",
        }
    )
    return (exception.model_dump(mode="json"),)


def _to_image_request(
    runtime: ImageRuntimeInput,
    *,
    session: Session,
    storage: LocalStorage,
    product_id: str,
) -> WorkflowImageRequest:
    references = tuple(
        _load_image_reference(session, product_id=product_id, reference=item, storage=storage)
        for item in runtime.reference_images
    )
    return WorkflowImageRequest(
        compiled_prompt=_compile_image_model_prompt(runtime),
        generation_spec=GenerationSpec.model_validate(runtime.generation_spec),
        references=references,
        image_type_key=runtime.image_type_key,
    )


def _load_prompt_reference(
    session: Session,
    *,
    product_id: str,
    reference,
    storage: LocalStorage,
) -> PromptReferenceImage:
    asset, image_bytes = _read_asset(session, product_id=product_id, asset_id=reference.asset_id, storage=storage)
    return PromptReferenceImage(
        asset_id=asset.id,
        role=reference.role,
        label=reference.label,
        filename=asset.original_filename,
        mime_type=asset.media_object.mime_type,
        image_bytes=image_bytes,
    )


def _load_image_reference(
    session: Session,
    *,
    product_id: str,
    reference,
    storage: LocalStorage,
) -> WorkflowImageReference:
    asset, image_bytes = _read_asset(session, product_id=product_id, asset_id=reference.asset_id, storage=storage)
    return WorkflowImageReference(
        asset_id=asset.id,
        role=reference.role,
        label=reference.label,
        filename=asset.original_filename,
        mime_type=asset.media_object.mime_type,
        bytes_data=image_bytes,
    )


def _read_asset(
    session: Session,
    *,
    product_id: str,
    asset_id: str,
    storage: LocalStorage,
) -> tuple[ProductImageAsset, bytes]:
    asset = session.scalar(
        select(ProductImageAsset)
        .options(selectinload(ProductImageAsset.media_object))
        .where(ProductImageAsset.id == asset_id, ProductImageAsset.product_id == product_id)
    )
    if asset is None:
        raise BusinessValidationError("参考图不属于该商品")
    media = asset.media_object
    if media.mime_type not in SUPPORTED_REFERENCE_MIME_TYPES:
        raise BusinessValidationError("参考图仅支持 PNG、JPEG 或 WEBP")
    try:
        return asset, storage.resolve(media.storage_path).read_bytes()
    except OSError as exc:
        raise BusinessValidationError("参考图文件不可读取") from exc


def _compile_image_model_prompt(runtime: ImageRuntimeInput) -> str:
    payload = _prompt_payload_for_image_model(runtime)
    spec = runtime.generation_spec if isinstance(runtime.generation_spec, dict) else {}
    composition = payload.get("composition") if isinstance(payload.get("composition"), dict) else {}
    content = payload.get("content") if isinstance(payload.get("content"), dict) else {}
    atmosphere = payload.get("atmosphere") if isinstance(payload.get("atmosphere"), dict) else {}
    fidelity = payload.get("product_fidelity") if isinstance(payload.get("product_fidelity"), dict) else {}
    text = payload.get("text") if isinstance(payload.get("text"), dict) else {}
    image_type_key = runtime.image_type_key or ""
    family = image_type_family(image_type_key)
    type_option = agent_product_image_type_option(image_type_key)
    type_title = type_option.title if type_option else image_type_key or "电商商品图"
    job = image_type_generation_job(image_type_key)
    brief_lines = [
        f"生成一张能上淘宝/天猫详情的{type_title}，不是参考图修图交差。",
        "商品外形、结构、颜色、材质和可见零件以参考图为准。",
        "参考图只提供商品本体，构图、布光、场景和排版必须按图种重做。",
        LISTING_LOOK_RULE,
        NO_CAPTION_ON_REFERENCE_RULE + "。",
        "不要编造 Logo、认证、规格数字、价格或参考图与商品资料中未出现的结构。",
    ]
    if job:
        brief_lines.append(f"图种任务：{job}")
    if family == "infographic":
        brief_lines.append(
            "这是详情卖点图：抠出商品重新排版。一个主标题加 2 到 4 条对齐的短利益点，色块克制。商品仍是主角。"
        )
    elif family == "evidence":
        brief_lines.append("只能使用用户提供的资质或工厂画面，没有素材就不要生成假文件或假车间。")
    else:
        brief_lines.append(
            "这是可上架的商品摄影：主体约占画面 55%–75%，有光影质感。"
            "不要大面积空洞把商品挤到一角，也不要贴满标签。"
        )
    policy = spec.get("text_policy") or "none"
    if policy == "none":
        brief_lines.append(NO_ON_IMAGE_TEXT_RULE + "。")
    elif policy == "required":
        language = spec.get("text_language")
        if isinstance(language, str) and language.strip():
            brief_lines.append(f"画面必须包含图片内文字，语种为{language.strip()}，写短利益点，不要说明书。")
        else:
            brief_lines.append("画面必须包含图片内文字，写短利益点，不要说明书。")
    design_goal = _usable_prompt_text(payload.get("design_goal"))
    if design_goal:
        brief_lines.append(f"图目标：{design_goal}")
    viewpoint = _usable_prompt_text(composition.get("viewpoint"))
    layout = _usable_prompt_text(composition.get("layout"))
    if viewpoint or layout:
        brief_lines.append("构图：" + "，".join(item for item in (viewpoint, layout) if item))
    share = composition.get("product_share_percent")
    if isinstance(share, int | float):
        brief_lines.append(f"商品占比约 {share:g}%")
    focus = _usable_prompt_texts(content.get("focus"))
    if focus:
        brief_lines.append("主体：" + "、".join(focus))
    selling_points = _usable_prompt_texts(content.get("selling_points"))
    if selling_points:
        brief_lines.append("卖点：" + "、".join(selling_points))
    background = _usable_prompt_text(content.get("background"))
    if background:
        brief_lines.append(f"背景：{background}")
    lighting = _usable_prompt_text(atmosphere.get("lighting"))
    keywords = _usable_prompt_texts(atmosphere.get("keywords"))
    mood = "、".join(keywords) if keywords else ""
    if lighting or mood:
        brief_lines.append("氛围：" + "，".join(item for item in (lighting, mood) if item))
    requirements = _usable_prompt_texts(fidelity.get("requirements"))
    if requirements:
        brief_lines.append("保真：" + "、".join(requirements))
    if policy != "none":
        copy_bits = [
            _usable_prompt_text(text.get("headline")),
            _usable_prompt_text(text.get("subtitle")),
            _usable_prompt_text(text.get("body")),
        ]
        copy_bits = [item for item in copy_bits if item]
        if copy_bits:
            brief_lines.append("图片内文字：" + "；".join(copy_bits))
    shared_rules = _usable_prompt_texts(payload.get("shared_rules"))
    if shared_rules:
        brief_lines.append("规则：" + "；".join(shared_rules))
    if runtime.variation_instruction:
        brief_lines.append(f"变化：{runtime.variation_instruction}")
    contract = json.dumps(
        {
            "contract_version": 3,
            "task": "generate_one_ecommerce_listing_image",
            "image_type_key": image_type_key or None,
            "image_type_family": family,
            "prompt_artifact": payload,
            "visual_system": runtime.visual_system,
            "visual_overlay": runtime.visual_overlay,
            "variation_instruction": runtime.variation_instruction,
            "generation_spec": runtime.generation_spec,
            "reference_assets": [
                {"asset_id": item.asset_id, "label": item.label, "edge_id": item.edge_id}
                for item in runtime.reference_images
            ],
            "incoming_edge_ids": list(runtime.incoming_edge_ids),
        },
        ensure_ascii=False,
        sort_keys=True,
        indent=2,
    )
    return "\n".join(brief_lines) + "\n\n" + contract


def _prompt_payload_for_image_model(runtime: ImageRuntimeInput) -> dict[str, Any]:
    payload = dict(runtime.prompt_payload)
    spec = runtime.generation_spec if isinstance(runtime.generation_spec, dict) else {}
    if spec.get("text_policy") != "none":
        return payload
    payload["text"] = {"headline": None, "subtitle": None, "body": None}
    composition = payload.get("composition")
    if isinstance(composition, dict) and composition.get("copy_regions"):
        payload["composition"] = {**composition, "copy_regions": []}
    return payload


def _usable_prompt_text(value: object) -> str:
    if not isinstance(value, str):
        return ""
    text = value.strip()
    if not text or text == PROMPT_CONTEXT_DERIVED_PLACEHOLDER:
        return ""
    return text


def _usable_prompt_texts(value: object) -> list[str]:
    if not isinstance(value, list):
        return []
    return [item for item in (_usable_prompt_text(entry) for entry in value) if item]


def _prompt_from_runtime(
    runtime: PromptRuntimeInput,
    *,
    title: str,
    generate_from_context: bool | None = None,
) -> ImagePromptPayloadV1:
    stored = strip_v3_prompt_payload(runtime.prompt_config) if runtime.prompt_config else {}
    if generate_from_context is None:
        seed = _prompt_config_is_generation_seed(runtime.prompt_config)
    else:
        seed = generate_from_context
    brief_goal, brief_copy, brief_prohibitions = _brief_fields(runtime.briefs)
    product_name = _fact_value(runtime.product_facts, "product_name")
    type_option = agent_product_image_type_option(runtime.image_type_key or "")
    design_goal = stored.get("design_goal") or brief_goal
    if not design_goal:
        if type_option:
            design_goal = image_type_prompt_goal(type_option.key)
            if product_name:
                design_goal = f"为「{product_name}」生成{design_goal}"
        else:
            design_goal = f"为「{product_name}」生成{title}" if product_name else f"生成{title}"
    creative_boundary = [item for item in stored.get("creative_boundary") or [] if isinstance(item, str) and item]
    for item in brief_prohibitions:
        if item not in creative_boundary:
            creative_boundary.append(item)
    if runtime.text_policy == "none" and NO_ON_IMAGE_TEXT_RULE not in creative_boundary:
        creative_boundary.append(NO_ON_IMAGE_TEXT_RULE)
    text = stored.get("text") if isinstance(stored.get("text"), dict) else {}
    if runtime.text_policy == "none" and not _prompt_config_has_authored_text(runtime.prompt_config):
        text = {}
    elif brief_copy and not any(text.get(key) for key in ("headline", "subtitle", "body")):
        text = {
            **text,
            "headline": brief_copy[0],
            "body": "\n".join(brief_copy[1:]) or None,
        }
    shared_rules = [item for item in stored.get("shared_rules") or [] if isinstance(item, str) and item]
    if not shared_rules:
        shared_rules = list(IDENTITY_SHARED_RULES)
    if runtime.text_policy == "none" and NO_ON_IMAGE_TEXT_RULE not in shared_rules:
        shared_rules.append(NO_ON_IMAGE_TEXT_RULE)
    stored_content = stored.get("content") if isinstance(stored.get("content"), dict) else None
    derived = PROMPT_CONTEXT_DERIVED_PLACEHOLDER
    return ImagePromptPayloadV1.model_validate(
        {
            "schema_version": 1,
            "shared_rules": shared_rules,
            "design_goal": design_goal,
            "product_fidelity": stored.get("product_fidelity")
            or {
                "complex_structure": True,
                "product_present": True,
                "picture_in_picture": "none",
                "requirements": ["锁住参考图中的商品外形和材质", "构图和排版按图种重做"],
            },
            "creative_boundary": creative_boundary,
            "composition": stored.get("composition")
            or {
                "viewpoint": derived if seed else "正面",
                "product_share_percent": 70,
                "layout": derived if seed else "商品居中",
                "copy_regions": [],
            },
            "content": stored_content
            or {
                "focus": [product_name or (type_option.title if type_option else title)],
                "selling_points": [],
                "background": derived if seed else "干净背景",
                "decorations": [],
            },
            "text": {
                "headline": text.get("headline"),
                "subtitle": text.get("subtitle"),
                "body": text.get("body"),
            },
            "atmosphere": stored.get("atmosphere")
            or {
                "keywords": [derived] if seed else ["清晰"],
                "lighting": derived if seed else "均匀照明",
            },
            "images": [
                {
                    "image_plan_key": V3_PROMPT_PROVIDER_PLAN_KEY,
                    "instruction": design_goal if isinstance(design_goal, str) else f"生成{title}",
                }
            ],
        }
    )


def _apply_text_policy_to_prompt_payload(
    payload: ImagePromptPayloadV1,
    *,
    text_policy: str,
    keep_authored: bool,
) -> ImagePromptPayloadV1:
    if text_policy != "none" or keep_authored:
        return payload
    composition = payload.composition
    if composition.copy_regions:
        composition = composition.model_copy(update={"copy_regions": []})
    return payload.model_copy(update={"text": PromptTextContent(), "composition": composition})


def _prompt_config_has_authored_text(prompt_config: dict[str, Any] | None) -> bool:
    if not prompt_config:
        return False
    text = prompt_config.get("text")
    if isinstance(text, dict) and any(isinstance(item, str) and item.strip() for item in text.values()):
        return True
    composition = prompt_config.get("composition")
    if isinstance(composition, dict):
        regions = composition.get("copy_regions")
        if isinstance(regions, list) and any(isinstance(item, str) and item.strip() for item in regions):
            return True
    return False


def _prompt_config_is_generation_seed(prompt_config: dict[str, Any] | None) -> bool:
    if not prompt_config:
        return True
    for key in _AUTHORED_PROMPT_KEYS:
        value = prompt_config.get(key)
        if key == "text" and isinstance(value, dict):
            if any(isinstance(item, str) and item.strip() for item in value.values()):
                return False
            continue
        if key == "creative_boundary" and isinstance(value, list):
            if any(isinstance(item, str) and item.strip() for item in value):
                return False
            continue
        if value in (None, {}, [], ""):
            continue
        return False
    return True


def _fact_value(facts: tuple[dict[str, Any], ...], key: str) -> str:
    needle = key.casefold()
    for item in facts:
        if str(item.get("key") or "").casefold() != needle:
            continue
        value = item.get("value")
        if isinstance(value, str) and value.strip():
            return value.strip()
    return ""


def _brief_fields(briefs: tuple[dict[str, Any], ...]) -> tuple[str, list[str], list[str]]:
    goals: list[str] = []
    required_copy: list[str] = []
    prohibitions: list[str] = []
    for brief in briefs:
        if not brief:
            continue
        design_goals = brief.get("design_goals")
        if isinstance(design_goals, list):
            goals.extend(item for item in design_goals if isinstance(item, str) and item.strip())
        goal = brief.get("goal") or brief.get("title")
        if isinstance(goal, str) and goal.strip():
            goals.append(goal.strip())
        copy_items = brief.get("required_copy")
        if isinstance(copy_items, list):
            required_copy.extend(item for item in copy_items if isinstance(item, str) and item.strip())
        banned = brief.get("prohibitions")
        if isinstance(banned, list):
            prohibitions.extend(item for item in banned if isinstance(item, str) and item.strip())
    unique_goals = list(dict.fromkeys(goals))
    return (
        unique_goals[0] if unique_goals else "",
        list(dict.fromkeys(required_copy)),
        list(dict.fromkeys(prohibitions)),
    )


def _prompt_context_trace(runtime: PromptRuntimeInput) -> dict[str, Any]:
    return {
        "incoming_edge_ids": list(runtime.incoming_edge_ids),
        "input_digest": runtime.input_digest,
        "fact_count": len(runtime.product_facts),
        "brief_count": len(runtime.briefs),
        "reference_asset_ids": [item.asset_id for item in runtime.reference_images],
        "visual_system_version_id": runtime.visual_system_version_id,
        "text_policy": runtime.text_policy,
        "text_language": runtime.text_language,
    }


def _context_trace(runtime: ContextRuntimeInput) -> dict[str, Any]:
    return {
        "incoming_edge_ids": list(runtime.incoming_edge_ids),
        "input_digest": runtime.input_digest,
        "node_type": runtime.node_type.value,
        "fact_count": len(runtime.product_facts),
        "reference_asset_ids": [item.asset_id for item in runtime.reference_images],
        "text_policy": runtime.text_policy,
        "text_language": runtime.text_language,
    }


def _image_context_trace(runtime: ImageRuntimeInput) -> dict[str, Any]:
    return {
        "incoming_edge_ids": list(runtime.incoming_edge_ids),
        "input_digest": runtime.input_digest,
        "prompt_artifact_id": runtime.prompt_artifact_id,
        "prompt_edge_id": runtime.prompt_edge_id,
        "reference_asset_ids": [item.asset_id for item in runtime.reference_images],
        "visual_system_version_id": runtime.visual_system_version_id,
    }


def _claim_queued_node_run(session: Session, node_run: WorkflowGraphNodeRun) -> bool:
    result = session.execute(
        update(WorkflowGraphNodeRun)
        .where(
            WorkflowGraphNodeRun.id == node_run.id,
            WorkflowGraphNodeRun.status == WorkflowNodeStatus.QUEUED,
        )
        .values(status=WorkflowNodeStatus.RUNNING)
    )
    session.commit()
    session.refresh(node_run)
    return result.rowcount == 1


def _complete_skipped_node_run(
    session: Session,
    *,
    node_run: WorkflowGraphNodeRun,
    sources: dict[str, GraphSourceRecord],
    input_digest: str,
    trace: dict[str, Any],
) -> bool:
    if node_run.node_id is None:
        return False
    record = sources.get(node_run.node_id)
    if record is None or record.current_artifact_id is None or record.current_input_digest != input_digest:
        return False
    node_run.status = WorkflowNodeStatus.SUCCEEDED
    node_run.finished_at = now_utc()
    node_run.compiled_context_json = trace
    node_run.output_json = {"artifact_id": record.current_artifact_id, "skipped": True}
    session.commit()
    return True


def _fail_claimed_node(session: Session, *, run_id: str, node_run_id: str, reason: str) -> None:
    session.rollback()
    run = session.get(WorkflowGraphRun, run_id)
    node_run = session.get(WorkflowGraphNodeRun, node_run_id)
    if run is None:
        return
    if node_run is None:
        _fail_run(session, run_id=run_id, reason=reason)
        return
    _fail_node_and_run(session, run=run, node_run=node_run, reason=reason)


def _fail_node_and_run(session: Session, *, run: WorkflowGraphRun, node_run: WorkflowGraphNodeRun, reason: str) -> None:
    now = now_utc()
    node_run.status = WorkflowNodeStatus.FAILED
    node_run.failure_reason = reason
    node_run.finished_at = now
    run.status = WorkflowRunStatus.FAILED
    run.failure_reason = reason
    run.finished_at = now
    session.commit()


def _fail_run(session: Session, *, run_id: str, reason: str) -> None:
    run = session.get(WorkflowGraphRun, run_id)
    if run is None or run.status != WorkflowRunStatus.RUNNING:
        return
    run.status = WorkflowRunStatus.FAILED
    run.failure_reason = reason
    run.finished_at = now_utc()
    session.commit()
