from __future__ import annotations

from helpers import _make_demo_image_bytes
from sqlalchemy import select

from productflow_backend.application.product_workflow.dependencies import WorkflowExecutionDependencies
from productflow_backend.application.product_workflow.graph_commands import apply_graph_change_set, get_workflow_graph
from productflow_backend.application.product_workflow.graph_contracts import (
    DisconnectEdgeOp,
    MoveNodesOp,
    RenameNodeOp,
    UpdateNodeConfigOp,
    WorkflowChangeSet,
)
from productflow_backend.application.product_workflow.graph_direct_create import create_product_with_direct_graph
from productflow_backend.application.product_workflow.graph_execution import (
    NO_ON_IMAGE_TEXT_RULE,
    execute_graph_run,
)
from productflow_backend.application.product_workflow.graph_queries import project_workflow_graph
from productflow_backend.application.product_workflow.graph_runs import submit_graph_run
from productflow_backend.application.product_workflow.graph_template import DirectCreateImageType
from productflow_backend.domain.enums import (
    AsyncDispatchStatus,
    GraphActorType,
    GraphConfigStatus,
    GraphEdgeRole,
    GraphNodeType,
    GraphRunScope,
    JobStatus,
    WorkflowNodeStatus,
    WorkflowRunStatus,
)
from productflow_backend.infrastructure.db.models import (
    AsyncDispatch,
    DeliveryRenditionJob,
    WorkflowGraphArtifact,
    WorkflowGraphNode,
    WorkflowGraphNodeRun,
)
from productflow_backend.infrastructure.image.base import (
    ImageProvider,
    WorkflowGeneratedImage,
    WorkflowImageRequest,
    WorkflowImageResult,
)
from productflow_backend.infrastructure.prompt.base import (
    PromptGenerationProvider,
    PromptGenerationRequest,
    PromptGenerationResult,
)


class InventingTextPromptProvider(PromptGenerationProvider):
    provider_name = "inventing-v3-prompt"

    def __init__(self) -> None:
        self.requests: list[PromptGenerationRequest] = []

    def generate_prompt(self, request: PromptGenerationRequest) -> PromptGenerationResult:
        self.requests.append(request)
        payload = request.current_prompt.model_copy(
            update={
                "design_goal": "带字海报",
                "text": request.current_prompt.text.model_copy(update={"headline": "筋膜枪", "body": "299"}),
            }
        )
        return PromptGenerationResult(payload=payload, model="inventing-prompt", response_id="resp-text")


class RecordingPromptProvider(PromptGenerationProvider):
    provider_name = "recording-v3-prompt"

    def __init__(self) -> None:
        self.requests: list[PromptGenerationRequest] = []

    def generate_prompt(self, request: PromptGenerationRequest) -> PromptGenerationResult:
        self.requests.append(request)
        payload = request.current_prompt.model_copy(update={"design_goal": "由 v3 运行写出的提示词"})
        return PromptGenerationResult(payload=payload, model="recording-prompt", response_id="resp-1")


class RecordingImageProvider(ImageProvider):
    provider_name = "recording-v3-image"

    def __init__(self, image_bytes: bytes) -> None:
        self.image_bytes = image_bytes
        self.requests: list[WorkflowImageRequest] = []

    def generate_workflow_image(self, request: WorkflowImageRequest) -> WorkflowImageResult:
        self.requests.append(request)
        return WorkflowImageResult(
            images=(WorkflowGeneratedImage(bytes_data=self.image_bytes, mime_type="image/png"),),
            model="recording-image",
            provider_status="succeeded",
            effective_parameters={"size": "1024x1024"},
        )


