import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Check,
  Download,
  History,
  Image as ImageIcon,
  ImagePlus,
  Layers3,
  Loader2,
  MessagesSquare,
  Pencil,
  Plus,
  Save,
  Sparkles,
  Trash2,
  X,
} from "lucide-react";
import { useNavigate, useParams } from "react-router-dom";

import { ImageDropZone } from "../components/ImageDropZone";
import { ImageSizePicker } from "../components/ImageSizePicker";
import { TopNav } from "../components/TopNav";
import { api, ApiError } from "../lib/api";
import { formatDateTime } from "../lib/format";
import { DEFAULT_IMAGE_GENERATION_MAX_DIMENSION, buildImageSizeOptions } from "../lib/imageSizes";
import { clampGenerationCount, groupImageSessionRounds, pruneSelectedReferenceIds } from "./image-chat/branching";
import type {
  ImageSessionDetail,
  ImageSessionGenerationTask,
  ImageSessionListResponse,
  ProductDetail,
  ProductSummary,
  SourceAsset,
} from "../lib/types";

function getSessionReferenceAssets(imageSession: ImageSessionDetail | undefined) {
  return imageSession?.assets.filter((asset) => asset.kind === "reference_upload") ?? [];
}

const MAX_BRANCH_CONTEXT_IMAGES = 6;

function isActiveGenerationTask(task: ImageSessionGenerationTask) {
  return task.status === "queued" || task.status === "running";
}

function generationTaskLabel(task: ImageSessionGenerationTask) {
  if (task.status === "queued") {
    return task.queue_position ? `排队中 · 第 ${task.queue_position} 位` : "排队中";
  }
  if (task.status === "running") {
    return "生成中";
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
    return `正在生成，前方 0 个；全局运行 ${task.queue_running_count} 个，排队 ${task.queue_queued_count} 个。`;
  }
  return "";
}

