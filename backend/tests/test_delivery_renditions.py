from __future__ import annotations

from collections.abc import Callable
from datetime import UTC, datetime, timedelta
from io import BytesIO

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes
from PIL import Image
from sqlalchemy import func, select
from workflow_draft_helpers import make_workflow_draft_payload

from productflow_backend.application.async_delivery import run_async_dispatcher_once
from productflow_backend.application.delivery_renditions.renderer import render_delivery_rendition
from productflow_backend.application.delivery_renditions.service import (
    _fail_delivery_rendition_job,
    _persist_delivery_rendition_result,
    claim_delivery_rendition_job,
    create_delivery_rendition_job,
    execute_delivery_rendition_job,
    get_delivery_rendition_job,
    mark_delivery_rendition_job_enqueue_failed,
    retry_delivery_rendition_job,
    submit_delivery_rendition_job,
)
from productflow_backend.application.durable_recovery import recover_unfinished_delivery_rendition_jobs
from productflow_backend.application.product_images.assets import (
    clear_product_cover,
    delete_product_image_asset,
)
from productflow_backend.application.product_images.queries import get_gallery_asset_detail
from productflow_backend.application.product_workflow import execution as workflow_execution
from productflow_backend.application.product_workflow.dependencies import WorkflowExecutionDependencies
from productflow_backend.application.product_workflow.v2_execution import execute_v2_workflow_node_run
from productflow_backend.application.products import create_canonical_product, delete_product
from productflow_backend.application.workflow_drafts.materialization import materialize_workflow_draft
from productflow_backend.application.workflow_drafts.service import (
    confirm_workflow_draft_revision,
    create_workflow_draft,
)
from productflow_backend.domain.enums import (
    AsyncDispatchStatus,
    JobStatus,
    WorkflowNodeStatus,
    WorkflowNodeType,
    WorkflowRunStatus,
)
from productflow_backend.domain.errors import (
    BusinessValidationError,
    ConflictError,
    QueueUnavailableError,
)
from productflow_backend.infrastructure.db.models import (
    AsyncDispatch,
    DeliveryRenditionJob,
    Product,
    ProductImageAsset,
    WorkflowImageGenerationRecord,
    WorkflowNodeRun,
    WorkflowRun,
)
from productflow_backend.infrastructure.image.base import (
    ImageProvider,
    WorkflowGeneratedImage,
    WorkflowImageRequest,
    WorkflowImageResult,
)
from productflow_backend.infrastructure.storage import LocalStorage
from productflow_backend.presentation.schemas.delivery_renditions import DeliveryRenditionJobResponse


class StaticWorkflowImageProvider(ImageProvider):
    provider_name = "static-workflow-image"

    def __init__(self, image_bytes: bytes, *, on_generate: Callable[[], None] | None = None) -> None:
        self.image_bytes = image_bytes
        self.on_generate = on_generate
        self.requests: list[WorkflowImageRequest] = []

    def generate_workflow_image(self, request: WorkflowImageRequest) -> WorkflowImageResult:
        self.requests.append(request)
        if self.on_generate is not None:
            self.on_generate()
        return WorkflowImageResult(
            images=(WorkflowGeneratedImage(bytes_data=self.image_bytes, mime_type="image/png"),),
            model="static-v1",
            provider_response_id="delivery-source",
            provider_status="completed",
            effective_parameters={},
        )


def _image_bytes(
    *,
    mode: str = "RGB",
    size: tuple[int, int] = (80, 60),
    color: tuple[int, ...] = (220, 40, 40),
    image_format: str = "PNG",
) -> bytes:
    image = Image.new(mode, size, color)
    output = BytesIO()
    image.save(output, format=image_format)
    return output.getvalue()


def test_delivery_rendition_response_schema_exposes_only_persisted_states() -> None:
    status_schema = DeliveryRenditionJobResponse.model_json_schema()["properties"]["status"]

    assert status_schema["enum"] == ["queued", "running", "succeeded", "failed"]


