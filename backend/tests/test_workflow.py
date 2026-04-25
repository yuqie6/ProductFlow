from __future__ import annotations

import logging
import os
import threading
import time
from datetime import UTC, datetime, timedelta
from io import BytesIO
from pathlib import Path

import pytest
import sqlalchemy as sa
from alembic.config import Config
from fastapi.testclient import TestClient
from PIL import Image
from pydantic import ValidationError

from alembic import command
from productflow_backend.application.contracts import (
    CopyPayload,
    CreativeBriefPayload,
    PosterGenerationInput,
    ProductInput,
    ReferenceImageInput,
)
from productflow_backend.application.use_cases import (
    add_reference_images,
    confirm_copy_set,
    create_copy_job,
    create_poster_job,
    create_product,
    delete_product,
    execute_copy_job,
    execute_poster_job,
    get_product_detail,
)
from productflow_backend.config import get_runtime_settings, get_settings
from productflow_backend.domain.enums import (
    CopyStatus,
    ImageSessionAssetKind,
    JobKind,
    JobStatus,
    PosterKind,
    SourceAssetKind,
    WorkflowNodeStatus,
    WorkflowNodeType,
    WorkflowRunStatus,
)
from productflow_backend.infrastructure.db.models import (
    AppSetting,
    CopySet,
    ImageSession,
    ImageSessionAsset,
    JobRun,
    PosterVariant,
    ProductWorkflow,
    SourceAsset,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRun,
)
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.image.responses_provider import OpenAIResponsesImageProvider
from productflow_backend.infrastructure.queue import recover_unfinished_jobs, recover_unfinished_workflow_runs


def _make_demo_image_bytes() -> bytes:
    return _make_demo_image_bytes_with_size(800, 800)


def _make_demo_image_bytes_with_size(width: int, height: int) -> bytes:
    image = Image.new("RGB", (width, height), (240, 240, 240))
    buffer = BytesIO()
    image.save(buffer, format="PNG")
    return buffer.getvalue()


def _make_demo_image_data_url() -> str:
    import base64

    encoded = base64.b64encode(_make_demo_image_bytes()).decode("utf-8")
    return f"data:image/png;base64,{encoded}"


def _read_image_size(image_bytes: bytes) -> tuple[int, int]:
    with Image.open(BytesIO(image_bytes)) as image:
        return image.size


def _login(client: TestClient) -> None:
    login = client.post("/api/auth/session", json={"admin_key": "super-secret-admin-key"})
    assert login.status_code == 200


def _wait_for_workflow_run(
    client: TestClient,
    product_id: str,
    *,
    status: str | None = None,
    timeout: float = 5.0,
) -> dict:
    deadline = time.monotonic() + timeout
    last_payload: dict | None = None
    while time.monotonic() < deadline:
        response = client.get(f"/api/products/{product_id}/workflow")
        assert response.status_code == 200
        last_payload = response.json()
        latest_run = last_payload["runs"][0] if last_payload["runs"] else None
        if latest_run and (status is None or latest_run["status"] == status):
            return last_payload
        time.sleep(0.05)
    assert last_payload is not None
    raise AssertionError(f"workflow run did not reach {status or 'any status'}: {last_payload['runs'][:1]}")


@pytest.fixture(autouse=True)
def _execute_workflow_queue_inline(monkeypatch: pytest.MonkeyPatch) -> None:
    """Keep API workflow tests deterministic while production delivery goes through Dramatiq."""

    from productflow_backend.application.product_workflows import execute_product_workflow_run

    monkeypatch.setattr(
        "productflow_backend.presentation.routes.product_workflows.enqueue_workflow_run",
        execute_product_workflow_run,
    )


def test_auth_session_required(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)

    unauthorized = client.get("/api/products")
    assert unauthorized.status_code == 401

    _login(client)

    authorized = client.get("/api/products")
    assert authorized.status_code == 200
    assert authorized.json()["items"] == []


