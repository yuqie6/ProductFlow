from __future__ import annotations

from pathlib import Path

import pytest
from helpers import _make_demo_image_bytes

from productflow_backend.application import product_workflows
from productflow_backend.application.canvas_templates import (
    CanvasTemplate,
    CanvasTemplateNodeSpec,
    CanvasTemplateScenario,
    CanvasTemplateScenarioMetadata,
    get_builtin_canvas_template,
    list_builtin_canvas_templates,
)
from productflow_backend.application.product_workflow.node_config import normalize_workflow_node_config
from productflow_backend.application.product_workflow.templates import (
    TEMPLATE_METADATA_CONFIG_KEY,
    materialize_canvas_template_graph,
)
from productflow_backend.application.use_cases import create_product
from productflow_backend.domain.enums import WorkflowNodeType
from productflow_backend.domain.errors import BusinessValidationError


def test_copy_node_config_normalizes_known_fields_and_preserves_safe_extensions() -> None:
    normalized = normalize_workflow_node_config(
        WorkflowNodeType.COPY_GENERATION,
        {
            "instruction": "  Create a clear layout hierarchy  ",
            "requested_slots": ["  Headline  "],
            "adapter_options": {"keep": True},
        },
    )

    assert normalized == {
        "version": 2,
        "instruction": "Create a clear layout hierarchy",
        "purpose": None,
        "channel": None,
        "tone": None,
        "copy_language_hint": None,
        "output_mode": "layout_brief",
        "requested_slots": [
            {
                "key": "slot_1",
                "label": "Headline",
                "required": False,
                "hint": None,
            }
        ],
        "adapter_options": {"keep": True},
    }


def test_image_node_config_normalizes_size_hint_and_tool_options(configured_env: Path) -> None:
    normalized = normalize_workflow_node_config(
        WorkflowNodeType.IMAGE_GENERATION,
        {
            "size": "1500X800",
            "visible_text_language_hint": "  Use Vietnamese for visible text.  ",
            "tool_options": {
                "quality": "high",
                "output_format": "  ",
                "n": 2,
                "unknown": "drop",
            },
            "adapter_options": {"keep": True},
        },
    )

    assert normalized == {
        "size": "1504x800",
        "visible_text_language_hint": "Use Vietnamese for visible text.",
        "tool_options": {"quality": "high"},
        "adapter_options": {"keep": True},
    }
    assert "visible_text_language_hint" not in normalize_workflow_node_config(
        WorkflowNodeType.IMAGE_GENERATION,
        {"visible_text_language_hint": "  "},
    )


@pytest.mark.parametrize(
    "node_type",
    [WorkflowNodeType.REFERENCE_IMAGE, WorkflowNodeType.PRODUCT_CONTEXT],
)
def test_non_generation_node_config_is_shallow_copied(node_type: WorkflowNodeType) -> None:
    nested = {"keep": True}
    config = {"label": "Reference", "adapter_options": nested}

    normalized = normalize_workflow_node_config(node_type, config)

    assert normalized == config
    assert normalized is not config
    assert normalized["adapter_options"] is nested


@pytest.mark.parametrize(
    ("node_type", "config_json", "message"),
    [
        (
            WorkflowNodeType.COPY_GENERATION,
            {"requested_slots": "Headline"},
            "文案 requested_slots 必须是数组",
        ),
        (
            WorkflowNodeType.IMAGE_GENERATION,
            {"size": "bad-size"},
            "生图尺寸 必须使用 宽x高 格式，例如 1024x1024",
        ),
    ],
)
def test_invalid_generation_node_config_raises_business_validation_error(
    node_type: WorkflowNodeType,
    config_json: dict[str, object],
    message: str,
) -> None:
    with pytest.raises(BusinessValidationError, match=message):
        normalize_workflow_node_config(node_type, config_json)


def test_product_workflows_facade_exports_shared_node_config_owner() -> None:
    assert product_workflows.normalize_workflow_node_config is normalize_workflow_node_config


def test_runtime_node_create_and_update_use_shared_config_owner(configured_env: Path, db_session) -> None:
    product = create_product(
        db_session,
        name="节点配置合同商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="node-config.png",
        content_type="image/png",
    )
    created_workflow = product_workflows.create_workflow_node(
        db_session,
        product_id=product.id,
        node_type=WorkflowNodeType.COPY_GENERATION,
        title="合同文案",
        position_x=500,
        position_y=200,
        config_json={
            "instruction": "Create a layout hierarchy",
            "adapter_options": {"source": "create"},
        },
    )
    created_node = next(node for node in created_workflow.nodes if node.title == "合同文案")
    assert created_node.config_json == normalize_workflow_node_config(
        WorkflowNodeType.COPY_GENERATION,
        {
            "instruction": "Create a layout hierarchy",
            "adapter_options": {"source": "create"},
        },
    )

    updated_workflow = product_workflows.update_workflow_node(
        db_session,
        node_id=created_node.id,
        title=None,
        position_x=None,
        position_y=None,
        config_json={
            "instruction": "List three feature points",
            "requested_slots": ["Headline"],
            "adapter_options": {"source": "update"},
        },
    )
    updated_node = next(node for node in updated_workflow.nodes if node.id == created_node.id)
    assert updated_node.config_json == normalize_workflow_node_config(
        WorkflowNodeType.COPY_GENERATION,
        {
            "instruction": "List three feature points",
            "requested_slots": ["Headline"],
            "adapter_options": {"source": "update"},
        },
    )


def test_builtin_copy_configs_use_shared_owner_without_changing_explicit_mode() -> None:
    for template in list_builtin_canvas_templates():
        for node in template.nodes:
            if node.node_type == WorkflowNodeType.COPY_GENERATION:
                assert node.config_json == normalize_workflow_node_config(node.node_type, node.config_json)

    feature_template = get_builtin_canvas_template("ecommerce-feature-infographic-v1")
    feature_copy = next(node for node in feature_template.nodes if node.key == "feature_copy")
    assert feature_copy.config_json["output_mode"] == "layout_brief"


def test_builtin_materialization_normalizes_config_after_metadata(configured_env: Path, db_session) -> None:
    product = create_product(
        db_session,
        name="模板配置合同商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="template-node-config.png",
        content_type="image/png",
    )
    workflow = product_workflows.get_or_create_product_workflow(db_session, product.id)
    raw_config = {
        "instruction": "Create a clear layout hierarchy",
        "adapter_options": {"keep": True},
    }
    template = CanvasTemplate(
        key="test-node-config-materialization-v1",
        kind="node_group",
        title="节点配置测试模板",
        description="验证模板落库前的节点配置归一化。",
        source="builtin",
        scenario=CanvasTemplateScenarioMetadata(
            scenario=CanvasTemplateScenario.MAIN_IMAGE,
            title="节点配置测试",
            description="验证节点配置归一化。",
            ecommerce_stage="test",
        ),
        nodes=(
            CanvasTemplateNodeSpec(
                key="copy",
                node_type=WorkflowNodeType.COPY_GENERATION,
                title="测试文案",
                config_json=raw_config,
            ),
        ),
    )

    nodes = materialize_canvas_template_graph(db_session, workflow=workflow, template=template)

    expected_config = normalize_workflow_node_config(
        WorkflowNodeType.COPY_GENERATION,
        {
            **raw_config,
            TEMPLATE_METADATA_CONFIG_KEY: {
                "source": "builtin",
                "template_key": template.key,
                "node_key": "copy",
            },
        },
    )
    assert nodes["copy"].config_json == expected_config