def test_prompt_then_image_run_writes_artifacts_without_plan_keys(db_session) -> None:
    image_bytes = _make_demo_image_bytes()
    created = create_product_with_direct_graph(
        db_session,
        name="运行演示商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(image_bytes, "ref.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    prompt_provider = RecordingPromptProvider()
    image_provider = RecordingImageProvider(image_bytes)
    dependencies = WorkflowExecutionDependencies(
        prompt_generation_provider_resolver=lambda: prompt_provider,
        image_provider_resolver=lambda: image_provider,
    )
    prompt_node = next(
        node for node in created.projection.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION
    )
    submission = submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.GRAPH,
        enqueue=lambda run_id: execute_graph_run(run_id, dependencies=dependencies),
    )
    assert submission.run.status == WorkflowRunStatus.SUCCEEDED
    node_statuses = {item.node_id: item.status for item in submission.run.node_runs}
    assert set(node_statuses.values()) == {WorkflowNodeStatus.SUCCEEDED}
    artifacts = list(db_session.scalars(select(WorkflowGraphArtifact)))
    assert {artifact.artifact_type for artifact in artifacts} == {
        "creative_brief",
        "visual_system",
        "prompt",
        "image",
    }
    prompt_artifact = next(artifact for artifact in artifacts if artifact.artifact_type == "prompt")
    assert "images" not in prompt_artifact.payload_json
    assert "fact_keys" not in prompt_artifact.payload_json
    assert prompt_artifact.payload_json["design_goal"] == "由 v3 运行写出的提示词"
    prompt_row = db_session.get(WorkflowGraphNode, prompt_node.id)
    assert prompt_row is not None
    assert prompt_row.current_artifact_id == prompt_artifact.id
    assert (prompt_row.config_json or {}).get("prompt", {}).get("design_goal") == "由 v3 运行写出的提示词"
    assert prompt_provider.requests[0].image_plan_keys == ("output",)
    assert prompt_provider.requests[0].visual_system is None
    assert prompt_provider.requests[0].generate_from_context is True
    assert prompt_provider.requests[0].text_policy == "none"
    assert prompt_provider.requests[0].current_prompt.text.headline is None
    assert NO_ON_IMAGE_TEXT_RULE in prompt_provider.requests[0].current_prompt.shared_rules
    assert prompt_provider.requests[0].image_type_title == "首屏海报图"
    assert prompt_provider.requests[0].image_type_description == "搜索列表首图，商品够大能认"
    assert prompt_provider.requests[0].image_type_family == "photography"
    assert prompt_provider.requests[0].image_type_job is not None
    assert "55%" in prompt_provider.requests[0].image_type_job or "占画面" in prompt_provider.requests[0].image_type_job
    assert prompt_provider.requests[0].reference_images
    assert prompt_provider.requests[0].current_prompt.content.background != "干净背景"
    assert "首屏海报图" in prompt_provider.requests[0].current_prompt.design_goal
    assert any(fact.get("key") == "product_name" for fact in prompt_provider.requests[0].facts)
    assert any(fact.get("value") == "运行演示商品" for fact in prompt_provider.requests[0].facts)
    assert "能上淘宝" in image_provider.requests[0].compiled_prompt
    assert NO_ON_IMAGE_TEXT_RULE in image_provider.requests[0].compiled_prompt
    assert "原图贴字" in image_provider.requests[0].compiled_prompt or "只加一行字" in image_provider.requests[0].compiled_prompt
    assert "contract_version" in image_provider.requests[0].compiled_prompt
    assert "image_plan_key" not in image_provider.requests[0].compiled_prompt
    assert image_provider.requests[0].references
    projection = project_workflow_graph(db_session, created.graph)
    image_node = next(node for node in projection.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION)
    image_artifact = next(artifact for artifact in artifacts if artifact.artifact_type == "image")
    assert image_node.preview_asset_id == image_artifact.product_image_asset_id
    prompt_view = next(node for node in projection.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    assert prompt_view.current_artifact_payload is not None
    assert prompt_view.current_artifact_payload["design_goal"] == "由 v3 运行写出的提示词"
    asset_node = next(node for node in projection.nodes if node.node_type == GraphNodeType.IMAGE_ASSET)
    assert asset_node.preview_asset_id == asset_node.bound_asset_id
    assert asset_node.unused is False
    assert asset_node.config.get("role") == "product_identity"
    assert prompt_provider.requests[0].reference_images[0].role == "product_identity"
    assert image_provider.requests[0].references[0].role == "product_identity"


def test_workflow_run_passes_image_asset_role_to_providers(db_session) -> None:
    image_bytes = _make_demo_image_bytes()
    created = create_product_with_direct_graph(
        db_session,
        name="参考角色演示商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(image_bytes, "ref.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    asset_node = next(node for node in created.projection.nodes if node.node_type == GraphNodeType.IMAGE_ASSET)
    apply_graph_change_set(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=created.graph.revision,
            summary="环境参考",
            actor_type=GraphActorType.USER,
            operations=[
                UpdateNodeConfigOp(
                    node_ref=asset_node.id,
                    config={**asset_node.config, "role": "environment"},
                    bound_asset_id=asset_node.bound_asset_id,
                )
            ],
        ),
    )
    prompt_provider = RecordingPromptProvider()
    image_provider = RecordingImageProvider(image_bytes)
    dependencies = WorkflowExecutionDependencies(
        prompt_generation_provider_resolver=lambda: prompt_provider,
        image_provider_resolver=lambda: image_provider,
    )
    submission = submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.GRAPH,
        enqueue=lambda run_id: execute_graph_run(run_id, dependencies=dependencies),
    )
    assert submission.run.status == WorkflowRunStatus.SUCCEEDED
    assert prompt_provider.requests[0].reference_images[0].role == "environment"
    assert image_provider.requests[0].references[0].role == "environment"


