from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass
from typing import Any

from pydantic import ValidationError
from sqlalchemy import func, select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.media_assets import inspect_image_bytes, stage_product_image_asset
from productflow_backend.application.product_workflow.run_state import (
    WorkflowSafeExecutionError,
    claim_workflow_node_run,
    mark_workflow_run_failed,
    requeue_workflow_node_run_after_capacity_wait,
    workflow_run_failure_context,
)
from productflow_backend.application.product_workflow_dependencies import (
    WorkflowExecutionDependencies,
    default_workflow_execution_dependencies,
)
from productflow_backend.application.storage_compensation import StorageWriteCompensation
from productflow_backend.application.time import now_utc
from productflow_backend.application.workflow_drafts.contracts import (
    GenerationSpec,
    ImagePromptPayloadV1,
    VisualSystemDraftPayload,
)
from productflow_backend.domain.enums import (
    ProductImageOriginType,
    WorkflowNodeStatus,
    WorkflowNodeType,
    WorkflowRunStatus,
)
from productflow_backend.domain.errors import ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    ImagePromptArtifact,
    ImagePromptArtifactVersion,
    ImagePromptArtifactVersionReference,
    Product,
    ProductFactSetVersion,
    ProductImageAsset,
    ProductWorkflow,
    VisualException,
    VisualSystemVersionReference,
    WorkflowEdge,
    WorkflowImageGenerationRecord,
    WorkflowImageGenerationReference,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRun,
)
from productflow_backend.infrastructure.image.base import (
    ImageProvider,
    WorkflowGeneratedImage,
    WorkflowImageReference,
    WorkflowImageRequest,
    WorkflowImageResult,
    infer_extension,
)
from productflow_backend.infrastructure.prompt.base import (
    PromptGenerationProvider,
    PromptGenerationRequest,
    PromptGenerationResult,
    PromptReferenceImage,
)
from productflow_backend.infrastructure.storage import LocalStorage

V2_WORKFLOW_SCHEMA_VERSION = 2
MAX_PROMPT_REFERENCE_IMAGES = 6
SUPPORTED_PROMPT_IMAGE_TYPES = {"image/png", "image/jpeg", "image/webp"}


@dataclass(frozen=True, slots=True)
class PreparedPromptGeneration:
    run_id: str
    node_id: str
    node_run_id: str
    prompt_artifact_id: str
    current_prompt_version_id: str
    visual_variant_keys: frozenset[str]
    fact_keys: frozenset[str]
    request: PromptGenerationRequest


@dataclass(frozen=True, slots=True)
class PreparedImageGeneration:
    run_id: str
    node_id: str
    node_run_id: str
    product_id: str
    workflow_id: str
    image_plan_key: str
    prompt_artifact_version_id: str
    visual_system_version_id: str
    compiled_prompt: str
    compiled_prompt_hash: str
    request: WorkflowImageRequest


def execute_v2_workflow_node_run(
    session: Session,
    *,
    node_run_id: str,
    dependencies: WorkflowExecutionDependencies | None = None,
    storage: LocalStorage | None = None,
) -> None:
    node_run = session.get(WorkflowNodeRun, node_run_id)
    if node_run is None:
        raise NotFoundError("工作流节点运行不存在")
    run = node_run.workflow_run
    workflow = run.workflow
    node = node_run.node
    _ensure_v2_single_node_run(session, workflow=workflow, run=run, node=node)
    if node.node_type not in {WorkflowNodeType.PROMPT_GENERATION, WorkflowNodeType.IMAGE_GENERATION}:
        raise ConflictError("schema-v2 executor 只处理提示词或图片生成节点")
    if node_run.status != WorkflowNodeStatus.QUEUED:
        return

    claim = claim_workflow_node_run(session, node_run_id=node_run.id, node_id=node.id)
    if not claim.claimed:
        if claim.should_requeue:
            requeue_workflow_node_run_after_capacity_wait(node_run.id)
        return

    resolved_dependencies = dependencies or default_workflow_execution_dependencies()
    resolved_storage = storage or LocalStorage()
    storage_writes = StorageWriteCompensation()
    try:
        if node.node_type == WorkflowNodeType.PROMPT_GENERATION:
            prepared_prompt = _prepare_prompt_generation(
                session,
                node_run_id=node_run.id,
                storage=resolved_storage,
            )
            session.commit()
            prompt_provider = resolved_dependencies.prompt_generation_provider()
            prompt_result = _generate_prompt(prompt_provider, prepared_prompt.request)
            _validate_prompt_result(prepared_prompt, prompt_result)
            _persist_prompt_result(
                session,
                prepared=prepared_prompt,
                result=prompt_result,
                provider_name=prompt_provider.provider_name,
            )
        else:
            prepared_image = _prepare_image_generation(
                session,
                node_run_id=node_run.id,
                storage=resolved_storage,
            )
            session.commit()
            image_provider = resolved_dependencies.image_provider()
            image_result = _generate_workflow_image(image_provider, prepared_image.request)
            generated_image = _validate_workflow_image_result(image_result)
            _persist_image_result(
                session,
                prepared=prepared_image,
                result=image_result,
                generated_image=generated_image,
                provider_name=image_provider.provider_name,
                storage=resolved_storage,
                storage_writes=storage_writes,
            )
    except Exception as exc:  # noqa: BLE001
        session.rollback()
        storage_writes.cleanup()
        failure = workflow_run_failure_context(exc)
        mark_workflow_run_failed(
            session,
            run_id=run.id,
            failed_node_id=node.id,
            **failure,
        )


