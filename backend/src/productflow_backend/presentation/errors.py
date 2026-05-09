from __future__ import annotations

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse

from productflow_backend.domain.errors import BusinessError


def business_error_to_response(exc: BusinessError) -> JSONResponse:
    return JSONResponse(status_code=exc.status_code, content={"detail": str(exc)})


async def business_error_exception_handler(_: Request, exc: BusinessError) -> JSONResponse:
    return business_error_to_response(exc)


def register_exception_handlers(app: FastAPI) -> None:
    app.add_exception_handler(BusinessError, business_error_exception_handler)
