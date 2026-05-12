import { useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, PointerEvent as ReactPointerEvent, WheelEvent as ReactWheelEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Check,
  Download,
  GalleryHorizontalEnd,
  History,
  Image as ImageIcon,
  ImagePlus,
  Layers3,
  Loader2,
  MessagesSquare,
  OctagonX,
  Pencil,
  Plus,
  RotateCcw,
  Save,
  Settings,
  Sparkles,
  Trash2,
} from "lucide-react";
import { useNavigate, useParams } from "react-router-dom";

import { GalleryImagePreviewDialog } from "../components/GalleryImagePreviewDialog";
import { ImageDropZone } from "../components/ImageDropZone";
import { ImageGenerationSettingsPanel } from "../components/ImageGenerationSettingsPanel";
import { ImageGenerationSettingsTabs, type ImageGenerationSettingsTab } from "../components/ImageGenerationSettingsTabs";
import { ImageToolControls } from "../components/ImageToolControls";
import { PromptPreviewDialog, type PromptPreview } from "../components/PromptPreviewDialog";
import { TopNav } from "../components/TopNav";
import { api, ApiError } from "../lib/api";
import { formatDateTime } from "../lib/format";
import { DEFAULT_IMAGE_TOOL_ALLOWED_FIELDS } from "../lib/imageToolOptions";
import { useI18n } from "../lib/preferences";
import {
  DEFAULT_IMAGE_GENERATION_MAX_DIMENSION,
  buildImageSizeOptions,
  formatImageSizeValue,
} from "../lib/imageSizes";
import {
  HISTORY_PANEL_DEFAULT_HEIGHT,
  HISTORY_PANEL_MIN_HEIGHT,
  LEFT_PANEL_DEFAULT_WIDTH,
  LEFT_PANEL_MIN_WIDTH,
  RIGHT_PANEL_DEFAULT_WIDTH,
  RIGHT_PANEL_MIN_WIDTH,
  clampImageChatPanelLayout,
  clampPanelSize,
  getHistoryPanelMaxHeight,
  getLeftPanelMaxWidth,
  getRightPanelMaxWidth,
  wheelDeltaToPixels,
} from "./image-chat/resizableLayout";
import {
  buildImageGenerationSubmitSignature,
  buildImageSessionHistoryTree,
  clampGenerationCount,
  compactImageToolOptions,
  findImageHistoryPlaceholder,
  imageGenerationRetryMetadata,
  isImageSessionGenerationTaskActive,
  isImageSessionGenerationTaskAutoRetrying,
  isImageSessionGenerationTaskCancelable,
  isImageSessionGenerationTaskRetryable,
  mergeImageSessionStatusIntoDetail,
  pruneSelectedReferenceIds,
  reconcileImageSessionSelection,
  requiresImageSessionGenerationBase,
  selectImageGenerationTaskNextPlaceholderId,
  selectSubmittedImageGenerationTaskPlaceholderId,
  shouldBlockDuplicateGenerationSubmit,
  shouldRefreshImageSessionDetailFromStatus,
} from "./image-chat/branching";
import type {
  ImageGenerationSubmitGuard,
  ImageGenerationSubmitPayload,
  ImageHistoryBranch,
  ImageHistoryCandidate,
  ImageHistoryPlaceholderCandidate,
} from "./image-chat/branching";
import type {
  ImageSessionDetail,
  ImageSessionAsset,
  ImageSessionRound,
  ImageSessionGenerationTask,
  ImageSessionListResponse,
  ImageSessionStatus,
  ImageToolOptions,
  ProductDetail,
  ProductSummary,
  SourceAsset,
} from "../lib/types";

const DUPLICATE_GENERATION_SUBMIT_WINDOW_MS = 1800;
const MAX_BRANCH_CONTEXT_IMAGES = 6;
const DESKTOP_RESIZABLE_LAYOUT_QUERY = "(min-width: 1024px)";

type ImageChatResizeTarget = "left" | "right" | "history";

function handleHistoryWheelScroll(event: ReactWheelEvent<HTMLDivElement>) {
  if (event.ctrlKey) {
    return;
  }
  const container = event.currentTarget;
  const maxScrollLeft = container.scrollWidth - container.clientWidth;
  if (maxScrollLeft <= 1) {
    return;
  }
  const absDeltaX = Math.abs(event.deltaX);
  const absDeltaY = Math.abs(event.deltaY);
  if (absDeltaY === 0 || absDeltaX >= absDeltaY) {
    return;
  }
  const delta = wheelDeltaToPixels(event.deltaY, event.deltaMode, container.clientWidth);
  const nextScrollLeft = clampPanelSize(container.scrollLeft + delta, 0, maxScrollLeft);
  if (nextScrollLeft === container.scrollLeft) {
    return;
  }
  event.preventDefault();
  container.scrollLeft = nextScrollLeft;
}

function getSessionReferenceAssets(imageSession: ImageSessionDetail | undefined): ImageSessionAsset[] {
  return imageSession?.assets.filter((asset) => asset.kind === "reference_upload") ?? [];
}

function generationTaskQueueText(task: ImageSessionGenerationTask, t: ReturnType<typeof useI18n>["t"]) {
  const retryMetadata = imageGenerationRetryMetadata(task);
  if (isImageSessionGenerationTaskAutoRetrying(task) && retryMetadata?.auto_retry_attempt && retryMetadata.max_attempts) {
    return t("chat.autoRetryText", {
      attempt: Math.min(retryMetadata.auto_retry_attempt + 1, retryMetadata.max_attempts),
      max: retryMetadata.max_attempts,
      reason: retryMetadata.last_failure_reason ?? t("chat.autoRetryGenericReason"),
    });
  }
  if (task.status === "queued") {
    const ahead = task.queued_ahead_count ?? 0;
    const position = task.queue_position
      ? t("chat.queuePosition", { position: task.queue_position })
      : t("chat.queueWaiting");
    return t("chat.queueText", {
      ahead,
      position,
      active: task.queue_active_count,
      max: task.queue_max_concurrent_tasks,
    });
  }
  if (task.status === "running") {
    const providerStatus = task.provider_response_status
      ? t("chat.providerStatus", { status: task.provider_response_status })
      : "";
    return t("chat.runningText", {
      providerStatus,
      progress: task.progress_updated_at ? formatDateTime(task.progress_updated_at) : t("chat.progressJustStarted"),
      running: task.queue_running_count,
      queued: task.queue_queued_count,
    });
  }
  return "";
}

function imageRoundSizeLabel(round: ImageSessionRound, t: ReturnType<typeof useI18n>["t"]) {
  if (round.actual_size && round.actual_size !== round.size) {
    return t("gallery.sizeActualRequested", { actual: round.actual_size, requested: round.size });
  }
  return round.actual_size ?? round.size;
}

function placeholderStatusLabel(candidate: ImageHistoryPlaceholderCandidate, t: ReturnType<typeof useI18n>["t"]) {
  const retryMetadata = imageGenerationRetryMetadata(candidate.task);
  if (
    isImageSessionGenerationTaskAutoRetrying(candidate.task) &&
    retryMetadata?.auto_retry_attempt &&
    retryMetadata.max_attempts
  ) {
    return t("chat.statusAutoRetry", {
      attempt: Math.min(retryMetadata.auto_retry_attempt + 1, retryMetadata.max_attempts),
      max: retryMetadata.max_attempts,
    });
  }
  if (candidate.status === "queued") {
    return candidate.task.queue_position
      ? t("chat.statusQueuedPosition", { position: candidate.task.queue_position })
      : t("chat.statusQueued");
  }
  if (candidate.status === "running") {
    return t("chat.statusRunning", { index: candidate.candidate_index, count: candidate.candidate_count });
  }
  if (candidate.status === "completed") {
    return t("chat.statusCompletedRefreshing");
  }
  if (candidate.status === "failed") {
    return t("chat.statusFailed");
  }
  if (candidate.status === "cancelled") {
    return t("chat.statusCancelled");
  }
  return t("chat.statusCompleted");
}

function placeholderStatusClass(candidate: ImageHistoryPlaceholderCandidate) {
  if (candidate.status === "failed") {
    return "border-red-200 bg-red-50 text-red-700 dark:border-red-400/40 dark:bg-red-500/15 dark:text-red-100";
  }
  if (candidate.status === "queued") {
    return "border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-300/40 dark:bg-amber-500/15 dark:text-amber-100";
  }
  if (candidate.status === "completed") {
    return "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-300/40 dark:bg-emerald-500/15 dark:text-emerald-100";
  }
  if (candidate.status === "cancelled") {
    return "border-slate-200 bg-slate-50 text-slate-600 dark:border-slate-600 dark:bg-slate-800/80 dark:text-slate-200";
  }
  return "border-indigo-200 bg-indigo-50 text-indigo-700 dark:border-violet-400/50 dark:bg-violet-500/15 dark:text-violet-100";
}