def _ensure_v2_single_node_run(
    session: Session,
    *,
    workflow: ProductWorkflow,
    run: WorkflowRun,
    node: WorkflowNode,
) -> None:
    if workflow.schema_version != V2_WORKFLOW_SCHEMA_VERSION or node.schema_version != V2_WORKFLOW_SCHEMA_VERSION:
        raise ConflictError("schema-v2 executor 拒绝处理 schema-v1 工作流或节点")
    if not workflow.active:
        raise ConflictError("只能运行 active schema-v2 工作流")
    node_run_count = session.scalar(
        select(func.count()).select_from(WorkflowNodeRun).where(WorkflowNodeRun.workflow_run_id == run.id)
    )
    if node_run_count != 1:
        raise ConflictError("schema-v2 单节点运行必须且只能包含一个 WorkflowNodeRun")


def _prepare_prompt_generation(
    session: Session,
    *,
    node_run_id: str,
    storage: LocalStorage,
) -> PreparedPromptGeneration:
    node_run = session.scalar(
        select(WorkflowNodeRun)
        .options(
            selectinload(WorkflowNodeRun.workflow_run).selectinload(WorkflowRun.workflow),
            selectinload(WorkflowNodeRun.node).selectinload(WorkflowNode.current_prompt_artifact_version),
        )
        .where(WorkflowNodeRun.id == node_run_id)
    )
    if node_run is None:
        raise NotFoundError("工作流节点运行不存在")
    run = node_run.workflow_run
    node = node_run.node
    workflow = run.workflow
    if run.status != WorkflowRunStatus.RUNNING or node_run.status != WorkflowNodeStatus.RUNNING:
        raise ConflictError("工作流节点运行不再处于 running 状态")
    if node.node_type != WorkflowNodeType.PROMPT_GENERATION:
        raise ConflictError("当前 schema-v2 节点不是提示词节点")
    current_version = node.current_prompt_artifact_version
    if current_version is None:
        raise ConflictError("提示词节点缺少 current Prompt Artifact version")
    prompt_artifact = current_version.artifact
    if prompt_artifact.workflow_id != workflow.id:
        raise ConflictError("提示词节点绑定了其他工作流的 Prompt Artifact")
    if prompt_artifact.image_type_key != node.config_json.get("image_type_key"):
        raise ConflictError("提示词节点 image type 与 Prompt Artifact 不一致")

    current_prompt = _parse_prompt_payload(current_version)
    image_plan_keys = tuple(image.image_plan_key for image in current_prompt.images)
    image_nodes = list(
        session.scalars(
            select(WorkflowNode)
            .where(
                WorkflowNode.workflow_id == workflow.id,
                WorkflowNode.node_type == WorkflowNodeType.IMAGE_GENERATION,
            )
            .order_by(WorkflowNode.node_key, WorkflowNode.id)
        )
    )
    matching_image_nodes = [
        image_node
        for image_node in image_nodes
        if image_node.config_json.get("prompt_plan_key") == node.config_json.get("prompt_plan_key")
    ]
    node_image_plan_keys = {image_node.config_json.get("image_plan_key") for image_node in matching_image_nodes}
    if node_image_plan_keys != set(image_plan_keys):
        raise ConflictError("Prompt Artifact 的逐图计划与工作流图片节点不一致")

    visual_version = workflow.visual_system_version
    if visual_version is None:
        raise ConflictError("schema-v2 工作流缺少 VisualSystemVersion")
    visual_system = _parse_visual_system(visual_version.payload_json, visual_version.payload_hash)
    visual_variant_keys = frozenset(variant.key for variant in visual_system.variants)

    source_revision = workflow.source_draft_revision
    fact_set = source_revision.fact_set_version if source_revision is not None else None
    facts = _confirmed_facts(fact_set)
    fact_keys = frozenset(str(fact["key"]) for fact in facts)
    exceptions = _matching_visual_exceptions(
        workflow.visual_exceptions,
        image_type_key=prompt_artifact.image_type_key,
        image_plan_keys=set(image_plan_keys),
    )
    text_languages = tuple(
        sorted(
            {
                language
                for image_node in matching_image_nodes
                if isinstance((spec := image_node.config_json.get("generation_spec")), dict)
                and isinstance((language := spec.get("text_language")), str)
                and language
            }
        )
    )
    references = _load_prompt_references(
        session,
        workflow=workflow,
        prompt_node=node,
        current_version=current_version,
        storage=storage,
    )
    return PreparedPromptGeneration(
        run_id=run.id,
        node_id=node.id,
        node_run_id=node_run.id,
        prompt_artifact_id=prompt_artifact.id,
        current_prompt_version_id=current_version.id,
        visual_variant_keys=visual_variant_keys,
        fact_keys=fact_keys,
        request=PromptGenerationRequest(
            image_type_key=prompt_artifact.image_type_key,
            image_plan_keys=image_plan_keys,
            facts=tuple(facts),
            visual_system=visual_system,
            visual_exceptions=tuple(exceptions),
            current_prompt=current_prompt,
            text_languages=text_languages,
            reference_images=tuple(references),
        ),
    )


