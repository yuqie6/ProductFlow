"""Node.js/Pi adapter HTTP 客户端。对方 session/event files 只服务交互执行，不是 durable 业务权威。"""

from __future__ import annotations

import json
from collections.abc import AsyncIterator
from datetime import datetime
from functools import lru_cache
from typing import Any, Literal
from urllib.parse import quote, urlsplit

import httpx
from pydantic import BaseModel, ConfigDict, Field, ValidationError, field_validator, model_serializer, model_validator

from productflow_backend.config import get_settings
from productflow_backend.domain.enums import AgentToolStepKind, AgentToolStepStatus, AgentTurnStatus


class AgentServiceQuestionOption(BaseModel):
    model_config = ConfigDict(extra="ignore")

    label: str
    description: str = ""


class AgentServiceQuestion(BaseModel):
    model_config = ConfigDict(extra="ignore")

    id: str
    header: str
    question: str
    options: list[AgentServiceQuestionOption] = Field(default_factory=list)


class AgentServiceArtifact(BaseModel):
    model_config = ConfigDict(extra="ignore")

    name: str
    value: dict[str, Any]
    step_id: str


class AgentServiceToolStepValidationIssue(BaseModel):
    model_config = ConfigDict(extra="forbid")

    path: str = Field(min_length=1, max_length=200)
    message: str = Field(min_length=1, max_length=500)


class AgentServiceToolStepDetails(BaseModel):
    model_config = ConfigDict(extra="forbid")

    phase: Literal["skill_load", "context_injection", "question", "tool_result"] | None = None
    skill_name: str | None = Field(default=None, min_length=1, max_length=64)
    resource_path: str | None = Field(default=None, min_length=1, max_length=256)
    instruction_excerpt: str | None = Field(default=None, min_length=1, max_length=12_000)
    instruction_truncated: bool | None = None
    context_sections: list[str] | None = Field(default=None, max_length=8)
    runtime_context_keys: list[str] | None = Field(default=None, max_length=32)
    contract_fields: list[str] | None = Field(default=None, max_length=16)
    page_route: str | None = Field(default=None, max_length=512)
    page_type: str | None = Field(default=None, max_length=80)
    selected_asset_count: int | None = Field(default=None, ge=0, le=100)
    visible_asset_count: int | None = Field(default=None, ge=0, le=100)
    context_bytes: int | None = Field(default=None, ge=0, le=64 * 1024)
    input_summary: str | None = Field(default=None, min_length=1, max_length=240)
    output_summary: str | None = Field(default=None, min_length=1, max_length=240)
    error_code: str | None = Field(default=None, min_length=1, max_length=120)
    error_message: str | None = Field(default=None, min_length=1, max_length=1000)
    retryable: bool | None = None
    validation_issues: list[AgentServiceToolStepValidationIssue] | None = Field(default=None, max_length=8)
    question_id: str | None = Field(default=None, min_length=1, max_length=120)
    question_header: str | None = Field(default=None, min_length=1, max_length=32)
    question_text: str | None = Field(default=None, min_length=1, max_length=2000)
    option_labels: list[str] | None = Field(default=None, max_length=5)

    @model_serializer(mode="wrap")
    def serialize_without_empty_fields(self, handler: Any) -> dict[str, Any]:
        return {key: value for key, value in handler(self).items() if value is not None}

    @model_validator(mode="after")
    def validate_bounded_details(self) -> AgentServiceToolStepDetails:
        for value in (
            self.skill_name,
            self.resource_path,
            self.page_route,
            self.page_type,
            self.input_summary,
            self.output_summary,
            self.error_code,
            self.error_message,
            self.question_id,
            self.question_header,
        ):
            if value is not None and ("\n" in value or "\r" in value):
                raise ValueError("tool step detail strings must be single-line")
        if self.instruction_excerpt is not None and not self.instruction_excerpt.strip():
            raise ValueError("instruction_excerpt must not be blank")
        for values in (self.context_sections, self.runtime_context_keys, self.contract_fields, self.option_labels):
            if values is not None and any(not value.strip() or len(value) > 160 for value in values):
                raise ValueError("tool step detail lists contain an invalid value")
        if self.option_labels is not None and len(set(self.option_labels)) != len(self.option_labels):
            raise ValueError("tool step option labels must be unique")
        encoded = json.dumps(self.model_dump(mode="json", exclude_none=True), ensure_ascii=False, separators=(",", ":"))
        if len(encoded.encode()) > 16 * 1024:
            raise ValueError("tool step details exceed the bounded size")
        return self


