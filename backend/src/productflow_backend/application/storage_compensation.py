from __future__ import annotations

import logging
from collections.abc import Callable, Iterator
from contextlib import contextmanager

from sqlalchemy.orm import Session

from productflow_backend.infrastructure.storage import LocalStorage

logger = logging.getLogger(__name__)


class StorageWriteCompensation:
    """跟踪一次 DB 变更创建的文件；回滚时只删本次写入。

    不删除事先存在的共享媒体。commit 成功后若仍可能抛错，必须先 release()。
    """

    def __init__(self) -> None:
        self._writes: list[tuple[LocalStorage, str]] = []

    def track(self, storage: LocalStorage, relative_path: str) -> str:
        """登记本次创建的相对路径，供失败补偿使用。"""

        self._writes.append((storage, relative_path))
        return relative_path

    def release(self) -> None:
        """所属数据库事务提交后再忘记这些文件。

        commit 之后必须调用，否则后续异常的 cleanup 会误删已入账文件。
        """

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
    """调用方负责 commit。异常时先 rollback DB，再删本次文件。正常退出不删文件。"""

    compensation = StorageWriteCompensation()
    try:
        yield compensation
    except BaseException:
        session.rollback()
        compensation.cleanup()
        raise


def best_effort_storage_delete(delete: Callable[[], None], *, target: str) -> None:
    """仅用于 DB 已提交后的文件清理；失败只记日志，不回滚业务行。"""
    try:
        delete()
    except (OSError, ValueError):
        logger.exception("数据库记录已提交，但存储清理失败: target=%s", target)
