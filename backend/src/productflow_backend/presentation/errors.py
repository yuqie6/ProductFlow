from __future__ import annotations

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse

from productflow_backend.domain.errors import BusinessError


def business_error_to_response(exc: BusinessError) -> JSONResponse:
    return JSONResponse(status_code=exc.status_code, content={"detail": str(exc)})


async def business_error_exception_handler(_: Request, exc: BusinessError) -> JSONResponse:
    return business_error_to_response(exc)


async def request_validation_exception_handler(_: Request, exc: RequestValidationError) -> JSONResponse:
    """仅回显字段和错误说明，不返回输入值或校验上下文。"""
    errors: list[dict[str, str]] = []
    for error in exc.errors():
        field = ".".join(str(part) for part in error.get("loc", ()) if part not in {"body", "query", "path"})
        message = str(error.get("msg", "参数不合法")).removeprefix("Value error, ")
        if error.get("type") == "missing":
            message = "此字段不能为空"
        errors.append({"field": field, "message": message})
    detail = "；".join(
        f"{error['field']}：{error['message']}" if error["field"] else error["message"] for error in errors
    )
    return JSONResponse(status_code=422, content={"detail": detail or "请求参数不合法", "errors": errors})


def register_exception_handlers(app: FastAPI) -> None:
    app.add_exception_handler(BusinessError, business_error_exception_handler)
    app.add_exception_handler(RequestValidationError, request_validation_exception_handler)
