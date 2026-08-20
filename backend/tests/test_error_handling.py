from __future__ import annotations

import json

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes

from productflow_backend.application.image_sessions.service import (
    create_image_session,
    submit_image_session_generation_task,
)
from productflow_backend.application.products import create_canonical_product
from productflow_backend.domain.errors import (
    BusinessError,
    BusinessValidationError,
    NotFoundError,
    QueueUnavailableError,
)
from productflow_backend.infrastructure.logging import current_log_context
from productflow_backend.presentation.api import create_app
from productflow_backend.presentation.errors import business_error_to_response


def test_typed_not_found_maps_to_404_without_message_suffix() -> None:
    response = business_error_to_response(NotFoundError("资源已移除"))

    assert response.status_code == 404
    assert json.loads(response.body) == {"detail": "资源已移除"}


def test_typed_business_error_maps_to_400() -> None:
    response = business_error_to_response(BusinessError("请选择一张图片"))

    assert response.status_code == 400
    assert json.loads(response.body) == {"detail": "请选择一张图片"}


def test_typed_image_file_missing_remains_400() -> None:
    response = business_error_to_response(BusinessValidationError("图片文件不存在"))

    assert response.status_code == 400
    assert json.loads(response.body) == {"detail": "图片文件不存在"}


def test_typed_workflow_integrity_error_remains_400() -> None:
    response = business_error_to_response(BusinessValidationError("工作流连线引用了不存在的节点"))

    assert response.status_code == 400
    assert json.loads(response.body) == {"detail": "工作流连线引用了不存在的节点"}


def test_typed_queue_unavailable_maps_to_503() -> None:
    response = business_error_to_response(QueueUnavailableError("任务队列暂不可用，请稍后重试"))

    assert response.status_code == 503
    assert json.loads(response.body) == {"detail": "任务队列暂不可用，请稍后重试"}


def test_global_business_error_handler_preserves_detail_shape(configured_env) -> None:  # noqa: ARG001
    app = create_app()
    assert BusinessError in app.exception_handlers
    assert ValueError not in app.exception_handlers
    assert Exception not in app.exception_handlers

    @app.get("/typed-not-found")
    def typed_not_found() -> None:
        assert current_log_context()["request_id"] == "typed-request-1"
        raise NotFoundError("资源已移除")

    client = TestClient(app)
    response = client.get("/typed-not-found", headers={"X-Request-ID": "typed-request-1"})

    assert response.status_code == 404
    assert response.headers["X-Request-ID"] == "typed-request-1"
    assert response.json() == {"detail": "资源已移除"}
    assert "code" not in response.json()
    assert current_log_context()["request_id"] == "-"


def test_product_workflow_route_uses_global_business_error_handler(configured_env) -> None:  # noqa: ARG001
    app = create_app()
    client = TestClient(app)
    _login(client)

    response = client.get("/api/v2/products/missing-product/workflow")

    assert response.status_code == 404
    assert response.json() == {"detail": "商品不存在"}


def test_product_route_uses_global_business_error_handler(configured_env) -> None:  # noqa: ARG001
    app = create_app()
    client = TestClient(app)
    _login(client)

    missing = client.get("/api/v2/products/missing-product")

    assert missing.status_code == 404
    assert missing.json() == {"detail": "商品不存在"}
    assert "code" not in missing.json()

    invalid = client.post(
        "/api/v2/products",
        data={"name": "   "},
        files={"images": ("blank.png", _make_demo_image_bytes(), "image/png")},
    )

    assert invalid.status_code == 400
    assert invalid.json() == {"detail": "商品名不能为空"}
    assert "code" not in invalid.json()


def test_image_session_route_uses_global_business_error_handler(configured_env) -> None:  # noqa: ARG001
    app = create_app()
    client = TestClient(app)
    _login(client)

    missing = client.get("/api/image-sessions/missing-session")

    assert missing.status_code == 404
    assert missing.json() == {"detail": "连续生图会话不存在"}
    assert "code" not in missing.json()

    created = client.post("/api/image-sessions", json={"title": "typed route 生图"})
    assert created.status_code == 201
    session_id = created.json()["id"]
    first = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "首轮排队", "size": "1024x1024"},
    )
    assert first.status_code == 202
    invalid = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "第二轮缺少基图", "size": "1024x1024"},
    )

    assert invalid.status_code == 400
    assert invalid.json() == {"detail": "后续生图必须选择一张本会话已生成图片作为基图"}
    assert "code" not in invalid.json()


def test_high_risk_business_paths_raise_typed_validation_errors(db_session, configured_env) -> None:  # noqa: ARG001
    with pytest.raises(BusinessValidationError, match="商品名不能为空"):
        create_canonical_product(
            db_session,
            name="   ",
            category=None,
            price=None,
            source_note=None,
            image_uploads=[(_make_demo_image_bytes(), "blank.png", "image/png")],
        )
    db_session.rollback()

    with pytest.raises(BusinessValidationError, match="价格格式不正确"):
        create_canonical_product(
            db_session,
            name="价格格式错误商品",
            category=None,
            price="abc",
            source_note=None,
            image_uploads=[(_make_demo_image_bytes(), "invalid-price.png", "image/png")],
        )
    db_session.rollback()

    image_session = create_image_session(db_session, title="typed error 生图")
    with pytest.raises(BusinessValidationError, match="一次生成数量必须在 1-10 张之间"):
        submit_image_session_generation_task(
            db_session,
            image_session_id=image_session.id,
            prompt="数量越界",
            size="1024x1024",
            generation_count=11,
        )
