from __future__ import annotations

from helpers import _make_demo_image_bytes
from sqlalchemy import select

from productflow_backend.application.product_workflow.dependencies import WorkflowExecutionDependencies
from productflow_backend.application.product_workflow.graph_commands import apply_graph_change_set, get_workflow_graph
from productflow_backend.application.product_workflow.graph_contracts import (
    MoveNodesOp,
    RenameNodeOp,
    UpdateNodeConfigOp,
    WorkflowChangeSet,
)
from productflow_backend.application.product_workflow.graph_direct_create import create_product_with_direct_graph
from productflow_backend.application.product_workflow.graph_execution import execute_graph_run
from productflow_backend.application.product_workflow.graph_queries import project_workflow_graph
from productflow_backend.application.product_workflow.graph_runs import submit_graph_run
from productflow_backend.application.product_workflow.graph_template import DirectCreateImageType
from productflow_backend.domain.enums import (
    GraphConfigStatus,
    GraphNodeType,
    GraphRunScope,
    WorkflowNodeStatus,
    WorkflowRunStatus,
)
from productflow_backend.infrastructure.db.models import (
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
        scope=GraphRunScope.TO_NODE,
        target_node_id=next(
            node.id for node in created.projection.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION
        ),
        enqueue=lambda run_id: execute_graph_run(run_id, dependencies=dependencies),
    )
    assert submission.run.status == WorkflowRunStatus.SUCCEEDED
    node_statuses = {item.node_id: item.status for item in submission.run.node_runs}
    assert set(node_statuses.values()) == {WorkflowNodeStatus.SUCCEEDED}
    artifacts = list(db_session.scalars(select(WorkflowGraphArtifact)))
    assert {artifact.artifact_type for artifact in artifacts} == {"prompt", "image"}
    prompt_artifact = next(artifact for artifact in artifacts if artifact.artifact_type == "prompt")
    assert "images" not in prompt_artifact.payload_json
    assert "fact_keys" not in prompt_artifact.payload_json
    assert prompt_artifact.payload_json["design_goal"] == "由 v3 运行写出的提示词"
    prompt_row = db_session.get(WorkflowGraphNode, prompt_node.id)
    assert prompt_row is not None
    assert prompt_row.current_artifact_id == prompt_artifact.id
    assert prompt_provider.requests[0].image_plan_keys == ("output",)
    assert prompt_provider.requests[0].visual_system is None
    assert prompt_provider.requests[0].reference_images == ()
    assert "contract_version" in image_provider.requests[0].compiled_prompt
    assert "image_plan_key" not in image_provider.requests[0].compiled_prompt
    projection = project_workflow_graph(db_session, created.graph)
    image_node = next(node for node in projection.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION)
    image_artifact = next(artifact for artifact in artifacts if artifact.artifact_type == "image")
    assert image_node.preview_asset_id == image_artifact.product_image_asset_id
    unused_asset = next(node for node in projection.nodes if node.node_type == GraphNodeType.IMAGE_ASSET)
    assert unused_asset.preview_asset_id == unused_asset.bound_asset_id


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
    assert len(artifact_ids) == 2
    assert len(node_run_ids) == 2

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
    assert {row.node_id for row in remaining_artifacts} == {prompt_node.id, image_node.id}
    remaining_runs = list(db_session.scalars(select(WorkflowGraphNodeRun)))
    assert {row.id for row in remaining_runs} == node_run_ids
    assert {row.node_id for row in remaining_runs} == {prompt_node.id, image_node.id}
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
    assert request.current_prompt.shared_rules == ["来自节点配置"]
    assert request.current_prompt.design_goal == "节点目标"
    assert "禁止编造认证" in request.current_prompt.creative_boundary
    assert request.current_prompt.text.headline == "主标题"
    assert request.visual_system is None
    assert request.image_plan_keys == ("output",)

    projection = project_workflow_graph(db_session, created.graph)
    prompt_view = next(node for node in projection.nodes if node.id == prompt_node.id)
    assert prompt_view.config_status == GraphConfigStatus.READY
    apply_graph_change_set(
        db_session,
        product_id=created.product.id,
        graph_id=created.graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=updated.graph.revision,
            summary="改提示词使产物过期",
            operations=[
                UpdateNodeConfigOp(
                    node_ref=prompt_node.id,
                    config={
                        "image_type_key": "hero",
                        "prompt": {
                            "shared_rules": ["已改过"],
                            "design_goal": "新目标",
                        },
                    },
                )
            ],
        ),
    )
    stale = project_workflow_graph(db_session, created.graph)
    stale_prompt = next(node for node in stale.nodes if node.id == prompt_node.id)
    assert stale_prompt.config_status == GraphConfigStatus.STALE