def _prepare_image_generation(
    session: Session,
    *,
    node_run_id: str,
    storage: LocalStorage,
) -> PreparedImageGeneration:
    node_run = session.scalar(
        select(WorkflowNodeRun)
        .options(
            selectinload(WorkflowNodeRun.workflow_run).selectinload(WorkflowRun.workflow),
            selectinload(WorkflowNodeRun.node),
        )
        .where(WorkflowNodeRun.id == node_run_id)
    )
    if node_run is None:
        raise NotFoundError("工作流节点运行不存在")
    run = node_run.workflow_run
    node = node_run.node
    workflow = run.workflow
    if run.status != WorkflowRunStatus.RUNNING or node_run.status != WorkflowNodeStatus.RUNNING:
        raise ConflictError("工作流节点运行不再处于 running 状态")
    if node.node_type != WorkflowNodeType.IMAGE_GENERATION:
        raise ConflictError("当前 schema-v2 节点不是图片生成节点")

    image_type_key = node.config_json.get("image_type_key")
    image_plan_key = node.config_json.get("image_plan_key")
    prompt_plan_key = node.config_json.get("prompt_plan_key")
    if not all(isinstance(value, str) and value for value in (image_type_key, image_plan_key, prompt_plan_key)):
        raise ConflictError("图片节点缺少 image type、image plan 或 prompt plan key")
    try:
        generation_spec = GenerationSpec.model_validate(node.config_json.get("generation_spec"))
    except ValidationError as exc:
        raise ConflictError("图片节点 GenerationSpec 不符合 schema") from exc

    prompt_nodes = list(
        session.scalars(
            select(WorkflowNode)
            .options(selectinload(WorkflowNode.current_prompt_artifact_version))
            .where(
                WorkflowNode.workflow_id == workflow.id,
                WorkflowNode.node_type == WorkflowNodeType.PROMPT_GENERATION,
            )
        )
    )
    matching_prompt_nodes = [
        prompt_node
        for prompt_node in prompt_nodes
        if prompt_node.config_json.get("prompt_plan_key") == prompt_plan_key
    ]
    if len(matching_prompt_nodes) != 1:
        raise ConflictError("图片节点必须且只能解析到一个提示词节点")
    prompt_node = matching_prompt_nodes[0]
    prompt_version = prompt_node.current_prompt_artifact_version
    if prompt_version is None:
        raise ConflictError("图片节点上游提示词节点缺少 current version")
    prompt_artifact = prompt_version.artifact
    if prompt_artifact.workflow_id != workflow.id or prompt_artifact.image_type_key != image_type_key:
        raise ConflictError("图片节点与 Prompt Artifact 的 workflow 或 image type 不一致")
    prompt_payload = _parse_prompt_payload(prompt_version)
    per_image_prompt = next(
        (image for image in prompt_payload.images if image.image_plan_key == image_plan_key),
        None,
    )
    if per_image_prompt is None:
        raise ConflictError("Prompt Artifact 缺少当前图片节点的逐图计划")

    visual_version = workflow.visual_system_version
    if visual_version is None:
        raise ConflictError("schema-v2 工作流缺少 VisualSystemVersion")
    visual_system = _parse_visual_system(visual_version.payload_json, visual_version.payload_hash)
    variant = next(
        (item for item in visual_system.variants if item.key == prompt_payload.visual_variant_key),
        None,
    )
    if prompt_payload.visual_variant_key is not None and variant is None:
        raise ConflictError("Prompt Artifact 引用了 VisualSystemVersion 中不存在的 variant")

    source_revision = workflow.source_draft_revision
    facts = _confirmed_facts(source_revision.fact_set_version if source_revision is not None else None)
    facts_by_key = {str(fact["key"]): fact for fact in facts}
    if not set(prompt_payload.fact_keys).issubset(facts_by_key):
        raise ConflictError("Prompt Artifact 引用了未确认的商品事实")
    selected_facts = [facts_by_key[key] for key in prompt_payload.fact_keys]
    exceptions = _matching_visual_exceptions(
        workflow.visual_exceptions,
        image_type_key=image_type_key,
        image_plan_keys={image_plan_key},
    )
    references = _load_image_references(
        session,
        workflow=workflow,
        image_node=node,
        storage=storage,
    )
    compiled_prompt = _compile_image_prompt(
        image_type_key=image_type_key,
        image_plan_key=image_plan_key,
        prompt_payload=prompt_payload,
        per_image_prompt=per_image_prompt.model_dump(mode="json"),
        visual_system=visual_system,
        visual_variant=variant.model_dump(mode="json") if variant is not None else None,
        visual_exceptions=exceptions,
        facts=selected_facts,
        generation_spec=generation_spec,
        variation_instruction=node.config_json.get("variation_instruction"),
        references=references,
    )
    return PreparedImageGeneration(
        run_id=run.id,
        node_id=node.id,
        node_run_id=node_run.id,
        product_id=workflow.product_id,
        workflow_id=workflow.id,
        image_plan_key=image_plan_key,
        prompt_artifact_version_id=prompt_version.id,
        visual_system_version_id=visual_version.id,
        compiled_prompt=compiled_prompt,
        compiled_prompt_hash=hashlib.sha256(compiled_prompt.encode("utf-8")).hexdigest(),
        request=WorkflowImageRequest(
            compiled_prompt=compiled_prompt,
            generation_spec=generation_spec,
            references=tuple(references),
        ),
    )


