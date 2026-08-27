/**
 * 局部编辑任务解析。unknown 表示无法证明供应商副作用，不是 failed 的别名。
 */

import type {
  LocalImageEditAdoptionEvent,
  LocalImageEditCapability,
  LocalImageEditMaskGeometry,
  LocalImageEditOperation,
  LocalImageEditProviderAttempt,
  LocalImageEditTask,
  LocalImageEditTaskListResponse,
  LocalImageEditTaskStatus,
  ProductImageAsset,
} from "./types";

const OPERATIONS = new Set<LocalImageEditOperation>(["remove", "replace_text", "inpaint"]);
const STATUSES = new Set<LocalImageEditTaskStatus>([
  "draft",
  "queued",
  "running",
  "succeeded",
  "failed",
  "cancelled",
  "unknown",
]);
const ORIGINS = new Set<ProductImageAsset["origin_type"]>([
  "upload",
  "workflow_generation",
  "image_session_attach",
  "local_edit",
]);
const VERIFICATION_STATUSES = new Set<ProductImageAsset["verification_status"]>([
  "verified",
  "legacy_pending",
  "missing",
]);
const CAPABILITY_KEYS = new Set([
  "provider_name",
  "supported",
  "mode",
  "operations",
  "requires_mask",
  "max_reference_images",
  "reason",
]);
const ASSET_KEYS = new Set([
  "id",
  "product_id",
  "media_object_id",
  "origin_type",
  "display_name",
  "original_filename",
  "image_type_key",
  "user_folder_id",
  "parent_asset_id",
  "source_image_session_asset_id",
  "source_library_asset_id",
  "mime_type",
  "byte_size",
  "width",
  "height",
  "verification_status",
  "download_url",
  "preview_url",
  "thumbnail_url",
  "created_at",
  "updated_at",
]);
const GEOMETRY_KEYS = new Set([
  "source_width",
  "source_height",
  "viewport_width",
  "viewport_height",
  "viewport_to_source",
  "transform_direction",
]);
const ATTEMPT_KEYS = new Set([
  "id",
  "attempt_id",
  "attempt_number",
  "operation_key",
  "phase",
  "effect_result",
  "provider_name",
  "provider_model",
  "provider_response_id",
  "provider_status",
  "late_result_asset",
  "detail",
  "created_at",
  "updated_at",
]);
const ADOPTION_EVENT_KEYS = new Set([
  "id",
  "task_id",
  "graph_id",
  "node_id",
  "event_type",
  "from_artifact_id",
  "to_artifact_id",
  "related_event_id",
  "created_at",
]);
const TASK_KEYS = new Set([
  "id",
  "product_id",
  "status",
  "revision",
  "operation",
  "instruction",
  "source_text",
  "replacement_text",
  "mask_geometry",
  "source_media_sha256",
  "source_asset",
  "result_asset",
  "references",
  "reference_asset_ids",
  "target_graph_id",
  "target_node_id",
  "target_graph_revision",
  "source_artifact_id",
  "source_artifact_asset_id",
  "source_artifact_input_digest",
  "idempotency_key",
  "request_hash",
  "requested_provider_name",
  "requested_local_edit_mode",
  "attempts",
  "active_attempt_id",
  "progress_phase",
  "failure_reason",
  "is_retryable",
  "is_cancelable",
  "provider_name",
  "provider_model",
  "provider_response_id",
  "provider_status",
  "provider_attempts",
  "adoption_events",
  "created_at",
  "updated_at",
  "queued_at",
  "started_at",
  "finished_at",
]);

export function isLocalImageEditOperation(value: unknown): value is LocalImageEditOperation {
  return typeof value === "string" && OPERATIONS.has(value as LocalImageEditOperation);
}

export function isLocalImageEditTaskStatus(value: unknown): value is LocalImageEditTaskStatus {
  return typeof value === "string" && STATUSES.has(value as LocalImageEditTaskStatus);
}