export function ImageChatPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { productId } = useParams();
  const isProductMode = Boolean(productId);
  const autoCreateTriggered = useRef(false);
  const pendingGeneratedRoundCountRef = useRef<number | null>(null);

  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(null);
  const [selectedGeneratedAssetId, setSelectedGeneratedAssetId] = useState<string | null>(null);
  const [branchBaseAssetId, setBranchBaseAssetId] = useState<string | null>(null);
  const [selectedReferenceAssetIds, setSelectedReferenceAssetIds] = useState<string[]>([]);
  const [generationCount, setGenerationCount] = useState(1);
  const [draft, setDraft] = useState("");
  const [size, setSize] = useState("1024x1024");
  const [titleDraft, setTitleDraft] = useState("");
  const [renameEnabled, setRenameEnabled] = useState(false);
  const [targetProductId, setTargetProductId] = useState("");
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
    }
  }, [createSessionMutation, selectedSessionId, sessionItems, sessionsQuery.isLoading]);

  const sessionDetailQuery = useQuery({
    queryKey: ["image-session", selectedSessionId],
    queryFn: () => api.getImageSession(selectedSessionId!),
    enabled: Boolean(selectedSessionId),
    refetchInterval: (query) => {
      const data = query.state.data as ImageSessionDetail | undefined;
      return data?.generation_tasks.some(isActiveGenerationTask) ? 1500 : false;
    },
  });

  const imageSession = sessionDetailQuery.data;
  const sessionReferenceAssets = useMemo(() => getSessionReferenceAssets(imageSession), [imageSession]);
  const maxSelectedReferenceCount = branchBaseAssetId ? MAX_BRANCH_CONTEXT_IMAGES - 1 : MAX_BRANCH_CONTEXT_IMAGES;
  const roundGroups = useMemo(() => groupImageSessionRounds(imageSession?.rounds ?? []), [imageSession]);
  const visibleGenerationTasks = useMemo(
    () => imageSession?.generation_tasks.filter((task) => task.status !== "succeeded").slice(0, 4) ?? [],
    [imageSession],
  );
  const hasActiveGenerationTask = visibleGenerationTasks.some(isActiveGenerationTask);

  useEffect(() => {
    if (!imageSession) {
      return;
    }
    setTitleDraft(imageSession.title);
    if (!selectedGeneratedAssetId || !imageSession.rounds.some((round) => round.generated_asset.id === selectedGeneratedAssetId)) {
      setSelectedGeneratedAssetId(imageSession.rounds.at(-1)?.generated_asset.id ?? null);
    }
    if (branchBaseAssetId && !imageSession.rounds.some((round) => round.generated_asset.id === branchBaseAssetId)) {
      setBranchBaseAssetId(null);
    }
    if (
      pendingGeneratedRoundCountRef.current !== null &&
      imageSession.rounds.length > pendingGeneratedRoundCountRef.current
    ) {
      pendingGeneratedRoundCountRef.current = null;
      setSelectedGeneratedAssetId(imageSession.rounds.at(-1)?.generated_asset.id ?? null);
      setSuccessMessage("新候选已生成");
      setErrorMessage("");
    }
    setSelectedReferenceAssetIds((current) =>
      pruneSelectedReferenceIds(
        current,
        imageSession.assets.filter((asset) => asset.kind === "reference_upload").map((asset) => asset.id),
        maxSelectedReferenceCount,
      ),
    );
  }, [branchBaseAssetId, imageSession, maxSelectedReferenceCount, selectedGeneratedAssetId]);

  const selectedRound = useMemo(() => {
    if (!imageSession?.rounds.length) {
      return null;
    }
    return (
      imageSession.rounds.find((round) => round.generated_asset.id === selectedGeneratedAssetId) ?? imageSession.rounds.at(-1) ?? null
    );
  }, [imageSession, selectedGeneratedAssetId]);

  const branchBaseRound = useMemo(() => {
    if (!imageSession?.rounds.length || !branchBaseAssetId) {
      return null;
    }
    return imageSession.rounds.find((round) => round.generated_asset.id === branchBaseAssetId) ?? null;
  }, [branchBaseAssetId, imageSession]);

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

  const uploadReferenceMutation = useMutation({
    mutationFn: (files: File[]) => api.addImageSessionReferenceImages(selectedSessionId!, files),
    onSuccess: (updated) => {
      const previousReferenceIds = new Set(sessionReferenceAssets.map((asset) => asset.id));
      const uploadedReferenceIds = updated.assets
        .filter((asset) => asset.kind === "reference_upload" && !previousReferenceIds.has(asset.id))
        .map((asset) => asset.id);
      queryClient.setQueryData(["image-session", updated.id], updated);
      void queryClient.invalidateQueries({ queryKey: ["image-sessions", productId ?? "standalone"] });
      if (uploadedReferenceIds.length) {
        setSelectedReferenceAssetIds((current) =>
          pruneSelectedReferenceIds(
            [...current, ...uploadedReferenceIds],
            updated.assets.filter((asset) => asset.kind === "reference_upload").map((asset) => asset.id),
            maxSelectedReferenceCount,
          ),
        );
      }
      setSuccessMessage("参考图已上传");
      setErrorMessage("");
    },
    onError: (error) => {
      setErrorMessage(error instanceof ApiError ? error.detail : "参考图上传失败");
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

  const deleteSessionReferenceMutation = useMutation({
    mutationFn: (assetId: string) => api.deleteImageSessionReferenceImage(selectedSessionId!, assetId),
    onSuccess: (updated) => {
      queryClient.setQueryData(["image-session", updated.id], updated);
      void queryClient.invalidateQueries({ queryKey: ["image-sessions", productId ?? "standalone"] });
      setSelectedReferenceAssetIds((current) =>
        pruneSelectedReferenceIds(
          current,
          updated.assets.filter((asset) => asset.kind === "reference_upload").map((asset) => asset.id),
          maxSelectedReferenceCount,
        ),
      );
      setSuccessMessage("参考图已删除");
      setErrorMessage("");
    },
    onError: (error) => {
      setErrorMessage(error instanceof ApiError ? error.detail : "参考图删除失败");
    },
  });

  const generateMutation = useMutation({
    mutationFn: (payload: {
      prompt: string;
      size: string;
      base_asset_id: string | null;
      selected_reference_asset_ids: string[];
      generation_count: number;
    }) => api.generateImageSessionRound(selectedSessionId!, payload),
    onSuccess: (updated) => {
      queryClient.setQueryData(["image-session", updated.id], updated);
      void queryClient.invalidateQueries({ queryKey: ["image-sessions", productId ?? "standalone"] });
      setDraft("");
      setSuccessMessage(generationCount > 1 ? `已提交生成任务 · ${generationCount} 张候选` : "已提交生成任务");
      setErrorMessage("");
    },
    onError: (error) => {
      setErrorMessage(error instanceof ApiError ? error.detail : "生成失败");
    },
  });

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
    if (!selectedSessionId || !prompt || generateMutation.isPending) {
      return;
    }
    pendingGeneratedRoundCountRef.current = imageSession?.rounds.length ?? 0;
    generateMutation.mutate({
      prompt,
      size,
      base_asset_id: branchBaseAssetId,
      selected_reference_asset_ids: selectedReferenceAssetIds,
      generation_count: clampGenerationCount(generationCount),
    });
  }

  function handleContinueFrom(roundAssetId: string) {
    setBranchBaseAssetId(roundAssetId);
    setSelectedGeneratedAssetId(roundAssetId);
    setSuccessMessage("");
    setErrorMessage("");
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

  function handleDeleteSession(sessionId: string) {
    if (deleteSessionMutation.isPending) {
      return;
    }
    if (!window.confirm("删除这个会话？会话里的参考图和生成记录都会移除，已保存到商品的图片不受影响。")) {
      return;
    }
    deleteSessionMutation.mutate(sessionId);
  }

  function handleDeleteSessionReference(assetId: string) {
    if (deleteSessionReferenceMutation.isPending) {
      return;
    }
    if (!window.confirm("删除这张会话参考图？后续生成将不再参考它。")) {
      return;
    }
    deleteSessionReferenceMutation.mutate(assetId);
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

  function handleUploadReferenceFiles(files: File[]) {
    if (!files.length || !selectedSessionId || uploadReferenceMutation.isPending) {
      return;
    }
    uploadReferenceMutation.mutate(files);
  }

  return (
    <div className="flex h-screen flex-col overflow-hidden bg-slate-100 text-slate-900">
      <TopNav
        breadcrumbs={isProductMode ? `${productQuery.data?.name ?? "商品"} / 连续生图` : "连续生图"}
        onHome={() => navigate(isProductMode && productId ? `/products/${productId}` : "/products")}
        onLogout={() => logoutMutation.mutate()}
      />

      <main className="flex min-h-0 flex-1 overflow-hidden">
        <aside className="flex w-72 shrink-0 flex-col border-r border-slate-200 bg-white/95">
          <div className="border-b border-slate-200 px-4 py-4">
            <div className="flex items-center justify-between gap-3">
              <div>
                <div className="text-sm font-semibold text-slate-950">会话列表</div>
                <div className="mt-1 text-xs text-slate-500">{sessionItems.length} 个方向</div>
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

          <div className="min-h-0 flex-1 space-y-2 overflow-y-auto p-3">
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
                    className={`group relative overflow-hidden rounded-2xl border transition-all ${
                      active
                        ? "border-indigo-300 bg-indigo-50 shadow-sm shadow-indigo-100 ring-1 ring-indigo-200/80"
                        : "border-slate-200 bg-white hover:border-slate-300 hover:bg-slate-50"
                    }`}
                  >
                    <button
                      type="button"
                      onClick={() => {
                        setSelectedSessionId(item.id);
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
                      disabled={deleting}
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

        <section className="flex min-w-0 flex-1 flex-col overflow-hidden bg-slate-100">
          <div className="flex min-h-0 flex-1 flex-col p-4 pb-3">
            <div className="mb-3 flex items-center justify-between gap-3">
              <div>
                <div className="flex items-center gap-2 text-xs font-medium text-slate-500">
                  <span className="inline-flex h-7 items-center rounded-full bg-white px-3 shadow-sm ring-1 ring-slate-200">
                    当前结果
                  </span>
                  {branchBaseRound ? (
                    <span className="inline-flex h-7 items-center gap-1 rounded-full bg-indigo-600 px-3 text-white shadow-sm shadow-indigo-500/20">
                      <Layers3 size={12} /> 分支生成
                    </span>
                  ) : null}
                </div>
                <h1 className="mt-2 text-xl font-semibold tracking-tight text-slate-950">
                  {imageSession?.title ?? "连续生图工作台"}
                </h1>
              </div>
              <div className="flex items-center gap-2">
                {selectedRound ? (
                  <>
                    <span className="hidden rounded-full border border-slate-200 bg-white px-3 py-1.5 text-xs text-slate-600 shadow-sm md:inline-flex">
                      {selectedRound.size} · 候选 {selectedRound.candidate_index}/{selectedRound.candidate_count}
                    </span>
                    <a
                      href={api.toApiUrl(selectedRound.generated_asset.download_url)}
                      target="_blank"
                      rel="noreferrer"
                      className="inline-flex items-center rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm font-medium text-slate-700 shadow-sm transition-colors hover:border-indigo-200 hover:text-indigo-700"
                    >
                      <Download size={15} className="mr-1.5" /> 下载
                    </a>
                  </>
                ) : null}
              </div>
            </div>

            <div className="relative flex min-h-[320px] flex-1 items-center justify-center overflow-hidden rounded-3xl border border-slate-200 bg-white shadow-sm">
              <div className="absolute inset-0 bg-[radial-gradient(#cbd5e1_1px,transparent_1px)] [background-size:20px_20px]" />
              <div className="absolute inset-x-0 top-0 z-10 flex items-center justify-between px-5 py-4">
                {selectedRound ? (
                  <div className="rounded-full bg-white/90 px-3 py-1.5 text-xs font-medium text-slate-600 shadow-sm ring-1 ring-slate-200 backdrop-blur">
                    {formatDateTime(selectedRound.created_at)} · {selectedRound.model_name}
                  </div>
                ) : (
                  <div className="rounded-full bg-white/90 px-3 py-1.5 text-xs font-medium text-slate-500 shadow-sm ring-1 ring-slate-200 backdrop-blur">
                    等待第一张结果
                  </div>
                )}
                {selectedRound ? (
                  <button
                    type="button"
                    onClick={() => handleContinueFrom(selectedRound.generated_asset.id)}
                    className={`rounded-full px-3 py-1.5 text-xs font-semibold shadow-sm ring-1 transition-colors backdrop-blur ${
                      selectedRound.generated_asset.id === branchBaseAssetId
                        ? "bg-indigo-600 text-white ring-indigo-500"
                        : "bg-white/90 text-slate-700 ring-slate-200 hover:text-indigo-700"
                    }`}
                  >
                    从当前继续
                  </button>
                ) : null}
              </div>

              {selectedRound ? (
                <div className="relative z-0 flex h-full min-h-0 w-full items-center justify-center px-4 pb-4 pt-14 sm:px-6 sm:pb-6 sm:pt-16">
                  <img
                    src={api.toApiUrl(selectedRound.generated_asset.preview_url)}
                    alt="当前结果"
                    decoding="async"
                    className="h-full w-full object-contain drop-shadow-2xl"
                  />
                </div>
              ) : (
                <div className="relative z-0 flex flex-col items-center gap-4 text-center text-slate-400">
                  <div className="flex h-16 w-16 items-center justify-center rounded-3xl bg-white shadow-sm ring-1 ring-slate-200">
                    <Sparkles size={28} />
                  </div>
                  <div>
                    <div className="text-sm font-semibold text-slate-600">还没有结果</div>
                    <div className="mt-1 text-xs text-slate-400">在右侧输入画面描述后开始生成。</div>
                  </div>
                </div>
              )}
            </div>
          </div>

          <div className="flex h-[clamp(10rem,18vh,12.5rem)] shrink-0 flex-col border-t border-slate-200 bg-white/95 px-4 py-3 shadow-[0_-8px_24px_rgba(15,23,42,0.04)]">
            <div className="mb-2 flex items-center justify-between gap-3">
              <div>
                <div className="text-sm font-semibold text-slate-950">历史记录</div>
                <div className="text-xs text-slate-500">选择候选预览，或指定分支基图。</div>
              </div>
              {branchBaseRound ? (
                <button
                  type="button"
                  onClick={() => setBranchBaseAssetId(null)}
                  className="inline-flex items-center rounded-full border border-slate-200 px-3 py-1.5 text-xs font-medium text-slate-600 transition-colors hover:border-slate-300 hover:text-slate-900"
                >
                  <X size={12} className="mr-1" /> 清空基图
                </button>
              ) : null}
            </div>

            {roundGroups.length ? (
              <div className="flex min-h-0 flex-1 gap-3 overflow-x-auto pb-1">
                {roundGroups.map((group) => (
                  <div key={group.id} className="flex h-full shrink-0 gap-2 rounded-2xl border border-slate-200 bg-slate-50/80 p-2">
                    <div className="flex w-28 shrink-0 flex-col justify-between rounded-xl bg-white p-2 text-xs text-slate-500 ring-1 ring-slate-200">
                      <div>
                        <div className="font-semibold text-slate-800">{group.base_asset_id ? "分支" : "首轮"}</div>
                        <div className="mt-1">{group.rounds.length} 张</div>
                      </div>
                      <div className="line-clamp-3 text-[11px] leading-4 text-slate-400">{group.prompt}</div>
                    </div>
                    {group.rounds.map((round) => {
                      const active = round.generated_asset.id === selectedGeneratedAssetId;
                      const asBase = round.generated_asset.id === branchBaseAssetId;
                      return (
                        <div
                          key={round.id}
                          className={`group/card relative aspect-square h-full shrink-0 overflow-hidden rounded-2xl border bg-white transition-all ${
                            active ? "border-indigo-400 ring-2 ring-indigo-200" : "border-slate-200 hover:border-slate-300"
                          } ${asBase ? "shadow-md shadow-indigo-200/70" : "shadow-sm shadow-slate-200/60"}`}
                        >
                          <button
                            type="button"
                            onClick={() => setSelectedGeneratedAssetId(round.generated_asset.id)}
                            className="block h-full w-full text-left"
                          >
                            <img
                              src={api.toApiUrl(round.generated_asset.thumbnail_url)}
                              alt={round.prompt}
                              loading="lazy"
                              decoding="async"
                              className="h-full w-full object-cover"
                            />
                            <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-slate-950/80 via-slate-950/35 to-transparent p-2 pt-8 text-white">
                              <div className="flex items-center justify-between gap-2 text-[11px] font-medium">
                                <span>{round.candidate_count > 1 ? `${round.candidate_index}/${round.candidate_count}` : round.size}</span>
                                {active ? <span className="rounded-full bg-white/20 px-1.5 py-0.5">已选</span> : null}
                              </div>
                            </div>
                          </button>
                          {asBase ? (
                            <div className="absolute left-2 top-2 rounded-full bg-indigo-600 px-2 py-1 text-[11px] font-semibold text-white shadow-sm">
                              基图
                            </div>
                          ) : null}
                          <button
                            type="button"
                            onClick={() => handleContinueFrom(round.generated_asset.id)}
                            className={`absolute right-2 top-2 rounded-full px-2 py-1 text-[11px] font-semibold shadow-sm transition-colors ${
                              asBase
                                ? "bg-indigo-600 text-white"
                                : "bg-white/95 text-slate-700 opacity-100 ring-1 ring-slate-200 hover:text-indigo-700 md:opacity-0 md:group-hover/card:opacity-100"
                            }`}
                          >
                            从这张继续
                          </button>
                        </div>
                      );
                    })}
                  </div>
                ))}
              </div>
            ) : (
              <div className="flex min-h-0 flex-1 items-center justify-center rounded-2xl border border-dashed border-slate-200 bg-slate-50 text-sm text-slate-400">
                生成结果会出现在这里
              </div>
            )}
          </div>
        </section>

        <aside className="flex w-[clamp(320px,24vw,380px)] shrink-0 flex-col border-l border-slate-200 bg-white">
          <div className="min-h-0 flex-1 overflow-y-auto px-5 py-5">
            <div className="mb-5">
              <div className="mb-3 flex items-center justify-between gap-3">
                <div>
                  <div className="text-sm font-semibold text-slate-950">生成控制台</div>
                  <div className="mt-1 text-xs text-slate-500">名称、参考图、尺寸与保存。</div>
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

            <div className="space-y-4">
              {isProductMode ? (
                <ProductContextPanel
                  product={productQuery.data}
                  sourceImage={sourceImage}
                  referenceImages={productReferenceImages}
                  deletingReferenceAssetId={
                    deleteProductReferenceMutation.isPending ? (deleteProductReferenceMutation.variables ?? null) : null
                  }
                  onDeleteReference={handleDeleteProductReference}
                />
              ) : (
                <StandaloneTargetPanel
                  products={products}
                  targetProductId={targetProductId}
                  onTargetProductChange={setTargetProductId}
                />
              )}

              <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4">
                <div className="mb-2 flex items-center justify-between gap-3">
                  <div className="text-sm font-semibold text-slate-950">分支基图</div>
                  {branchBaseRound ? (
                    <button
                      type="button"
                      onClick={() => setBranchBaseAssetId(null)}
                      className="text-xs font-medium text-slate-500 transition-colors hover:text-slate-900"
                    >
                      清空
                    </button>
                  ) : null}
                </div>
                {branchBaseRound ? (
                  <div className="grid grid-cols-[76px_minmax(0,1fr)] gap-3">
                    <img
                      src={api.toApiUrl(branchBaseRound.generated_asset.thumbnail_url)}
                      alt="分支基图"
                      loading="lazy"
                      decoding="async"
                      className="h-20 w-full rounded-xl object-cover ring-1 ring-slate-200"
                    />
                    <div className="min-w-0 text-xs leading-5 text-slate-500">
                      <div className="truncate font-medium text-slate-800">{branchBaseRound.prompt}</div>
                      <div>{branchBaseRound.size}</div>
                      <div>本轮从这张继续。</div>
                    </div>
                  </div>
                ) : (
                  <div className="text-xs leading-5 text-slate-500">未指定基图，将按画面描述和已选参考图生成。</div>
                )}
              </div>

              <div className="rounded-2xl border border-slate-200 bg-white p-4">
                <div className="mb-2 text-sm font-semibold text-slate-950">参考图</div>
                <ImageDropZone
                  ariaLabel="上传会话参考图"
                  multiple
                  disabled={!selectedSessionId || uploadReferenceMutation.isPending}
                  className="flex cursor-pointer items-center justify-center rounded-xl border border-dashed border-slate-300 bg-slate-50 px-4 py-4 text-sm text-slate-600 transition-colors hover:border-indigo-300 hover:bg-indigo-50/40"
                  onFiles={handleUploadReferenceFiles}
                >
                  {({ isDragging }) => (
                    <>
                      {uploadReferenceMutation.isPending ? (
                        <Loader2 size={16} className="mr-2 animate-spin" />
                      ) : (
                        <ImagePlus size={16} className="mr-2" />
                      )}
                      {isDragging ? "松开上传" : "上传参考图"}
                    </>
                  )}
                </ImageDropZone>
                <div className="mt-2 text-xs leading-5 text-slate-500">
                  已选 {selectedReferenceAssetIds.length}/{maxSelectedReferenceCount}；勾选的图会进入下一轮。
                </div>
                {sessionReferenceAssets.length ? (
                  <div className="mt-3 grid grid-cols-4 gap-2">
                    {sessionReferenceAssets.map((asset) => {
                      const deleting = deleteSessionReferenceMutation.isPending && deleteSessionReferenceMutation.variables === asset.id;
                      const selected = selectedReferenceAssetIds.includes(asset.id);
                      const selectionLimitReached = !selected && selectedReferenceAssetIds.length >= maxSelectedReferenceCount;
                      return (
                        <div
                          key={asset.id}
                          className={`group relative overflow-hidden rounded-xl border bg-slate-50 ${
                            selected ? "border-indigo-500 ring-2 ring-indigo-100" : "border-slate-200"
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
                          <label className="absolute bottom-1 left-1 inline-flex items-center rounded-md bg-white/95 px-1.5 py-1 text-[11px] font-medium text-slate-700 shadow-sm ring-1 ring-slate-200">
                            <input
                              type="checkbox"
                              checked={selected}
                              disabled={selectionLimitReached}
                              onChange={(event) => handleReferenceToggle(asset.id, event.target.checked)}
                              className="mr-1 h-3 w-3 rounded border-slate-300 text-indigo-600 focus:ring-indigo-500"
                            />
                            使用
                          </label>
                          <button
                            type="button"
                            aria-label="删除参考图"
                            onClick={() => handleDeleteSessionReference(asset.id)}
                            disabled={deleting}
                            className="absolute right-1 top-1 inline-flex h-7 w-7 items-center justify-center rounded-lg bg-white/90 text-slate-500 opacity-100 shadow-sm ring-1 ring-slate-200 transition-colors hover:text-red-600 disabled:opacity-60 md:opacity-0 md:group-hover:opacity-100"
                          >
                            {deleting ? <Loader2 size={13} className="animate-spin" /> : <Trash2 size={13} />}
                          </button>
                        </div>
                      );
                    })}
                  </div>
                ) : null}
              </div>

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

              <div>
                <div className="mb-2 text-sm font-semibold text-slate-950">尺寸</div>
                <ImageSizePicker
                  value={size}
                  presets={sizeOptions}
                  maxDimension={imageGenerationMaxDimension}
                  onChange={setSize}
                />
              </div>

              <div>
                <label className="mb-2 block text-sm font-semibold text-slate-950" htmlFor="generation-count">
                  生成数量
                </label>
                <select
                  id="generation-count"
                  value={generationCount}
                  onChange={(event) => setGenerationCount(clampGenerationCount(Number(event.target.value)))}
                  className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm text-slate-900 focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-100"
                >
                  {[1, 2, 3, 4].map((count) => (
                    <option key={count} value={count}>
                      {count} 张候选
                    </option>
                  ))}
                </select>
              </div>

              <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4">
                <div className="mb-3 text-sm font-semibold text-slate-950">保存至商品库</div>
                {selectedRound ? (
                  <div className="space-y-3">
                    {!isProductMode ? (
                      <button
                        type="button"
                        onClick={() => handleAttach("reference")}
                        disabled={!targetProductId || attachMutation.isPending}
                        className="inline-flex w-full items-center justify-center rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm font-medium text-slate-700 transition-colors hover:border-slate-300 hover:text-slate-950 disabled:opacity-60"
                      >
                        {attachMutation.isPending ? <Loader2 size={14} className="mr-2 animate-spin" /> : <Check size={14} className="mr-2" />}
                        保存为参考图
                      </button>
                    ) : (
                      <div className="grid gap-2">
                        <button
                          type="button"
                          onClick={() => handleAttach("reference")}
                          disabled={attachMutation.isPending}
                          className="inline-flex items-center justify-center rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm font-medium text-slate-700 transition-colors hover:border-slate-300 hover:text-slate-950 disabled:opacity-60"
                        >
                          {attachMutation.isPending ? <Loader2 size={14} className="mr-2 animate-spin" /> : <Check size={14} className="mr-2" />}
                          加入参考图
                        </button>
                        <button
                          type="button"
                          onClick={() => handleAttach("main_source")}
                          disabled={attachMutation.isPending}
                          className="inline-flex items-center justify-center rounded-xl bg-slate-900 px-3 py-2 text-sm font-semibold text-white transition-colors hover:bg-slate-800 disabled:opacity-60"
                        >
                          {attachMutation.isPending ? <Loader2 size={14} className="mr-2 animate-spin" /> : <ImageIcon size={14} className="mr-2" />}
                          设为商品主图
                        </button>
                      </div>
                    )}
                  </div>
                ) : (
                  <div className="text-sm text-slate-500">生成图片后可保存。</div>
                )}
              </div>

              {visibleGenerationTasks.length ? (
                <div className="space-y-2 rounded-2xl border border-slate-200 bg-white p-4">
                  <div className="text-sm font-semibold text-slate-950">生成任务</div>
                  {visibleGenerationTasks.map((task) => {
                    const active = isActiveGenerationTask(task);
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
                        <div className="mt-1 line-clamp-2 text-xs opacity-80">{task.prompt}</div>
                        {task.status === "failed" ? (
                          <div className="mt-1 text-xs">{task.failure_reason ?? "图片生成失败，请稍后重试"}</div>
                        ) : (
                          <div className="mt-1 text-xs opacity-70">
                            {generationTaskQueueText(task)}
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              ) : null}

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

          <div className="sticky bottom-0 border-t border-slate-200 bg-white/95 p-4 shadow-[0_-8px_24px_rgba(15,23,42,0.06)] backdrop-blur">
            <button
              type="button"
              onClick={handleGenerate}
              disabled={!selectedSessionId || !draft.trim() || generateMutation.isPending || hasActiveGenerationTask}
              className="inline-flex w-full items-center justify-center rounded-2xl bg-indigo-600 px-4 py-3.5 text-sm font-semibold text-white shadow-lg shadow-indigo-600/20 transition-colors hover:bg-indigo-500 disabled:opacity-60"
            >
              {generateMutation.isPending || hasActiveGenerationTask ? (
                <Loader2 size={15} className="mr-2 animate-spin" />
              ) : (
                <Sparkles size={15} className="mr-2" />
              )}
              {hasActiveGenerationTask ? "已有任务生成中" : generationCount > 1 ? `开始生成 · ${generationCount} 张候选` : "开始生成"}
            </button>
          </div>
        </aside>
      </main>
    </div>
  );

}

function ProductContextPanel({
  product,
  sourceImage,
  referenceImages,
  deletingReferenceAssetId,
  onDeleteReference,
}: {
  product: ProductDetail | undefined;
  sourceImage: SourceAsset | null;
  referenceImages: SourceAsset[];
  deletingReferenceAssetId: string | null;
  onDeleteReference: (assetId: string) => void;
}) {
  return (
    <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4">
      <div className="mb-3 text-sm font-semibold text-zinc-900">商品上下文</div>
      {product ? (
        <>
          <div className="mb-3 text-xs text-zinc-500">商品图用于回看与保存；连续生图只使用你选择的分支基图和会话参考图。</div>
          <div className="grid grid-cols-[88px_minmax(0,1fr)] gap-3">
            <div className="overflow-hidden rounded-lg border border-zinc-200 bg-white">
              {sourceImage ? (
                <img src={api.toApiUrl(sourceImage.thumbnail_url)} alt={product.name} decoding="async" className="h-24 w-full object-cover" />
              ) : (
                <div className="flex h-24 items-center justify-center text-zinc-300">
                  <ImageIcon size={20} />
                </div>
              )}
            </div>
            <div className="min-w-0">
              <div className="truncate font-medium text-zinc-900">{product.name}</div>
              <div className="mt-1 text-xs text-zinc-500">当前参考图 {referenceImages.length} 张</div>
            </div>
          </div>
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
        </>
      ) : (
        <div className="flex justify-center py-6 text-zinc-400">
          <Loader2 size={16} className="animate-spin" />
        </div>
      )}
    </div>
  );
}

function StandaloneTargetPanel({
  products,
  targetProductId,
  onTargetProductChange,
}: {
  products: ProductSummary[];
  targetProductId: string;
  onTargetProductChange: (value: string) => void;
}) {
  return (
    <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4">
      <div className="mb-2 text-sm font-semibold text-zinc-900">目标商品</div>
      <div className="text-xs leading-5 text-zinc-500">先自由生成，满意后再把图片保存到已有商品。</div>
      <select
        value={targetProductId}
        onChange={(event) => onTargetProductChange(event.target.value)}
        className="mt-3 w-full rounded-md border border-zinc-200 bg-white px-3 py-2 text-sm text-zinc-900 focus:border-zinc-900 focus:outline-none focus:ring-1 focus:ring-zinc-900"
      >
        {products.map((product) => (
          <option key={product.id} value={product.id}>
            {product.name}
          </option>
        ))}
      </select>
    </div>
  );
}
