/**
 * 模型可见的工具结果信封与归约式截断。
 *
 * 超限时保留最高信号切片，并在 guidance 里写明如何缩小范围。
 * details 走清单声明的 result_meta_schema，供 UI 卡片与日志回放使用。
 * load_productflow_skill 例外：正文仍是原始 Markdown，meta 走同一 schema。
 */

import type { AgentToolResult } from "@earendil-works/pi-coding-agent";
import type { ImageContent } from "@earendil-works/pi-ai/compat";
import { type JsonObject } from "./contracts.js";
import {
  toolManifestEntry,
  validateToolResultMeta,
  type ToolManifestEntry,
  type ToolResultReducer,
} from "./tool-manifest.js";

const LIST_PREVIEW_COUNT = 10;

export interface ToolResultEnvelope {
  schema_version: 1;
  data: unknown;
  guidance?: string;
}

export interface ToolResultMeta extends JsonObject {
  schema_version: 1;
  kind: string;
}

export function encodeToolResult(
  name: string,
  data: unknown,
  extra: JsonObject = {},
  extras?: { images?: ImageContent[] },
): AgentToolResult<JsonObject> {
  const entry = requireEntry(name);
  const reduced = reduceForEntry(entry, data);
  const envelope: ToolResultEnvelope = {
    schema_version: 1,
    data: reduced.data,
    ...(reduced.guidance ? { guidance: reduced.guidance } : {}),
  };
  const meta: ToolResultMeta = {
    schema_version: 1,
    kind: entry.ui_kind,
    ...extra,
    ...(reduced.truncated
      ? {
        truncated: true,
        original_bytes: reduced.originalBytes,
        max_bytes: entry.truncation_bytes,
      }
      : {}),
  };
  validateToolResultMeta(name, meta);
  const text: { type: "text"; text: string } = { type: "text", text: encodeJSON(envelope) };
  if (extras?.images?.length) {
    return { content: [text, ...extras.images], details: meta };
  }
  return { content: [text], details: meta };
}

export function encodeSkillResult(
  name: string,
  text: string,
  extra: JsonObject,
): AgentToolResult<JsonObject> {
  const entry = requireEntry("load_productflow_skill");
  const meta: ToolResultMeta = {
    schema_version: 1,
    kind: entry.ui_kind,
    ...extra,
  };
  validateToolResultMeta("load_productflow_skill", meta);
  void name;
  return { content: [{ type: "text", text }], details: meta };
}

function requireEntry(name: string): ToolManifestEntry {
  const entry = toolManifestEntry(name);
  if (!entry) throw new Error(`Unknown ProductFlow tool manifest entry: ${name}`);
  return entry;
}

interface ReducedValue {
  data: unknown;
  guidance?: string;
  truncated: boolean;
  originalBytes: number;
}

function reduceForEntry(entry: ToolManifestEntry, value: unknown): ReducedValue {
  const encoded = encodeJSON(value);
  const originalBytes = Buffer.byteLength(encoded, "utf8");
  if (originalBytes <= entry.truncation_bytes) {
    return { data: value, truncated: false, originalBytes };
  }
  const reduced = applyReducer(entry.result_reducer, value, originalBytes, entry.truncation_bytes);
  const reducedEncoded = encodeJSON(reduced.data);
  if (Buffer.byteLength(reducedEncoded, "utf8") <= entry.truncation_bytes) {
    return { ...reduced, truncated: true, originalBytes };
  }
  return {
    data: fallbackSlice(value, entry.result_reducer),
    guidance: truncationGuidance(entry.result_reducer),
    truncated: true,
    originalBytes,
  };
}

function applyReducer(kind: ToolResultReducer, value: unknown, originalBytes: number, maxBytes: number): Omit<ReducedValue, "truncated" | "originalBytes"> {
  void originalBytes;
  void maxBytes;
  switch (kind) {
    case "list":
      return reduceList(value);
    case "context":
      return reduceContext(value);
    case "detail":
      return reduceDetail(value);
    default:
      return {
        data: { preview: jsonPreview(value, 2_048) },
        guidance: truncationGuidance(kind),
      };
  }
}

function reduceList(value: unknown): Omit<ReducedValue, "truncated" | "originalBytes"> {
  const items = listItems(value).map((item) => compactListItem(item, 2_048));
  const preview = items.slice(0, LIST_PREVIEW_COUNT);
  const guidance = truncationGuidance("list");
  const total = listItems(value).length;
  if (Array.isArray(value)) {
    return { data: { items: preview, returned_count: preview.length, total_count: total }, guidance };
  }
  if (isRecord(value)) {
    const rest = { ...value };
    delete rest.items;
    delete rest.runs;
    return {
      data: {
        ...rest,
        items: preview,
        returned_count: preview.length,
        total_count: total,
      },
      guidance,
    };
  }
  return { data: { items: preview, returned_count: preview.length, total_count: total }, guidance };
}

