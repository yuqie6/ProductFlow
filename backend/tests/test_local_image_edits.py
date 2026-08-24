from __future__ import annotations

from dataclasses import dataclass
from datetime import timedelta
from hashlib import sha256
from io import BytesIO

import pytest
from helpers import _make_demo_image_bytes_with_size
from PIL import Image
from sqlalchemy import select

from productflow_backend.application.async_delivery import delivery_key_for_actor
from productflow_backend.application.local_image_edits.contracts import (
    LocalEditMaskGeometry,
    LocalImageEditDraft,
)
from productflow_backend.application.local_image_edits.service import (
    LOCAL_EDIT_ACTOR_NAME,
    _persist_provider_result,
    adopt_local_image_edit_result,
    cancel_local_image_edit_task,
    claim_local_image_edit_task,
    create_local_image_edit_task,
    execute_local_image_edit_task,
    recover_local_image_edit_task,
    revert_local_image_edit_adoption,
    submit_local_image_edit_task,
    update_local_image_edit_task,
)
from productflow_backend.application.media_objects import media_object_has_references
from productflow_backend.application.product_images.assets import delete_product_image_asset
from productflow_backend.application.product_workflow.graph_direct_create import create_product_with_direct_graph
from productflow_backend.application.product_workflow.graph_template import DirectCreateImageType
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import (
    AsyncDispatchStatus,
    GraphArtifactType,
    GraphNodeType,
    LocalImageEditTaskStatus,
    MediaVerificationStatus,
    ProductImageOriginType,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError
from productflow_backend.domain.local_image_edits import LocalImageEditOperation
from productflow_backend.infrastructure.db.models import (
    AsyncDispatch,
    LocalImageEditProviderAttempt,
    LocalImageEditTask,
    MediaObject,
    ProductImageAsset,
    WorkflowGraphArtifact,
    WorkflowGraphNode,
)
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.image.base import (
    ImageProvider,
    LocalEditCapability,
    LocalEditRequest,
    LocalEditResult,
    WorkflowGeneratedImage,
)
from productflow_backend.infrastructure.storage import LocalStorage


@dataclass(frozen=True, slots=True)
class EditContext:
    product_id: str
    source_asset_id: str
    reference_asset_id: str | None
    graph_id: str
    target_node_id: str
    source_artifact_id: str | None
    source_bytes: bytes


def _mask_bytes(width: int, height: int, *, selected: bool = True) -> bytes:
    image = Image.new("RGBA", (width, height), (255, 255, 255, 255))
    alpha = image.getchannel("A")
    if selected:
        alpha.putpixel((0, 0), 0)
    alpha.putpixel((min(1, width - 1), 0), 128)
    alpha.putpixel((width - 1, height - 1), 255)
    image.putalpha(alpha)
    output = BytesIO()
    image.save(output, format="PNG", optimize=True)
    return output.getvalue()


def _context(db_session, *, with_reference: bool = False, with_target: bool = False) -> EditContext:
    source_bytes = _make_demo_image_bytes_with_size(4, 3)
    uploads = [(source_bytes, "source.png", "image/png")]
    if with_reference:
        uploads.append((_make_demo_image_bytes_with_size(4, 3), "reference.png", "image/png"))
    created = create_product_with_direct_graph(
        db_session,
        name="局部编辑测试商品",
        category="测试",
        price=None,
        source_note=None,
        image_uploads=uploads,
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    image_node = next(node for node in created.projection.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION)
    source_asset = created.created_assets[0]
    reference_asset_id = created.created_assets[1].id if with_reference else None
    source_artifact_id: str | None = None
    if with_target:
        digest = sha256(b"source-input").hexdigest()
        source_artifact = WorkflowGraphArtifact(
            graph_id=created.graph.id,
            node_id=image_node.id,
            node_run_id=None,
            artifact_type=GraphArtifactType.IMAGE,
            schema_version=3,
            graph_revision=created.graph.revision,
            payload_json={"schema_version": 3, "product_image_asset_id": source_asset.id},
            payload_hash=digest,
            input_digest=digest,
            product_image_asset_id=source_asset.id,
            provider_name="fixture",
            provider_model="fixture",
        )
        db_session.add(source_artifact)
        db_session.flush()
        image_node_row = db_session.get(WorkflowGraphNode, image_node.id)
        assert image_node_row is not None
        image_node_row.current_artifact_id = source_artifact.id
        db_session.commit()
        source_artifact_id = source_artifact.id
    return EditContext(
        product_id=created.product.id,
        source_asset_id=source_asset.id,
        reference_asset_id=reference_asset_id,
        graph_id=created.graph.id,
        target_node_id=image_node.id,
        source_artifact_id=source_artifact_id,
        source_bytes=source_bytes,
    )


def _draft(context: EditContext, *, reference: bool = False) -> LocalImageEditDraft:
    return LocalImageEditDraft(
        operation=LocalImageEditOperation.INPAINT,
        instruction="擦除选区并保持商品边缘自然",
        mask_geometry=LocalEditMaskGeometry(
            source_width=4,
            source_height=3,
            viewport_width=4,
            viewport_height=3,
            viewport_to_source=(1, 0, 0, 1, 0, 0),
        ),
        reference_asset_ids=(context.reference_asset_id,) if reference and context.reference_asset_id else (),
    )


def _new_task(db_session, context: EditContext, *, reference: bool = False, target: bool = False) -> LocalImageEditTask:
    return create_local_image_edit_task(
        db_session,
        product_id=context.product_id,
        source_asset_id=context.source_asset_id,
        draft=_draft(context, reference=reference),
        mask_png_bytes=_mask_bytes(4, 3),
        target_node_id=context.target_node_id if target else None,
        storage=LocalStorage(),
    )


class SupportingImageProvider(ImageProvider):
    provider_name = "fixture-image-provider"

    def __init__(self, result_bytes: bytes | None = None) -> None:
        self.requests: list[LocalEditRequest] = []
        self.result_bytes = result_bytes or _make_demo_image_bytes_with_size(4, 3)

    def generate_workflow_image(self, request):
        del request
        raise AssertionError("局部编辑测试不应调用普通 workflow generation")

    @property
    def local_edit_capability(self) -> LocalEditCapability:
        return LocalEditCapability(
            provider_name=self.provider_name,
            supported=True,
            mode="masked_edit",
            operations=tuple(LocalImageEditOperation),
            max_reference_images=6,
        )

    def edit_local(self, request: LocalEditRequest) -> LocalEditResult:
        self.requests.append(request)
        return LocalEditResult(
            images=(WorkflowGeneratedImage(bytes_data=self.result_bytes, mime_type="image/png"),),
            model="fixture-model",
            provider_response_id="fixture-response",
            provider_status="succeeded",
            effective_mode="masked_edit",
            effective_parameters={"size": request.size},
            provider_output_json={"request_id": "fixture-response", "b64_json": "must-not-persist"},
        )


class RaisingImageProvider(SupportingImageProvider):
    def edit_local(self, request: LocalEditRequest) -> LocalEditResult:
        del request
        raise RuntimeError("provider connection lost")


class SecretRaisingImageProvider(SupportingImageProvider):
    def edit_local(self, request: LocalEditRequest) -> LocalEditResult:
        del request
        raise RuntimeError("provider secret sk-test-should-never-be-persisted")


class JpegImageProvider(SupportingImageProvider):
    def edit_local(self, request: LocalEditRequest) -> LocalEditResult:
        result = super().edit_local(request)
        image = Image.open(BytesIO(result.images[0].bytes_data)).convert("RGB")
        output = BytesIO()
        image.save(output, format="JPEG")
        return result.model_copy(
            update={
                "images": (WorkflowGeneratedImage(bytes_data=output.getvalue(), mime_type="image/jpeg"),),
            }
        )


class DriftedImageProvider(SupportingImageProvider):
    provider_name = "drifted-image-provider"


class UnsupportedImageProvider(ImageProvider):
    provider_name = "unsupported-fixture-provider"

    def generate_workflow_image(self, request):
        del request
        raise AssertionError("unsupported fixture should stop before provider call")


def _submit(db_session, task: LocalImageEditTask, key: str = "edit-key"):
    return submit_local_image_edit_task(
        db_session,
        task_id=task.id,
        idempotency_key=key,
        requested_provider_name=SupportingImageProvider.provider_name,
        requested_local_edit_mode="masked_edit",
    )


def _execute(task_id: str, provider: ImageProvider, configured_env) -> None:
    del configured_env
    execute_local_image_edit_task(
        session_factory=get_session_factory(),
        task_id=task_id,
        provider=provider,
        storage=LocalStorage(),
    )


def test_draft_update_replaces_only_unreferenced_mask_and_fences_revision(db_session, configured_env) -> None:
    context = _context(db_session)
    task = _new_task(db_session, context)
    old_mask_id = task.mask_media_object_id
    updated = update_local_image_edit_task(
        db_session,
        task_id=task.id,
        expected_revision=1,
        draft=_draft(context),
        mask_png_bytes=_mask_bytes(4, 3),
        storage=LocalStorage(),
    )
    assert updated.revision == 2
    assert updated.mask_media_object_id != old_mask_id
    assert db_session.get(MediaObject, old_mask_id) is None
    with pytest.raises(ConflictError):
        update_local_image_edit_task(
            db_session,
            task_id=task.id,
            expected_revision=1,
            draft=_draft(context),
            storage=LocalStorage(),
        )


def test_reference_media_requires_verified_complete_metadata(db_session, configured_env) -> None:
    context = _context(db_session, with_reference=True)
    reference = db_session.get(ProductImageAsset, context.reference_asset_id)
    assert reference is not None and reference.media_object is not None
    reference.media_object.verification_status = MediaVerificationStatus.LEGACY_PENDING
    db_session.commit()
    with pytest.raises(BusinessValidationError, match="参考图"):
        _new_task(db_session, context, reference=True)


def test_submit_is_idempotent_and_stages_durable_dispatch(db_session, configured_env) -> None:
    context = _context(db_session)
    task = _new_task(db_session, context)
    first = _submit(db_session, task)
    assert first.created is True
    assert first.task.status == LocalImageEditTaskStatus.QUEUED
    dispatch = db_session.scalar(
        select(AsyncDispatch).where(
            AsyncDispatch.delivery_key == delivery_key_for_actor(LOCAL_EDIT_ACTOR_NAME, task.id)
        )
    )
    assert dispatch is not None and dispatch.actor_name == LOCAL_EDIT_ACTOR_NAME
    replay = _submit(db_session, task)
    assert replay.created is False
    assert replay.dispatch.id == dispatch.id


def test_execute_duplicate_delivery_is_noop_for_fresh_running_and_terminal_task(db_session, configured_env) -> None:
    context = _context(db_session)
    task = _new_task(db_session, context)
    _submit(db_session, task)
    claim_local_image_edit_task(db_session, task_id=task.id)
    db_session.commit()
    fresh_provider = SupportingImageProvider()
    fresh_result = _execute(task.id, fresh_provider, configured_env)
    del fresh_result
    assert fresh_provider.requests == []

    db_session.expire_all()
    current = db_session.get(LocalImageEditTask, task.id)
    assert current is not None and current.status == LocalImageEditTaskStatus.RUNNING
    _persist_provider_result(
        db_session,
        storage=LocalStorage(),
        task_id=task.id,
        attempt_id=current.active_attempt_id or "missing",
        snapshot={},
        result=LocalEditResult(
            images=(WorkflowGeneratedImage(bytes_data=_make_demo_image_bytes_with_size(4, 3), mime_type="image/png"),),
            model="fixture-model",
            provider_response_id="terminal-response",
            provider_status="succeeded",
            effective_mode="masked_edit",
            effective_parameters={},
        ),
    )
    terminal_provider = SupportingImageProvider()
    _execute(task.id, terminal_provider, configured_env)
    assert terminal_provider.requests == []


def test_claim_rejects_fresh_duplicate_and_only_requeues_stale_claimed(db_session, configured_env) -> None:
    context = _context(db_session)
    task = _new_task(db_session, context)
    _submit(db_session, task)
    first_claim = claim_local_image_edit_task(db_session, task_id=task.id)
    db_session.commit()
    with pytest.raises(ConflictError):
        claim_local_image_edit_task(db_session, task_id=task.id)
    db_session.rollback()
    current = db_session.get(LocalImageEditTask, task.id)
    assert current is not None and current.status == LocalImageEditTaskStatus.RUNNING

    current.progress_phase = "claimed"
    current.started_at = now_utc() - timedelta(hours=1)
    db_session.commit()
    replacement = claim_local_image_edit_task(db_session, task_id=task.id)
    db_session.commit()
    attempts = list(
        db_session.scalars(
            select(LocalImageEditProviderAttempt)
            .where(LocalImageEditProviderAttempt.task_id == task.id)
            .order_by(LocalImageEditProviderAttempt.attempt_number)
        )
    )
    assert replacement.attempt_number == 2
    assert attempts[0].phase == "failed"
    assert attempts[1].phase == "claimed"
    assert first_claim.attempt_id == attempts[0].attempt_id


def test_stale_provider_boundary_becomes_unknown_but_fresh_provider_boundary_is_untouched(
    db_session,
    configured_env,
) -> None:
    context = _context(db_session)
    task = _new_task(db_session, context)
    _submit(db_session, task)
    claim = claim_local_image_edit_task(db_session, task_id=task.id)
    db_session.commit()
    current = db_session.get(LocalImageEditTask, task.id)
    assert current is not None
    current.progress_phase = "provider_call"
    current.started_at = now_utc()
    db_session.commit()
    with pytest.raises(ConflictError):
        claim_local_image_edit_task(db_session, task_id=task.id)
    db_session.rollback()
    current = db_session.get(LocalImageEditTask, task.id)
    assert current is not None and current.status == LocalImageEditTaskStatus.RUNNING
    current.started_at = now_utc() - timedelta(hours=1)
    db_session.commit()
    with pytest.raises(ConflictError):
        claim_local_image_edit_task(db_session, task_id=task.id)
    db_session.rollback()
    current = db_session.get(LocalImageEditTask, task.id)
    assert current is not None and current.status == LocalImageEditTaskStatus.UNKNOWN
    attempt = db_session.scalar(
        select(LocalImageEditProviderAttempt).where(LocalImageEditProviderAttempt.attempt_id == claim.attempt_id)
    )
    assert attempt is not None and attempt.effect_result == "unknown"


def test_recovery_requeues_delivered_claim_and_fails_closed_unknown_phase(db_session, configured_env) -> None:
    context = _context(db_session)
    task = _new_task(db_session, context)
    _submit(db_session, task)
    claim_local_image_edit_task(db_session, task_id=task.id)
    db_session.commit()
    current = db_session.get(LocalImageEditTask, task.id)
    assert current is not None
    current.progress_phase = "claimed"
    current.started_at = now_utc() - timedelta(hours=1)
    dispatch = db_session.scalar(select(AsyncDispatch).where(AsyncDispatch.aggregate_id == task.id))
    assert dispatch is not None
    dispatch.status = AsyncDispatchStatus.CONSUMED
    db_session.commit()

    recovered = recover_local_image_edit_task(
        db_session,
        task_id=task.id,
        reset_stale_running=True,
        stale_after=timedelta(minutes=10),
    )
    assert recovered.outcome == "requeued"
    db_session.expire_all()
    current = db_session.get(LocalImageEditTask, task.id)
    dispatch = db_session.scalar(select(AsyncDispatch).where(AsyncDispatch.aggregate_id == task.id))
    assert current is not None and current.status == LocalImageEditTaskStatus.QUEUED
    assert dispatch is not None and dispatch.status == AsyncDispatchStatus.PENDING

    current.progress_phase = "corrupt-phase"
    current.started_at = now_utc() - timedelta(hours=1)
    current.active_attempt_id = "missing-attempt"
    current.status = LocalImageEditTaskStatus.RUNNING
    db_session.commit()
    recovered = recover_local_image_edit_task(
        db_session,
        task_id=task.id,
        reset_stale_running=True,
        stale_after=timedelta(minutes=10),
    )
    assert recovered.outcome == "unknown"
    db_session.expire_all()
    current = db_session.get(LocalImageEditTask, task.id)
    assert current is not None and current.status == LocalImageEditTaskStatus.UNKNOWN


def test_success_stages_lineage_without_auto_adoption_and_preserves_provider_name(db_session, configured_env) -> None:
    context = _context(db_session, with_reference=True)
    task = _new_task(db_session, context, reference=True)
    _submit(db_session, task)
    provider = SupportingImageProvider()
    _execute(task.id, provider, configured_env)
    db_session.expire_all()
    persisted = db_session.get(LocalImageEditTask, task.id)
    assert persisted is not None and persisted.status == LocalImageEditTaskStatus.SUCCEEDED
    assert persisted.result_asset_id is not None
    result_asset = db_session.get(ProductImageAsset, persisted.result_asset_id)
    assert result_asset is not None
    assert result_asset.origin_type == ProductImageOriginType.LOCAL_EDIT
    assert result_asset.parent_asset_id == context.source_asset_id
    assert persisted.provider_name == provider.provider_name
    attempt = db_session.scalar(
        select(LocalImageEditProviderAttempt).where(LocalImageEditProviderAttempt.task_id == task.id)
    )
    assert attempt is not None and attempt.provider_name == provider.provider_name
    assert attempt.provider_model == "fixture-model"
    assert attempt.request_json is not None
    assert attempt.request_json["provider_intent"] == {
        "provider_name": "fixture-image-provider",
        "local_edit_mode": "masked_edit",
    }
    assert attempt.result_json == {"request_id": "fixture-response"}
    assert provider.requests and provider.requests[0].reference_images[0].bytes_data


@pytest.mark.parametrize(
    ("provider", "expected_status"),
    [
        (UnsupportedImageProvider(), LocalImageEditTaskStatus.FAILED),
        (RaisingImageProvider(), LocalImageEditTaskStatus.UNKNOWN),
    ],
)
def test_provider_capability_and_exception_are_visible_terminal_states(
    db_session,
    configured_env,
    provider: ImageProvider,
    expected_status: LocalImageEditTaskStatus,
) -> None:
    context = _context(db_session)
    task = _new_task(db_session, context)
    _submit(db_session, task)
    _execute(task.id, provider, configured_env)
    db_session.expire_all()
    persisted = db_session.get(LocalImageEditTask, task.id)
    assert persisted is not None and persisted.status == expected_status
    assert persisted.result_asset_id is None


def test_provider_capability_intent_drift_fails_before_provider_call(db_session, configured_env) -> None:
    context = _context(db_session)
    task = _new_task(db_session, context)
    _submit(db_session, task)
    provider = DriftedImageProvider()
    _execute(task.id, provider, configured_env)
    db_session.expire_all()
    persisted = db_session.get(LocalImageEditTask, task.id)
    assert persisted is not None and persisted.status == LocalImageEditTaskStatus.FAILED
    assert persisted.provider_status == "capability_mismatch"
    assert persisted.failure_reason == "提交时记录的图片 provider 能力与当前绑定不一致"
    assert provider.requests == []


def test_provider_exception_audit_never_persists_exception_text(db_session, configured_env) -> None:
    context = _context(db_session)
    task = _new_task(db_session, context)
    _submit(db_session, task)
    _execute(task.id, SecretRaisingImageProvider(), configured_env)
    db_session.expire_all()
    persisted = db_session.get(LocalImageEditTask, task.id)
    assert persisted is not None and persisted.status == LocalImageEditTaskStatus.UNKNOWN
    assert "sk-test-should-never-be-persisted" not in (persisted.failure_reason or "")
    attempt = db_session.scalar(
        select(LocalImageEditProviderAttempt).where(LocalImageEditProviderAttempt.task_id == task.id)
    )
    assert attempt is not None
    assert attempt.provider_status == "RuntimeError"
    assert "sk-test-should-never-be-persisted" not in (attempt.detail or "")


def test_provider_result_uses_actual_mime_extension(db_session, configured_env) -> None:
    context = _context(db_session)
    task = _new_task(db_session, context)
    _submit(db_session, task)
    _execute(task.id, JpegImageProvider(), configured_env)
    db_session.expire_all()
    persisted = db_session.get(LocalImageEditTask, task.id)
    assert persisted is not None and persisted.result_asset_id is not None
    result_asset = db_session.get(ProductImageAsset, persisted.result_asset_id)
    assert result_asset is not None
    assert result_asset.original_filename.endswith(".jpg")
    assert result_asset.media_object.storage_path.endswith(".jpg")


def test_reference_verification_drift_fails_before_provider_call(db_session, configured_env) -> None:
    context = _context(db_session, with_reference=True)
    task = _new_task(db_session, context, reference=True)
    _submit(db_session, task)
    reference = db_session.get(ProductImageAsset, context.reference_asset_id)
    assert reference is not None and reference.media_object is not None
    reference.media_object.verification_status = MediaVerificationStatus.LEGACY_PENDING
    db_session.commit()
    provider = SupportingImageProvider()
    _execute(task.id, provider, configured_env)
    db_session.expire_all()
    persisted = db_session.get(LocalImageEditTask, task.id)
    assert persisted is not None and persisted.status == LocalImageEditTaskStatus.FAILED
    assert provider.requests == []


def test_cancelled_late_success_is_audited_without_library_asset(db_session, configured_env) -> None:
    context = _context(db_session)
    task = _new_task(db_session, context)
    _submit(db_session, task)
    claim = claim_local_image_edit_task(db_session, task_id=task.id)
    db_session.commit()
    current = db_session.get(LocalImageEditTask, task.id)
    assert current is not None
    current.progress_phase = "provider_call"
    db_session.commit()
    cancel_local_image_edit_task(db_session, task_id=task.id, expected_revision=1)
    late_result = LocalEditResult(
        images=(WorkflowGeneratedImage(bytes_data=_make_demo_image_bytes_with_size(4, 3), mime_type="image/png"),),
        model="late-model",
        provider_response_id="late-response",
        provider_status="succeeded",
        effective_mode="masked_edit",
        effective_parameters={},
        provider_output_json={"response_id": "late-response"},
    )
    _persist_provider_result(
        db_session,
        storage=LocalStorage(),
        task_id=task.id,
        attempt_id=claim.attempt_id,
        snapshot={},
        result=late_result,
    )
    db_session.expire_all()
    persisted = db_session.get(LocalImageEditTask, task.id)
    assert persisted is not None
    assert persisted.status == LocalImageEditTaskStatus.CANCELLED
    assert persisted.result_asset_id is None
    attempt = db_session.scalar(
        select(LocalImageEditProviderAttempt).where(LocalImageEditProviderAttempt.attempt_id == claim.attempt_id)
    )
    assert attempt is not None
    assert attempt.late_result_asset_id is None
    assert attempt.result_json["late_image_sha256"]
    assert list(
        db_session.scalars(
            select(ProductImageAsset).where(
                ProductImageAsset.product_id == context.product_id,
                ProductImageAsset.origin_type == ProductImageOriginType.LOCAL_EDIT,
            )
        )
    ) == []
    _persist_provider_result(
        db_session,
        storage=LocalStorage(),
        task_id=task.id,
        attempt_id=claim.attempt_id,
        snapshot={},
        result=late_result,
    )
    db_session.expire_all()
    attempt = db_session.scalar(
        select(LocalImageEditProviderAttempt).where(LocalImageEditProviderAttempt.attempt_id == claim.attempt_id)
    )
    assert attempt is not None and attempt.late_result_asset_id is None


def test_adopt_and_revert_require_current_artifact_fence(db_session, configured_env) -> None:
    context = _context(db_session, with_target=True)
    task = _new_task(db_session, context, target=True)
    _submit(db_session, task)
    _execute(task.id, SupportingImageProvider(), configured_env)
    adoption = adopt_local_image_edit_result(
        db_session,
        task_id=task.id,
        expected_current_artifact_id=context.source_artifact_id,
    )
    assert adoption.artifact.node_run_id is None
    assert adoption.artifact.input_digest == "".join([sha256(b"source-input").hexdigest()])
    node = db_session.get(WorkflowGraphNode, context.target_node_id)
    assert node is not None and node.current_artifact_id == adoption.artifact.id
    with pytest.raises(ConflictError):
        adopt_local_image_edit_result(
            db_session,
            task_id=task.id,
            expected_current_artifact_id=context.source_artifact_id,
        )
    reverted = revert_local_image_edit_adoption(
        db_session,
        adoption_event_id=adoption.event.id,
        expected_current_artifact_id=adoption.artifact.id,
    )
    assert reverted.current_artifact_id == context.source_artifact_id
    with pytest.raises(ConflictError):
        revert_local_image_edit_adoption(
            db_session,
            adoption_event_id=adoption.event.id,
            expected_current_artifact_id=adoption.artifact.id,
        )


def test_mask_and_reference_asset_deletion_are_protected(db_session, configured_env) -> None:
    context = _context(db_session, with_reference=True)
    task = _new_task(db_session, context, reference=True)
    assert media_object_has_references(db_session, task.mask_media_object_id) is True
    with pytest.raises(ConflictError):
        delete_product_image_asset(db_session, asset_id=context.reference_asset_id, storage=LocalStorage())
