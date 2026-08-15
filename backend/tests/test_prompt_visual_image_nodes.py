from __future__ import annotations

import json
from copy import deepcopy
from datetime import UTC, datetime
from io import BytesIO
from pathlib import Path
from types import SimpleNamespace

import pytest
from helpers import _make_demo_image_bytes
from PIL import Image
from sqlalchemy import func, select
from workflow_draft_helpers import make_workflow_draft_payload

from productflow_backend.application.media_assets import clear_product_cover, delete_product_image_asset
from productflow_backend.application.product_workflow import execution as workflow_execution
from productflow_backend.application.product_workflow.execution import (
    execute_product_workflow_node_run,
    execute_product_workflow_run,
)
from productflow_backend.application.product_workflow.v2_execution import (
    execute_v2_workflow_node_run as execute_v2_workflow_node_only,
)
from productflow_backend.application.product_workflow.v2_reference_bindings import (
    bind_v2_reference_node_asset,
)
from productflow_backend.application.product_workflow.v2_runs import (
    cancel_v2_workflow_run,
    get_v2_workflow_node_run,
    get_v2_workflow_run,
    retry_v2_workflow_run,
    submit_v2_workflow_node_run,
    submit_v2_workflow_run,
)
from productflow_backend.application.product_workflow_dependencies import WorkflowExecutionDependencies
from productflow_backend.application.use_cases import (
    add_canonical_product_images,
    create_canonical_product,
    delete_product,
)
from productflow_backend.application.workflow_drafts.contracts import (
    GenerationSpec,
    ImagePromptPayloadV1,
    VisualSystemDraftPayload,
)
from productflow_backend.application.workflow_drafts.materialization import materialize_workflow_draft
from productflow_backend.application.workflow_drafts.service import (
    confirm_workflow_draft_revision,
    create_workflow_draft,
)
from productflow_backend.domain.enums import WorkflowNodeStatus, WorkflowNodeType, WorkflowRunStatus
from productflow_backend.domain.errors import ConflictError, NotFoundError, QueueUnavailableError
from productflow_backend.infrastructure.db.models import (
    ImagePromptArtifactVersion,
    ImagePromptArtifactVersionReference,
    Product,
    ProductImageAsset,
    ProductWorkflow,
    VisualSystemVersion,
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
)
from productflow_backend.infrastructure.image.gemini_provider import (
    GeminiImageResult,
    GoogleGeminiImageClient,
    GoogleGeminiImageProvider,
)
from productflow_backend.infrastructure.image.images_provider import (
    ImagesAPIResult,
    OpenAIImagesClient,
    OpenAIImagesImageProvider,
)
from productflow_backend.infrastructure.image.responses_provider import (
    OpenAIResponsesImageProvider,
    ResponsesImageResult,
)
from productflow_backend.infrastructure.prompt.base import (
    PromptGenerationProvider,
    PromptGenerationRequest,
    PromptGenerationResult,
    PromptReferenceImage,
)
from productflow_backend.infrastructure.prompt.openai_provider import OpenAIPromptGenerationProvider
from productflow_backend.infrastructure.provider_config import (
    ResolvedImageProviderConfig,
    ResolvedPromptProviderConfig,
)


class RecordingPromptProvider(PromptGenerationProvider):
    provider_name = "recording"

    def __init__(
        self,
        *,
        invalid_image_order: bool = False,
        include_all_reference_evidence: bool = False,
    ) -> None:
        self.invalid_image_order = invalid_image_order
        self.include_all_reference_evidence = include_all_reference_evidence
        self.requests: list[PromptGenerationRequest] = []

    def generate_prompt(self, request: PromptGenerationRequest) -> PromptGenerationResult:
        self.requests.append(request)
        images = list(request.current_prompt.images)
        if self.invalid_image_order:
            images.reverse()
        updates = {
            "design_goal": "由多模态提示词节点重新生成",
            "images": images,
        }
        if self.include_all_reference_evidence:
            updates["evidence_asset_ids"] = [reference.asset_id for reference in request.reference_images]
        payload = request.current_prompt.model_copy(update=updates)
        return PromptGenerationResult(payload=payload, model="recording-prompt-v1", response_id="resp-prompt-1")


class RecordingImageProvider(ImageProvider):
    provider_name = "recording-image"

    def __init__(self, *, image_bytes: bytes, image_count: int = 1, model: str = "recording-image-v1") -> None:
        self.image_bytes = image_bytes
        self.image_count = image_count
        self.model = model
        self.requests: list[WorkflowImageRequest] = []

    def generate_workflow_image(self, request: WorkflowImageRequest) -> WorkflowImageResult:
        self.requests.append(request)
        return WorkflowImageResult(
            images=tuple(
                WorkflowGeneratedImage(bytes_data=self.image_bytes, mime_type="image/jpeg")
                for _ in range(self.image_count)
            ),
            model=self.model,
            provider_response_id="resp-image-1",
            provider_status="completed",
            effective_parameters={
                "adapter": "recording",
                "sent_quality": request.generation_spec.quality_intent,
                "reference_image_count": len(request.references),
            },
            provider_request_json={
                "prompt": request.compiled_prompt,
                "input": [{"image_url": "data:image/png;base64,secret"}],
            },
            provider_output_json={"status": "completed", "result": "raw-base64-value"},
        )


def _png_bytes(*, color: tuple[int, int, int], size: tuple[int, int]) -> bytes:
    image = Image.new("RGB", size, color)
    buffer = BytesIO()
    image.save(buffer, format="PNG")
    return buffer.getvalue()


def _add_scene_before_hero(payload: dict) -> dict:
    payload = deepcopy(payload)
    hero_type = payload["image_types"][0]
    hero_type["order"] = 1
    hero_type["quantity"] = 1
    hero_type["images"] = hero_type["images"][:1]
    hero_prompt = payload["prompt_plans"][0]
    hero_prompt["payload"]["images"] = hero_prompt["payload"]["images"][:1]
    payload["nodes"] = [
        node for node in payload["nodes"] if node.get("image_plan_key") != "hero-2"
    ]
    payload["edges"] = [
        edge
        for edge in payload["edges"]
        if edge["source_node_key"] != "hero-image-2-node" and edge["target_node_key"] != "hero-image-2-node"
    ]

    scene_image = deepcopy(hero_type["images"][0])
    scene_image.update(key="scene-1", order=0, variation_instruction="工业车间使用场景")
    scene_type = {
        "key": "scene",
        "title": "场景展示图",
        "order": 0,
        "quantity": 1,
        "prompt_plan_key": "scene-prompt",
        "images": [scene_image],
    }
    scene_prompt = deepcopy(hero_prompt)
    scene_prompt.update(key="scene-prompt", image_type_key="scene", title="场景展示提示词")
    scene_prompt["payload"]["images"][0]["image_plan_key"] = "scene-1"
    scene_prompt["payload"]["images"][0]["instruction"] = "工业车间使用场景"
    payload["image_types"].append(scene_type)
    payload["prompt_plans"].append(scene_prompt)
    payload["nodes"].extend(
        [
            {
                "key": "scene-prompt-node",
                "node_type": "prompt_generation",
                "title": "场景展示提示词",
                "position_x": 680,
                "position_y": 460,
                "folder_key": "hero-folder",
                "prompt_plan_key": "scene-prompt",
            },
            {
                "key": "scene-image-node",
                "node_type": "image_generation",
                "title": "场景展示图 1",
                "position_x": 980,
                "position_y": 660,
                "folder_key": "hero-folder",
                "image_plan_key": "scene-1",
            },
        ]
    )
    payload["edges"].extend(
        [
            {
                "key": "context-to-scene-prompt",
                "source_node_key": "product-context",
                "target_node_key": "scene-prompt-node",
                "source_handle": "facts",
                "target_handle": "facts",
            },
            {
                "key": "reference-to-scene-prompt",
                "source_node_key": "product-reference-node",
                "target_node_key": "scene-prompt-node",
                "source_handle": "asset",
                "target_handle": "reference",
            },
            {
                "key": "scene-prompt-to-image",
                "source_node_key": "scene-prompt-node",
                "target_node_key": "scene-image-node",
                "source_handle": "prompt",
                "target_handle": "prompt",
            },
        ]
    )
    return payload


