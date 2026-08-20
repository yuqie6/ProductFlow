from __future__ import annotations

from base64 import b64encode
from io import BytesIO
from types import SimpleNamespace

import pytest
from helpers import _make_demo_image_bytes, _make_demo_image_data_url
from PIL import Image

from productflow_backend.infrastructure.image.gemini_provider import (
    GoogleGeminiImageClient,
    GoogleGeminiReferenceImage,
    map_productflow_size_to_gemini_image_config,
)
from productflow_backend.infrastructure.image.images_provider import (
    ImagesReferenceImage,
    OpenAIImagesClient,
)
from productflow_backend.infrastructure.image.responses_provider import (
    OpenAIResponsesImageClient,
    ResponsesReferenceImage,
)
from productflow_backend.infrastructure.provider_config import ResolvedImageProviderConfig
from productflow_backend.infrastructure.provider_effects import ProviderEffectQueryResult


def _responses_config(*, background: bool = False) -> ResolvedImageProviderConfig:
    return ResolvedImageProviderConfig(
        provider_kind="openai_responses",
        model="gpt-image-2",
        api_key="demo-api-key",
        base_url="https://example.test/v1",
        responses_background_enabled=background,
    )


def _images_config() -> ResolvedImageProviderConfig:
    return ResolvedImageProviderConfig(
        provider_kind="openai_images",
        model="gpt-image-1",
        api_key="demo-api-key",
        base_url="https://example.test/v1",
        images_quality="high",
        images_style="vivid",
    )


class DummyImagesAPIItem:
    def __init__(self, b64_json: str | None, revised_prompt: str | None = "revised prompt") -> None:
        self.b64_json = b64_json
        self.revised_prompt = revised_prompt


class DummyImagesAPIResponse:
    def __init__(self, b64_json: str | None) -> None:
        self.data = [DummyImagesAPIItem(b64_json)]


class DummyImageGenerationCall:
    type = "image_generation_call"

    def __init__(self, *, call_id: str, result: str, **metadata: object) -> None:
        self.id = call_id
        self.result = result
        for key, value in metadata.items():
            setattr(self, key, value)

    def model_dump(self, *, mode: str, exclude_none: bool) -> dict[str, object]:
        return {key: value for key, value in vars(self).items() if value is not None} | {"type": self.type}


class DummyResponsesResult:
    def __init__(self, *, response_id: str, output: list[object], status: str = "completed") -> None:
        self.id = response_id
        self.output = output
        self.status = status

    def model_dump(self, *, mode: str, exclude_none: bool) -> dict[str, object]:
        return {
            "id": self.id,
            "status": self.status,
            "output": [
                item.model_dump(mode=mode, exclude_none=exclude_none)
                if hasattr(item, "model_dump")
                else item
                for item in self.output
            ],
        }