export function parseLocalImageEditCapability(value: unknown): LocalImageEditCapability | null {
  if (!isRecord(value) || !hasExactKeys(value, CAPABILITY_KEYS)) return null;
  if (
    typeof value.provider_name !== "string"
    || !value.provider_name.trim()
    || typeof value.supported !== "boolean"
    || (value.mode !== null && typeof value.mode !== "string")
    || !Array.isArray(value.operations)
    || value.operations.some((operation) => !isLocalImageEditOperation(operation))
    || new Set(value.operations).size !== value.operations.length
    || typeof value.requires_mask !== "boolean"
    || !isNonNegativeInteger(value.max_reference_images)
    || (value.reason !== null && typeof value.reason !== "string")
  ) {
    return null;
  }
  return {
    provider_name: value.provider_name,
    supported: value.supported,
    mode: value.mode as string | null,
    operations: value.operations as LocalImageEditOperation[],
    requires_mask: value.requires_mask,
    max_reference_images: value.max_reference_images,
    reason: value.reason as string | null,
  };
}

export function parseLocalImageEditTaskList(value: unknown): LocalImageEditTaskListResponse | null {
  if (!isRecord(value) || !hasExactKeys(value, new Set(["items"]))) return null;
  if (!Array.isArray(value.items)) return null;
  const items = value.items.map(parseLocalImageEditTask);
  if (items.some((item): item is null => item === null)) return null;
  return { items: items as LocalImageEditTask[] };
}

export function parseLocalImageEditTask(value: unknown): LocalImageEditTask | null {
  if (!isRecord(value) || !hasExactKeys(value, TASK_KEYS)) return null;
  const sourceAsset = parseProductImageAsset(value.source_asset);
  const resultAsset = value.result_asset === null ? null : parseProductImageAsset(value.result_asset);
  const references = Array.isArray(value.references)
    ? value.references.map(parseProductImageAsset)
    : null;
  const providerAttempts = Array.isArray(value.provider_attempts)
    ? value.provider_attempts.map(parseProviderAttempt)
    : null;
  const adoptionEvents = Array.isArray(value.adoption_events)
    ? value.adoption_events.map(parseAdoptionEvent)
    : null;
  const referenceAssetIds = Array.isArray(value.reference_asset_ids)
    && value.reference_asset_ids.every((item): item is string => typeof item === "string" && Boolean(item.trim()))
    ? value.reference_asset_ids
    : null;
  if (
    typeof value.id !== "string"
    || !value.id.trim()
    || typeof value.product_id !== "string"
    || !value.product_id.trim()
    || !isLocalImageEditTaskStatus(value.status)
    || !isNonNegativeInteger(value.revision)
    || !isLocalImageEditOperation(value.operation)
    || !isNullableString(value.instruction)
    || !isNullableString(value.source_text)
    || !isNullableString(value.replacement_text)
    || !parseLocalImageEditMaskGeometry(value.mask_geometry)
    || typeof value.source_media_sha256 !== "string"
    || !value.source_media_sha256.trim()
    || !sourceAsset
    || (value.result_asset !== null && !resultAsset)
    || !references
    || references.some((asset): asset is null => asset === null)
    || !referenceAssetIds
    || referenceAssetIds.length !== new Set(referenceAssetIds).size
    || !providerAttempts
    || providerAttempts.some((attempt): attempt is null => attempt === null)
    || !adoptionEvents
    || adoptionEvents.some((event): event is null => event === null)
    || !isNullableString(value.target_graph_id)
    || !isNullableString(value.target_node_id)
    || !isNullablePositiveInteger(value.target_graph_revision)
    || !isNullableString(value.source_artifact_id)
    || !isNullableString(value.source_artifact_asset_id)
    || !isNullableString(value.source_artifact_input_digest)
    || !isNullableString(value.idempotency_key)
    || !isNullableString(value.request_hash)
    || !isNullableString(value.requested_provider_name)
    || !isNullableString(value.requested_local_edit_mode)
    || !isNonNegativeInteger(value.attempts)
    || !isNullableString(value.active_attempt_id)
    || !isNullableString(value.progress_phase)
    || !isNullableString(value.failure_reason)
    || typeof value.is_retryable !== "boolean"
    || typeof value.is_cancelable !== "boolean"
    || !isNullableString(value.provider_name)
    || !isNullableString(value.provider_model)
    || !isNullableString(value.provider_response_id)
    || !isNullableString(value.provider_status)
    || !isTimestamp(value.created_at)
    || !isTimestamp(value.updated_at)
    || !isNullableTimestamp(value.queued_at)
    || !isNullableTimestamp(value.started_at)
    || !isNullableTimestamp(value.finished_at)
  ) {
    return null;
  }
  const maskGeometry = parseLocalImageEditMaskGeometry(value.mask_geometry);
  if (!maskGeometry) return null;
  return {
    id: value.id,
    product_id: value.product_id,
    status: value.status,
    revision: value.revision,
    operation: value.operation,
    instruction: value.instruction as string | null,
    source_text: value.source_text as string | null,
    replacement_text: value.replacement_text as string | null,
    mask_geometry: maskGeometry,
    source_media_sha256: value.source_media_sha256,
    source_asset: sourceAsset,
    result_asset: resultAsset,
    references: references as ProductImageAsset[],
    reference_asset_ids: referenceAssetIds,
    target_graph_id: value.target_graph_id as string | null,
    target_node_id: value.target_node_id as string | null,
    target_graph_revision: value.target_graph_revision as number | null,
    source_artifact_id: value.source_artifact_id as string | null,
    source_artifact_asset_id: value.source_artifact_asset_id as string | null,
    source_artifact_input_digest: value.source_artifact_input_digest as string | null,
    idempotency_key: value.idempotency_key as string | null,
    request_hash: value.request_hash as string | null,
    requested_provider_name: value.requested_provider_name as string | null,
    requested_local_edit_mode: value.requested_local_edit_mode as string | null,
    attempts: value.attempts,
    active_attempt_id: value.active_attempt_id as string | null,
    progress_phase: value.progress_phase as string | null,
    failure_reason: value.failure_reason as string | null,
    is_retryable: value.is_retryable,
    is_cancelable: value.is_cancelable,
    provider_name: value.provider_name as string | null,
    provider_model: value.provider_model as string | null,
    provider_response_id: value.provider_response_id as string | null,
    provider_status: value.provider_status as string | null,
    provider_attempts: providerAttempts as LocalImageEditProviderAttempt[],
    adoption_events: adoptionEvents as LocalImageEditAdoptionEvent[],
    created_at: value.created_at,
    updated_at: value.updated_at,
    queued_at: value.queued_at as string | null,
    started_at: value.started_at as string | null,
    finished_at: value.finished_at as string | null,
  };
}