def _compile_image_prompt(
    *,
    image_type_key: str,
    image_plan_key: str,
    prompt_payload: ImagePromptPayloadV1,
    per_image_prompt: dict[str, Any],
    visual_system: VisualSystemDraftPayload,
    visual_variant: dict[str, Any] | None,
    visual_exceptions: list[dict[str, Any]],
    facts: list[dict[str, Any]],
    generation_spec: GenerationSpec,
    variation_instruction: Any,
    references: list[WorkflowImageReference],
) -> str:
    contract = {
        "contract_version": 1,
        "task": "generate_one_ecommerce_product_image",
        "image_type_key": image_type_key,
        "image_plan_key": image_plan_key,
        "confirmed_product_facts": facts,
        "visual_system": visual_system.model_dump(mode="json"),
        "visual_variant": visual_variant,
        "visual_exceptions": visual_exceptions,
        "prompt_artifact": {
            key: value
            for key, value in prompt_payload.model_dump(mode="json").items()
            if key != "images"
        },
        "per_image_prompt": per_image_prompt,
        "variation_instruction": variation_instruction if isinstance(variation_instruction, str) else None,
        "generation_spec": generation_spec.model_dump(mode="json"),
        "reference_assets": [
            {
                "asset_id": reference.asset_id,
                "role": reference.role,
                "label": reference.label,
            }
            for reference in references
        ],
    }
    encoded = json.dumps(contract, ensure_ascii=False, sort_keys=True, indent=2)
    return (
        "请严格依据以下已确认合同生成一张电商商品图片。商品形态、比例、材质和结构以参考图为准；"
        "不得编造 Logo、认证、规格、价格或未提供的商品特征。\n\n"
        f"{encoded}"
    )


def _load_image_references(
    session: Session,
    *,
    workflow: ProductWorkflow,
    image_node: WorkflowNode,
    storage: LocalStorage,
) -> list[WorkflowImageReference]:
    nodes = list(session.scalars(select(WorkflowNode).where(WorkflowNode.workflow_id == workflow.id)))
    nodes_by_id = {node.id: node for node in nodes}
    edges = list(session.scalars(select(WorkflowEdge).where(WorkflowEdge.workflow_id == workflow.id)))
    incoming_by_target: dict[str, list[str]] = {}
    for edge in edges:
        incoming_by_target.setdefault(edge.target_node_id, []).append(edge.source_node_id)

    ancestor_ids: set[str] = set()
    pending = list(incoming_by_target.get(image_node.id, []))
    while pending:
        source_id = pending.pop()
        if source_id in ancestor_ids:
            continue
        ancestor_ids.add(source_id)
        pending.extend(incoming_by_target.get(source_id, []))

    candidate_nodes = sorted(
        (
            node
            for node_id in ancestor_ids
            if (node := nodes_by_id.get(node_id)) is not None
            and node.node_type in {WorkflowNodeType.REFERENCE_IMAGE, WorkflowNodeType.IMAGE_GENERATION}
        ),
        key=lambda item: (item.position_x, item.position_y, item.node_key or "", item.id),
    )
    candidates: list[tuple[str, str, str, bool]] = []
    for candidate in candidate_nodes:
        if candidate.bound_image_asset_id is None:
            if candidate.node_type == WorkflowNodeType.IMAGE_GENERATION:
                raise ConflictError("上游图片节点尚无可复用结果")
            raise ConflictError("上游参考图节点缺少绑定资产")
        if candidate.node_type == WorkflowNodeType.REFERENCE_IMAGE:
            role = str(candidate.config_json.get("role") or "reference")
            label = str(candidate.config_json.get("label") or candidate.title)
        else:
            role = "upstream_generated"
            label = candidate.title
        candidates.append((candidate.bound_image_asset_id, role, label))

    unique_candidates: list[tuple[str, str, str]] = []
    seen_asset_ids: set[str] = set()
    for candidate in candidates:
        if candidate[0] in seen_asset_ids:
            continue
        seen_asset_ids.add(candidate[0])
        unique_candidates.append(candidate)
    if not unique_candidates:
        raise ConflictError("图片节点至少需要一个显式上游参考资产")
    if len(unique_candidates) > MAX_PROMPT_REFERENCE_IMAGES:
        raise ConflictError(f"图片节点参考图片不能超过 {MAX_PROMPT_REFERENCE_IMAGES} 张")

    references: list[WorkflowImageReference] = []
    for asset_id, role, label in unique_candidates:
        asset, image_bytes = _read_product_image_asset(
            session,
            product_id=workflow.product_id,
            asset_id=asset_id,
            storage=storage,
        )
        references.append(
            WorkflowImageReference(
                asset_id=asset.id,
                role=role,
                label=label,
                filename=asset.original_filename,
                mime_type=asset.media_object.mime_type,
                bytes_data=image_bytes,
            )
        )
    return references


