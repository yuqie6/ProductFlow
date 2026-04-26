from __future__ import annotations

from pathlib import Path

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes

from productflow_backend.application.use_cases import create_copy_job, create_product
from productflow_backend.infrastructure.db.models import AppSetting, CopySet


def _set_generation_cap(db_session, value: int) -> None:
    db_session.add(AppSetting(key="generation_max_concurrent_tasks", value=str(value)))
    db_session.commit()


def _create_product(db_session, name: str):
    return create_product(
        db_session,
        name=name,
        category=None,
        price=None,
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename=f"{name}.png",
        content_type="image/png",
    )


def _add_confirmed_copy_set(db_session, product) -> CopySet:
    copy_set = CopySet(
        product_id=product.id,
        title=f"{product.name} 文案",
        selling_points=["卖点一"],
        poster_headline=f"{product.name} 海报",
        cta="立即了解",
        model_title=f"{product.name} 文案",
        model_selling_points=["卖点一"],
        model_poster_headline=f"{product.name} 海报",
        model_cta="立即了解",
        provider_name="test",
        model_name="test",
        prompt_version="test",
    )
    db_session.add(copy_set)
    db_session.flush()
    product.current_confirmed_copy_set_id = copy_set.id
    db_session.commit()
    return copy_set


def test_generation_cap_rejects_async_resource_entrypoints(
    configured_env: Path,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    monkeypatch.setattr(
        "productflow_backend.presentation.routes.products.enqueue_copy_job",
        lambda job_id: pytest.fail(f"busy copy job must not enqueue: {job_id}"),
    )
    monkeypatch.setattr(
        "productflow_backend.presentation.routes.products.enqueue_poster_job",
        lambda job_id: pytest.fail(f"busy poster job must not enqueue: {job_id}"),
    )
    monkeypatch.setattr(
        "productflow_backend.presentation.routes.product_workflows.enqueue_workflow_run",
        lambda run_id: pytest.fail(f"busy workflow run must not enqueue: {run_id}"),
    )

    busy_product = _create_product(db_session, "占用并发商品")
    create_copy_job(db_session, product_id=busy_product.id)
    copy_target = _create_product(db_session, "文案限流商品")
    poster_target = _create_product(db_session, "海报限流商品")
    _add_confirmed_copy_set(db_session, poster_target)
    workflow_target = _create_product(db_session, "工作流限流商品")
    _set_generation_cap(db_session, 1)

    app = create_app()
    client = TestClient(app)
    _login(client)

    copy_response = client.post(f"/api/products/{copy_target.id}/copy-jobs")
    assert copy_response.status_code == 429
    assert copy_response.json()["detail"] == "当前生成任务较多，请稍后再试"

    poster_response = client.post(f"/api/products/{poster_target.id}/poster-jobs")
    assert poster_response.status_code == 429
    assert poster_response.json()["detail"] == "当前生成任务较多，请稍后再试"

    workflow_response = client.post(f"/api/products/{workflow_target.id}/workflow/run", json={})
    assert workflow_response.status_code == 429
    assert workflow_response.json()["detail"] == "当前生成任务较多，请稍后再试"


def test_generation_cap_allows_idempotent_existing_copy_job_response(
    configured_env: Path,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.presentation.api import create_app

    monkeypatch.setattr(
        "productflow_backend.presentation.routes.products.enqueue_copy_job",
        lambda job_id: pytest.fail(f"existing active copy job must not enqueue again: {job_id}"),
    )

    product = _create_product(db_session, "重复提交商品")
    existing_job = create_copy_job(db_session, product_id=product.id).job
    _set_generation_cap(db_session, 1)

    app = create_app()
    client = TestClient(app)
    _login(client)

    response = client.post(f"/api/products/{product.id}/copy-jobs")

    assert response.status_code == 202
    payload = response.json()
    assert payload["id"] == existing_job.id
    assert payload["status"] == "queued"


def test_generation_cap_does_not_mask_poster_business_validation(
    configured_env: Path,
    db_session,
) -> None:
    from productflow_backend.presentation.api import create_app

    busy_product = _create_product(db_session, "占用并发商品")
    create_copy_job(db_session, product_id=busy_product.id)
    poster_target = _create_product(db_session, "缺少确认文案商品")
    _set_generation_cap(db_session, 1)

    app = create_app()
    client = TestClient(app)
    _login(client)

    response = client.post(f"/api/products/{poster_target.id}/poster-jobs")

    assert response.status_code == 400
    assert response.json()["detail"] == "请先确认一版文案，再生成海报"


def test_generation_cap_rejects_synchronous_image_session_generation(
    configured_env: Path,
    db_session,
) -> None:
    from productflow_backend.presentation.api import create_app

    busy_product = _create_product(db_session, "同步占用商品")
    create_copy_job(db_session, product_id=busy_product.id)
    _set_generation_cap(db_session, 1)

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post("/api/image-sessions", json={"title": "同步限流"})
    assert created.status_code == 201

    generated = client.post(
        f"/api/image-sessions/{created.json()['id']}/generate",
        json={"prompt": "这次应该被并发上限拦截", "size": "1024x1024"},
    )

    assert generated.status_code == 429
    assert generated.json()["detail"] == "当前生成任务较多，请稍后再试"


def test_synchronous_generation_admission_releases_slot_on_exception(
    configured_env: Path,
    db_session,
) -> None:
    from productflow_backend.application.admission import active_generation_task_count, admit_synchronous_generation

    assert active_generation_task_count(db_session) == 0
    with pytest.raises(RuntimeError, match="provider failed"):
        with admit_synchronous_generation(db_session):
            assert active_generation_task_count(db_session) == 1
            raise RuntimeError("provider failed")

    assert active_generation_task_count(db_session) == 0
