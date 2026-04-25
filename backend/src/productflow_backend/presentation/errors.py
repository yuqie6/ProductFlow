from __future__ import annotations

from typing import NoReturn

from fastapi import HTTPException


def raise_value_error_as_http(exc: ValueError) -> NoReturn:
    detail = str(exc)
    if detail == "海报文件不存在":
        raise HTTPException(status_code=400, detail=detail) from exc
    if detail.endswith("不存在"):
        raise HTTPException(status_code=404, detail=detail) from exc
    raise HTTPException(status_code=400, detail=detail) from exc