class AgentServiceToolStep(BaseModel):
    model_config = ConfigDict(extra="forbid")

    step_id: str = Field(min_length=1, max_length=200)
    kind: AgentToolStepKind
    summary: str = Field(min_length=1, max_length=160)
    status: AgentToolStepStatus
    tool_name: str | None = Field(default=None, min_length=1, max_length=120)
    details: AgentServiceToolStepDetails | None = None

    @field_validator("step_id")
    @classmethod
    def validate_step_id(cls, value: str) -> str:
        if not value.strip():
            raise ValueError("step_id must not be blank")
        if len(value.encode("utf-8")) > 200:
            raise ValueError("step_id must not exceed 200 bytes")
        return value

    @field_validator("summary")
    @classmethod
    def validate_summary(cls, value: str) -> str:
        if "\n" in value or "\r" in value:
            raise ValueError("summary must be a single line")
        if not value.strip():
            raise ValueError("summary must not be blank")
        if len(value.encode("utf-8")) > 160:
            raise ValueError("summary must not exceed 160 bytes")
        return value

    @field_validator("tool_name")
    @classmethod
    def validate_tool_name(cls, value: str | None) -> str | None:
        if value is not None and ("\n" in value or "\r" in value or not value.strip()):
            raise ValueError("tool_name must be a single non-empty line")
        return value


class AgentServiceTurnState(BaseModel):
    model_config = ConfigDict(extra="ignore")

    api_version: str
    run_id: str
    turn_id: str
    status: AgentTurnStatus
    execution_attempt: int | None = Field(default=None, ge=1)
    execution_fencing_token: int | None = Field(default=None, ge=1)
    question: AgentServiceQuestion | None = None
    artifact: AgentServiceArtifact | None = None
    tool_steps: list[AgentServiceToolStep] | None = Field(default=None, max_length=100)
    output: str = ""
    error: str = ""
    created_at: datetime
    updated_at: datetime
    started_at: datetime | None = None
    finished_at: datetime | None = None


class AgentServiceRequestError(RuntimeError):
    def __init__(self, *, status_code: int | None, code: str, safe_message: str) -> None:
        super().__init__(safe_message)
        self.status_code = status_code
        self.code = code
        self.safe_message = safe_message


