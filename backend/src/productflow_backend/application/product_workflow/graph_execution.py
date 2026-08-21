from __future__ import annotations

import hashlib
import json
import logging
from typing import Any

from sqlalchemy import select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.product_images.assets import stage_product_image_asset
from productflow_backend.application.product_workflow.dependencies import (
    WorkflowExecutionDependencies,
    default_workflow_execution_dependencies,
)
from productflow_backend.application.product_workflow.graph_compiler import (
    GraphRuntimeArtifacts,
    ImageRuntimeInput,
    PromptRuntimeInput,
    applied_graph_from_snapshot,
    artifacts_from_sources,
    compile_image_runtime,
    compile_prompt_runtime,
    sources_from_snapshot,
    strip_v3_prompt_payload,
)
from productflow_backend.application.storage_compensation import StorageWriteCompensation
from productflow_backend.application.time import now_utc
from productflow_backend.application.workflow_drafts.contracts import (
    GenerationSpec,
    ImagePromptPayloadV1,
    VisualSystemDraftPayload,
)
from productflow_backend.domain.enums import (
    GraphArtifactType,
    GraphNodeType,
    ProductImageOriginType,
    WorkflowNodeStatus,
    WorkflowRunStatus,
)
from productflow_backend.domain.errors import BusinessValidationError, NotFoundError
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
from productflow_backend.infrastructure.image.base import WorkflowImageReference, WorkflowImageRequest
from productflow_backend.infrastructure.prompt.base import PromptGenerationRequest, PromptReferenceImage
from productflow_backend.infrastructure.storage import LocalStorage

logger = logging.getLogger(__name__)
SUPPORTED_REFERENCE_MIME_TYPES = {"image/png", "image/jpeg", "image/webp"}


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
        if run.status == WorkflowRunStatus.CANCELLED:
            return
        if node_run.status != WorkflowNodeStatus.QUEUED:
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
            _fail_node_and_run(session, run=run, node_run=node_run, reason=str(exc))
            return
        except Exception:
            logger.exception("schema-v3 graph node run failed: run_id=%s node_run_id=%s", run.id, node_run.id)
            _fail_node_and_run(session, run=run, node_run=node_run, reason="节点运行失败")
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
    node_run.status = WorkflowNodeStatus.RUNNING
    session.commit()
    if node_run.node_id is None:
        raise BusinessValidationError("运行节点已从当前图中删除")
    applied_node = graph.node(node_run.node_id)
    if applied_node.node_type == GraphNodeType.PROMPT_GENERATION:
        runtime = compile_prompt_runtime(graph, node_run.node_id, sources, artifacts)
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
        payload = strip_v3_prompt_payload(result.payload.model_dump(mode="json"))
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
        artifacts = artifacts.with_prompt(node_run.node_id, artifact_id=artifact.id, payload=payload)
    elif applied_node.node_type == GraphNodeType.IMAGE_GENERATION:
        runtime = compile_image_runtime(graph, node_run.node_id, sources, artifacts)
        node_run.compiled_context_json = _image_context_trace(runtime)
        image_provider = dependencies.image_provider()
        image_result = image_provider.generate_workflow_image(
            _to_image_request(runtime, session=session, storage=storage, product_id=product_id)
        )
        generated = image_result.images[0]
        product = session.get(Product, product_id)
        if product is None:
            raise NotFoundError("商品不存在")
        storage_writes = StorageWriteCompensation()
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
        storage_writes.release()
    else:
        raise BusinessValidationError("不能运行该节点类型")
    node_run.status = WorkflowNodeStatus.SUCCEEDED
    node_run.finished_at = now_utc()
    node_run.output_json = {"artifact_id": artifact.id}
    session.commit()
    return artifacts


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
    artifact = WorkflowGraphArtifact(
        graph_id=run.graph_id,
        node_id=node_run.node_id,
        node_run_id=node_run.id,
        artifact_type=artifact_type,
        schema_version=3,
        graph_revision=run.graph_revision,
        payload_json=payload,
        payload_hash=hashlib.sha256(encoded.encode("utf-8")).hexdigest(),
        input_digest=input_digest,
        product_image_asset_id=product_image_asset_id,
        provider_name=provider_name,
        provider_model=provider_model,
    )
    session.add(artifact)
    session.flush()
    live_graph = session.get(WorkflowGraph, run.graph_id)
    if live_graph is not None and live_graph.revision == run.graph_revision:
        node = session.get(WorkflowGraphNode, node_run.node_id)
        if node is not None:
            node.current_artifact_id = artifact.id
    return artifact