export function parseLocalImageEditMaskGeometry(value: unknown): LocalImageEditMaskGeometry | null {
  if (!isRecord(value) || !hasExactKeys(value, GEOMETRY_KEYS)) return null;
  const transform = value.viewport_to_source;
  if (
    !isPositiveInteger(value.source_width)
    || !isPositiveInteger(value.source_height)
    || !isFinitePositiveNumber(value.viewport_width)
    || !isFinitePositiveNumber(value.viewport_height)
    || !Array.isArray(transform)
    || transform.length !== 6
    || transform.some((item) => typeof item !== "number" || !Number.isFinite(item))
    || value.transform_direction !== "viewport_to_source"
  ) {
    return null;
  }
  const [a, b, c, d, e, f] = transform;
  const determinant = a * d - b * c;
  if (!Number.isFinite(determinant) || Math.abs(determinant) <= 1e-12) return null;
  return {
    source_width: value.source_width,
    source_height: value.source_height,
    viewport_width: value.viewport_width,
    viewport_height: value.viewport_height,
    viewport_to_source: [a, b, c, d, e, f],
    transform_direction: "viewport_to_source",
  };
}

function parseProductImageAsset(value: unknown): ProductImageAsset | null {
  if (!isRecord(value) || !hasExactKeys(value, ASSET_KEYS)) return null;
  if (
    !isNonEmptyString(value.id)
    || !isNonEmptyString(value.product_id)
    || !isNonEmptyString(value.media_object_id)
    || typeof value.origin_type !== "string"
    || !ORIGINS.has(value.origin_type as ProductImageAsset["origin_type"])
    || !isString(value.display_name)
    || !isString(value.original_filename)
    || !isNullableString(value.image_type_key)
    || !isNullableString(value.user_folder_id)
    || !isNullableString(value.parent_asset_id)
    || !isNullableString(value.source_image_session_asset_id)
    || !isNullableString(value.source_library_asset_id)
    || !isNonEmptyString(value.mime_type)
    || !isNullableNonNegativeInteger(value.byte_size)
    || !isNullablePositiveInteger(value.width)
    || !isNullablePositiveInteger(value.height)
    || typeof value.verification_status !== "string"
    || !VERIFICATION_STATUSES.has(value.verification_status as ProductImageAsset["verification_status"])
    || !isNonEmptyString(value.download_url)
    || !isNonEmptyString(value.preview_url)
    || !isNonEmptyString(value.thumbnail_url)
    || !isTimestamp(value.created_at)
    || !isTimestamp(value.updated_at)
  ) {
    return null;
  }
  return value as unknown as ProductImageAsset;
}

