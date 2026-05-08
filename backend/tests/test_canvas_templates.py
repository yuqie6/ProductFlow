from __future__ import annotations

import pytest
from pydantic import ValidationError

from productflow_backend.application.canvas_templates import (
    BUILTIN_CANVAS_TEMPLATES,
    SUPPORTED_CANVAS_TEMPLATE_NODE_TYPES,
    CanvasTemplate,
    CanvasTemplateDefaultExternalConnection,
    CanvasTemplateEdgeSpec,
    CanvasTemplateNodeSpec,
    CanvasTemplateOutputSlot,
    CanvasTemplateScenario,
    CanvasTemplateScenarioMetadata,
    CanvasTemplateSuggestedConnection,
    get_builtin_canvas_template,
    list_builtin_canvas_templates,
    validate_canvas_template,
)
from productflow_backend.domain.enums import WorkflowNodeType
from productflow_backend.domain.errors import BusinessValidationError


def _minimal_template(**overrides: object) -> CanvasTemplate:
    data = {
        "key": "test-template",
        "kind": "full_canvas",
        "title": "测试模板",
        "description": "用于模板验证测试。",
        "scenario": CanvasTemplateScenarioMetadata(
            scenario=CanvasTemplateScenario.MAIN_IMAGE,
            title="主图",
            description="主图测试场景。",
            ecommerce_stage="listing",
        ),
        "nodes": (
            CanvasTemplateNodeSpec(
                key="product",
                node_type=WorkflowNodeType.PRODUCT_CONTEXT,
                title="商品",
            ),
            CanvasTemplateNodeSpec(
                key="image",
                node_type=WorkflowNodeType.IMAGE_GENERATION,
                title="生图",
                config_json={"instruction": "生成商品图", "size": "1024x1024"},
                instruction_seed="生成商品图",
                size="1024x1024",
            ),
            CanvasTemplateNodeSpec(
                key="output",
                node_type=WorkflowNodeType.REFERENCE_IMAGE,
                title="输出",
                config_json={"role": "output", "label": "输出"},
                output_slot_label="输出",
            ),
        ),
        "edges": (
            CanvasTemplateEdgeSpec(source_node_key="product", target_node_key="image"),
            CanvasTemplateEdgeSpec(source_node_key="image", target_node_key="output"),
        ),
        "output_slots": (
            CanvasTemplateOutputSlot(
                node_key="output",
                label="输出",
                description="输出槽。",
            ),
        ),
    }
    data.update(overrides)
    return CanvasTemplate(**data)


def test_builtin_canvas_template_catalog_covers_required_ecommerce_scenarios() -> None:
    templates = list_builtin_canvas_templates()

    assert templates == BUILTIN_CANVAS_TEMPLATES
    assert len({template.key for template in templates}) == len(templates)
    assert {template.scenario.scenario for template in templates} >= {
        CanvasTemplateScenario.MAIN_IMAGE,
        CanvasTemplateScenario.SKU_VARIANT,
        CanvasTemplateScenario.MODEL_LIFESTYLE,
        CanvasTemplateScenario.SCENE_IMAGE,
        CanvasTemplateScenario.DETAIL_MATERIAL,
        CanvasTemplateScenario.CAMPAIGN_PROMOTION,
        CanvasTemplateScenario.WHITE_BACKGROUND,
    }
    assert {template.kind for template in templates} == {"full_canvas", "node_group"}


def test_builtin_canvas_templates_satisfy_v1_contract() -> None:
    for template in list_builtin_canvas_templates():
        validate_canvas_template(template)
        assert template.version == 1
        assert template.prompt_seeds
        assert template.instruction_seeds
        assert template.output_slots
        assert all(node.node_type in SUPPORTED_CANVAS_TEMPLATE_NODE_TYPES for node in template.nodes)
        assert all(
            node.config_json.get("instruction") == node.instruction_seed
            for node in template.nodes
            if node.instruction_seed is not None
        )
        assert all(
            node.config_json.get("size") == node.size
            for node in template.nodes
            if node.node_type == WorkflowNodeType.IMAGE_GENERATION and node.size is not None
        )

        nodes_by_key = {node.key: node for node in template.nodes}
        for slot in template.output_slots:
            assert nodes_by_key[slot.node_key].node_type == WorkflowNodeType.REFERENCE_IMAGE
        for hint in template.reference_input_hints:
            assert nodes_by_key[hint.node_key].node_type == WorkflowNodeType.REFERENCE_IMAGE
        for edge in template.edges:
            assert edge.source_node_key != edge.target_node_key
            assert edge.source_node_key in nodes_by_key
            assert edge.target_node_key in nodes_by_key