def _parse_prompt_payload(version: ImagePromptArtifactVersion) -> ImagePromptPayloadV1:
    try:
        payload = ImagePromptPayloadV1.model_validate(version.payload_json)
    except ValidationError as exc:
        raise ConflictError("Prompt Artifact version payload 不符合 schema version 1") from exc
    if _json_hash(payload.model_dump(mode="json")) != version.payload_hash:
        raise ConflictError("Prompt Artifact version payload hash 不一致")
    return payload


def _parse_visual_system(payload_json: dict[str, Any], payload_hash: str) -> VisualSystemDraftPayload:
    try:
        payload = VisualSystemDraftPayload.model_validate(payload_json)
    except ValidationError as exc:
        raise ConflictError("VisualSystemVersion payload 不符合 schema version 1") from exc
    if _json_hash(payload.model_dump(mode="json")) != payload_hash:
        raise ConflictError("VisualSystemVersion payload hash 不一致")
    return payload


def _confirmed_facts(fact_set: ProductFactSetVersion | None) -> list[dict[str, Any]]:
    if fact_set is None:
        raise ConflictError("schema-v2 工作流缺少确认商品事实版本")
    facts = fact_set.payload_json.get("facts")
    if not isinstance(facts, list) or not facts or any(not isinstance(fact, dict) for fact in facts):
        raise ConflictError("确认商品事实版本 payload 无效")
    return [dict(fact) for fact in facts]


def _matching_visual_exceptions(
    exceptions: list[VisualException],
    *,
    image_type_key: str,
    image_plan_keys: set[str],
) -> list[dict[str, Any]]:
    matched = [
        exception
        for exception in exceptions
        if exception.scope_type == "workflow"
        or (exception.scope_type == "image_type" and exception.scope_key == image_type_key)
        or (exception.scope_type == "image_plan" and exception.scope_key in image_plan_keys)
    ]
    return [
        {
            "key": exception.exception_key,
            "scope": {"type": exception.scope_type, "key": exception.scope_key},
            "overrides": exception.overrides_json,
            "reason": exception.reason,
        }
        for exception in sorted(matched, key=lambda item: item.exception_key)
    ]


def _load_prompt_references(
    session: Session,
    *,
    workflow: ProductWorkflow,
    prompt_node: WorkflowNode,
    current_version: ImagePromptArtifactVersion,
    storage: LocalStorage,
) -> list[PromptReferenceImage]:
    candidates: list[tuple[str, str, str]] = []
    incoming_edges = list(
        session.scalars(
            select(WorkflowEdge)
            .where(WorkflowEdge.workflow_id == workflow.id, WorkflowEdge.target_node_id == prompt_node.id)
            .order_by(WorkflowEdge.edge_key, WorkflowEdge.id)
        )
    )
    source_node_ids = [edge.source_node_id for edge in incoming_edges]
    if source_node_ids:
        reference_nodes = list(
            session.scalars(
                select(WorkflowNode)
                .where(
                    WorkflowNode.id.in_(source_node_ids),
                    WorkflowNode.node_type == WorkflowNodeType.REFERENCE_IMAGE,
                )
                .order_by(WorkflowNode.position_x, WorkflowNode.position_y, WorkflowNode.node_key, WorkflowNode.id)
            )
        )
        for node in reference_nodes:
            if node.bound_image_asset_id is not None:
                candidates.append(
                    (
                        node.bound_image_asset_id,
                        str(node.config_json.get("role") or "reference"),
                        str(node.config_json.get("label") or node.title),
                        True,
                    )
                )

    visual_references = list(
        session.scalars(
            select(VisualSystemVersionReference)
            .where(VisualSystemVersionReference.visual_system_version_id == workflow.visual_system_version_id)
            .order_by(VisualSystemVersionReference.position)
        )
    )
    visual_reference_asset_ids = {reference.asset_id for reference in visual_references}
    prompt_references = list(
        session.scalars(
            select(ImagePromptArtifactVersionReference)
            .where(ImagePromptArtifactVersionReference.prompt_artifact_version_id == current_version.id)
            .order_by(ImagePromptArtifactVersionReference.position)
        )
    )
    candidates.extend(
        (
            reference.asset_id,
            reference.purpose,
            "提示词证据",
            reference.asset_id not in visual_reference_asset_ids,
        )
        for reference in prompt_references
    )
    candidates.extend(
        (reference.asset_id, reference.role, reference.label, False) for reference in visual_references
    )

    unique_candidates: list[tuple[str, str, str, bool]] = []
    seen_asset_ids: set[str] = set()
    for candidate in candidates:
        if candidate[0] in seen_asset_ids:
            continue
        seen_asset_ids.add(candidate[0])
        unique_candidates.append(candidate)
    if len(unique_candidates) > MAX_PROMPT_REFERENCE_IMAGES:
        raise ConflictError(f"提示词节点参考图片不能超过 {MAX_PROMPT_REFERENCE_IMAGES} 张")

    references: list[PromptReferenceImage] = []
    for asset_id, role, label, require_product_ownership in unique_candidates:
        asset, image_bytes = _read_product_image_asset(
            session,
            product_id=workflow.product_id if require_product_ownership else None,
            asset_id=asset_id,
            storage=storage,
        )
        references.append(
            PromptReferenceImage(
                asset_id=asset.id,
                role=role,
                label=label,
                filename=asset.original_filename,
                mime_type=asset.media_object.mime_type,
                image_bytes=image_bytes,
            )
        )
    return references


