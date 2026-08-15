from __future__ import annotations

from collections.abc import Iterator
from contextlib import contextmanager

from sqlalchemy.engine import Connection, Engine


@contextmanager
def open_legacy_read_only_connection(engine: Engine) -> Iterator[Connection]:
    """Open a source inspection connection that cannot modify PostgreSQL or SQLite."""

    dialect = engine.dialect.name
    if dialect not in {"postgresql", "sqlite"}:
        raise RuntimeError(f"遗留资产只读读取暂不支持数据库方言: {dialect}")

    with engine.connect() as connection:
        if dialect == "postgresql":
            transaction = connection.begin()
            try:
                connection.exec_driver_sql("SET TRANSACTION READ ONLY")
                if connection.exec_driver_sql("SHOW transaction_read_only").scalar_one() != "on":
                    raise RuntimeError("PostgreSQL 只读事务未生效")
                yield connection
            finally:
                if transaction.is_active:
                    transaction.rollback()
            return

        original_query_only = int(connection.exec_driver_sql("PRAGMA query_only").scalar_one())
        connection.rollback()
        try:
            connection.exec_driver_sql("PRAGMA query_only = ON")
            if int(connection.exec_driver_sql("PRAGMA query_only").scalar_one()) != 1:
                raise RuntimeError("SQLite query_only 未生效")
            yield connection
        finally:
            connection.rollback()
            connection.exec_driver_sql(f"PRAGMA query_only = {original_query_only}")
            connection.rollback()


__all__ = ["open_legacy_read_only_connection"]