export function ImageChatPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { productId } = useParams();
  const isProductMode = Boolean(productId);
  const autoCreateTriggered = useRef(false);
  const pendingGeneratedRoundCountRef = useRef<number | null>(null);
  const duplicateSubmitGuardRef = useRef<ImageGenerationSubmitGuard | null>(null);

  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(null);
  const [selectedGeneratedAssetId, setSelectedGeneratedAssetId] = useState<string | null>(null);
  const [selectedTaskPlaceholderId, setSelectedTaskPlaceholderId] = useState<string | null>(null);
  const [branchBaseAssetId, setBranchBaseAssetId] = useState<string | null>(null);
  const [selectedReferenceAssetIds, setSelectedReferenceAssetIds] = useState<string[]>([]);
  const [generationCount, setGenerationCount] = useState(1);
  const [draft, setDraft] = useState("");
  const [size, setSize] = useState("1024x1024");
  const [toolOptions, setToolOptions] = useState<ImageToolOptions>({});
  const [settingsTab, setSettingsTab] = useState<ImageGenerationSettingsTab>("basic");
  const [titleDraft, setTitleDraft] = useState("");
  const [renameEnabled, setRenameEnabled] = useState(false);
  const [targetProductId, setTargetProductId] = useState("");
  const [promptPreview, setPromptPreview] = useState<PromptPreview | null>(null);
  const [previewRound, setPreviewRound] = useState<ImageSessionRound | null>(null);
  const [errorMessage, setErrorMessage] = useState("");
  const [successMessage, setSuccessMessage] = useState("");
  const [leftPanelWidth, setLeftPanelWidth] = useState(LEFT_PANEL_DEFAULT_WIDTH);
  const [rightPanelWidth, setRightPanelWidth] = useState(RIGHT_PANEL_DEFAULT_WIDTH);
  const [historyPanelHeight, setHistoryPanelHeight] = useState(HISTORY_PANEL_DEFAULT_HEIGHT);

  const leftPanelStyle = {
    "--image-chat-left-panel-width": `${leftPanelWidth}px`,
  } as CSSProperties;
  const rightPanelStyle = {
    "--image-chat-right-panel-width": `${rightPanelWidth}px`,
  } as CSSProperties;
  const historyPanelStyle = {
    "--image-chat-history-panel-height": `${historyPanelHeight}px`,
  } as CSSProperties;

  useEffect(() => {
    function clampPanelSizesToViewport() {
      if (!window.matchMedia(DESKTOP_RESIZABLE_LAYOUT_QUERY).matches) {
        return;
      }
      const nextLayout = clampImageChatPanelLayout(
        {
          leftPanelWidth,
          rightPanelWidth,
          historyPanelHeight,
        },
        {
          viewportWidth: window.innerWidth,
          viewportHeight: window.innerHeight,
        },
      );
      if (nextLayout.leftPanelWidth !== leftPanelWidth) {
        setLeftPanelWidth(nextLayout.leftPanelWidth);
      }
      if (nextLayout.rightPanelWidth !== rightPanelWidth) {
        setRightPanelWidth(nextLayout.rightPanelWidth);
      }
      if (nextLayout.historyPanelHeight !== historyPanelHeight) {
        setHistoryPanelHeight(nextLayout.historyPanelHeight);
      }
    }

    clampPanelSizesToViewport();
    window.addEventListener("resize", clampPanelSizesToViewport);
    return () => window.removeEventListener("resize", clampPanelSizesToViewport);
  }, [historyPanelHeight, leftPanelWidth, rightPanelWidth]);

  const sessionsQuery = useQuery({
    queryKey: ["image-sessions", productId ?? "standalone"],
    queryFn: () => api.listImageSessions(productId),
  });

  const sessionItems = sessionsQuery.data?.items ?? [];

  const productQuery = useQuery({
    queryKey: ["product", productId],
    queryFn: () => api.getProduct(productId!),
    enabled: isProductMode,
  });

  const productsQuery = useQuery({
    queryKey: ["products"],
    queryFn: () => api.listProducts({ page_size: 100 }),
    enabled: !isProductMode,
  });
  const runtimeConfigQuery = useQuery({
    queryKey: ["runtime-config"],
    queryFn: api.getRuntimeConfig,
  });

  const products = productsQuery.data?.items ?? [];
  const imageGenerationMaxDimension =
    runtimeConfigQuery.data?.image_generation_max_dimension ?? DEFAULT_IMAGE_GENERATION_MAX_DIMENSION;
  const imageToolAllowedFields = runtimeConfigQuery.data?.image_tool_allowed_fields ?? DEFAULT_IMAGE_TOOL_ALLOWED_FIELDS;
  const deletionEnabled = runtimeConfigQuery.data?.deletion_enabled ?? false;
  const sizeOptions = useMemo(
    () => buildImageSizeOptions(imageGenerationMaxDimension),
    [imageGenerationMaxDimension],
  );

  function resetImageSessionSelection() {
    setSelectedGeneratedAssetId(null);
    setSelectedTaskPlaceholderId(null);
    setBranchBaseAssetId(null);
    setSelectedReferenceAssetIds([]);
  }

  function handleSelectSession(sessionId: string) {
    setSelectedSessionId(sessionId);
    resetImageSessionSelection();
    setSuccessMessage("");
    setErrorMessage("");
  }

  useEffect(() => {
    if (!isProductMode && products.length && !targetProductId) {
      setTargetProductId(products[0].id);
    }
  }, [isProductMode, products, targetProductId]);

  const createSessionMutation = useMutation({
    mutationFn: () => api.createImageSession(productId ? { product_id: productId } : {}),
    onSuccess: async (imageSession) => {
      setSelectedSessionId(imageSession.id);
      resetImageSessionSelection();
      queryClient.setQueryData(["image-session", imageSession.id], imageSession);
      await queryClient.invalidateQueries({ queryKey: ["image-sessions", productId ?? "standalone"] });
      setErrorMessage("");
    },
    onError: (error) => {
      setErrorMessage(error instanceof ApiError ? error.detail : t("chat.createFailed"));
    },
  });

  useEffect(() => {
    if (sessionsQuery.isLoading || createSessionMutation.isPending) {
      return;
    }
    if (sessionItems.length === 0 && !autoCreateTriggered.current) {
      autoCreateTriggered.current = true;
      createSessionMutation.mutate();
      return;
    }
    if (selectedSessionId && sessionItems.some((item) => item.id === selectedSessionId)) {
      return;
    }
    if (sessionItems.length) {
      setSelectedSessionId(sessionItems[0].id);
      resetImageSessionSelection();
    }
  }, [createSessionMutation, selectedSessionId, sessionItems, sessionsQuery.isLoading]);

  const sessionDetailQuery = useQuery({
    queryKey: ["image-session", selectedSessionId],
    queryFn: () => api.getImageSession(selectedSessionId!),
    enabled: Boolean(selectedSessionId),
  });

  const imageSession = sessionDetailQuery.data;
  const historyBranches = useMemo(
    () => buildImageSessionHistoryTree(imageSession?.rounds ?? [], imageSession?.generation_tasks ?? []),
    [imageSession],
  );
  const requiresGenerationBase = requiresImageSessionGenerationBase(
    imageSession?.rounds ?? [],
    imageSession?.generation_tasks ?? [],
  );
  const sessionReferenceAssets = useMemo(() => getSessionReferenceAssets(imageSession), [imageSession]);
  const maxSelectedReferenceCount = branchBaseAssetId ? MAX_BRANCH_CONTEXT_IMAGES - 1 : MAX_BRANCH_CONTEXT_IMAGES;
  const hasActiveGenerationTask = imageSession?.generation_tasks.some(isImageSessionGenerationTaskActive) ?? false;

  const sessionStatusQuery = useQuery({
    queryKey: ["image-session-status", selectedSessionId],
    queryFn: () => api.getImageSessionStatus(selectedSessionId!),
    enabled: Boolean(selectedSessionId && hasActiveGenerationTask),
    refetchInterval: (query) => {
      const data = query.state.data as ImageSessionStatus | undefined;
      return data?.has_active_generation_task ? 1500 : false;
    },
  });

  useEffect(() => {
    const status = sessionStatusQuery.data;
    if (!status || !selectedSessionId || status.id !== selectedSessionId) {
      return;
    }
    const detail = queryClient.getQueryData<ImageSessionDetail>(["image-session", status.id]);
    const shouldRefetchDetail = shouldRefreshImageSessionDetailFromStatus(detail, status);
    if (detail) {
      queryClient.setQueryData(["image-session", status.id], mergeImageSessionStatusIntoDetail(detail, status));
    }
    if (shouldRefetchDetail) {
      void queryClient.invalidateQueries({ queryKey: ["image-session", status.id] });
      void queryClient.invalidateQueries({ queryKey: ["image-sessions", productId ?? "standalone"] });
    }
  }, [productId, queryClient, selectedSessionId, sessionStatusQuery.data]);

  useEffect(() => {
    if (!imageSession) {
      return;
    }
    setTitleDraft(imageSession.title);
    const reconciled = reconcileImageSessionSelection({
      rounds: imageSession.rounds,
      generationTasks: imageSession.generation_tasks,
      historyBranches,
      selectedGeneratedAssetId,
      selectedTaskPlaceholderId,
      branchBaseAssetId,
      selectedReferenceAssetIds,
      availableReferenceAssetIds: sessionReferenceAssets.map((asset) => asset.id),
      maxSelectedReferenceCount,
      pendingGeneratedRoundCount: pendingGeneratedRoundCountRef.current,
    });

    if (reconciled.selectedGeneratedAssetId !== selectedGeneratedAssetId) {
      setSelectedGeneratedAssetId(reconciled.selectedGeneratedAssetId);
    }
    if (reconciled.selectedTaskPlaceholderId !== selectedTaskPlaceholderId) {
      setSelectedTaskPlaceholderId(reconciled.selectedTaskPlaceholderId);
    }
    if (reconciled.branchBaseAssetId !== branchBaseAssetId) {
      setBranchBaseAssetId(reconciled.branchBaseAssetId);
    }
    if (reconciled.selectedReferenceAssetIds !== selectedReferenceAssetIds) {
      setSelectedReferenceAssetIds(reconciled.selectedReferenceAssetIds);
    }
    pendingGeneratedRoundCountRef.current = reconciled.pendingGeneratedRoundCount;

    if (reconciled.generatedRoundCompleted) {
      setSuccessMessage(t("chat.newCandidate"));
      setErrorMessage("");
    }
  }, [
    branchBaseAssetId,
    historyBranches,
    imageSession,
    maxSelectedReferenceCount,
    selectedReferenceAssetIds,
    selectedGeneratedAssetId,
    sessionReferenceAssets,
    selectedTaskPlaceholderId,
  ]);

  const selectedRound = useMemo(() => {
    if (selectedTaskPlaceholderId) {
      return null;
    }
    if (!imageSession?.rounds.length) {
      return null;
    }
    return (
      imageSession.rounds.find((round) => round.generated_asset.id === selectedGeneratedAssetId) ?? imageSession.rounds.at(-1) ?? null
    );
  }, [imageSession, selectedGeneratedAssetId, selectedTaskPlaceholderId]);

  const selectedPlaceholder = useMemo(
    () => findImageHistoryPlaceholder(historyBranches, selectedTaskPlaceholderId),
    [historyBranches, selectedTaskPlaceholderId],
  );
  const activePreviewRound =
    previewRound && imageSession?.rounds.some((round) => round.id === previewRound.id) ? previewRound : null;

  const branchBaseRound = useMemo(() => {
    if (!imageSession?.rounds.length || !branchBaseAssetId) {
      return null;
    }
    return imageSession.rounds.find((round) => round.generated_asset.id === branchBaseAssetId) ?? null;
  }, [branchBaseAssetId, imageSession]);
  const baseRequirementMessage =
    requiresGenerationBase && !branchBaseRound ? t("chat.baseRequired") : "";

  const sourceImage = useMemo(
    () => productQuery.data?.source_assets.find((asset) => asset.kind === "original_image") ?? null,
    [productQuery.data],
  );

  const productReferenceImages = useMemo(
    () => productQuery.data?.source_assets.filter((asset) => asset.kind === "reference_image") ?? [],
    [productQuery.data],
  );

  const logoutMutation = useMutation({
    mutationFn: api.destroySession,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["session"] });
      navigate("/login", { replace: true });
    },
  });

  const renameSessionMutation = useMutation({
    mutationFn: (title: string) => api.updateImageSession(selectedSessionId!, { title }),
    onSuccess: (updated) => {
      queryClient.setQueryData(["image-session", updated.id], updated);
      void queryClient.invalidateQueries({ queryKey: ["image-sessions", productId ?? "standalone"] });
      setRenameEnabled(false);
      setSuccessMessage(t("chat.renameSuccess"));
      setErrorMessage("");
    },
    onError: (error) => {
      setErrorMessage(error instanceof ApiError ? error.detail : t("chat.renameFailed"));
    },
  });

  const uploadReferenceMutation = useMutation({
    mutationFn: (input: { sessionId: string; files: File[] }) =>
      api.addImageSessionReferenceImages(input.sessionId, input.files),
    onSuccess: (updated, input) => {
      const previousReferenceIds = new Set(
        input.sessionId === selectedSessionId ? sessionReferenceAssets.map((asset) => asset.id) : [],
      );
      const uploadedReferenceIds = updated.assets
        .filter((asset) => asset.kind === "reference_upload" && !previousReferenceIds.has(asset.id))
        .map((asset) => asset.id);
      queryClient.setQueryData(["image-session", updated.id], updated);
      void queryClient.invalidateQueries({ queryKey: ["image-sessions", productId ?? "standalone"] });
      const isCurrentSession = updated.id === selectedSessionId;
      if (isCurrentSession && uploadedReferenceIds.length) {
        setSelectedReferenceAssetIds((current) =>
          pruneSelectedReferenceIds(
            [...current, ...uploadedReferenceIds],
            getSessionReferenceAssets(updated).map((asset) => asset.id),
            maxSelectedReferenceCount,
          ),
        );
      }
      if (isCurrentSession) {
        setSuccessMessage(t("chat.referenceUploaded"));
        setErrorMessage("");
      }
    },
    onError: (error) => {
      setErrorMessage(error instanceof ApiError ? error.detail : t("chat.referenceUploadFailed"));
    },
  });

  const deleteSessionReferenceMutation = useMutation({
    mutationFn: (input: { sessionId: string; assetId: string }) =>
      api.deleteImageSessionReferenceImage(input.sessionId, input.assetId),
    onSuccess: (updated) => {
      queryClient.setQueryData(["image-session", updated.id], updated);
      void queryClient.invalidateQueries({ queryKey: ["image-sessions", productId ?? "standalone"] });
      const isCurrentSession = updated.id === selectedSessionId;
      if (isCurrentSession) {
        setSelectedReferenceAssetIds((current) =>
          pruneSelectedReferenceIds(
            current,
            getSessionReferenceAssets(updated).map((asset) => asset.id),
            maxSelectedReferenceCount,
          ),
        );
        setSuccessMessage(t("chat.referenceDeleted"));
        setErrorMessage("");
      }
    },
    onError: (error) => {
      setErrorMessage(error instanceof ApiError ? error.detail : t("chat.referenceDeleteFailed"));
    },
  });

  const deleteSessionMutation = useMutation({
    mutationFn: (sessionId: string) => api.deleteImageSession(sessionId),
    onSuccess: async (_response, deletedSessionId) => {
      const remainingSessions = sessionItems.filter((item) => item.id !== deletedSessionId);
      queryClient.setQueryData<ImageSessionListResponse>(
        ["image-sessions", productId ?? "standalone"],
        (current) => current ? { ...current, items: current.items.filter((item) => item.id !== deletedSessionId) } : current,
      );
      queryClient.removeQueries({ queryKey: ["image-session", deletedSessionId] });
      if (selectedSessionId === deletedSessionId) {
        setSelectedSessionId(remainingSessions[0]?.id ?? null);
        resetImageSessionSelection();
        if (!remainingSessions.length) {
          autoCreateTriggered.current = false;
        }
      }
      await queryClient.invalidateQueries({ queryKey: ["image-sessions", productId ?? "standalone"] });
      setSuccessMessage(t("chat.sessionDeleted"));
      setErrorMessage("");
    },
    onError: (error) => {
      setErrorMessage(error instanceof ApiError ? error.detail : t("chat.sessionDeleteFailed"));
    },
  });

  const generateMutation = useMutation({
    mutationFn: (payload: ImageGenerationSubmitPayload) => api.generateImageSessionRound(selectedSessionId!, payload),
    onSuccess: (updated, variables) => {
      queryClient.setQueryData(["image-session", updated.id], updated);
      void queryClient.invalidateQueries({ queryKey: ["image-sessions", productId ?? "standalone"] });
      const placeholderId = selectSubmittedImageGenerationTaskPlaceholderId(updated.generation_tasks, variables);
      if (placeholderId) {
        setSelectedTaskPlaceholderId(placeholderId);
        setSelectedGeneratedAssetId(null);
      }
      setDraft("");
      setSuccessMessage(
        variables.generation_count > 1
          ? t("chat.submittedCount", { count: variables.generation_count })
          : t("chat.submitted"),
      );
      setErrorMessage("");
    },
    onError: (error, variables) => {
      const signature = buildImageGenerationSubmitSignature(variables);
      if (duplicateSubmitGuardRef.current?.signature === signature) {
        duplicateSubmitGuardRef.current = null;
      }
      setErrorMessage(error instanceof ApiError ? error.detail : t("chat.generateFailed"));
    },
  });

  const retryGenerationTaskMutation = useMutation({
    mutationFn: (input: { sessionId: string; taskId: string }) =>
      api.retryImageSessionGenerationTask(input.sessionId, input.taskId),
    onSuccess: (updated, input) => {
      queryClient.setQueryData(["image-session", updated.id], updated);
      void queryClient.invalidateQueries({ queryKey: ["image-sessions", productId ?? "standalone"] });
      const retriedTask = updated.generation_tasks.find((task) => task.id === input.taskId);
      if (retriedTask) {
        setSelectedTaskPlaceholderId(selectImageGenerationTaskNextPlaceholderId(retriedTask));
        setSelectedGeneratedAssetId(null);
      }
      setSuccessMessage(t("chat.retrySubmitted"));
      setErrorMessage("");
    },
    onError: (error) => {
      setErrorMessage(error instanceof ApiError ? error.detail : t("chat.retryFailed"));
    },
  });

  const cancelGenerationTaskMutation = useMutation({
    mutationFn: (input: { sessionId: string; taskId: string }) =>
      api.cancelImageSessionGenerationTask(input.sessionId, input.taskId),
    onSuccess: (updated) => {
      queryClient.setQueryData(["image-session", updated.id], updated);
      void queryClient.invalidateQueries({ queryKey: ["image-session-status", updated.id] });
      void queryClient.invalidateQueries({ queryKey: ["image-sessions", productId ?? "standalone"] });
      setSuccessMessage(t("chat.cancelledTask"));
      setErrorMessage("");
    },
    onError: (error) => {
      setErrorMessage(error instanceof ApiError ? error.detail : t("chat.cancelFailed"));
    },
  });

  const generateDisabled =
    !selectedSessionId || !imageSession || !draft.trim() || generateMutation.isPending || Boolean(baseRequirementMessage);

  const attachMutation = useMutation({
    mutationFn: (payload: { assetId: string; target: "reference" | "main_source"; productId?: string }) =>
      api.attachImageSessionAssetToProduct(selectedSessionId!, payload.assetId, {
        target: payload.target,
        product_id: payload.productId,
      }),
    onSuccess: async (response) => {
      setSuccessMessage(response.message);
      setErrorMessage("");
      await queryClient.invalidateQueries({ queryKey: ["products"] });
      await queryClient.invalidateQueries({ queryKey: ["product", response.product_id] });
    },
    onError: (error) => {
      setErrorMessage(error instanceof ApiError ? error.detail : t("chat.saveProductFailed"));
    },
  });

  const saveGalleryMutation = useMutation({
    mutationFn: (assetId: string) => api.saveGalleryEntry(assetId),
    onSuccess: async () => {
      setSuccessMessage(t("chat.savedGallery"));
      setErrorMessage("");
      await queryClient.invalidateQueries({ queryKey: ["gallery"] });
    },
    onError: (error) => {
      setErrorMessage(error instanceof ApiError ? error.detail : t("chat.saveGalleryFailed"));
    },
  });

  const deleteProductReferenceMutation = useMutation({
    mutationFn: (assetId: string) => api.deleteSourceAsset(assetId),
    onSuccess: async (updated) => {
      queryClient.setQueryData(["product", updated.id], updated);
      await queryClient.invalidateQueries({ queryKey: ["product", updated.id] });
      if (selectedSessionId) {
        await queryClient.invalidateQueries({ queryKey: ["image-session", selectedSessionId] });
      }
      setSuccessMessage(t("chat.productReferenceDeleted"));
      setErrorMessage("");
    },
    onError: (error) => {
      setErrorMessage(error instanceof ApiError ? error.detail : t("chat.productReferenceDeleteFailed"));
    },
  });

  function handleGenerate() {
    const prompt = draft.trim();
    if (!selectedSessionId || !imageSession || !prompt || generateMutation.isPending) {
      return;
    }
    if (baseRequirementMessage) {
      setErrorMessage(baseRequirementMessage);
      return;
    }
    const selectedReferenceIds = pruneSelectedReferenceIds(
      selectedReferenceAssetIds,
      sessionReferenceAssets.map((asset) => asset.id),
      maxSelectedReferenceCount,
    );
    const payload: ImageGenerationSubmitPayload = {
      prompt,
      size,
      base_asset_id: requiresGenerationBase ? branchBaseAssetId : null,
      selected_reference_asset_ids: selectedReferenceIds,
      generation_count: clampGenerationCount(generationCount),
      tool_options: compactImageToolOptions(toolOptions, imageToolAllowedFields),
    };
    const signature = buildImageGenerationSubmitSignature(payload);
    const now = Date.now();
    if (
      shouldBlockDuplicateGenerationSubmit(
        duplicateSubmitGuardRef.current,
        signature,
        now,
        DUPLICATE_GENERATION_SUBMIT_WINDOW_MS,
      )
    ) {
      setErrorMessage(t("chat.duplicateSubmit"));
      return;
    }
    duplicateSubmitGuardRef.current = { signature, submittedAt: now };
    pendingGeneratedRoundCountRef.current = imageSession?.rounds.length ?? 0;
    generateMutation.mutate(payload);
  }

  function handleRetryGenerationTask(task: ImageSessionGenerationTask) {
    if (!selectedSessionId || retryGenerationTaskMutation.isPending || !isImageSessionGenerationTaskRetryable(task)) {
      return;
    }
    pendingGeneratedRoundCountRef.current = imageSession?.rounds.length ?? 0;
    retryGenerationTaskMutation.mutate({ sessionId: selectedSessionId, taskId: task.id });
  }

  function handleCancelGenerationTask(task: ImageSessionGenerationTask) {
    if (
      !selectedSessionId ||
      cancelGenerationTaskMutation.isPending ||
      !isImageSessionGenerationTaskCancelable(task)
    ) {
      return;
    }
    cancelGenerationTaskMutation.mutate({ sessionId: selectedSessionId, taskId: task.id });
  }

  function handleRename() {
    const nextTitle = titleDraft.trim();
    if (!selectedSessionId || !nextTitle || nextTitle === imageSession?.title) {
      setRenameEnabled(false);
      return;
    }
    renameSessionMutation.mutate(nextTitle);
  }

  function handleAttach(target: "reference" | "main_source") {
    if (!selectedRound) {
      return;
    }
    if (!isProductMode && !targetProductId) {
      setErrorMessage(t("chat.selectProductFirst"));
      return;
    }
    attachMutation.mutate({
      assetId: selectedRound.generated_asset.id,
      target,
      productId: isProductMode ? productId : targetProductId,
    });
  }

  function handleSaveSelectedToGallery() {
    if (!selectedRound || saveGalleryMutation.isPending) {
      return;
    }
    saveGalleryMutation.mutate(selectedRound.generated_asset.id);
  }

  function handleDeleteSession(sessionId: string) {
    if (deleteSessionMutation.isPending) {
      return;
    }
    if (!deletionEnabled) {
      setErrorMessage(t("chat.deleteDisabled"));
      return;
    }
    if (!window.confirm(t("chat.confirmDeleteSession"))) {
      return;
    }
    deleteSessionMutation.mutate(sessionId);
  }

  function handleDeleteProductReference(assetId: string) {
    if (deleteProductReferenceMutation.isPending) {
      return;
    }
    if (!window.confirm(t("chat.confirmDeleteProductReference"))) {
      return;
    }
    deleteProductReferenceMutation.mutate(assetId);
  }

  function handleReferenceToggle(assetId: string, checked: boolean) {
    setSelectedReferenceAssetIds((current) => {
      const next = checked ? [...current, assetId] : current.filter((id) => id !== assetId);
      return pruneSelectedReferenceIds(
        next,
        sessionReferenceAssets.map((asset) => asset.id),
        maxSelectedReferenceCount,
      );
    });
  }

  function handleDeleteSessionReference(assetId: string) {
    if (!selectedSessionId || deleteSessionReferenceMutation.isPending) {
      return;
    }
    if (!window.confirm(t("chat.confirmDeleteSessionReference"))) {
      return;
    }
    deleteSessionReferenceMutation.mutate({ sessionId: selectedSessionId, assetId });
  }

  function handleUploadReferenceFiles(files: File[]) {
    if (!selectedSessionId || uploadReferenceMutation.isPending || files.length === 0) {
      return;
    }
    uploadReferenceMutation.mutate({ sessionId: selectedSessionId, files });
  }

  function handlePanelResizeStart(target: ImageChatResizeTarget, event: ReactPointerEvent<HTMLButtonElement>) {
    if (event.button !== 0) {
      return;
    }
    event.preventDefault();
    const startX = event.clientX;
    const startY = event.clientY;
    const startLeftWidth = leftPanelWidth;
    const startRightWidth = rightPanelWidth;
    const startHistoryHeight = historyPanelHeight;
    const previousCursor = document.body.style.cursor;
    const previousUserSelect = document.body.style.userSelect;
    document.body.style.cursor = target === "history" ? "row-resize" : "col-resize";
    document.body.style.userSelect = "none";

    const handlePointerMove = (moveEvent: PointerEvent) => {
      if (target === "left") {
        const maxWidth = getLeftPanelMaxWidth(window.innerWidth, startRightWidth);
        setLeftPanelWidth(clampPanelSize(startLeftWidth + moveEvent.clientX - startX, LEFT_PANEL_MIN_WIDTH, maxWidth));
        return;
      }
      if (target === "right") {
        const maxWidth = getRightPanelMaxWidth(window.innerWidth, startLeftWidth);
        setRightPanelWidth(clampPanelSize(startRightWidth + startX - moveEvent.clientX, RIGHT_PANEL_MIN_WIDTH, maxWidth));
        return;
      }
      const maxHeight = getHistoryPanelMaxHeight(window.innerHeight);
      setHistoryPanelHeight(clampPanelSize(startHistoryHeight + startY - moveEvent.clientY, HISTORY_PANEL_MIN_HEIGHT, maxHeight));
    };

    const finishResize = () => {
      document.body.style.cursor = previousCursor;
      document.body.style.userSelect = previousUserSelect;
      window.removeEventListener("pointermove", handlePointerMove);
      window.removeEventListener("pointerup", finishResize);
      window.removeEventListener("pointercancel", finishResize);
    };

    window.addEventListener("pointermove", handlePointerMove);
    window.addEventListener("pointerup", finishResize);
    window.addEventListener("pointercancel", finishResize);
  }

  return (
    <div className="flex min-h-screen flex-col bg-slate-100 text-slate-900 dark:bg-[#060a12] dark:text-slate-100 lg:h-screen lg:overflow-hidden">
      <TopNav
        breadcrumbs={isProductMode ? `${productQuery.data?.name ?? t("chat.productFallback")} / ${t("chat.breadcrumb")}` : t("chat.breadcrumb")}
        onHome={() => navigate(isProductMode && productId ? `/products/${productId}` : "/products")}
        onLogout={() => logoutMutation.mutate()}
      />

      <main className="flex flex-1 flex-col pb-28 lg:min-h-0 lg:flex-row lg:overflow-hidden lg:pb-0">
        <aside
          className="relative flex w-full shrink-0 flex-col border-b border-slate-200 bg-white/95 dark:border-slate-700/80 dark:bg-[#0f1726] dark:shadow-[12px_0_36px_rgba(0,0,0,0.24)] dark:backdrop-blur-xl lg:w-[var(--image-chat-left-panel-width)] lg:border-b-0 lg:border-r"
          style={leftPanelStyle}
        >
          <button
            type="button"
            aria-label={t("chat.resizeSessions")}
            title={t("chat.resizeSessionsTitle")}
            onPointerDown={(event) => handlePanelResizeStart("left", event)}
            className="absolute right-[-5px] top-0 z-20 hidden h-full w-3 cursor-col-resize items-center justify-center transition-colors hover:bg-indigo-50/70 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:hover:bg-violet-500/15 lg:flex"
          >
            <span className="h-12 w-1 rounded-full bg-slate-300 dark:bg-slate-600" />
          </button>
          <div className="border-b border-slate-200 px-4 py-4 dark:border-slate-800">
            <div className="flex items-center justify-between gap-3">
              <div>
                <div className="text-sm font-semibold text-slate-950 dark:text-white">{t("chat.sessions")}</div>
                <div className="mt-1 text-xs text-slate-500 dark:text-slate-400">{t("chat.count", { count: sessionItems.length })}</div>
              </div>
              <button
                type="button"
                onClick={() => createSessionMutation.mutate()}
                disabled={createSessionMutation.isPending}
                className="inline-flex h-9 w-9 items-center justify-center rounded-xl bg-indigo-600 text-white shadow-sm shadow-indigo-500/20 transition-colors hover:bg-indigo-500 disabled:opacity-60 dark:bg-gradient-to-br dark:from-indigo-500 dark:to-violet-500 dark:shadow-violet-900/35 dark:ring-1 dark:ring-violet-300/30"
                aria-label={t("chat.newSession")}
              >
                {createSessionMutation.isPending ? <Loader2 size={15} className="animate-spin" /> : <Plus size={16} />}
              </button>
            </div>
          </div>

          <div className="flex gap-3 overflow-x-auto p-3 lg:min-h-0 lg:flex-1 lg:flex-col lg:gap-2 lg:overflow-x-visible lg:overflow-y-auto">
            {sessionsQuery.isLoading ? (
              <div className="flex justify-center py-12 text-slate-400">
                <Loader2 size={18} className="animate-spin" />
              </div>
            ) : sessionItems.length ? (
              sessionItems.map((item) => {
                const active = item.id === selectedSessionId;
                const deleting = deleteSessionMutation.isPending && deleteSessionMutation.variables === item.id;
                return (
                  <div
                    key={item.id}
                    className={`group relative w-64 shrink-0 overflow-hidden rounded-2xl border transition-all lg:w-auto ${
                      active
                        ? "border-indigo-300 bg-indigo-50 shadow-sm shadow-indigo-100 ring-1 ring-indigo-200/80 dark:border-violet-500/80 dark:bg-violet-500/14 dark:shadow-violet-950/30 dark:ring-violet-400/45"
                        : "border-slate-200 bg-white hover:border-slate-300 hover:bg-slate-50 dark:border-slate-700/75 dark:bg-[#151f33] dark:hover:border-violet-500/45 dark:hover:bg-[#1a2740]"
                    }`}
                  >
                    <button
                      type="button"
                      onClick={() => handleSelectSession(item.id)}
                      className="flex w-full items-center gap-3 p-2.5 pr-10 text-left"
                    >
                      <div className="relative flex h-16 w-16 shrink-0 items-center justify-center overflow-hidden rounded-xl bg-slate-100 text-slate-400 ring-1 ring-slate-200 dark:bg-[#0a1020] dark:text-slate-400 dark:ring-slate-600/80">
                        {item.latest_generated_asset ? (
                          <img
                            src={api.toApiUrl(item.latest_generated_asset.thumbnail_url)}
                            alt={item.title}
                            loading="lazy"
                            decoding="async"
                            className="h-full w-full object-cover"
                          />
                        ) : (
                          <MessagesSquare size={18} />
                        )}
                        {active ? <div className="absolute inset-0 ring-2 ring-inset ring-indigo-500/60 dark:ring-violet-400/80" /> : null}
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className={`truncate text-sm font-semibold ${active ? "text-indigo-950 dark:text-white" : "text-slate-900 dark:text-slate-100"}`}>
                          {item.title}
                        </div>
                        <div className="mt-1 flex items-center gap-1.5 text-[11px] text-slate-500 dark:text-slate-300">
                          <History size={11} />
                          <span>{t("chat.roundCount", { count: item.rounds_count })}</span>
                        </div>
                        <div className="mt-0.5 truncate text-[11px] text-slate-400 dark:text-slate-500">{formatDateTime(item.updated_at)}</div>
                      </div>
                    </button>
                    <button
                      type="button"
                      aria-label={t("chat.deleteSession")}
                      onClick={() => handleDeleteSession(item.id)}
                      disabled={deleting || !deletionEnabled}
                      title={deletionEnabled ? t("chat.deleteSession") : t("chat.deleteDisabled")}
                      className="absolute right-2 top-2 inline-flex h-7 w-7 items-center justify-center rounded-lg bg-white/95 text-slate-400 opacity-100 shadow-sm ring-1 ring-slate-200 transition-colors hover:text-red-600 disabled:opacity-60 dark:bg-slate-950/88 dark:text-slate-400 dark:ring-slate-700 dark:hover:text-red-300 md:opacity-0 md:group-hover:opacity-100"
                    >
                      {deleting ? <Loader2 size={13} className="animate-spin" /> : <Trash2 size={13} />}
                    </button>
                  </div>
                );
              })
            ) : (
              <div className="rounded-2xl border border-dashed border-slate-200 p-5 text-center text-sm text-slate-500 dark:border-slate-700 dark:text-slate-400">
                {t("chat.noSessions")}
              </div>
            )}
          </div>
        </aside>

        <section className="flex min-w-0 flex-col bg-slate-100 dark:bg-[#0b1220] lg:min-h-0 lg:flex-1 lg:overflow-hidden">
          <div className="flex flex-col p-3 pb-2 lg:min-h-0 lg:flex-1">
            <div className="mb-3 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <div className="min-w-0">
                <div className="flex items-center gap-2 text-xs font-medium text-slate-500 dark:text-slate-300">
                  <span className="inline-flex h-7 items-center rounded-full bg-white px-3 shadow-sm ring-1 ring-slate-200 dark:border dark:border-violet-400/30 dark:bg-slate-950/70 dark:text-violet-100 dark:ring-violet-400/20">
                    {t("chat.currentResult")}
                  </span>
                  {branchBaseRound ? (
                    <span className="inline-flex h-7 items-center gap-1 rounded-full bg-indigo-600 px-3 text-white shadow-sm shadow-indigo-500/20 dark:bg-violet-500/20 dark:text-violet-100 dark:ring-1 dark:ring-violet-400/40">
                      <Layers3 size={12} /> {t("chat.baseSelected")}
                    </span>
                  ) : null}
                </div>
                <h1 className="mt-2 text-xl font-semibold tracking-tight text-slate-950 dark:text-white">
                  {imageSession?.title ?? t("chat.workbench")}
                </h1>
                {selectedRound ? (
                  <div className="mt-1 text-xs font-medium text-slate-500 dark:text-slate-400 md:hidden">
                    {imageRoundSizeLabel(selectedRound, t)} · {t("chat.candidate", { index: selectedRound.candidate_index, count: selectedRound.candidate_count })}
                  </div>
                ) : selectedPlaceholder ? (
                  <div className="mt-1 text-xs font-medium text-slate-500 dark:text-slate-400 md:hidden">
                    {placeholderStatusLabel(selectedPlaceholder, t)} · {t("chat.candidate", { index: selectedPlaceholder.candidate_index, count: selectedPlaceholder.candidate_count })}
                  </div>
                ) : null}
              </div>
              <div className="flex w-full flex-wrap items-center justify-start gap-2 sm:w-auto sm:justify-end">
                {selectedRound ? (
                  <>
                    <span className="hidden rounded-full border border-slate-200 bg-white px-3 py-1.5 text-xs text-slate-600 shadow-sm dark:border-slate-700 dark:bg-slate-950/80 dark:text-slate-200 md:inline-flex">
                      {imageRoundSizeLabel(selectedRound, t)} · {t("chat.candidate", { index: selectedRound.candidate_index, count: selectedRound.candidate_count })}
                    </span>
                    <a
                      href={api.toApiUrl(selectedRound.generated_asset.download_url)}
                      target="_blank"
                      rel="noreferrer"
                      title={t("chat.downloadCurrent")}
                      aria-label={t("chat.downloadCurrent")}
                      className="inline-flex h-9 w-9 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-700 shadow-sm transition-colors hover:border-indigo-200 hover:text-indigo-700 dark:border-slate-700 dark:bg-slate-950/80 dark:text-slate-200 dark:hover:border-violet-400/60 dark:hover:text-violet-100"
                    >
                      <Download size={15} />
                    </a>
                    <button
                      type="button"
                      onClick={handleSaveSelectedToGallery}
                      disabled={saveGalleryMutation.isPending}
                      title={t("chat.saveSelectedGallery")}
                      aria-label={t("chat.saveSelectedGallery")}
                      className="inline-flex h-10 shrink-0 items-center justify-center rounded-xl bg-indigo-600 px-4 text-sm font-semibold text-white shadow-sm shadow-indigo-500/20 ring-1 ring-indigo-500 transition-colors hover:bg-indigo-700 disabled:opacity-60 dark:bg-gradient-to-r dark:from-indigo-500 dark:to-violet-500 dark:shadow-violet-900/35 dark:ring-violet-300/35"
                    >
                      {saveGalleryMutation.isPending ? (
                        <Loader2 size={16} className="mr-2 animate-spin" />
                      ) : (
                        <GalleryHorizontalEnd size={16} className="mr-2" />
                      )}
                      {t("chat.sendGallery")}
                    </button>
                  </>
                ) : selectedPlaceholder ? (
                  <span className={`rounded-full border px-3 py-1.5 text-xs font-medium shadow-sm ${placeholderStatusClass(selectedPlaceholder)}`}>
                    {placeholderStatusLabel(selectedPlaceholder, t)}
                  </span>
                ) : null}
              </div>
            </div>

            <div className="relative flex min-h-[320px] flex-1 items-center justify-center overflow-hidden rounded-3xl border border-slate-200 bg-white shadow-sm max-h-[72vh] dark:border-slate-600/80 dark:bg-[#121b2d] dark:shadow-[0_0_0_1px_rgba(139,92,246,0.10),0_24px_80px_rgba(0,0,0,0.35)] lg:min-h-[360px] lg:max-h-none">
              <div className="absolute inset-0 bg-[radial-gradient(#cbd5e1_1px,transparent_1px)] [background-size:20px_20px] dark:bg-[radial-gradient(rgba(148,163,184,0.26)_1px,transparent_1px)]" />
              <div className="absolute inset-x-0 top-0 z-10 flex items-center justify-between gap-3 px-5 py-4">
                {selectedRound ? (
                  <div className="min-w-0 max-w-[calc(100%-5.5rem)] truncate rounded-full bg-white/90 px-3 py-1.5 text-xs font-medium text-slate-600 shadow-sm ring-1 ring-slate-200 backdrop-blur dark:bg-slate-950/82 dark:text-slate-200 dark:ring-slate-700">
                    {formatDateTime(selectedRound.created_at)} · {selectedRound.model_name}
                  </div>
                ) : selectedPlaceholder ? (
                  <div className="min-w-0 max-w-[calc(100%-5.5rem)] truncate rounded-full bg-white/90 px-3 py-1.5 text-xs font-medium text-slate-600 shadow-sm ring-1 ring-slate-200 backdrop-blur dark:bg-slate-950/82 dark:text-slate-200 dark:ring-slate-700">
                    {placeholderStatusLabel(selectedPlaceholder, t)} · {formatImageSizeValue(selectedPlaceholder.size)}
                  </div>
                ) : (
                  <div className="rounded-full bg-white/90 px-3 py-1.5 text-xs font-medium text-slate-500 shadow-sm ring-1 ring-slate-200 backdrop-blur dark:border dark:border-violet-400/35 dark:bg-slate-950/82 dark:text-violet-100 dark:ring-violet-400/20">
                    {t("chat.waitingFirstResult")}
                  </div>
                )}
                {branchBaseRound ? (
                  <div className="inline-flex h-8 items-center gap-1.5 rounded-full bg-indigo-600 px-3 text-xs font-semibold text-white shadow-sm shadow-indigo-500/20 dark:bg-violet-500/20 dark:text-violet-100 dark:ring-1 dark:ring-violet-400/40">
                    <Layers3 size={13} />
                    {t("chat.baseSelected")}
                  </div>
                ) : null}
              </div>

              {selectedRound ? (
                <div className="relative z-0 flex h-full min-h-0 w-full items-center justify-center px-2 pb-2 pt-12 sm:px-3 sm:pb-3 sm:pt-14">
                  <button
                    type="button"
                    onClick={() => setPreviewRound(selectedRound)}
                    className="flex h-full w-full items-center justify-center rounded-2xl focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:focus-visible:ring-violet-400"
                    aria-label={t("chat.previewCurrent")}
                    title={t("chat.previewCurrent")}
                  >
                    <img
                      src={api.toApiUrl(selectedRound.generated_asset.download_url)}
                      alt={t("chat.currentResultAlt")}
                      decoding="async"
                      className="max-h-full max-w-full object-contain drop-shadow-2xl"
                    />
                  </button>
                </div>
              ) : selectedPlaceholder ? (
                <GenerationCanvasPlaceholder
                  candidate={selectedPlaceholder}
                  retrying={
                    retryGenerationTaskMutation.isPending &&
                    retryGenerationTaskMutation.variables?.taskId === selectedPlaceholder.task_id
                  }
                  cancelling={
                    cancelGenerationTaskMutation.isPending &&
                    cancelGenerationTaskMutation.variables?.taskId === selectedPlaceholder.task_id
                  }
                  onRetry={handleRetryGenerationTask}
                  onCancel={handleCancelGenerationTask}
                  t={t}
                />
              ) : (
                <div className="relative z-0 flex flex-col items-center gap-4 text-center text-slate-400 dark:text-slate-100">
                  <div className="flex h-16 w-16 items-center justify-center rounded-3xl bg-white shadow-sm ring-1 ring-slate-200 dark:bg-slate-950/86 dark:text-violet-200 dark:ring-violet-400/35">
                    <Sparkles size={28} />
                  </div>
                  <div>
                    <div className="text-sm font-semibold text-slate-600 dark:text-white">{t("chat.noResult")}</div>
                  </div>
                </div>
              )}
            </div>
            {selectedRound?.provider_notes.length ? (
              <div className="mt-2 flex flex-wrap gap-2 rounded-2xl border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-400/35 dark:bg-amber-500/10 dark:text-amber-200">
                {selectedRound.provider_notes.map((note) => (
                  <span key={note}>{note}</span>
                ))}
              </div>
            ) : selectedPlaceholder?.failure_reason ? (
              <div className="mt-2 rounded-2xl border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-400/35 dark:bg-red-500/10 dark:text-red-200">
                {selectedPlaceholder.failure_reason}
              </div>
            ) : selectedPlaceholder?.provider_notes.length ? (
              <div className="mt-2 flex flex-wrap gap-2 rounded-2xl border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-400/35 dark:bg-amber-500/10 dark:text-amber-200">
                {selectedPlaceholder.provider_notes.map((note) => (
                  <span key={note}>{note}</span>
                ))}
              </div>
            ) : null}
          </div>

          <div
            className="relative flex h-44 shrink-0 flex-col border-t border-slate-200 bg-white/95 px-3 py-2.5 shadow-[0_-8px_24px_rgba(15,23,42,0.04)] dark:border-slate-700/80 dark:bg-[#0f1726] dark:shadow-[0_-18px_40px_rgba(0,0,0,0.24)] lg:h-[var(--image-chat-history-panel-height)]"
            style={historyPanelStyle}
          >
            <button
              type="button"
              aria-label={t("chat.resizeHistory")}
              title={t("chat.resizeHistoryTitle")}
              onPointerDown={(event) => handlePanelResizeStart("history", event)}
              className="absolute inset-x-0 -top-1 z-20 hidden h-3 cursor-row-resize items-center justify-center transition-colors hover:bg-indigo-50/70 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:hover:bg-violet-500/15 lg:flex"
            >
              <span className="h-1 w-12 rounded-full bg-slate-300 dark:bg-slate-600" />
            </button>
            <div className="mb-2 flex items-center justify-between gap-3">
              <div>
                <div className="text-sm font-semibold text-slate-950 dark:text-white">{t("chat.history")}</div>
              </div>
              {branchBaseRound ? (
                <div className="rounded-full border border-indigo-200 bg-indigo-50 px-2 py-1 text-xs font-semibold text-indigo-700 dark:border-violet-400/40 dark:bg-violet-500/15 dark:text-violet-100">
                  {t("chat.clickHistoryBase")}
                </div>
              ) : null}
            </div>

            {historyBranches.length ? (
              <div className="flex min-h-0 flex-1 gap-3 overflow-x-auto pb-1" onWheel={handleHistoryWheelScroll}>
                {historyBranches.map((branch) => (
                  <HistoryBranchStrip
                    key={branch.id}
                    branch={branch}
                    selectedGeneratedAssetId={selectedGeneratedAssetId}
                    selectedTaskPlaceholderId={selectedTaskPlaceholderId}
                    branchBaseAssetId={branchBaseAssetId}
                    onSelectRound={(assetId) => {
                      setSelectedGeneratedAssetId(assetId);
                      setBranchBaseAssetId(assetId);
                      setSelectedTaskPlaceholderId(null);
                      setSuccessMessage("");
                      setErrorMessage("");
                    }}
                    onSelectPlaceholder={(placeholderId) => {
                      setSelectedTaskPlaceholderId(placeholderId);
                      setSelectedGeneratedAssetId(null);
                    }}
                    onPreviewPrompt={setPromptPreview}
                    t={t}
                  />
                ))}
              </div>
            ) : (
              <div className="flex min-h-0 flex-1 items-center justify-center rounded-2xl border border-dashed border-slate-200 bg-slate-50 text-sm text-slate-400 dark:border-slate-700 dark:bg-slate-950/40 dark:text-slate-500">
                {t("chat.resultsAppearHere")}
              </div>
            )}
          </div>
        </section>

        <aside
          className="relative flex w-full shrink-0 flex-col border-t border-slate-200 bg-white dark:border-slate-700/80 dark:bg-[#0f1726] dark:shadow-[-12px_0_36px_rgba(0,0,0,0.24)] dark:backdrop-blur-xl lg:w-[var(--image-chat-right-panel-width)] lg:border-l lg:border-t-0"
          style={rightPanelStyle}
        >
          <button
            type="button"
            aria-label={t("chat.resizeSettings")}
            title={t("chat.resizeSettingsTitle")}
            onPointerDown={(event) => handlePanelResizeStart("right", event)}
            className="absolute left-[-5px] top-0 z-20 hidden h-full w-3 cursor-col-resize items-center justify-center transition-colors hover:bg-indigo-50/70 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:hover:bg-violet-500/15 lg:flex"
          >
            <span className="h-12 w-1 rounded-full bg-slate-300 dark:bg-slate-600" />
          </button>
          <div className="min-h-0 flex-1 px-4 py-5 lg:overflow-y-auto lg:px-5">
            <div className="mb-5">
              <div className="mb-3 flex items-center justify-between gap-3">
                <div>
                  <div className="inline-flex items-center gap-1.5 text-sm font-semibold text-slate-950 dark:text-white">
                    <Settings size={15} /> {t("chat.generationSettings")}
                  </div>
                </div>
                <button
                  type="button"
                  onClick={() => setRenameEnabled((current) => !current)}
                  className="inline-flex h-8 items-center rounded-lg border border-slate-200 px-2.5 text-xs font-medium text-slate-600 transition-colors hover:border-slate-300 hover:text-slate-900 dark:border-slate-700 dark:bg-slate-950/60 dark:text-slate-300 dark:hover:border-violet-400/50 dark:hover:text-violet-100"
                >
                  <Pencil size={12} className="mr-1.5" /> {t("chat.rename")}
                </button>
              </div>
              <div className="flex gap-2">
                <input
                  value={titleDraft}
                  onChange={(event) => setTitleDraft(event.target.value)}
                  disabled={!renameEnabled || renameSessionMutation.isPending}
                  className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm text-slate-900 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-100 disabled:bg-slate-50 disabled:text-slate-500 dark:border-slate-700 dark:bg-slate-950/70 dark:text-slate-100 dark:focus:border-violet-400 dark:focus:ring-violet-400/20 dark:disabled:bg-slate-900 dark:disabled:text-slate-500"
                />
                {renameEnabled ? (
                  <button
                    type="button"
                    onClick={handleRename}
                    className="inline-flex items-center rounded-xl bg-indigo-600 px-3 py-2 text-sm font-semibold text-white transition-colors hover:bg-indigo-500 dark:bg-violet-500 dark:hover:bg-violet-400"
                    aria-label={t("chat.saveSessionName")}
                  >
                    {renameSessionMutation.isPending ? <Loader2 size={14} className="animate-spin" /> : <Save size={14} />}
                  </button>
                ) : null}
              </div>
            </div>

            <ImageGenerationSettingsTabs
              value={settingsTab}
              onChange={setSettingsTab}
              basic={
                <div className="space-y-4">
                  <ProductAssociationPanel
                    isProductMode={isProductMode}
                    product={productQuery.data}
                    products={products}
                    targetProductId={targetProductId}
                    sourceImage={sourceImage}
                    referenceImages={productReferenceImages}
                    selectedRound={selectedRound}
                    attachBusy={attachMutation.isPending}
                    deletingReferenceAssetId={
                      deleteProductReferenceMutation.isPending ? (deleteProductReferenceMutation.variables ?? null) : null
                    }
                    onTargetProductChange={setTargetProductId}
                    onDeleteReference={handleDeleteProductReference}
                    onAttach={handleAttach}
                    t={t}
                  />

                  <SessionReferencePanel
                    assets={sessionReferenceAssets}
                    selectedAssetIds={selectedReferenceAssetIds}
                    maxSelectedCount={maxSelectedReferenceCount}
                    uploadBusy={uploadReferenceMutation.isPending}
                    deletingAssetId={
                      deleteSessionReferenceMutation.isPending
                        ? (deleteSessionReferenceMutation.variables?.assetId ?? null)
                        : null
                    }
                    disabled={!selectedSessionId}
                    onFiles={handleUploadReferenceFiles}
                    onToggle={handleReferenceToggle}
                    onDelete={handleDeleteSessionReference}
                    t={t}
                  />

                  <div>
                    <label className="mb-2 block text-sm font-semibold text-slate-950 dark:text-white" htmlFor="image-chat-prompt">
                      {t("chat.prompt")}
                    </label>
                    <textarea
                      id="image-chat-prompt"
                      value={draft}
                      onChange={(event) => setDraft(event.target.value)}
                      rows={6}
                      placeholder={isProductMode ? t("chat.productPromptPlaceholder") : t("chat.freePromptPlaceholder")}
                      className="w-full resize-none rounded-2xl border border-slate-200 px-3 py-3 text-sm leading-6 text-slate-900 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-100 dark:border-slate-700 dark:bg-slate-950/70 dark:text-slate-100 dark:placeholder:text-slate-500 dark:focus:border-violet-400 dark:focus:ring-violet-400/20"
                    />
                  </div>

                  <ImageGenerationSettingsPanel
                    size={size}
                    sizeOptions={sizeOptions}
                    maxDimension={imageGenerationMaxDimension}
                    toolOptions={toolOptions}
                    allowedToolFields={imageToolAllowedFields}
                    generationCount={generationCount}
                    generationCountOptions={[1, 2, 3, 4]}
                    onSizeChange={setSize}
                    onToolOptionsChange={setToolOptions}
                    onGenerationCountChange={(count) => setGenerationCount(clampGenerationCount(count))}
                    showToolOptions={false}
                  />
                </div>
              }
              advanced={
                <ImageToolControls value={toolOptions} allowedFields={imageToolAllowedFields} onChange={setToolOptions} />
              }
            />

            <div className="space-y-4">
              {successMessage ? (
                <div className="rounded-xl border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm text-emerald-700 dark:border-emerald-400/35 dark:bg-emerald-500/10 dark:text-emerald-200">
                  {successMessage}
                </div>
              ) : null}
              {errorMessage ? (
                <div className="rounded-xl border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-400/35 dark:bg-red-500/10 dark:text-red-200">{errorMessage}</div>
              ) : null}
            </div>
          </div>

          <div className="fixed inset-x-0 bottom-0 z-40 border-t border-slate-200 bg-white/95 p-3 pb-[calc(env(safe-area-inset-bottom)+0.75rem)] shadow-[0_-8px_24px_rgba(15,23,42,0.10)] backdrop-blur dark:border-slate-800 dark:bg-slate-950/90 dark:shadow-[0_-18px_40px_rgba(0,0,0,0.32)] lg:sticky lg:inset-x-auto lg:bottom-0 lg:p-4">
            {baseRequirementMessage ? (
              <div className="mb-2 rounded-xl border border-amber-200 bg-amber-50 px-3 py-2 text-xs font-medium text-amber-700 dark:border-amber-400/35 dark:bg-amber-500/10 dark:text-amber-200">
                {baseRequirementMessage}
              </div>
            ) : null}
            <button
              type="button"
              onClick={handleGenerate}
              disabled={generateDisabled}
              className="inline-flex w-full items-center justify-center rounded-2xl bg-indigo-600 px-4 py-3.5 text-sm font-semibold text-white shadow-lg shadow-indigo-600/20 transition-colors hover:bg-indigo-500 disabled:opacity-60 dark:bg-gradient-to-r dark:from-indigo-500 dark:via-violet-500 dark:to-fuchsia-500 dark:shadow-violet-900/45 dark:ring-1 dark:ring-violet-300/35"
            >
              {generateMutation.isPending ? (
                <Loader2 size={15} className="mr-2 animate-spin" />
              ) : (
                <Sparkles size={15} className="mr-2" />
              )}
              {generateMutation.isPending
                ? t("chat.submitting")
                : generationCount > 1
                  ? t("chat.startGenerateCount", { count: generationCount })
                  : t("chat.startGenerate")}
            </button>
          </div>
        </aside>
      </main>
      {promptPreview ? (
        <PromptPreviewDialog preview={promptPreview} onClose={() => setPromptPreview(null)} />
      ) : null}
      {activePreviewRound ? (
        <GalleryImagePreviewDialog
          ariaLabel={t("chat.currentPreviewLabel")}
          imageUrl={api.toApiUrl(activePreviewRound.generated_asset.preview_url)}
          imageAlt={activePreviewRound.prompt || t("chat.currentResultAlt")}
          title={t("gallery.prompt")}
          subtitle={activePreviewRound.generated_asset.original_filename}
          body={activePreviewRound.prompt || t("gallery.noPrompt")}
          metadataRows={[
            { label: t("gallery.meta.size"), value: imageRoundSizeLabel(activePreviewRound, t) },
            {
              label: t("gallery.meta.model"),
              value:
                [activePreviewRound.provider_name, activePreviewRound.model_name].filter(Boolean).join(" / ") ||
                t("common.unknown"),
            },
            {
              label: t("gallery.meta.candidate"),
              value: `${activePreviewRound.candidate_index}/${activePreviewRound.candidate_count}`,
            },
            { label: t("chat.generatedAt"), value: formatDateTime(activePreviewRound.created_at) },
          ]}
          providerNotes={activePreviewRound.provider_notes}
          providerNotesTitle={t("gallery.providerNotes")}
          downloadUrl={activePreviewRound.generated_asset.download_url}
          downloadLabel={t("gallery.download")}
          closeLabel={t("gallery.closePreview")}
          onClose={() => setPreviewRound(null)}
        />
      ) : null}
    </div>
  );

}

