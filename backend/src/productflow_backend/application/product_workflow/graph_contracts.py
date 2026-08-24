"""schema-v3 Graph Command 的 ChangeSet 合同；config 不得带未登记或退休拓扑键。"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from pydantic import BaseModel, ConfigDict, Field, StringConstraints, model_validator

from productflow_backend.domain.enums import GraphActorType, GraphNodeType
from productflow_backend.domain.graph_catalog import FORBIDDEN_GRAPH_CONFIG_KEYS

GraphRef = Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=80)]
GraphTitle = Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=255)]

FORBIDDEN_GRAPH_TOPOLOGY_KEYS = FORBIDDEN_GRAPH_CONFIG_KEYS


class StrictGraphModel(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True)


class CreateNodeOp(StrictGraphModel):
    op: Literal["create_node"] = "create_node"
    client_ref: GraphRef
    node_type: GraphNodeType
    title: GraphTitle
    position_x: int = 0
    position_y: int = 0
    config: dict[str, Any] = Field(default_factory=dict)
    bound_asset_id: str | None = None  # image_asset 绑定身份；连线另走 connect_nodes
    group_ref: GraphRef | None = None

    @model_validator(mode="after")
    def reject_topology_keys(self) -> CreateNodeOp:
        _reject_topology_keys(self.config)
        return self


class UpdateNodeConfigOp(StrictGraphModel):
    op: Literal["update_node_config"] = "update_node_config"
    node_ref: GraphRef
    config: dict[str, Any]
    bound_asset_id: str | None = None

    @model_validator(mode="after")
    def reject_topology_keys(self) -> UpdateNodeConfigOp:
        _reject_topology_keys(self.config)
        return self


class RenameNodeOp(StrictGraphModel):
    op: Literal["rename_node"] = "rename_node"
    node_ref: GraphRef
    title: GraphTitle


class DeleteNodeOp(StrictGraphModel):
    op: Literal["delete_node"] = "delete_node"
    node_ref: GraphRef


class ConnectNodesOp(StrictGraphModel):
    op: Literal["connect_nodes"] = "connect_nodes"
    client_ref: GraphRef
    source_ref: GraphRef
    target_ref: GraphRef
    order: int = Field(default=0, ge=0)


class DisconnectEdgeOp(StrictGraphModel):
    op: Literal["disconnect_edge"] = "disconnect_edge"
    edge_ref: GraphRef


class MoveNodesOp(StrictGraphModel):
    op: Literal["move_nodes"] = "move_nodes"
    nodes: list[tuple[GraphRef, int, int]] = Field(min_length=1)


class CreateGroupOp(StrictGraphModel):
    """画布一层视觉分组，无执行状态、端口或运行行为。"""

    op: Literal["create_group"] = "create_group"
    client_ref: GraphRef
    title: GraphTitle
    member_refs: tuple[GraphRef, ...] = ()


class MoveNodesToGroupOp(StrictGraphModel):
    op: Literal["move_nodes_to_group"] = "move_nodes_to_group"
    group_ref: GraphRef | None
    node_refs: tuple[GraphRef, ...] = Field(min_length=1)


class RenameGroupOp(StrictGraphModel):
    op: Literal["rename_group"] = "rename_group"
    group_ref: GraphRef
    title: GraphTitle


class DissolveGroupOp(StrictGraphModel):
    op: Literal["dissolve_group"] = "dissolve_group"
    group_ref: GraphRef


GraphOperation = Annotated[
    CreateNodeOp
    | UpdateNodeConfigOp
    | RenameNodeOp
    | DeleteNodeOp
    | ConnectNodesOp
    | DisconnectEdgeOp
    | MoveNodesOp
    | CreateGroupOp
    | MoveNodesToGroupOp
    | RenameGroupOp
    | DissolveGroupOp,
    Field(discriminator="op"),
]


class WorkflowChangeSet(StrictGraphModel):
    """对某一 revision 的原子图变更；应用后才成为 live 图。"""

    base_graph_revision: int = Field(ge=0)
    summary: Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=500)]
    actor_type: GraphActorType = GraphActorType.USER
    operations: list[GraphOperation] = Field(min_length=1)

    @model_validator(mode="after")
    def validate_unique_create_refs(self) -> WorkflowChangeSet:
        create_refs = [
            op.client_ref
            for op in self.operations
            if isinstance(op, (CreateNodeOp, CreateGroupOp, ConnectNodesOp))
        ]
        if len(create_refs) != len(set(create_refs)):
            raise ValueError("ChangeSet 内部 client_ref 不能重复")
        return self


def _reject_topology_keys(config: dict[str, Any]) -> None:
    illegal = FORBIDDEN_GRAPH_TOPOLOGY_KEYS.intersection(config)
    if illegal:
        raise ValueError(f"节点配置不能包含拓扑字段: {', '.join(sorted(illegal))}")