def test_visual_node_run_fills_overlay_without_generating_images(db_session) -> None:
    image_bytes = _make_demo_image_bytes()
    created = create_product_with_direct_graph(
        db_session,
        name="只跑视觉规范",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(image_bytes, "ref.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    visual_node = next(node for node in created.projection.nodes if node.node_type == GraphNodeType.VISUAL_SYSTEM)
    image_provider = RecordingImageProvider(image_bytes)
    submission = submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.NODE,
        target_node_id=visual_node.id,
        enqueue=lambda run_id: execute_graph_run(
            run_id,
            dependencies=WorkflowExecutionDependencies(
                prompt_generation_provider_resolver=lambda: RecordingPromptProvider(),
                image_provider_resolver=lambda: image_provider,
            ),
        ),
    )
    assert submission.run.status == WorkflowRunStatus.SUCCEEDED
    assert [item.node_id for item in submission.run.node_runs] == [visual_node.id]
    artifacts = list(db_session.scalars(select(WorkflowGraphArtifact)))
    assert {artifact.artifact_type for artifact in artifacts} == {"visual_system"}
    visual_row = db_session.get(WorkflowGraphNode, visual_node.id)
    assert visual_row is not None
    overlay = (visual_row.config_json or {}).get("visual_overlay")
    assert isinstance(overlay, dict)
    assert overlay.get("style")
    assert image_provider.requests == []
    db_session.expire_all()
    projection = project_workflow_graph(db_session, created.graph)
    visual_view = next(node for node in projection.nodes if node.id == visual_node.id)
    assert visual_view.config_status == GraphConfigStatus.READY
    overlay_config = visual_view.config.get("visual_overlay")
    assert isinstance(overlay_config, dict)
    assert overlay_config.get("style")


def test_brief_node_run_fills_fields_without_generating_images(db_session) -> None:
    image_bytes = _make_demo_image_bytes()
    created = create_product_with_direct_graph(
        db_session,
        name="只跑创作要求",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(image_bytes, "ref.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    brief_node = next(node for node in created.projection.nodes if node.node_type == GraphNodeType.CREATIVE_BRIEF)
    image_provider = RecordingImageProvider(image_bytes)
    submission = submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.NODE,
        target_node_id=brief_node.id,
        enqueue=lambda run_id: execute_graph_run(
            run_id,
            dependencies=WorkflowExecutionDependencies(
                prompt_generation_provider_resolver=lambda: RecordingPromptProvider(),
                image_provider_resolver=lambda: image_provider,
            ),
        ),
    )
    assert submission.run.status == WorkflowRunStatus.SUCCEEDED
    assert [item.node_id for item in submission.run.node_runs] == [brief_node.id]
    artifacts = list(db_session.scalars(select(WorkflowGraphArtifact)))
    assert {artifact.artifact_type for artifact in artifacts} == {"creative_brief"}
    brief_row = db_session.get(WorkflowGraphNode, brief_node.id)
    assert brief_row is not None
    config = brief_row.config_json or {}
    assert config.get("design_goals") or config.get("goal")
    assert image_provider.requests == []
    db_session.expire_all()
    projection = project_workflow_graph(db_session, created.graph)
    brief_view = next(node for node in projection.nodes if node.id == brief_node.id)
    assert brief_view.config_status == GraphConfigStatus.READY


def test_authored_prompt_fields_are_not_treated_as_generation_seed(db_session) -> None:
    image_bytes = _make_demo_image_bytes()
    created = create_product_with_direct_graph(
        db_session,
        name="手写提示词商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(image_bytes, "ref.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    prompt_node = next(
        node for node in created.projection.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION
    )
    apply_graph_change_set(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=created.graph.revision,
            summary="写入用户构图",
            actor_type=GraphActorType.USER,
            operations=[
                UpdateNodeConfigOp(
                    node_ref=prompt_node.id,
                    config={
                        "image_type_key": "hero",
                        "prompt": {
                            "design_goal": "用户写的构图目标",
                            "composition": {
                                "viewpoint": "俯拍",
                                "product_share_percent": 80,
                                "layout": "左商品右文案",
                            },
                        },
                    },
                )
            ],
        ),
    )
    prompt_provider = RecordingPromptProvider()
    submission = submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.NODE,
        target_node_id=prompt_node.id,
        enqueue=lambda run_id: execute_graph_run(
            run_id,
            dependencies=WorkflowExecutionDependencies(
                prompt_generation_provider_resolver=lambda: prompt_provider,
                image_provider_resolver=lambda: RecordingImageProvider(image_bytes),
            ),
        ),
    )
    assert submission.run.status == WorkflowRunStatus.SUCCEEDED
    assert prompt_provider.requests[0].generate_from_context is False
    assert prompt_provider.requests[0].current_prompt.composition.viewpoint == "俯拍"
    assert prompt_provider.requests[0].current_prompt.design_goal == "用户写的构图目标"


def test_text_policy_none_strips_invented_on_image_copy(db_session) -> None:
    image_bytes = _make_demo_image_bytes()
    created = create_product_with_direct_graph(
        db_session,
        name="禁字商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(image_bytes, "ref.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    prompt_node = next(
        node for node in created.projection.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION
    )
    prompt_provider = InventingTextPromptProvider()
    submission = submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.NODE,
        target_node_id=prompt_node.id,
        enqueue=lambda run_id: execute_graph_run(
            run_id,
            dependencies=WorkflowExecutionDependencies(
                prompt_generation_provider_resolver=lambda: prompt_provider,
                image_provider_resolver=lambda: RecordingImageProvider(image_bytes),
            ),
        ),
    )
    assert submission.run.status == WorkflowRunStatus.SUCCEEDED
    assert prompt_provider.requests[0].text_policy == "none"
    artifacts = list(db_session.scalars(select(WorkflowGraphArtifact)))
    prompt_artifact = next(artifact for artifact in artifacts if artifact.artifact_type == "prompt")
    assert prompt_artifact.payload_json["design_goal"] == "带字海报"
    assert not (prompt_artifact.payload_json.get("text") or {}).get("headline")
    assert not (prompt_artifact.payload_json.get("text") or {}).get("body")


def test_prompt_run_validates_inline_visual_overlay_as_provider_exception(db_session) -> None:
    image_bytes = _make_demo_image_bytes()
    created = create_product_with_direct_graph(
        db_session,
        name="内联视觉覆盖商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(image_bytes, "ref.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    visual_node = next(
        node for node in created.projection.nodes if node.node_type == GraphNodeType.VISUAL_SYSTEM
    )
    prompt_node = next(
        node for node in created.projection.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION
    )
    apply_graph_change_set(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=created.graph.revision,
            summary="保存内联视觉覆盖",
            actor_type=GraphActorType.USER,
            operations=[
                UpdateNodeConfigOp(
                    node_ref=visual_node.id,
                    config={"visual_overlay": {"style": ["干净白底"]}},
                )
            ],
        ),
    )
    prompt_provider = RecordingPromptProvider()
    submission = submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.NODE,
        target_node_id=prompt_node.id,
        enqueue=lambda run_id: execute_graph_run(
            run_id,
            dependencies=WorkflowExecutionDependencies(
                prompt_generation_provider_resolver=lambda: prompt_provider,
                image_provider_resolver=lambda: RecordingImageProvider(image_bytes),
            ),
        ),
    )
    assert submission.run.status == WorkflowRunStatus.SUCCEEDED
    request = prompt_provider.requests[0]
    assert request.visual_system is None
    assert request.visual_exceptions[0]["overrides"] == [
        {"field": "style", "value": ["干净白底"]},
    ]