function GenerationCanvasPlaceholder({
  candidate,
  retrying,
  cancelling,
  onRetry,
  onCancel,
  t,
}: {
  candidate: ImageHistoryPlaceholderCandidate;
  retrying: boolean;
  cancelling: boolean;
  onRetry: (task: ImageSessionGenerationTask) => void;
  onCancel: (task: ImageSessionGenerationTask) => void;
  t: ReturnType<typeof useI18n>["t"];
}) {
  const active = candidate.status === "queued" || candidate.status === "running";
  const failed = candidate.status === "failed";
  const cancelled = candidate.status === "cancelled";
  const retryable = isImageSessionGenerationTaskRetryable(candidate.task);
  const queueText = generationTaskQueueText(candidate.task, t);
  const retryMetadata = imageGenerationRetryMetadata(candidate.task);
  const nonRetryableReason = candidate.failure_reason ?? retryMetadata?.last_failure_reason;

  return (
    <div className="relative z-0 flex h-full min-h-0 w-full items-center justify-center px-6 pb-6 pt-16">
      <div className="flex max-w-md flex-col items-center text-center">
        <div
          className={`relative flex h-24 w-24 items-center justify-center rounded-3xl border shadow-sm ${
            failed
              ? "border-red-200 bg-red-50 text-red-600 dark:border-red-400/35 dark:bg-red-500/10 dark:text-red-200"
              : "border-indigo-100 bg-indigo-50 text-indigo-700 dark:border-violet-400/35 dark:bg-violet-500/14 dark:text-violet-100"
          }`}
        >
          {active ? <div className="absolute inset-2 rounded-3xl bg-indigo-200/70 opacity-70 blur-xl animate-pulse" /> : null}
          {active ? <Loader2 size={30} className="relative animate-spin" /> : <Sparkles size={30} className="relative" />}
        </div>
        <div className="mt-4 text-sm font-semibold text-slate-900">{placeholderStatusLabel(candidate, t)}</div>
        <div className="mt-1 text-xs text-slate-500">
          {t("chat.candidate", { index: candidate.candidate_index, count: candidate.candidate_count })} · {formatImageSizeValue(candidate.size)}
        </div>
        {queueText ? <div className="mt-3 max-w-sm text-xs leading-5 text-slate-500">{queueText}</div> : null}
        <div className="mt-4 line-clamp-3 max-w-sm rounded-xl border border-slate-200/80 bg-white/80 px-3 py-2 text-xs font-medium leading-5 text-[#334155] shadow-sm dark:border-slate-700/70 dark:bg-slate-950/75 dark:text-[#e2e8f0]">
          {candidate.prompt}
        </div>
        {isImageSessionGenerationTaskCancelable(candidate.task) ? (
          <button
            type="button"
            onClick={() => onCancel(candidate.task)}
            disabled={cancelling}
            className="mt-5 inline-flex items-center justify-center rounded-xl border border-red-200 bg-white px-4 py-2 text-sm font-semibold text-red-600 shadow-sm transition-colors hover:bg-red-50 disabled:opacity-60 dark:border-red-400/40 dark:bg-[#0b1220] dark:text-red-200 dark:hover:bg-red-500/12"
          >
            {cancelling ? <Loader2 size={15} className="mr-2 animate-spin" /> : <OctagonX size={15} className="mr-2" />}
            {t("chat.cancelGeneration")}
          </button>
        ) : null}
        {failed && retryable ? (
          <button
            type="button"
            onClick={() => onRetry(candidate.task)}
            disabled={retrying}
            className="mt-5 inline-flex items-center justify-center rounded-xl bg-red-600 px-4 py-2 text-sm font-semibold text-white shadow-sm shadow-red-500/20 transition-colors hover:bg-red-500 disabled:opacity-60"
          >
            {retrying ? <Loader2 size={15} className="mr-2 animate-spin" /> : <RotateCcw size={15} className="mr-2" />}
            {t("chat.retryGeneration")}
          </button>
        ) : failed ? (
          <div className="mt-5 max-w-sm rounded-xl border border-red-200 bg-white px-3 py-2 text-xs font-medium leading-5 text-red-500 dark:border-red-400/40 dark:bg-[#0b1220] dark:text-red-200">
            <div>{t("chat.notRetryable")}</div>
            {nonRetryableReason ? <div className="mt-1 text-red-500/80 dark:text-red-100/80">{nonRetryableReason}</div> : null}
          </div>
        ) : cancelled ? (
          <div className="mt-5 rounded-xl border border-slate-200 bg-white px-3 py-2 text-xs font-medium text-slate-500">
            {t("chat.taskCancelled")}
          </div>
        ) : null}
      </div>
    </div>
  );
}