def test_settings_api_persists_database_overrides(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    initial = client.get("/api/settings")
    assert initial.status_code == 200
    initial_items = {item["key"]: item for item in initial.json()["items"]}
    assert initial_items["image_provider_kind"]["value"] == "mock"
    assert initial_items["image_provider_kind"]["source"] == "env_default"

    updated = client.patch(
        "/api/settings",
        json={
            "values": {
                "image_provider_kind": "openai_responses",
                "image_api_key": "database-image-key",
                "image_generate_model": "gpt-5.4-mini",
                "job_retry_delay_ms": 2500,
            }
        },
    )
    assert updated.status_code == 200
    updated_items = {item["key"]: item for item in updated.json()["items"]}
    assert updated_items["image_provider_kind"]["value"] == "openai_responses"
    assert updated_items["image_provider_kind"]["source"] == "database"
    assert updated_items["image_api_key"]["value"] == ""
    assert updated_items["image_api_key"]["has_value"] is True
    assert get_runtime_settings().image_provider_kind == "openai_responses"
    assert get_runtime_settings().image_api_key == "database-image-key"
    assert get_runtime_settings().job_retry_delay_ms == 2500

    session = get_session_factory()()
    try:
        assert session.get(AppSetting, "image_provider_kind").value == "openai_responses"
    finally:
        session.close()

    reset = client.patch("/api/settings", json={"reset_keys": ["image_provider_kind"]})
    assert reset.status_code == 200
    reset_items = {item["key"]: item for item in reset.json()["items"]}
    assert reset_items["image_provider_kind"]["value"] == "mock"
    assert reset_items["image_provider_kind"]["source"] == "env_default"


def test_prompt_settings_api_accepts_rejects_and_resets(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    initial = client.get("/api/settings")
    assert initial.status_code == 200
    initial_items = {item["key"]: item for item in initial.json()["items"]}
    assert initial_items["prompt_copy_system"]["category"] == "提示词"
    assert initial_items["prompt_copy_system"]["input_type"] == "textarea"
    assert initial_items["prompt_copy_system"]["secret"] is False
    assert initial_items["prompt_copy_system"]["source"] == "env_default"
    assert "淘宝电商文案助手" in initial_items["prompt_copy_system"]["value"]

    updated = client.patch("/api/settings", json={"values": {"prompt_copy_system": "自定义文案系统提示"}})
    assert updated.status_code == 200
    updated_items = {item["key"]: item for item in updated.json()["items"]}
    assert updated_items["prompt_copy_system"]["value"] == "自定义文案系统提示"
    assert updated_items["prompt_copy_system"]["source"] == "database"

    empty = client.patch("/api/settings", json={"values": {"prompt_copy_system": "   "}})
    assert empty.status_code == 400
    assert "不能为空" in empty.json()["detail"]

    reset = client.patch("/api/settings", json={"reset_keys": ["prompt_copy_system"]})
    assert reset.status_code == 200
    reset_items = {item["key"]: item for item in reset.json()["items"]}
    assert reset_items["prompt_copy_system"]["source"] == "env_default"
    assert "淘宝电商文案助手" in reset_items["prompt_copy_system"]["value"]


def test_settings_api_rejects_invalid_effective_config(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    response = client.patch(
        "/api/settings",
        json={"values": {"image_main_image_size": "2048x2048", "image_allowed_sizes": "1024x1024"}},
    )

    assert response.status_code == 400
    assert "主图尺寸" in response.json()["detail"]


def test_settings_api_rejects_malformed_image_sizes_before_persist(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    response = client.patch(
        "/api/settings",
        json={
            "values": {
                "image_main_image_size": "foo",
                "image_promo_poster_size": "foo",
                "image_allowed_sizes": "foo",
            }
        },
    )

    assert response.status_code == 400
    assert "宽x高" in response.json()["detail"]

    session = get_session_factory()()
    try:
        assert session.get(AppSetting, "image_main_image_size") is None
        assert session.get(AppSetting, "image_promo_poster_size") is None
        assert session.get(AppSetting, "image_allowed_sizes") is None
    finally:
        session.close()


def test_settings_api_normalizes_custom_image_sizes_for_generation(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    updated = client.patch(
        "/api/settings",
        json={
            "values": {
                "image_main_image_size": "512X512",
                "image_promo_poster_size": "1024x768",
                "image_allowed_sizes": "512X512, 1024x768,512x512",
            }
        },
    )
    assert updated.status_code == 200
    updated_items = {item["key"]: item for item in updated.json()["items"]}
    assert updated_items["image_main_image_size"]["value"] == "512x512"
    assert updated_items["image_allowed_sizes"]["value"] == "512x512,1024x768"

    created = client.post("/api/image-sessions", json={"title": "自定义尺寸"})
    assert created.status_code == 201
    session_id = created.json()["id"]

    generated = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "生成一张自定义尺寸商品图", "size": "512x512"},
    )

    assert generated.status_code == 200
    assert generated.json()["rounds"][-1]["size"] == "512x512"


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

    chat_prompt = ImageChatService()._build_prompt(
        "改成白底",
        [ImageChatTurn(role="user", content="先做一个主图")],
        "1024x1024",
    )
    assert "自定义连续生图 1024x1024" in chat_prompt
    assert "用户：先做一个主图" in chat_prompt
    assert chat_prompt.endswith("改成白底")


def test_sqlalchemy_enum_columns_use_database_values() -> None:
    assert SourceAsset.__table__.c.kind.type.enums == [member.value for member in SourceAssetKind]
    assert ImageSessionAsset.__table__.c.kind.type.enums == [member.value for member in ImageSessionAssetKind]
    assert CopySet.__table__.c.status.type.enums == [member.value for member in CopyStatus]
    assert PosterVariant.__table__.c.kind.type.enums == [member.value for member in PosterKind]
    assert JobRun.__table__.c.kind.type.enums == [member.value for member in JobKind]
    assert JobRun.__table__.c.status.type.enums == [member.value for member in JobStatus]
    assert JobRun.__table__.c.target_poster_kind.type.enums == [member.value for member in PosterKind]
    assert WorkflowNode.__table__.c.node_type.type.enums == [member.value for member in WorkflowNodeType]
    assert WorkflowNode.__table__.c.status.type.enums == [member.value for member in WorkflowNodeStatus]
    assert WorkflowRun.__table__.c.status.type.enums == [member.value for member in WorkflowRunStatus]


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


def test_copy_job_persists_normalized_provider_scalar_lists(
    db_session, configured_env: Path, monkeypatch
) -> None:
    class ListAudienceTextProvider:
        provider_name = "list-audience"
        prompt_version = "test-v1"

        def generate_brief(self, product: ProductInput) -> tuple[CreativeBriefPayload, str]:
            return (
                CreativeBriefPayload.model_validate(
                    {
                        "positioning": f"{product.name} 的入门场景",
                        "audience": ["摄影入门用户", "小红书图文内容创作者"],
                        "selling_angles": ["上手快", "构图稳", "出片自然"],
                        "taboo_phrases": [],
                        "poster_style_hint": "干净明亮",
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
                        "title": [product.name, "入门也能拍得稳"],
                        "selling_points": [
                            f"适合{brief.audience}",
                            "构图辅助更直观",
                            "轻松提升日常出片效率",
                        ],
                        "poster_headline": ["新手拍照", "画面更稳"],
                        "cta": ["立即提升", "出片率"],
                    }
                ),
                "list-audience-copy",
            )

    monkeypatch.setattr(
        "productflow_backend.application.use_cases.get_text_provider",
        lambda: ListAudienceTextProvider(),
    )

    product = create_product(
        db_session,
        name="手机摄影支架",
        category="数码配件",
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="tripod.png",
        content_type="image/png",
    )

    copy_job = create_copy_job(db_session, product_id=product.id).job
    assert execute_copy_job(copy_job.id) is False

    db_session.expire_all()
    product_after_copy = get_product_detail(db_session, product.id)
    assert product_after_copy.creative_briefs[0].payload["audience"] == "摄影入门用户、小红书图文内容创作者"
    assert product_after_copy.copy_sets[0].title == "手机摄影支架、入门也能拍得稳"
    assert product_after_copy.copy_sets[0].poster_headline == "新手拍照、画面更稳"
    assert product_after_copy.copy_sets[0].cta == "立即提升、出片率"
    assert "摄影入门用户、小红书图文内容创作者" in product_after_copy.copy_sets[0].selling_points[0]


def test_end_to_end_copy_and_poster_workflow(db_session, configured_env: Path) -> None:
    product = create_product(
        db_session,
        name="防滑菜板",
        category="家居百货",
        price="29.90",
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="product.png",
        content_type="image/png",
    )

    copy_job = create_copy_job(db_session, product_id=product.id).job
    execute_copy_job(copy_job.id)

    db_session.expire_all()
    product_after_copy = get_product_detail(db_session, product.id)
    assert len(product_after_copy.copy_sets) == 1
    copy_set = product_after_copy.copy_sets[0]
    assert "防滑菜板" in copy_set.title

    confirm_copy_set(db_session, copy_set_id=copy_set.id)
    poster_job = create_poster_job(db_session, product_id=product.id).job
    execute_poster_job(poster_job.id)

    db_session.expire_all()
    product_after_poster = get_product_detail(db_session, product.id)
    assert product_after_poster.confirmed_copy_set is not None
    assert len(product_after_poster.poster_variants) == 2

    poster_paths = [Path(configured_env) / poster.storage_path for poster in product_after_poster.poster_variants]
    assert all(path.exists() for path in poster_paths)


def test_product_create_persists_source_note_for_ai_context(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    response = client.post(
        "/api/products",
        data={
            "name": "露营保温杯",
            "category": "户外",
            "price": "79.00",
            "source_note": "316 不锈钢，主打长效保温和车载杯架适配。",
        },
        files={"image": ("cup.png", _make_demo_image_bytes(), "image/png")},
    )

    assert response.status_code == 201
    payload = response.json()
    assert payload["source_note"] == "316 不锈钢，主打长效保温和车载杯架适配。"

    minimal = client.post(
        "/api/products",
        data={"name": "极简商品壳"},
        files={"image": ("minimal.png", _make_demo_image_bytes(), "image/png")},
    )
    assert minimal.status_code == 201
    minimal_payload = minimal.json()
    assert minimal_payload["category"] is None
    assert minimal_payload["price"] is None
    assert minimal_payload["source_note"] is None


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


def test_product_workflow_dag_runs_and_persists_artifacts(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "多功能收纳架"},
        files={"image": ("rack.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    workflow = workflow_response.json()
    assert len(workflow["nodes"]) >= 4
    assert len(workflow["edges"]) >= 3
    assert {node["node_type"] for node in workflow["nodes"]} == {
        "product_context",
        "reference_image",
        "copy_generation",
        "image_generation",
    }

    context_node = next(node for node in workflow["nodes"] if node["node_type"] == "product_context")
    updated_context = client.patch(
        f"/api/workflow-nodes/{context_node['id']}",
        json={
            "position_x": 96,
            "position_y": 144,
            "config_json": {
                "name": "多功能收纳架",
                "category": "家居",
                "price": "49.90",
                "source_note": "免打孔安装，适合厨房和浴室，强调承重和整洁。",
            }
        },
    )
    assert updated_context.status_code == 200
    moved_context = next(node for node in updated_context.json()["nodes"] if node["id"] == context_node["id"])
    assert moved_context["position_x"] == 96
    assert moved_context["position_y"] == 144

    manual_reference = client.post(
        f"/api/products/{product_id}/workflow/nodes",
        json={
            "node_type": "reference_image",
            "title": "风格参考",
            "position_x": 320,
            "position_y": 260,
            "config_json": {"role": "style", "label": "厨房风格"},
        },
    )
    assert manual_reference.status_code == 201
    upload_node = next(
        node
        for node in manual_reference.json()["nodes"]
        if node["node_type"] == "reference_image" and node["title"] == "风格参考"
    )
    uploaded = client.post(
        f"/api/workflow-nodes/{upload_node['id']}/image",
        data={"role": "style", "label": "厨房风格图"},
        files={"image": ("style.png", _make_demo_image_bytes(), "image/png")},
    )
    assert uploaded.status_code == 200
    uploaded_node = next(node for node in uploaded.json()["nodes"] if node["id"] == upload_node["id"])
    assert uploaded_node["output_json"]["source_asset_ids"]

    copy_node = next(node for node in workflow["nodes"] if node["node_type"] == "copy_generation")
    updated = client.patch(
        f"/api/workflow-nodes/{copy_node['id']}",
        json={"config_json": {"instruction": "突出免打孔和厨房整洁场景"}},
    )
    assert updated.status_code == 200
    reference_to_copy = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": upload_node["id"],
            "target_node_id": copy_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert reference_to_copy.status_code == 201
    image_node = next(node for node in workflow["nodes"] if node["node_type"] == "image_generation")
    updated_image = client.patch(
        f"/api/workflow-nodes/{image_node['id']}",
        json={"config_json": {"instruction": "沿用上游文案和参考图，生成主图", "size": "1024x1024"}},
    )
    assert updated_image.status_code == 200

    upstream_edge = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": upload_node["id"],
            "target_node_id": image_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert upstream_edge.status_code == 201
    default_reference_node = next(
        node
        for node in workflow["nodes"]
        if node["node_type"] == "reference_image" and node["title"] == "参考图"
    )
    default_target_edge = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": image_node["id"],
            "target_node_id": default_reference_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert default_target_edge.status_code == 201
    second_target = client.post(
        f"/api/products/{product_id}/workflow/nodes",
        json={
            "node_type": "reference_image",
            "title": "参考图 2",
            "position_x": 1160,
            "position_y": 240,
            "config_json": {"role": "reference", "label": "参考图 2"},
        },
    )
    assert second_target.status_code == 201
    second_target_node = next(node for node in second_target.json()["nodes"] if node["title"] == "参考图 2")
    second_target_edge = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": image_node["id"],
            "target_node_id": second_target_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert second_target_edge.status_code == 201
    duplicate_target_edge = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": image_node["id"],
            "target_node_id": second_target_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert duplicate_target_edge.status_code == 201
    workflow_before_run = duplicate_target_edge.json()

    run_response = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert run_response.status_code == 200
    assert run_response.json()["runs"][0]["status"] == "running"
    run_payload = _wait_for_workflow_run(client, product_id, status="succeeded")
    assert run_payload["runs"][0]["status"] == "succeeded"
    assert all(node["status"] == "succeeded" for node in run_payload["nodes"])
    copy_output = next(node for node in run_payload["nodes"] if node["node_type"] == "copy_generation")["output_json"]
    assert copy_output["copy_set_id"]
    assert "免打孔" in " ".join(copy_output["selling_points"])
    assert "厨房风格图" in " ".join(copy_output["selling_points"])
    edited_copy = client.patch(
        f"/api/workflow-nodes/{copy_node['id']}/copy",
        json={
            "title": "厨房免打孔收纳架",
            "selling_points": ["免打孔安装", "厨房台面更整洁", "承重稳定"],
            "poster_headline": "厨房整洁一步到位",
            "cta": "立即整理厨房",
        },
    )
    assert edited_copy.status_code == 200
    edited_copy_node = next(node for node in edited_copy.json()["nodes"] if node["id"] == copy_node["id"])
    assert edited_copy_node["output_json"]["title"] == "厨房免打孔收纳架"
    assert edited_copy_node["output_json"]["poster_headline"] == "厨房整洁一步到位"
    rerun_image = client.post(
        f"/api/products/{product_id}/workflow/run",
        json={"start_node_id": image_node["id"]},
    )
    assert rerun_image.status_code == 200
    rerun_payload = _wait_for_workflow_run(client, product_id, status="succeeded")
    assert rerun_payload["runs"][0]["status"] == "succeeded"
    rerun_copy_output = next(node for node in rerun_payload["nodes"] if node["id"] == copy_node["id"])["output_json"]
    rerun_image_output = next(node for node in rerun_payload["nodes"] if node["id"] == image_node["id"])["output_json"]
    assert rerun_copy_output["copy_set_id"] == copy_output["copy_set_id"]
    assert rerun_copy_output["poster_headline"] == "厨房整洁一步到位"
    assert rerun_image_output["copy_set_id"] == copy_output["copy_set_id"]
    image_output = next(node for node in run_payload["nodes"] if node["node_type"] == "image_generation")["output_json"]
    assert "poster_variant_ids" not in image_output
    assert len(image_output["generated_poster_variant_ids"]) == 2
    assert image_output["target_count"] == 2
    assert len(image_output["filled_source_asset_ids"]) == 2
    assert len(image_output["filled_reference_node_ids"]) == 2
    assert image_output["size"] == "1024x1024"
    context_sources = image_output["context_sources"]
    assert any(source["label"] == "文案" and "多功能收纳架" in source["text"] for source in context_sources)
    assert any(source["label"] == "参考图" and "厨房风格图" in source["text"] for source in context_sources)
    assert image_output["context_summary"]["reference_image_count"] >= 1
    filled_nodes = [
        node for node in run_payload["nodes"] if node["id"] in set(image_output["filled_reference_node_ids"])
    ]
    assert all(node["output_json"]["source_asset_ids"] for node in filled_nodes)

    product_after = client.get(f"/api/products/{product_id}")
    assert product_after.status_code == 200
    product_payload = product_after.json()
    assert any(copy_set["id"] == copy_output["copy_set_id"] for copy_set in product_payload["copy_sets"])
    assert len(product_payload["poster_variants"]) == 4
    reference_assets = [asset for asset in product_payload["source_assets"] if asset["kind"] == "reference_image"]
    assert len(reference_assets) == 5

    rejected_cycle = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": image_node["id"],
            "target_node_id": copy_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert rejected_cycle.status_code == 400
    assert "循环依赖" in rejected_cycle.json()["detail"]
    refreshed = client.get(f"/api/products/{product_id}/workflow")
    assert refreshed.status_code == 200
    assert len(refreshed.json()["edges"]) == len(workflow_before_run["edges"])

    edge_to_delete = refreshed.json()["edges"][0]
    deleted_edge = client.delete(f"/api/workflow-edges/{edge_to_delete['id']}")
    assert deleted_edge.status_code == 200
    deleted_payload = deleted_edge.json()
    assert len(deleted_payload["edges"]) == len(workflow_before_run["edges"]) - 1
    assert edge_to_delete["id"] not in {edge["id"] for edge in deleted_payload["edges"]}

    isolated_image = client.post(
        f"/api/products/{product_id}/workflow/nodes",
        json={
            "node_type": "image_generation",
            "title": "未连接生图",
            "position_x": 620,
            "position_y": 420,
            "config_json": {"instruction": "生成但不落槽", "size": "1024x1024"},
        },
    )
    assert isolated_image.status_code == 201
    isolated_image_node = next(node for node in isolated_image.json()["nodes"] if node["title"] == "未连接生图")
    context_to_isolated = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": context_node["id"],
            "target_node_id": isolated_image_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert context_to_isolated.status_code == 201
    copy_to_isolated = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": copy_node["id"],
            "target_node_id": isolated_image_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert copy_to_isolated.status_code == 201
    direct_run = client.post(
        f"/api/products/{product_id}/workflow/run",
        json={"start_node_id": isolated_image_node["id"]},
    )
    assert direct_run.status_code == 200
    direct_payload = _wait_for_workflow_run(client, product_id, status="failed")
    assert direct_payload["runs"][0]["status"] == "failed"
    assert "至少一个图片/参考图节点" in direct_payload["runs"][0]["failure_reason"]
    direct_node = next(node for node in direct_payload["nodes"] if node["id"] == isolated_image_node["id"])
    assert direct_node["status"] == "failed"
    assert "至少一个图片/参考图节点" in direct_node["failure_reason"]

    session = get_session_factory()()
    try:
        assert session.query(ProductWorkflow).filter_by(product_id=product_id).count() == 1
    finally:
        session.close()


def test_reference_workflow_node_upload_replaces_current_image(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "桌面收纳盒"},
        files={"image": ("box.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    reference_node = next(node for node in workflow_response.json()["nodes"] if node["node_type"] == "reference_image")

    first_upload = client.post(
        f"/api/workflow-nodes/{reference_node['id']}/image",
        data={"role": "style", "label": "第一次参考"},
        files={"image": ("first.png", _make_demo_image_bytes(), "image/png")},
    )
    assert first_upload.status_code == 200
    first_node = next(node for node in first_upload.json()["nodes"] if node["id"] == reference_node["id"])
    first_asset_id = first_node["output_json"]["source_asset_ids"][0]
    assert first_node["config_json"]["source_asset_ids"] == [first_asset_id]
    assert first_node["output_json"]["source_asset_ids"] == [first_asset_id]

    second_upload = client.post(
        f"/api/workflow-nodes/{reference_node['id']}/image",
        data={"role": "style", "label": "第二次参考"},
        files={"image": ("second.png", _make_demo_image_bytes_with_size(640, 480), "image/png")},
    )
    assert second_upload.status_code == 200
    second_node = next(node for node in second_upload.json()["nodes"] if node["id"] == reference_node["id"])
    second_asset_id = second_node["output_json"]["source_asset_ids"][0]
    assert second_asset_id != first_asset_id
    assert second_node["config_json"]["source_asset_ids"] == [second_asset_id]
    assert second_node["output_json"]["source_asset_ids"] == [second_asset_id]
    assert second_node["output_json"]["image_asset_ids"] == [second_asset_id]
    assert len(second_node["output_json"]["images"]) == 1

    product_after = client.get(f"/api/products/{product_id}")
    assert product_after.status_code == 200
    reference_asset_ids = {
        asset["id"] for asset in product_after.json()["source_assets"] if asset["kind"] == "reference_image"
    }
    assert {first_asset_id, second_asset_id}.issubset(reference_asset_ids)


def test_image_generation_fill_replaces_reference_node_current_image(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "床头灯"},
        files={"image": ("lamp.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    workflow = workflow_response.json()
    image_node = next(node for node in workflow["nodes"] if node["node_type"] == "image_generation")
    reference_node = next(node for node in workflow["nodes"] if node["node_type"] == "reference_image")

    upload = client.post(
        f"/api/workflow-nodes/{reference_node['id']}/image",
        data={"role": "reference", "label": "旧参考"},
        files={"image": ("old.png", _make_demo_image_bytes(), "image/png")},
    )
    assert upload.status_code == 200
    uploaded_reference = next(node for node in upload.json()["nodes"] if node["id"] == reference_node["id"])
    old_asset_id = uploaded_reference["output_json"]["source_asset_ids"][0]

    connected = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": image_node["id"],
            "target_node_id": reference_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert connected.status_code == 201

    run_response = client.post(f"/api/products/{product_id}/workflow/run", json={"start_node_id": image_node["id"]})
    assert run_response.status_code == 200
    payload = _wait_for_workflow_run(client, product_id, status="succeeded")
    filled_reference = next(node for node in payload["nodes"] if node["id"] == reference_node["id"])
    new_asset_id = filled_reference["output_json"]["source_asset_ids"][0]
    assert new_asset_id != old_asset_id
    assert filled_reference["config_json"]["source_asset_ids"] == [new_asset_id]
    assert filled_reference["output_json"]["source_asset_ids"] == [new_asset_id]
    assert len(filled_reference["output_json"]["images"]) == 1

    product_after = client.get(f"/api/products/{product_id}")
    assert product_after.status_code == 200
    reference_asset_ids = {
        asset["id"] for asset in product_after.json()["source_assets"] if asset["kind"] == "reference_image"
    }
    assert {old_asset_id, new_asset_id}.issubset(reference_asset_ids)


def test_image_generation_fills_multiple_targets_with_concurrent_provider_calls(
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

    class CoordinatedImageProvider:
        provider_name = "coordinated"
        prompt_version = "coordinated-v1"

        def __init__(self) -> None:
            self._lock = threading.Lock()
            self._both_started = threading.Event()
            self.started = 0
            self.max_in_flight = 0
            self._in_flight = 0
            self.thread_ids: list[int] = []

        def generate_poster_image(
            self,
            poster: PosterGenerationInput,
            kind: PosterKind,
        ) -> tuple[GeneratedImagePayload, str]:
            del poster
            with self._lock:
                self.thread_ids.append(threading.get_ident())
                self.started += 1
                self._in_flight += 1
                self.max_in_flight = max(self.max_in_flight, self._in_flight)
                call_index = self.started
                if self.started >= 2:
                    self._both_started.set()
            if not self._both_started.wait(timeout=1.0):
                raise AssertionError("provider calls were not initiated concurrently")
            try:
                return (
                    GeneratedImagePayload(
                        kind=kind,
                        bytes_data=_make_demo_image_bytes(),
                        mime_type="image/png",
                        width=800,
                        height=800,
                        variant_label=f"coordinated-{call_index}",
                    ),
                    "coordinated-v1",
                )
            finally:
                with self._lock:
                    self._in_flight -= 1

    fake_provider = CoordinatedImageProvider()
    provider_factory_thread_ids: list[int] = []

    def fake_provider_factory() -> CoordinatedImageProvider:
        provider_factory_thread_ids.append(threading.get_ident())
        return fake_provider

    monkeypatch.setattr(
        "productflow_backend.application.product_workflows.get_image_provider",
        fake_provider_factory,
    )

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "并发生图商品"},
        files={"image": ("parallel.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    workflow = workflow_response.json()
    image_node = next(node for node in workflow["nodes"] if node["node_type"] == "image_generation")
    second_target = client.post(
        f"/api/products/{product_id}/workflow/nodes",
        json={
            "node_type": "reference_image",
            "title": "并发参考图 2",
            "position_x": 1180,
            "position_y": 240,
            "config_json": {"role": "reference", "label": "并发参考图 2"},
        },
    )
    assert second_target.status_code == 201
    second_target_node = next(node for node in second_target.json()["nodes"] if node["title"] == "并发参考图 2")
    connected = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": image_node["id"],
            "target_node_id": second_target_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert connected.status_code == 201

    run_response = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert run_response.status_code == 200
    payload = _wait_for_workflow_run(client, product_id, status="succeeded")
    image_output = next(node for node in payload["nodes"] if node["id"] == image_node["id"])["output_json"]
    assert len(provider_factory_thread_ids) == 2
    assert set(provider_factory_thread_ids).isdisjoint(fake_provider.thread_ids)
    assert fake_provider.started == 2
    assert fake_provider.max_in_flight == 2
    assert image_output["target_count"] == 2
    assert len(image_output["filled_reference_node_ids"]) == 2
    assert len(image_output["filled_source_asset_ids"]) == 2
    assert len(image_output["generated_poster_variant_ids"]) == 2
    assert "poster_variant_ids" not in image_output


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


def test_product_workflow_singleton_context_and_direct_image_run(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "直跑台灯"},
        files={"image": ("lamp.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    list_response = client.get("/api/products?page=1&page_size=1")
    assert list_response.status_code == 200
    listed = list_response.json()
    assert listed["total"] == 1
    summary = listed["items"][0]
    assert summary["source_image_filename"] == "lamp.png"
    assert summary["source_image_thumbnail_url"].endswith("variant=thumbnail")

    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    workflow = workflow_response.json()
    context_node = next(node for node in workflow["nodes"] if node["node_type"] == "product_context")
    image_node = next(node for node in workflow["nodes"] if node["node_type"] == "image_generation")

    duplicate_context = client.post(
        f"/api/products/{product_id}/workflow/nodes",
        json={
            "node_type": "product_context",
            "title": "重复商品",
            "position_x": 120,
            "position_y": 120,
            "config_json": {},
        },
    )
    assert duplicate_context.status_code == 400
    assert duplicate_context.json()["detail"] == "商品资料节点已存在"

    session = get_session_factory()()
    try:
        persisted_workflow = session.scalar(sa.select(ProductWorkflow).where(ProductWorkflow.product_id == product_id))
        assert persisted_workflow is not None
        duplicate_node = WorkflowNode(
            workflow_id=persisted_workflow.id,
            node_type=WorkflowNodeType.PRODUCT_CONTEXT,
            title="历史重复商品",
            position_x=180,
            position_y=140,
            config_json={},
        )
        session.add(duplicate_node)
        session.commit()
    finally:
        session.close()

    normalized_response = client.get(f"/api/products/{product_id}/workflow")
    assert normalized_response.status_code == 200
    normalized_workflow = normalized_response.json()
    assert [node["node_type"] for node in normalized_workflow["nodes"]].count("product_context") == 1

    removable_nodes = [
        node for node in normalized_workflow["nodes"] if node["node_type"] in {"copy_generation", "reference_image"}
    ]
    for removable in removable_nodes:
        deleted = client.delete(f"/api/workflow-nodes/{removable['id']}")
        assert deleted.status_code == 200

    patched_image = client.patch(
        f"/api/workflow-nodes/{image_node['id']}",
        json={"config_json": {"instruction": "只根据商品资料生成干净主图", "size": "1024x1024"}},
    )
    assert patched_image.status_code == 200

    run_response = client.post(
        f"/api/products/{product_id}/workflow/run",
        json={"start_node_id": image_node["id"]},
    )
    assert run_response.status_code == 200
    payload = _wait_for_workflow_run(client, product_id, status="failed")
    assert [node["node_type"] for node in payload["nodes"]] == ["product_context", "image_generation"]
    image_node_after = next(node for node in payload["nodes"] if node["id"] == image_node["id"])
    assert image_node_after["status"] == "failed"
    assert "至少一个图片/参考图节点" in image_node_after["failure_reason"]
    assert next(node for node in payload["nodes"] if node["id"] == context_node["id"])["node_type"] == "product_context"


def test_direct_downstream_run_uses_latest_saved_product_context(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "旅行背包"},
        files={"image": ("bag.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    workflow = workflow_response.json()
    context_node = next(node for node in workflow["nodes"] if node["node_type"] == "product_context")
    image_node = next(node for node in workflow["nodes"] if node["node_type"] == "image_generation")

    initial_context = client.patch(
        f"/api/workflow-nodes/{context_node['id']}",
        json={
            "config_json": {
                "name": "旅行背包",
                "category": "旧类目",
                "price": "199",
                "source_note": "旧说明：城市通勤。",
            }
        },
    )
    assert initial_context.status_code == 200
    first_run = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert first_run.status_code == 200
    first_payload = _wait_for_workflow_run(client, product_id, status="succeeded")
    stale_context_output = next(
        node for node in first_payload["nodes"] if node["id"] == context_node["id"]
    )["output_json"]
    assert stale_context_output["source_note"] == "旧说明：城市通勤。"

    latest_context = client.patch(
        f"/api/workflow-nodes/{context_node['id']}",
        json={
            "config_json": {
                "name": "旅行背包",
                "category": "户外装备",
                "price": "249",
                "source_note": "最新说明：防泼水牛津布，适合短途出差和周末露营。",
            }
        },
    )
    assert latest_context.status_code == 200

    selected_run = client.post(
        f"/api/products/{product_id}/workflow/run",
        json={"start_node_id": image_node["id"]},
    )
    assert selected_run.status_code == 200
    payload = _wait_for_workflow_run(client, product_id, status="succeeded")
    image_output = next(node for node in payload["nodes"] if node["id"] == image_node["id"])["output_json"]

    assert image_output["context_summary"]["product_context"]["category"] == "户外装备"
    assert image_output["context_summary"]["product_context"]["price"] == "249"
    assert (
        image_output["context_summary"]["product_context"]["source_note"]
        == "最新说明：防泼水牛津布，适合短途出差和周末露营。"
    )
    assert any("最新说明" in source["text"] for source in image_output["context_sources"])


def test_product_context_source_image_reaches_image_generation_context(
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
                    variant_label=f"capturing-r{len(poster.reference_images)}",
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
        data={"name": "旅行背包"},
        files={"image": ("bag.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    workflow = workflow_response.json()
    context_node = next(node for node in workflow["nodes"] if node["node_type"] == "product_context")
    image_node = next(node for node in workflow["nodes"] if node["node_type"] == "image_generation")
    assert any(
        edge["source_node_id"] == context_node["id"] and edge["target_node_id"] == image_node["id"]
        for edge in workflow["edges"]
    )

    selected_run = client.post(
        f"/api/products/{product_id}/workflow/run",
        json={"start_node_id": image_node["id"]},
    )
    assert selected_run.status_code == 200
    payload = _wait_for_workflow_run(client, product_id, status="succeeded")
    image_output = next(node for node in payload["nodes"] if node["id"] == image_node["id"])["output_json"]

    assert image_output["context_summary"]["reference_image_count"] == 1
    assert any(
        source["label"] == "商品图" and "bag.png" in source["text"]
        for source in image_output["context_sources"]
    )
    assert len(captured_inputs) == 1
    provider_input = captured_inputs[0]
    assert len(provider_input.reference_images) == 1
    assert provider_input.reference_images[0].path == provider_input.source_image


def test_single_node_workflow_run_reuses_succeeded_upstream_outputs(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

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

    initial_workflow = client.get(f"/api/products/{product_id}/workflow")
    assert initial_workflow.status_code == 200
    workflow = initial_workflow.json()
    upstream_image_node = next(node for node in workflow["nodes"] if node["node_type"] == "image_generation")
    upstream_reference_node = next(node for node in workflow["nodes"] if node["node_type"] == "reference_image")
    upstream_slot_edge = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": upstream_image_node["id"],
            "target_node_id": upstream_reference_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert upstream_slot_edge.status_code == 201

    first_run = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert first_run.status_code == 200
    first_payload = _wait_for_workflow_run(client, product_id, status="succeeded")
    assert first_payload["runs"][0]["status"] == "succeeded"
    succeeded_image_node = next(node for node in first_payload["nodes"] if node["id"] == upstream_image_node["id"])
    succeeded_reference_node = next(
        node for node in first_payload["nodes"] if node["id"] == upstream_reference_node["id"]
    )
    upstream_poster_ids = succeeded_image_node["output_json"]["generated_poster_variant_ids"]
    upstream_reference_asset_ids = succeeded_reference_node["output_json"]["source_asset_ids"]
    assert upstream_poster_ids
    assert upstream_reference_asset_ids

    downstream_image = client.post(
        f"/api/products/{product_id}/workflow/nodes",
        json={
            "node_type": "image_generation",
            "title": "下游生图",
            "position_x": 900,
            "position_y": 360,
            "config_json": {"instruction": "沿用上游图片继续生成", "size": "1024x1024"},
        },
    )
    assert downstream_image.status_code == 201
    downstream_image_node = next(node for node in downstream_image.json()["nodes"] if node["title"] == "下游生图")
    downstream_reference = client.post(
        f"/api/products/{product_id}/workflow/nodes",
        json={
            "node_type": "reference_image",
            "title": "下游参考图",
            "position_x": 1180,
            "position_y": 360,
            "config_json": {"role": "reference", "label": "下游参考图"},
        },
    )
    assert downstream_reference.status_code == 201
    downstream_reference_node = next(
        node for node in downstream_reference.json()["nodes"] if node["title"] == "下游参考图"
    )
    upstream_edge = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": upstream_image_node["id"],
            "target_node_id": downstream_image_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert upstream_edge.status_code == 201
    target_edge = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": downstream_image_node["id"],
            "target_node_id": downstream_reference_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert target_edge.status_code == 201

    product_before = client.get(f"/api/products/{product_id}")
    assert product_before.status_code == 200
    copy_count_before = len(product_before.json()["copy_sets"])
    poster_count_before = len(product_before.json()["poster_variants"])
    source_asset_count_before = len(product_before.json()["source_assets"])

    single_run = client.post(
        f"/api/products/{product_id}/workflow/run",
        json={"start_node_id": downstream_image_node["id"]},
    )
    assert single_run.status_code == 200
    single_payload = _wait_for_workflow_run(client, product_id, status="succeeded")
    assert single_payload["runs"][0]["status"] == "succeeded"
    assert [node_run["node_id"] for node_run in single_payload["runs"][0]["node_runs"]] == [downstream_image_node["id"]]

    unchanged_upstream_image = next(node for node in single_payload["nodes"] if node["id"] == upstream_image_node["id"])
    unchanged_reference = next(node for node in single_payload["nodes"] if node["id"] == upstream_reference_node["id"])
    downstream_after = next(node for node in single_payload["nodes"] if node["id"] == downstream_image_node["id"])
    assert unchanged_upstream_image["output_json"]["generated_poster_variant_ids"] == upstream_poster_ids
    assert unchanged_reference["output_json"]["source_asset_ids"] == upstream_reference_asset_ids
    assert downstream_after["output_json"]["copy_set_id"] == unchanged_upstream_image["output_json"]["copy_set_id"]
    assert len(downstream_after["output_json"]["generated_poster_variant_ids"]) == 1
    assert "poster_variant_ids" not in downstream_after["output_json"]

    product_after = client.get(f"/api/products/{product_id}")
    assert product_after.status_code == 200
    assert len(product_after.json()["copy_sets"]) == copy_count_before
    assert len(product_after.json()["poster_variants"]) == poster_count_before + 1
    assert len(product_after.json()["source_assets"]) == source_asset_count_before + 1


def test_workflow_run_kickoff_prevents_duplicate_active_runs(db_session, configured_env: Path) -> None:
    from productflow_backend.application.product_workflows import delete_workflow_node, start_product_workflow_run

    product = create_product(
        db_session,
        name="防重复运行商品",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="product.png",
        content_type="image/png",
    )

    first = start_product_workflow_run(db_session, product_id=product.id)
    second = start_product_workflow_run(db_session, product_id=product.id)

    assert first.created is True
    assert first.should_enqueue is True
    assert second.created is False
    assert second.should_enqueue is True
    assert second.run_id == first.run_id
    assert [run.id for run in second.workflow.runs if run.status == WorkflowRunStatus.RUNNING] == [first.run_id]

    protected_node = first.workflow.nodes[0]
    with pytest.raises(ValueError, match="运行中，稍后删除"):
        delete_workflow_node(db_session, node_id=protected_node.id)
    with pytest.raises(ValueError, match="商品工作流运行中，稍后删除"):
        delete_product(db_session, product_id=product.id)

    db_session.add(WorkflowRun(workflow_id=first.workflow.id, status=WorkflowRunStatus.RUNNING))
    with pytest.raises(sa.exc.IntegrityError):
        db_session.commit()
    db_session.rollback()


def test_workflow_run_endpoint_enqueues_durable_actor_and_reuses_active_run(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    sent_run_ids: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.presentation.routes.product_workflows.enqueue_workflow_run",
        lambda run_id: sent_run_ids.append(run_id),
    )

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "队列工作流商品"},
        files={"image": ("workflow.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    first = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert first.status_code == 200
    first_run_id = first.json()["runs"][0]["id"]
    assert first.json()["runs"][0]["status"] == "running"
    assert sent_run_ids == [first_run_id]

    second = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert second.status_code == 200
    assert second.json()["runs"][0]["id"] == first_run_id
    assert sent_run_ids == [first_run_id, first_run_id]

    session = get_session_factory()()
    try:
        node_run = session.query(WorkflowNodeRun).filter_by(workflow_run_id=first_run_id).first()
        assert node_run is not None
        node_run.status = WorkflowNodeStatus.RUNNING
        session.commit()
    finally:
        session.close()

    third = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert third.status_code == 200
    assert third.json()["runs"][0]["id"] == first_run_id
    assert sent_run_ids == [first_run_id, first_run_id]


def test_workflow_run_enqueue_failure_marks_run_failed(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    def fail_enqueue(_: str) -> None:
        raise RuntimeError("redis unavailable")

    monkeypatch.setattr("productflow_backend.presentation.routes.product_workflows.enqueue_workflow_run", fail_enqueue)

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "入队失败商品"},
        files={"image": ("workflow.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    response = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert response.status_code == 503
    assert response.json()["detail"] == "任务队列暂不可用，请稍后重试"

    workflow = client.get(f"/api/products/{product_id}/workflow")
    assert workflow.status_code == 200
    payload = workflow.json()
    assert payload["runs"][0]["status"] == "failed"
    assert payload["runs"][0]["failure_reason"] == "任务队列暂不可用，请稍后重试"
    assert all(node["status"] not in {"queued", "running"} for node in payload["nodes"])


def test_recover_unfinished_workflow_runs_requeues_queued_runs(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.application.product_workflows import start_product_workflow_run

    product = create_product(
        db_session,
        name="恢复队列工作流",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="workflow.png",
        content_type="image/png",
    )
    kickoff = start_product_workflow_run(db_session, product_id=product.id)
    sent_run_ids: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.infrastructure.queue.enqueue_workflow_run",
        lambda run_id: sent_run_ids.append(run_id),
    )

    summary = recover_unfinished_workflow_runs()

    assert summary.queued_runs == 1
    assert summary.stale_running_runs == 0
    assert summary.enqueued_runs == 1
    assert sent_run_ids == [kickoff.run_id]


def test_recover_unfinished_workflow_runs_resets_stale_running_node_runs(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.application.product_workflows import start_product_workflow_run

    product = create_product(
        db_session,
        name="恢复执行中工作流",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="workflow.png",
        content_type="image/png",
    )
    kickoff = start_product_workflow_run(db_session, product_id=product.id)
    node_run = db_session.query(WorkflowNodeRun).filter_by(workflow_run_id=kickoff.run_id).first()
    assert node_run is not None
    node = db_session.get(WorkflowNode, node_run.node_id)
    assert node is not None
    node_run.status = WorkflowNodeStatus.RUNNING
    node_run.started_at = datetime.now(UTC) - timedelta(hours=2)
    node.status = WorkflowNodeStatus.RUNNING
    db_session.commit()

    sent_run_ids: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.infrastructure.queue.enqueue_workflow_run",
        lambda run_id: sent_run_ids.append(run_id),
    )

    summary = recover_unfinished_workflow_runs(reset_stale_running=True, stale_running_after=timedelta(minutes=30))
    db_session.refresh(node_run)
    db_session.refresh(node)

    assert summary.queued_runs == 0
    assert summary.stale_running_runs == 1
    assert summary.enqueued_runs == 1
    assert sent_run_ids == [kickoff.run_id]
    assert node_run.status == WorkflowNodeStatus.QUEUED
    assert node.status == WorkflowNodeStatus.QUEUED


def test_duplicate_workflow_messages_noop_for_terminal_or_running_runs(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.application.product_workflows import (
        execute_product_workflow_run,
        start_product_workflow_run,
    )

    monkeypatch.setattr(
        "productflow_backend.application.product_workflows._execute_node",
        lambda *args, **kwargs: pytest.fail("duplicate message must not execute providers"),
    )

    product = create_product(
        db_session,
        name="重复消息工作流",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="workflow.png",
        content_type="image/png",
    )
    terminal = start_product_workflow_run(db_session, product_id=product.id)
    terminal_run = db_session.get(WorkflowRun, terminal.run_id)
    assert terminal_run is not None
    terminal_run.status = WorkflowRunStatus.SUCCEEDED
    terminal_run.finished_at = datetime.now(UTC)
    db_session.commit()

    execute_product_workflow_run(terminal.run_id)
    db_session.refresh(terminal_run)
    assert terminal_run.status == WorkflowRunStatus.SUCCEEDED

    product_two = create_product(
        db_session,
        name="执行中重复消息工作流",
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="workflow-two.png",
        content_type="image/png",
    )
    running = start_product_workflow_run(db_session, product_id=product_two.id)
    running_node_run = db_session.query(WorkflowNodeRun).filter_by(workflow_run_id=running.run_id).first()
    assert running_node_run is not None
    running_node_run.status = WorkflowNodeStatus.RUNNING
    running_node_run.started_at = datetime.now(UTC)
    db_session.commit()

    execute_product_workflow_run(running.run_id)
    db_session.refresh(running_node_run)
    assert running_node_run.status == WorkflowNodeStatus.RUNNING


def test_workflow_node_can_be_deleted_with_connected_edges(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "可删节点商品"},
        files={"image": ("node.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]
    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    workflow = workflow_response.json()
    copy_node = next(node for node in workflow["nodes"] if node["node_type"] == "copy_generation")
    connected_edge_ids = {
        edge["id"]
        for edge in workflow["edges"]
        if edge["source_node_id"] == copy_node["id"] or edge["target_node_id"] == copy_node["id"]
    }
    assert connected_edge_ids

    deleted = client.delete(f"/api/workflow-nodes/{copy_node['id']}")
    assert deleted.status_code == 200
    deleted_payload = deleted.json()
    assert copy_node["id"] not in {node["id"] for node in deleted_payload["nodes"]}
    assert all(
        edge["source_node_id"] != copy_node["id"] and edge["target_node_id"] != copy_node["id"]
        for edge in deleted_payload["edges"]
    )
    assert connected_edge_ids.isdisjoint({edge["id"] for edge in deleted_payload["edges"]})

    refreshed = client.get(f"/api/products/{product_id}/workflow")
    assert refreshed.status_code == 200
    assert copy_node["id"] not in {node["id"] for node in refreshed.json()["nodes"]}


def test_product_can_be_deleted_from_api(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "待删除商品"},
        files={"image": ("delete.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]
    product_root = configured_env / "products" / product_id
    assert product_root.exists()
    run = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert run.status_code == 200
    completed = _wait_for_workflow_run(client, product_id, status="succeeded")
    assert completed["runs"][0]["node_runs"]
    product_with_artifacts = client.get(f"/api/products/{product_id}")
    assert product_with_artifacts.status_code == 200
    assert product_with_artifacts.json()["copy_sets"]
    assert product_with_artifacts.json()["poster_variants"]

    deleted = client.delete(f"/api/products/{product_id}")
    assert deleted.status_code == 204
    assert deleted.content == b""

    listed = client.get("/api/products")
    assert listed.status_code == 200
    assert product_id not in {item["id"] for item in listed.json()["items"]}
    missing = client.get(f"/api/products/{product_id}")
    assert missing.status_code == 404
    assert not product_root.exists()


def test_single_reference_run_reruns_upstream_when_target_slot_missing_artifact(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "桌面灯"},
        files={"image": ("lamp.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    workflow_response = client.get(f"/api/products/{product_id}/workflow")
    assert workflow_response.status_code == 200
    workflow = workflow_response.json()
    image_node = next(node for node in workflow["nodes"] if node["node_type"] == "image_generation")

    first_run = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert first_run.status_code == 200
    assert _wait_for_workflow_run(client, product_id, status="succeeded")["runs"][0]["status"] == "succeeded"

    new_reference = client.post(
        f"/api/products/{product_id}/workflow/nodes",
        json={
            "node_type": "reference_image",
            "title": "新增参考图",
            "position_x": 1180,
            "position_y": 380,
            "config_json": {"role": "reference", "label": "新增参考图"},
        },
    )
    assert new_reference.status_code == 201
    new_reference_node = next(node for node in new_reference.json()["nodes"] if node["title"] == "新增参考图")
    connected = client.post(
        f"/api/products/{product_id}/workflow/edges",
        json={
            "source_node_id": image_node["id"],
            "target_node_id": new_reference_node["id"],
            "source_handle": "output",
            "target_handle": "input",
        },
    )
    assert connected.status_code == 201

    product_before = client.get(f"/api/products/{product_id}")
    assert product_before.status_code == 200
    copy_count_before = len(product_before.json()["copy_sets"])

    slot_run = client.post(
        f"/api/products/{product_id}/workflow/run",
        json={"start_node_id": new_reference_node["id"]},
    )
    assert slot_run.status_code == 200
    payload = _wait_for_workflow_run(client, product_id, status="succeeded")
    assert payload["runs"][0]["status"] == "succeeded"
    assert [node_run["node_id"] for node_run in payload["runs"][0]["node_runs"]] == [
        image_node["id"],
        new_reference_node["id"],
    ]
    filled_reference = next(node for node in payload["nodes"] if node["id"] == new_reference_node["id"])
    rerun_image = next(node for node in payload["nodes"] if node["id"] == image_node["id"])
    assert filled_reference["output_json"]["source_asset_ids"]
    assert new_reference_node["id"] in rerun_image["output_json"]["filled_reference_node_ids"]

    product_after = client.get(f"/api/products/{product_id}")
    assert product_after.status_code == 200
    assert len(product_after.json()["copy_sets"]) == copy_count_before


def test_reference_images_can_be_attached_to_product(db_session, configured_env: Path) -> None:
    product = create_product(
        db_session,
        name="陶瓷马克杯",
        category="家居",
        price="39.00",
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="mug.png",
        content_type="image/png",
    )

    updated = add_reference_images(
        db_session,
        product_id=product.id,
        reference_image_uploads=[
            (_make_demo_image_bytes(), "sample-1.png", "image/png"),
            (_make_demo_image_bytes(), "sample-2.png", "image/png"),
        ],
    )

    reference_assets = [asset for asset in updated.source_assets if asset.kind == SourceAssetKind.REFERENCE_IMAGE]
    assert len(reference_assets) == 2
    assert all((Path(configured_env) / asset.storage_path).exists() for asset in reference_assets)


def test_product_reference_image_can_be_deleted(configured_env: Path, db_session) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "香薰蜡烛", "category": "家居", "price": "49.00"},
        files=[
            ("image", ("main.png", _make_demo_image_bytes(), "image/png")),
            ("reference_images", ("ref.png", _make_demo_image_bytes(), "image/png")),
        ],
    )
    assert created.status_code == 201
    payload = created.json()
    original_asset = next(asset for asset in payload["source_assets"] if asset["kind"] == "original_image")
    reference_asset = next(asset for asset in payload["source_assets"] if asset["kind"] == "reference_image")

    db_session.expire_all()
    persisted_reference = db_session.get(SourceAsset, reference_asset["id"])
    assert persisted_reference is not None
    reference_path = Path(configured_env) / persisted_reference.storage_path
    assert reference_path.exists()

    deleted = client.delete(f"/api/source-assets/{reference_asset['id']}")
    assert deleted.status_code == 200
    assert all(asset["id"] != reference_asset["id"] for asset in deleted.json()["source_assets"])

    db_session.expire_all()
    assert db_session.get(SourceAsset, reference_asset["id"]) is None
    assert not reference_path.exists()

    rejected = client.delete(f"/api/source-assets/{original_asset['id']}")
    assert rejected.status_code == 400
    assert "只能删除商品参考图" in rejected.json()["detail"]


def test_image_session_rounds_support_same_conversation(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)

    _login(client)

    created = client.post("/api/image-sessions", json={"title": "护手霜连续生图"})
    assert created.status_code == 201
    session_id = created.json()["id"]

    first = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={
            "prompt": "做一张奶油质感的护手霜广告图，柔光，白底，产品居中",
            "size": "1024x1024",
        },
    )
    assert first.status_code == 200
    first_payload = first.json()
    assert len(first_payload["rounds"]) == 1
    assert first_payload["rounds"][0]["generated_asset"]["download_url"].startswith("/api/image-session-assets/")
    assert first_payload["rounds"][0]["generated_asset"]["preview_url"].endswith("variant=preview")
    assert first_payload["rounds"][0]["generated_asset"]["thumbnail_url"].endswith("variant=thumbnail")
    thumbnail = client.get(first_payload["rounds"][0]["generated_asset"]["thumbnail_url"])
    assert thumbnail.status_code == 200
    assert max(_read_image_size(thumbnail.content)) <= 320

    upload = client.post(
        f"/api/image-sessions/{session_id}/reference-images",
        files={"reference_images": ("sample.png", _make_demo_image_bytes(), "image/png")},
    )
    assert upload.status_code == 200
    upload_payload = upload.json()
    assert any(asset["kind"] == "reference_upload" for asset in upload_payload["assets"])

    second = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={
            "prompt": "保持同样产品和光线，把背景改成浴室台面，增加一点水珠",
            "size": "1024x1024",
        },
    )
    assert second.status_code == 200
    second_payload = second.json()
    assert len(second_payload["rounds"]) == 2
    assert second_payload["rounds"][-1]["provider_name"] == "mock"
    assert second_payload["rounds"][-1]["assistant_message"].startswith("已基于当前对话继续生成")


def test_image_session_reference_image_can_be_deleted(configured_env: Path, db_session) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post("/api/image-sessions", json={"title": "参考图删除"})
    assert created.status_code == 201
    session_id = created.json()["id"]
    upload = client.post(
        f"/api/image-sessions/{session_id}/reference-images",
        files={"reference_images": ("sample.png", _make_demo_image_bytes(), "image/png")},
    )
    assert upload.status_code == 200
    reference_asset = next(asset for asset in upload.json()["assets"] if asset["kind"] == "reference_upload")

    db_session.expire_all()
    persisted_asset = db_session.get(ImageSessionAsset, reference_asset["id"])
    assert persisted_asset is not None
    reference_path = Path(configured_env) / persisted_asset.storage_path
    assert reference_path.exists()

    deleted = client.delete(f"/api/image-sessions/{session_id}/reference-images/{reference_asset['id']}")
    assert deleted.status_code == 200
    assert all(asset["id"] != reference_asset["id"] for asset in deleted.json()["assets"])

    db_session.expire_all()
    assert db_session.get(ImageSessionAsset, reference_asset["id"]) is None
    assert not reference_path.exists()


def test_image_session_can_be_deleted_with_files(configured_env: Path, db_session) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post("/api/image-sessions", json={"title": "整会话删除"})
    assert created.status_code == 201
    session_id = created.json()["id"]
    upload = client.post(
        f"/api/image-sessions/{session_id}/reference-images",
        files={"reference_images": ("sample.png", _make_demo_image_bytes(), "image/png")},
    )
    assert upload.status_code == 200
    generated = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "做一张白底商品图", "size": "1024x1024"},
    )
    assert generated.status_code == 200

    db_session.expire_all()
    asset_paths = [
        Path(configured_env) / asset.storage_path
        for asset in db_session.query(ImageSessionAsset).filter(ImageSessionAsset.session_id == session_id).all()
    ]
    assert asset_paths
    assert all(path.exists() for path in asset_paths)
    session_root = Path(configured_env) / "image_sessions" / session_id
    assert session_root.exists()

    deleted = client.delete(f"/api/image-sessions/{session_id}")
    assert deleted.status_code == 204

    listed = client.get("/api/image-sessions")
    assert listed.status_code == 200
    assert all(item["id"] != session_id for item in listed.json()["items"])

    db_session.expire_all()
    assert db_session.get(ImageSession, session_id) is None
    assert all(not path.exists() for path in asset_paths)
    assert not session_root.exists()


def test_image_session_result_can_write_back_to_product(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    create_product_response = client.post(
        "/api/products",
        data={"name": "护手霜", "category": "个护", "price": "59.00"},
        files={"image": ("cream.png", _make_demo_image_bytes(), "image/png")},
    )
    assert create_product_response.status_code == 201
    product_id = create_product_response.json()["id"]

    created = client.post("/api/image-sessions", json={"product_id": product_id})
    assert created.status_code == 201
    session_id = created.json()["id"]

    generated = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "做一张高级浴室台面护手霜广告图", "size": "1024x1024"},
    )
    assert generated.status_code == 200
    generated_payload = generated.json()
    generated_asset_id = generated_payload["rounds"][-1]["generated_asset"]["id"]

    attach_reference = client.post(
        f"/api/image-sessions/{session_id}/assets/{generated_asset_id}/attach-to-product",
        json={"target": "reference"},
    )
    assert attach_reference.status_code == 200
    assert attach_reference.json()["message"] == "已加入商品参考图"

    product_after_reference = client.get(f"/api/products/{product_id}")
    assert product_after_reference.status_code == 200
    reference_assets = [
        asset for asset in product_after_reference.json()["source_assets"] if asset["kind"] == "reference_image"
    ]
    assert len(reference_assets) >= 1

    attach_main = client.post(
        f"/api/image-sessions/{session_id}/assets/{generated_asset_id}/attach-to-product",
        json={"target": "main_source"},
    )
    assert attach_main.status_code == 200
    assert attach_main.json()["message"] == "已设为商品主图"

    product_after_main = client.get(f"/api/products/{product_id}")
    assert product_after_main.status_code == 200
    original_assets = [
        asset for asset in product_after_main.json()["source_assets"] if asset["kind"] == "original_image"
    ]
    all_reference_assets = [
        asset for asset in product_after_main.json()["source_assets"] if asset["kind"] == "reference_image"
    ]
    assert len(original_assets) == 1
    assert len(all_reference_assets) >= 2


def test_product_asset_variant_urls_serve_preview_and_thumbnail(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    create_product_response = client.post(
        "/api/products",
        data={"name": "大尺寸主图样例", "category": "个护", "price": "99.00"},
        files={"image": ("large.png", _make_demo_image_bytes_with_size(2400, 1800), "image/png")},
    )
    assert create_product_response.status_code == 201
    source_asset = next(
        asset for asset in create_product_response.json()["source_assets"] if asset["kind"] == "original_image"
    )

    assert source_asset["download_url"].startswith("/api/source-assets/")
    assert source_asset["preview_url"].endswith("variant=preview")
    assert source_asset["thumbnail_url"].endswith("variant=thumbnail")

    preview = client.get(source_asset["preview_url"])
    assert preview.status_code == 200
    assert preview.headers["content-type"].startswith("image/")
    assert max(_read_image_size(preview.content)) <= 1600

    thumbnail = client.get(source_asset["thumbnail_url"])
    assert thumbnail.status_code == 200
    assert thumbnail.headers["content-type"].startswith("image/")
    assert max(_read_image_size(thumbnail.content)) <= 320


def test_image_session_openai_responses_persists_previous_response_context(
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

    first = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "生成日漫风商品场景", "size": "1024x1024"},
    )
    assert first.status_code == 200
    first_round = first.json()["rounds"][-1]
    assert first_round["provider_name"] == "openai-responses"
    assert first_round["provider_response_id"] == "resp_1"
    assert first_round["previous_response_id"] is None
    assert first_round["image_generation_call_id"] == "ig_1"

    second = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "保持主体，把背景改成晴天街角", "size": "1024x1024"},
    )
    assert second.status_code == 200
    second_round = second.json()["rounds"][-1]
    assert second_round["provider_response_id"] == "resp_2"
    assert second_round["previous_response_id"] == "resp_1"
    assert second_round["image_generation_call_id"] == "ig_2"

    assert client_kwargs[0] == {"api_key": "demo-api-key", "base_url": "https://example.test/v1"}
    assert calls[0]["model"] == "gpt-5.4"
    assert calls[0]["tools"] == [{"type": "image_generation", "size": "1024x1024"}]
    assert "previous_response_id" not in calls[0]
    assert calls[1]["previous_response_id"] == "resp_1"
    assert calls[1]["tools"] == [{"type": "image_generation", "size": "1024x1024"}]
    first_content = calls[0]["input"][0]["content"]
    assert first_content[0]["type"] == "input_text"
    assert any(
        item["type"] == "input_image" and item["image_url"].startswith("data:image/png;base64,")
        for item in first_content
    )
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
    assert model_name == "gpt-5.4"
    payload = calls[0]
    assert payload["model"] == "gpt-5.4"
    assert payload["tools"] == [{"type": "image_generation", "size": "1024x1024"}]
    content = payload["input"][0]["content"]
    assert content[0]["type"] == "input_text"
    prompt_text = content[0]["text"]
    assert "商品描述/补充说明：防水牛津布，适合通勤和短途出差。" in prompt_text
    assert "本轮图片要求：背景更干净，强调收纳空间。" in prompt_text
    assert len([item for item in content if item["type"] == "input_image"]) == 2
    assert "/images/generations" not in str(payload)
    assert "/images/edits" not in str(payload)


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

    copy_job = create_copy_job(db_session, product_id=product.id).job
    execute_copy_job(copy_job.id)
    db_session.expire_all()

    product_after_copy = get_product_detail(db_session, product.id)
    copy_set = product_after_copy.copy_sets[0]
    confirm_copy_set(db_session, copy_set_id=copy_set.id)

    poster_job = create_poster_job(db_session, product_id=product.id).job
    execute_poster_job(poster_job.id)
    db_session.expire_all()

    product_after_poster = get_product_detail(db_session, product.id)
    assert len(product_after_poster.poster_variants) == 2
    assert all("mock:mock-generated-r3" in poster.template_name for poster in product_after_poster.poster_variants)


def test_duplicate_active_copy_job_reuses_existing_job(db_session, configured_env: Path) -> None:
    product = create_product(
        db_session,
        name="收纳盒",
        category="家居",
        price="19.90",
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="box.png",
        content_type="image/png",
    )

    first_result = create_copy_job(db_session, product_id=product.id)
    second_result = create_copy_job(db_session, product_id=product.id)

    assert first_result.job.id == second_result.job.id
    assert first_result.created is True
    assert second_result.created is False


def test_default_log_dir_uses_backend_storage_when_running_from_backend(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.config import get_settings
    from productflow_backend.infrastructure.logging import get_log_file_path

    backend_dir = Path(__file__).resolve().parents[1]
    monkeypatch.delenv("LOG_DIR", raising=False)
    monkeypatch.chdir(backend_dir)
    get_settings.cache_clear()

    settings = get_settings()

    assert settings.log_dir == backend_dir / "storage" / "logs"
    assert get_log_file_path(settings) == backend_dir / "storage" / "logs" / "productflow.log"


def test_log_cleanup_deletes_expired_persistent_logs(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    from productflow_backend.config import get_settings
    from productflow_backend.infrastructure.logging import cleanup_old_logs

    log_dir = tmp_path / "logs"
    log_dir.mkdir()
    old_log = log_dir / "old.log"
    fresh_log = log_dir / "fresh.log"
    old_log.write_text("old", encoding="utf-8")
    fresh_log.write_text("fresh", encoding="utf-8")
    old_timestamp = time.time() - 3 * 24 * 60 * 60
    old_log.touch()
    fresh_log.touch()
    os.utime(old_log, (old_timestamp, old_timestamp))
    monkeypatch.setenv("LOG_DIR", str(log_dir))
    monkeypatch.setenv("LOG_RETENTION_DAYS", "1")
    get_settings.cache_clear()

    deleted = cleanup_old_logs(get_settings())

    assert deleted == 1
    assert not old_log.exists()
    assert fresh_log.exists()


def test_configure_logging_keeps_single_stdout_handler_and_log_dir_override(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
    capsys: pytest.CaptureFixture[str],
) -> None:
    from productflow_backend.config import get_settings
    from productflow_backend.infrastructure.logging import configure_logging, get_log_file_path

    log_dir = tmp_path / "stdout-logs"
    monkeypatch.setenv("LOG_DIR", str(log_dir))
    monkeypatch.setenv("LOG_LEVEL", "INFO")
    get_settings.cache_clear()
    settings = get_settings()

    root_logger = logging.getLogger()
    original_handlers = list(root_logger.handlers)
    original_level = root_logger.level
    original_propagate = root_logger.propagate

    try:
        root_logger.handlers = []
        configure_logging(settings)
        configure_logging(settings)

        logging.getLogger("productflow_backend.tests.stdout").info("stdout and file visible line")
        for handler in root_logger.handlers:
            handler.flush()

        productflow_stream_handlers = [
            handler for handler in root_logger.handlers if getattr(handler, "_productflow_stream_handler", False)
        ]
        productflow_file_handlers = [
            handler for handler in root_logger.handlers if getattr(handler, "_productflow_file_handler", False)
        ]
        captured = capsys.readouterr()
        log_text = get_log_file_path(settings).read_text(encoding="utf-8")

        assert get_log_file_path(settings).parent == log_dir
        assert len(productflow_stream_handlers) == 1
        assert len(productflow_file_handlers) == 1
        assert "stdout and file visible line" in captured.out
        assert log_text.count("stdout and file visible line") == 1
    finally:
        for handler in list(root_logger.handlers):
            root_logger.removeHandler(handler)
            handler.close()
        root_logger.handlers = original_handlers
        root_logger.setLevel(original_level)
        root_logger.propagate = original_propagate


def test_configure_logging_mirrors_uvicorn_lifecycle_and_access_logs(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    from productflow_backend.config import get_settings
    from productflow_backend.infrastructure.logging import configure_logging, get_log_file_path

    log_dir = tmp_path / "uvicorn-logs"
    monkeypatch.setenv("LOG_DIR", str(log_dir))
    monkeypatch.setenv("LOG_LEVEL", "INFO")
    get_settings.cache_clear()
    settings = get_settings()

    root_logger = logging.getLogger()
    uvicorn_logger = logging.getLogger("uvicorn")
    error_logger = logging.getLogger("uvicorn.error")
    access_logger = logging.getLogger("uvicorn.access")
    loggers = (root_logger, uvicorn_logger, error_logger, access_logger)
    original_state = {
        logger.name: (list(logger.handlers), logger.level, logger.propagate)
        for logger in loggers
    }

    try:
        uvicorn_logger.propagate = False
        error_logger.propagate = True
        access_logger.propagate = False

        configure_logging(settings)
        configure_logging(settings)

        logging.getLogger("productflow_backend.tests.logging").info("application persistent line")
        error_logger.info("Started server process [12345]")
        error_logger.info("Application startup complete.")
        access_logger.info('%s - "%s %s HTTP/%s" %d', "127.0.0.1:29282", "GET", "/healthz", "1.1", 200)
        for logger in loggers:
            for handler in logger.handlers:
                handler.flush()

        log_text = get_log_file_path(settings).read_text(encoding="utf-8")

        assert log_text.count("application persistent line") == 1
        assert log_text.count("Started server process [12345]") == 1
        assert log_text.count("Application startup complete.") == 1
        assert log_text.count('127.0.0.1:29282 - "GET /healthz HTTP/1.1" 200 OK') == 1
        productflow_file_handlers = [
            handler
            for logger in (root_logger, error_logger, access_logger)
            for handler in logger.handlers
            if getattr(handler, "_productflow_file_handler", False)
        ]
        assert len({id(handler) for handler in productflow_file_handlers}) == 1
        assert not any(
            getattr(handler, "_productflow_stream_handler", False)
            for logger in (error_logger, access_logger)
            for handler in logger.handlers
        )
    finally:
        for logger in loggers:
            saved_handlers, saved_level, saved_propagate = original_state[logger.name]
            for handler in list(logger.handlers):
                if handler not in saved_handlers:
                    logger.removeHandler(handler)
                    handler.close()
            logger.handlers = saved_handlers
            logger.setLevel(saved_level)
            logger.propagate = saved_propagate



def test_recover_unfinished_jobs_requeues_queued_jobs(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    product = create_product(
        db_session,
        name="收纳盒",
        category="家居",
        price="19.90",
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="box.png",
        content_type="image/png",
    )
    job = create_copy_job(db_session, product_id=product.id).job
    sent: list[tuple[str, JobKind]] = []

    monkeypatch.setattr(
        "productflow_backend.infrastructure.queue._send_job_to_queue",
        lambda job_id, kind: sent.append((job_id, kind)),
    )

    summary = recover_unfinished_jobs()

    assert summary.queued_jobs == 1
    assert summary.stale_running_jobs == 0
    assert summary.enqueued_jobs == 1
    assert sent == [(job.id, JobKind.COPY_GENERATION)]


def test_recover_unfinished_jobs_resets_stale_running_jobs(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    product = create_product(
        db_session,
        name="收纳盒",
        category="家居",
        price="19.90",
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="box.png",
        content_type="image/png",
    )
    job = create_copy_job(db_session, product_id=product.id).job
    job.status = JobStatus.RUNNING
    job.started_at = datetime.now(UTC) - timedelta(hours=2)
    db_session.commit()
    sent: list[tuple[str, JobKind]] = []

    monkeypatch.setattr(
        "productflow_backend.infrastructure.queue._send_job_to_queue",
        lambda job_id, kind: sent.append((job_id, kind)),
    )

    summary = recover_unfinished_jobs(reset_stale_running=True, stale_running_after=timedelta(minutes=30))
    db_session.refresh(job)

    assert summary.queued_jobs == 0
    assert summary.stale_running_jobs == 1
    assert summary.enqueued_jobs == 1
    assert sent == [(job.id, JobKind.COPY_GENERATION)]
    assert job.status == JobStatus.QUEUED
    assert job.started_at is None


def test_product_create_rejects_invalid_price_and_invalid_image(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    invalid_price = client.post(
        "/api/products",
        data={"name": "护手霜", "category": "个护", "price": "abc"},
        files={"image": ("cream.png", _make_demo_image_bytes(), "image/png")},
    )
    assert invalid_price.status_code == 400

    invalid_image = client.post(
        "/api/products",
        data={"name": "护手霜", "category": "个护", "price": "59.00"},
        files={"image": ("cream.png", b"not an image", "image/png")},
    )
    assert invalid_image.status_code == 400


def test_image_generation_rejects_disallowed_size(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post("/api/image-sessions", json={"title": "尺寸校验"})
    assert created.status_code == 201
    rejected = client.post(
        f"/api/image-sessions/{created.json()['id']}/generate",
        json={"prompt": "生成一张图", "size": "99999x99999"},
    )
    assert rejected.status_code == 422


def test_legacy_image_chat_route_is_removed(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    response = client.post(
        "/api/image-chat/generate",
        json={"prompt": "做一张白底商品图", "size": "1024x1024"},
    )
    assert response.status_code == 404


def test_alembic_upgrade_head_supports_sqlite(tmp_path: Path, monkeypatch) -> None:
    database_path = tmp_path / "alembic.db"
    storage_root = tmp_path / "storage"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(storage_root))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "head")

    assert database_path.exists()
    get_settings.cache_clear()


def test_alembic_upgrade_removes_legacy_workflow_nodes(tmp_path: Path, monkeypatch) -> None:
    database_path = tmp_path / "legacy-workflow.db"
    storage_root = tmp_path / "storage"
    monkeypatch.setenv("ADMIN_ACCESS_KEY", "super-secret-admin-key")
    monkeypatch.setenv("SESSION_SECRET", "super-secret-session-key-123")
    monkeypatch.setenv("DATABASE_URL", f"sqlite:///{database_path}")
    monkeypatch.setenv("REDIS_URL", "redis://localhost:6379/9")
    monkeypatch.setenv("STORAGE_ROOT", str(storage_root))
    get_settings.cache_clear()

    backend_dir = Path(__file__).resolve().parents[1]
    config = Config(str(backend_dir / "alembic.ini"))
    config.set_main_option("script_location", str(backend_dir / "alembic"))
    command.upgrade(config, "20260424_0009")

    engine = sa.create_engine(f"sqlite:///{database_path}")
    now = "2026-04-24 00:00:00"
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO products (id, name, created_at, updated_at) "
                "VALUES ('product-1', '旧工作流商品', :now, :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO product_workflows (id, product_id, title, active, created_at, updated_at) "
                "VALUES ('workflow-1', 'product-1', '旧工作流', 1, :now, :now)"
            ),
            {"now": now},
        )
        for node_id, node_type in (
            ("context-1", "product_context"),
            ("copy-1", "copy_generation"),
            ("legacy-text-1", "legacy_text"),
            ("image-1", "image_generation"),
            ("legacy-result-1", "legacy_result"),
            ("slot-1", "image_upload"),
        ):
            connection.execute(
                sa.text(
                    "INSERT INTO workflow_nodes "
                    "(id, workflow_id, node_type, title, position_x, position_y, config_json, status, "
                    "created_at, updated_at) "
                    "VALUES (:id, 'workflow-1', :node_type, :id, 0, 0, '{}', 'idle', :now, :now)"
                ),
                {"id": node_id, "node_type": node_type, "now": now},
            )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_edges "
                "(id, workflow_id, source_node_id, target_node_id, source_handle, target_handle, created_at) "
                "VALUES "
                "('edge-old-target', 'workflow-1', 'copy-1', 'legacy-text-1', 'output', 'input', :now), "
                "('edge-old-source', 'workflow-1', 'legacy-result-1', 'image-1', 'output', 'input', :now), "
                "('edge-supported', 'workflow-1', 'context-1', 'copy-1', 'output', 'input', :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_runs (id, workflow_id, status, started_at) "
                "VALUES ('run-1', 'workflow-1', 'running', :now)"
            ),
            {"now": now},
        )
        connection.execute(
            sa.text(
                "INSERT INTO workflow_node_runs (id, workflow_run_id, node_id, status, started_at) "
                "VALUES "
                "('node-run-old', 'run-1', 'legacy-text-1', 'succeeded', :now), "
                "('node-run-supported', 'run-1', 'copy-1', 'succeeded', :now)"
            ),
            {"now": now},
        )

    command.upgrade(config, "head")

    with engine.connect() as connection:
        node_types = connection.execute(sa.text("SELECT node_type FROM workflow_nodes ORDER BY id")).scalars().all()
        edge_ids = connection.execute(sa.text("SELECT id FROM workflow_edges ORDER BY id")).scalars().all()
        node_run_ids = connection.execute(sa.text("SELECT id FROM workflow_node_runs ORDER BY id")).scalars().all()

    assert node_types == ["product_context", "copy_generation", "image_generation", "reference_image"]
    assert edge_ids == ["edge-supported"]
    assert node_run_ids == ["node-run-supported"]
    get_settings.cache_clear()