def _create_generated_source(
    db_session,
    *,
    auto_delivery: bool = False,
    delivery_spec_during_generation: dict[str, object] | None = None,
    provider_capture: list[StaticWorkflowImageProvider] | None = None,
) -> tuple[ProductImageAsset, WorkflowRun, WorkflowNodeRun]:
    product = create_canonical_product(
        db_session,
        name="交付派生测试商品",
        category="测试",
        price="99.00",
        source_note="交付派生测试",
        image_uploads=[(_make_demo_image_bytes(), "reference.png", "image/png")],
    )
    payload = make_workflow_draft_payload(reference_asset_id=product.image_assets[0].id)
    selected_plan_key = "hero-1" if auto_delivery else "hero-2"
    if auto_delivery:
        payload["image_types"][0]["images"][0]["delivery_spec"] = {
            "width": 48,
            "height": 48,
            "format": "png",
            "fit": "contain",
        }
    else:
        payload["image_types"][0]["images"][1]["delivery_spec"] = None
    draft = create_workflow_draft(
        db_session,
        product_id=product.id,
        payload=payload,
        ready_for_confirmation=True,
    )
    confirm_workflow_draft_revision(
        db_session,
        product_id=product.id,
        draft_id=draft.id,
        expected_draft_version=1,
    )
    materialized = materialize_workflow_draft(
        db_session,
        product_id=product.id,
        draft_id=draft.id,
        expected_draft_version=1,
        expected_workflow_revision=0,
        idempotency_key="delivery-source",
    )
    image_node = next(
        node
        for node in materialized.workflow.nodes
        if node.node_type == WorkflowNodeType.IMAGE_GENERATION
        and node.config_json["image_plan_key"] == selected_plan_key
    )
    run = WorkflowRun(workflow_id=materialized.workflow.id, status=WorkflowRunStatus.RUNNING)
    db_session.add(run)
    db_session.flush()
    image_node.status = WorkflowNodeStatus.QUEUED
    node_run = WorkflowNodeRun(
        workflow_run_id=run.id,
        node_id=image_node.id,
        status=WorkflowNodeStatus.QUEUED,
    )
    db_session.add(node_run)
    db_session.commit()
    def update_delivery_spec() -> None:
        if delivery_spec_during_generation is None:
            return
        db_session.refresh(image_node)
        image_node.config_json = {
            **image_node.config_json,
            "delivery_spec": delivery_spec_during_generation,
        }
        db_session.commit()

    provider = StaticWorkflowImageProvider(_image_bytes(), on_generate=update_delivery_spec)
    if provider_capture is not None:
        provider_capture.append(provider)
    execute_v2_workflow_node_run(
        db_session,
        node_run_id=node_run.id,
        dependencies=WorkflowExecutionDependencies(image_provider_resolver=lambda: provider),
    )
    workflow_execution._execute_product_workflow_run(
        db_session,
        run_id=run.id,
        enqueue_node_run=lambda _: pytest.fail("single-node run must be terminal after node execution"),
    )
    db_session.expire_all()
    record = db_session.scalar(
        select(WorkflowImageGenerationRecord).where(
            WorkflowImageGenerationRecord.workflow_node_run_id == node_run.id
        )
    )
    assert record is not None
    source = db_session.get(ProductImageAsset, record.result_asset_id)
    assert source is not None
    return source, run, node_run


def test_renderer_contain_preserves_alpha_and_exact_contract() -> None:
    rendered = render_delivery_rendition(
        _image_bytes(mode="RGBA", size=(8, 4), color=(255, 0, 0, 255)),
        {
            "width": 12,
            "height": 12,
            "format": "png",
            "fit": "contain",
        },
    )

    assert rendered.metadata.mime_type == "image/png"
    assert (rendered.metadata.width, rendered.metadata.height) == (12, 12)
    with Image.open(BytesIO(rendered.bytes_data)) as image:
        assert image.mode == "RGBA"
        assert image.getpixel((0, 0))[3] == 0
        assert image.getpixel((6, 6)) == (255, 0, 0, 255)