function parseProviderAttempt(value: unknown): LocalImageEditProviderAttempt | null {
  if (!isRecord(value) || !hasExactKeys(value, ATTEMPT_KEYS)) return null;
  const lateResultAsset = value.late_result_asset === null ? null : parseProductImageAsset(value.late_result_asset);
  if (
    !isNonEmptyString(value.id)
    || !isNonEmptyString(value.attempt_id)
    || !isPositiveInteger(value.attempt_number)
    || !isNonEmptyString(value.operation_key)
    || !isNonEmptyString(value.phase)
    || !isNonEmptyString(value.effect_result)
    || !isNonEmptyString(value.provider_name)
    || !isNullableString(value.provider_model)
    || !isNullableString(value.provider_response_id)
    || !isNullableString(value.provider_status)
    || (value.late_result_asset !== null && !lateResultAsset)
    || !isNullableString(value.detail)
    || !isTimestamp(value.created_at)
    || !isTimestamp(value.updated_at)
  ) {
    return null;
  }
  return {
    id: value.id,
    attempt_id: value.attempt_id,
    attempt_number: value.attempt_number,
    operation_key: value.operation_key,
    phase: value.phase,
    effect_result: value.effect_result,
    provider_name: value.provider_name,
    provider_model: value.provider_model as string | null,
    provider_response_id: value.provider_response_id as string | null,
    provider_status: value.provider_status as string | null,
    late_result_asset: lateResultAsset,
    detail: value.detail as string | null,
    created_at: value.created_at,
    updated_at: value.updated_at,
  };
}

function parseAdoptionEvent(value: unknown): LocalImageEditAdoptionEvent | null {
  if (!isRecord(value) || !hasExactKeys(value, ADOPTION_EVENT_KEYS)) return null;
  if (
    !isNonEmptyString(value.id)
    || !isNonEmptyString(value.task_id)
    || !isNonEmptyString(value.graph_id)
    || !isNonEmptyString(value.node_id)
    || (value.event_type !== "adopt" && value.event_type !== "revert")
    || !isNonEmptyString(value.from_artifact_id)
    || !isNonEmptyString(value.to_artifact_id)
    || !isNullableString(value.related_event_id)
    || !isTimestamp(value.created_at)
  ) {
    return null;
  }
  return {
    id: value.id,
    task_id: value.task_id,
    graph_id: value.graph_id,
    node_id: value.node_id,
    event_type: value.event_type,
    from_artifact_id: value.from_artifact_id,
    to_artifact_id: value.to_artifact_id,
    related_event_id: value.related_event_id as string | null,
    created_at: value.created_at,
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function hasExactKeys(value: Record<string, unknown>, allowed: Set<string>): boolean {
  const keys = Object.keys(value);
  return keys.length === allowed.size && keys.every((key) => allowed.has(key));
}

function isString(value: unknown): value is string {
  return typeof value === "string";
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === "string" && Boolean(value.trim());
}

function isNullableString(value: unknown): value is string | null {
  return value === null || typeof value === "string";
}

function isPositiveInteger(value: unknown): value is number {
  return typeof value === "number" && Number.isInteger(value) && value > 0;
}

function isNonNegativeInteger(value: unknown): value is number {
  return typeof value === "number" && Number.isInteger(value) && value >= 0;
}

function isNullablePositiveInteger(value: unknown): value is number | null {
  return value === null || isPositiveInteger(value);
}

function isNullableNonNegativeInteger(value: unknown): value is number | null {
  return value === null || isNonNegativeInteger(value);
}

function isFinitePositiveNumber(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && value > 0;
}

function isTimestamp(value: unknown): value is string {
  return isNonEmptyString(value) && Number.isFinite(Date.parse(value));
}

function isNullableTimestamp(value: unknown): value is string | null {
  return value === null || isTimestamp(value);
}