function HistoryBranchStrip({
  branch,
  selectedGeneratedAssetId,
  selectedTaskPlaceholderId,
  branchBaseAssetId,
  onSelectRound,
  onSelectPlaceholder,
  onPreviewPrompt,
  t,
}: {
  branch: ImageHistoryBranch;
  selectedGeneratedAssetId: string | null;
  selectedTaskPlaceholderId: string | null;
  branchBaseAssetId: string | null;
  onSelectRound: (assetId: string) => void;
  onSelectPlaceholder: (placeholderId: string) => void;
  onPreviewPrompt: (preview: PromptPreview) => void;
  t: ReturnType<typeof useI18n>["t"];
}) {
  const depthOffset = Math.min(branch.depth, 4) * 18;
  const branchLabel = branch.base_asset_id ? t("chat.branch", { depth: branch.depth }) : t("chat.firstRound");

  return (
    <div
      className="relative flex h-full shrink-0 gap-2 rounded-2xl border border-slate-200 bg-slate-50/80 p-2 dark:border-slate-700/80 dark:bg-[#151f33]"
      style={{ marginLeft: depthOffset }}
    >
      {branch.depth > 0 ? (
        <div className="pointer-events-none absolute -left-3 top-1/2 h-px w-3 bg-slate-300 dark:bg-slate-700" />
      ) : null}
      <div className="flex w-28 shrink-0 flex-col justify-between rounded-xl bg-white p-2 text-xs text-slate-500 ring-1 ring-slate-200 dark:bg-[#0b1220] dark:text-slate-400 dark:ring-slate-600/80">
        <div>
          <div className="flex items-center gap-1.5 font-semibold text-slate-800 dark:text-slate-100">
            {branch.depth > 0 ? <Layers3 size={12} /> : <History size={12} />}
            {branchLabel}
          </div>
          <div className="mt-1">{t("chat.imageCount", { count: branch.candidates.length })}</div>
        </div>
        <button
          type="button"
          onClick={() =>
            onPreviewPrompt({
              title: branch.base_asset_id ? t("chat.branchPrompt") : t("chat.firstPrompt"),
              text: branch.prompt,
              meta: `${t("chat.imageCount", { count: branch.candidates.length })} · ${formatDateTime(branch.created_at)}`,
            })
          }
          className="line-clamp-3 rounded-md text-left text-[11px] leading-4 text-slate-400 transition-colors hover:text-indigo-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:text-slate-500 dark:hover:text-violet-200"
        >
          {branch.prompt}
        </button>
      </div>
      {branch.candidates.map((candidate) => (
        <HistoryCandidateCard
          key={candidate.id}
          candidate={candidate}
          selectedGeneratedAssetId={selectedGeneratedAssetId}
          selectedTaskPlaceholderId={selectedTaskPlaceholderId}
          branchBaseAssetId={branchBaseAssetId}
          onSelectRound={onSelectRound}
          onSelectPlaceholder={onSelectPlaceholder}
          t={t}
        />
      ))}
    </div>
  );
}