def _read_product_image_asset(
    session: Session,
    *,
    product_id: str | None,
    asset_id: str,
    storage: LocalStorage,
) -> tuple[ProductImageAsset, bytes]:
    asset = session.scalar(
        select(ProductImageAsset)
        .options(selectinload(ProductImageAsset.media_object))
        .where(ProductImageAsset.id == asset_id)
    )
    if asset is None or (product_id is not None and asset.product_id != product_id):
        raise ConflictError("工作流节点引用了不存在或属于其他商品的图片资产")
    media = asset.media_object
    if media.mime_type not in SUPPORTED_PROMPT_IMAGE_TYPES:
        raise ConflictError("工作流节点参考图片仅支持 PNG、JPEG 或 WEBP")
    path = storage.resolve(media.storage_path)
    try:
        return asset, path.read_bytes()
    except OSError as exc:
        raise ConflictError("工作流节点参考图片文件不可读取") from exc


def _generate_prompt(
    provider: PromptGenerationProvider,
    request: PromptGenerationRequest,
) -> PromptGenerationResult:
    try:
        return provider.generate_prompt(request)
    except ValidationError as exc:
        raise WorkflowSafeExecutionError(
            "提示词 provider 返回内容不符合 Prompt Artifact schema",
            retryable=False,
            retry_hint="revise_input",
            failure_category="provider_contract",
        ) from exc
    except WorkflowSafeExecutionError:
        raise
    except Exception as exc:
        raise WorkflowSafeExecutionError(
            "提示词生成失败，请稍后重试",
            retryable=True,
            retry_hint="retry_later",
            failure_category="provider_failure",
        ) from exc


def _validate_prompt_result(prepared: PreparedPromptGeneration, result: PromptGenerationResult) -> None:
    result_image_plan_keys = tuple(image.image_plan_key for image in result.payload.images)
    if result_image_plan_keys != prepared.request.image_plan_keys:
        raise WorkflowSafeExecutionError(
            "提示词 provider 改变了逐图计划集合或顺序",
            retryable=False,
            retry_hint="revise_input",
            failure_category="provider_contract",
        )
    if not set(result.payload.fact_keys).issubset(prepared.fact_keys):
        raise WorkflowSafeExecutionError(
            "提示词 provider 引用了未确认的商品事实",
            retryable=False,
            retry_hint="revise_input",
            failure_category="provider_contract",
        )
    reference_asset_ids = {reference.asset_id for reference in prepared.request.reference_images}
    if not set(result.payload.evidence_asset_ids).issubset(reference_asset_ids):
        raise WorkflowSafeExecutionError(
            "提示词 provider 引用了未提供的图片证据",
            retryable=False,
            retry_hint="revise_input",
            failure_category="provider_contract",
        )
    variant_key = result.payload.visual_variant_key
    if variant_key is not None and variant_key not in prepared.visual_variant_keys:
        raise WorkflowSafeExecutionError(
            "提示词 provider 引用了不存在的视觉变体",
            retryable=False,
            retry_hint="revise_input",
            failure_category="provider_contract",
        )