def _create_materialized_workflow(db_session, *, include_scene_before_hero: bool = False):
    product = create_canonical_product(
        db_session,
        name="硬质刀具收纳套装",
        category="工业收纳",
        price="299.00",
        source_note="五款收纳盘，橙蓝配色。",
        image_uploads=[(_make_demo_image_bytes(), "product.png", "image/png")],
    )
    reference_asset_id = product.image_assets[0].id
    payload = make_workflow_draft_payload(reference_asset_id=reference_asset_id)
    if include_scene_before_hero:
        payload = _add_scene_before_hero(payload)
    draft = create_workflow_draft(
        db_session,
        product_id=product.id,
        payload=payload,
        ready_for_confirmation=True,
        source_turn_id="turn-prompt",
        source_artifact_step_id="artifact-prompt",
    )
    confirmed = confirm_workflow_draft_revision(
        db_session,
        product_id=product.id,
        draft_id=draft.id,
        expected_draft_version=1,
    )
    result = materialize_workflow_draft(
        db_session,
        product_id=product.id,
        draft_id=confirmed.id,
        expected_draft_version=1,
        expected_workflow_revision=0,
        idempotency_key="prompt-runtime",
    )
    return product, result.workflow


def _queue_single_node_run(db_session, *, workflow, node):
    run = WorkflowRun(workflow_id=workflow.id, status=WorkflowRunStatus.RUNNING)
    db_session.add(run)
    db_session.flush()
    node.status = WorkflowNodeStatus.QUEUED
    node_run = WorkflowNodeRun(
        workflow_run_id=run.id,
        node_id=node.id,
        status=WorkflowNodeStatus.QUEUED,
    )
    db_session.add(node_run)
    db_session.commit()
    return run, node_run


def execute_v2_workflow_node_run(db_session, *, node_run_id, dependencies=None, storage=None):
    """Run one v2 node and let the shared scheduler own WorkflowRun finalization."""

    changed = execute_v2_workflow_node_only(
        db_session,
        node_run_id=node_run_id,
        dependencies=dependencies,
        storage=storage,
    )
    if not changed:
        return
    node_run = db_session.get(WorkflowNodeRun, node_run_id)
    assert node_run is not None
    workflow_execution._execute_product_workflow_run(
        db_session,
        run_id=node_run.workflow_run_id,
        enqueue_node_run=lambda _: pytest.fail("terminal single-node run must not dispatch another node"),
        return_after_dispatch=False,
    )


def test_openai_prompt_provider_sends_native_multimodal_content_parts() -> None:
    artifact = make_workflow_draft_payload()
    current_prompt = ImagePromptPayloadV1.model_validate(artifact["prompt_plans"][0]["payload"])
    request = PromptGenerationRequest(
        image_type_key="hero",
        image_plan_keys=("hero-1", "hero-2"),
        facts=tuple(artifact["facts"]),
        visual_system=VisualSystemDraftPayload.model_validate(artifact["visual_system"]["payload"]),
        visual_exceptions=(),
        current_prompt=current_prompt,
        text_languages=("zh-CN",),
        reference_images=(
            PromptReferenceImage(
                asset_id=artifact["reference_bindings"][0]["asset_id"],
                role="product_identity",
                label="商品参考图",
                filename="product.png",
                mime_type="image/png",
                image_bytes=_make_demo_image_bytes(),
            ),
        ),
    )
    captured: dict[str, object] = {}

    class FakeResponses:
        def parse(self, **kwargs):
            captured.update(kwargs)
            return SimpleNamespace(id="resp-openai-prompt", output_parsed=current_prompt)

    provider = OpenAIPromptGenerationProvider(
        ResolvedPromptProviderConfig(
            provider_kind="openai",
            model="prompt-model",
            api_key="test-key",
        )
    )
    provider.client = SimpleNamespace(responses=FakeResponses())

    result = provider.generate_prompt(request)

    assert captured["model"] == "prompt-model"
    assert captured["text_format"] is ImagePromptPayloadV1
    input_messages = captured["input"]
    assert isinstance(input_messages, list)
    content = input_messages[0]["content"]
    assert [part["type"] for part in content] == ["input_text", "input_text", "input_image"]
    assert content[2]["image_url"].startswith("data:image/png;base64,")
    assert request.reference_images[0].asset_id in content[0]["text"]
    assert request.reference_images[0].asset_id in content[1]["text"]
    assert result.payload == current_prompt
    assert result.model == "prompt-model"
    assert result.response_id == "resp-openai-prompt"


def test_prompt_node_appends_a_new_artifact_version(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    prompt_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.PROMPT_GENERATION)
    initial_version_id = prompt_node.current_prompt_artifact_version_id
    assert initial_version_id is not None
    run, node_run = _queue_single_node_run(db_session, workflow=workflow, node=prompt_node)
    provider = RecordingPromptProvider()

    execute_v2_workflow_node_run(
        db_session,
        node_run_id=node_run.id,
        dependencies=WorkflowExecutionDependencies(
            prompt_generation_provider_resolver=lambda: provider,
        ),
    )

    db_session.expire_all()
    persisted_run = db_session.get(WorkflowRun, run.id)
    persisted_node_run = db_session.get(WorkflowNodeRun, node_run.id)
    persisted_node = db_session.get(type(prompt_node), prompt_node.id)
    assert persisted_run is not None and persisted_run.status == WorkflowRunStatus.SUCCEEDED
    assert persisted_node_run is not None and persisted_node_run.status == WorkflowNodeStatus.SUCCEEDED
    assert persisted_node is not None and persisted_node.status == WorkflowNodeStatus.SUCCEEDED
    assert persisted_node.current_prompt_artifact_version_id != initial_version_id
    assert len(provider.requests) == 1
    provider_request = provider.requests[0]
    assert provider_request.image_plan_keys == ("hero-1", "hero-2")
    assert [fact["key"] for fact in provider_request.facts] == ["product_name"]
    assert [reference.asset_id for reference in provider_request.reference_images] == [product.image_assets[0].id]
    assert provider_request.reference_images[0].image_bytes == _make_demo_image_bytes()

    versions = list(
        db_session.scalars(
            select(ImagePromptArtifactVersion)
            .where(ImagePromptArtifactVersion.artifact_id == persisted_node.current_prompt_artifact_version.artifact_id)
            .order_by(ImagePromptArtifactVersion.version)
        )
    )
    assert [version.version for version in versions] == [1, 2]
    assert versions[0].id == initial_version_id
    assert versions[1].source_node_run_id == node_run.id
    assert versions[1].provider_name == "recording"
    assert versions[1].provider_model == "recording-prompt-v1"
    assert versions[1].provider_response_id == "resp-prompt-1"
    assert versions[1].payload_json["design_goal"] == "由多模态提示词节点重新生成"
    persisted_text = json.dumps(
        {
            "payload": versions[1].payload_json,
            "node_output": persisted_node.output_json,
            "run_output": persisted_node_run.output_json,
        },
        ensure_ascii=False,
    )
    assert "data:image/" not in persisted_text
    assert ";base64," not in persisted_text


