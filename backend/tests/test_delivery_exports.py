from __future__ import annotations

import json
from io import BytesIO
from pathlib import Path
from zipfile import ZipFile

import pytest
from fastapi.testclient import TestClient
from helpers import _login
from test_delivery_renditions import _create_generated_source

from productflow_backend.application.delivery_renditions.exports import (
    _deduplicate_filename,
    _read_exact_bounded_file,
    export_delivery_rendition_jobs,
)
from productflow_backend.application.delivery_renditions.service import (
    execute_delivery_rendition_job,
    get_delivery_rendition_job,
    submit_delivery_rendition_job,
)
from productflow_backend.application.media_objects import inspect_image_bytes
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.storage import LocalStorage


def _complete_jobs(db_session, *, provider_capture=None):
    source, run, node_run = _create_generated_source(
        db_session,
        provider_capture=provider_capture,
    )
    specs = (
        {"width": 48, "height": 48, "format": "png", "fit": "contain"},
        {
            "width": 64,
            "height": 32,
            "format": "jpeg",
            "fit": "contain",
            "background_color": "#FFFFFF",
        },
    )
    jobs = []
    for spec in specs:
        job = submit_delivery_rendition_job(
            db_session,
            source_asset_id=source.id,
            delivery_spec=spec,
            enqueue=lambda _: None,
        )
        execute_delivery_rendition_job(job.id)
        db_session.expire_all()
        jobs.append(get_delivery_rendition_job(db_session, job.id))
    return source, run, node_run, jobs


def _read_archive(archive_path: Path) -> tuple[bytes, dict[str, object], dict[str, bytes]]:
    content = archive_path.read_bytes()
    with ZipFile(BytesIO(content)) as archive:
        manifest = json.loads(archive.read("manifest.json"))
        files = {
            name: archive.read(name)
            for name in archive.namelist()
            if name != "manifest.json"
        }
    return content, manifest, files


def test_delivery_export_is_deterministic_and_manifest_matches_measured_files(
    configured_env,
    db_session,
) -> None:
    providers = []
    source, run, node_run, jobs = _complete_jobs(db_session, provider_capture=providers)
    first = export_delivery_rendition_jobs(
        db_session,
        product_id=source.product_id,
        rendition_job_ids=[job.id for job in jobs],
    )
    second = export_delivery_rendition_jobs(
        db_session,
        product_id=source.product_id,
        rendition_job_ids=[job.id for job in jobs],
    )
    try:
        first_bytes, manifest, files = _read_archive(first.path)
        second_bytes, second_manifest, _ = _read_archive(second.path)
    finally:
        first.path.unlink(missing_ok=True)
        second.path.unlink(missing_ok=True)

    assert first_bytes == second_bytes
    assert manifest == second_manifest
    assert manifest["schema_version"] == 1
    assert manifest["kind"] == "productflow.delivery_export"
    assert manifest["complete"] is True
    assert manifest["missing_items"] == []
    assert manifest["generated_at"] == max(job.finished_at for job in jobs).isoformat()
    assert len(manifest["items"]) == 2
    assert len(providers) == 1
    assert len(providers[0].requests) == 1

    item_by_job_id = {item["rendition_job"]["id"]: item for item in manifest["items"]}
    for index, job in enumerate(jobs, start=1):
        item = item_by_job_id[job.id]
        filename = item["filename"]
        assert filename in files
        assert f"-{index:02d}-" in filename
        assert job.id not in filename
        assert source.id not in filename
        assert filename.endswith(".png" if job.spec_json["format"] == "png" else ".jpg")
        assert item["graph"]["revision"] == run.graph_revision
        assert item["run"] == {"id": run.id, "revision": run.graph_revision}
        assert item["node_run_id"] == node_run.id
        assert item["generated_at"] == job.finished_at.isoformat()

        measured = inspect_image_bytes(files[filename])
        assert item["measured"] == {
            "mime_type": measured.mime_type,
            "width": measured.width,
            "height": measured.height,
            "byte_size": measured.byte_size,
            "sha256": measured.sha256,
        }
        assert item["result_asset"]["sha256"] == measured.sha256

    manifest_text = json.dumps(manifest, ensure_ascii=False)
    for forbidden in ("storage_path", "api_key", "provider_secret", "secret"):
        assert forbidden not in manifest_text


