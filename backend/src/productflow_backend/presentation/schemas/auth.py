from __future__ import annotations

from pydantic import BaseModel, Field


class SessionCreateRequest(BaseModel):
    admin_key: str = Field(default="")


class SessionResponse(BaseModel):
    ok: bool = True


class SessionStateResponse(BaseModel):
    authenticated: bool
    access_required: bool
