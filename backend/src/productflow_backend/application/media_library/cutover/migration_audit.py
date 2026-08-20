from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime


@dataclass(frozen=True, slots=True)
class MediaLibraryMigrationAudit:
    snapshot_token: str
    gallery_count: int
    session_asset_count: int
    captured_at: datetime
    source_hash: str
    source_rows: tuple[tuple[str, str | None, str | None], ...] = ()
    database_snapshot_token: str | None = None
    storage_snapshot_id: str | None = None
    blocker_report: tuple[tuple[str, str], ...] = ()
