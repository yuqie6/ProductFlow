from __future__ import annotations

import json
from collections.abc import AsyncIterator
from datetime import datetime
from functools import lru_cache
from typing import Any
from urllib.parse import quote, urlsplit

import httpx
from pydantic import BaseModel, ConfigDict, Field, ValidationError

from productflow_backend.config import get_settings
from productflow_backend.domain.enums import AgentTurnStatus


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


class AgentServiceTurnState(BaseModel):
    model_config = ConfigDict(extra="ignore")

    api_version: str
    run_id: str
    turn_id: str
    status: AgentTurnStatus
    question: AgentServiceQuestion | None = None
    artifact: AgentServiceArtifact | None = None
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
        input_text: str,
        asset_ids: list[str],
        idempotency_key: str,
    ) -> AgentServiceTurnState:
        return self._request_state(
            "POST",
            self._conversation_path(conversation_id) + "/turns",
            json_body={
                "input_text": input_text,
                "asset_ids": asset_ids,
                "idempotency_key": idempotency_key,
            },
        )

    def get_turn(self, *, conversation_id: str, turn_id: str) -> AgentServiceTurnState:
        return self._request_state(
            "GET",
            self._turn_path(conversation_id, turn_id),
        )

    def cancel_turn(self, *, conversation_id: str, turn_id: str) -> AgentServiceTurnState:
        return self._request_state(
            "POST",
            self._turn_path(conversation_id, turn_id) + "/cancel",
            json_body={},
        )

    def resume_turn(self, *, conversation_id: str, turn_id: str) -> AgentServiceTurnState:
        return self._request_state(
            "POST",
            self._turn_path(conversation_id, turn_id) + "/resume",
            json_body={},
        )

    def answer_question(
        self,
        *,
        conversation_id: str,
        turn_id: str,
        question_id: str,
        answer: dict[str, Any],
    ) -> AgentServiceTurnState:
        return self._request_state(
            "POST",
            self._turn_path(conversation_id, turn_id)
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
    ) -> AsyncIterator[bytes]:
        headers = self._headers()
        params = {"after": str(after)}
        try:
            async with httpx.AsyncClient(timeout=None) as client:
                async with client.stream(
                    "GET",
                    self.base_url + self._turn_path(conversation_id, turn_id) + "/events",
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
    def _turn_path(cls, conversation_id: str, turn_id: str) -> str:
        return cls._conversation_path(conversation_id) + f"/turns/{quote(turn_id, safe='')}"


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
    "AgentServiceTurnState",
    "get_agent_service_client",
]
