from __future__ import annotations

from collections import Counter, defaultdict
from collections.abc import Iterable, Mapping
from typing import Any


def group_rows(rows: Iterable[dict[str, Any]], key: str) -> dict[str, list[dict[str, Any]]]:
    grouped: dict[str, list[dict[str, Any]]] = defaultdict(list)
    for row in rows:
        value = optional_id(row, key)
        if value is not None:
            grouped[value].append(row)
    return dict(grouped)


def row_index(rows: Iterable[dict[str, Any]]) -> dict[str, dict[str, Any]]:
    return {required_id(row, "id"): row for row in rows}


def count_field(rows: Iterable[dict[str, Any]], key: str) -> dict[str, int]:
    counts = Counter(status_key(row.get(key)) for row in rows)
    return dict(sorted(counts.items()))


def status_key(value: Any) -> str:
    if value is None:
        return "<null>"
    return str(getattr(value, "value", value))


def required_id(row: Mapping[str, Any], key: str) -> str:
    value = optional_id(row, key)
    return value if value is not None else "<null>"


def optional_id(row: Mapping[str, Any], key: str) -> str | None:
    value = row.get(key)
    return None if value is None else str(value)


def optional_int(value: Any) -> int | None:
    return None if value is None else int(value)


def as_bool(value: Any) -> bool:
    if isinstance(value, str):
        return value.strip().lower() in {"1", "true", "yes", "on"}
    return bool(value)


__all__ = [
    "as_bool",
    "count_field",
    "group_rows",
    "optional_id",
    "optional_int",
    "required_id",
    "row_index",
    "status_key",
]