def _persist_prompt_result(
    session: Session,
    *,
    prepared: PreparedPromptGeneration,
    result: PromptGenerationResult,
    provider_name: str,
) -> None:
    node_run = session.scalar(
        select(WorkflowNodeRun).where(WorkflowNodeRun.id == prepared.node_run_id).with_for_update()
    )
    run = session.scalar(select(WorkflowRun).where(WorkflowRun.id == prepared.run_id).with_for_update())
    node = session.scalar(select(WorkflowNode).where(WorkflowNode.id == prepared.node_id).with_for_update())
    prompt_artifact = session.scalar(
        select(ImagePromptArtifact)
        .where(ImagePromptArtifact.id == prepared.prompt_artifact_id)
        .with_for_update()
    )
    if node_run is None or run is None or node is None or prompt_artifact is None:
        raise ConflictError("提示词节点运行状态已不存在")
    if run.status == WorkflowRunStatus.CANCELLED:
        session.rollback()
        return
    if run.status != WorkflowRunStatus.RUNNING or node_run.status != WorkflowNodeStatus.RUNNING:
        raise ConflictError("提示词节点运行状态已变化")
    if node.current_prompt_artifact_version_id != prepared.current_prompt_version_id:
        raise ConflictError("提示词节点 current version 已变化，拒绝覆盖新结果")

    next_version = (
        session.scalar(
            select(func.max(ImagePromptArtifactVersion.version)).where(
                ImagePromptArtifactVersion.artifact_id == prompt_artifact.id
            )
        )
        or 0
    ) + 1
    payload_json = result.payload.model_dump(mode="json")
    version = ImagePromptArtifactVersion(
        artifact_id=prompt_artifact.id,
        version=next_version,
        schema_version=1,
        payload_json=payload_json,
        payload_hash=_json_hash(payload_json),
        source_node_run_id=node_run.id,
        provider_name=provider_name,
        provider_model=result.model,
        provider_response_id=result.response_id,
    )
    session.add(version)
    session.flush()
    for position, asset_id in enumerate(result.payload.evidence_asset_ids):
        session.add(
            ImagePromptArtifactVersionReference(
                prompt_artifact_version_id=version.id,
                asset_id=asset_id,
                purpose="evidence",
                position=position,
            )
        )

    output = {
        "contract_version": 2,
        "prompt_artifact_id": prompt_artifact.id,
        "prompt_artifact_version_id": version.id,
        "version": version.version,
    }
    now = now_utc()
    node.current_prompt_artifact_version_id = version.id
    node.output_json = output
    node.status = WorkflowNodeStatus.SUCCEEDED
    node.failure_reason = None
    node.last_run_at = now
    node_run.status = WorkflowNodeStatus.SUCCEEDED
    node_run.output_json = output
    node_run.finished_at = now
    run.status = WorkflowRunStatus.SUCCEEDED
    run.failure_reason = None
    run.finished_at = now
    run.workflow.updated_at = now
    session.commit()


def _generate_workflow_image(
    provider: ImageProvider,
    request: WorkflowImageRequest,
) -> WorkflowImageResult:
    try:
        return provider.generate_workflow_image(request)
    except WorkflowSafeExecutionError:
        raise
    except Exception as exc:
        raise WorkflowSafeExecutionError(
            "图片生成失败，请稍后重试",
            retryable=True,
            retry_hint="retry_later",
            failure_category="provider_failure",
        ) from exc


def _validate_workflow_image_result(result: WorkflowImageResult) -> WorkflowGeneratedImage:
    if len(result.images) != 1:
        raise WorkflowSafeExecutionError(
            "schema-v2 单图节点要求 provider 恰好返回一张图片",
            retryable=False,
            retry_hint="revise_input",
            failure_category="provider_contract",
        )
    return result.images[0]


