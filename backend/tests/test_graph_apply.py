from __future__ import annotations

import pytest

from productflow_backend.application.product_workflow.graph_apply import (
    EMPTY_GRAPH,
    apply_workflow_change_set,
    invert_applied_graph,
)
from productflow_backend.application.product_workflow.graph_contracts import (
    ConnectNodesOp,
    CreateGroupOp,
    CreateNodeOp,
    DissolveGroupOp,
    MoveNodesToGroupOp,
    RenameGroupOp,
    RenameNodeOp,
    UpdateNodeConfigOp,
    WorkflowChangeSet,
)
from productflow_backend.application.product_workflow.graph_template import (
    DirectCreateImageType,
    build_direct_create_template,
)
from productflow_backend.domain.enums import GraphActorType, GraphNodeType
from productflow_backend.domain.errors import BusinessValidationError


def test_invert_restores_direct_create_template_to_empty_graph() -> None:
    change_set = build_direct_create_template(
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
        reference_asset_ids=["asset-a"],
    )
    after = apply_workflow_change_set(EMPTY_GRAPH, change_set)
    inverse_ops = invert_applied_graph(EMPTY_GRAPH, after)
    restored = apply_workflow_change_set(
        after,
        WorkflowChangeSet(
            base_graph_revision=after.revision,
            summary="撤销直接创建模版",
            actor_type=GraphActorType.USER,
            operations=inverse_ops,
        ),
    )
    assert restored.nodes == ()
    assert restored.edges == ()
    assert restored.groups == ()
    assert restored.revision == after.revision + 1


def test_group_rename_and_dissolve_round_trip() -> None:
    graph = apply_workflow_change_set(
        EMPTY_GRAPH,
        WorkflowChangeSet(
            base_graph_revision=0,
            summary="创建分组",
            operations=[
                CreateNodeOp(
                    client_ref="brief",
                    node_type=GraphNodeType.CREATIVE_BRIEF,
                    title="创作要求",
                ),
                CreateGroupOp(client_ref="folder", title="主视觉", member_refs=("brief",)),
            ],
        ),
    )
    assert graph.node("brief").group_id == "folder"
    renamed = apply_workflow_change_set(
        graph,
        WorkflowChangeSet(
            base_graph_revision=graph.revision,
            summary="重命名分组",
            operations=[RenameGroupOp(group_ref="folder", title="海报组")],
        ),
    )
    assert renamed.group("folder").title == "海报组"
    dissolved = apply_workflow_change_set(
        renamed,
        WorkflowChangeSet(
            base_graph_revision=renamed.revision,
            summary="解散分组",
            operations=[DissolveGroupOp(group_ref="folder")],
        ),
    )
    assert dissolved.groups == ()
    assert dissolved.node("brief").group_id is None


def test_duplicate_same_role_edge_is_rejected() -> None:
    graph = apply_workflow_change_set(
        EMPTY_GRAPH,
        WorkflowChangeSet(
            base_graph_revision=0,
            summary="建节点",
            operations=[
                CreateNodeOp(
                    client_ref="product",
                    node_type=GraphNodeType.PRODUCT_SOURCE,
                    title="商品",
                ),
                CreateNodeOp(
                    client_ref="prompt",
                    node_type=GraphNodeType.PROMPT_GENERATION,
                    title="提示词",
                ),
                ConnectNodesOp(client_ref="facts-1", source_ref="product", target_ref="prompt"),
            ],
        ),
    )
    with pytest.raises(BusinessValidationError, match="相同输入连线已存在"):
        apply_workflow_change_set(
            graph,
            WorkflowChangeSet(
                base_graph_revision=graph.revision,
                summary="重复连线",
                operations=[ConnectNodesOp(client_ref="facts-2", source_ref="product", target_ref="prompt")],
            ),
        )


def test_move_nodes_to_group_none_ungroups() -> None:
    graph = apply_workflow_change_set(
        EMPTY_GRAPH,
        WorkflowChangeSet(
            base_graph_revision=0,
            summary="分组后再移出",
            operations=[
                CreateNodeOp(
                    client_ref="brief",
                    node_type=GraphNodeType.CREATIVE_BRIEF,
                    title="创作要求",
                ),
                CreateGroupOp(client_ref="folder", title="组", member_refs=("brief",)),
            ],
        ),
    )
    moved = apply_workflow_change_set(
        graph,
        WorkflowChangeSet(
            base_graph_revision=graph.revision,
            summary="移出分组",
            operations=[MoveNodesToGroupOp(group_ref=None, node_refs=("brief",))],
        ),
    )
    assert moved.node("brief").group_id is None
    inverse = invert_applied_graph(graph, moved)
    restored = apply_workflow_change_set(
        moved,
        WorkflowChangeSet(
            base_graph_revision=moved.revision,
            summary="撤销移出",
            operations=inverse,
        ),
    )
    assert restored.node("brief").group_id == "folder"


def test_rename_inverse_restores_title() -> None:
    graph = apply_workflow_change_set(
        EMPTY_GRAPH,
        WorkflowChangeSet(
            base_graph_revision=0,
            summary="建节点",
            operations=[
                CreateNodeOp(
                    client_ref="brief",
                    node_type=GraphNodeType.CREATIVE_BRIEF,
                    title="创作要求",
                )
            ],
        ),
    )
    renamed = apply_workflow_change_set(
        graph,
        WorkflowChangeSet(
            base_graph_revision=graph.revision,
            summary="改名",
            operations=[RenameNodeOp(node_ref="brief", title="新标题")],
        ),
    )
    restored = apply_workflow_change_set(
        renamed,
        WorkflowChangeSet(
            base_graph_revision=renamed.revision,
            summary="撤销改名",
            operations=invert_applied_graph(graph, renamed),
        ),
    )
    assert restored.node("brief").title == "创作要求"


def test_update_node_config_can_unbind_asset() -> None:
    graph = apply_workflow_change_set(
        EMPTY_GRAPH,
        build_direct_create_template(
            image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
            reference_asset_ids=["asset-a"],
        ),
    )
    asset = next(node for node in graph.nodes if node.bound_asset_id == "asset-a")
    unbound = apply_workflow_change_set(
        graph,
        WorkflowChangeSet(
            base_graph_revision=graph.revision,
            summary="解绑素材",
            operations=[UpdateNodeConfigOp(node_ref=asset.id, config=dict(asset.config), bound_asset_id=None)],
        ),
    )
    assert unbound.node(asset.id).bound_asset_id is None
    kept = apply_workflow_change_set(
        graph,
        WorkflowChangeSet(
            base_graph_revision=graph.revision,
            summary="只改配置不解绑",
            operations=[UpdateNodeConfigOp(node_ref=asset.id, config={"label": "仍绑定"})],
        ),
    )
    assert kept.node(asset.id).bound_asset_id == "asset-a"
