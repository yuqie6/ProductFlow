from __future__ import annotations

from base64 import b64encode
from io import BytesIO
from pathlib import Path

import pytest
from fastapi.testclient import TestClient
from helpers import (
    _execute_workflow_queue_inline,
    _login,
    _make_demo_image_bytes,
    _make_demo_image_data_url,
    _wait_for_workflow_run,
)
from PIL import Image
from pydantic import ValidationError

from productflow_backend.application.contracts import (
    CopyPayload,
    CreativeBriefPayload,
    PosterGenerationInput,
    ProductInput,
    ReferenceImageInput,
)
from productflow_backend.application.product_workflows import run_product_workflow
from productflow_backend.application.use_cases import (
    create_product,
    get_product_detail,
)
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import (
    PosterKind,
)
from productflow_backend.infrastructure.db.models import (
    AppSetting,
)
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.image.responses_provider import OpenAIResponsesImageProvider


@pytest.fixture(autouse=True)
def _execute_workflow_queue_inline_fixture(monkeypatch: pytest.MonkeyPatch) -> None:
    """Keep API workflow tests deterministic while production delivery goes through Dramatiq."""

    _execute_workflow_queue_inline(monkeypatch)


def test_prompt_settings_reach_provider_prompt_builders(configured_env: Path, monkeypatch) -> None:
    from productflow_backend.infrastructure.image.chat_service import ImageChatService, ImageChatTurn
    from productflow_backend.infrastructure.prompts import render_prompt_template
    from productflow_backend.infrastructure.text.openai_provider import OpenAITextProvider

    assert render_prompt_template(
        "示例 JSON：{\"title\":\"{title}\"}；未知：{unknown}；坏括号：{",
        {"title": "主标题"},
    ) == "示例 JSON：{\"title\":\"主标题\"}；未知：{unknown}；坏括号：{"

    session = get_session_factory()()
    try:
        session.add_all(
            [
                AppSetting(key="prompt_brief_system", value="自定义商品理解提示"),
                AppSetting(key="prompt_copy_system", value="自定义文案提示"),
                AppSetting(
                    key="prompt_poster_image_template",
                    value="自定义海报 {product_name} / {instruction} / {kind_label} / {selling_points}",
                ),
                AppSetting(
                    key="prompt_poster_image_edit_template",
                    value="自定义改图 {product_name} / {instruction} / {kind_label} / {size}",
                ),
                AppSetting(
                    key="prompt_image_chat_template",
                    value="自定义连续生图 {size} / {history_block} / {prompt}",
                ),
            ]
        )
        session.commit()
    finally:
        session.close()

    text_calls: list[dict] = []

    class DummyTextResponse:
        def __init__(self, output_text: str) -> None:
            self.output_text = output_text

    class DummyTextResponses:
        def create(self, **kwargs):
            text_calls.append(kwargs)
            if len(text_calls) == 1:
                return DummyTextResponse(
                    '{"positioning":"入门定位","audience":"新手","selling_angles":["稳","快","省"],'
                    '"taboo_phrases":[],"poster_style_hint":"白底"}'
                )
            return DummyTextResponse(
                '{"title":"标题","selling_points":["稳","快","省"],"poster_headline":"主标题","cta":"立即购买"}'
            )

    class DummyTextOpenAI:
        def __init__(self, **kwargs) -> None:
            self.responses = DummyTextResponses()

    monkeypatch.setattr("productflow_backend.infrastructure.text.openai_provider.OpenAI", DummyTextOpenAI)

    text_provider = OpenAITextProvider()
    product_input = ProductInput(
        name="测试商品",
        category="类目",
        price="9.90",
        source_note="说明",
        image_path="/tmp/a.png",
    )
    brief, _ = text_provider.generate_brief(product_input)
    text_provider.generate_copy(product_input, brief)

    assert text_calls[0]["input"][0]["content"] == "自定义商品理解提示"
    assert text_calls[1]["input"][0]["content"] == "自定义文案提示"

    source_path = configured_env / "prompt-provider-source.png"
    source_path.parent.mkdir(parents=True, exist_ok=True)
    source_path.write_bytes(_make_demo_image_bytes())
    poster_prompt = OpenAIResponsesImageProvider()._build_prompt(
        PosterGenerationInput(
            product_name="测试商品",
            category="类目",
            price="9.90",
            source_note="说明",
            instruction="强调轻便",
            title="短标题",
            selling_points=["卖点一", "卖点二", "卖点三"],
            poster_headline="主标题",
            cta="立即购买",
            source_image=source_path,
        ),
        PosterKind.MAIN_IMAGE,
        "1024x1024",
    )
    assert poster_prompt == "自定义海报 测试商品 / 强调轻便 / 主图 / 卖点一；卖点二；卖点三"

    edit_prompt = OpenAIResponsesImageProvider()._build_prompt(
        PosterGenerationInput(
            copy_prompt_mode="image_edit",
            product_name="测试商品",
            category="类目",
            price="9.90",
            source_note="说明",
            instruction="改成白底，保留主体",
            title="不应依赖的短标题",
            selling_points=["不应依赖的卖点一", "不应依赖的卖点二", "不应依赖的卖点三"],
            poster_headline="不应依赖的主标题",
            cta="不应依赖的 CTA",
            source_image=source_path,
        ),
        PosterKind.MAIN_IMAGE,
        "1024x1024",
    )
    assert edit_prompt == "自定义改图 测试商品 / 改成白底，保留主体 / 主图 / 1024x1024"

    chat_prompt = ImageChatService()._build_prompt(
        "改成白底",
        [ImageChatTurn(role="user", content="先做一个主图")],
        "1024x1024",
    )
    assert "自定义连续生图 1024x1024" in chat_prompt
    assert "用户：先做一个主图" in chat_prompt
    assert chat_prompt.endswith("改成白底")

