from __future__ import annotations

from fastapi.testclient import TestClient
from helpers import _login

from productflow_backend.presentation.api import create_app

PRODUCT_ID = "00000000-0000-0000-0000-000000000001"
DRAFT_ID = "00000000-0000-0000-0000-000000000002"

RETIRED_GET_PATHS = (
    f"/api/v2/products/{PRODUCT_ID}/workflow-drafts",
    f"/api/v2/products/{PRODUCT_ID}/workflow-drafts/{DRAFT_ID}",
    "/api/v2/legacy-archives",
    "/api/v2/legacy-archives/workflow/missing",
    "/api/v2/gallery",
    "/api/v2/agent-conversations/missing/workflow-draft-reviews/missing",
)

RETIRED_POST_PATHS = (
    f"/api/v2/products/{PRODUCT_ID}/workflow-drafts",
    f"/api/v2/products/{PRODUCT_ID}/workflow-drafts/{DRAFT_ID}/confirm",
    "/api/v2/legacy-archives/rebuilds",
)


def test_retired_workflow_draft_and_archive_urls_return_404(configured_env) -> None:
    client = TestClient(create_app())
    _login(client)
    for path in RETIRED_GET_PATHS:
        response = client.get(path)
        assert response.status_code == 404, path
    for path in RETIRED_POST_PATHS:
        response = client.post(path, json={})
        assert response.status_code == 404, path
