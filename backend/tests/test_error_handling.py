from __future__ import annotations

import pytest
from fastapi import HTTPException

from productflow_backend.domain.errors import BusinessError, BusinessValidationError, NotFoundError
from productflow_backend.presentation.errors import raise_value_error_as_http


def _mapped_http_error(exc: ValueError) -> HTTPException:
    with pytest.raises(HTTPException) as raised:
        raise_value_error_as_http(exc)
    return raised.value


def test_typed_not_found_maps_to_404_without_message_suffix() -> None:
    error = _mapped_http_error(NotFoundError("资源已移除"))

    assert error.status_code == 404
    assert error.detail == "资源已移除"


def test_typed_business_error_maps_to_400() -> None:
    error = _mapped_http_error(BusinessError("请选择一张图片"))

    assert error.status_code == 400
    assert error.detail == "请选择一张图片"


def test_typed_poster_file_missing_remains_400() -> None:
    error = _mapped_http_error(BusinessValidationError("海报文件不存在"))

    assert error.status_code == 400
    assert error.detail == "海报文件不存在"


def test_typed_workflow_integrity_error_remains_400() -> None:
    error = _mapped_http_error(BusinessValidationError("工作流连线引用了不存在的节点"))

    assert error.status_code == 400
    assert error.detail == "工作流连线引用了不存在的节点"


def test_legacy_value_error_fallback_remains_compatible() -> None:
    missing = _mapped_http_error(ValueError("旧资源不存在"))
    poster_file_missing = _mapped_http_error(ValueError("海报文件不存在"))
    generic = _mapped_http_error(ValueError("普通业务错误"))

    assert missing.status_code == 404
    assert missing.detail == "旧资源不存在"
    assert poster_file_missing.status_code == 400
    assert poster_file_missing.detail == "海报文件不存在"
    assert generic.status_code == 400
    assert generic.detail == "普通业务错误"
