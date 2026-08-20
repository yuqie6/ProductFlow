from __future__ import annotations

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse

from productflow_backend.domain.errors import BusinessError, StructuredBusinessValidationError


def business_error_to_response(exc: BusinessError) -> JSONResponse:
    content: dict[str, object] = {"detail": str(exc)}
    if isinstance(exc, StructuredBusinessValidationError):
        content["error"] = {
            "code": exc.error_code,
            "message": str(exc),
            "details": {"issues": exc.issues},
        }
    return JSONResponse(status_code=exc.status_code, content=content)


async def business_error_exception_handler(_: Request, exc: BusinessError) -> JSONResponse:
    return business_error_to_response(exc)


def register_exception_handlers(app: FastAPI) -> None:
    app.add_exception_handler(BusinessError, business_error_exception_handler)