def test_ai_payload_normalizes_scalar_text_lists_without_swallowing_malformed_values() -> None:
    brief = CreativeBriefPayload.model_validate(
        {
            "positioning": ["摄影入门工具", "桌面拍摄辅助"],
            "audience": ["摄影入门用户", "小红书图文内容创作者"],
            "selling_angles": ["上手快", "构图稳", "出片自然"],
            "taboo_phrases": [],
            "poster_style_hint": ["干净明亮", "真实生活感"],
        }
    )
    copy = CopyPayload.model_validate(
        {
            "title": ["手机摄影支架", "新手也能拍得稳"],
            "selling_points": ["上手快", "构图稳", "出片自然"],
            "poster_headline": ["新手拍照", "画面更稳"],
            "cta": ["马上试试", "轻松出片"],
        }
    )

    assert brief.positioning == "摄影入门工具、桌面拍摄辅助"
    assert brief.audience == "摄影入门用户、小红书图文内容创作者"
    assert brief.poster_style_hint == "干净明亮、真实生活感"
    assert copy.title == "手机摄影支架、新手也能拍得稳"
    assert copy.poster_headline == "新手拍照、画面更稳"
    assert copy.cta == "马上试试、轻松出片"

    for bad_value in ([], ["摄影入门用户", ""], [{"label": "摄影入门用户"}]):
        with pytest.raises(ValidationError):
            CreativeBriefPayload.model_validate(
                {
                    "positioning": "摄影入门工具",
                    "audience": bad_value,
                    "selling_angles": ["上手快", "构图稳", "出片自然"],
                    "taboo_phrases": [],
                    "poster_style_hint": "干净明亮",
                }
            )
        with pytest.raises(ValidationError):
            CopyPayload.model_validate(
                {
                    "title": bad_value,
                    "selling_points": ["上手快", "构图稳", "出片自然"],
                    "poster_headline": "新手拍照更稳",
                    "cta": "马上试试",
                }
            )

