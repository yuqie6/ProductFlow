from __future__ import annotations

import threading

from itsdangerous import TimestampSigner
from starlette.middleware.sessions import SessionMiddleware

SESSION_TIMESTAMP_ROLLBACK_TOLERANCE_SECONDS = 5


class MonotonicTimestampSigner(TimestampSigner):
    """墙钟短暂回拨时，仍保持 session 时间戳校验稳定。"""

    def __init__(self, *args, **kwargs) -> None:
        super().__init__(*args, **kwargs)
        self._timestamp_lock = threading.Lock()
        self._last_timestamp = 0

    def get_timestamp(self) -> int:
        current_timestamp = super().get_timestamp()
        with self._timestamp_lock:
            if self._last_timestamp <= current_timestamp:
                self._last_timestamp = current_timestamp
            elif self._last_timestamp - current_timestamp > SESSION_TIMESTAMP_ROLLBACK_TOLERANCE_SECONDS:
                self._last_timestamp = current_timestamp
            return self._last_timestamp


class ClockStableSessionMiddleware(SessionMiddleware):
    """带进程内单调时间戳签名的 Starlette session 中间件。"""

    def __init__(self, *args, **kwargs) -> None:
        super().__init__(*args, **kwargs)
        self.signer = MonotonicTimestampSigner(self.signer.secret_key)