def _to_prompt_request(
    runtime: PromptRuntimeInput,
    *,
    title: str,
    session: Session,
    storage: LocalStorage,
    product_id: str,
) -> PromptGenerationRequest:
    visual = (
        VisualSystemDraftPayload.model_validate(runtime.visual_system)
        if runtime.visual_system is not None
        else None
    )
    return PromptGenerationRequest(
        image_type_key=runtime.image_type_key or "unspecified",
        image_plan_keys=("output",),
        facts=runtime.product_facts,
        visual_system=visual,
        visual_exceptions=(),
        current_prompt=_prompt_from_runtime(runtime, title=title),
        text_languages=(),
        reference_images=tuple(
            _load_prompt_reference(session, product_id=product_id, reference=reference, storage=storage)
            for reference in runtime.reference_images
        ),
    )


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
    compiled = json.dumps(
        {
            "contract_version": 3,
            "task": "generate_one_ecommerce_product_image",
            "prompt_artifact": runtime.prompt_payload,
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
    return WorkflowImageRequest(
        compiled_prompt=(
            "请严格依据以下已确认合同生成一张电商商品图片。商品形态以参考图为准；"
            "不得编造 Logo、认证、规格、价格或未提供的商品特征。\n\n"
            f"{compiled}"
        ),
        generation_spec=GenerationSpec.model_validate(runtime.generation_spec),
        references=references,
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
        role="reference",
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
        role="reference",
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


def _prompt_from_runtime(runtime: PromptRuntimeInput, *, title: str) -> ImagePromptPayloadV1:
    stored = strip_v3_prompt_payload(runtime.prompt_config) if runtime.prompt_config else {}
    brief_goal, brief_copy, brief_prohibitions = _brief_fields(runtime.briefs)
    design_goal = stored.get("design_goal") or brief_goal or f"生成{title}"
    creative_boundary = [item for item in stored.get("creative_boundary") or [] if isinstance(item, str) and item]
    for item in brief_prohibitions:
        if item not in creative_boundary:
            creative_boundary.append(item)
    text = stored.get("text") if isinstance(stored.get("text"), dict) else {}
    if brief_copy and not any(text.get(key) for key in ("headline", "subtitle", "body")):
        text = {
            **text,
            "headline": brief_copy[0],
            "body": "\n".join(brief_copy[1:]) or None,
        }
    shared_rules = [item for item in stored.get("shared_rules") or [] if isinstance(item, str) and item]
    return ImagePromptPayloadV1.model_validate(
        {
            "schema_version": 1,
            "shared_rules": shared_rules or ["保持商品结构与参考图一致"],
            "design_goal": design_goal,
            "product_fidelity": stored.get("product_fidelity")
            or {
                "complex_structure": True,
                "product_present": True,
                "picture_in_picture": "none",
                "requirements": ["还原商品形态"],
            },
            "creative_boundary": creative_boundary,
            "composition": stored.get("composition")
            or {
                "viewpoint": "正面",
                "product_share_percent": 70,
                "layout": "商品居中",
                "copy_regions": [],
            },
            "content": stored.get("content")
            or {
                "focus": [title],
                "selling_points": [],
                "background": "干净背景",
                "decorations": [],
            },
            "text": {
                "headline": text.get("headline"),
                "subtitle": text.get("subtitle"),
                "body": text.get("body"),
            },
            "atmosphere": stored.get("atmosphere") or {"keywords": ["清晰"], "lighting": "均匀照明"},
            "images": [
                {
                    "image_plan_key": "output",
                    "instruction": design_goal if isinstance(design_goal, str) else f"生成{title}",
                }
            ],
        }
    )


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
