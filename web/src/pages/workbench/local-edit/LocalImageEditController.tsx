import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertCircle, Loader2, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { ApiError, api } from "../../../lib/api";
import { isLocalImageEditOperation } from "../../../lib/localImageEdits";
import { useI18n } from "../../../lib/preferences";
import type {
  GalleryAsset,
  LocalImageEditCreateInput,
  LocalImageEditOperation,
  LocalImageEditTask,
  LocalImageEditTaskStatus,
} from "../../../lib/types";
import {
  LocalImageEditDialog,
  type LocalImageEditCapability as DialogCapability,
  type LocalImageEditLabels,
  type LocalImageEditSubmitPayload,
} from "./LocalImageEditDialog";

export interface LocalImageEditOpenRequest {
  sourceAssetId: string;
  targetNodeId: string | null;
  operation?: LocalImageEditOperation;
}

interface LocalImageEditControllerOptions {
  productId: string;
  graphId?: string | null;
}

interface LoadedSource {
  asset: GalleryAsset;
  url: string;
}

class LocalImageEditSourceError extends Error {
  constructor(readonly code: "invalid_dimensions" | "read_failed" | "empty") {
    super(code);
  }
}

const ACTIVE_TASK_STATUSES = new Set<LocalImageEditTaskStatus>(["queued", "running"]);
const RECOVERABLE_TASK_STATUSES = new Set<LocalImageEditTaskStatus>([
  "draft",
  "queued",
  "running",
  "unknown",
]);

export function selectLatestActiveLocalImageEditTask(
  tasks: readonly LocalImageEditTask[],
  request: LocalImageEditOpenRequest,
): LocalImageEditTask | null {
  return tasks
    .filter((task) => (
      RECOVERABLE_TASK_STATUSES.has(task.status)
      && task.source_asset.id === request.sourceAssetId
      && task.target_node_id === request.targetNodeId
    ))
    .sort(compareTaskFreshness)[0] ?? null;
}

export function latestActiveAdoptionEvent(task: LocalImageEditTask | null) {
  if (!task) return null;
  const latest = [...task.adoption_events].sort(compareEventFreshness)[0] ?? null;
  return latest?.event_type === "adopt" ? latest : null;
}

export function persistedDraftIdempotencyKey(task: Pick<LocalImageEditTask, "id" | "revision">): string {
  return `local-edit-draft-${stableHash(`${task.id}:${task.revision}`)}`.slice(0, 120);
}

export function continuedEditTargetNodeId(
  task: LocalImageEditTask | null,
  requestedTargetNodeId: string | null,
): string | null {
  return latestActiveAdoptionEvent(task) ? requestedTargetNodeId : null;
}

export function shouldPollLocalImageEditTask(status: LocalImageEditTaskStatus | null | undefined): boolean {
  return status !== null && status !== undefined && ACTIVE_TASK_STATUSES.has(status);
}