def test_prompt_run_accepts_generated_overlay_with_localized_color_roles(db_session) -> None:
    image_bytes = _make_demo_image_bytes()
    created = create_product_with_direct_graph(
        db_session,
        name="生成视觉覆盖商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(image_bytes, "ref.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    visual_node = next(
        node for node in created.projection.nodes if node.node_type == GraphNodeType.VISUAL_SYSTEM
    )
    prompt_node = next(
        node for node in created.projection.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION
    )
    apply_graph_change_set(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=created.graph.revision,
            summary="写入模型生成的视觉覆盖",
            actor_type=GraphActorType.USER,
            operations=[
                UpdateNodeConfigOp(
                    node_ref=visual_node.id,
                    config={
                        "visual_overlay": {
                            "style": ["极简棚拍"],
                            "colors": [{"role": "背景", "value": "#F4F4F5", "label": ""}],
                            "prohibitions": ["不要改结构"],
                        }
                    },
                )
            ],
        ),
    )
    prompt_provider = RecordingPromptProvider()
    submission = submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.NODE,
        target_node_id=prompt_node.id,
        enqueue=lambda run_id: execute_graph_run(
            run_id,
            dependencies=WorkflowExecutionDependencies(
                prompt_generation_provider_resolver=lambda: prompt_provider,
                image_provider_resolver=lambda: RecordingImageProvider(image_bytes),
            ),
        ),
    )
    assert submission.run.status == WorkflowRunStatus.SUCCEEDED
    request = prompt_provider.requests[0]
    assert request.visual_system is None
    overrides = {item["field"]: item["value"] for item in request.visual_exceptions[0]["overrides"]}
    assert overrides["style"] == ["极简棚拍"]
    assert overrides["colors"] == [{"role": "color-1", "value": "#F4F4F5", "label": "背景"}]
    assert overrides["prohibitions"] == ["不要改结构"]


