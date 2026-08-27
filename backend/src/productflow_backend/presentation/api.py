"""FastAPI 组装层。业务规则在 application/domain，路由只做鉴权、校验和投影。"""

from __future__ import annotations

from collections.abc import AsyncIterator
from contextlib import asynccontextmanager

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from starlette.types import ASGIApp, Message, Receive, Scope, Send

from productflow_backend.application.settings import initialize_provider_bindings_if_available
from productflow_backend.config import get_settings
from productflow_backend.infrastructure.logging import (
    cleanup_old_logs,
    configure_logging,
    new_request_id,
    reset_request_id,
    set_request_id,
)
from productflow_backend.presentation.errors import register_exception_handlers
from productflow_backend.presentation.routes.agent_conversations import router as agent_conversations_router
from productflow_backend.presentation.routes.agent_internal import router as agent_internal_router
from productflow_backend.presentation.routes.agent_product_workspaces import (
    router as agent_product_workspaces_router,
)
from productflow_backend.presentation.routes.agent_runtime import router as agent_runtime_router
from productflow_backend.presentation.routes.agent_sessions import router as agent_sessions_router
from productflow_backend.presentation.routes.agent_tasks import router as agent_tasks_router
from productflow_backend.presentation.routes.agent_tasks_internal import router as agent_tasks_internal_router
from productflow_backend.presentation.routes.agent_workbenches import router as agent_workbenches_router
from productflow_backend.presentation.routes.auth import router as auth_router
from productflow_backend.presentation.routes.delivery_presets import router as delivery_presets_router
from productflow_backend.presentation.routes.delivery_renditions import (
    router as delivery_renditions_router,
)
from productflow_backend.presentation.routes.delivery_renditions import (
    v3_router as delivery_renditions_v3_router,
)
from productflow_backend.presentation.routes.generation_queue import router as generation_queue_router
from productflow_backend.presentation.routes.global_agent_conversations import (
    router as global_agent_conversations_router,
)
from productflow_backend.presentation.routes.image_fidelity_checks import router as image_fidelity_checks_router
from productflow_backend.presentation.routes.image_sessions import router as image_sessions_router
from productflow_backend.presentation.routes.local_image_edits import router as local_image_edits_router
from productflow_backend.presentation.routes.media_library import router as media_library_router
from productflow_backend.presentation.routes.products import router as products_router
from productflow_backend.presentation.routes.settings import router as settings_router
from productflow_backend.presentation.routes.workflow_graphs import router as workflow_graphs_router
from productflow_backend.presentation.routes.workflow_recipes import v3_router as workflow_recipes_v3_router
from productflow_backend.presentation.session import ClockStableSessionMiddleware

REQUEST_ID_HEADER = b"x-request-id"


def create_app() -> FastAPI:
    """创建 FastAPI 应用，注册中间件和路由。"""
    settings = get_settings()
    configure_logging(settings)

    @asynccontextmanager
    async def lifespan(_: FastAPI) -> AsyncIterator[None]:
        cleanup_old_logs(settings)
        initialize_provider_bindings_if_available()
        yield

    app = FastAPI(title="ProductFlow API", version="0.1.0", lifespan=lifespan)
    register_exception_handlers(app)
    app.add_middleware(
        CORSMiddleware,
        allow_origins=settings.cors_origins,
        allow_credentials=True,
        allow_methods=["*"],
        allow_headers=["*"],
    )
    app.add_middleware(
        ClockStableSessionMiddleware,
        secret_key=settings.session_secret,
        same_site="lax",
        https_only=settings.session_cookie_secure,
    )
    app.add_middleware(RequestIdMiddleware)

    @app.get("/healthz")
    def healthcheck() -> dict[str, str]:
        return {"status": "ok"}

    app.include_router(auth_router)
    app.include_router(agent_runtime_router)
    app.include_router(agent_sessions_router)
    app.include_router(agent_tasks_router)
    app.include_router(agent_internal_router)
    app.include_router(agent_tasks_internal_router)
    app.include_router(agent_conversations_router)
    app.include_router(global_agent_conversations_router)
    app.include_router(agent_product_workspaces_router)
    app.include_router(agent_workbenches_router)
    app.include_router(generation_queue_router)
    app.include_router(media_library_router)
    app.include_router(products_router)
    app.include_router(delivery_presets_router)
    app.include_router(delivery_renditions_router)
    app.include_router(delivery_renditions_v3_router)
    app.include_router(image_fidelity_checks_router)
    app.include_router(workflow_graphs_router)
    app.include_router(workflow_recipes_v3_router)
    app.include_router(image_sessions_router)
    app.include_router(local_image_edits_router)
    app.include_router(settings_router)
    return app


class RequestIdMiddleware:
    def __init__(self, app: ASGIApp) -> None:
        self.app = app

    async def __call__(self, scope: Scope, receive: Receive, send: Send) -> None:
        if scope["type"] != "http":
            await self.app(scope, receive, send)
            return

        request_id = _request_id_from_scope(scope) or new_request_id()
        token = set_request_id(request_id)

        async def send_with_request_id(message: Message) -> None:
            if message["type"] == "http.response.start":
                message["headers"] = _headers_with_request_id(message.get("headers", []), request_id)
            await send(message)

        try:
            await self.app(scope, receive, send_with_request_id)
        finally:
            reset_request_id(token)


def _request_id_from_scope(scope: Scope) -> str | None:
    for header_name, header_value in scope.get("headers", []):
        if header_name.lower() == REQUEST_ID_HEADER:
            return header_value.decode("latin-1")
    return None


def _headers_with_request_id(headers: list[tuple[bytes, bytes]], request_id: str) -> list[tuple[bytes, bytes]]:
    response_headers = [(name, value) for name, value in headers if name.lower() != REQUEST_ID_HEADER]
    response_headers.append((REQUEST_ID_HEADER, request_id.encode("latin-1", errors="replace")))
    return response_headers
