from __future__ import annotations

from collections.abc import AsyncIterator
from contextlib import asynccontextmanager

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from starlette.middleware.sessions import SessionMiddleware

from productflow_backend.config import get_settings
from productflow_backend.infrastructure.logging import cleanup_old_logs, configure_logging
from productflow_backend.infrastructure.queue import recover_unfinished_jobs, recover_unfinished_workflow_runs
from productflow_backend.presentation.routes.auth import router as auth_router
from productflow_backend.presentation.routes.image_sessions import router as image_sessions_router
from productflow_backend.presentation.routes.jobs import router as jobs_router
from productflow_backend.presentation.routes.product_workflows import router as product_workflows_router
from productflow_backend.presentation.routes.products import router as products_router
from productflow_backend.presentation.routes.settings import router as settings_router


def create_app() -> FastAPI:
    """创建 FastAPI 应用，注册中间件和路由。"""
    settings = get_settings()
    configure_logging(settings)

    @asynccontextmanager
    async def lifespan(_: FastAPI) -> AsyncIterator[None]:
        cleanup_old_logs(settings)
        recover_unfinished_jobs()
        recover_unfinished_workflow_runs()
        yield

    app = FastAPI(title="ProductFlow API", version="0.1.0", lifespan=lifespan)
    app.add_middleware(
        CORSMiddleware,
        allow_origins=settings.cors_origins,
        allow_credentials=True,
        allow_methods=["*"],
        allow_headers=["*"],
    )
    app.add_middleware(
        SessionMiddleware,
        secret_key=settings.session_secret,
        same_site="lax",
        https_only=settings.session_cookie_secure,
    )

    @app.get("/healthz")
    def healthcheck() -> dict[str, str]:
        return {"status": "ok"}

    app.include_router(auth_router)
    app.include_router(products_router)
    app.include_router(product_workflows_router)
    app.include_router(jobs_router)
    app.include_router(image_sessions_router)
    app.include_router(settings_router)
    return app
