from __future__ import annotations

import base64
from io import BytesIO
from typing import TYPE_CHECKING

import pytest
from fastapi.testclient import TestClient
from PIL import Image

if TYPE_CHECKING:
    from productflow_backend.application.product_workflow_dependencies import WorkflowExecutionDependencies


def _make_demo_image_bytes() -> bytes:
    return _make_demo_image_bytes_with_size(800, 800)


def _make_demo_image_bytes_with_size(width: int, height: int) -> bytes:
    image = Image.new("RGB", (width, height), (240, 240, 240))
    buffer = BytesIO()
    image.save(buffer, format="PNG")
    return buffer.getvalue()


def _make_demo_image_data_url() -> str:
    encoded = base64.b64encode(_make_demo_image_bytes()).decode("utf-8")
    return f"data:image/png;base64,{encoded}"


def _read_image_size(image_bytes: bytes) -> tuple[int, int]:
    with Image.open(BytesIO(image_bytes)) as image:
        return image.size


def _login(client: TestClient) -> None:
    login = client.post("/api/auth/session", json={"admin_key": "super-secret-admin-key"})
    assert login.status_code == 200
    assert "session" in client.cookies, "login did not persist session cookie"
    state = client.get("/api/auth/session")
    assert state.status_code == 200, f"session state failed after login: {state.status_code}: {state.text}"
    assert state.json()["authenticated"] is True, (
        f"login response did not create authenticated session: "
        f"has_session_cookie={'session' in client.cookies} state={state.text}"
    )


def _unlock_settings(client: TestClient) -> None:
    unlock = client.post("/api/settings/unlock", json={"token": "super-secret-settings-token"})
    assert unlock.status_code == 200


def _enable_deletion(client: TestClient) -> None:
    _unlock_settings(client)
    response = client.patch("/api/settings", json={"values": {"deletion_enabled": True}})
    assert response.status_code == 200


def _execute_workflow_queue_inline(
    monkeypatch: pytest.MonkeyPatch,
    *,
    dependencies: WorkflowExecutionDependencies | None = None,
) -> None:
    from productflow_backend.application.product_workflows import (
        execute_product_workflow_node_run,
        execute_product_workflow_run,
    )

    def execute_inline(run_id: str) -> None:
        execute_product_workflow_run(run_id)

    def execute_node_inline(node_run_id: str) -> None:
        execute_product_workflow_node_run(node_run_id, dependencies=dependencies)

    monkeypatch.setattr(
        "productflow_backend.application.product_workflow.execution.enqueue_workflow_run",
        execute_inline,
    )
    monkeypatch.setattr(
        "productflow_backend.application.product_workflow.execution.enqueue_workflow_node_run",
        execute_node_inline,
    )