def test_prompt_node_reads_reused_visual_system_references_across_products(db_session) -> None:
    source_product, source_workflow = _create_materialized_workflow(db_session)
    visual_version_id = source_workflow.visual_system_version_id
    assert visual_version_id is not None
    source_reference_id = source_product.image_assets[0].id

    consumer_product = create_canonical_product(
        db_session,
        name="视觉体系使用商品",
        category="工业收纳",
        price="199.00",
        source_note="复用视觉体系",
        image_uploads=[(_make_demo_image_bytes(), "consumer.png", "image/png")],
    )
    consumer_reference_id = consumer_product.image_assets[0].id
    payload = make_workflow_draft_payload(reference_asset_id=consumer_reference_id)
    payload["visual_system"] = {
        "mode": "confirmed_version",
        "version_id": visual_version_id,
    }
    draft = create_workflow_draft(
        db_session,
        product_id=consumer_product.id,
        payload=payload,
        ready_for_confirmation=True,
    )
    confirm_workflow_draft_revision(
        db_session,
        product_id=consumer_product.id,
        draft_id=draft.id,
        expected_draft_version=1,
    )
    materialized = materialize_workflow_draft(
        db_session,
        product_id=consumer_product.id,
        draft_id=draft.id,
        expected_draft_version=1,
        expected_workflow_revision=0,
        idempotency_key="reused-visual-system",
    )
    prompt_node = next(
        node for node in materialized.workflow.nodes if node.node_type == WorkflowNodeType.PROMPT_GENERATION
    )
    _, node_run = _queue_single_node_run(
        db_session,
        workflow=materialized.workflow,
        node=prompt_node,
    )
    provider = RecordingPromptProvider(include_all_reference_evidence=True)

    execute_v2_workflow_node_run(
        db_session,
        node_run_id=node_run.id,
        dependencies=WorkflowExecutionDependencies(
            prompt_generation_provider_resolver=lambda: provider,
        ),
    )

    assert len(provider.requests) == 1
    assert [reference.asset_id for reference in provider.requests[0].reference_images] == [
        consumer_reference_id,
        source_reference_id,
    ]

    db_session.expire_all()
    persisted_prompt_node = db_session.get(WorkflowNode, prompt_node.id)
    assert persisted_prompt_node is not None
    _, second_node_run = _queue_single_node_run(
        db_session,
        workflow=materialized.workflow,
        node=persisted_prompt_node,
    )
    second_provider = RecordingPromptProvider()
    execute_v2_workflow_node_run(
        db_session,
        node_run_id=second_node_run.id,
        dependencies=WorkflowExecutionDependencies(
            prompt_generation_provider_resolver=lambda: second_provider,
        ),
    )

    assert len(second_provider.requests) == 1
    assert [reference.asset_id for reference in second_provider.requests[0].reference_images] == [
        consumer_reference_id,
        source_reference_id,
    ]


def test_prompt_node_rejects_provider_plan_drift_without_switching_current_version(db_session) -> None:
    _, workflow = _create_materialized_workflow(db_session)
    prompt_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.PROMPT_GENERATION)
    initial_version_id = prompt_node.current_prompt_artifact_version_id
    run, node_run = _queue_single_node_run(db_session, workflow=workflow, node=prompt_node)

    execute_v2_workflow_node_run(
        db_session,
        node_run_id=node_run.id,
        dependencies=WorkflowExecutionDependencies(
            prompt_generation_provider_resolver=lambda: RecordingPromptProvider(invalid_image_order=True),
        ),
    )

    db_session.expire_all()
    persisted_run = db_session.get(WorkflowRun, run.id)
    persisted_node_run = db_session.get(WorkflowNodeRun, node_run.id)
    persisted_node = db_session.get(type(prompt_node), prompt_node.id)
    assert persisted_run is not None and persisted_run.status == WorkflowRunStatus.FAILED
    assert persisted_run.is_retryable is False
    assert persisted_node_run is not None and persisted_node_run.status == WorkflowNodeStatus.FAILED
    assert persisted_node is not None
    assert persisted_node.current_prompt_artifact_version_id == initial_version_id
    assert db_session.scalar(select(func.count()).select_from(ImagePromptArtifactVersion)) == 1


def test_v2_node_run_submission_is_durable_and_idempotent(db_session) -> None:
    _, workflow = _create_materialized_workflow(db_session)
    prompt_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.PROMPT_GENERATION)
    enqueued_run_ids: list[str] = []

    first = submit_v2_workflow_node_run(
        db_session,
        node_id=prompt_node.id,
        enqueue=enqueued_run_ids.append,
    )
    second = submit_v2_workflow_node_run(
        db_session,
        node_id=prompt_node.id,
        enqueue=enqueued_run_ids.append,
    )

    assert first.created is True
    assert second.created is False
    assert second.node_run.id == first.node_run.id
    assert enqueued_run_ids == [first.node_run.workflow_run_id]
    assert db_session.scalar(select(func.count()).select_from(WorkflowRun)) == 1
    assert db_session.scalar(select(func.count()).select_from(WorkflowNodeRun)) == 1
    queried = get_v2_workflow_node_run(db_session, node_run_id=first.node_run.id)
    assert queried.status == WorkflowNodeStatus.QUEUED
    assert queried.workflow_run.status == WorkflowRunStatus.RUNNING

    provider = RecordingPromptProvider()
    execute_product_workflow_node_run(
        first.node_run.id,
        dependencies=WorkflowExecutionDependencies(prompt_generation_provider_resolver=lambda: provider),
    )
    execute_product_workflow_run(first.node_run.workflow_run_id)

    db_session.expire_all()
    completed = get_v2_workflow_node_run(db_session, node_run_id=first.node_run.id)
    assert completed.status == WorkflowNodeStatus.SUCCEEDED
    assert completed.workflow_run.status == WorkflowRunStatus.SUCCEEDED
    assert completed.prompt_artifact_version is not None
    assert len(provider.requests) == 1


def test_v2_node_run_queue_failure_marks_run_and_node_failed(db_session) -> None:
    _, workflow = _create_materialized_workflow(db_session)
    prompt_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.PROMPT_GENERATION)

    def unavailable_queue(_: str) -> None:
        raise RuntimeError("redis unavailable")

    with pytest.raises(QueueUnavailableError, match="任务队列暂不可用"):
        submit_v2_workflow_node_run(
            db_session,
            node_id=prompt_node.id,
            enqueue=unavailable_queue,
        )

    db_session.expire_all()
    run = db_session.scalar(select(WorkflowRun))
    node_run = db_session.scalar(select(WorkflowNodeRun))
    persisted_node = db_session.get(WorkflowNode, prompt_node.id)
    assert run is not None and run.status == WorkflowRunStatus.FAILED
    assert node_run is not None and node_run.status == WorkflowNodeStatus.FAILED
    assert persisted_node is not None and persisted_node.status == WorkflowNodeStatus.FAILED
    assert run.failure_reason == "任务队列暂不可用，请稍后重试"
    assert node_run.failure_reason == run.failure_reason


def test_v2_workflow_scheduler_dispatches_single_queued_node_run(db_session, monkeypatch) -> None:
    _, workflow = _create_materialized_workflow(db_session)
    prompt_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.PROMPT_GENERATION)
    submission = submit_v2_workflow_node_run(db_session, node_id=prompt_node.id, enqueue=lambda _: None)
    enqueued_node_run_ids: list[str] = []
    monkeypatch.setattr(workflow_execution, "enqueue_workflow_node_run", enqueued_node_run_ids.append)

    execute_product_workflow_run(submission.node_run.workflow_run_id)

    assert enqueued_node_run_ids == [submission.node_run.id]
    db_session.expire_all()
    queued = get_v2_workflow_node_run(db_session, node_run_id=submission.node_run.id)
    assert queued.status == WorkflowNodeStatus.QUEUED
    assert queued.workflow_run.status == WorkflowRunStatus.RUNNING


def test_v2_workflow_scheduler_marks_queue_delivery_failure_on_run_and_node(db_session, monkeypatch) -> None:
    _, workflow = _create_materialized_workflow(db_session)
    prompt_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.PROMPT_GENERATION)
    submission = submit_v2_workflow_node_run(db_session, node_id=prompt_node.id, enqueue=lambda _: None)

    def unavailable_queue(_: str) -> None:
        raise RuntimeError("redis unavailable during recovery")

    monkeypatch.setattr(workflow_execution, "enqueue_workflow_node_run", unavailable_queue)
    execute_product_workflow_run(submission.node_run.workflow_run_id)

    db_session.expire_all()
    failed = get_v2_workflow_node_run(db_session, node_run_id=submission.node_run.id)
    assert failed.status == WorkflowNodeStatus.FAILED
    assert failed.failure_reason == "任务队列暂不可用，请稍后重试"
    assert failed.workflow_run.status == WorkflowRunStatus.FAILED
    assert failed.workflow_run.failure_reason == failed.failure_reason


