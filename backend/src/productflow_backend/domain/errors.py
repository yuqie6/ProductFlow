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


class StructuredBusinessValidationError(BusinessValidationError):
    """Business validation failure with bounded machine-readable issue details."""

    def __init__(self, message: str, *, code: str, issues: list[dict[str, str]]) -> None:
        super().__init__(message)
        self.error_code = code
        self.issues = [
            {"path": issue["path"], "message": issue["message"]}
            for issue in issues
            if issue.get("path") and issue.get("message")
        ][:8]


class NotFoundError(BusinessError):
    """Requested domain/application resource does not exist."""

    status_code: ClassVar[int] = 404


class ConflictError(BusinessError):
    """Requested mutation conflicts with an existing durable business reference."""

    status_code: ClassVar[int] = 409


class ResourceBusyError(BusinessError):
    """Global provider/worker resource capacity is currently exhausted."""

    status_code: ClassVar[int] = 429


class QueueUnavailableError(BusinessError):
    """Durable queue delivery failed after the database task state was persisted."""

    status_code: ClassVar[int] = 503


class AgentServiceUnavailableError(BusinessError):
    """工作流 Agent 内部服务不可用或返回了无法信任的响应。"""

    status_code: ClassVar[int] = 503