def test_builtin_node_group_templates_keep_readable_horizontal_spacing() -> None:
    for template in list_builtin_canvas_templates():
        if template.kind != "node_group":
            continue
        nodes = sorted(template.nodes, key=lambda node: node.position_x)

        assert all(
            next_node.position_x - current_node.position_x >= 400
            for current_node, next_node in zip(nodes, nodes[1:], strict=False)
        )


def test_builtin_catalog_expresses_dag_safe_downstream_iteration() -> None:
    main_template = get_builtin_canvas_template("ecommerce-main-image-v1")

    assert any(
        edge.source_node_key == "output" and edge.target_node_key == "iteration_image"
        for edge in main_template.edges
    )
    assert any(
        edge.source_node_key == "iteration_image" and edge.target_node_key == "iteration_output"
        for edge in main_template.edges
    )
    assert all(edge.source_node_key != edge.target_node_key for edge in main_template.edges)
    validate_canvas_template(main_template)


def test_template_validator_rejects_missing_edge_reference() -> None:
    valid = _minimal_template()
    invalid = valid.model_copy(
        update={
            "edges": (
                *valid.edges,
                CanvasTemplateEdgeSpec(source_node_key="missing", target_node_key="output"),
            )
        }
    )

    with pytest.raises(BusinessValidationError, match="连线引用了不存在的节点"):
        validate_canvas_template(invalid)


def test_template_validator_rejects_self_edge_and_cycle() -> None:
    valid = _minimal_template()
    self_edge = valid.model_copy(
        update={
            "edges": (
                *valid.edges,
                CanvasTemplateEdgeSpec(source_node_key="image", target_node_key="image"),
            )
        }
    )
    cycle = valid.model_copy(
        update={
            "edges": (
                *valid.edges,
                CanvasTemplateEdgeSpec(source_node_key="output", target_node_key="image"),
            )
        }
    )

    with pytest.raises(BusinessValidationError, match="不能连接到自身"):
        validate_canvas_template(self_edge)
    with pytest.raises(BusinessValidationError, match="不能包含循环依赖"):
        validate_canvas_template(cycle)


def test_template_validator_rejects_invalid_output_slot_reference() -> None:
    valid = _minimal_template()
    invalid = valid.model_copy(
        update={
            "output_slots": (
                CanvasTemplateOutputSlot(
                    node_key="image",
                    label="错误输出",
                    description="输出槽不能挂在生图节点上。",
                ),
            )
        }
    )

    with pytest.raises(BusinessValidationError, match="输出槽必须引用参考图节点"):
        validate_canvas_template(invalid)


def test_template_model_construction_runs_contract_validation() -> None:
    valid = _minimal_template()
    data = valid.model_dump()
    data["edges"] = (
        *valid.edges,
        CanvasTemplateEdgeSpec(source_node_key="missing", target_node_key="output"),
    )

    with pytest.raises(ValidationError, match="连线引用了不存在的节点"):
        CanvasTemplate(**data)


def test_template_validator_rejects_unsupported_node_type() -> None:
    valid = _minimal_template()
    invalid_node = CanvasTemplateNodeSpec(
        key="legacy-image-slot",
        node_type=WorkflowNodeType.REFERENCE_IMAGE,
        title="旧节点",
    ).model_copy(update={"node_type": "image_slot"})
    invalid = valid.model_copy(update={"nodes": (*valid.nodes, invalid_node)})

    with pytest.raises(BusinessValidationError, match="包含不支持的节点类型"):
        validate_canvas_template(invalid)