def test_renderer_cover_anchor_and_jpeg_background_are_deterministic() -> None:
    source = Image.new("RGB", (10, 20), (255, 0, 0))
    for y in range(10, 20):
        for x in range(10):
            source.putpixel((x, y), (0, 0, 255))
    source_output = BytesIO()
    source.save(source_output, format="PNG")

    top = render_delivery_rendition(
        source_output.getvalue(),
        {
            "width": 10,
            "height": 10,
            "format": "png",
            "fit": "cover",
            "crop_anchor": "top",
        },
    )
    bottom = render_delivery_rendition(
        source_output.getvalue(),
        {
            "width": 10,
            "height": 10,
            "format": "png",
            "fit": "cover",
            "crop_anchor": "bottom",
        },
    )
    with Image.open(BytesIO(top.bytes_data)) as top_image:
        assert top_image.getpixel((5, 5)) == (255, 0, 0)
    with Image.open(BytesIO(bottom.bytes_data)) as bottom_image:
        assert bottom_image.getpixel((5, 5)) == (0, 0, 255)

    jpeg = render_delivery_rendition(
        _image_bytes(mode="RGBA", size=(4, 4), color=(0, 0, 0, 0)),
        {
            "width": 8,
            "height": 8,
            "format": "jpeg",
            "fit": "contain",
        },
    )
    assert jpeg.metadata.mime_type == "image/jpeg"
    with Image.open(BytesIO(jpeg.bytes_data)) as jpeg_image:
        assert jpeg_image.mode == "RGB"
        assert all(channel >= 250 for channel in jpeg_image.getpixel((0, 0)))


def test_renderer_enforces_max_bytes_without_changing_format_or_size() -> None:
    with pytest.raises(BusinessValidationError, match="PNG 交付图无法"):
        render_delivery_rendition(
            _image_bytes(size=(64, 64)),
            {
                "width": 64,
                "height": 64,
                "format": "png",
                "fit": "cover",
                "max_byte_size": 1,
            },
        )

    webp = render_delivery_rendition(
        _image_bytes(size=(128, 96)),
        {
            "width": 80,
            "height": 80,
            "format": "webp",
            "fit": "cover",
            "max_byte_size": 4096,
        },
    )
    assert webp.metadata.mime_type == "image/webp"
    assert (webp.metadata.width, webp.metadata.height) == (80, 80)
    assert webp.metadata.byte_size <= 4096


def test_renderer_reports_unavailable_encoder_as_contract_failure(monkeypatch) -> None:
    source_bytes = _image_bytes()

    def unavailable_encoder(*_args, **_kwargs) -> None:
        raise OSError("encoder unavailable")

    monkeypatch.setattr(Image.Image, "save", unavailable_encoder)
    with pytest.raises(BusinessValidationError, match="不支持 WEBP"):
        render_delivery_rendition(
            source_bytes,
            {"width": 32, "height": 32, "format": "webp", "fit": "cover"},
        )


def test_job_creation_is_idempotent_and_rejects_non_generation_sources(db_session) -> None:
    source, _, _ = _create_generated_source(db_session)
    spec = {"width": 96, "height": 96, "format": "png", "fit": "contain"}

    first = create_delivery_rendition_job(db_session, source_asset_id=source.id, delivery_spec=spec)
    db_session.commit()
    second = create_delivery_rendition_job(db_session, source_asset_id=source.id, delivery_spec=spec)

    assert first.created is True
    assert second.created is False
    assert second.job.id == first.job.id
    assert db_session.scalar(select(func.count()).select_from(DeliveryRenditionJob)) == 1

    upload = source.product.image_assets[0]
    with pytest.raises(BusinessValidationError, match="schema-v2"):
        create_delivery_rendition_job(db_session, source_asset_id=upload.id, delivery_spec=spec)


