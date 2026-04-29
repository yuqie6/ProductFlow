import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Check,
  Download,
  GalleryHorizontalEnd,
  History,
  Image as ImageIcon,
  Layers3,
  Loader2,
  MessagesSquare,
  Pencil,
  Plus,
  Save,
  Settings,
  Sparkles,
  Trash2,
} from "lucide-react";
import { useNavigate, useParams } from "react-router-dom";

import { ImageSizePicker } from "../components/ImageSizePicker";
import { ImageToolControls } from "../components/ImageToolControls";
import { PromptPreviewDialog, type PromptPreview } from "../components/PromptPreviewDialog";
import { TopNav } from "../components/TopNav";
import { api, ApiError } from "../lib/api";
import { formatDateTime } from "../lib/format";
import { DEFAULT_IMAGE_TOOL_ALLOWED_FIELDS } from "../lib/imageToolOptions";
import {
  DEFAULT_IMAGE_GENERATION_MAX_DIMENSION,
  buildImageSizeOptions,
  formatImageSizeValue,
} from "../lib/imageSizes";
import {
  buildImageGenerationSubmitSignature,
  buildImageSessionHistoryTree,
  clampGenerationCount,
  compactImageToolOptions,
  findImageGenerationTaskPlaceholderRound,
  findImageHistoryPlaceholder,
  getImageGenerationTaskPlaceholderId,
  isImageSessionGenerationTaskActive,
  mergeImageSessionStatusIntoDetail,
  requiresImageSessionGenerationBase,
  selectVisibleGenerationTasks,
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
  ImageSessionRound,
  ImageSessionGenerationTask,
  ImageSessionListResponse,
  ImageSessionStatus,
  ImageToolOptions,
  ProductDetail,
  ProductSummary,
  SourceAsset,
} from "../lib/types";

const DELETION_DISABLED_MESSAGE = "删除功能已关闭，请联系管理员";
const DUPLICATE_GENERATION_SUBMIT_WINDOW_MS = 1800;

function generationTaskLabel(task: ImageSessionGenerationTask) {
  if (task.status === "queued") {
    return task.queue_position ? `排队中 · 第 ${task.queue_position} 位` : "排队中";
  }
  if (task.status === "running") {
    const total = task.generation_count || 1;
    const current = task.active_candidate_index ?? Math.min(task.completed_candidates + 1, total);
    return `生成中 · ${task.completed_candidates}/${total} 已完成 · 当前第 ${current} 张`;
  }
  if (task.status === "failed") {
    return "生成失败";
  }
  return "已完成";
}

function generationTaskQueueText(task: ImageSessionGenerationTask) {
  if (task.status === "queued") {
    const ahead = task.queued_ahead_count ?? 0;
    const position = task.queue_position ? `当前第 ${task.queue_position} 位` : "当前排队中";
    return `前方 ${ahead} 个，${position}；全局活跃 ${task.queue_active_count}/${task.queue_max_concurrent_tasks}。`;
  }
  if (task.status === "running") {
    const providerStatus = task.provider_response_status ? `供应商状态 ${task.provider_response_status}；` : "";
    return `${providerStatus}最近进度 ${task.progress_updated_at ? formatDateTime(task.progress_updated_at) : "刚开始"}；全局运行 ${task.queue_running_count} 个，排队 ${task.queue_queued_count} 个。`;
  }
  return "";
}

function imageRoundSizeLabel(round: ImageSessionRound) {
  if (round.actual_size && round.actual_size !== round.size) {
    return `实际 ${round.actual_size} · 请求 ${round.size}`;
  }
  return round.actual_size ?? round.size;
}

function placeholderStatusLabel(candidate: ImageHistoryPlaceholderCandidate) {
  if (candidate.status === "queued") {
    return candidate.task.queue_position ? `排队中 · 第 ${candidate.task.queue_position} 位` : "排队中";
  }
  if (candidate.status === "running") {
    return `生成中 · ${candidate.candidate_index}/${candidate.candidate_count}`;
  }
  if (candidate.status === "completed") {
    return "已完成，刷新中";
  }
  if (candidate.status === "failed") {
    return "生成失败";
  }
  return "已完成";
}

function placeholderStatusClass(candidate: ImageHistoryPlaceholderCandidate) {
  if (candidate.status === "failed") {
    return "border-red-200 bg-red-50 text-red-700";
  }
  if (candidate.status === "queued") {
    return "border-amber-200 bg-amber-50 text-amber-700";
  }
  if (candidate.status === "completed") {
    return "border-emerald-200 bg-emerald-50 text-emerald-700";
  }
  return "border-indigo-200 bg-indigo-50 text-indigo-700";
}

function taskMatchesSubmitPayload(task: ImageSessionGenerationTask, payload: ImageGenerationSubmitPayload) {
  return (
    buildImageGenerationSubmitSignature({
      prompt: task.prompt,
      size: task.size,
      base_asset_id: task.base_asset_id,
      selected_reference_asset_ids: task.selected_reference_asset_ids,
      generation_count: task.generation_count,
      tool_options: task.tool_options,
    }) === buildImageGenerationSubmitSignature(payload)
  );
}

function selectSubmittedTaskPlaceholderId(
  tasks: ImageSessionGenerationTask[],
  payload: ImageGenerationSubmitPayload,
): string | null {
  const newestTasks = [...tasks].sort((left, right) => Date.parse(right.created_at) - Date.parse(left.created_at));
  const task =
    newestTasks.find((item) => taskMatchesSubmitPayload(item, payload)) ??
    newestTasks.find(isImageSessionGenerationTaskActive) ??
    newestTasks[0];
  if (!task) {
    return null;
  }
  const candidateIndex = Math.min(
    clampGenerationCount(task.generation_count || 1),
    task.active_candidate_index ?? Math.max(1, task.completed_candidates + 1),
  );
  return getImageGenerationTaskPlaceholderId(task, candidateIndex);
}