def _persist_image_result(
    session: Session,
    *,
    prepared: PreparedImageGeneration,
    result: WorkflowImageResult,
    generated_image: WorkflowGeneratedImage,
    provider_name: str,
    storage: LocalStorage,
    storage_writes: StorageWriteCompensation,
) -> None:
    node_run = session.scalar(
        select(WorkflowNodeRun).where(WorkflowNodeRun.id == prepared.node_run_id).with_for_update()
    )
    run = session.scalar(select(WorkflowRun).where(WorkflowRun.id == prepared.run_id).with_for_update())
    node = session.scalar(select(WorkflowNode).where(WorkflowNode.id == prepared.node_id).with_for_update())
    workflow = session.scalar(
        select(ProductWorkflow).where(ProductWorkflow.id == prepared.workflow_id).with_for_update()
    )
    product = session.scalar(select(Product).where(Product.id == prepared.product_id).with_for_update())
    if node_run is None or run is None or node is None or workflow is None or product is None:
        raise ConflictError("图片节点运行状态已不存在")
    if run.status == WorkflowRunStatus.CANCELLED:
        session.rollback()
        return
    if run.status != WorkflowRunStatus.RUNNING or node_run.status != WorkflowNodeStatus.RUNNING:
        raise ConflictError("图片节点运行状态已变化")
    if workflow.visual_system_version_id != prepared.visual_system_version_id:
        raise ConflictError("图片节点 VisualSystemVersion 已变化，拒绝保存过期结果")
    current_prompt_node_id = session.scalar(
        select(WorkflowNode.id).where(
            WorkflowNode.workflow_id == workflow.id,
            WorkflowNode.current_prompt_artifact_version_id == prepared.prompt_artifact_version_id,
        )
    )
    if current_prompt_node_id is None:
        raise ConflictError("图片节点上游 Prompt Artifact current version 已变化")
    try:
        current_generation_spec = GenerationSpec.model_validate(node.config_json.get("generation_spec"))
    except ValidationError as exc:
        raise ConflictError("图片节点 GenerationSpec 已变为无效内容") from exc
    if current_generation_spec != prepared.request.generation_spec:
        raise ConflictError("图片节点 GenerationSpec 已变化，拒绝保存过期结果")
    current_references = _load_image_references(
        session,
        workflow=workflow,
        image_node=node,
        storage=storage,
    )
    if [reference.asset_id for reference in current_references] != [
        reference.asset_id for reference in prepared.request.references
    ]:
        raise ConflictError("图片节点上游参考资产已变化，拒绝保存过期结果")

    actual = inspect_image_bytes(generated_image.bytes_data)
    effective_parameters = _sanitize_provider_metadata(result.effective_parameters) or {}
    provider_request_json = _sanitize_provider_metadata(result.provider_request_json)
    provider_output_json = _sanitize_provider_metadata(result.provider_output_json)
    extension = infer_extension(actual.mime_type)
    asset = stage_product_image_asset(
        session,
        product=product,
        content=generated_image.bytes_data,
        filename=f"{prepared.image_plan_key}{extension}",
        expected_mime_type=actual.mime_type,
        display_name=node.title,
        origin_type=ProductImageOriginType.WORKFLOW_GENERATION,
        storage=storage,
        storage_writes=storage_writes,
    )
    session.flush()
    actual_media = {
        "mime_type": actual.mime_type,
        "width": actual.width,
        "height": actual.height,
        "byte_size": actual.byte_size,
        "sha256": actual.sha256,
    }
    record = WorkflowImageGenerationRecord(
        workflow_node_run_id=node_run.id,
        product_id=product.id,
        workflow_id=workflow.id,
        node_id=node.id,
        result_asset_id=asset.id,
        visual_system_version_id=prepared.visual_system_version_id,
        prompt_artifact_version_id=prepared.prompt_artifact_version_id,
        requested_spec_json=prepared.request.generation_spec.model_dump(mode="json"),
        effective_parameters_json=effective_parameters,
        actual_media_json=actual_media,
        compiled_prompt=prepared.compiled_prompt,
        compiled_prompt_hash=prepared.compiled_prompt_hash,
        provider_name=provider_name,
        provider_model=result.model,
        provider_response_id=result.provider_response_id,
        provider_status=result.provider_status,
        provider_request_json=provider_request_json,
        provider_output_json=provider_output_json,
    )
    session.add(record)
    session.flush()
    for position, reference in enumerate(prepared.request.references):
        session.add(
            WorkflowImageGenerationReference(
                generation_record_id=record.id,
                asset_id=reference.asset_id,
                role=reference.role,
                label=reference.label,
                position=position,
            )
        )

    output = {
        "contract_version": 2,
        "generation_record_id": record.id,
        "result_asset_id": asset.id,
        "requested_spec": record.requested_spec_json,
        "effective_parameters": record.effective_parameters_json,
        "actual_media": actual_media,
    }
    now = now_utc()
    node.bound_image_asset_id = asset.id
    node.output_json = output
    node.status = WorkflowNodeStatus.SUCCEEDED
    node.failure_reason = None
    node.last_run_at = now
    node_run.status = WorkflowNodeStatus.SUCCEEDED
    node_run.output_json = output
    node_run.finished_at = now
    run.status = WorkflowRunStatus.SUCCEEDED
    run.failure_reason = None
    run.finished_at = now
    workflow.updated_at = now
    product.updated_at = now
    session.commit()


def _sanitize_provider_metadata(value: dict[str, Any] | None) -> dict[str, Any] | None:
    if value is None:
        return None

    def sanitize(item: Any, *, key: str | None = None) -> Any:
        if isinstance(item, bytes):
            raise WorkflowSafeExecutionError(
                "图片 provider 元数据不能包含原始 bytes",
                retryable=False,
                retry_hint="revise_input",
                failure_category="provider_contract",
            )
        if isinstance(item, str):
            if key in {"image_url", "result", "b64_json"} or item.startswith("data:image/"):
                return "<inline image omitted>"
            return item
        if isinstance(item, list):
            return [sanitize(child) for child in item]
        if isinstance(item, dict):
            return {str(child_key): sanitize(child, key=str(child_key)) for child_key, child in item.items()}
        if item is None or isinstance(item, int | float | bool):
            return item
        raise WorkflowSafeExecutionError(
            "图片 provider 元数据包含不可持久化值",
            retryable=False,
            retry_hint="revise_input",
            failure_category="provider_contract",
        )

    sanitized = sanitize(value)
    if not isinstance(sanitized, dict):
        raise WorkflowSafeExecutionError(
            "图片 provider 元数据必须是对象",
            retryable=False,
            retry_hint="revise_input",
            failure_category="provider_contract",
        )
    return sanitized


def _json_hash(payload: dict[str, Any]) -> str:
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


__all__ = ["execute_v2_workflow_node_run"]
