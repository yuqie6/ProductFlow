from __future__ import annotations

from typing import ClassVar


class BusinessError(ValueError):
    """展示层之下抛出的、可给用户看的预期业务失败。"""

    status_code: ClassVar[int] = 400

    def __init__(self, message: str) -> None:
        super().__init__(message)
        self.message = message


class BusinessValidationError(BusinessError):
    """HTTP 语法合法，但相对当前工作流状态无效。"""

    status_code: ClassVar[int] = 400


class StructuredBusinessValidationError(BusinessValidationError):
    """带有界、可机读 issue 明细的业务校验失败。"""

    def __init__(self, message: str, *, code: str, issues: list[dict[str, str]]) -> None:
        super().__init__(message)
        self.error_code = code
        self.issues = [
            {"path": issue["path"], "message": issue["message"]}
            for issue in issues
            if issue.get("path") and issue.get("message")
        ][:8]


class NotFoundError(BusinessError):
    """请求的领域或应用资源不存在。"""

    status_code: ClassVar[int] = 404


class ConflictError(BusinessError):
    """请求的变更与已有耐久业务引用冲突。"""

    status_code: ClassVar[int] = 409


class GoneError(BusinessError):
    """请求的合同已退休，在线路径不可达。"""

    status_code: ClassVar[int] = 410


class ResourceBusyError(BusinessError):
    """全局 provider / worker 容量当前已耗尽。"""

    status_code: ClassVar[int] = 429


class QueueUnavailableError(BusinessError):
    """数据库任务状态已持久化之后，耐久队列投递失败。"""

    status_code: ClassVar[int] = 503


class AgentServiceUnavailableError(BusinessError):
    """工作流 Agent 内部服务不可用或返回了无法信任的响应。"""

    status_code: ClassVar[int] = 503