export function useLocalImageEditController({
  productId,
  graphId = null,
}: LocalImageEditControllerOptions): {
  openLocalImageEdit: (request: LocalImageEditOpenRequest) => void;
  dialog: ReactNode;
} {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [request, setRequest] = useState<LocalImageEditOpenRequest | null>(null);
  const [source, setSource] = useState<LoadedSource | null>(null);
  const [sourceLoading, setSourceLoading] = useState(false);
  const [sourceError, setSourceError] = useState<string | null>(null);
  const [task, setTask] = useState<LocalImageEditTask | null>(null);
  const [commandBusy, setCommandBusy] = useState(false);
  const [commandError, setCommandError] = useState<string | null>(null);
  const idempotencyKeysRef = useRef(new Map<string, string>());
  const currentRequestKeyRef = useRef<string | null>(null);

  const capabilityQuery = useQuery({
    queryKey: ["local-image-edit-capability"],
    queryFn: () => api.getLocalImageEditCapability(),
    staleTime: 60_000,
    retry: false,
  });
  const taskQuery = useQuery({
    queryKey: ["local-image-edit-task", productId, task?.id ?? null],
    queryFn: () => api.getLocalImageEdit(productId, task!.id),
    enabled: Boolean(request && task),
    initialData: task ?? undefined,
    refetchInterval: (query) => {
      const status = query.state.data?.status ?? task?.status;
      return shouldPollLocalImageEditTask(status) ? 1000 : false;
    },
  });

  useEffect(() => {
    if (taskQuery.data) setTask(taskQuery.data);
  }, [taskQuery.data]);

  useEffect(() => {
    if (!request) {
      setSource(null);
      setSourceLoading(false);
      setSourceError(null);
      setTask(null);
      return;
    }
    let cancelled = false;
    setSource(null);
    setSourceLoading(true);
    setSourceError(null);
    setCommandError(null);
    setTask(null);
    void loadVerifiedSource(productId, request.sourceAssetId)
      .then(async (loaded) => {
        if (cancelled) {
          revokeLocalImageEditObjectUrl(loaded.url);
          return;
        }
        setSource(loaded);
        try {
          const list = await api.listLocalImageEdits(productId, 50);
          if (!cancelled) {
            setTask(selectLatestActiveLocalImageEditTask(list.items, request));
          }
        } catch {
          // A failed recovery list must not hide the explicit edit surface.
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) setSourceError(sourceErrorDetail(error, t));
      })
      .finally(() => {
        if (!cancelled) setSourceLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [productId, request, t]);

  useEffect(() => () => {
    if (source?.url) revokeLocalImageEditObjectUrl(source.url);
  }, [source?.url]);

  const openLocalImageEdit = useCallback((next: LocalImageEditOpenRequest) => {
    if (!next.sourceAssetId.trim()) return;
    setRequest({
      sourceAssetId: next.sourceAssetId,
      targetNodeId: next.targetNodeId,
      operation: next.operation ?? "inpaint",
    });
  }, []);

  const close = useCallback(() => {
    setRequest(null);
    setSource(null);
    setTask(null);
    setCommandError(null);
    currentRequestKeyRef.current = null;
  }, []);

  const invalidateTaskViews = useCallback(async (taskId: string, includeGraph: boolean) => {
    const invalidations = [
      queryClient.invalidateQueries({ queryKey: ["local-image-edit-task", productId, taskId] }),
      queryClient.invalidateQueries({ queryKey: ["product-image-library", productId] }),
      queryClient.invalidateQueries({ queryKey: ["product-image-library-assets", productId] }),
    ];
    if (includeGraph) {
      invalidations.push(queryClient.invalidateQueries({ queryKey: ["workflow-graph", productId] }));
      invalidations.push(queryClient.invalidateQueries({ queryKey: ["graph-runs", productId, graphId] }));
      invalidations.push(queryClient.invalidateQueries({ queryKey: ["workflow-media-library", productId, graphId] }));
    }
    await Promise.all(invalidations);
  }, [graphId, productId, queryClient]);

  const handleSubmit = useCallback(async (payload: LocalImageEditSubmitPayload) => {
    if (!request) return;
    setCommandBusy(true);
    setCommandError(null);
    try {
      const requestKey = await localEditRequestKey(request, payload);
      const idempotencyKey = idempotencyKeysRef.current.get(requestKey) ?? createIdempotencyKey(requestKey);
      idempotencyKeysRef.current.set(requestKey, idempotencyKey);
      currentRequestKeyRef.current = requestKey;
      const input: LocalImageEditCreateInput = {
        source_asset_id: request.sourceAssetId,
        operation: payload.operation,
        mask: payload.mask,
        mask_geometry: toWireGeometry(payload.maskGeometry),
        reference_asset_ids: [],
        instruction: payload.instruction,
        source_text: payload.sourceText,
        replacement_text: payload.replacementText,
        target_node_id: request.targetNodeId,
      };
      const persisted = task?.status === "draft"
        ? await api.updateLocalImageEdit(productId, task.id, {
          expected_revision: task.revision,
          operation: input.operation,
          mask: input.mask,
          mask_geometry: input.mask_geometry,
          reference_asset_ids: input.reference_asset_ids,
          instruction: input.instruction,
          source_text: input.source_text,
          replacement_text: input.replacement_text,
        })
        : await api.createLocalImageEdit(productId, input);
      setTask(persisted);
      const submitted = await api.submitLocalImageEdit(productId, persisted.id, idempotencyKey);
      setTask(submitted);
      await invalidateTaskViews(submitted.id, false);
    } catch (error) {
      const message = errorDetail(error, t("localEdit.actionFailed"));
      setCommandError(message);
      throw error;
    } finally {
      setCommandBusy(false);
    }
  }, [invalidateTaskViews, productId, request, t, task]);

  const handleSubmitExisting = useCallback(async () => {
    if (!task || task.status !== "draft") return;
    const requestKey = currentRequestKeyRef.current ?? `persisted-draft:${task.id}:${task.revision}`;
    const idempotencyKey = idempotencyKeysRef.current.get(requestKey) ?? persistedDraftIdempotencyKey(task);
    idempotencyKeysRef.current.set(requestKey, idempotencyKey);
    currentRequestKeyRef.current = requestKey;
    setCommandBusy(true);
    setCommandError(null);
    try {
      const submitted = await api.submitLocalImageEdit(productId, task.id, idempotencyKey);
      setTask(submitted);
    } catch (error) {
      setCommandError(errorDetail(error, t("localEdit.actionFailed")));
    } finally {
      setCommandBusy(false);
    }
  }, [productId, t, task]);

  const handleCancel = useCallback(async () => {
    if (!task || !task.is_cancelable) return;
    setCommandBusy(true);
    setCommandError(null);
    try {
      setTask(await api.cancelLocalImageEdit(productId, task.id, task.revision));
    } catch (error) {
      setCommandError(errorDetail(error, t("localEdit.actionFailed")));
    } finally {
      setCommandBusy(false);
    }
  }, [productId, t, task]);

  const handleRetry = useCallback(async () => {
    if (!task) return;
    if (task.status === "draft") {
      await handleSubmitExisting();
      return;
    }
    if (task.status !== "failed" || !task.is_retryable) return;
    setCommandBusy(true);
    setCommandError(null);
    try {
      setTask(await api.retryLocalImageEdit(productId, task.id, task.revision));
    } catch (error) {
      setCommandError(errorDetail(error, t("localEdit.actionFailed")));
    } finally {
      setCommandBusy(false);
    }
  }, [handleSubmitExisting, productId, t, task]);

  const handleAdopt = useCallback(async () => {
    if (!task?.target_node_id || !task.source_artifact_id || !task.result_asset) return;
    setCommandBusy(true);
    setCommandError(null);
    try {
      const next = await api.adoptLocalImageEdit(productId, task.id, task.source_artifact_id);
      setTask(next);
      await invalidateTaskViews(next.id, true);
    } catch (error) {
      setCommandError(errorDetail(error, t("localEdit.actionFailed")));
    } finally {
      setCommandBusy(false);
    }
  }, [invalidateTaskViews, productId, t, task]);

  const handleRevert = useCallback(async () => {
    const adoption = latestActiveAdoptionEvent(task);
    if (!task || !adoption) return;
    setCommandBusy(true);
    setCommandError(null);
    try {
      const next = await api.revertLocalImageEdit(
        productId,
        task.id,
        adoption.id,
        adoption.to_artifact_id,
      );
      setTask(next);
      await invalidateTaskViews(next.id, true);
    } catch (error) {
      setCommandError(errorDetail(error, t("localEdit.actionFailed")));
    } finally {
      setCommandBusy(false);
    }
  }, [invalidateTaskViews, productId, t, task]);

  const keepInLibrary = useCallback(async () => {
    if (task) await invalidateTaskViews(task.id, false);
    close();
  }, [close, invalidateTaskViews, task]);

  const continueEdit = useCallback(() => {
    const resultAssetId = task?.result_asset?.id;
    if (!resultAssetId || !request) return;
    currentRequestKeyRef.current = null;
    setTask(null);
    setRequest({
      sourceAssetId: resultAssetId,
      targetNodeId: continuedEditTargetNodeId(task, request.targetNodeId),
      operation: task.operation,
    });
  }, [request, task]);

  const dialogLabels = useMemo<Partial<LocalImageEditLabels>>(() => ({
    title: t("localEdit.title"),
    editState: t("localEdit.editState"),
    resultState: t("localEdit.resultState"),
    source: t("localEdit.source"),
    result: t("localEdit.result"),
    compare: t("localEdit.compare"),
    operation: t("localEdit.operation"),
    instruction: t("localEdit.instruction"),
    sourceText: t("localEdit.sourceText"),
    replacementText: t("localEdit.replacementText"),
    brushSize: t("localEdit.brushSize"),
    hardness: t("localEdit.hardness"),
    eraseSelection: t("localEdit.eraseSelection"),
    clear: t("localEdit.clear"),
    submit: t("localEdit.submit"),
    close: t("localEdit.close"),
    sourceLoading: t("localEdit.sourceLoading"),
    sourceLoadFailed: t("localEdit.sourceLoadFailed"),
    actionFailed: t("localEdit.actionFailed"),
    capabilityUnavailable: t("localEdit.capabilityUnavailable"),
    targetNodeImpact: t("localEdit.targetNodeImpact"),
    resultReady: t("localEdit.resultReady"),
    keepInLibrary: t("localEdit.keepInLibrary"),
    adopt: t("localEdit.adopt"),
    revertAdoption: t("localEdit.revertAdoption"),
    continueEdit: t("localEdit.continueEdit"),
    maskCanvasLabel: t("localEdit.maskCanvasLabel"),
    sourceAlt: t("localEdit.sourceAlt"),
    resultAlt: t("localEdit.resultAlt"),
    operations: {
      remove: t("localEdit.operation.remove"),
      replace_text: t("localEdit.operation.replaceText"),
      inpaint: t("localEdit.operation.inpaint"),
    },
    validationMessages: {
      unsupported_operation: t("localEdit.validation.unsupportedOperation"),
      source_unavailable: t("localEdit.validation.sourceUnavailable"),
      invalid_source_dimensions: t("localEdit.validation.invalidSourceDimensions"),
      instruction_required: t("localEdit.validation.instructionRequired"),
      source_text_required: t("localEdit.validation.sourceTextRequired"),
      replacement_text_required: t("localEdit.validation.replacementTextRequired"),
      mask_required: t("localEdit.validation.maskRequired"),
      mask_size_mismatch: t("localEdit.validation.maskSizeMismatch"),
      mask_needs_edit_and_protection: t("localEdit.validation.maskNeedsEditAndProtection"),
    },
    statusLabels: {
      draft: t("localEdit.status.draft"),
      queued: t("localEdit.status.queued"),
      running: t("localEdit.status.running"),
      succeeded: t("localEdit.status.succeeded"),
      failed: t("localEdit.status.failed"),
      cancelled: t("localEdit.status.cancelled"),
      unknown: t("localEdit.status.unknown"),
    },
    cancelTask: t("localEdit.cancel"),
    retryTask: t("localEdit.retry"),
    phase: t("localEdit.phase"),
    provider: t("localEdit.provider"),
  }), [t]);

  const dialogCapability: DialogCapability | undefined = capabilityQuery.data
    ? {
      supported: capabilityQuery.data.supported,
      message: capabilityQuery.data.reason,
      operations: capabilityQuery.data.operations.filter(isLocalImageEditOperation),
    }
    : capabilityQuery.error
      ? { supported: false, message: errorDetail(capabilityQuery.error, t("localEdit.capabilityUnavailable")) }
      : undefined;
  const activeAdoption = latestActiveAdoptionEvent(task);
  const dialog = request && source ? (
    <LocalImageEditDialog
      sourceAsset={{
        assetId: source.asset.id,
        url: source.url,
        sourceWidth: source.asset.width as number,
        sourceHeight: source.asset.height as number,
        alt: source.asset.display_name,
      }}
      resultUrl={task?.result_asset ? api.toApiUrl(task.result_asset.preview_url) : null}
      operation={task?.operation ?? request.operation ?? "inpaint"}
      instruction={task?.instruction}
      sourceText={task?.source_text}
      replacementText={task?.replacement_text}
      labels={dialogLabels}
      busy={commandBusy || sourceLoading || capabilityQuery.isLoading || Boolean(task && shouldPollLocalImageEditTask(task.status))}
      error={sourceError ?? commandError}
      capability={dialogCapability}
      targetNodeImpact={request.targetNodeId ? t("localEdit.targetNodeImpact") : null}
      adopted={Boolean(activeAdoption)}
      taskStatus={task?.status ?? null}
      progressPhase={task?.progress_phase}
      providerName={task?.provider_name ?? task?.requested_provider_name}
      failureReason={task?.failure_reason}
      isRetryable={task?.is_retryable ?? false}
      isCancelable={task?.is_cancelable ?? false}
      onSubmit={handleSubmit}
      onClose={close}
      onKeepInLibrary={keepInLibrary}
      onAdopt={request.targetNodeId && task?.status === "succeeded" && !activeAdoption ? handleAdopt : undefined}
      onRevert={request.targetNodeId && activeAdoption ? handleRevert : undefined}
      onContinue={continueEdit}
      onCancel={handleCancel}
      onRetry={(task?.status === "failed" && task.is_retryable) || task?.status === "draft" ? handleRetry : undefined}
    />
  ) : request ? (
    <ControllerState
      loading={sourceLoading}
      error={sourceError}
      loadingLabel={t("localEdit.sourceLoading")}
      closeLabel={t("localEdit.close")}
      onClose={close}
    />
  ) : null;

  return { openLocalImageEdit, dialog };
}

function ControllerState({
  loading,
  error,
  loadingLabel,
  closeLabel,
  onClose,
}: {
  loading: boolean;
  error: string | null;
  loadingLabel: string;
  closeLabel: string;
  onClose: () => void;
}) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/55 p-4" data-local-edit-controller-state>
      <div role="dialog" aria-modal="true" className="w-full max-w-sm rounded-xl bg-surface-raised p-5 text-text-primary shadow-2xl">
        <div className="flex items-start gap-3">
          {loading ? <Loader2 size={18} className="mt-0.5 animate-spin text-accent" aria-hidden="true" /> : <AlertCircle size={18} className="mt-0.5 text-state-error" aria-hidden="true" />}
          <p role={error ? "alert" : "status"} className="min-w-0 flex-1 text-sm leading-6">{error ?? loadingLabel}</p>
          <button type="button" onClick={onClose} className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md text-text-secondary hover:bg-surface-subtle" aria-label={closeLabel} title={closeLabel}>
            <X size={17} aria-hidden="true" />
          </button>
        </div>
      </div>
    </div>
  );
}

async function loadVerifiedSource(productId: string, assetId: string): Promise<LoadedSource> {
  const asset = await api.getGalleryAsset(productId, assetId);
  const sourceWidth = asset.width;
  const sourceHeight = asset.height;
  if (
    asset.verification_status !== "verified"
    || typeof sourceWidth !== "number"
    || typeof sourceHeight !== "number"
    || !Number.isInteger(sourceWidth)
    || !Number.isInteger(sourceHeight)
    || sourceWidth <= 0
    || sourceHeight <= 0
  ) {
    throw new LocalImageEditSourceError("invalid_dimensions");
  }
  const response = await fetch(api.toApiUrl(asset.download_url), { credentials: "include" });
  if (!response.ok) throw new LocalImageEditSourceError("read_failed");
  const blob = await response.blob();
  if (blob.size < 1) throw new LocalImageEditSourceError("empty");
  return { asset, url: URL.createObjectURL(blob) };
}

function toWireGeometry(geometry: LocalImageEditSubmitPayload["maskGeometry"]): LocalImageEditCreateInput["mask_geometry"] {
  return {
    source_width: geometry.sourceWidth,
    source_height: geometry.sourceHeight,
    viewport_width: geometry.viewportWidth,
    viewport_height: geometry.viewportHeight,
    viewport_to_source: geometry.viewportToSource,
    transform_direction: geometry.transformDirection,
  };
}

async function localEditRequestKey(
  request: LocalImageEditOpenRequest,
  payload: LocalImageEditSubmitPayload,
): Promise<string> {
  const bytes = await payload.mask.arrayBuffer();
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  const maskDigest = bytesToHex(new Uint8Array(digest));
  return JSON.stringify({
    source_asset_id: request.sourceAssetId,
    target_node_id: request.targetNodeId,
    operation: payload.operation,
    instruction: payload.instruction,
    source_text: payload.sourceText,
    replacement_text: payload.replacementText,
    mask_digest: maskDigest,
    mask_geometry: toWireGeometry(payload.maskGeometry),
  });
}

function createIdempotencyKey(requestKey: string): string {
  const suffix = typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `local-edit-${stableHash(requestKey)}-${suffix}`.slice(0, 120);
}

function stableHash(value: string): string {
  let hash = 2166136261;
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return (hash >>> 0).toString(16);
}

function bytesToHex(bytes: Uint8Array): string {
  return [...bytes].map((byte) => byte.toString(16).padStart(2, "0")).join("");
}

function compareTaskFreshness(left: LocalImageEditTask, right: LocalImageEditTask): number {
  return compareTimestamp(left.updated_at, right.updated_at)
    || compareTimestamp(left.created_at, right.created_at)
    || right.id.localeCompare(left.id);
}

function compareEventFreshness(left: LocalImageEditTask["adoption_events"][number], right: LocalImageEditTask["adoption_events"][number]): number {
  return compareTimestamp(left.created_at, right.created_at) || right.id.localeCompare(left.id);
}

function compareTimestamp(left: string, right: string): number {
  const leftTime = Date.parse(left);
  const rightTime = Date.parse(right);
  if (Number.isFinite(leftTime) && Number.isFinite(rightTime) && leftTime !== rightTime) return rightTime - leftTime;
  return right.localeCompare(left);
}

export function revokeLocalImageEditObjectUrl(url: string): void {
  if (typeof URL !== "undefined") URL.revokeObjectURL(url);
}

function errorDetail(error: unknown, fallback: string): string {
  if (error instanceof ApiError && error.detail) return error.detail;
  if (error instanceof Error && error.message) return error.message;
  return fallback;
}

function sourceErrorDetail(error: unknown, t: (key: "localEdit.sourceLoadFailed" | "localEdit.validation.invalidSourceDimensions") => string): string {
  if (error instanceof LocalImageEditSourceError && error.code === "invalid_dimensions") {
    return t("localEdit.validation.invalidSourceDimensions");
  }
  if (error instanceof LocalImageEditSourceError) return t("localEdit.sourceLoadFailed");
  return errorDetail(error, t("localEdit.sourceLoadFailed"));
}
