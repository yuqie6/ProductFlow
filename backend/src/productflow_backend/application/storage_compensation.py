from __future__ import annotations

import logging
from collections.abc import Callable, Iterator
from contextlib import contextmanager

from sqlalchemy.orm import Session

from productflow_backend.infrastructure.storage import LocalStorage

logger = logging.getLogger(__name__)


class StorageWriteCompensation:
    """Tracks files created by one database mutation and removes them on rollback."""

    def __init__(self) -> None:
        self._writes: list[tuple[LocalStorage, str]] = []

    def track(self, storage: LocalStorage, relative_path: str) -> str:
        self._writes.append((storage, relative_path))
        return relative_path

    def release(self) -> None:
        """Forget files after their owning database transaction commits."""

        self._writes = []

    def cleanup(self) -> None:
        writes = reversed(self._writes)
        self._writes = []
        for storage, relative_path in writes:
            try:
                storage.delete_image_with_variants(relative_path)
            except (OSError, ValueError):
                logger.exception("存储写入补偿清理失败: path=%s", relative_path)


@contextmanager
def compensate_storage_writes(session: Session) -> Iterator[StorageWriteCompensation]:
    compensation = StorageWriteCompensation()
    try:
        yield compensation
    except BaseException:
        session.rollback()
        compensation.cleanup()
        raise


def best_effort_storage_delete(delete: Callable[[], None], *, target: str) -> None:
    try:
        delete()
    except (OSError, ValueError):
        logger.exception("数据库记录已提交，但存储清理失败: target=%s", target)