def test_older_run_does_not_overwrite_current_artifact_after_revision_change(db_session) -> None:
    image_bytes = _make_demo_image_bytes()
    created = create_product_with_direct_graph(
        db_session,
        name="revision fence 商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(image_bytes, "ref.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    prompt_node = next(
        node for node in created.projection.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION
    )
    submission = submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.NODE,
        target_node_id=prompt_node.id,
        enqueue=lambda _run_id: None,
    )
    apply_graph_change_set(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=created.graph.revision,
            summary="运行中改图",
            operations=[RenameNodeOp(node_ref=prompt_node.id, title="新提示词标题")],
        ),
    )
    execute_graph_run(
        submission.run.id,
        dependencies=WorkflowExecutionDependencies(
            prompt_generation_provider_resolver=lambda: RecordingPromptProvider(),
            image_provider_resolver=lambda: RecordingImageProvider(image_bytes),
        ),
    )
    node = db_session.get(WorkflowGraphNode, prompt_node.id)
    assert node is not None
    assert node.current_artifact_id is None
    artifacts = list(db_session.scalars(select(WorkflowGraphArtifact)))
    assert len(artifacts) == 1
    assert artifacts[0].graph_revision == 1


def test_successful_run_artifacts_survive_rename_and_move(db_session) -> None:
    enabled = db_session.connection().exec_driver_sql("PRAGMA foreign_keys").scalar()
    assert enabled == 1
    image_bytes = _make_demo_image_bytes()
    created = create_product_with_direct_graph(
        db_session,
        name="改图后保留产物",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(image_bytes, "ref.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    prompt_node = next(
        node for node in created.projection.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION
    )
    image_node = next(
        node for node in created.projection.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION
    )
    submission = submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.TO_NODE,
        target_node_id=image_node.id,
        enqueue=lambda run_id: execute_graph_run(
            run_id,
            dependencies=WorkflowExecutionDependencies(
                prompt_generation_provider_resolver=lambda: RecordingPromptProvider(),
                image_provider_resolver=lambda: RecordingImageProvider(image_bytes),
            ),
        ),
    )
    assert submission.run.status == WorkflowRunStatus.SUCCEEDED
    prompt_row = db_session.get(WorkflowGraphNode, prompt_node.id)
    image_row = db_session.get(WorkflowGraphNode, image_node.id)
    assert prompt_row is not None and prompt_row.current_artifact_id
    assert image_row is not None and image_row.current_artifact_id
    prompt_artifact_id = prompt_row.current_artifact_id
    image_artifact_id = image_row.current_artifact_id
    artifact_ids = {row.id for row in db_session.scalars(select(WorkflowGraphArtifact))}
    node_run_ids = {row.id for row in db_session.scalars(select(WorkflowGraphNodeRun))}
    assert len(artifact_ids) == 4
    assert len(node_run_ids) == 4

    moved_x = image_node.position_x + 48
    moved_y = image_node.position_y + 24
    apply_graph_change_set(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=created.graph.revision,
            summary="运行成功后重命名并移动",
            operations=[
                RenameNodeOp(node_ref=prompt_node.id, title="改名后的提示词"),
                MoveNodesOp(nodes=[(image_node.id, moved_x, moved_y)]),
            ],
        ),
    )

    prompt_row = db_session.get(WorkflowGraphNode, prompt_node.id)
    image_row = db_session.get(WorkflowGraphNode, image_node.id)
    assert prompt_row is not None
    assert image_row is not None
    assert prompt_row.title == "改名后的提示词"
    assert prompt_row.current_artifact_id == prompt_artifact_id
    assert image_row.current_artifact_id == image_artifact_id
    assert image_row.position_x == moved_x
    assert image_row.position_y == moved_y
    remaining_artifacts = list(db_session.scalars(select(WorkflowGraphArtifact)))
    assert {row.id for row in remaining_artifacts} == artifact_ids
    remaining_runs = list(db_session.scalars(select(WorkflowGraphNodeRun)))
    assert {row.id for row in remaining_runs} == node_run_ids
    graph = get_workflow_graph(db_session, product_id=created.product.id, graph_id=created.graph.id)
    projection = project_workflow_graph(db_session, graph)
    image_view = next(node for node in projection.nodes if node.id == image_node.id)
    image_artifact = db_session.get(WorkflowGraphArtifact, image_artifact_id)
    assert image_artifact is not None
    assert image_view.preview_asset_id == image_artifact.product_image_asset_id


