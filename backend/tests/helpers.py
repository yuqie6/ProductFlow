from __future__ import annotations

import base64
import time
from io import BytesIO

import pytest
from fastapi.testclient import TestClient
from PIL import Image


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


def _execute_workflow_queue_inline(monkeypatch: pytest.MonkeyPatch) -> None:
    from productflow_backend.application.product_workflows import execute_product_workflow_run

    monkeypatch.setattr(
        "productflow_backend.presentation.routes.product_workflows.enqueue_workflow_run",
        execute_product_workflow_run,
    )