def test_v2_full_workflow_submission_is_one_durable_run_and_rejects_partial_overlap(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    enqueued_run_ids: list[str] = []

    first = submit_v2_workflow_run(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        enqueue=enqueued_run_ids.append,
    )
    duplicate = submit_v2_workflow_run(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        enqueue=enqueued_run_ids.append,
    )

    runnable_node_ids = {
        node.id
        for node in workflow.nodes
        if node.node_type in {WorkflowNodeType.PROMPT_GENERATION, WorkflowNodeType.IMAGE_GENERATION}
    }
    assert first.created is True
    assert duplicate.created is False
    assert duplicate.run.id == first.run.id
    assert enqueued_run_ids == [first.run.id]
    assert {node_run.node_id for node_run in first.run.node_runs} == runnable_node_ids
    assert db_session.scalar(select(func.count()).select_from(WorkflowRun)) == 1

    for node_run in first.run.node_runs:
        node_run.status = WorkflowNodeStatus.SUCCEEDED
    db_session.commit()
    duplicate_after_nodes_finished = submit_v2_workflow_run(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        enqueue=enqueued_run_ids.append,
    )
    assert duplicate_after_nodes_finished.created is False
    assert duplicate_after_nodes_finished.run.id == first.run.id
    with pytest.raises(ConflictError, match="相关节点已有运行中的任务"):
        submit_v2_workflow_node_run(
            db_session,
            node_id=next(iter(runnable_node_ids)),
            enqueue=lambda _: None,
        )

    cancel_v2_workflow_run(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        run_id=first.run.id,
    )
    prompt_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.PROMPT_GENERATION)
    partial = submit_v2_workflow_node_run(db_session, node_id=prompt_node.id, enqueue=lambda _: None)
    with pytest.raises(ConflictError, match="相关节点已有运行中的任务"):
        submit_v2_workflow_run(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            enqueue=lambda _: None,
        )
    cancel_v2_workflow_run(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        run_id=partial.node_run.workflow_run_id,
    )


def test_v2_full_workflow_scheduler_waits_for_prompt_and_dispatches_images_in_parallel(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    submission = submit_v2_workflow_run(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        enqueue=lambda _: None,
    )
    node_runs_by_node_id = {node_run.node_id: node_run for node_run in submission.run.node_runs}
    prompt_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.PROMPT_GENERATION)
    image_nodes = [node for node in workflow.nodes if node.node_type == WorkflowNodeType.IMAGE_GENERATION]
    dispatched: list[str] = []

    workflow_execution._execute_product_workflow_run(
        db_session,
        run_id=submission.run.id,
        enqueue_node_run=dispatched.append,
    )
    assert dispatched == [node_runs_by_node_id[prompt_node.id].id]

    prompt_provider = RecordingPromptProvider()
    execute_v2_workflow_node_only(
        db_session,
        node_run_id=node_runs_by_node_id[prompt_node.id].id,
        dependencies=WorkflowExecutionDependencies(
            prompt_generation_provider_resolver=lambda: prompt_provider,
        ),
    )
    db_session.expire_all()
    db_session.refresh(submission.run)
    assert submission.run.status == WorkflowRunStatus.RUNNING

    dispatched.clear()
    workflow_execution._execute_product_workflow_run(
        db_session,
        run_id=submission.run.id,
        enqueue_node_run=dispatched.append,
    )
    assert set(dispatched) == {node_runs_by_node_id[node.id].id for node in image_nodes}

    image_provider = RecordingImageProvider(image_bytes=_png_bytes(color=(80, 120, 180), size=(80, 64)))
    for image_node in image_nodes:
        execute_v2_workflow_node_only(
            db_session,
            node_run_id=node_runs_by_node_id[image_node.id].id,
            dependencies=WorkflowExecutionDependencies(image_provider_resolver=lambda: image_provider),
        )
    workflow_execution._execute_product_workflow_run(
        db_session,
        run_id=submission.run.id,
        enqueue_node_run=lambda _: pytest.fail("completed workflow must not dispatch another node"),
    )

    persisted = get_v2_workflow_run(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        run_id=submission.run.id,
    )
    assert persisted.status == WorkflowRunStatus.SUCCEEDED
    assert all(node_run.status == WorkflowNodeStatus.SUCCEEDED for node_run in persisted.node_runs)
    assert len(prompt_provider.requests) == 1
    assert len(image_provider.requests) == len(image_nodes)


def test_v2_full_workflow_scheduler_preserves_image_to_image_order(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    image_nodes = sorted(
        (node for node in workflow.nodes if node.node_type == WorkflowNodeType.IMAGE_GENERATION),
        key=lambda node: node.config_json["image_plan_key"],
    )
    upstream_image, downstream_image = image_nodes
    db_session.add(
        WorkflowEdge(
            workflow_id=workflow.id,
            edge_key="generated-image-reference",
            source_node_id=upstream_image.id,
            target_node_id=downstream_image.id,
            source_handle="image",
            target_handle="reference",
        )
    )
    db_session.commit()
    db_session.expire_all()
    workflow = db_session.get(ProductWorkflow, workflow.id)
    submission = submit_v2_workflow_run(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        enqueue=lambda _: None,
    )
    node_runs = {node_run.node_id: node_run for node_run in submission.run.node_runs}
    prompt_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.PROMPT_GENERATION)

    first_dispatch: list[str] = []
    workflow_execution._execute_product_workflow_run(
        db_session,
        run_id=submission.run.id,
        enqueue_node_run=first_dispatch.append,
    )
    assert first_dispatch == [node_runs[prompt_node.id].id]
    execute_v2_workflow_node_only(
        db_session,
        node_run_id=node_runs[prompt_node.id].id,
        dependencies=WorkflowExecutionDependencies(
            prompt_generation_provider_resolver=lambda: RecordingPromptProvider(),
        ),
    )
    db_session.expire_all()

    second_dispatch: list[str] = []
    workflow_execution._execute_product_workflow_run(
        db_session,
        run_id=submission.run.id,
        enqueue_node_run=second_dispatch.append,
    )
    assert second_dispatch == [node_runs[upstream_image.id].id]
    image_provider = RecordingImageProvider(image_bytes=_png_bytes(color=(40, 80, 160), size=(72, 72)))
    execute_v2_workflow_node_only(
        db_session,
        node_run_id=node_runs[upstream_image.id].id,
        dependencies=WorkflowExecutionDependencies(image_provider_resolver=lambda: image_provider),
    )
    db_session.expire_all()

    third_dispatch: list[str] = []
    workflow_execution._execute_product_workflow_run(
        db_session,
        run_id=submission.run.id,
        enqueue_node_run=third_dispatch.append,
    )
    assert third_dispatch == [node_runs[downstream_image.id].id]
    execute_v2_workflow_node_only(
        db_session,
        node_run_id=node_runs[downstream_image.id].id,
        dependencies=WorkflowExecutionDependencies(image_provider_resolver=lambda: image_provider),
    )
    workflow_execution._execute_product_workflow_run(
        db_session,
        run_id=submission.run.id,
        enqueue_node_run=lambda _: pytest.fail("completed workflow must not dispatch another node"),
    )

    db_session.expire_all()
    persisted_downstream = db_session.get(WorkflowNode, downstream_image.id)
    persisted_upstream = db_session.get(WorkflowNode, upstream_image.id)
    assert db_session.get(WorkflowRun, submission.run.id).status == WorkflowRunStatus.SUCCEEDED
    assert persisted_upstream.bound_image_asset_id in {
        reference.asset_id for reference in image_provider.requests[1].references
    }
    assert persisted_downstream.bound_image_asset_id is not None


