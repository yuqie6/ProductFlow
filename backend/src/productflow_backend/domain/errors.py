from __future__ import annotations

from typing import ClassVar


class BusinessError(ValueError):
    """Expected user-facing business failure raised below the presentation layer."""

    status_code: ClassVar[int] = 400

    def __init__(self, message: str) -> None:
        super().__init__(message)
        self.message = message


class BusinessValidationError(BusinessError):
    """Business request is syntactically valid HTTP, but invalid for the current workflow state."""

    status_code: ClassVar[int] = 400


class NotFoundError(BusinessError):
    """Requested domain/application resource does not exist."""

    status_code: ClassVar[int] = 404