def test_v2_image_success_atomically_creates_queued_rendition_job(db_session) -> None:
    source, run, node_run = _create_generated_source(db_session, auto_delivery=True)

    job = db_session.scalar(
        select(DeliveryRenditionJob).where(DeliveryRenditionJob.source_asset_id == source.id)
    )
    assert job is not None
    assert job.status == JobStatus.QUEUED
    assert job.spec_json == {
        "width": 48,
        "height": 48,
        "format": "png",
        "max_byte_size": None,
        "fit": "contain",
        "background_color": None,
        "crop_anchor": None,
    }
    assert db_session.get(WorkflowRun, run.id).status == WorkflowRunStatus.SUCCEEDED
    persisted_node_run = db_session.get(WorkflowNodeRun, node_run.id)
    assert persisted_node_run.status == WorkflowNodeStatus.SUCCEEDED
    assert "rendition" not in persisted_node_run.output_json


def test_v2_image_without_delivery_spec_creates_only_original_asset(db_session) -> None:
    source, run, node_run = _create_generated_source(db_session)

    assert db_session.scalar(select(func.count()).select_from(DeliveryRenditionJob)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowImageGenerationRecord)) == 1
    assert db_session.scalar(select(func.count()).select_from(ProductImageAsset)) == 2
    assert db_session.get(WorkflowRun, run.id).status == WorkflowRunStatus.SUCCEEDED
    assert db_session.get(WorkflowNodeRun, node_run.id).output_json["result_asset_id"] == source.id


def test_delivery_spec_change_during_provider_call_does_not_invalidate_generated_image(db_session) -> None:
    next_spec = {
        "width": 52,
        "height": 40,
        "format": "webp",
        "fit": "cover",
        "crop_anchor": "right",
    }

    source, run, node_run = _create_generated_source(
        db_session,
        delivery_spec_during_generation=next_spec,
    )

    job = db_session.scalar(
        select(DeliveryRenditionJob).where(DeliveryRenditionJob.source_asset_id == source.id)
    )
    assert job is not None
    assert job.spec_json == {**next_spec, "max_byte_size": None, "background_color": None}
    assert db_session.scalar(select(func.count()).select_from(WorkflowImageGenerationRecord)) == 1
    assert db_session.get(WorkflowRun, run.id).status == WorkflowRunStatus.SUCCEEDED
    assert db_session.get(WorkflowNodeRun, node_run.id).status == WorkflowNodeStatus.SUCCEEDED


def test_v2_rendition_dispatch_stage_does_not_reverse_image_success(
    db_session,
) -> None:
    source, run, node_run = _create_generated_source(db_session, auto_delivery=True)

    job = db_session.scalar(
        select(DeliveryRenditionJob).where(DeliveryRenditionJob.source_asset_id == source.id)
    )
    assert job is not None
    assert job.status == JobStatus.QUEUED
    assert job.is_retryable is True
    db_session.expire_all()
    dispatch = db_session.scalar(
        select(AsyncDispatch).where(AsyncDispatch.aggregate_id == job.id)
    )
    assert dispatch is not None
    assert dispatch.status == AsyncDispatchStatus.PENDING
    assert db_session.get(WorkflowRun, run.id).status == WorkflowRunStatus.SUCCEEDED
    assert db_session.get(WorkflowNodeRun, node_run.id).status == WorkflowNodeStatus.SUCCEEDED