def test_v2_full_workflow_failure_blocks_dependents_and_retry_excludes_succeeded_branch(db_session) -> None:
    from productflow_backend.application.product_workflow.run_state import mark_workflow_node_run_failed

    product, workflow = _create_materialized_workflow(db_session, include_scene_before_hero=True)
    submission = submit_v2_workflow_run(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        enqueue=lambda _: None,
    )
    node_runs = {node_run.node_id: node_run for node_run in submission.run.node_runs}
    prompt_nodes = {
        node.config_json["image_type_key"]: node
        for node in workflow.nodes
        if node.node_type == WorkflowNodeType.PROMPT_GENERATION
    }
    image_nodes = {
        node.config_json["image_type_key"]: node
        for node in workflow.nodes
        if node.node_type == WorkflowNodeType.IMAGE_GENERATION
    }
    initial_dispatch: list[str] = []
    workflow_execution._execute_product_workflow_run(
        db_session,
        run_id=submission.run.id,
        enqueue_node_run=initial_dispatch.append,
    )
    assert set(initial_dispatch) == {
        node_runs[prompt_nodes["hero"].id].id,
        node_runs[prompt_nodes["scene"].id].id,
    }

    mark_workflow_node_run_failed(
        db_session,
        node_run_id=node_runs[prompt_nodes["hero"].id].id,
        reason="主图提示词生成失败",
    )
    execute_v2_workflow_node_only(
        db_session,
        node_run_id=node_runs[prompt_nodes["scene"].id].id,
        dependencies=WorkflowExecutionDependencies(
            prompt_generation_provider_resolver=lambda: RecordingPromptProvider(),
        ),
    )
    db_session.expire_all()
    next_dispatch: list[str] = []
    workflow_execution._execute_product_workflow_run(
        db_session,
        run_id=submission.run.id,
        enqueue_node_run=next_dispatch.append,
    )
    assert next_dispatch == [node_runs[image_nodes["scene"].id].id]
    execute_v2_workflow_node_only(
        db_session,
        node_run_id=node_runs[image_nodes["scene"].id].id,
        dependencies=WorkflowExecutionDependencies(
            image_provider_resolver=lambda: RecordingImageProvider(
                image_bytes=_png_bytes(color=(30, 90, 150), size=(64, 64))
            ),
        ),
    )
    workflow_execution._execute_product_workflow_run(
        db_session,
        run_id=submission.run.id,
        enqueue_node_run=lambda _: pytest.fail("failed workflow has no further ready node"),
    )

    failed = get_v2_workflow_run(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        run_id=submission.run.id,
    )
    assert failed.status == WorkflowRunStatus.FAILED
    assert failed.progress_metadata["run_scope"] == "workflow"
    assert node_runs[image_nodes["scene"].id].status == WorkflowNodeStatus.SUCCEEDED
    hero_image_node_ids = {
        node.id
        for node in workflow.nodes
        if node.node_type == WorkflowNodeType.IMAGE_GENERATION
        and node.config_json["image_type_key"] == "hero"
    }
    assert all(
        next(item for item in failed.node_runs if item.node_id == node_id).failure_reason == "上游节点失败"
        for node_id in hero_image_node_ids
    )

    retry = retry_v2_workflow_run(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        run_id=failed.id,
        enqueue=lambda _: None,
    )
    assert retry.created is True
    assert {node_run.node_id for node_run in retry.run.node_runs} == {
        prompt_nodes["hero"].id,
        *hero_image_node_ids,
    }
    assert retry.run.progress_metadata["source_run_id"] == failed.id
    assert retry.run.progress_metadata["manual_retry"] is True

    retry_prompt_run = next(
        node_run for node_run in retry.run.node_runs if node_run.node_id == prompt_nodes["hero"].id
    )
    mark_workflow_node_run_failed(
        db_session,
        node_run_id=retry_prompt_run.id,
        reason="重试仍然失败",
    )
    db_session.expire_all()
    persisted_retry = get_v2_workflow_run(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        run_id=retry.run.id,
    )
    assert persisted_retry.progress_metadata["source_run_id"] == failed.id
    assert persisted_retry.progress_metadata["manual_retry"] is True


def test_v2_full_workflow_cancel_and_durable_recovery_use_workflow_run(db_session, configured_env) -> None:
    from productflow_backend.application.durable_recovery import recover_unfinished_workflow_runs

    product, workflow = _create_materialized_workflow(db_session)
    submission = submit_v2_workflow_run(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        enqueue=lambda _: None,
    )
    recovered_run_ids: list[str] = []
    summary = recover_unfinished_workflow_runs(enqueue=recovered_run_ids.append)
    assert summary.queued_runs == 1
    assert recovered_run_ids == [submission.run.id]

    cancelled = cancel_v2_workflow_run(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        run_id=submission.run.id,
    )
    assert cancelled.status == WorkflowRunStatus.CANCELLED
    assert all(node_run.failure_reason == "已取消" for node_run in cancelled.node_runs)
    assert cancel_v2_workflow_run(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        run_id=submission.run.id,
    ).status == WorkflowRunStatus.CANCELLED