def test_openai_responses_client_sends_native_references_and_sanitizes_history(
    configured_env,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    calls: list[dict] = []
    client_kwargs: list[dict] = []
    encoded_result = _make_demo_image_data_url().split(",", maxsplit=1)[1]

    class DummyResponses:
        def create(self, **kwargs):
            calls.append(kwargs)
            return DummyResponsesResult(
                response_id="resp-1",
                output=[
                    DummyImageGenerationCall(
                        call_id="ig-1",
                        result=encoded_result,
                        size="1024x1024",
                        quality="high",
                    )
                ],
            )

    class DummyOpenAI:
        def __init__(self, **kwargs) -> None:
            client_kwargs.append(kwargs)
            self.responses = DummyResponses()

    monkeypatch.setattr("productflow_backend.infrastructure.image.responses_provider.OpenAI", DummyOpenAI)
    client = OpenAIResponsesImageClient(_responses_config())
    result = client.generate_image(
        prompt="生成商品主图",
        size="1024x1024",
        reference_images=[ResponsesReferenceImage(_make_demo_image_bytes(), "image/png", "reference.png")],
        tool_options={"quality": "high"},
    )

    assert client_kwargs == [{"api_key": "demo-api-key", "base_url": "https://example.test/v1"}]
    assert calls[0]["tools"] == [{"type": "image_generation", "size": "1024x1024", "quality": "high"}]
    content = calls[0]["input"][0]["content"]
    assert content[0] == {"type": "input_text", "text": "生成商品主图"}
    assert content[1]["image_url"].startswith("data:image/png;base64,")
    assert encoded_result not in str(result.provider_request_json)
    assert "<base64 omitted" in str(result.provider_request_json)
    assert result.provider_response_id == "resp-1"
    assert result.image_generation_call_id == "ig-1"
    assert result.mime_type == "image/png"


def test_openai_responses_client_polls_background_and_reports_progress(
    configured_env,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    encoded_result = _make_demo_image_data_url().split(",", maxsplit=1)[1]
    calls: list[dict] = []
    retrieved: list[str] = []
    progress: list[dict] = []

    class DummyResponses:
        def create(self, **kwargs):
            calls.append(kwargs)
            return DummyResponsesResult(response_id="resp-background", output=[], status="queued")

        def retrieve(self, response_id: str):
            retrieved.append(response_id)
            return DummyResponsesResult(
                response_id=response_id,
                output=[DummyImageGenerationCall(call_id="ig-background", result=encoded_result)],
            )

    class DummyOpenAI:
        def __init__(self, **kwargs) -> None:
            self.responses = DummyResponses()

    monkeypatch.setattr("productflow_backend.infrastructure.image.responses_provider.OpenAI", DummyOpenAI)
    monkeypatch.setattr("productflow_backend.infrastructure.image.responses_provider.sleep", lambda _: None)
    result = OpenAIResponsesImageClient(_responses_config(background=True)).generate_image(
        prompt="后台生成",
        size="1024x1024",
        progress_callback=progress.append,
    )

    assert calls[0]["background"] is True
    assert retrieved == ["resp-background"]
    assert result.provider_response_id == "resp-background"
    assert [event["provider_response_status"] for event in progress] == ["queued", "completed"]
    assert progress[-1]["provider_response"]["output"][0]["result"].startswith("<base64 omitted")


def test_openai_responses_client_reconcile_only_reads_terminal_provider_record(
    configured_env,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    encoded_result = _make_demo_image_data_url().split(",", maxsplit=1)[1]
    create_calls: list[dict] = []
    retrieve_calls: list[str] = []

    class DummyResponses:
        def create(self, **kwargs):
            create_calls.append(kwargs)
            raise AssertionError("reconciliation must not submit another provider request")

        def retrieve(self, response_id: str):
            retrieve_calls.append(response_id)
            return DummyResponsesResult(
                response_id=response_id,
                output=[DummyImageGenerationCall(call_id="ig-reconciled", result=encoded_result)],
            )

    class DummyOpenAI:
        def __init__(self, **kwargs) -> None:
            self.responses = DummyResponses()

    monkeypatch.setattr("productflow_backend.infrastructure.image.responses_provider.OpenAI", DummyOpenAI)
    result = OpenAIResponsesImageClient(_responses_config()).reconcile_response("resp-reconcile")

    assert result == ProviderEffectQueryResult(
        effect_result="applied",
        reconciliation_state="applied",
        provider_response_id="resp-reconcile",
        provider_status="completed",
        result_json={
            "provider_response_id": "resp-reconcile",
            "provider_status": "completed",
            "has_image_generation_call": True,
            "has_image_result": True,
        },
    )
    assert retrieve_calls == ["resp-reconcile"]
    assert create_calls == []


def test_openai_responses_client_falls_back_from_optional_fields(
    configured_env,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    calls: list[dict] = []
    encoded_result = _make_demo_image_data_url().split(",", maxsplit=1)[1]

    class DummyResponses:
        def create(self, **kwargs):
            calls.append(kwargs)
            if len(calls) == 1:
                raise RuntimeError("unsupported field quality")
            return DummyResponsesResult(
                response_id="resp-fallback",
                output=[DummyImageGenerationCall(call_id="ig-fallback", result=encoded_result)],
            )

    class DummyOpenAI:
        def __init__(self, **kwargs) -> None:
            self.responses = DummyResponses()

    monkeypatch.setattr("productflow_backend.infrastructure.image.responses_provider.OpenAI", DummyOpenAI)
    result = OpenAIResponsesImageClient(_responses_config()).generate_image(
        prompt="参数回退",
        size="1024x1024",
        tool_options={"quality": "high", "output_format": "webp"},
    )

    assert calls[0]["tools"] == [
        {"type": "image_generation", "size": "1024x1024", "quality": "high", "output_format": "webp"}
    ]
    assert calls[1]["tools"] == [{"type": "image_generation", "size": "1024x1024"}]
    assert result.provider_output_json["_productflow"]["notes"] == [
        {"kind": "fallback", "message": "供应商不支持部分参数，已按基础参数完成。"}
    ]


def test_openai_responses_client_distinguishes_text_only_completion(
    configured_env,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    class DummyResponses:
        def create(self, **kwargs):
            return DummyResponsesResult(
                response_id="resp-text",
                output=[
                    {
                        "type": "message",
                        "content": [{"type": "output_text", "text": "无法生成这张图片"}],
                    }
                ],
            )

    class DummyOpenAI:
        def __init__(self, **kwargs) -> None:
            self.responses = DummyResponses()

    monkeypatch.setattr("productflow_backend.infrastructure.image.responses_provider.OpenAI", DummyOpenAI)
    with pytest.raises(RuntimeError, match="返回的是文字回复"):
        OpenAIResponsesImageClient(_responses_config()).generate_image(prompt="只返回文字", size="1024x1024")


def test_openai_responses_client_infers_mime_type_from_real_bytes(
    configured_env,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    output = BytesIO()
    Image.new("RGB", (16, 16), (255, 255, 255)).save(output, format="JPEG")
    encoded_result = b64encode(output.getvalue()).decode("utf-8")

    class DummyResponses:
        def create(self, **kwargs):
            return DummyResponsesResult(
                response_id="resp-jpeg",
                output=[DummyImageGenerationCall(call_id="ig-jpeg", result=encoded_result, output_format="png")],
            )

    class DummyOpenAI:
        def __init__(self, **kwargs) -> None:
            self.responses = DummyResponses()

    monkeypatch.setattr("productflow_backend.infrastructure.image.responses_provider.OpenAI", DummyOpenAI)
    result = OpenAIResponsesImageClient(_responses_config()).generate_image(prompt="返回 JPEG", size="1024x1024")
    assert result.mime_type == "image/jpeg"


def test_openai_images_client_generates_and_falls_back_for_multi_image_edit(
    configured_env,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    encoded_result = _make_demo_image_data_url().split(",", maxsplit=1)[1]
    generate_calls: list[dict] = []
    edit_calls: list[dict] = []

    class DummyImages:
        def generate(self, **kwargs):
            generate_calls.append(kwargs)
            return DummyImagesAPIResponse(encoded_result)

        def edit(self, **kwargs):
            edit_calls.append(kwargs)
            if len(edit_calls) == 1:
                raise RuntimeError("multiple images unsupported")
            return DummyImagesAPIResponse(encoded_result)

    class DummyOpenAI:
        def __init__(self, **kwargs) -> None:
            self.images = DummyImages()

    monkeypatch.setattr("productflow_backend.infrastructure.image.images_provider.OpenAI", DummyOpenAI)
    client = OpenAIImagesClient(_images_config())
    generated = client.generate(prompt="生成图", size="1024x1024")[0]
    edited = client.edit(
        image=[
            ImagesReferenceImage(_make_demo_image_bytes(), "image/png", "base.png"),
            ImagesReferenceImage(_make_demo_image_bytes(), "image/png", "reference.png"),
        ],
        prompt="改图",
        size="1024x1024",
    )[0]

    assert generate_calls[0] == {
        "model": "gpt-image-1",
        "prompt": "生成图",
        "size": "1024x1024",
        "n": 1,
        "response_format": "b64_json",
        "quality": "high",
        "style": "vivid",
    }
    assert isinstance(edit_calls[0]["image"], list)
    assert edit_calls[1]["image"].name == "base.png"
    assert edited.provider_output_json["_productflow"]["requested_image_count"] == 2
    assert edited.provider_output_json["_productflow"]["effective_image_count"] == 1
    assert generated.mime_type == "image/png"


def test_google_gemini_client_maps_size_and_omits_raw_image_bytes(
    configured_env,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    calls: list[dict] = []
    client_kwargs: list[dict] = []
    image_bytes = _make_demo_image_bytes()

    class DummyModels:
        def generate_content(self, **kwargs):
            calls.append(kwargs)
            return SimpleNamespace(
                response_id="gemini-response-1",
                model_version="gemini-test-version",
                parts=[
                    SimpleNamespace(text="ok"),
                    SimpleNamespace(inline_data=SimpleNamespace(data=image_bytes, mime_type="image/png")),
                ],
            )

    class DummyClient:
        def __init__(self, **kwargs) -> None:
            client_kwargs.append(kwargs)
            self.models = DummyModels()

    monkeypatch.setattr("productflow_backend.infrastructure.image.gemini_provider.genai.Client", DummyClient)
    config = ResolvedImageProviderConfig(
        provider_kind="google_gemini_image",
        model="gemini-3.1-flash-image-preview",
        api_key="google-api-key",
        gemini_api_version="v1beta",
        gemini_output_mime_type="image/png",
    )
    result = GoogleGeminiImageClient(config).generate_image(
        prompt="生成商品图",
        size="2048x1152",
        reference_images=[GoogleGeminiReferenceImage(image_bytes, "image/png", "reference.png")],
    )

    assert client_kwargs[0]["api_key"] == "google-api-key"
    assert calls[0]["model"] == "gemini-3.1-flash-image-preview"
    assert len(calls[0]["contents"]) == 2
    assert result.provider_request_json["image_config"] == {
        "aspect_ratio": "16:9",
        "image_size": "2K",
        "output_mime_type": "image/png",
    }
    assert "base64" not in str(result.provider_request_json).lower()
    assert image_bytes.hex() not in str(result.provider_output_json)

    mapped = map_productflow_size_to_gemini_image_config("3840x2160", "gemini-3-pro-image-preview")
    assert (mapped.aspect_ratio, mapped.image_size) == ("16:9", "4K")


def test_image_clients_require_profile_api_keys(configured_env) -> None:
    missing_responses_key = ResolvedImageProviderConfig(provider_kind="openai_responses", model="gpt-image-2")
    with pytest.raises(RuntimeError, match="缺少 API Key"):
        OpenAIResponsesImageClient(missing_responses_key).generate_image(prompt="生成图", size="1024x1024")

    missing_gemini_key = ResolvedImageProviderConfig(
        provider_kind="google_gemini_image",
        model="gemini-2.5-flash-image",
    )
    with pytest.raises(RuntimeError, match="缺少 API Key"):
        GoogleGeminiImageClient(missing_gemini_key).generate_image(prompt="生成图", size="1024x1024")