def test_job_execution_persists_child_asset_without_mutating_workflow_success(
    configured_env,
    db_session,
) -> None:
    providers: list[StaticWorkflowImageProvider] = []
    source, run, node_run = _create_generated_source(db_session, provider_capture=providers)
    assert len(providers) == 1
    assert len(providers[0].requests) == 1
    clear_product_cover(db_session, product_id=source.product_id)
    job = submit_delivery_rendition_job(
        db_session,
        source_asset_id=source.id,
        delivery_spec={
            "width": 48,
            "height": 48,
            "format": "jpeg",
            "fit": "contain",
            "background_color": "#FFFFFF",
        },
        enqueue=lambda _: None,
    )

    execute_delivery_rendition_job(job.id)

    db_session.expire_all()
    assert len(providers[0].requests) == 1
    completed = get_delivery_rendition_job(db_session, job.id)
    assert completed.status == JobStatus.SUCCEEDED
    assert completed.attempts == 1
    assert completed.result_asset is not None
    assert completed.result_asset.parent_asset_id == source.id
    assert completed.result_asset.image_type_key == source.image_type_key
    assert completed.result_asset.media_object.mime_type == "image/jpeg"
    assert (completed.result_asset.media_object.width, completed.result_asset.media_object.height) == (48, 48)
    assert db_session.get(WorkflowRun, run.id).status == WorkflowRunStatus.SUCCEEDED
    assert db_session.get(WorkflowNodeRun, node_run.id).status == WorkflowNodeStatus.SUCCEEDED
    assert db_session.get(Product, source.product_id).cover_image_asset_id is None

    gallery_record = get_gallery_asset_detail(
        db_session,
        product_id=source.product_id,
        asset_id=completed.result_asset.id,
    )
    assert gallery_record.rendition is not None
    assert gallery_record.rendition.job_id == job.id
    assert gallery_record.rendition.source_asset_id == source.id
    assert gallery_record.rendition.delivery_spec["format"] == "jpeg"
    assert gallery_record.generation is not None
    assert gallery_record.generation.node_run_id == node_run.id

    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)
    detail = client.get(
        f"/api/v2/products/{source.product_id}/image-assets/{completed.result_asset.id}"
    )
    assert detail.status_code == 200, detail.text
    assert detail.json()["rendition"] == {
        "job_id": job.id,
        "source_asset_id": source.id,
        "delivery_spec": completed.spec_json,
        "status": "succeeded",
    }
    assert detail.json()["generation"]["node_run_id"] == node_run.id

    with pytest.raises(ConflictError, match="交付派生任务"):
        delete_product_image_asset(db_session, asset_id=completed.result_asset.id)


def test_rendition_contract_failure_does_not_mutate_generation_success(
    configured_env,
    db_session,
) -> None:
    providers: list[StaticWorkflowImageProvider] = []
    source, run, node_run = _create_generated_source(db_session, provider_capture=providers)
    job = submit_delivery_rendition_job(
        db_session,
        source_asset_id=source.id,
        delivery_spec={
            "width": 64,
            "height": 64,
            "format": "png",
            "fit": "cover",
            "max_byte_size": 1,
        },
        enqueue=lambda _: None,
    )

    execute_delivery_rendition_job(job.id)

    db_session.expire_all()
    failed = get_delivery_rendition_job(db_session, job.id)
    assert failed.status == JobStatus.FAILED
    assert failed.is_retryable is False
    assert failed.result_asset_id is None
    assert len(providers[0].requests) == 1
    assert db_session.scalar(select(func.count()).select_from(WorkflowImageGenerationRecord)) == 1
    assert db_session.get(WorkflowRun, run.id).status == WorkflowRunStatus.SUCCEEDED
    assert db_session.get(WorkflowNodeRun, node_run.id).status == WorkflowNodeStatus.SUCCEEDED


def test_rendition_storage_failure_is_retryable_and_keeps_generation_success(
    configured_env,
    db_session,
) -> None:
    class FailingMediaStorage(LocalStorage):
        def save_media_image(self, media_id: str, filename: str, content: bytes) -> str:
            raise OSError("injected rendition storage failure")

    source, run, node_run = _create_generated_source(db_session)
    asset_count = db_session.scalar(select(func.count()).select_from(ProductImageAsset))
    job = submit_delivery_rendition_job(
        db_session,
        source_asset_id=source.id,
        delivery_spec={"width": 32, "height": 32, "format": "webp", "fit": "cover"},
        enqueue=lambda _: None,
    )

    execute_delivery_rendition_job(job.id, storage=FailingMediaStorage())

    db_session.expire_all()
    failed = get_delivery_rendition_job(db_session, job.id)
    assert failed.status == JobStatus.FAILED
    assert failed.is_retryable is True
    assert failed.result_asset_id is None
    assert db_session.scalar(select(func.count()).select_from(ProductImageAsset)) == asset_count
    assert db_session.scalar(select(func.count()).select_from(WorkflowImageGenerationRecord)) == 1
    assert db_session.get(WorkflowRun, run.id).status == WorkflowRunStatus.SUCCEEDED
    assert db_session.get(WorkflowNodeRun, node_run.id).status == WorkflowNodeStatus.SUCCEEDED