def test_prompt_run_uses_stored_prompt_and_brief_fields(db_session) -> None:
    created = create_product_with_direct_graph(
        db_session,
        name="提示词配置商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "ref.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    prompt_node = next(
        node for node in created.projection.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION
    )
    brief_node = next(node for node in created.projection.nodes if node.node_type == GraphNodeType.CREATIVE_BRIEF)
    updated = apply_graph_change_set(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=created.graph.revision,
            summary="写入提示词与创作要求",
            operations=[
                UpdateNodeConfigOp(
                    node_ref=prompt_node.id,
                    config={
                        "image_type_key": "hero",
                        "prompt": {
                            "shared_rules": ["来自节点配置"],
                            "design_goal": "节点目标",
                        },
                    },
                ),
                UpdateNodeConfigOp(
                    node_ref=brief_node.id,
                    config={
                        "design_goals": ["brief 目标"],
                        "required_copy": ["主标题"],
                        "prohibitions": ["禁止编造认证"],
                    },
                ),
            ],
        ),
    )
    prompt_provider = RecordingPromptProvider()
    dependencies = WorkflowExecutionDependencies(
        prompt_generation_provider_resolver=lambda: prompt_provider,
        image_provider_resolver=lambda: RecordingImageProvider(_make_demo_image_bytes()),
    )
    submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.NODE,
        target_node_id=prompt_node.id,
        enqueue=lambda run_id: execute_graph_run(run_id, dependencies=dependencies),
    )
    request = prompt_provider.requests[0]
    assert request.current_prompt.shared_rules == ["来自节点配置", NO_ON_IMAGE_TEXT_RULE]
    assert request.current_prompt.design_goal == "节点目标"
    assert "禁止编造认证" in request.current_prompt.creative_boundary
    assert NO_ON_IMAGE_TEXT_RULE in request.current_prompt.creative_boundary
    assert request.current_prompt.text.headline is None
    assert request.text_policy == "none"
    assert request.visual_system is None
    assert request.image_plan_keys == ("output",)

    projection = project_workflow_graph(db_session, created.graph)
    prompt_view = next(node for node in projection.nodes if node.id == prompt_node.id)
    assert prompt_view.config_status == GraphConfigStatus.READY
    reference_edge = next(edge for edge in prompt_view.incoming if edge.role == GraphEdgeRole.REFERENCE)
    apply_graph_change_set(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=updated.graph.revision,
            summary="断开参考图使产物过期",
            operations=[DisconnectEdgeOp(edge_ref=reference_edge.id)],
        ),
    )
    stale = project_workflow_graph(db_session, created.graph)
    stale_prompt = next(node for node in stale.nodes if node.id == prompt_node.id)
    assert stale_prompt.config_status == GraphConfigStatus.STALE


def test_image_success_queues_delivery_rendition_from_live_node_spec(db_session) -> None:
    image_bytes = _make_demo_image_bytes()
    created = create_product_with_direct_graph(
        db_session,
        name="交付排队商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(image_bytes, "ref.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    image_node = next(
        node for node in created.projection.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION
    )
    apply_graph_change_set(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=created.graph.revision,
            summary="写入交付规格",
            operations=[
                UpdateNodeConfigOp(
                    node_ref=image_node.id,
                    config={
                        "image_type_key": "hero",
                        "generation_spec": dict(image_node.config["generation_spec"]),
                        "delivery_spec": {
                            "width": 48,
                            "height": 48,
                            "format": "png",
                            "fit": "contain",
                        },
                    },
                )
            ],
        ),
    )
    dependencies = WorkflowExecutionDependencies(
        prompt_generation_provider_resolver=lambda: RecordingPromptProvider(),
        image_provider_resolver=lambda: RecordingImageProvider(image_bytes),
    )
    submission = submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.TO_NODE,
        target_node_id=image_node.id,
        enqueue=lambda run_id: execute_graph_run(run_id, dependencies=dependencies),
    )
    assert submission.run.status == WorkflowRunStatus.SUCCEEDED
    artifact = db_session.scalar(
        select(WorkflowGraphArtifact).where(WorkflowGraphArtifact.artifact_type == "image")
    )
    assert artifact is not None
    job = db_session.scalar(
        select(DeliveryRenditionJob).where(
            DeliveryRenditionJob.source_asset_id == artifact.product_image_asset_id
        )
    )
    assert job is not None
    assert job.status == JobStatus.QUEUED
    assert job.spec_json["width"] == 48
    dispatch = db_session.scalar(select(AsyncDispatch).where(AsyncDispatch.aggregate_id == job.id))
    assert dispatch is not None
    assert dispatch.status == AsyncDispatchStatus.PENDING
    assert dispatch.actor_name == "run_delivery_rendition_job"