def test_image_node_creates_one_canonical_asset_and_preserves_rerun_history(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    clear_product_cover(db_session, product_id=product.id)
    image_node = next(
        node
        for node in workflow.nodes
        if node.node_type == WorkflowNodeType.IMAGE_GENERATION and node.config_json["image_plan_key"] == "hero-1"
    )
    first_provider = RecordingImageProvider(
        image_bytes=_png_bytes(color=(220, 40, 40), size=(80, 64)),
        model="provider-a",
    )
    first_run, first_node_run = _queue_single_node_run(db_session, workflow=workflow, node=image_node)

    execute_v2_workflow_node_run(
        db_session,
        node_run_id=first_node_run.id,
        dependencies=WorkflowExecutionDependencies(image_provider_resolver=lambda: first_provider),
    )

    db_session.expire_all()
    persisted_first_run = db_session.get(WorkflowRun, first_run.id)
    persisted_node = db_session.get(type(image_node), image_node.id)
    first_record = db_session.scalar(
        select(WorkflowImageGenerationRecord).where(
            WorkflowImageGenerationRecord.workflow_node_run_id == first_node_run.id
        )
    )
    assert persisted_first_run is not None and persisted_first_run.status == WorkflowRunStatus.SUCCEEDED
    assert persisted_node is not None and persisted_node.status == WorkflowNodeStatus.SUCCEEDED
    assert first_record is not None
    assert persisted_node.bound_image_asset_id == first_record.result_asset_id
    assert db_session.get(Product, product.id).cover_image_asset_id == first_record.result_asset_id
    assert db_session.get(ProductImageAsset, first_record.result_asset_id).image_type_key == "hero"
    assert first_record.requested_spec_json == image_node.config_json["generation_spec"]
    assert first_record.effective_parameters_json == {
        "adapter": "recording",
        "sent_quality": "high",
        "reference_image_count": 1,
    }
    assert first_record.actual_media_json["mime_type"] == "image/png"
    assert first_record.actual_media_json["width"] == 80
    assert first_record.actual_media_json["height"] == 64
    assert first_record.provider_model == "provider-a"
    assert first_record.provider_request_json["input"][0]["image_url"] == "<inline image omitted>"
    assert first_record.provider_output_json["result"] == "<inline image omitted>"
    assert len(first_record.references) == 1
    assert first_record.references[0].asset_id == product.image_assets[0].id
    assert len(first_provider.requests) == 1
    assert [reference.asset_id for reference in first_provider.requests[0].references] == [product.image_assets[0].id]
    assert "product_name" in first_record.compiled_prompt
    assert "hero-1" in first_record.compiled_prompt
    first_asset_id = first_record.result_asset_id

    second_provider = RecordingImageProvider(
        image_bytes=_png_bytes(color=(30, 90, 220), size=(96, 72)),
        model="provider-b",
    )
    second_run, second_node_run = _queue_single_node_run(db_session, workflow=workflow, node=persisted_node)
    execute_v2_workflow_node_run(
        db_session,
        node_run_id=second_node_run.id,
        dependencies=WorkflowExecutionDependencies(image_provider_resolver=lambda: second_provider),
    )

    db_session.expire_all()
    persisted_second_run = db_session.get(WorkflowRun, second_run.id)
    rerun_node = db_session.get(type(image_node), image_node.id)
    records = list(
        db_session.scalars(
            select(WorkflowImageGenerationRecord)
            .where(WorkflowImageGenerationRecord.node_id == image_node.id)
            .order_by(WorkflowImageGenerationRecord.created_at, WorkflowImageGenerationRecord.id)
        )
    )
    assert persisted_second_run is not None and persisted_second_run.status == WorkflowRunStatus.SUCCEEDED
    assert rerun_node is not None
    assert len(records) == 2
    assert rerun_node.bound_image_asset_id == records[1].result_asset_id
    assert rerun_node.bound_image_asset_id != first_asset_id
    assert db_session.get(Product, product.id).cover_image_asset_id == first_asset_id
    assert db_session.get(ProductImageAsset, first_asset_id) is not None
    assert records[0].compiled_prompt_hash == records[1].compiled_prompt_hash
    assert records[0].prompt_artifact_version_id == records[1].prompt_artifact_version_id
    assert [record.provider_model for record in records] == ["provider-a", "provider-b"]
    assert db_session.scalar(select(func.count()).select_from(ProductImageAsset)) == 3
    assert db_session.scalar(select(func.count()).select_from(ImagePromptArtifactVersion)) == 1


def test_image_node_auto_cover_prefers_successful_hero_and_preserves_manual_cover(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session, include_scene_before_hero=True)
    uploaded_cover_id = product.cover_image_asset_id
    hero_node = next(
        node
        for node in workflow.nodes
        if node.node_type == WorkflowNodeType.IMAGE_GENERATION and node.config_json["image_type_key"] == "hero"
    )
    scene_node = next(
        node
        for node in workflow.nodes
        if node.node_type == WorkflowNodeType.IMAGE_GENERATION and node.config_json["image_type_key"] == "scene"
    )
    assert hero_node.config_json["cover_priority"] < scene_node.config_json["cover_priority"]
    assert hero_node.config_json["image_type_order"] > scene_node.config_json["image_type_order"]

    _, hero_node_run = _queue_single_node_run(db_session, workflow=workflow, node=hero_node)
    execute_v2_workflow_node_run(
        db_session,
        node_run_id=hero_node_run.id,
        dependencies=WorkflowExecutionDependencies(
            image_provider_resolver=lambda: RecordingImageProvider(
                image_bytes=_png_bytes(color=(220, 80, 20), size=(96, 72)),
                model="hero-provider",
            )
        ),
    )
    db_session.expire_all()
    persisted_hero = db_session.get(WorkflowNode, hero_node.id)
    assert persisted_hero is not None and persisted_hero.bound_image_asset_id is not None
    assert db_session.get(Product, product.id).cover_image_asset_id == uploaded_cover_id

    clear_product_cover(db_session, product_id=product.id)
    _, scene_node_run = _queue_single_node_run(
        db_session,
        workflow=db_session.get(ProductWorkflow, workflow.id),
        node=db_session.get(WorkflowNode, scene_node.id),
    )
    execute_v2_workflow_node_run(
        db_session,
        node_run_id=scene_node_run.id,
        dependencies=WorkflowExecutionDependencies(
            image_provider_resolver=lambda: RecordingImageProvider(
                image_bytes=_png_bytes(color=(40, 100, 190), size=(96, 72)),
                model="scene-provider",
            )
        ),
    )
    db_session.expire_all()
    assert db_session.get(Product, product.id).cover_image_asset_id == persisted_hero.bound_image_asset_id


def test_image_node_does_not_auto_fill_cover_after_workflow_becomes_inactive(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    clear_product_cover(db_session, product_id=product.id)
    image_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.IMAGE_GENERATION)
    _, node_run = _queue_single_node_run(db_session, workflow=workflow, node=image_node)

    class DeactivatingImageProvider(RecordingImageProvider):
        def generate_workflow_image(self, request: WorkflowImageRequest) -> WorkflowImageResult:
            persisted_workflow = db_session.get(ProductWorkflow, workflow.id)
            assert persisted_workflow is not None
            persisted_workflow.active = False
            db_session.commit()
            return super().generate_workflow_image(request)

    execute_v2_workflow_node_run(
        db_session,
        node_run_id=node_run.id,
        dependencies=WorkflowExecutionDependencies(
            image_provider_resolver=lambda: DeactivatingImageProvider(
                image_bytes=_png_bytes(color=(100, 120, 140), size=(64, 64))
            )
        ),
    )

    db_session.expire_all()
    assert db_session.get(Product, product.id).cover_image_asset_id is None
    assert db_session.scalar(select(func.count()).select_from(WorkflowImageGenerationRecord)) == 1


def test_v2_reference_rebind_stales_downstream_until_prompt_is_regenerated(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    original_reference_id = product.image_assets[0].id
    replacement = add_canonical_product_images(
        db_session,
        product_id=product.id,
        image_uploads=[
            (_png_bytes(color=(20, 160, 80), size=(72, 72)), "replacement.png", "image/png")
        ],
    )[0]
    reference_node = next(
        node for node in workflow.nodes if node.node_type == WorkflowNodeType.REFERENCE_IMAGE
    )
    prompt_node = next(
        node for node in workflow.nodes if node.node_type == WorkflowNodeType.PROMPT_GENERATION
    )
    image_nodes = [
        node for node in workflow.nodes if node.node_type == WorkflowNodeType.IMAGE_GENERATION
    ]
    first_image_node = image_nodes[0]
    initial_prompt_version_id = prompt_node.current_prompt_artifact_version_id

    _, initial_image_run = _queue_single_node_run(
        db_session,
        workflow=workflow,
        node=first_image_node,
    )
    execute_v2_workflow_node_run(
        db_session,
        node_run_id=initial_image_run.id,
        dependencies=WorkflowExecutionDependencies(
            image_provider_resolver=lambda: RecordingImageProvider(
                image_bytes=_png_bytes(color=(220, 40, 40), size=(80, 64))
            )
        ),
    )
    db_session.expire_all()
    previous_result_id = db_session.get(WorkflowNode, first_image_node.id).bound_image_asset_id
    assert previous_result_id is not None
    previous_record_count = db_session.scalar(
        select(func.count()).select_from(WorkflowImageGenerationRecord)
    )

    result = bind_v2_reference_node_asset(
        db_session,
        product_id=product.id,
        workflow_id=workflow.id,
        node_id=reference_node.id,
        asset_id=replacement.id,
        expected_workflow_revision=workflow.revision,
        expected_bound_asset_id=original_reference_id,
    )
    assert result.changed is True
    assert result.previous_asset_id == original_reference_id
    assert set(result.affected_node_ids) == {prompt_node.id, *(node.id for node in image_nodes)}

    db_session.expire_all()
    rebound_reference = db_session.get(WorkflowNode, reference_node.id)
    stale_prompt = db_session.get(WorkflowNode, prompt_node.id)
    stale_image = db_session.get(WorkflowNode, first_image_node.id)
    persisted_workflow = db_session.get(ProductWorkflow, workflow.id)
    assert rebound_reference.bound_image_asset_id == replacement.id
    assert stale_prompt.status == WorkflowNodeStatus.IDLE
    assert stale_prompt.current_prompt_artifact_version_id == initial_prompt_version_id
    assert stale_prompt.output_json["references_stale"] is True
    assert stale_prompt.output_json["superseded_reference_asset_ids"] == [original_reference_id]
    assert stale_image.status == WorkflowNodeStatus.IDLE
    assert stale_image.bound_image_asset_id == previous_result_id
    assert persisted_workflow.revision == workflow.revision == 1
    assert db_session.get(ProductImageAsset, previous_result_id) is not None
    assert db_session.scalar(
        select(func.count()).select_from(WorkflowImageGenerationRecord)
    ) == previous_record_count

    with pytest.raises(ConflictError, match="请先重新生成提示词"):
        submit_v2_workflow_node_run(
            db_session,
            node_id=first_image_node.id,
            enqueue=lambda _: None,
        )

    prompt_provider = RecordingPromptProvider(include_all_reference_evidence=True)
    _, prompt_node_run = _queue_single_node_run(
        db_session,
        workflow=persisted_workflow,
        node=stale_prompt,
    )
    execute_v2_workflow_node_run(
        db_session,
        node_run_id=prompt_node_run.id,
        dependencies=WorkflowExecutionDependencies(
            prompt_generation_provider_resolver=lambda: prompt_provider,
        ),
    )
    assert len(prompt_provider.requests) == 1
    assert replacement.id in {
        reference.asset_id for reference in prompt_provider.requests[0].reference_images
    }
    db_session.expire_all()
    refreshed_prompt = db_session.get(WorkflowNode, prompt_node.id)
    assert refreshed_prompt.status == WorkflowNodeStatus.SUCCEEDED
    assert refreshed_prompt.current_prompt_artifact_version_id != initial_prompt_version_id
    assert refreshed_prompt.output_json.get("references_stale") is None
    assert refreshed_prompt.output_json.get("superseded_reference_asset_ids") is None

    submission = submit_v2_workflow_node_run(
        db_session,
        node_id=first_image_node.id,
        enqueue=lambda _: None,
    )
    assert submission.created is True


def test_v2_reference_rebind_rejects_active_downstream_and_scope_mismatch(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    replacement = add_canonical_product_images(
        db_session,
        product_id=product.id,
        image_uploads=[(_make_demo_image_bytes(), "replacement.png", "image/png")],
    )[0]
    other = create_canonical_product(
        db_session,
        name="其他商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "other.png", "image/png")],
    )
    reference_node = next(
        node for node in workflow.nodes if node.node_type == WorkflowNodeType.REFERENCE_IMAGE
    )
    prompt_node = next(
        node for node in workflow.nodes if node.node_type == WorkflowNodeType.PROMPT_GENERATION
    )
    original_reference_id = reference_node.bound_image_asset_id
    _queue_single_node_run(db_session, workflow=workflow, node=prompt_node)

    with pytest.raises(ConflictError, match="正在运行"):
        bind_v2_reference_node_asset(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            node_id=reference_node.id,
            asset_id=replacement.id,
            expected_workflow_revision=workflow.revision,
            expected_bound_asset_id=original_reference_id,
        )
    with pytest.raises(NotFoundError, match="商品图片不存在"):
        bind_v2_reference_node_asset(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            node_id=reference_node.id,
            asset_id=other.image_assets[0].id,
            expected_workflow_revision=workflow.revision,
            expected_bound_asset_id=original_reference_id,
        )
    with pytest.raises(ConflictError, match="revision 已变化"):
        bind_v2_reference_node_asset(
            db_session,
            product_id=product.id,
            workflow_id=workflow.id,
            node_id=reference_node.id,
            asset_id=replacement.id,
            expected_workflow_revision=workflow.revision + 1,
            expected_bound_asset_id=original_reference_id,
        )


def test_image_rerun_commit_failure_cleans_new_file_and_preserves_previous_result(
    configured_env,
    db_session,
    monkeypatch,
) -> None:
    _, workflow = _create_materialized_workflow(db_session)
    image_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.IMAGE_GENERATION)
    first_run, first_node_run = _queue_single_node_run(db_session, workflow=workflow, node=image_node)
    execute_v2_workflow_node_run(
        db_session,
        node_run_id=first_node_run.id,
        dependencies=WorkflowExecutionDependencies(
            image_provider_resolver=lambda: RecordingImageProvider(
                image_bytes=_png_bytes(color=(180, 50, 30), size=(72, 72)),
                model="provider-before-failure",
            )
        ),
    )
    db_session.expire_all()
    persisted_node = db_session.get(WorkflowNode, image_node.id)
    assert persisted_node is not None and persisted_node.bound_image_asset_id is not None
    previous_asset_id = persisted_node.bound_image_asset_id
    previous_asset_count = db_session.scalar(select(func.count()).select_from(ProductImageAsset))
    previous_record_count = db_session.scalar(select(func.count()).select_from(WorkflowImageGenerationRecord))
    files_before = {
        path.relative_to(configured_env)
        for path in configured_env.rglob("*")
        if path.is_file()
    }

    failed_run, failed_node_run = _queue_single_node_run(db_session, workflow=workflow, node=persisted_node)
    original_commit = db_session.commit
    commit_calls = 0

    def fail_result_commit() -> None:
        nonlocal commit_calls
        commit_calls += 1
        if commit_calls == 3:
            raise RuntimeError("injected v2 result commit failure")
        original_commit()

    monkeypatch.setattr(db_session, "commit", fail_result_commit)
    execute_v2_workflow_node_run(
        db_session,
        node_run_id=failed_node_run.id,
        dependencies=WorkflowExecutionDependencies(
            image_provider_resolver=lambda: RecordingImageProvider(
                image_bytes=_png_bytes(color=(20, 100, 210), size=(96, 64)),
                model="provider-failed-commit",
            )
        ),
    )

    db_session.expire_all()
    failed_run_row = db_session.get(WorkflowRun, failed_run.id)
    failed_node_run_row = db_session.get(WorkflowNodeRun, failed_node_run.id)
    node_after_failure = db_session.get(WorkflowNode, image_node.id)
    files_after = {
        path.relative_to(configured_env)
        for path in configured_env.rglob("*")
        if path.is_file()
    }
    assert failed_run_row is not None and failed_run_row.status == WorkflowRunStatus.FAILED
    assert failed_node_run_row is not None and failed_node_run_row.status == WorkflowNodeStatus.FAILED
    assert node_after_failure is not None and node_after_failure.bound_image_asset_id == previous_asset_id
    assert db_session.scalar(select(func.count()).select_from(ProductImageAsset)) == previous_asset_count
    assert db_session.scalar(select(func.count()).select_from(WorkflowImageGenerationRecord)) == previous_record_count
    assert files_after == files_before
    assert db_session.get(WorkflowRun, first_run.id).status == WorkflowRunStatus.SUCCEEDED


def test_image_node_rejects_multiple_provider_images_without_writing_asset(db_session) -> None:
    _, workflow = _create_materialized_workflow(db_session)
    image_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.IMAGE_GENERATION)
    run, node_run = _queue_single_node_run(db_session, workflow=workflow, node=image_node)
    provider = RecordingImageProvider(
        image_bytes=_png_bytes(color=(20, 20, 20), size=(32, 32)),
        image_count=2,
    )

    execute_v2_workflow_node_run(
        db_session,
        node_run_id=node_run.id,
        dependencies=WorkflowExecutionDependencies(image_provider_resolver=lambda: provider),
    )

    db_session.expire_all()
    persisted_run = db_session.get(WorkflowRun, run.id)
    persisted_node = db_session.get(type(image_node), image_node.id)
    assert persisted_run is not None and persisted_run.status == WorkflowRunStatus.FAILED
    assert persisted_run.is_retryable is False
    assert persisted_node is not None and persisted_node.bound_image_asset_id is None
    assert db_session.scalar(select(func.count()).select_from(ProductImageAsset)) == 1
    assert db_session.scalar(select(func.count()).select_from(WorkflowImageGenerationRecord)) == 0


def test_asset_delete_reports_v2_node_visual_and_prompt_references(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    reference_asset_id = product.image_assets[0].id
    clear_product_cover(db_session, product_id=product.id)

    with pytest.raises(ConflictError, match="工作流节点绑定"):
        delete_product_image_asset(db_session, asset_id=reference_asset_id)

    for node in workflow.nodes:
        if node.bound_image_asset_id == reference_asset_id:
            node.bound_image_asset_id = None
    db_session.commit()
    with pytest.raises(ConflictError, match="视觉体系版本"):
        delete_product_image_asset(db_session, asset_id=reference_asset_id)

    for reference in db_session.scalars(
        select(VisualSystemVersionReference).where(VisualSystemVersionReference.asset_id == reference_asset_id)
    ):
        db_session.delete(reference)
    db_session.commit()
    with pytest.raises(ConflictError, match="提示词版本"):
        delete_product_image_asset(db_session, asset_id=reference_asset_id)


def test_asset_delete_reports_generation_result_and_reference_history(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    reference_asset_id = product.image_assets[0].id
    image_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.IMAGE_GENERATION)
    _, node_run = _queue_single_node_run(db_session, workflow=workflow, node=image_node)
    execute_v2_workflow_node_run(
        db_session,
        node_run_id=node_run.id,
        dependencies=WorkflowExecutionDependencies(
            image_provider_resolver=lambda: RecordingImageProvider(
                image_bytes=_png_bytes(color=(80, 120, 180), size=(64, 64)),
            )
        ),
    )
    db_session.expire_all()
    generated_node = db_session.get(WorkflowNode, image_node.id)
    assert generated_node is not None and generated_node.bound_image_asset_id is not None
    generated_asset_id = generated_node.bound_image_asset_id
    generated_node.bound_image_asset_id = None
    db_session.commit()

    with pytest.raises(ConflictError, match="生成历史作为结果"):
        delete_product_image_asset(db_session, asset_id=generated_asset_id)

    clear_product_cover(db_session, product_id=product.id)
    for node in db_session.scalars(select(WorkflowNode).where(WorkflowNode.workflow_id == workflow.id)):
        if node.bound_image_asset_id == reference_asset_id:
            node.bound_image_asset_id = None
    for reference in db_session.scalars(
        select(VisualSystemVersionReference).where(VisualSystemVersionReference.asset_id == reference_asset_id)
    ):
        db_session.delete(reference)
    for reference in db_session.scalars(
        select(ImagePromptArtifactVersionReference).where(
            ImagePromptArtifactVersionReference.asset_id == reference_asset_id
        )
    ):
        db_session.delete(reference)
    db_session.commit()

    assert db_session.scalar(
        select(WorkflowImageGenerationReference.id).where(
            WorkflowImageGenerationReference.asset_id == reference_asset_id
        )
    )
    with pytest.raises(ConflictError, match="生成历史作为参考图"):
        delete_product_image_asset(db_session, asset_id=reference_asset_id)


def test_product_delete_cascades_v2_generation_history_and_owned_visual_system(db_session) -> None:
    product, workflow = _create_materialized_workflow(db_session)
    visual_system_version_id = workflow.visual_system_version_id
    assert visual_system_version_id is not None
    image_node = next(node for node in workflow.nodes if node.node_type == WorkflowNodeType.IMAGE_GENERATION)
    _, node_run = _queue_single_node_run(db_session, workflow=workflow, node=image_node)
    execute_v2_workflow_node_run(
        db_session,
        node_run_id=node_run.id,
        dependencies=WorkflowExecutionDependencies(
            image_provider_resolver=lambda: RecordingImageProvider(
                image_bytes=_png_bytes(color=(45, 75, 105), size=(64, 48)),
            )
        ),
    )

    delete_product(db_session, product_id=product.id)

    assert db_session.get(type(product), product.id) is None
    assert db_session.scalar(select(func.count()).select_from(ProductImageAsset)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowImageGenerationRecord)) == 0
    assert db_session.scalar(select(func.count()).select_from(ImagePromptArtifactVersion)) == 0
    assert db_session.get(VisualSystemVersion, visual_system_version_id) is None


def _workflow_image_request() -> WorkflowImageRequest:
    return WorkflowImageRequest(
        compiled_prompt="严格保持商品结构",
        generation_spec=GenerationSpec(
            aspect_ratio="16:9",
            resolution_tier="high",
            quality_intent="high",
            reference_fidelity="high",
            background_intent="opaque",
            text_policy="none",
        ),
        references=(
            WorkflowImageReference(
                asset_id="asset-1",
                role="product_identity",
                label="商品参考图",
                filename="product.png",
                mime_type="image/png",
                bytes_data=_make_demo_image_bytes(),
            ),
        ),
    )


def test_responses_workflow_adapter_uses_one_image_tool_call_with_native_reference(
    configured_env: Path,
    monkeypatch,
) -> None:
    provider = OpenAIResponsesImageProvider(
        ResolvedImageProviderConfig(
            provider_kind="openai_responses",
            model="responses-image-model",
            api_key="test-key",
        )
    )
    captured: dict[str, object] = {}

    def fake_generate_image(**kwargs):
        captured.update(kwargs)
        return ResponsesImageResult(
            bytes_data=_make_demo_image_bytes(),
            mime_type="image/png",
            model_name="responses-image-model",
            provider_name="openai-responses",
            prompt_version="v1",
            size=kwargs["size"],
            generated_at=datetime.now(UTC),
            provider_response_id="resp-image",
            previous_response_id=None,
            image_generation_call_id="call-image",
            provider_request_json={
                "tools": [{"type": "image_generation", "size": kwargs["size"], "quality": "high"}]
            },
            provider_output_json={
                "status": "completed",
                "_productflow": {
                    "effective_image_tool": {"size": kwargs["size"], "quality": "high"},
                    "notes": [],
                },
            },
        )

    monkeypatch.setattr(provider.client, "generate_image", fake_generate_image)
    result = provider.generate_workflow_image(_workflow_image_request())

    assert captured["size"] == "1536x1024"
    assert len(captured["reference_images"]) == 1
    assert captured["reference_images"][0].bytes_data == _make_demo_image_bytes()
    assert captured["tool_options"]["quality"] == "high"
    assert captured["tool_options"]["input_fidelity"] == "high"
    assert len(result.images) == 1
    assert result.effective_parameters["size"] == "1536x1024"
    assert result.effective_parameters["reference_image_count"] == 1


def test_images_workflow_adapter_uses_single_edit_for_references(monkeypatch) -> None:
    captured: dict[str, object] = {}

    def fake_edit(self, **kwargs):
        captured.update(kwargs)
        return [
            ImagesAPIResult(
                bytes_data=_make_demo_image_bytes(),
                mime_type="image/png",
                model_name="images-model",
                size=kwargs["size"],
                generated_at=datetime.now(UTC),
                revised_prompt=None,
                provider_request_json={
                    "model": "images-model",
                    "size": kwargs["size"],
                    "n": kwargs["n"],
                    "quality": kwargs["quality"],
                    "image_count": len(kwargs["image"]),
                },
                provider_output_json={"_productflow": {"effective_image_count": len(kwargs["image"])}},
            )
        ]

    monkeypatch.setattr(OpenAIImagesClient, "edit", fake_edit)
    provider = OpenAIImagesImageProvider(
        ResolvedImageProviderConfig(
            provider_kind="openai_images",
            model="images-model",
            api_key="test-key",
        )
    )
    result = provider.generate_workflow_image(_workflow_image_request())

    assert captured["n"] == 1
    assert captured["size"] == "1536x1024"
    assert len(captured["image"]) == 1
    assert captured["image"][0].bytes_data == _make_demo_image_bytes()
    assert len(result.images) == 1
    assert result.effective_parameters["operation"] == "edit"
    assert result.effective_parameters["reference_image_count"] == 1


def test_images_workflow_adapter_records_effective_reference_count_after_fallback(monkeypatch) -> None:
    request = _workflow_image_request()
    request = request.model_copy(
        update={
            "references": (
                request.references[0],
                request.references[0].model_copy(
                    update={"asset_id": "asset-reference-2", "filename": "reference-2.png"}
                ),
            )
        }
    )

    def fake_edit(self, **kwargs):
        return [
            ImagesAPIResult(
                bytes_data=_make_demo_image_bytes(),
                mime_type="image/png",
                model_name="images-model",
                size=kwargs["size"],
                generated_at=datetime.now(UTC),
                revised_prompt=None,
                provider_request_json={
                    "model": "images-model",
                    "size": kwargs["size"],
                    "n": kwargs["n"],
                    "quality": kwargs["quality"],
                    "image_count": 1,
                },
                provider_output_json={"_productflow": {"effective_image_count": 1}},
            )
        ]

    monkeypatch.setattr(OpenAIImagesClient, "edit", fake_edit)
    provider = OpenAIImagesImageProvider(
        ResolvedImageProviderConfig(
            provider_kind="openai_images",
            model="images-model",
            api_key="test-key",
        )
    )

    result = provider.generate_workflow_image(request)

    assert result.effective_parameters["reference_image_count"] == 1
    assert result.effective_parameters["image_count"] == 1


def test_gemini_workflow_adapter_maps_ratio_and_resolution_once(monkeypatch) -> None:
    captured: dict[str, object] = {}

    def fake_generate_image(self, **kwargs):
        captured.update(kwargs)
        return GeminiImageResult(
            bytes_data=_make_demo_image_bytes(),
            mime_type="image/png",
            model_name="gemini-3.1-flash-image-preview",
            provider_name="google-gemini-image",
            prompt_version="v1",
            size=kwargs["size"],
            generated_at=datetime.now(UTC),
            provider_response_id="gemini-response",
            provider_request_json={
                "image_config": {"aspect_ratio": "16:9", "image_size": "2K"},
            },
            provider_output_json={
                "_productflow": {
                    "effective_aspect_ratio": "16:9",
                    "effective_image_size": "2K",
                    "notes": [],
                }
            },
        )

    monkeypatch.setattr(GoogleGeminiImageClient, "generate_image", fake_generate_image)
    provider = GoogleGeminiImageProvider(
        ResolvedImageProviderConfig(
            provider_kind="google_gemini_image",
            model="gemini-3.1-flash-image-preview",
            api_key="test-key",
        )
    )
    result = provider.generate_workflow_image(_workflow_image_request())

    assert captured["size"] == "2048x1152"
    assert len(captured["reference_images"]) == 1
    assert len(result.images) == 1
    assert result.effective_parameters["aspect_ratio"] == "16:9"
    assert result.effective_parameters["image_size"] == "2K"