def test_rendition_filename_preserves_extension_at_database_limit(db_session) -> None:
    source, _, _ = _create_generated_source(db_session)
    source.original_filename = f"{'a' * 255}.png"
    db_session.commit()
    job = submit_delivery_rendition_job(
        db_session,
        source_asset_id=source.id,
        delivery_spec={"width": 20, "height": 10, "format": "jpeg", "fit": "cover"},
        enqueue=lambda _: None,
    )

    execute_delivery_rendition_job(job.id)

    db_session.expire_all()
    completed = get_delivery_rendition_job(db_session, job.id)
    assert completed.result_asset is not None
    assert len(completed.result_asset.original_filename) == 255
    assert completed.result_asset.original_filename.endswith("-20x10.jpg")


def test_product_aggregate_delete_removes_rendition_jobs_and_assets(db_session) -> None:
    source, _, _ = _create_generated_source(db_session)
    job = submit_delivery_rendition_job(
        db_session,
        source_asset_id=source.id,
        delivery_spec={"width": 24, "height": 24, "format": "png", "fit": "cover"},
        enqueue=lambda _: None,
    )
    execute_delivery_rendition_job(job.id)
    product_id = source.product_id

    delete_product(db_session, product_id=product_id)

    assert db_session.get(DeliveryRenditionJob, job.id) is None
    assert db_session.scalar(
        select(func.count()).select_from(ProductImageAsset).where(ProductImageAsset.product_id == product_id)
    ) == 0


def test_queue_failure_is_retryable_and_does_not_change_source_run(db_session) -> None:
    source, run, node_run = _create_generated_source(db_session)

    with pytest.raises(QueueUnavailableError):
        submit_delivery_rendition_job(
            db_session,
            source_asset_id=source.id,
            delivery_spec={"width": 32, "height": 32, "format": "png", "fit": "cover"},
            enqueue=lambda _: (_ for _ in ()).throw(ConnectionError("redis down")),
        )

    failed = db_session.scalar(select(DeliveryRenditionJob))
    assert failed is not None
    assert failed.status == JobStatus.FAILED
    assert failed.is_retryable is True
    assert db_session.get(WorkflowRun, run.id).status == WorkflowRunStatus.SUCCEEDED
    assert db_session.get(WorkflowNodeRun, node_run.id).status == WorkflowNodeStatus.SUCCEEDED

    retried = retry_delivery_rendition_job(db_session, job_id=failed.id, enqueue=lambda _: None)
    assert retried.status == JobStatus.QUEUED
    assert db_session.scalar(select(func.count()).select_from(WorkflowImageGenerationRecord)) == 1


def test_duplicate_submit_queue_failure_does_not_downgrade_existing_queued_job(db_session) -> None:
    source, _, _ = _create_generated_source(db_session)
    spec = {"width": 47, "height": 31, "format": "png", "fit": "cover"}
    queued = submit_delivery_rendition_job(
        db_session,
        source_asset_id=source.id,
        delivery_spec=spec,
        enqueue=lambda _: None,
    )

    with pytest.raises(QueueUnavailableError):
        submit_delivery_rendition_job(
            db_session,
            source_asset_id=source.id,
            delivery_spec=spec,
            enqueue=lambda _: (_ for _ in ()).throw(ConnectionError("redis down")),
        )

    db_session.expire_all()
    persisted = get_delivery_rendition_job(db_session, queued.id)
    assert persisted.status == JobStatus.QUEUED
    assert persisted.failure_reason is None
    assert persisted.is_retryable is True