class BoomImageProvider(RecordingImageProvider):
    def generate_workflow_image(self, request: WorkflowImageRequest) -> WorkflowImageResult:
        self.requests.append(request)
        raise RuntimeError("provider exploded")


def test_image_node_fails_when_measured_aspect_disagrees_with_spec(db_session) -> None:
    image_bytes = _make_demo_image_bytes()
    created = create_product_with_direct_graph(
        db_session,
        name="比例校验商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(image_bytes, "ref.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    image_node = next(node for node in created.projection.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION)
    spec = dict(image_node.config.get("generation_spec") or {})
    spec["aspect_ratio"] = "3:4"
    apply_graph_change_set(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=created.graph.revision,
            summary="改成竖版",
            actor_type=GraphActorType.USER,
            operations=[UpdateNodeConfigOp(node_ref=image_node.id, config={**image_node.config, "generation_spec": spec})],
        ),
    )
    image_provider = RecordingImageProvider(image_bytes)
    submission = submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.GRAPH,
        enqueue=lambda run_id: execute_graph_run(
            run_id,
            dependencies=WorkflowExecutionDependencies(
                prompt_generation_provider_resolver=lambda: RecordingPromptProvider(),
                image_provider_resolver=lambda: image_provider,
            ),
        ),
    )
    assert submission.run.status == WorkflowRunStatus.FAILED
    image_run = next(item for item in submission.run.node_runs if item.node_id == image_node.id)
    assert image_run.status == WorkflowNodeStatus.FAILED
    assert image_run.failure_reason is not None
    assert "3:4" in image_run.failure_reason
    assert image_provider.requests
    artifacts = list(db_session.scalars(select(WorkflowGraphArtifact).where(WorkflowGraphArtifact.artifact_type == "image")))
    assert artifacts == []


def test_failed_run_does_not_keep_executing_queued_nodes(db_session) -> None:
    image_bytes = _make_demo_image_bytes()
    created = create_product_with_direct_graph(
        db_session,
        name="失败停跑商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(image_bytes, "ref.png", "image/png")],
        image_types=[
            DirectCreateImageType(key="hero", quantity=1, order=0),
            DirectCreateImageType(key="scene", quantity=1, order=1),
        ],
    )
    image_provider = BoomImageProvider(image_bytes)
    submission = submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.GRAPH,
        enqueue=lambda run_id: execute_graph_run(
            run_id,
            dependencies=WorkflowExecutionDependencies(
                prompt_generation_provider_resolver=lambda: RecordingPromptProvider(),
                image_provider_resolver=lambda: image_provider,
            ),
        ),
    )
    assert submission.run.status == WorkflowRunStatus.FAILED
    image_runs = [
        item
        for item in submission.run.node_runs
        if item.node_id
        and next(node for node in created.projection.nodes if node.id == item.node_id).node_type == GraphNodeType.IMAGE_GENERATION
    ]
    assert len(image_runs) == 2
    assert {item.status for item in image_runs} == {WorkflowNodeStatus.FAILED, WorkflowNodeStatus.QUEUED}
    queued = next(item for item in image_runs if item.status == WorkflowNodeStatus.QUEUED)
    execute_graph_run(
        submission.run.id,
        dependencies=WorkflowExecutionDependencies(
            prompt_generation_provider_resolver=lambda: RecordingPromptProvider(),
            image_provider_resolver=lambda: RecordingImageProvider(image_bytes),
        ),
    )
    db_session.refresh(queued)
    db_session.refresh(submission.run)
    assert queued.status == WorkflowNodeStatus.QUEUED
    assert submission.run.status == WorkflowRunStatus.FAILED