class AgentServiceClient:
    """交互式 Turn HTTP 面。网络失败是 unavailable，不能据此把业务标 failed。"""

    def __init__(
        self,
        *,
        base_url: str,
        internal_token: str,
        connect_timeout_seconds: float = 5.0,
        read_timeout_seconds: float = 90.0,
    ) -> None:
        normalized_url = base_url.strip().rstrip("/")
        parsed = urlsplit(normalized_url)
        if (
            parsed.scheme not in {"http", "https"}
            or not parsed.netloc
            or parsed.username is not None
            or parsed.password is not None
            or parsed.query
            or parsed.fragment
            or parsed.path not in {"", "/"}
        ):
            raise ValueError("Agent service base URL 必须是无 userinfo/query/fragment 的绝对 HTTP(S) 地址")
        normalized_token = internal_token.strip()
        if not normalized_token:
            raise ValueError("Agent service internal token 不能为空")
        self.base_url = normalized_url
        self.internal_token = normalized_token
        self.timeout = httpx.Timeout(
            connect=connect_timeout_seconds,
            read=read_timeout_seconds,
            write=read_timeout_seconds,
            pool=connect_timeout_seconds,
        )

    def start_turn(
        self,
        *,
        conversation_id: str,
        task_id: str | None = None,
        input_text: str,
        asset_ids: list[str],
        idempotency_key: str,
        page_context: dict[str, Any] | None = None,
        turn_id: str | None = None,
    ) -> AgentServiceTurnState:
        return self._request_state(
            "POST",
            self._execution_path(conversation_id, task_id) + "/turns",
            json_body={
                "input_text": input_text,
                "asset_ids": asset_ids,
                "idempotency_key": idempotency_key,
                "page_context": page_context,
                **({"turn_id": turn_id} if turn_id is not None else {}),
            },
        )

    def get_turn(
        self,
        *,
        conversation_id: str,
        turn_id: str,
        task_id: str | None = None,
    ) -> AgentServiceTurnState:
        return self._request_state(
            "GET",
            self._turn_path(conversation_id, turn_id, task_id=task_id),
        )

    def cancel_turn(
        self,
        *,
        conversation_id: str,
        turn_id: str,
        task_id: str | None = None,
    ) -> AgentServiceTurnState:
        return self._request_state(
            "POST",
            self._turn_path(conversation_id, turn_id, task_id=task_id) + "/cancel",
            json_body={},
        )

    def resume_turn(
        self,
        *,
        conversation_id: str,
        turn_id: str,
        task_id: str | None = None,
    ) -> AgentServiceTurnState:
        return self._request_state(
            "POST",
            self._turn_path(conversation_id, turn_id, task_id=task_id) + "/resume",
            json_body={},
        )

    def answer_question(
        self,
        *,
        conversation_id: str,
        turn_id: str,
        question_id: str,
        answer: dict[str, Any],
        task_id: str | None = None,
    ) -> AgentServiceTurnState:
        return self._request_state(
            "POST",
            self._turn_path(conversation_id, turn_id, task_id=task_id)
            + "/questions/"
            + quote(question_id, safe="")
            + "/answer",
            json_body={"answer": answer},
        )

    async def stream_turn_events(
        self,
        *,
        conversation_id: str,
        turn_id: str,
        after: int,
        task_id: str | None = None,
    ) -> AsyncIterator[bytes]:
        headers = self._headers()
        params = {"after": str(after)}
        try:
            async with httpx.AsyncClient(timeout=None) as client:
                async with client.stream(
                    "GET",
                    self.base_url + self._turn_path(conversation_id, turn_id, task_id=task_id) + "/events",
                    headers=headers,
                    params=params,
                ) as response:
                    if response.status_code < 200 or response.status_code >= 300:
                        data = await response.aread()
                        raise _response_error(response.status_code, data)
                    async for chunk in response.aiter_raw():
                        if chunk:
                            yield chunk
        except AgentServiceRequestError:
            raise
        except (httpx.HTTPError, OSError) as exc:
            raise AgentServiceRequestError(
                status_code=None,
                code="unavailable",
                safe_message="Agent 服务暂时不可用",
            ) from exc

    def _request_state(
        self,
        method: str,
        path: str,
        *,
        json_body: dict[str, Any] | None = None,
    ) -> AgentServiceTurnState:
        try:
            with httpx.Client(timeout=self.timeout) as client:
                response = client.request(
                    method,
                    self.base_url + path,
                    headers=self._headers(),
                    json=json_body,
                )
        except (httpx.HTTPError, OSError) as exc:
            # 连接层失败无法证明 Turn 结果，留给同步/恢复标 unknown 或重试。
            raise AgentServiceRequestError(
                status_code=None,
                code="unavailable",
                safe_message="Agent 服务暂时不可用",
            ) from exc
        if response.status_code < 200 or response.status_code >= 300:
            raise _response_error(response.status_code, response.content)
        try:
            return AgentServiceTurnState.model_validate_json(response.content)
        except ValidationError as exc:
            raise AgentServiceRequestError(
                status_code=502,
                code="invalid_response",
                safe_message="Agent 服务返回了无效响应",
            ) from exc

    def _headers(self) -> dict[str, str]:
        return {
            "Authorization": f"Bearer {self.internal_token}",
            "Accept": "application/json",
        }

    @staticmethod
    def _conversation_path(conversation_id: str) -> str:
        return f"/internal/v1/conversations/{quote(conversation_id, safe='')}"

    @classmethod
    def _execution_path(cls, conversation_id: str, task_id: str | None) -> str:
        # Task 走独立 run；否则退回 conversation 投影。
        if task_id:
            return f"/internal/v1/tasks/{quote(task_id, safe='')}"
        return cls._conversation_path(conversation_id)

    @classmethod
    def _turn_path(cls, conversation_id: str, turn_id: str, *, task_id: str | None = None) -> str:
        return cls._execution_path(conversation_id, task_id) + f"/turns/{quote(turn_id, safe='')}"


def _response_error(status_code: int, body: bytes) -> AgentServiceRequestError:
    code = "upstream_error"
    message = "Agent 服务请求失败"
    try:
        payload = json.loads(body[:8192])
        error = payload.get("error") if isinstance(payload, dict) else None
        if isinstance(error, dict):
            candidate_code = error.get("code")
            candidate_message = error.get("message")
            if isinstance(candidate_code, str) and candidate_code:
                code = candidate_code[:120]
            if isinstance(candidate_message, str) and candidate_message:
                message = candidate_message[:1000]
    except (UnicodeDecodeError, json.JSONDecodeError):
        pass
    return AgentServiceRequestError(
        status_code=status_code,
        code=code,
        safe_message=message,
    )


@lru_cache(maxsize=1)
def get_agent_service_client() -> AgentServiceClient:
    settings = get_settings()
    if settings.agent_service_base_url is None or settings.agent_service_internal_token is None:
        raise AgentServiceRequestError(
            status_code=None,
            code="not_configured",
            safe_message="Agent 服务尚未配置",
        )
    return AgentServiceClient(
        base_url=settings.agent_service_base_url,
        internal_token=settings.agent_service_internal_token,
        connect_timeout_seconds=settings.agent_service_connect_timeout_seconds,
        read_timeout_seconds=settings.agent_service_read_timeout_seconds,
    )


__all__ = [
    "AgentServiceArtifact",
    "AgentServiceClient",
    "AgentServiceQuestion",
    "AgentServiceQuestionOption",
    "AgentServiceRequestError",
    "AgentServiceToolStep",
    "AgentServiceToolStepDetails",
    "AgentServiceToolStepValidationIssue",
    "AgentServiceTurnState",
    "get_agent_service_client",
]