export function ImageChatPage() {
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
  const [generationCount, setGenerationCount] = useState(1);
  const [draft, setDraft] = useState("");
  const [size, setSize] = useState("1024x1024");
  const [toolOptions, setToolOptions] = useState<ImageToolOptions>({});
  const [settingsTab, setSettingsTab] = useState<"basic" | "advanced">("basic");
  const [titleDraft, setTitleDraft] = useState("");
  const [renameEnabled, setRenameEnabled] = useState(false);
  const [targetProductId, setTargetProductId] = useState("");
  const [promptPreview, setPromptPreview] = useState<PromptPreview | null>(null);
  const [errorMessage, setErrorMessage] = useState("");
  const [successMessage, setSuccessMessage] = useState("");

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

  useEffect(() => {
    if (!isProductMode && products.length && !targetProductId) {
      setTargetProductId(products[0].id);
    }
  }, [isProductMode, products, targetProductId]);

  const createSessionMutation = useMutation({
    mutationFn: () => api.createImageSession(productId ? { product_id: productId } : {}),
    onSuccess: async (imageSession) => {
      setSelectedSessionId(imageSession.id);
      setBranchBaseAssetId(null);
      setSelectedGeneratedAssetId(null);
      setSelectedTaskPlaceholderId(null);
      queryClient.setQueryData(["image-session", imageSession.id], imageSession);
      await queryClient.invalidateQueries({ queryKey: ["image-sessions", productId ?? "standalone"] });
      setErrorMessage("");
    },
    onError: (error) => {
      setErrorMessage(error instanceof ApiError ? error.detail : "创建会话失败");
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
      setSelectedGeneratedAssetId(null);
      setSelectedTaskPlaceholderId(null);
      setBranchBaseAssetId(null);
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
  const visibleGenerationTasks = useMemo(
    () => selectVisibleGenerationTasks(imageSession?.generation_tasks ?? []),
    [imageSession],
  );
  const requiresGenerationBase = requiresImageSessionGenerationBase(
    imageSession?.rounds ?? [],
    imageSession?.generation_tasks ?? [],
  );
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
    const selectedPlaceholderStillExists = Boolean(
      selectedTaskPlaceholderId && findImageHistoryPlaceholder(historyBranches, selectedTaskPlaceholderId),
    );
    const selectedPlaceholderReplacementRound =
      selectedTaskPlaceholderId && !selectedPlaceholderStillExists
        ? findImageGenerationTaskPlaceholderRound(
            imageSession.rounds,
            imageSession.generation_tasks,
            selectedTaskPlaceholderId,
          )
        : null;
    const selectedPlaceholderWasReplaced = Boolean(selectedTaskPlaceholderId && !selectedPlaceholderStillExists);
    const selectedCompletedAssetId =
      !selectedTaskPlaceholderId &&
      selectedGeneratedAssetId &&
      imageSession.rounds.some((round) => round.generated_asset.id === selectedGeneratedAssetId)
        ? selectedGeneratedAssetId
        : null;
    if (selectedTaskPlaceholderId && !selectedPlaceholderStillExists) {
      setSelectedTaskPlaceholderId(null);
      setSelectedGeneratedAssetId(
        selectedPlaceholderReplacementRound?.generated_asset.id ?? imageSession.rounds.at(-1)?.generated_asset.id ?? null,
      );
    } else if (
      !selectedTaskPlaceholderId &&
      (!selectedGeneratedAssetId || !imageSession.rounds.some((round) => round.generated_asset.id === selectedGeneratedAssetId))
    ) {
      setSelectedGeneratedAssetId(imageSession.rounds.at(-1)?.generated_asset.id ?? null);
    }
    if (branchBaseAssetId && !imageSession.rounds.some((round) => round.generated_asset.id === branchBaseAssetId)) {
      setBranchBaseAssetId(null);
    }
    if (selectedCompletedAssetId && branchBaseAssetId !== selectedCompletedAssetId) {
      setBranchBaseAssetId(selectedCompletedAssetId);
    }
    if (
      pendingGeneratedRoundCountRef.current !== null &&
      imageSession.rounds.length > pendingGeneratedRoundCountRef.current
    ) {
      pendingGeneratedRoundCountRef.current = null;
      if (!selectedTaskPlaceholderId || selectedPlaceholderWasReplaced) {
        setSelectedTaskPlaceholderId(null);
        if (!selectedPlaceholderReplacementRound) {
          setSelectedGeneratedAssetId(imageSession.rounds.at(-1)?.generated_asset.id ?? null);
        }
      }
      setSuccessMessage("新候选已生成");
      setErrorMessage("");
    }
  }, [
    branchBaseAssetId,
    historyBranches,
    imageSession,
    selectedGeneratedAssetId,
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

  const branchBaseRound = useMemo(() => {
    if (!imageSession?.rounds.length || !branchBaseAssetId) {
      return null;
    }
    return imageSession.rounds.find((round) => round.generated_asset.id === branchBaseAssetId) ?? null;
  }, [branchBaseAssetId, imageSession]);
  const baseRequirementMessage =
    requiresGenerationBase && !branchBaseRound ? "请选择一张已完成历史图作为本轮基图" : "";

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
      setSuccessMessage("会话名称已更新");
      setErrorMessage("");
    },
    onError: (error) => {
      setErrorMessage(error instanceof ApiError ? error.detail : "会话重命名失败");
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
        setSelectedGeneratedAssetId(null);
        setSelectedTaskPlaceholderId(null);
        setBranchBaseAssetId(null);
        if (!remainingSessions.length) {
          autoCreateTriggered.current = false;
        }
      }
      await queryClient.invalidateQueries({ queryKey: ["image-sessions", productId ?? "standalone"] });
      setSuccessMessage("会话已删除");
      setErrorMessage("");
    },
    onError: (error) => {
      setErrorMessage(error instanceof ApiError ? error.detail : "会话删除失败");
    },
  });

  const generateMutation = useMutation({
    mutationFn: (payload: ImageGenerationSubmitPayload) => api.generateImageSessionRound(selectedSessionId!, payload),
    onSuccess: (updated, variables) => {
      queryClient.setQueryData(["image-session", updated.id], updated);
      void queryClient.invalidateQueries({ queryKey: ["image-sessions", productId ?? "standalone"] });
      const placeholderId = selectSubmittedTaskPlaceholderId(updated.generation_tasks, variables);
      if (placeholderId) {
        setSelectedTaskPlaceholderId(placeholderId);
        setSelectedGeneratedAssetId(null);
      }
      setDraft("");
      setSuccessMessage(
        variables.generation_count > 1 ? `已提交生成任务 · ${variables.generation_count} 张候选` : "已提交生成任务",
      );
      setErrorMessage("");
    },
    onError: (error, variables) => {
      const signature = buildImageGenerationSubmitSignature(variables);
      if (duplicateSubmitGuardRef.current?.signature === signature) {
        duplicateSubmitGuardRef.current = null;
      }
      setErrorMessage(error instanceof ApiError ? error.detail : "生成失败");
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
      setErrorMessage(error instanceof ApiError ? error.detail : "保存到商品失败");
    },
  });

  const saveGalleryMutation = useMutation({
    mutationFn: (assetId: string) => api.saveGalleryEntry(assetId),
    onSuccess: async () => {
      setSuccessMessage("已保存至画廊");
      setErrorMessage("");
      await queryClient.invalidateQueries({ queryKey: ["gallery"] });
    },
    onError: (error) => {
      setErrorMessage(error instanceof ApiError ? error.detail : "保存到画廊失败");
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
      setSuccessMessage("商品参考图已删除");
      setErrorMessage("");
    },
    onError: (error) => {
      setErrorMessage(error instanceof ApiError ? error.detail : "商品参考图删除失败");
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
    const payload: ImageGenerationSubmitPayload = {
      prompt,
      size,
      base_asset_id: requiresGenerationBase ? branchBaseAssetId : null,
      selected_reference_asset_ids: [],
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
      setErrorMessage("相同任务刚刚提交，稍等片刻即可在历史记录中查看状态。");
      return;
    }
    duplicateSubmitGuardRef.current = { signature, submittedAt: now };
    pendingGeneratedRoundCountRef.current = imageSession?.rounds.length ?? 0;
    generateMutation.mutate(payload);
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
      setErrorMessage("请先选择要保存到的商品");
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
      setErrorMessage(DELETION_DISABLED_MESSAGE);
      return;
    }
    if (!window.confirm("删除这个会话？会话里的参考图和生成记录都会移除，已保存到商品的图片不受影响。")) {
      return;
    }
    deleteSessionMutation.mutate(sessionId);
  }

  function handleDeleteProductReference(assetId: string) {
    if (deleteProductReferenceMutation.isPending) {
      return;
    }
    if (!window.confirm("删除这张商品参考图？后续生成将不再参考它。")) {
      return;
    }
    deleteProductReferenceMutation.mutate(assetId);
  }

  return (
    <div className="flex min-h-screen flex-col bg-slate-100 text-slate-900 lg:h-screen lg:overflow-hidden">
      <TopNav
        breadcrumbs={isProductMode ? `${productQuery.data?.name ?? "商品"} / 文/图生图` : "文/图生图"}
        onHome={() => navigate(isProductMode && productId ? `/products/${productId}` : "/products")}
        onLogout={() => logoutMutation.mutate()}
      />

      <main className="flex flex-1 flex-col pb-28 lg:min-h-0 lg:flex-row lg:overflow-hidden lg:pb-0">
        <aside className="flex w-full shrink-0 flex-col border-b border-slate-200 bg-white/95 lg:w-72 lg:border-b-0 lg:border-r">
          <div className="border-b border-slate-200 px-4 py-4">
            <div className="flex items-center justify-between gap-3">
              <div>
                <div className="text-sm font-semibold text-slate-950">会话列表</div>
                <div className="mt-1 text-xs text-slate-500">{sessionItems.length} 个</div>
              </div>
              <button
                type="button"
                onClick={() => createSessionMutation.mutate()}
                disabled={createSessionMutation.isPending}
                className="inline-flex h-9 w-9 items-center justify-center rounded-xl bg-indigo-600 text-white shadow-sm shadow-indigo-500/20 transition-colors hover:bg-indigo-500 disabled:opacity-60"
                aria-label="新建会话"
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
                        ? "border-indigo-300 bg-indigo-50 shadow-sm shadow-indigo-100 ring-1 ring-indigo-200/80"
                        : "border-slate-200 bg-white hover:border-slate-300 hover:bg-slate-50"
                    }`}
                  >
                    <button
                      type="button"
                      onClick={() => {
                        setSelectedSessionId(item.id);
                        setSelectedGeneratedAssetId(null);
                        setSelectedTaskPlaceholderId(null);
                        setBranchBaseAssetId(null);
                        setSuccessMessage("");
                        setErrorMessage("");
                      }}
                      className="flex w-full items-center gap-3 p-2.5 pr-10 text-left"
                    >
                      <div className="relative flex h-16 w-16 shrink-0 items-center justify-center overflow-hidden rounded-xl bg-slate-100 text-slate-400 ring-1 ring-slate-200">
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
                        {active ? <div className="absolute inset-0 ring-2 ring-inset ring-indigo-500/60" /> : null}
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className={`truncate text-sm font-semibold ${active ? "text-indigo-950" : "text-slate-900"}`}>
                          {item.title}
                        </div>
                        <div className="mt-1 flex items-center gap-1.5 text-[11px] text-slate-500">
                          <History size={11} />
                          <span>{item.rounds_count} 轮</span>
                        </div>
                        <div className="mt-0.5 truncate text-[11px] text-slate-400">{formatDateTime(item.updated_at)}</div>
                      </div>
                    </button>
                    <button
                      type="button"
                      aria-label="删除会话"
                      onClick={() => handleDeleteSession(item.id)}
                      disabled={deleting || !deletionEnabled}
                      title={deletionEnabled ? "删除会话" : DELETION_DISABLED_MESSAGE}
                      className="absolute right-2 top-2 inline-flex h-7 w-7 items-center justify-center rounded-lg bg-white/95 text-slate-400 opacity-100 shadow-sm ring-1 ring-slate-200 transition-colors hover:text-red-600 disabled:opacity-60 md:opacity-0 md:group-hover:opacity-100"
                    >
                      {deleting ? <Loader2 size={13} className="animate-spin" /> : <Trash2 size={13} />}
                    </button>
                  </div>
                );
              })
            ) : (
              <div className="rounded-2xl border border-dashed border-slate-200 p-5 text-center text-sm text-slate-500">
                暂无会话
              </div>
            )}
          </div>
        </aside>

        <section className="flex min-w-0 flex-col bg-slate-100 lg:min-h-0 lg:flex-1 lg:overflow-hidden">
          <div className="flex flex-col p-3 pb-2 lg:min-h-0 lg:flex-1">
            <div className="mb-3 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <div className="min-w-0">
                <div className="flex items-center gap-2 text-xs font-medium text-slate-500">
                  <span className="inline-flex h-7 items-center rounded-full bg-white px-3 shadow-sm ring-1 ring-slate-200">
                    当前结果
                  </span>
                  {branchBaseRound ? (
                    <span className="inline-flex h-7 items-center gap-1 rounded-full bg-indigo-600 px-3 text-white shadow-sm shadow-indigo-500/20">
                      <Layers3 size={12} /> 已选基图
                    </span>
                  ) : null}
                </div>
                <h1 className="mt-2 text-xl font-semibold tracking-tight text-slate-950">
                  {imageSession?.title ?? "文/图生图工作台"}
                </h1>
                {selectedRound ? (
                  <div className="mt-1 text-xs font-medium text-slate-500 md:hidden">
                    {imageRoundSizeLabel(selectedRound)} · 候选 {selectedRound.candidate_index}/{selectedRound.candidate_count}
                  </div>
                ) : selectedPlaceholder ? (
                  <div className="mt-1 text-xs font-medium text-slate-500 md:hidden">
                    {placeholderStatusLabel(selectedPlaceholder)} · 候选 {selectedPlaceholder.candidate_index}/{selectedPlaceholder.candidate_count}
                  </div>
                ) : null}
              </div>
              <div className="flex w-full flex-wrap items-center justify-start gap-2 sm:w-auto sm:justify-end">
                {selectedRound ? (
                  <>
                    <span className="hidden rounded-full border border-slate-200 bg-white px-3 py-1.5 text-xs text-slate-600 shadow-sm md:inline-flex">
                      {imageRoundSizeLabel(selectedRound)} · 候选 {selectedRound.candidate_index}/{selectedRound.candidate_count}
                    </span>
                    <a
                      href={api.toApiUrl(selectedRound.generated_asset.download_url)}
                      target="_blank"
                      rel="noreferrer"
                      title="下载当前图片"
                      aria-label="下载当前图片"
                      className="inline-flex h-9 w-9 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-700 shadow-sm transition-colors hover:border-indigo-200 hover:text-indigo-700"
                    >
                      <Download size={15} />
                    </a>
                    <button
                      type="button"
                      onClick={handleSaveSelectedToGallery}
                      disabled={saveGalleryMutation.isPending}
                      title="将当前选中的生成候选保存到全局画廊"
                      aria-label="将当前选中的生成候选保存到全局画廊"
                      className="inline-flex h-10 shrink-0 items-center justify-center rounded-xl bg-indigo-600 px-4 text-sm font-semibold text-white shadow-sm shadow-indigo-500/20 ring-1 ring-indigo-500 transition-colors hover:bg-indigo-700 disabled:opacity-60"
                    >
                      {saveGalleryMutation.isPending ? (
                        <Loader2 size={16} className="mr-2 animate-spin" />
                      ) : (
                        <GalleryHorizontalEnd size={16} className="mr-2" />
                      )}
                      投至画廊
                    </button>
                  </>
                ) : selectedPlaceholder ? (
                  <span className={`rounded-full border px-3 py-1.5 text-xs font-medium shadow-sm ${placeholderStatusClass(selectedPlaceholder)}`}>
                    {placeholderStatusLabel(selectedPlaceholder)}
                  </span>
                ) : null}
              </div>
            </div>

            <div className="relative flex min-h-[320px] flex-1 items-center justify-center overflow-hidden rounded-3xl border border-slate-200 bg-white shadow-sm max-h-[72vh] lg:min-h-[360px] lg:max-h-none">
              <div className="absolute inset-0 bg-[radial-gradient(#cbd5e1_1px,transparent_1px)] [background-size:20px_20px]" />
              <div className="absolute inset-x-0 top-0 z-10 flex items-center justify-between gap-3 px-5 py-4">
                {selectedRound ? (
                  <div className="min-w-0 max-w-[calc(100%-5.5rem)] truncate rounded-full bg-white/90 px-3 py-1.5 text-xs font-medium text-slate-600 shadow-sm ring-1 ring-slate-200 backdrop-blur">
                    {formatDateTime(selectedRound.created_at)} · {selectedRound.model_name}
                  </div>
                ) : selectedPlaceholder ? (
                  <div className="min-w-0 max-w-[calc(100%-5.5rem)] truncate rounded-full bg-white/90 px-3 py-1.5 text-xs font-medium text-slate-600 shadow-sm ring-1 ring-slate-200 backdrop-blur">
                    {placeholderStatusLabel(selectedPlaceholder)} · {formatImageSizeValue(selectedPlaceholder.size)}
                  </div>
                ) : (
                  <div className="rounded-full bg-white/90 px-3 py-1.5 text-xs font-medium text-slate-500 shadow-sm ring-1 ring-slate-200 backdrop-blur">
                    等待第一张结果
                  </div>
                )}
                {branchBaseRound ? (
                  <div className="inline-flex h-8 items-center gap-1.5 rounded-full bg-indigo-600 px-3 text-xs font-semibold text-white shadow-sm shadow-indigo-500/20">
                    <Layers3 size={13} />
                    已选基图
                  </div>
                ) : null}
              </div>

              {selectedRound ? (
                <div className="relative z-0 flex h-full min-h-0 w-full items-center justify-center px-2 pb-2 pt-12 sm:px-3 sm:pb-3 sm:pt-14">
                  <img
                    src={api.toApiUrl(selectedRound.generated_asset.download_url)}
                    alt="当前结果"
                    decoding="async"
                    className="max-h-full max-w-full object-contain drop-shadow-2xl"
                  />
                </div>
              ) : selectedPlaceholder ? (
                <GenerationCanvasPlaceholder candidate={selectedPlaceholder} />
              ) : (
                <div className="relative z-0 flex flex-col items-center gap-4 text-center text-slate-400">
                  <div className="flex h-16 w-16 items-center justify-center rounded-3xl bg-white shadow-sm ring-1 ring-slate-200">
                    <Sparkles size={28} />
                  </div>
                  <div>
                    <div className="text-sm font-semibold text-slate-600">还没有结果</div>
                  </div>
                </div>
              )}
            </div>
            {selectedRound?.provider_notes.length ? (
              <div className="mt-2 flex flex-wrap gap-2 rounded-2xl border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">
                {selectedRound.provider_notes.map((note) => (
                  <span key={note}>{note}</span>
                ))}
              </div>
            ) : selectedPlaceholder?.failure_reason ? (
              <div className="mt-2 rounded-2xl border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700">
                {selectedPlaceholder.failure_reason}
              </div>
            ) : selectedPlaceholder?.provider_notes.length ? (
              <div className="mt-2 flex flex-wrap gap-2 rounded-2xl border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">
                {selectedPlaceholder.provider_notes.map((note) => (
                  <span key={note}>{note}</span>
                ))}
              </div>
            ) : null}
          </div>

          <div className="flex h-44 shrink-0 flex-col border-t border-slate-200 bg-white/95 px-3 py-2.5 shadow-[0_-8px_24px_rgba(15,23,42,0.04)] lg:h-[clamp(9.5rem,16vh,11rem)]">
            <div className="mb-2 flex items-center justify-between gap-3">
              <div>
                <div className="text-sm font-semibold text-slate-950">历史记录</div>
              </div>
              {branchBaseRound ? <div className="text-xs font-medium text-indigo-700">点击历史图可切换基图</div> : null}
            </div>

            {historyBranches.length ? (
              <div className="flex min-h-0 flex-1 gap-3 overflow-x-auto pb-1">
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
                  />
                ))}
              </div>
            ) : (
              <div className="flex min-h-0 flex-1 items-center justify-center rounded-2xl border border-dashed border-slate-200 bg-slate-50 text-sm text-slate-400">
                生成结果会出现在这里
              </div>
            )}
          </div>
        </section>

        <aside className="flex w-full shrink-0 flex-col border-t border-slate-200 bg-white lg:w-[clamp(300px,21vw,340px)] lg:border-l lg:border-t-0">
          <div className="min-h-0 flex-1 px-4 py-5 lg:overflow-y-auto lg:px-5">
            <div className="mb-5">
              <div className="mb-3 flex items-center justify-between gap-3">
                <div>
                  <div className="inline-flex items-center gap-1.5 text-sm font-semibold text-slate-950">
                    <Settings size={15} /> 生成设置
                  </div>
                </div>
                <button
                  type="button"
                  onClick={() => setRenameEnabled((current) => !current)}
                  className="inline-flex h-8 items-center rounded-lg border border-slate-200 px-2.5 text-xs font-medium text-slate-600 transition-colors hover:border-slate-300 hover:text-slate-900"
                >
                  <Pencil size={12} className="mr-1.5" /> 重命名
                </button>
              </div>
              <div className="flex gap-2">
                <input
                  value={titleDraft}
                  onChange={(event) => setTitleDraft(event.target.value)}
                  disabled={!renameEnabled || renameSessionMutation.isPending}
                  className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm text-slate-900 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-100 disabled:bg-slate-50 disabled:text-slate-500"
                />
                {renameEnabled ? (
                  <button
                    type="button"
                    onClick={handleRename}
                    className="inline-flex items-center rounded-xl bg-indigo-600 px-3 py-2 text-sm font-semibold text-white transition-colors hover:bg-indigo-500"
                    aria-label="保存会话名称"
                  >
                    {renameSessionMutation.isPending ? <Loader2 size={14} className="animate-spin" /> : <Save size={14} />}
                  </button>
                ) : null}
              </div>
            </div>

            <div className="mb-4 grid grid-cols-2 gap-1 rounded-xl border border-slate-200 bg-slate-100 p-1">
              {(
                [
                  ["basic", "生成设置"],
                  ["advanced", "高级"],
                ] as const
              ).map(([value, label]) => (
                <button
                  key={value}
                  type="button"
                  onClick={() => setSettingsTab(value)}
                  className={`h-9 rounded-lg text-sm font-semibold transition-colors ${
                    settingsTab === value ? "bg-white text-indigo-700 shadow-sm" : "text-slate-500 hover:text-slate-900"
                  }`}
                >
                  {label}
                </button>
              ))}
            </div>

            <div className="space-y-4">
              {settingsTab === "basic" ? (
                <>
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
                  />

                  <div>
                    <label className="mb-2 block text-sm font-semibold text-slate-950" htmlFor="image-chat-prompt">
                      画面描述
                    </label>
                    <textarea
                      id="image-chat-prompt"
                      value={draft}
                      onChange={(event) => setDraft(event.target.value)}
                      rows={6}
                      placeholder={isProductMode ? "描述这一轮要在商品图上调整什么。" : "描述你想生成的画面。"}
                      className="w-full resize-none rounded-2xl border border-slate-200 px-3 py-3 text-sm leading-6 text-slate-900 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-100"
                    />
                  </div>

                  <div className="rounded-2xl border border-slate-200 bg-white p-4">
                    <div className="mb-3 flex items-center justify-between gap-3">
                      <div className="text-sm font-semibold text-slate-950">参数</div>
                      <span className="text-[11px] font-medium text-slate-400">{formatImageSizeValue(size)}</span>
                    </div>
                    <ImageSizePicker
                      value={size}
                      presets={sizeOptions}
                      maxDimension={imageGenerationMaxDimension}
                      onChange={setSize}
                    />
                    <label className="mt-3 block" htmlFor="generation-count">
                      <span className="mb-1.5 block text-xs font-semibold text-slate-700">生成数量</span>
                      <select
                        id="generation-count"
                        value={generationCount}
                        onChange={(event) => setGenerationCount(clampGenerationCount(Number(event.target.value)))}
                        className="w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm text-slate-900 focus:border-indigo-500 focus:bg-white focus:outline-none focus:ring-2 focus:ring-indigo-100"
                      >
                        {[1, 2, 3, 4].map((count) => (
                          <option key={count} value={count}>
                            {count} 张候选
                          </option>
                        ))}
                      </select>
                    </label>
                  </div>
                </>
              ) : (
                <>

                  <ImageToolControls
                    value={toolOptions}
                    allowedFields={imageToolAllowedFields}
                    onChange={setToolOptions}
                  />

              {visibleGenerationTasks.length ? (
                <div className="space-y-2 rounded-2xl border border-slate-200 bg-white p-4">
                  <div className="text-sm font-semibold text-slate-950">生成任务</div>
                  {visibleGenerationTasks.map((task) => {
                    const active = isImageSessionGenerationTaskActive(task);
                    return (
                      <div
                        key={task.id}
                        className={`rounded-xl border px-3 py-2 text-sm ${
                          task.status === "failed"
                            ? "border-red-200 bg-red-50 text-red-700"
                            : "border-indigo-100 bg-indigo-50 text-indigo-800"
                        }`}
                      >
                        <div className="flex items-center justify-between gap-2">
                          <div className="inline-flex items-center font-semibold">
                            {active ? <Loader2 size={13} className="mr-1.5 animate-spin" /> : null}
                            {generationTaskLabel(task)}
                          </div>
                          <span className="text-xs opacity-70">{task.generation_count} 张</span>
                        </div>
                        <button
                          type="button"
                          onClick={() =>
                            setPromptPreview({
                              title: "任务 Prompt",
                              text: task.prompt,
                              meta: `${generationTaskLabel(task)} · ${formatImageSizeValue(task.size)} · ${task.generation_count} 张`,
                            })
                          }
                          className="mt-1 line-clamp-2 rounded text-left text-xs opacity-80 transition-opacity hover:opacity-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500"
                        >
                          {task.prompt}
                        </button>
                        {task.status === "failed" ? (
                          <div className="mt-1 text-xs">{task.failure_reason ?? "图片生成失败，请稍后重试"}</div>
                        ) : generationTaskQueueText(task) ? (
                          <div className="mt-1 text-xs opacity-70">
                            {generationTaskQueueText(task)}
                          </div>
                        ) : null}
                      </div>
                    );
                  })}
                </div>
              ) : null}
                </>
              )}

              {successMessage ? (
                <div className="rounded-xl border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm text-emerald-700">
                  {successMessage}
                </div>
              ) : null}
              {errorMessage ? (
                <div className="rounded-xl border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{errorMessage}</div>
              ) : null}
            </div>
          </div>

          <div className="fixed inset-x-0 bottom-0 z-40 border-t border-slate-200 bg-white/95 p-3 pb-[calc(env(safe-area-inset-bottom)+0.75rem)] shadow-[0_-8px_24px_rgba(15,23,42,0.10)] backdrop-blur lg:sticky lg:inset-x-auto lg:bottom-0 lg:p-4">
            {baseRequirementMessage ? (
              <div className="mb-2 rounded-xl border border-amber-200 bg-amber-50 px-3 py-2 text-xs font-medium text-amber-700">
                {baseRequirementMessage}
              </div>
            ) : null}
            <button
              type="button"
              onClick={handleGenerate}
              disabled={generateDisabled}
              className="inline-flex w-full items-center justify-center rounded-2xl bg-indigo-600 px-4 py-3.5 text-sm font-semibold text-white shadow-lg shadow-indigo-600/20 transition-colors hover:bg-indigo-500 disabled:opacity-60"
            >
              {generateMutation.isPending ? (
                <Loader2 size={15} className="mr-2 animate-spin" />
              ) : (
                <Sparkles size={15} className="mr-2" />
              )}
              {generateMutation.isPending
                ? "提交中"
                : generationCount > 1
                  ? `开始生成 · ${generationCount} 张候选`
                  : "开始生成"}
            </button>
          </div>
        </aside>
      </main>
      {promptPreview ? (
        <PromptPreviewDialog preview={promptPreview} onClose={() => setPromptPreview(null)} />
      ) : null}
    </div>
  );

}

function GenerationCanvasPlaceholder({ candidate }: { candidate: ImageHistoryPlaceholderCandidate }) {
  const active = candidate.status === "queued" || candidate.status === "running";
  const failed = candidate.status === "failed";
  const queueText = generationTaskQueueText(candidate.task);

  return (
    <div className="relative z-0 flex h-full min-h-0 w-full items-center justify-center px-6 pb-6 pt-16">
      <div className="flex max-w-md flex-col items-center text-center">
        <div
          className={`relative flex h-24 w-24 items-center justify-center rounded-3xl border shadow-sm ${
            failed ? "border-red-200 bg-red-50 text-red-600" : "border-indigo-100 bg-indigo-50 text-indigo-700"
          }`}
        >
          {active ? <div className="absolute inset-2 rounded-3xl bg-indigo-200/70 opacity-70 blur-xl animate-pulse" /> : null}
          {active ? <Loader2 size={30} className="relative animate-spin" /> : <Sparkles size={30} className="relative" />}
        </div>
        <div className="mt-4 text-sm font-semibold text-slate-900">{placeholderStatusLabel(candidate)}</div>
        <div className="mt-1 text-xs text-slate-500">
          候选 {candidate.candidate_index}/{candidate.candidate_count} · {formatImageSizeValue(candidate.size)}
        </div>
        {queueText ? <div className="mt-3 max-w-sm text-xs leading-5 text-slate-500">{queueText}</div> : null}
        <div className="mt-4 line-clamp-3 max-w-sm text-xs leading-5 text-slate-600">{candidate.prompt}</div>
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
}: {
  branch: ImageHistoryBranch;
  selectedGeneratedAssetId: string | null;
  selectedTaskPlaceholderId: string | null;
  branchBaseAssetId: string | null;
  onSelectRound: (assetId: string) => void;
  onSelectPlaceholder: (placeholderId: string) => void;
  onPreviewPrompt: (preview: PromptPreview) => void;
}) {
  const depthOffset = Math.min(branch.depth, 4) * 18;
  const branchLabel = branch.base_asset_id ? `分支 ${branch.depth}` : "首轮";

  return (
    <div
      className="relative flex h-full shrink-0 gap-2 rounded-2xl border border-slate-200 bg-slate-50/80 p-2"
      style={{ marginLeft: depthOffset }}
    >
      {branch.depth > 0 ? (
        <div className="pointer-events-none absolute -left-3 top-1/2 h-px w-3 bg-slate-300" />
      ) : null}
      <div className="flex w-28 shrink-0 flex-col justify-between rounded-xl bg-white p-2 text-xs text-slate-500 ring-1 ring-slate-200">
        <div>
          <div className="flex items-center gap-1.5 font-semibold text-slate-800">
            {branch.depth > 0 ? <Layers3 size={12} /> : <History size={12} />}
            {branchLabel}
          </div>
          <div className="mt-1">{branch.candidates.length} 张</div>
        </div>
        <button
          type="button"
          onClick={() =>
            onPreviewPrompt({
              title: branch.base_asset_id ? "分支 Prompt" : "首轮 Prompt",
              text: branch.prompt,
              meta: `${branch.candidates.length} 张 · ${formatDateTime(branch.created_at)}`,
            })
          }
          className="line-clamp-3 rounded-md text-left text-[11px] leading-4 text-slate-400 transition-colors hover:text-indigo-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500"
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
}: {
  candidate: ImageHistoryCandidate;
  selectedGeneratedAssetId: string | null;
  selectedTaskPlaceholderId: string | null;
  branchBaseAssetId: string | null;
  onSelectRound: (assetId: string) => void;
  onSelectPlaceholder: (placeholderId: string) => void;
}) {
  if (candidate.kind === "placeholder") {
    const active = candidate.id === selectedTaskPlaceholderId;
    const running = candidate.status === "queued" || candidate.status === "running";
    return (
      <div
        className={`group/card relative aspect-square h-full min-w-[7rem] shrink-0 overflow-hidden rounded-2xl border bg-white transition-all ${
          active ? "border-indigo-400 ring-2 ring-indigo-200" : "border-slate-200 hover:border-slate-300"
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
            <div className="relative flex h-12 w-12 items-center justify-center rounded-2xl bg-slate-50 text-indigo-600 ring-1 ring-slate-200">
              {running ? <Loader2 size={19} className="animate-spin" /> : <Sparkles size={19} />}
            </div>
          </div>
          <div>
            <div className="truncate text-[11px] font-semibold text-slate-700">{placeholderStatusLabel(candidate)}</div>
            <div className="mt-0.5 line-clamp-2 text-[10px] leading-3 text-slate-400">{candidate.prompt}</div>
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
      className={`group/card relative aspect-square h-full min-w-[7rem] shrink-0 overflow-hidden rounded-2xl border bg-white transition-all ${
        active ? "border-indigo-400 ring-2 ring-indigo-200" : "border-slate-200 hover:border-slate-300"
      } ${asBase ? "shadow-md shadow-indigo-200/70" : "shadow-sm shadow-slate-200/60"}`}
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
              {round.candidate_count > 1 ? `${round.candidate_index}/${round.candidate_count}` : imageRoundSizeLabel(round)}
            </span>
            {active ? <Check size={13} className="shrink-0" /> : null}
          </div>
        </div>
      </button>
      {asBase ? (
        <div className="absolute left-1.5 top-1.5 max-w-[calc(100%-2.75rem)] truncate rounded-full bg-indigo-600 px-1.5 py-0.5 text-[10px] font-semibold text-white shadow-sm">
          基图
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
}) {
  const saveDisabled = attachBusy || !selectedRound || (!isProductMode && !targetProductId);

  return (
    <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4">
      <div className="mb-3 text-sm font-semibold text-zinc-900">保存至商品库</div>
      {isProductMode ? (
        product ? (
          <div className="grid grid-cols-[88px_minmax(0,1fr)] gap-3">
            <ProductThumbnail sourceImage={sourceImage} alt={product.name} />
            <div className="min-w-0 self-center">
              <div className="truncate text-sm font-medium text-zinc-900">{product.name}</div>
              <div className="mt-1 text-xs text-zinc-500">当前参考图 {referenceImages.length} 张</div>
            </div>
          </div>
        ) : (
          <div className="flex justify-center py-6 text-zinc-400">
            <Loader2 size={16} className="animate-spin" />
          </div>
        )
      ) : (
        <label className="block">
          <span className="mb-1.5 block text-xs font-semibold text-slate-700">目标商品</span>
          <select
            value={targetProductId}
            onChange={(event) => onTargetProductChange(event.target.value)}
            className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm text-zinc-900 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-100"
          >
            {products.length ? null : <option value="">暂无商品</option>}
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
              <div key={asset.id} className="group relative overflow-hidden rounded-md border border-zinc-200 bg-white">
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
                  aria-label="删除商品参考图"
                  onClick={() => onDeleteReference(asset.id)}
                  disabled={deleting}
                  className="absolute right-1 top-1 inline-flex h-6 w-6 items-center justify-center rounded bg-white/90 text-zinc-500 opacity-100 shadow-sm ring-1 ring-zinc-200 transition-colors hover:text-red-600 disabled:opacity-60 md:opacity-0 md:group-hover:opacity-100"
                >
                  {deleting ? <Loader2 size={12} className="animate-spin" /> : <Trash2 size={12} />}
                </button>
              </div>
            );
          })}
        </div>
      ) : null}

      <div className="mt-4 border-t border-slate-200 pt-3">
        {selectedRound ? (
          <div className="mb-2 text-[11px] leading-5 text-slate-500">
            当前选中候选：{formatImageSizeValue(selectedRound.size)}
          </div>
        ) : (
          <div className="mb-2 rounded-xl border border-dashed border-slate-200 bg-white px-3 py-2 text-center text-sm text-slate-400">
            先从历史记录中选择一张图片
          </div>
        )}
        <div className="grid gap-2">
          <button
            type="button"
            onClick={() => onAttach("reference")}
            disabled={saveDisabled}
            className="inline-flex items-center justify-center rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm font-medium text-slate-700 transition-colors hover:border-slate-300 hover:text-slate-950 disabled:opacity-60"
          >
            {attachBusy ? <Loader2 size={14} className="mr-2 animate-spin" /> : <Check size={14} className="mr-2" />}
            {isProductMode ? "加入参考图" : "保存为参考图"}
          </button>
          {isProductMode ? (
            <button
              type="button"
              onClick={() => onAttach("main_source")}
              disabled={saveDisabled}
              className="inline-flex items-center justify-center rounded-xl bg-slate-900 px-3 py-2 text-sm font-semibold text-white transition-colors hover:bg-slate-800 disabled:opacity-60"
            >
              {attachBusy ? <Loader2 size={14} className="mr-2 animate-spin" /> : <ImageIcon size={14} className="mr-2" />}
              设为商品主图
            </button>
          ) : null}
        </div>
      </div>
    </div>
  );
}

function ProductThumbnail({ sourceImage, alt }: { sourceImage: SourceAsset | null; alt: string }) {
  return (
    <div className="overflow-hidden rounded-lg border border-zinc-200 bg-white">
      {sourceImage ? (
        <img src={api.toApiUrl(sourceImage.thumbnail_url)} alt={alt} decoding="async" className="h-24 w-full object-cover" />
      ) : (
        <div className="flex h-24 items-center justify-center text-zinc-300">
          <ImageIcon size={20} />
        </div>
      )}
    </div>
  );
}