def test_product_workflow_copy_run_normalizes_provider_scalar_lists(configured_env: Path, monkeypatch) -> None:
    class ListAudienceTextProvider:
        provider_name = "list-audience"
        prompt_version = "test-v1"

        def generate_brief(self, product: ProductInput) -> tuple[CreativeBriefPayload, str]:
            return (
                CreativeBriefPayload.model_validate(
                    {
                        "positioning": f"{product.name} 的内容创作场景",
                        "audience": ["摄影入门用户", "小红书图文内容创作者"],
                        "selling_angles": ["上手快", "画面稳", "适合图文内容"],
                        "taboo_phrases": [],
                        "poster_style_hint": "清爽真实",
                    }
                ),
                "list-audience-brief",
            )

        def generate_copy(
            self,
            product: ProductInput,
            brief: CreativeBriefPayload,
            instruction: str | None = None,
            reference_images: list[ReferenceImageInput] | None = None,
        ) -> tuple[CopyPayload, str]:
            return (
                CopyPayload.model_validate(
                    {
                        "title": [product.name, "新手拍摄更稳"],
                        "selling_points": [
                            f"适合{brief.audience}",
                            "手机拍摄角度更稳定",
                            "日常图文内容更好出片",
                        ],
                        "poster_headline": ["新手也能", "拍出稳定画面"],
                        "cta": ["马上", "试试"],
                    }
                ),
                "list-audience-copy",
            )

    monkeypatch.setattr(
        "productflow_backend.application.product_workflows.get_text_provider",
        lambda: ListAudienceTextProvider(),
    )

    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "手机摄影支架"},
        files={"image": ("tripod.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    run_response = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert run_response.status_code == 200
    assert run_response.json()["runs"][0]["status"] == "running"
    workflow_payload = _wait_for_workflow_run(client, product_id, status="succeeded")
    copy_node = next(node for node in workflow_payload["nodes"] if node["node_type"] == "copy_generation")
    assert copy_node["output_json"]["title"] == "手机摄影支架、新手拍摄更稳"
    assert copy_node["output_json"]["poster_headline"] == "新手也能、拍出稳定画面"
    assert copy_node["output_json"]["cta"] == "马上、试试"
    assert "摄影入门用户、小红书图文内容创作者" in " ".join(copy_node["output_json"]["selling_points"])

    product_response = client.get(f"/api/products/{product_id}")
    assert product_response.status_code == 200
    latest_brief = product_response.json()["latest_brief"]
    assert latest_brief["payload"]["audience"] == "摄影入门用户、小红书图文内容创作者"

def test_mock_image_provider_does_not_read_runtime_settings_during_generation(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.infrastructure.image.mock_provider import MockImageProvider

    source_path = configured_env / "mock-thread-safe-source.png"
    source_path.parent.mkdir(parents=True, exist_ok=True)
    source_path.write_bytes(_make_demo_image_bytes())
    provider = MockImageProvider()

    def fail_runtime_settings_lookup():
        raise AssertionError("runtime settings should be resolved before provider worker execution")

    monkeypatch.setattr(
        "productflow_backend.infrastructure.image.mock_provider.get_runtime_settings",
        fail_runtime_settings_lookup,
    )

    generated, model = provider.generate_poster_image(
        PosterGenerationInput(
            product_name="线程安全测试商品",
            category="测试类目",
            price="99",
            source_note="测试说明",
            instruction="生成测试图",
            title="测试标题",
            selling_points=["卖点一", "卖点二", "卖点三"],
            poster_headline="测试主标题",
            cta="立即测试",
            source_image=source_path,
            image_size="512x512",
        ),
        PosterKind.MAIN_IMAGE,
    )

    assert model == "mock-image-v1"
    assert generated.width == 512
    assert generated.height == 512
    assert generated.bytes_data

def test_image_generation_without_copy_link_uses_image_edit_prompt_mode(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.infrastructure.image.base import GeneratedImagePayload
    from productflow_backend.presentation.api import create_app

    session = get_session_factory()()
    try:
        session.add(AppSetting(key="poster_generation_mode", value="generated"))
        session.commit()
    finally:
        session.close()

    captured_inputs: list[PosterGenerationInput] = []

    class CapturingImageProvider:
        provider_name = "capturing"
        prompt_version = "capturing-v1"

        def generate_poster_image(
            self,
            poster: PosterGenerationInput,
            kind: PosterKind,
        ) -> tuple[GeneratedImagePayload, str]:
            captured_inputs.append(poster)
            return (
                GeneratedImagePayload(
                    kind=kind,
                    bytes_data=_make_demo_image_bytes(),
                    mime_type="image/png",
                    width=800,
                    height=800,
                    variant_label=f"capturing-{poster.copy_prompt_mode}",
                ),
                "capturing-v1",
            )

    monkeypatch.setattr(
        "productflow_backend.application.product_workflows.get_image_provider",
        CapturingImageProvider,
    )

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "露营杯"},
        files={"image": ("cup.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    workflow = workflow_response.json()
    copy_node = next(node for node in workflow["nodes"] if node["node_type"] == "copy_generation")
    image_node = next(node for node in workflow["nodes"] if node["node_type"] == "image_generation")

    deleted_copy = client.delete(f"/api/workflow-nodes/{copy_node['id']}")
    assert deleted_copy.status_code == 200
    patched_image = client.patch(
        f"/api/workflow-nodes/{image_node['id']}",
        json={"config_json": {"instruction": "基于商品图改成暖色露营场景", "size": "1024x1024"}},
    )
    assert patched_image.status_code == 200

    selected_run = client.post(
        f"/api/products/{product_id}/workflow/run",
        json={"start_node_id": image_node["id"]},
    )
    assert selected_run.status_code == 200
    payload = _wait_for_workflow_run(client, product_id, status="succeeded")
    image_output = next(node for node in payload["nodes"] if node["id"] == image_node["id"])["output_json"]

    assert image_output["context_summary"]["copy_prompt_mode"] == "image_edit"
    assert image_output["copy_set_id"]
    assert len(captured_inputs) == 1
    assert captured_inputs[0].copy_prompt_mode == "image_edit"
    assert captured_inputs[0].instruction and "暖色露营场景" in captured_inputs[0].instruction

def test_image_session_openai_responses_uses_explicit_branch_context(
    configured_env: Path,
    monkeypatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    monkeypatch.setenv("IMAGE_PROVIDER_KIND", "openai_responses")
    monkeypatch.setenv("IMAGE_BASE_URL", "https://example.test/v1")
    monkeypatch.setenv("IMAGE_API_KEY", "demo-api-key")
    monkeypatch.setenv("IMAGE_GENERATE_MODEL", "gpt-5.4")
    get_settings.cache_clear()

    calls: list[dict] = []
    client_kwargs: list[dict] = []
    encoded_result = _make_demo_image_data_url().split(",", maxsplit=1)[1]

    class DummyImageGenerationCall:
        type = "image_generation_call"

        def __init__(self, index: int) -> None:
            self.id = f"ig_{index}"
            self.result = encoded_result
            self.revised_prompt = f"revised prompt {index}"

        def model_dump(self, *, mode: str, exclude_none: bool) -> dict[str, str]:
            return {
                "id": self.id,
                "type": self.type,
                "status": "completed",
                "revised_prompt": self.revised_prompt,
                "result": self.result,
            }

    class DummyResponse:
        def __init__(self, index: int) -> None:
            self.id = f"resp_{index}"
            self.output = [DummyImageGenerationCall(index)]

        def model_dump(self, *, mode: str, exclude_none: bool) -> dict:
            return {
                "id": self.id,
                "output": [output.model_dump(mode=mode, exclude_none=exclude_none) for output in self.output],
            }

    class DummyResponses:
        def create(self, **kwargs):
            calls.append(kwargs)
            return DummyResponse(len(calls))

    class DummyOpenAI:
        def __init__(self, **kwargs) -> None:
            client_kwargs.append(kwargs)
            self.responses = DummyResponses()

    monkeypatch.setattr("productflow_backend.infrastructure.image.responses_provider.OpenAI", DummyOpenAI)

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post("/api/image-sessions", json={"title": "Responses 连续生图"})
    assert created.status_code == 201
    session_id = created.json()["id"]

    upload = client.post(
        f"/api/image-sessions/{session_id}/reference-images",
        files={"reference_images": ("sample.png", _make_demo_image_bytes(), "image/png")},
    )
    assert upload.status_code == 200
    reference_id = next(asset["id"] for asset in upload.json()["assets"] if asset["kind"] == "reference_upload")

    first = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "生成日漫风商品场景", "size": "1024x1024"},
    )
    assert first.status_code == 202
    first_round = first.json()["rounds"][-1]
    assert first_round["provider_name"] == "openai-responses"
    assert first_round["provider_response_id"] == "resp_1"
    assert first_round["previous_response_id"] is None
    assert first_round["image_generation_call_id"] == "ig_1"
    first_asset_id = first_round["generated_asset"]["id"]

    second_without_base = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "保持主体，把背景改成晴天街角", "size": "1024x1024"},
    )
    assert second_without_base.status_code == 400
    assert second_without_base.json()["detail"] == "后续生图必须选择一张本会话已生成图片作为基图"

    branched = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={
            "prompt": "只从第一张和手动选择的参考图继续",
            "size": "1024x1024",
            "base_asset_id": first_asset_id,
            "selected_reference_asset_ids": [reference_id],
        },
    )
    assert branched.status_code == 202
    branched_round = branched.json()["rounds"][-1]
    assert branched_round["provider_response_id"] == "resp_2"
    assert branched_round["previous_response_id"] is None
    assert branched_round["base_asset_id"] == first_asset_id
    assert branched_round["selected_reference_asset_ids"] == [reference_id]

    assert client_kwargs[0] == {"api_key": "demo-api-key", "base_url": "https://example.test/v1"}
    assert calls[0]["model"] == "gpt-5.4"
    assert calls[0]["tools"] == [{"type": "image_generation", "size": "1024x1024"}]
    assert "previous_response_id" not in calls[0]
    assert "previous_response_id" not in calls[1]
    assert isinstance(calls[0]["input"], str)
    branch_content = calls[1]["input"][0]["content"]
    assert branch_content[0]["type"] == "input_text"
    branch_images = [item for item in branch_content if item["type"] == "input_image"]
    assert len(branch_images) == 2
    assert all(item["image_url"].startswith("data:image/png;base64,") for item in branch_images)
    assert "/images/generations" not in str(calls)
    assert "/images/edits" not in str(calls)

def test_openai_responses_poster_provider_uses_image_generation_tool(
    configured_env: Path,
    monkeypatch,
) -> None:
    monkeypatch.setenv("IMAGE_BASE_URL", "https://example.test/v1")
    monkeypatch.setenv("IMAGE_API_KEY", "demo-api-key")
    monkeypatch.setenv("IMAGE_GENERATE_MODEL", "gpt-5.4")
    get_settings.cache_clear()

    calls: list[dict] = []
    encoded_result = _make_demo_image_data_url().split(",", maxsplit=1)[1]

    class DummyImageGenerationCall:
        type = "image_generation_call"
        id = "ig_poster"
        result = encoded_result

        def model_dump(self, *, mode: str, exclude_none: bool) -> dict[str, str]:
            return {"id": self.id, "type": self.type, "result": self.result}

    class DummyResponse:
        id = "resp_poster"
        output = [DummyImageGenerationCall()]

        def model_dump(self, *, mode: str, exclude_none: bool) -> dict:
            return {"id": self.id, "output": [self.output[0].model_dump(mode=mode, exclude_none=exclude_none)]}

    class DummyResponses:
        def create(self, **kwargs):
            calls.append(kwargs)
            return DummyResponse()

    class DummyOpenAI:
        def __init__(self, **kwargs) -> None:
            self.responses = DummyResponses()

    monkeypatch.setattr("productflow_backend.infrastructure.image.responses_provider.OpenAI", DummyOpenAI)

    source_path = configured_env / "provider-source.png"
    reference_path = configured_env / "provider-reference.png"
    source_path.parent.mkdir(parents=True, exist_ok=True)
    source_path.write_bytes(_make_demo_image_bytes())
    reference_path.write_bytes(_make_demo_image_bytes())

    provider = OpenAIResponsesImageProvider()
    generated_image, model_name = provider.generate_poster_image(
        poster=PosterGenerationInput(
            product_name="测试商品",
            category="测试类目",
            price="9.90",
            source_note="防水牛津布，适合通勤和短途出差。",
            instruction="背景更干净，强调收纳空间。",
            image_size="1536x1024",
            tool_options={"quality": "high", "output_format": "webp"},
            title="测试标题",
            selling_points=["卖点1", "卖点2", "卖点3"],
            poster_headline="测试海报标题",
            cta="立即购买",
            source_image=source_path,
            reference_images=[
                ReferenceImageInput(
                    path=reference_path,
                    mime_type="image/png",
                    filename="reference.png",
                )
            ],
        ),
        kind=PosterKind.MAIN_IMAGE,
    )

    assert generated_image.mime_type == "image/png"
    assert (generated_image.width, generated_image.height) == (800, 800)
    assert model_name == "gpt-5.4"
    payload = calls[0]
    assert payload["model"] == "gpt-5.4"
    assert payload["tools"] == [
        {"type": "image_generation", "size": "1536x1024", "quality": "high", "output_format": "webp"}
    ]
    content = payload["input"][0]["content"]
    assert content[0]["type"] == "input_text"
    prompt_text = content[0]["text"]
    assert "用户要求：背景更干净，强调收纳空间。" in prompt_text
    assert "- 补充说明：防水牛津布，适合通勤和短途出差。" in prompt_text
    assert "- 参考图片数量：2" in prompt_text
    assert len([item for item in content if item["type"] == "input_image"]) == 2
    assert "/images/generations" not in str(payload)
    assert "/images/edits" not in str(payload)
    assert "background" not in payload


def test_openai_responses_image_tool_optional_fields_are_omitted_until_configured(
    configured_env: Path,
    monkeypatch,
) -> None:
    monkeypatch.setenv("IMAGE_BASE_URL", "https://example.test/v1")
    monkeypatch.setenv("IMAGE_API_KEY", "demo-api-key")
    monkeypatch.setenv("IMAGE_GENERATE_MODEL", "gpt-5.4")
    get_settings.cache_clear()

    calls: list[dict] = []
    encoded_result = _make_demo_image_data_url().split(",", maxsplit=1)[1]

    class DummyImageGenerationCall:
        type = "image_generation_call"
        id = "ig_tool"
        result = encoded_result

        def model_dump(self, *, mode: str, exclude_none: bool) -> dict[str, str]:
            return {"id": self.id, "type": self.type, "result": self.result}

    class DummyResponse:
        id = "resp_tool"
        output = [DummyImageGenerationCall()]

        def model_dump(self, *, mode: str, exclude_none: bool) -> dict:
            return {"id": self.id, "output": [self.output[0].model_dump(mode=mode, exclude_none=exclude_none)]}

    class DummyResponses:
        def create(self, **kwargs):
            calls.append(kwargs)
            return DummyResponse()

    class DummyOpenAI:
        def __init__(self, **kwargs) -> None:
            self.responses = DummyResponses()

    monkeypatch.setattr("productflow_backend.infrastructure.image.responses_provider.OpenAI", DummyOpenAI)

    from productflow_backend.infrastructure.image.responses_provider import OpenAIResponsesImageClient

    OpenAIResponsesImageClient().generate_image(prompt="默认 payload", size="1024x1024")

    assert calls[-1]["model"] == "gpt-5.4"
    assert calls[-1]["tools"] == [{"type": "image_generation", "size": "1024x1024"}]
    assert "background" not in calls[-1]
    assert "tool_choice" not in calls[-1]

    session = get_session_factory()()
    try:
        session.add_all(
            [
                AppSetting(
                    key="image_tool_allowed_fields",
                    value=(
                        "model,quality,output_format,output_compression,background,moderation,action,"
                        "input_fidelity,partial_images,n"
                    ),
                ),
                AppSetting(key="image_tool_model", value="gpt-image-2"),
                AppSetting(key="image_tool_quality", value="high"),
                AppSetting(key="image_tool_output_format", value="jpeg"),
                AppSetting(key="image_tool_output_compression", value="80"),
                AppSetting(key="image_tool_background", value="transparent"),
                AppSetting(key="image_tool_moderation", value="low"),
                AppSetting(key="image_tool_action", value="generate"),
                AppSetting(key="image_tool_input_fidelity", value="high"),
                AppSetting(key="image_tool_partial_images", value="2"),
                AppSetting(key="image_tool_n", value="3"),
            ]
        )
        session.commit()
    finally:
        session.close()

    OpenAIResponsesImageClient().generate_image(prompt="带可选字段", size="1024x1536")

    assert calls[-1]["tools"] == [
        {
            "type": "image_generation",
            "size": "1024x1536",
            "model": "gpt-image-2",
            "quality": "high",
            "output_format": "jpeg",
            "output_compression": 80,
            "background": "transparent",
            "moderation": "low",
            "action": "generate",
            "input_fidelity": "high",
            "partial_images": 2,
            "n": 3,
        }
    ]
    assert "tool_choice" not in calls[-1]


def test_openai_responses_image_client_polls_background_response_and_reports_progress(
    configured_env: Path,
    monkeypatch,
) -> None:
    monkeypatch.setenv("IMAGE_API_KEY", "demo-api-key")
    monkeypatch.setenv("IMAGE_GENERATE_MODEL", "gpt-5.4")
    monkeypatch.setenv("IMAGE_RESPONSES_BACKGROUND_ENABLED", "true")
    get_settings.cache_clear()

    encoded_result = _make_demo_image_data_url().split(",", maxsplit=1)[1]
    calls: list[dict] = []
    retrieved: list[str] = []
    progress_events: list[dict] = []

    class DummyImageGenerationCall:
        type = "image_generation_call"
        id = "ig_background"
        result = encoded_result

        def model_dump(self, *, mode: str, exclude_none: bool) -> dict[str, str]:
            return {"id": self.id, "type": self.type, "result": self.result}

    class DummyResponse:
        def __init__(self, status: str, output: list | None = None) -> None:
            self.id = "resp_background"
            self.status = status
            self.output = output or []

        def model_dump(self, *, mode: str, exclude_none: bool) -> dict:
            return {
                "id": self.id,
                "status": self.status,
                "output": [
                    output.model_dump(mode=mode, exclude_none=exclude_none)
                    for output in self.output
                ],
            }

    class DummyResponses:
        def create(self, **kwargs):
            calls.append(kwargs)
            return DummyResponse("queued")

        def retrieve(self, response_id: str):
            retrieved.append(response_id)
            return DummyResponse("completed", [DummyImageGenerationCall()])

    class DummyOpenAI:
        def __init__(self, **kwargs) -> None:
            self.responses = DummyResponses()

    monkeypatch.setattr("productflow_backend.infrastructure.image.responses_provider.OpenAI", DummyOpenAI)
    monkeypatch.setattr("productflow_backend.infrastructure.image.responses_provider.sleep", lambda seconds: None)

    from productflow_backend.infrastructure.image.responses_provider import OpenAIResponsesImageClient

    result = OpenAIResponsesImageClient().generate_image(
        prompt="后台生成",
        size="1024x1024",
        progress_callback=progress_events.append,
    )

    assert calls[0]["background"] is True
    assert retrieved == ["resp_background"]
    assert result.provider_response_id == "resp_background"
    assert result.image_generation_call_id == "ig_background"
    assert [event["provider_response_status"] for event in progress_events] == ["queued", "completed"]
    assert progress_events[-1]["provider_response"]["output"][0]["result"].startswith("<base64 omitted")


def test_openai_responses_image_client_falls_back_when_background_is_unsupported(
    configured_env: Path,
    monkeypatch,
) -> None:
    monkeypatch.setenv("IMAGE_API_KEY", "demo-api-key")
    monkeypatch.setenv("IMAGE_GENERATE_MODEL", "gpt-5.4")
    monkeypatch.setenv("IMAGE_RESPONSES_BACKGROUND_ENABLED", "true")
    get_settings.cache_clear()

    encoded_result = _make_demo_image_data_url().split(",", maxsplit=1)[1]
    calls: list[dict] = []

    class DummyImageGenerationCall:
        type = "image_generation_call"
        id = "ig_sync_fallback"
        result = encoded_result

        def model_dump(self, *, mode: str, exclude_none: bool) -> dict[str, str]:
            return {"id": self.id, "type": self.type, "result": self.result}

    class DummyResponse:
        id = "resp_sync_fallback"
        output = [DummyImageGenerationCall()]

        def model_dump(self, *, mode: str, exclude_none: bool) -> dict:
            return {"id": self.id, "output": [self.output[0].model_dump(mode=mode, exclude_none=exclude_none)]}

    class DummyResponses:
        def create(self, **kwargs):
            calls.append(kwargs)
            if kwargs.get("background") is True:
                raise RuntimeError("unexpected keyword argument: background")
            return DummyResponse()

    class DummyOpenAI:
        def __init__(self, **kwargs) -> None:
            self.responses = DummyResponses()

    monkeypatch.setattr("productflow_backend.infrastructure.image.responses_provider.OpenAI", DummyOpenAI)

    from productflow_backend.infrastructure.image.responses_provider import OpenAIResponsesImageClient

    result = OpenAIResponsesImageClient().generate_image(prompt="兼容同步响应", size="1024x1024")

    assert len(calls) == 2
    assert calls[0]["background"] is True
    assert "background" not in calls[1]
    assert result.provider_response_id == "resp_sync_fallback"
    assert result.image_generation_call_id == "ig_sync_fallback"


def test_openai_responses_image_client_wraps_background_poll_errors(
    configured_env: Path,
    monkeypatch,
) -> None:
    monkeypatch.setenv("IMAGE_API_KEY", "demo-api-key")
    monkeypatch.setenv("IMAGE_GENERATE_MODEL", "gpt-5.4")
    monkeypatch.setenv("IMAGE_RESPONSES_BACKGROUND_ENABLED", "true")
    get_settings.cache_clear()

    class DummyResponse:
        id = "resp_poll_error"
        status = "queued"
        output = []

        def model_dump(self, *, mode: str, exclude_none: bool) -> dict:
            return {"id": self.id, "status": self.status, "output": []}

    class DummyResponses:
        def create(self, **kwargs):
            return DummyResponse()

        def retrieve(self, response_id: str):
            raise RuntimeError("raw provider failure with secret material")

    class DummyOpenAI:
        def __init__(self, **kwargs) -> None:
            self.responses = DummyResponses()

    monkeypatch.setattr("productflow_backend.infrastructure.image.responses_provider.OpenAI", DummyOpenAI)
    monkeypatch.setattr("productflow_backend.infrastructure.image.responses_provider.sleep", lambda seconds: None)

    from productflow_backend.infrastructure.image.responses_provider import OpenAIResponsesImageClient

    with pytest.raises(RuntimeError) as exc_info:
        OpenAIResponsesImageClient().generate_image(prompt="轮询失败", size="1024x1024")

    assert str(exc_info.value) == "图片供应商请求失败，请检查供应商配置后重试"
    assert "secret material" not in str(exc_info.value)


def test_openai_responses_image_client_retries_without_optional_fields_and_records_note(
    configured_env: Path,
    monkeypatch,
) -> None:
    monkeypatch.setenv("IMAGE_BASE_URL", "https://example.test/v1")
    monkeypatch.setenv("IMAGE_API_KEY", "demo-api-key")
    monkeypatch.setenv("IMAGE_GENERATE_MODEL", "gpt-5.4")
    get_settings.cache_clear()

    calls: list[dict] = []
    encoded_result = _make_demo_image_data_url().split(",", maxsplit=1)[1]

    class DummyImageGenerationCall:
        type = "image_generation_call"
        id = "ig_fallback"
        result = encoded_result
        size = "1024x1024"

        def model_dump(self, *, mode: str, exclude_none: bool) -> dict[str, str]:
            return {"id": self.id, "type": self.type, "result": self.result, "size": self.size}

    class DummyResponse:
        id = "resp_fallback"
        output = [DummyImageGenerationCall()]

        def model_dump(self, *, mode: str, exclude_none: bool) -> dict:
            return {"id": self.id, "output": [self.output[0].model_dump(mode=mode, exclude_none=exclude_none)]}

    class DummyResponses:
        def create(self, **kwargs):
            calls.append(kwargs)
            if len(calls) == 1:
                raise RuntimeError("raw provider 400 unsupported field image_tool_quality")
            return DummyResponse()

    class DummyOpenAI:
        def __init__(self, **kwargs) -> None:
            self.responses = DummyResponses()

    monkeypatch.setattr("productflow_backend.infrastructure.image.responses_provider.OpenAI", DummyOpenAI)

    from productflow_backend.infrastructure.image.responses_provider import OpenAIResponsesImageClient

    result = OpenAIResponsesImageClient().generate_image(
        prompt="带每轮字段",
        size="1024x1024",
        tool_options={"quality": "high", "output_format": "webp", "n": 2},
    )

    assert len(calls) == 2
    assert calls[0]["tools"] == [
        {"type": "image_generation", "size": "1024x1024", "quality": "high", "output_format": "webp", "n": 2}
    ]
    assert calls[1]["tools"] == [{"type": "image_generation", "size": "1024x1024"}]
    assert result.provider_request_json["_productflow"]["fallback_used"] is True
    assert result.provider_output_json["_productflow"]["notes"] == [
        {"kind": "fallback", "message": "供应商不支持部分参数，已按基础参数完成。"}
    ]
    assert "unsupported field" not in str(result.provider_output_json)


def test_openai_responses_image_client_records_provider_adjusted_note(
    configured_env: Path,
    monkeypatch,
) -> None:
    monkeypatch.setenv("IMAGE_BASE_URL", "https://example.test/v1")
    monkeypatch.setenv("IMAGE_API_KEY", "demo-api-key")
    monkeypatch.setenv("IMAGE_GENERATE_MODEL", "gpt-5.4")
    get_settings.cache_clear()

    encoded_result = _make_demo_image_data_url().split(",", maxsplit=1)[1]

    class DummyImageGenerationCall:
        type = "image_generation_call"
        id = "ig_adjusted"
        result = encoded_result
        output_format = "png"
        quality = "auto"

        def model_dump(self, *, mode: str, exclude_none: bool) -> dict[str, str]:
            return {
                "id": self.id,
                "type": self.type,
                "result": self.result,
                "output_format": self.output_format,
                "quality": self.quality,
            }

    class DummyResponse:
        id = "resp_adjusted"
        output = [DummyImageGenerationCall()]

        def model_dump(self, *, mode: str, exclude_none: bool) -> dict:
            return {"id": self.id, "output": [self.output[0].model_dump(mode=mode, exclude_none=exclude_none)]}

    class DummyResponses:
        def create(self, **kwargs):
            return DummyResponse()

    class DummyOpenAI:
        def __init__(self, **kwargs) -> None:
            self.responses = DummyResponses()

    monkeypatch.setattr("productflow_backend.infrastructure.image.responses_provider.OpenAI", DummyOpenAI)

    from productflow_backend.infrastructure.image.responses_provider import OpenAIResponsesImageClient

    result = OpenAIResponsesImageClient().generate_image(
        prompt="provider 调整字段",
        size="1024x1024",
        tool_options={"quality": "high", "output_format": "webp"},
    )

    metadata = result.provider_output_json["_productflow"]
    assert metadata["effective_image_tool"]["output_format"] == "png"
    assert metadata["notes"][0]["kind"] == "provider_adjusted"
    assert metadata["notes"][0]["fields"] == ["quality", "output_format"]


def test_openai_responses_image_client_sanitizes_client_initialization_errors(
    configured_env: Path,
    monkeypatch,
) -> None:
    monkeypatch.setenv("IMAGE_BASE_URL", "https://secret-provider.example/v1")
    monkeypatch.setenv("IMAGE_API_KEY", "sk-sensitive")
    monkeypatch.setenv("IMAGE_GENERATE_MODEL", "gpt-5.4")
    get_settings.cache_clear()

    class DummyOpenAI:
        def __init__(self, **kwargs) -> None:
            raise RuntimeError(f"raw provider init failed: {kwargs}")

    monkeypatch.setattr("productflow_backend.infrastructure.image.responses_provider.OpenAI", DummyOpenAI)

    from productflow_backend.infrastructure.image.responses_provider import OpenAIResponsesImageClient

    with pytest.raises(RuntimeError) as error:
        OpenAIResponsesImageClient().generate_image(prompt="初始化失败", size="1024x1024")

    assert str(error.value) == "图片供应商请求失败，请检查供应商配置后重试"
    assert isinstance(error.value.__cause__, RuntimeError)
    assert "sk-sensitive" not in str(error.value)
    assert "secret-provider" not in str(error.value)


def test_openai_responses_image_client_infers_mime_type_from_returned_bytes(
    configured_env: Path,
    monkeypatch,
) -> None:
    monkeypatch.setenv("IMAGE_BASE_URL", "https://example.test/v1")
    monkeypatch.setenv("IMAGE_API_KEY", "demo-api-key")
    monkeypatch.setenv("IMAGE_GENERATE_MODEL", "gpt-5.4")
    get_settings.cache_clear()

    buffer = BytesIO()
    Image.new("RGB", (16, 16), (255, 255, 255)).save(buffer, format="JPEG")
    encoded_result = b64encode(buffer.getvalue()).decode("utf-8")

    class DummyImageGenerationCall:
        type = "image_generation_call"
        id = "ig_jpeg"
        result = encoded_result
        output_format = "png"

        def model_dump(self, *, mode: str, exclude_none: bool) -> dict[str, str]:
            return {
                "id": self.id,
                "type": self.type,
                "result": self.result,
                "output_format": self.output_format,
            }

    class DummyResponse:
        id = "resp_jpeg"
        output = [DummyImageGenerationCall()]

        def model_dump(self, *, mode: str, exclude_none: bool) -> dict:
            return {"id": self.id, "output": [self.output[0].model_dump(mode=mode, exclude_none=exclude_none)]}

    class DummyResponses:
        def create(self, **kwargs):
            return DummyResponse()

    class DummyOpenAI:
        def __init__(self, **kwargs) -> None:
            self.responses = DummyResponses()

    monkeypatch.setattr("productflow_backend.infrastructure.image.responses_provider.OpenAI", DummyOpenAI)

    from productflow_backend.infrastructure.image.responses_provider import OpenAIResponsesImageClient

    result = OpenAIResponsesImageClient().generate_image(prompt="返回 JPEG", size="1024x1024")

    assert result.mime_type == "image/jpeg"

def test_generated_poster_mode_uses_image_provider(
    db_session,
    configured_env: Path,
    monkeypatch,
) -> None:
    monkeypatch.setenv("POSTER_GENERATION_MODE", "generated")
    monkeypatch.setenv("IMAGE_PROVIDER_KIND", "mock")
    get_settings.cache_clear()

    product = create_product(
        db_session,
        name="便携榨汁杯",
        category="小家电",
        price="89.00",
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="juicer.png",
        content_type="image/png",
        reference_image_uploads=[
            (_make_demo_image_bytes(), "ref-1.png", "image/png"),
            (_make_demo_image_bytes(), "ref-2.png", "image/png"),
        ],
    )

    run_product_workflow(db_session, product_id=product.id)
    db_session.expire_all()

    product_after_poster = get_product_detail(db_session, product.id)
    assert product_after_poster.poster_variants
    assert all(
        "workflow:mock:mock-generated-r1:mock-image-v1" in poster.template_name
        for poster in product_after_poster.poster_variants
    )

def test_default_image_prompts_are_low_pollution_context_carriers(configured_env: Path) -> None:
    from productflow_backend.infrastructure.image.chat_service import ImageChatService

    prompt = OpenAIResponsesImageProvider()._build_prompt(
        PosterGenerationInput(
            copy_prompt_mode="image_edit",
            product_name="",
            instruction="画一张蓝色抽象渐变",
            image_size="1280x720",
            title="不应注入的占位标题",
            selling_points=["不应注入的占位卖点"],
            poster_headline="不应注入的占位主标题",
            cta="不应注入的 CTA",
        ),
        PosterKind.MAIN_IMAGE,
        "1280x720",
    )
    chat_prompt = ImageChatService()._build_prompt("画一张蓝色抽象渐变", [], "1280x720")

    forbidden = ["电商海报", "继承输入参考图", "商品主体", "主标题", "卖点", "CTA", "价格标签"]
    assert all(term not in prompt for term in forbidden)
    assert all(term not in chat_prompt for term in ["继承已经确定", "主体", "构图与材质"])
    assert "不应注入" not in prompt
    assert "画一张蓝色抽象渐变" in prompt
    assert "无显式上游上下文" in prompt
    assert "1280x720" in chat_prompt