def test_template_validator_rejects_invalid_suggested_connection() -> None:
    valid = _minimal_template()
    missing_reference = valid.model_copy(
        update={
            "suggested_connections": (
                CanvasTemplateSuggestedConnection(
                    source_node_key="image",
                    target_node_key="missing",
                    reason="不存在的建议目标。",
                ),
            )
        }
    )
    self_reference = valid.model_copy(
        update={
            "suggested_connections": (
                CanvasTemplateSuggestedConnection(
                    source_node_key="image",
                    target_node_key="image",
                    reason="自连建议不成立。",
                ),
            )
        }
    )

    with pytest.raises(BusinessValidationError, match="连接建议引用了不存在的节点"):
        validate_canvas_template(missing_reference)
    with pytest.raises(BusinessValidationError, match="连接建议不能连接到自身"):
        validate_canvas_template(self_reference)


def test_template_validator_rejects_invalid_default_external_connection() -> None:
    valid = _minimal_template(
        kind="node_group",
        nodes=(
            CanvasTemplateNodeSpec(
                key="copy",
                node_type=WorkflowNodeType.COPY_GENERATION,
                title="文案",
            ),
            CanvasTemplateNodeSpec(
                key="reference",
                node_type=WorkflowNodeType.REFERENCE_IMAGE,
                title="参考图",
            ),
        ),
        edges=(),
        output_slots=(),
    )
    missing_reference = valid.model_copy(
        update={
            "default_external_connections": (
                CanvasTemplateDefaultExternalConnection(
                    source="existing_product_context",
                    target_node_key="missing",
                    label="自动接商品",
                    reason="目标不存在。",
                ),
            )
        }
    )
    invalid_target_type = valid.model_copy(
        update={
            "default_external_connections": (
                CanvasTemplateDefaultExternalConnection(
                    source="existing_product_context",
                    target_node_key="reference",
                    label="自动接商品",
                    reason="参考图不是生成节点。",
                ),
            )
        }
    )
    full_canvas = _minimal_template().model_copy(
        update={
            "default_external_connections": (
                CanvasTemplateDefaultExternalConnection(
                    source="existing_product_context",
                    target_node_key="image",
                    label="自动接商品",
                    reason="完整画布不声明外部连接。",
                ),
            )
        }
    )

    with pytest.raises(BusinessValidationError, match="默认外部连接引用了不存在的节点"):
        validate_canvas_template(missing_reference)
    with pytest.raises(BusinessValidationError, match="默认外部连接只能接入文案或生图节点"):
        validate_canvas_template(invalid_target_type)
    with pytest.raises(BusinessValidationError, match="只有节点组模板可以声明默认外部连接"):
        validate_canvas_template(full_canvas)


def test_template_validator_rejects_duplicate_node_keys() -> None:
    valid = _minimal_template()
    duplicate = valid.model_copy(
        update={
            "nodes": (
                *valid.nodes,
                CanvasTemplateNodeSpec(
                    key="image",
                    node_type=WorkflowNodeType.COPY_GENERATION,
                    title="重复节点",
                ),
            )
        }
    )

    with pytest.raises(BusinessValidationError, match="节点 key 不能重复"):
        validate_canvas_template(duplicate)


def test_template_validator_rejects_product_context_in_node_group() -> None:
    valid = _minimal_template()
    invalid = valid.model_copy(update={"kind": "node_group"})

    with pytest.raises(BusinessValidationError, match="节点组模板不能包含商品资料节点"):
        validate_canvas_template(invalid)


def test_template_validator_rejects_invalid_template_kind() -> None:
    valid = _minimal_template()
    invalid = valid.model_copy(update={"kind": "unknown"})

    with pytest.raises(BusinessValidationError, match="模板类型不支持"):
        validate_canvas_template(invalid)


def test_get_builtin_canvas_template_rejects_unknown_key() -> None:
    with pytest.raises(BusinessValidationError, match="画布模板不存在"):
        get_builtin_canvas_template("missing")


def test_template_node_type_allowlist_matches_current_workflow_node_types() -> None:
    assert SUPPORTED_CANVAS_TEMPLATE_NODE_TYPES == {
        WorkflowNodeType.PRODUCT_CONTEXT,
        WorkflowNodeType.REFERENCE_IMAGE,
        WorkflowNodeType.COPY_GENERATION,
        WorkflowNodeType.IMAGE_GENERATION,
    }
