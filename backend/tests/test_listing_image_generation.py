from __future__ import annotations

from productflow_backend.application.product_workflow.graph_compiler import ImageRuntimeInput
from productflow_backend.application.product_workflow.graph_execution import (
    NO_CAPTION_ON_REFERENCE_RULE,
    _compile_image_model_prompt,
)
from productflow_backend.application.workflow_drafts.contracts import GenerationSpec
from productflow_backend.infrastructure.image.base import WorkflowImageReference, WorkflowImageRequest
from productflow_backend.infrastructure.image.responses_provider import _workflow_responses_tool_options


def _image_runtime(*, image_type_key: str, text_policy: str = "required") -> ImageRuntimeInput:
    return ImageRuntimeInput(
        node_id="image-1",
        prompt_payload={
            "design_goal": "卖点",
            "composition": {"product_share_percent": 35, "layout": "商品偏下"},
            "content": {"selling_points": ["暖手"]},
            "atmosphere": {},
            "product_fidelity": {},
            "text": {"headline": "云白瓷 · 暖手400ml"},
            "shared_rules": [],
        },
        prompt_artifact_id="art-1",
        prompt_edge_id="edge-1",
        reference_images=(),
        visual_system=None,
        visual_system_version_id=None,
        visual_overlay=None,
        generation_spec={"text_policy": text_policy, "text_language": "zh-CN"},
        variation_instruction=None,
        incoming_edge_ids=(),
        input_digest="digest",
        image_type_key=image_type_key,
    )


def _reference() -> WorkflowImageReference:
    return WorkflowImageReference(
        asset_id="asset-1",
        role="product_identity",
        label="参考图",
        filename="mug.jpg",
        mime_type="image/jpeg",
        bytes_data=b"not-an-image",
    )


def test_selling_point_prompt_forbids_caption_on_reference() -> None:
    prompt = _compile_image_model_prompt(_image_runtime(image_type_key="selling_point"))
    assert "核心卖点图" in prompt
    assert "详情卖点图" in prompt or "抠出商品" in prompt
    assert NO_CAPTION_ON_REFERENCE_RULE in prompt
    assert "原图贴字" in prompt or "只加一行字" in prompt
    assert "2 到 4" in prompt
    assert "极简大留白" in prompt
    assert "爆炸贴" in prompt
    assert "3 到 5" not in prompt


def test_hero_prompt_requires_large_product_not_empty_canvas() -> None:
    prompt = _compile_image_model_prompt(_image_runtime(image_type_key="hero", text_policy="none"))
    assert "首屏海报图" in prompt
    assert "55%–75%" in prompt or "占画面" in prompt
    assert "空洞" in prompt or "角落" in prompt
    assert "爆炸贴" in prompt or "贴满" in prompt


def test_infographic_sends_low_reference_fidelity() -> None:
    spec = GenerationSpec.model_validate(
        {
            "aspect_ratio": "3:4",
            "resolution_tier": "high",
            "quality_intent": "high",
            "reference_fidelity": "high",
            "background_intent": "auto",
            "text_policy": "required",
            "text_language": "zh-CN",
        }
    )
    infographic = WorkflowImageRequest(
        compiled_prompt="卖点图",
        generation_spec=spec,
        references=(_reference(),),
        image_type_key="selling_point",
    )
    photography = WorkflowImageRequest(
        compiled_prompt="首图",
        generation_spec=spec,
        references=(_reference(),),
        image_type_key="hero",
    )
    infographic_options = _workflow_responses_tool_options(infographic)
    photography_options = _workflow_responses_tool_options(photography)
    assert infographic_options["input_fidelity"] == "low"
    assert photography_options["input_fidelity"] == "high"
    assert infographic_options.get("action") != "edit"
    assert photography_options.get("action") != "edit"
    assert infographic_options.get("action") == "generate"
    assert photography_options.get("action") == "generate"


def test_measured_aspect_matches_requested_spec() -> None:
    spec = GenerationSpec.model_validate(
        {
            "aspect_ratio": "3:4",
            "resolution_tier": "high",
            "quality_intent": "high",
            "reference_fidelity": "high",
            "background_intent": "auto",
            "text_policy": "required",
            "text_language": "zh-CN",
        }
    )
    from productflow_backend.infrastructure.image.base import (
        aspect_mismatch_message,
        measured_aspect_matches_spec,
    )

    assert measured_aspect_matches_spec(spec, 768, 1024) is True
    assert measured_aspect_matches_spec(spec, 1448, 1086) is False
    assert "3:4" in aspect_mismatch_message(spec, 1448, 1086)
    assert "1448" in aspect_mismatch_message(spec, 1448, 1086)