def test_delivery_rendition_http_contract_and_retry(
    configured_env,
    db_session,
) -> None:
    from productflow_backend.presentation.api import create_app

    source, _, _ = _create_generated_source(db_session)
    uploaded_source = source.product.image_assets[0]
    client = TestClient(create_app())
    _login(client)
    spec = {"width": 64, "height": 48, "format": "webp", "fit": "cover"}

    created = client.post(
        f"/api/v2/product-image-assets/{source.id}/renditions",
        json=spec,
    )
    assert created.status_code == 202, created.text
    payload = created.json()
    assert payload["source_asset_id"] == source.id
    assert payload["status"] == "queued"
    assert payload["delivery_spec"] == {
        **spec,
        "max_byte_size": None,
        "background_color": None,
        "crop_anchor": None,
    }
    assert "spec_hash" not in payload
    assert "active_attempt_id" not in payload

    db_session.expire_all()
    dispatch = db_session.scalar(
        select(AsyncDispatch).where(AsyncDispatch.aggregate_id == payload["id"])
    )
    assert dispatch is not None
    assert dispatch.status == AsyncDispatchStatus.PENDING
    assert dispatch.actor_name == "run_delivery_rendition_job"
    assert dispatch.delivery_key == f"run_delivery_rendition_job:{payload['id']}"

    duplicate = client.post(
        f"/api/v2/product-image-assets/{source.id}/renditions",
        json=spec,
    )
    assert duplicate.status_code == 202
    assert duplicate.json()["id"] == payload["id"]
    db_session.expire_all()
    assert (
        db_session.scalar(
            select(func.count())
            .select_from(AsyncDispatch)
            .where(AsyncDispatch.aggregate_id == payload["id"])
        )
        == 1
    )

    listed = client.get(f"/api/v2/product-image-assets/{source.id}/renditions")
    detail = client.get(f"/api/v2/delivery-rendition-jobs/{payload['id']}")
    assert listed.status_code == 200
    assert [item["id"] for item in listed.json()["items"]] == [payload["id"]]
    assert detail.status_code == 200
    assert detail.json()["id"] == payload["id"]

    invalid = client.post(
        f"/api/v2/product-image-assets/{source.id}/renditions",
        json={**spec, "unknown": True},
    )
    assert invalid.status_code == 422
    upload = client.post(
        f"/api/v2/product-image-assets/{uploaded_source.id}/renditions",
        json=spec,
    )
    assert upload.status_code == 400

    mark_delivery_rendition_job_enqueue_failed(
        db_session,
        job_id=payload["id"],
        reason="任务队列暂不可用，请稍后重试",
    )
    retried = client.post(f"/api/v2/delivery-rendition-jobs/{payload['id']}/retry")
    assert retried.status_code == 202, retried.text
    assert retried.json()["status"] == "queued"
    assert retried.json()["failure_reason"] is None
    db_session.expire_all()
    assert (
        db_session.scalar(
            select(func.count())
            .select_from(AsyncDispatch)
            .where(AsyncDispatch.aggregate_id == payload["id"])
        )
        == 1
    )