function HistoryCandidateCard({
  candidate,
  selectedGeneratedAssetId,
  selectedTaskPlaceholderId,
  branchBaseAssetId,
  onSelectRound,
  onSelectPlaceholder,
  t,
}: {
  candidate: ImageHistoryCandidate;
  selectedGeneratedAssetId: string | null;
  selectedTaskPlaceholderId: string | null;
  branchBaseAssetId: string | null;
  onSelectRound: (assetId: string) => void;
  onSelectPlaceholder: (placeholderId: string) => void;
  t: ReturnType<typeof useI18n>["t"];
}) {
  if (candidate.kind === "placeholder") {
    const active = candidate.id === selectedTaskPlaceholderId;
    const running = candidate.status === "queued" || candidate.status === "running";
    return (
      <div
        className={`group/card relative aspect-square h-full min-w-[7rem] shrink-0 overflow-hidden rounded-2xl border bg-white transition-all dark:bg-[#0b1220] ${
          active
            ? "border-indigo-400 ring-2 ring-indigo-200 dark:border-violet-400 dark:ring-violet-400/45"
            : "border-slate-200 hover:border-slate-300 dark:border-slate-700 dark:hover:border-violet-400/45"
        }`}
      >
        <button
          type="button"
          onClick={() => onSelectPlaceholder(candidate.id)}
          className="flex h-full w-full flex-col justify-between p-2 text-left"
        >
          <div className="flex items-center justify-between gap-2">
            <span className={`rounded-full border px-1.5 py-0.5 text-[10px] font-semibold ${placeholderStatusClass(candidate)}`}>
              {candidate.candidate_index}/{candidate.candidate_count}
            </span>
            {active ? <Check size={13} className="shrink-0 text-indigo-600" /> : null}
          </div>
          <div className="flex flex-1 items-center justify-center">
            <div className="relative flex h-12 w-12 items-center justify-center rounded-2xl bg-slate-50 text-indigo-600 ring-1 ring-slate-200 dark:bg-violet-500/12 dark:text-violet-200 dark:ring-violet-400/30">
              {running ? <Loader2 size={19} className="animate-spin" /> : <Sparkles size={19} />}
            </div>
          </div>
          <div>
            <div className="truncate text-[11px] font-semibold text-slate-700 dark:text-slate-100">{placeholderStatusLabel(candidate, t)}</div>
            <div className="mt-0.5 line-clamp-2 text-[10px] leading-3 text-slate-400 dark:text-slate-500">{candidate.prompt}</div>
          </div>
        </button>
      </div>
    );
  }

  const round = candidate.round;
  const active = round.generated_asset.id === selectedGeneratedAssetId;
  const asBase = round.generated_asset.id === branchBaseAssetId;
  return (
    <div
        className={`group/card relative aspect-square h-full min-w-[7rem] shrink-0 overflow-hidden rounded-2xl border bg-white transition-all dark:bg-[#0b1220] ${
        active
          ? "border-indigo-400 ring-2 ring-indigo-200 dark:border-violet-400 dark:ring-violet-400/45"
          : "border-slate-200 hover:border-slate-300 dark:border-slate-700 dark:hover:border-violet-400/45"
      } ${asBase ? "shadow-md shadow-indigo-200/70 dark:shadow-violet-950/40" : "shadow-sm shadow-slate-200/60 dark:shadow-slate-950/30"}`}
    >
      <button type="button" onClick={() => onSelectRound(round.generated_asset.id)} className="block h-full w-full text-left">
        <img
          src={api.toApiUrl(round.generated_asset.thumbnail_url)}
          alt={round.prompt}
          loading="lazy"
          decoding="async"
          className="h-full w-full object-cover"
        />
        <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-slate-950/85 via-slate-950/35 to-transparent p-1.5 pt-8 text-white">
          <div className="flex items-center justify-between gap-2 text-[11px] font-medium">
            <span className="min-w-0 truncate">
              {round.candidate_count > 1 ? `${round.candidate_index}/${round.candidate_count}` : imageRoundSizeLabel(round, t)}
            </span>
            {active ? <Check size={13} className="shrink-0" /> : null}
          </div>
        </div>
      </button>
      {asBase ? (
        <div className="absolute left-1.5 top-1.5 max-w-[calc(100%-2.75rem)] truncate rounded-full bg-indigo-600 px-1.5 py-0.5 text-[10px] font-semibold text-white shadow-sm dark:bg-violet-500/85 dark:ring-1 dark:ring-violet-200/30">
          {t("chat.baseImage")}
        </div>
      ) : null}
    </div>
  );
}

