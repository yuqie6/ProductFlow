from __future__ import annotations

from typing import NoReturn

from fastapi import HTTPException

from productflow_backend.domain.errors import BusinessError


def raise_value_error_as_http(exc: ValueError) -> NoReturn:
    detail = str(exc)
    if isinstance(exc, BusinessError):
        raise HTTPException(status_code=exc.status_code, detail=detail) from exc
    if detail == "海报文件不存在":
        raise HTTPException(status_code=400, detail=detail) from exc
    if detail.endswith("不存在"):
        raise HTTPException(status_code=404, detail=detail) from exc
    raise HTTPException(status_code=400, detail=detail) from exc
