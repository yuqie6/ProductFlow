import { sanitizeFilenamePart } from "../../lib/image-downloads";
import type {
  LegacyArchiveKind,
  LegacyArchiveListItem,
  LegacyArchivePage,
  MediaVerificationStatus,
} from "../../lib/types";

export const LEGACY_ARCHIVE_KINDS = [
  "workflow",
  "canvas_agent_thread",
  "user_template",
] as const satisfies readonly LegacyArchiveKind[];

export function isLegacyArchiveKind(value: string | null | undefined): value is LegacyArchiveKind {
  return LEGACY_ARCHIVE_KINDS.includes(value as LegacyArchiveKind);
}

export function flattenLegacyArchivePages(pages: LegacyArchivePage[] | undefined): LegacyArchiveListItem[] {
  return pages?.flatMap((page) => page.items) ?? [];
}

export function isLegacyArchiveMediaAvailable(status: MediaVerificationStatus): boolean {
  return status === "verified";
}

export function legacyArchiveDetailPath(
  kind: LegacyArchiveKind,
  archiveId: string,
  searchParams: URLSearchParams,
): string {
  const query = searchParams.toString();
  const path = `/history/${encodeURIComponent(kind)}/${encodeURIComponent(archiveId)}`;
  return query ? `${path}?${query}` : path;
}

export function legacyArchiveExportFilename(item: LegacyArchiveListItem): string {
  const title = sanitizeFilenamePart(item.title, "legacy-archive");
  return `${title}-${item.kind}-${item.id}.json`;
}

export function payloadRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

export function payloadRecords(payload: Record<string, unknown>, key: string): Array<Record<string, unknown>> {
  const value = payload[key];
  return Array.isArray(value)
    ? value.map(payloadRecord).filter((item): item is Record<string, unknown> => Boolean(item))
    : [];
}

export function recordText(record: Record<string, unknown>, key: string): string | null {
  const value = record[key];
  if (typeof value === "string" && value.trim()) {
    return value;
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  return null;
}

export function boundedJson(value: unknown, maxLength = 4_000): string {
  const serialized = JSON.stringify(value, null, 2) ?? "";
  if (serialized.length <= maxLength) {
    return serialized;
  }
  return `${serialized.slice(0, maxLength)}\n...`;
}