function SessionReferencePanel({
  assets,
  selectedAssetIds,
  maxSelectedCount,
  uploadBusy,
  deletingAssetId,
  disabled,
  onFiles,
  onToggle,
  onDelete,
  t,
}: {
  assets: ImageSessionAsset[];
  selectedAssetIds: string[];
  maxSelectedCount: number;
  uploadBusy: boolean;
  deletingAssetId: string | null;
  disabled: boolean;
  onFiles: (files: File[]) => void;
  onToggle: (assetId: string, checked: boolean) => void;
  onDelete: (assetId: string) => void;
  t: ReturnType<typeof useI18n>["t"];
}) {
  return (
    <div className="rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-700/80 dark:bg-[#151f33]">
      <div className="mb-2 text-sm font-semibold text-slate-950 dark:text-white">{t("chat.sessionReferences")}</div>
      <ImageDropZone
        ariaLabel={t("chat.uploadSessionReference")}
        multiple
        disabled={disabled || uploadBusy}
        className="flex cursor-pointer items-center justify-center rounded-xl border border-dashed border-slate-300 bg-slate-50 px-4 py-4 text-sm text-slate-600 transition-colors hover:border-indigo-300 hover:bg-indigo-50/40 dark:border-slate-600/80 dark:bg-[#0b1220] dark:text-slate-300 dark:hover:border-violet-400/55 dark:hover:bg-violet-500/10"
        onFiles={onFiles}
      >
        {({ isDragging }) => (
          <>
            {uploadBusy ? <Loader2 size={16} className="mr-2 animate-spin" /> : <ImagePlus size={16} className="mr-2" />}
            {isDragging ? t("chat.dropUpload") : t("chat.uploadReference")}
          </>
        )}
      </ImageDropZone>
      <div className="mt-2 text-xs leading-5 text-slate-500 dark:text-slate-400">
        {t("chat.selectedReferences", { selected: selectedAssetIds.length, max: maxSelectedCount })}
      </div>
      {assets.length ? (
        <div className="mt-3 grid grid-cols-4 gap-2">
          {assets.map((asset) => {
            const deleting = deletingAssetId === asset.id;
            const selected = selectedAssetIds.includes(asset.id);
            const selectionLimitReached = !selected && selectedAssetIds.length >= maxSelectedCount;
            return (
              <div
                key={asset.id}
                className={`group relative overflow-hidden rounded-xl border bg-slate-50 dark:bg-[#0b1220] ${
                  selected
                    ? "border-indigo-500 ring-2 ring-indigo-100 dark:border-violet-400 dark:ring-violet-400/45"
                    : "border-slate-200 dark:border-slate-700"
                }`}
              >
                <a href={api.toApiUrl(asset.preview_url)} target="_blank" rel="noreferrer" title={asset.original_filename}>
                  <img
                    src={api.toApiUrl(asset.thumbnail_url)}
                    alt={asset.original_filename}
                    loading="lazy"
                    decoding="async"
                    className="h-20 w-full object-cover"
                  />
                </a>
                <label className="absolute bottom-1 left-1 inline-flex h-6 w-6 items-center justify-center rounded-md bg-white/95 text-slate-700 shadow-sm ring-1 ring-slate-200 dark:bg-slate-950/90 dark:text-violet-100 dark:ring-violet-400/35">
                  <input
                    type="checkbox"
                    checked={selected}
                    disabled={selectionLimitReached}
                    onChange={(event) => onToggle(asset.id, event.target.checked)}
                    aria-label={t("chat.useReference")}
                    className="h-3 w-3 rounded border-slate-300 text-indigo-600 focus:ring-indigo-500"
                  />
                  <span className="sr-only">{t("chat.useReference")}</span>
                </label>
                <button
                  type="button"
                  aria-label={t("chat.deleteSessionReference")}
                  onClick={() => onDelete(asset.id)}
                  disabled={deleting}
                  className="absolute right-1 top-1 inline-flex h-7 w-7 items-center justify-center rounded-lg bg-white/90 text-slate-500 opacity-100 shadow-sm ring-1 ring-slate-200 transition-colors hover:text-red-600 disabled:opacity-60 dark:bg-slate-950/90 dark:text-slate-300 dark:ring-slate-700 dark:hover:text-red-300 md:opacity-0 md:group-hover:opacity-100"
                >
                  {deleting ? <Loader2 size={13} className="animate-spin" /> : <Trash2 size={13} />}
                </button>
              </div>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}

function ProductAssociationPanel({
  isProductMode,
  product,
  products,
  targetProductId,
  sourceImage,
  referenceImages,
  selectedRound,
  attachBusy,
  deletingReferenceAssetId,
  onTargetProductChange,
  onDeleteReference,
  onAttach,
  t,
}: {
  isProductMode: boolean;
  product: ProductDetail | undefined;
  products: ProductSummary[];
  targetProductId: string;
  sourceImage: SourceAsset | null;
  referenceImages: SourceAsset[];
  selectedRound: ImageSessionRound | null;
  attachBusy: boolean;
  deletingReferenceAssetId: string | null;
  onTargetProductChange: (value: string) => void;
  onDeleteReference: (assetId: string) => void;
  onAttach: (target: "reference" | "main_source") => void;
  t: ReturnType<typeof useI18n>["t"];
}) {
  const saveDisabled = attachBusy || !selectedRound || (!isProductMode && !targetProductId);

  return (
    <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4 dark:border-slate-700/80 dark:bg-[#151f33]">
      <div className="mb-3 text-sm font-semibold text-zinc-900 dark:text-white">{t("chat.saveToProduct")}</div>
      {isProductMode ? (
        product ? (
          <div className="grid grid-cols-[88px_minmax(0,1fr)] gap-3">
            <ProductThumbnail sourceImage={sourceImage} alt={product.name} />
            <div className="min-w-0 self-center">
              <div className="truncate text-sm font-medium text-zinc-900 dark:text-slate-100">{product.name}</div>
              <div className="mt-1 text-xs text-zinc-500 dark:text-slate-400">{t("chat.productReferenceCount", { count: referenceImages.length })}</div>
            </div>
          </div>
        ) : (
          <div className="flex justify-center py-6 text-zinc-400">
            <Loader2 size={16} className="animate-spin" />
          </div>
        )
      ) : (
        <label className="block">
          <span className="mb-1.5 block text-xs font-semibold text-slate-700 dark:text-slate-200">{t("chat.targetProduct")}</span>
          <select
            value={targetProductId}
            onChange={(event) => onTargetProductChange(event.target.value)}
            className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm text-zinc-900 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-100 dark:border-slate-700 dark:bg-slate-950/70 dark:text-slate-100 dark:focus:border-violet-400 dark:focus:ring-violet-400/20"
          >
            {products.length ? null : <option value="">{t("chat.noProducts")}</option>}
            {products.map((item) => (
              <option key={item.id} value={item.id}>
                {item.name}
              </option>
            ))}
          </select>
        </label>
      )}

      {referenceImages.length ? (
        <div className="mt-3 grid grid-cols-4 gap-2">
          {referenceImages.slice(0, 4).map((asset) => {
            const deleting = deletingReferenceAssetId === asset.id;
            return (
              <div key={asset.id} className="group relative overflow-hidden rounded-md border border-zinc-200 bg-white dark:border-slate-700 dark:bg-slate-950/70">
                <a href={api.toApiUrl(asset.preview_url)} target="_blank" rel="noreferrer" title={asset.original_filename}>
                  <img
                    src={api.toApiUrl(asset.thumbnail_url)}
                    alt={asset.original_filename}
                    loading="lazy"
                    decoding="async"
                    className="h-16 w-full object-cover"
                  />
                </a>
                <button
                  type="button"
                  aria-label={t("chat.deleteProductReference")}
                  onClick={() => onDeleteReference(asset.id)}
                  disabled={deleting}
                  className="absolute right-1 top-1 inline-flex h-6 w-6 items-center justify-center rounded bg-white/90 text-zinc-500 opacity-100 shadow-sm ring-1 ring-zinc-200 transition-colors hover:text-red-600 disabled:opacity-60 dark:bg-slate-950/90 dark:text-slate-300 dark:ring-slate-700 dark:hover:text-red-300 md:opacity-0 md:group-hover:opacity-100"
                >
                  {deleting ? <Loader2 size={12} className="animate-spin" /> : <Trash2 size={12} />}
                </button>
              </div>
            );
          })}
        </div>
      ) : null}

      <div className="mt-4 border-t border-slate-200 pt-3 dark:border-slate-800">
        {selectedRound ? (
          <div className="mb-2 text-[11px] leading-5 text-slate-500 dark:text-slate-400">
            {t("chat.selectedCandidate", { size: formatImageSizeValue(selectedRound.size) })}
          </div>
        ) : (
          <div className="mb-2 rounded-xl border border-dashed border-slate-200 bg-white px-3 py-2 text-center text-sm text-slate-400 dark:border-slate-700 dark:bg-slate-950/45 dark:text-slate-500">
            {t("chat.selectHistoryFirst")}
          </div>
        )}
        <div className="grid gap-2">
          <button
            type="button"
            onClick={() => onAttach("reference")}
            disabled={saveDisabled}
            className="inline-flex items-center justify-center rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm font-medium text-slate-700 transition-colors hover:border-slate-300 hover:text-slate-950 disabled:opacity-60 dark:border-slate-700 dark:bg-slate-950/70 dark:text-slate-200 dark:hover:border-violet-400/55 dark:hover:text-violet-100"
          >
            {attachBusy ? <Loader2 size={14} className="mr-2 animate-spin" /> : <Check size={14} className="mr-2" />}
            {isProductMode ? t("chat.addReference") : t("chat.saveAsReference")}
          </button>
          {isProductMode ? (
            <button
              type="button"
              onClick={() => onAttach("main_source")}
              disabled={saveDisabled}
              className="inline-flex items-center justify-center rounded-xl bg-slate-900 px-3 py-2 text-sm font-semibold text-white transition-colors hover:bg-slate-800 disabled:opacity-60 dark:bg-violet-500/20 dark:text-violet-100 dark:ring-1 dark:ring-violet-400/35 dark:hover:bg-violet-500/30"
            >
              {attachBusy ? <Loader2 size={14} className="mr-2 animate-spin" /> : <ImageIcon size={14} className="mr-2" />}
              {t("chat.setMainSource")}
            </button>
          ) : null}
        </div>
      </div>
    </div>
  );
}

function ProductThumbnail({ sourceImage, alt }: { sourceImage: SourceAsset | null; alt: string }) {
  return (
    <div className="overflow-hidden rounded-lg border border-zinc-200 bg-white dark:border-slate-700 dark:bg-slate-950/70">
      {sourceImage ? (
        <img src={api.toApiUrl(sourceImage.thumbnail_url)} alt={alt} decoding="async" className="h-24 w-full object-cover" />
      ) : (
        <div className="flex h-24 items-center justify-center text-zinc-300 dark:text-slate-500">
          <ImageIcon size={20} />
        </div>
      )}
    </div>
  );
}