def test_delivery_rendition_http_queue_failure_keeps_retryable_job(
    configured_env,
    db_session,
) -> None:
    from productflow_backend.presentation.api import create_app

    source, _, _ = _create_generated_source(db_session)
    client = TestClient(create_app())
    _login(client)

    response = client.post(
        f"/api/v2/product-image-assets/{source.id}/renditions",
        json={"width": 73, "height": 41, "format": "jpeg", "fit": "contain"},
    )

    assert response.status_code == 202, response.text
    payload = response.json()
    assert payload["status"] == "queued"
    db_session.expire_all()
    dispatch = db_session.scalar(
        select(AsyncDispatch).where(AsyncDispatch.aggregate_id == payload["id"])
    )
    assert dispatch is not None
    assert dispatch.status == AsyncDispatchStatus.PENDING

    def fail_enqueue(dispatch_id: str, aggregate_id: str) -> None:
        raise ConnectionError("redis down")

    run_async_dispatcher_once(
        enqueue=fail_enqueue,
        max_attempts=1,
    )

    db_session.expire_all()
    dispatch = db_session.get(AsyncDispatch, dispatch.id)
    assert dispatch is not None
    assert dispatch.status == AsyncDispatchStatus.SENT
    assert dispatch.last_error == "redis down"
    listed = client.get(f"/api/v2/product-image-assets/{source.id}/renditions")
    assert listed.status_code == 200
    job = listed.json()["items"][0]
    assert job["status"] == "queued"
    assert job["is_retryable"] is True
    assert job["result_asset"] is None


def test_duplicate_claim_and_old_attempt_cannot_overwrite_requeued_job(db_session) -> None:
    source, _, _ = _create_generated_source(db_session)
    creation = create_delivery_rendition_job(
        db_session,
        source_asset_id=source.id,
        delivery_spec={"width": 40, "height": 40, "format": "png", "fit": "cover"},
    )
    db_session.commit()
    old_claim = claim_delivery_rendition_job(db_session, job_id=creation.job.id, attempt_id="old-attempt")
    assert old_claim is not None
    assert claim_delivery_rendition_job(db_session, job_id=creation.job.id) is None
    assert _fail_delivery_rendition_job(
        db_session,
        job_id=creation.job.id,
        attempt_id="old-attempt",
        reason="temporary",
        retryable=True,
    )
    retry_delivery_rendition_job(db_session, job_id=creation.job.id, enqueue=lambda _: None)
    new_claim = claim_delivery_rendition_job(db_session, job_id=creation.job.id, attempt_id="new-attempt")
    assert new_claim is not None

    rendered = render_delivery_rendition(
        LocalStorage().resolve(source.media_object.storage_path).read_bytes(),
        old_claim.delivery_spec,
    )
    assert (
        _persist_delivery_rendition_result(
            db_session,
            claim=old_claim,
            rendered_bytes=rendered.bytes_data,
            expected_mime_type=rendered.metadata.mime_type,
            storage=LocalStorage(),
        )
        is False
    )
    current = get_delivery_rendition_job(db_session, creation.job.id)
    assert current.status == JobStatus.RUNNING
    assert current.active_attempt_id == "new-attempt"
    assert current.result_asset_id is None


def test_recovery_requeues_stale_attempt_and_rejects_old_completion(db_session) -> None:
    source, _, _ = _create_generated_source(db_session)
    creation = create_delivery_rendition_job(
        db_session,
        source_asset_id=source.id,
        delivery_spec={"width": 36, "height": 36, "format": "png", "fit": "cover"},
    )
    db_session.commit()
    old_claim = claim_delivery_rendition_job(
        db_session,
        job_id=creation.job.id,
        attempt_id="stale-attempt",
    )
    assert old_claim is not None
    creation.job.started_at = datetime.now(UTC) - timedelta(hours=2)
    db_session.commit()

    enqueued: list[str] = []
    summary = recover_unfinished_delivery_rendition_jobs(
        enqueue=enqueued.append,
        reset_stale_running=True,
        stale_running_after=timedelta(minutes=30),
    )
    db_session.expire_all()

    recovered = get_delivery_rendition_job(db_session, creation.job.id)
    assert summary.queued_jobs == 0
    assert summary.stale_running_jobs == 1
    assert summary.enqueued_jobs == 1
    assert enqueued == [creation.job.id]
    assert recovered.status == JobStatus.QUEUED
    assert recovered.active_attempt_id is None
    assert recovered.started_at is None
    assert (
        _fail_delivery_rendition_job(
            db_session,
            job_id=creation.job.id,
            attempt_id="stale-attempt",
            reason="late failure",
            retryable=True,
        )
        is False
    )