def test_duplicate_artifact_persist_does_not_crash_graph_run(db_session) -> None:
    image_bytes = _make_demo_image_bytes()
    created = create_product_with_direct_graph(
        db_session,
        name="幂等落库商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(image_bytes, "ref.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    visual_node = next(node for node in created.projection.nodes if node.node_type == GraphNodeType.VISUAL_SYSTEM)
    submission = submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.NODE,
        target_node_id=visual_node.id,
        enqueue=lambda _run_id: None,
    )
    visual_run = next(item for item in submission.run.node_runs if item.node_id == visual_node.id)
    digest = "a" * 64
    db_session.add(
        WorkflowGraphArtifact(
            graph_id=created.graph.id,
            node_id=visual_node.id,
            node_run_id=visual_run.id,
            artifact_type="visual_system",
            schema_version=3,
            graph_revision=created.graph.revision,
            payload_json={"style": ["占位"]},
            payload_hash=digest,
            input_digest=digest,
            provider_name="preloaded",
            provider_model="preloaded",
        )
    )
    db_session.commit()
    execute_graph_run(
        submission.run.id,
        dependencies=WorkflowExecutionDependencies(
            prompt_generation_provider_resolver=lambda: RecordingPromptProvider(),
            image_provider_resolver=lambda: RecordingImageProvider(image_bytes),
        ),
    )
    db_session.refresh(submission.run)
    assert submission.run.status == WorkflowRunStatus.SUCCEEDED
    artifacts = list(
        db_session.scalars(
            select(WorkflowGraphArtifact).where(WorkflowGraphArtifact.node_run_id == visual_run.id)
        )
    )
    assert len(artifacts) == 1


def test_matching_digest_skips_provider_and_stale_only_after_input_edit(db_session) -> None:
    image_bytes = _make_demo_image_bytes()
    created = create_product_with_direct_graph(
        db_session,
        name="跳过最新节点商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(image_bytes, "ref.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    first_prompt = RecordingPromptProvider()
    first_image = RecordingImageProvider(image_bytes)
    first = submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.GRAPH,
        enqueue=lambda run_id: execute_graph_run(
            run_id,
            dependencies=WorkflowExecutionDependencies(
                prompt_generation_provider_resolver=lambda: first_prompt,
                image_provider_resolver=lambda: first_image,
            ),
        ),
    )
    assert first.run.status == WorkflowRunStatus.SUCCEEDED
    db_session.expire_all()
    graph = get_workflow_graph(db_session, product_id=created.product.id, graph_id=created.graph.id)
    projection = project_workflow_graph(db_session, graph)
    visual_view = next(node for node in projection.nodes if node.node_type == GraphNodeType.VISUAL_SYSTEM)
    brief_view = next(node for node in projection.nodes if node.node_type == GraphNodeType.CREATIVE_BRIEF)
    prompt_view = next(node for node in projection.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    assert visual_view.config_status == GraphConfigStatus.READY
    assert brief_view.config_status == GraphConfigStatus.READY
    assert prompt_view.config_status == GraphConfigStatus.READY

    second_prompt = RecordingPromptProvider()
    second_image = RecordingImageProvider(image_bytes)
    second = submit_graph_run(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        scope=GraphRunScope.GRAPH,
        enqueue=lambda run_id: execute_graph_run(
            run_id,
            dependencies=WorkflowExecutionDependencies(
                prompt_generation_provider_resolver=lambda: second_prompt,
                image_provider_resolver=lambda: second_image,
            ),
        ),
    )
    assert second.run.status == WorkflowRunStatus.SUCCEEDED
    assert second_prompt.requests == []
    assert second_image.requests == []
    assert all(item.output_json and item.output_json.get("skipped") for item in second.run.node_runs)

    image_view = next(node for node in projection.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION)
    spec = dict(image_view.config.get("generation_spec") or {})
    spec["text_policy"] = "required"
    spec["text_language"] = "zh-CN"
    apply_graph_change_set(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=graph.revision,
            summary="改文案策略",
            actor_type=GraphActorType.USER,
            operations=[UpdateNodeConfigOp(node_ref=image_view.id, config={**image_view.config, "generation_spec": spec})],
        ),
    )
    db_session.expire_all()
    graph = get_workflow_graph(db_session, product_id=created.product.id, graph_id=created.graph.id)
    after_edit = project_workflow_graph(db_session, graph)
    visual_after = next(node for node in after_edit.nodes if node.node_type == GraphNodeType.VISUAL_SYSTEM)
    prompt_after = next(node for node in after_edit.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    assert visual_after.config_status == GraphConfigStatus.STALE
    assert prompt_after.config_status == GraphConfigStatus.STALE