function reduceContext(value: unknown): Omit<ReducedValue, "truncated" | "originalBytes"> {
  const record = isRecord(value) ? value : {};
  const live = isRecord(record.live_graph) ? record.live_graph : undefined;
  const data = {
    schema_version: typeof record.schema_version === "number" ? record.schema_version : 1,
    product: compactProduct(record.product),
    intake: record.intake ?? null,
    birth_expandable: record.birth_expandable ?? null,
    live_graph: live
      ? {
        id: live.id ?? null,
        title: live.title ?? null,
        revision: live.revision ?? null,
        node_count: countOrLength(live, "node_count", "nodes"),
        edge_count: countOrLength(live, "edge_count", "edges"),
        group_count: countOrLength(live, "group_count", "groups"),
      }
      : null,
    node_catalog: record.node_catalog,
    image_type_catalog: compactImageTypeCatalog(record.image_type_catalog),
    target: record.target,
  };
  return {
    data,
    guidance: truncationGuidance("context"),
  };
}

function reduceDetail(value: unknown): Omit<ReducedValue, "truncated" | "originalBytes"> {
  if (!isRecord(value)) {
    return { data: { preview: jsonPreview(value, 2_048) }, guidance: truncationGuidance("detail") };
  }
  const data: Record<string, unknown> = {};
  for (const [key, item] of Object.entries(value)) {
    const encoded = encodeJSON(item);
    if (Buffer.byteLength(encoded, "utf8") > 4_096) {
      data[key] = { omitted: true, bytes: Buffer.byteLength(encoded, "utf8") };
      continue;
    }
    data[key] = item;
  }
  return { data, guidance: truncationGuidance("detail") };
}

function fallbackSlice(value: unknown, kind: ToolResultReducer): unknown {
  if (kind === "context" && isRecord(value) && value.node_catalog !== undefined) {
    return {
      schema_version: typeof value.schema_version === "number" ? value.schema_version : 1,
      node_catalog: value.node_catalog,
    };
  }
  if (kind === "list") {
    const all = listItems(value);
    const items = all.slice(0, 3).map((item) => compactListItem(item, 256));
    return { items, returned_count: items.length, total_count: all.length };
  }
  return { preview: jsonPreview(value, 1_024) };
}

function truncationGuidance(kind: ToolResultReducer): string {
  switch (kind) {
    case "list":
      return "结果已截断。使用 query 和该工具的分页参数（after 或 cursor）以及更小的 limit 缩小范围后再读。";
    case "context":
      return "结果已截断。默认使用 response_format=concise；需要完整 config_fields 时再请求 detailed，或用 get_node_detail_v1 读取单个节点。";
    case "detail":
      return "结果已截断。缩小查询范围，或改用列表摘要后再请求单条详情。";
    default:
      return "结果已截断。缩小参数范围后重试。";
  }
}

function listItems(value: unknown): unknown[] {
  if (Array.isArray(value)) return value;
  if (isRecord(value) && Array.isArray(value.items)) return value.items;
  if (isRecord(value) && Array.isArray(value.runs)) return value.runs;
  return [];
}

function countOrLength(record: Record<string, unknown>, countKey: string, arrayKey: string): number {
  const counted = record[countKey];
  if (typeof counted === "number" && Number.isFinite(counted) && counted >= 0) return counted;
  const items = record[arrayKey];
  return Array.isArray(items) ? items.length : 0;
}

function compactProduct(value: unknown): unknown {
  if (!isRecord(value)) return value ?? null;
  const note = typeof value.source_note === "string" ? value.source_note.slice(0, 500) : value.source_note;
  return {
    id: value.id ?? null,
    name: value.name ?? null,
    category: value.category ?? null,
    price: value.price ?? null,
    source_note: note ?? null,
  };
}

function compactImageTypeCatalog(value: unknown): unknown {
  if (!Array.isArray(value)) return value ?? null;
  return value.flatMap((item) => {
    if (!isRecord(item) || typeof item.key !== "string") return [];
    return [{ key: item.key, title: item.title ?? null }];
  });
}

function compactListItem(item: unknown, maxBytes: number): unknown {
  const encoded = encodeJSON(item);
  if (Buffer.byteLength(encoded, "utf8") <= maxBytes) return item;
  if (typeof item === "string") return `${item.slice(0, 200)}…`;
  return { omitted: true, bytes: Buffer.byteLength(encoded, "utf8"), preview: jsonPreview(item, 200) };
}

function jsonPreview(value: unknown, maximum: number): string {
  const encoded = encodeJSON(value);
  if (encoded.length <= maximum) return encoded;
  return `${encoded.slice(0, maximum)}…`;
}

function encodeJSON(value: unknown): string {
  try {
    return JSON.stringify(value) ?? "null";
  } catch {
    return JSON.stringify({ schema_version: 1, error: "unserializable ProductFlow result" });
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}