def test_delivery_export_partial_reports_missing_jobs_and_default_blocks(
    configured_env,
    db_session,
) -> None:
    source, _, _, jobs = _complete_jobs(db_session)
    queued = submit_delivery_rendition_job(
        db_session,
        source_asset_id=source.id,
        delivery_spec={"width": 72, "height": 72, "format": "webp", "fit": "cover"},
        enqueue=lambda _: None,
    )

    with pytest.raises(ConflictError, match="未成功"):
        export_delivery_rendition_jobs(
            db_session,
            product_id=source.product_id,
            rendition_job_ids=[jobs[0].id, queued.id],
        )
    with pytest.raises(BusinessValidationError, match="重复"):
        export_delivery_rendition_jobs(
            db_session,
            product_id=source.product_id,
            rendition_job_ids=[jobs[0].id, jobs[0].id],
        )
    with pytest.raises(ConflictError, match="没有可导出"):
        export_delivery_rendition_jobs(
            db_session,
            product_id=source.product_id,
            rendition_job_ids=[queued.id],
            allow_partial=True,
        )

    archive = export_delivery_rendition_jobs(
        db_session,
        product_id=source.product_id,
        rendition_job_ids=[jobs[0].id, queued.id],
        allow_partial=True,
    )
    try:
        _, manifest, files = _read_archive(archive.path)
    finally:
        archive.path.unlink(missing_ok=True)

    assert manifest["complete"] is False
    assert manifest["allow_partial"] is True
    assert manifest["generated_at"] == jobs[0].finished_at.isoformat()
    assert [item["job_id"] for item in manifest["missing_items"]] == [queued.id]
    assert manifest["missing_items"][0]["status"] == "queued"
    assert len(files) == 1


def test_delivery_export_hides_cross_product_and_missing_jobs(db_session) -> None:
    source, _, _, jobs = _complete_jobs(db_session)
    other_source, _, _, _ = _complete_jobs(db_session)
    other_job = submit_delivery_rendition_job(
        db_session,
        source_asset_id=other_source.id,
        delivery_spec={"width": 48, "height": 48, "format": "png", "fit": "contain"},
        enqueue=lambda _: None,
    )

    with pytest.raises(NotFoundError):
        export_delivery_rendition_jobs(
            db_session,
            product_id=source.product_id,
            rendition_job_ids=[other_job.id],
        )
    with pytest.raises(NotFoundError):
        export_delivery_rendition_jobs(
            db_session,
            product_id=source.product_id,
            rendition_job_ids=["missing-rendition-job"],
        )
    with pytest.raises(NotFoundError):
        export_delivery_rendition_jobs(
            db_session,
            product_id="missing-product",
            rendition_job_ids=[jobs[0].id],
        )


def test_delivery_export_rejects_missing_result_file_and_is_bounded(configured_env, db_session, monkeypatch) -> None:
    source, _, _, jobs = _complete_jobs(db_session)
    result_media = jobs[0].result_asset.media_object
    assert result_media is not None
    LocalStorage().resolve(result_media.storage_path).unlink()

    with pytest.raises(ConflictError, match="文件不可用"):
        export_delivery_rendition_jobs(
            db_session,
            product_id=source.product_id,
            rendition_job_ids=[jobs[0].id],
        )
    with pytest.raises(BusinessValidationError, match="最多导出"):
        export_delivery_rendition_jobs(
            db_session,
            product_id=source.product_id,
            rendition_job_ids=[jobs[0].id] * 101,
        )


def test_delivery_export_enforces_total_bytes_before_writing(configured_env, db_session, monkeypatch) -> None:
    source, _, _, jobs = _complete_jobs(db_session)
    monkeypatch.setattr(
        "productflow_backend.application.delivery_renditions.exports.DELIVERY_EXPORT_MAX_BYTES",
        1,
    )

    with pytest.raises(BusinessValidationError, match="总大小"):
        export_delivery_rendition_jobs(
            db_session,
            product_id=source.product_id,
            rendition_job_ids=[jobs[0].id],
        )


def test_delivery_export_file_read_is_bounded_by_verified_size(tmp_path) -> None:
    changed = tmp_path / "changed.png"
    changed.write_bytes(b"verified" + b"unexpected trailing bytes")

    with pytest.raises(ConflictError, match="大小已变化"):
        _read_exact_bounded_file(changed, expected_byte_size=len(b"verified"))


def test_delivery_export_filename_deduplication_is_case_insensitive() -> None:
    used: set[str] = set()

    assert _deduplicate_filename("Hero.PNG", used) == "Hero.PNG"
    assert _deduplicate_filename("hero.png", used) == "hero-2.png"


def test_delivery_export_route_returns_zip_and_forbids_extra_fields(configured_env, db_session) -> None:
    source, _, _, jobs = _complete_jobs(db_session)
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)
    response = client.post(
        f"/api/v3/products/{source.product_id}/delivery-exports",
        json={"rendition_job_ids": [jobs[0].id]},
    )
    assert response.status_code == 200, response.text
    assert response.headers["content-type"].startswith("application/zip")
    assert "delivery-export.zip" in response.headers["content-disposition"]
    with ZipFile(BytesIO(response.content)) as archive:
        assert "manifest.json" in archive.namelist()
        manifest = json.loads(archive.read("manifest.json"))
    assert manifest["complete"] is True

    invalid = client.post(
        f"/api/v3/products/{source.product_id}/delivery-exports",
        json={"rendition_job_ids": [jobs[0].id], "unexpected": True},
    )
    assert invalid.status_code == 422

    duplicate = client.post(
        f"/api/v3/products/{source.product_id}/delivery-exports",
        json={"rendition_job_ids": [jobs[0].id, jobs[0].id]},
    )
    assert duplicate.status_code == 400
