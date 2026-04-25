import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Check,
  Image as ImageIcon,
  ImagePlus,
  Loader2,
  MessagesSquare,
  Pencil,
  Plus,
  Save,
  Sparkles,
  Trash2,
} from "lucide-react";
import { useNavigate, useParams } from "react-router-dom";

import { ImageDropZone } from "../components/ImageDropZone";
import { TopNav } from "../components/TopNav";
import { api, ApiError } from "../lib/api";
import { formatDateTime } from "../lib/format";
import type {
  ConfigResponse,
  ImageSessionDetail,
  ImageSessionListResponse,
  ProductDetail,
  ProductSummary,
  SourceAsset,
} from "../lib/types";

const DEFAULT_SIZE_OPTIONS = [
  { label: "1:1", value: "1024x1024" },
  { label: "3:4", value: "1024x1536" },
  { label: "横图", value: "1536x1024" },
] as const;
const SIZE_LABELS = new Map<string, string>(DEFAULT_SIZE_OPTIONS.map((option) => [option.value, option.label]));
const IMAGE_SIZE_PATTERN = /^\d+x\d+$/;

function getSessionReferenceAssets(imageSession: ImageSessionDetail | undefined) {
  return imageSession?.assets.filter((asset) => asset.kind === "reference_upload") ?? [];
}

function getAllowedSizeOptions(config: ConfigResponse | undefined) {
  const rawValue = config?.items.find((item) => item.key === "image_allowed_sizes")?.value;
  if (typeof rawValue !== "string") {
    return DEFAULT_SIZE_OPTIONS.map((option) => ({ value: option.value, label: option.label }));
  }
  const sizes = rawValue
    .split(",")
    .map((sizeValue) => sizeValue.trim().toLowerCase())
    .filter((sizeValue, index, allSizeValues) => IMAGE_SIZE_PATTERN.test(sizeValue) && allSizeValues.indexOf(sizeValue) === index);
  return sizes.map((value) => ({ value, label: SIZE_LABELS.get(value) ?? value }));
}

export function ImageChatPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { productId } = useParams();
  const isProductMode = Boolean(productId);
  const autoCreateTriggered = useRef(false);

  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(null);
  const [selectedGeneratedAssetId, setSelectedGeneratedAssetId] = useState<string | null>(null);
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

  const products = productsQuery.data?.items ?? [];

  const configQuery = useQuery({
    queryKey: ["config"],
    queryFn: api.getConfig,
  });
  const sizeOptions = useMemo(() => getAllowedSizeOptions(configQuery.data), [configQuery.data]);
  const sizeConfigReady = !configQuery.isLoading && !configQuery.isError && sizeOptions.length > 0;

  useEffect(() => {
    if (!sizeOptions.length || sizeOptions.some((option) => option.value === size)) {
      return;
    }
    setSize(sizeOptions[0].value);
  }, [sizeOptions, size]);

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
  });

  const imageSession = sessionDetailQuery.data;
  const sessionReferenceAssets = useMemo(() => getSessionReferenceAssets(imageSession), [imageSession]);

  useEffect(() => {
    if (!imageSession) {
      return;
    }
    setTitleDraft(imageSession.title);
    if (!selectedGeneratedAssetId || !imageSession.rounds.some((round) => round.generated_asset.id === selectedGeneratedAssetId)) {
      setSelectedGeneratedAssetId(imageSession.rounds.at(-1)?.generated_asset.id ?? null);
    }
  }, [imageSession, selectedGeneratedAssetId]);

  const selectedRound = useMemo(() => {
    if (!imageSession?.rounds.length) {
      return null;
    }
    return (
      imageSession.rounds.find((round) => round.generated_asset.id === selectedGeneratedAssetId) ?? imageSession.rounds.at(-1) ?? null
    );
  }, [imageSession, selectedGeneratedAssetId]);

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
      queryClient.setQueryData(["image-session", updated.id], updated);
      void queryClient.invalidateQueries({ queryKey: ["image-sessions", productId ?? "standalone"] });
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
      setSuccessMessage("参考图已删除");
      setErrorMessage("");
    },
    onError: (error) => {
      setErrorMessage(error instanceof ApiError ? error.detail : "参考图删除失败");
    },
  });

  const generateMutation = useMutation({
    mutationFn: (payload: { prompt: string; size: string }) => api.generateImageSessionRound(selectedSessionId!, payload),
    onSuccess: (updated) => {
      queryClient.setQueryData(["image-session", updated.id], updated);
      void queryClient.invalidateQueries({ queryKey: ["image-sessions", productId ?? "standalone"] });
      setSelectedGeneratedAssetId(updated.rounds.at(-1)?.generated_asset.id ?? null);
      setDraft("");
      setSuccessMessage("新一轮图片已生成");
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
    if (!sizeConfigReady) {
      const message = configQuery.isError
        ? "尺寸配置加载失败，请刷新后重试"
        : sizeOptions.length
          ? "尺寸配置加载中，请稍后再试"
          : "当前没有可用生图尺寸，请先在系统配置中设置";
      setErrorMessage(message);
      return;
    }
    generateMutation.mutate({ prompt, size });
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
    <div className="flex min-h-screen flex-col bg-zinc-50/60">
      <TopNav
        breadcrumbs={isProductMode ? `${productQuery.data?.name ?? "商品"} / 连续生图` : "连续生图"}
        onHome={() => navigate(isProductMode && productId ? `/products/${productId}` : "/products")}
        onLogout={() => logoutMutation.mutate()}
      />

      <main className="mx-auto flex w-full max-w-[1500px] flex-1 gap-6 px-6 py-8">
        <aside className="flex w-80 shrink-0 flex-col gap-4 rounded-2xl border border-zinc-200 bg-white p-4 shadow-sm">
          <div className="flex items-center justify-between">
            <div>
              <div className="text-sm font-semibold text-zinc-900">会话列表</div>
              <div className="mt-1 text-xs text-zinc-500">
                {isProductMode ? "为同一商品保存不同创意方向。" : "先自由生成，满意后再保存到商品。"}
              </div>
            </div>
            <button
              type="button"
              onClick={() => createSessionMutation.mutate()}
              className="inline-flex h-9 items-center rounded-md border border-zinc-200 px-3 text-sm font-medium text-zinc-700 transition-colors hover:border-zinc-300 hover:text-zinc-900"
            >
              {createSessionMutation.isPending ? <Loader2 size={14} className="animate-spin" /> : <Plus size={14} />}
            </button>
          </div>

          <div className="space-y-2 overflow-y-auto">
            {sessionsQuery.isLoading ? (
              <div className="flex justify-center py-12 text-zinc-400">
                <Loader2 size={18} className="animate-spin" />
              </div>
            ) : (
              sessionItems.map((item) => {
                const active = item.id === selectedSessionId;
                const deleting = deleteSessionMutation.isPending && deleteSessionMutation.variables === item.id;
                return (
                  <div
                    key={item.id}
                    className={`group relative rounded-xl border transition-colors ${
                      active ? "border-zinc-900 bg-zinc-50" : "border-zinc-200 hover:border-zinc-300 hover:bg-zinc-50"
                    }`}
                  >
                    <button
                      type="button"
                      onClick={() => {
                        setSelectedSessionId(item.id);
                        setSuccessMessage("");
                        setErrorMessage("");
                      }}
                      className="flex w-full items-start gap-3 p-3 pr-10 text-left"
                    >
                      <div className="flex h-14 w-14 shrink-0 items-center justify-center overflow-hidden rounded-lg bg-zinc-100 text-zinc-400">
                        {item.latest_generated_asset ? (
                          <img
                            src={api.toApiUrl(item.latest_generated_asset.thumbnail_url)}
                            alt={item.title}
                            loading="lazy"
                            decoding="async"
                            className="h-full w-full object-cover"
                          />
                        ) : (
                          <MessagesSquare size={16} />
                        )}
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className="truncate font-medium text-zinc-900">{item.title}</div>
                        <div className="mt-1 text-xs text-zinc-500">{item.rounds_count} 轮 · {formatDateTime(item.updated_at)}</div>
                      </div>
                    </button>
                    <button
                      type="button"
                      aria-label="删除会话"
                      onClick={() => handleDeleteSession(item.id)}
                      disabled={deleting}
                      className="absolute right-2 top-2 inline-flex h-7 w-7 items-center justify-center rounded-md bg-white/90 text-zinc-400 opacity-100 shadow-sm ring-1 ring-zinc-200 transition-colors hover:text-red-600 disabled:opacity-60 md:opacity-0 md:group-hover:opacity-100"
                    >
                      {deleting ? <Loader2 size={13} className="animate-spin" /> : <Trash2 size={13} />}
                    </button>
                  </div>
                );
              })
            )}
          </div>
        </aside>

        <section className="grid min-w-0 flex-1 gap-6 xl:grid-cols-[minmax(0,1.25fr)_420px]">
          <div className="flex min-h-[780px] flex-col rounded-2xl border border-zinc-200 bg-white p-5 shadow-sm">
            <div className="mb-4 flex items-center justify-between gap-3">
              <div>
                <div className="text-sm font-semibold text-zinc-900">当前结果</div>
                <div className="mt-1 text-xs text-zinc-500">
                  {selectedRound ? `${selectedRound.size} · ${formatDateTime(selectedRound.created_at)}` : "先输入需求，生成第一轮。"}
                </div>
              </div>
              {selectedRound ? (
                <a
                  href={api.toApiUrl(selectedRound.generated_asset.download_url)}
                  target="_blank"
                  rel="noreferrer"
                  className="inline-flex items-center rounded-md border border-zinc-200 px-3 py-2 text-sm font-medium text-zinc-700 transition-colors hover:border-zinc-300 hover:text-zinc-900"
                >
                  <ImageIcon size={14} className="mr-1.5" /> 下载原图
                </a>
              ) : null}
            </div>

            <div className="flex flex-1 items-center justify-center rounded-2xl border border-dashed border-zinc-200 bg-zinc-50 p-4">
              {selectedRound ? (
                <img
                  src={api.toApiUrl(selectedRound.generated_asset.preview_url)}
                  alt="当前结果"
                  decoding="async"
                  className="max-h-[620px] w-full rounded-xl object-contain"
                />
              ) : (
                <div className="flex flex-col items-center gap-3 text-zinc-400">
                  <Sparkles size={28} />
                  <div className="text-sm">当前会话还没有结果</div>
                </div>
              )}
            </div>

            <div className="mt-4 grid grid-cols-2 gap-3 md:grid-cols-4 xl:grid-cols-5">
              {imageSession?.rounds.map((round) => {
                const active = round.generated_asset.id === selectedGeneratedAssetId;
                return (
                  <button
                    key={round.id}
                    type="button"
                    onClick={() => setSelectedGeneratedAssetId(round.generated_asset.id)}
                    className={`rounded-xl border p-2 text-left transition-colors ${
                      active ? "border-zinc-900 bg-zinc-50" : "border-zinc-200 hover:border-zinc-300 hover:bg-zinc-50"
                    }`}
                  >
                    <img
                      src={api.toApiUrl(round.generated_asset.thumbnail_url)}
                      alt={round.prompt}
                      loading="lazy"
                      decoding="async"
                      className="h-24 w-full rounded-lg object-cover"
                    />
                    <div className="mt-2 line-clamp-2 text-xs text-zinc-600">{round.prompt}</div>
                  </button>
                );
              })}
            </div>
          </div>

          <aside className="flex min-h-[780px] flex-col gap-4 rounded-2xl border border-zinc-200 bg-white p-5 shadow-sm">
            <div>
              <div className="mb-2 flex items-center justify-between gap-3">
                <div>
                  <div className="text-sm font-semibold text-zinc-900">会话设置</div>
                  <div className="mt-1 text-xs text-zinc-500">管理名称、参考图和本轮生成需求。</div>
                </div>
                <button
                  type="button"
                  onClick={() => setRenameEnabled((current) => !current)}
                  className="inline-flex h-8 items-center rounded-md border border-zinc-200 px-2.5 text-xs font-medium text-zinc-600 transition-colors hover:border-zinc-300 hover:text-zinc-900"
                >
                  <Pencil size={12} className="mr-1.5" /> 重命名
                </button>
              </div>
              <div className="flex gap-2">
                <input
                  value={titleDraft}
                  onChange={(event) => setTitleDraft(event.target.value)}
                  disabled={!renameEnabled || renameSessionMutation.isPending}
                  className="w-full rounded-md border border-zinc-200 bg-white px-3 py-2 text-sm text-zinc-900 focus:border-zinc-900 focus:outline-none focus:ring-1 focus:ring-zinc-900 disabled:bg-zinc-50 disabled:text-zinc-500"
                />
                {renameEnabled ? (
                  <button
                    type="button"
                    onClick={handleRename}
                    className="inline-flex items-center rounded-md bg-zinc-900 px-3 py-2 text-sm font-medium text-white transition-colors hover:bg-zinc-800"
                  >
                    {renameSessionMutation.isPending ? <Loader2 size={14} className="animate-spin" /> : <Save size={14} />}
                  </button>
                ) : null}
              </div>
            </div>

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

            <div>
              <div className="mb-2 text-sm font-semibold text-zinc-900">参考图</div>
              <ImageDropZone
                ariaLabel="上传会话参考图"
                multiple
                disabled={!selectedSessionId || uploadReferenceMutation.isPending}
                className="flex cursor-pointer items-center justify-center rounded-xl border border-dashed border-zinc-300 bg-zinc-50 px-4 py-4 text-sm text-zinc-600 transition-colors hover:border-zinc-400 hover:bg-zinc-100"
                onFiles={handleUploadReferenceFiles}
              >
                {({ isDragging }) => (
                  <>
                    {uploadReferenceMutation.isPending ? (
                      <Loader2 size={16} className="mr-2 animate-spin" />
                    ) : (
                      <ImagePlus size={16} className="mr-2" />
                    )}
                    {isDragging ? "松开以上传参考图" : "拖拽或点击上传会话参考图"}
                  </>
                )}
              </ImageDropZone>
              <div className="mt-3 grid grid-cols-3 gap-2">
                {sessionReferenceAssets.map((asset) => {
                  const deleting = deleteSessionReferenceMutation.isPending && deleteSessionReferenceMutation.variables === asset.id;
                  return (
                    <div key={asset.id} className="group relative overflow-hidden rounded-lg border border-zinc-200 bg-zinc-50">
                      <a href={api.toApiUrl(asset.preview_url)} target="_blank" rel="noreferrer" title={asset.original_filename}>
                        <img
                          src={api.toApiUrl(asset.thumbnail_url)}
                          alt={asset.original_filename}
                          loading="lazy"
                          decoding="async"
                          className="h-24 w-full object-cover"
                        />
                      </a>
                      <button
                        type="button"
                        aria-label="删除参考图"
                        onClick={() => handleDeleteSessionReference(asset.id)}
                        disabled={deleting}
                        className="absolute right-1 top-1 inline-flex h-7 w-7 items-center justify-center rounded-md bg-white/90 text-zinc-500 opacity-100 shadow-sm ring-1 ring-zinc-200 transition-colors hover:text-red-600 disabled:opacity-60 md:opacity-0 md:group-hover:opacity-100"
                      >
                        {deleting ? <Loader2 size={13} className="animate-spin" /> : <Trash2 size={13} />}
                      </button>
                    </div>
                  );
                })}
              </div>
            </div>

            <div>
              <div className="mb-2 text-sm font-semibold text-zinc-900">本轮需求</div>
              <textarea
                value={draft}
                onChange={(event) => setDraft(event.target.value)}
                rows={5}
                placeholder={isProductMode ? "描述这一轮要在现有商品基础上改什么。" : "描述你想生成什么图。"}
                className="w-full resize-none rounded-xl border border-zinc-200 px-3 py-3 text-sm leading-6 text-zinc-900 focus:border-zinc-900 focus:outline-none focus:ring-1 focus:ring-zinc-900"
              />
            </div>

            <div>
              <div className="mb-2 text-sm font-semibold text-zinc-900">尺寸</div>
              <div className="grid grid-cols-3 gap-2">
                {sizeOptions.map((option) => {
                  const active = option.value === size;
                  return (
                    <button
                      key={option.value}
                      type="button"
                      onClick={() => setSize(option.value)}
                      disabled={!sizeConfigReady}
                      className={`rounded-md border px-3 py-2 text-sm transition-colors ${
                        active
                          ? "border-zinc-900 bg-zinc-900 text-white"
                          : "border-zinc-200 bg-white text-zinc-600 hover:border-zinc-300 hover:text-zinc-900"
                      }`}
                    >
                      {option.label}
                    </button>
                  );
                })}
              </div>
              {configQuery.isLoading ? (
                <div className="mt-2 text-xs text-zinc-500">正在读取系统允许尺寸...</div>
              ) : null}
              {configQuery.isError ? (
                <div className="mt-2 text-xs text-red-600">尺寸配置加载失败，暂不能继续生成。</div>
              ) : null}
              {!configQuery.isLoading && !configQuery.isError && !sizeOptions.length ? (
                <div className="mt-2 text-xs text-red-600">当前没有可用生图尺寸，请先在系统配置中设置。</div>
              ) : null}
            </div>

            {successMessage ? (
              <div className="rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm text-emerald-700">
                {successMessage}
              </div>
            ) : null}
            {errorMessage ? (
              <div className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{errorMessage}</div>
            ) : null}

            <button
              type="button"
              onClick={handleGenerate}
              disabled={!selectedSessionId || !draft.trim() || generateMutation.isPending || !sizeConfigReady}
              className="inline-flex w-full items-center justify-center rounded-xl bg-zinc-900 px-4 py-3 text-sm font-medium text-white transition-colors hover:bg-zinc-800 disabled:opacity-60"
            >
              {generateMutation.isPending ? <Loader2 size={14} className="mr-2 animate-spin" /> : <Sparkles size={14} className="mr-2" />}
              继续生成
            </button>

            <div className="mt-auto rounded-2xl border border-zinc-200 bg-zinc-50 p-4">
              <div className="mb-3 text-sm font-semibold text-zinc-900">保存到商品</div>
              {selectedRound ? (
                <div className="space-y-3">
                  {!isProductMode ? (
                    <button
                      type="button"
                      onClick={() => handleAttach("reference")}
                      disabled={!targetProductId || attachMutation.isPending}
                      className="inline-flex w-full items-center justify-center rounded-md border border-zinc-200 bg-white px-3 py-2 text-sm font-medium text-zinc-700 transition-colors hover:border-zinc-300 hover:text-zinc-900 disabled:opacity-60"
                    >
                      {attachMutation.isPending ? <Loader2 size={14} className="mr-2 animate-spin" /> : <Check size={14} className="mr-2" />}
                      保存为所选商品参考图
                    </button>
                  ) : (
                    <div className="grid gap-2 md:grid-cols-2">
                      <button
                        type="button"
                        onClick={() => handleAttach("reference")}
                        disabled={attachMutation.isPending}
                        className="inline-flex items-center justify-center rounded-md border border-zinc-200 bg-white px-3 py-2 text-sm font-medium text-zinc-700 transition-colors hover:border-zinc-300 hover:text-zinc-900 disabled:opacity-60"
                      >
                        {attachMutation.isPending ? <Loader2 size={14} className="mr-2 animate-spin" /> : <Check size={14} className="mr-2" />}
                        加入参考图
                      </button>
                      <button
                        type="button"
                        onClick={() => handleAttach("main_source")}
                        disabled={attachMutation.isPending}
                        className="inline-flex items-center justify-center rounded-md bg-zinc-900 px-3 py-2 text-sm font-medium text-white transition-colors hover:bg-zinc-800 disabled:opacity-60"
                      >
                        {attachMutation.isPending ? <Loader2 size={14} className="mr-2 animate-spin" /> : <ImageIcon size={14} className="mr-2" />}
                        设为商品主图
                      </button>
                    </div>
                  )}
                  <p className="text-xs leading-5 text-zinc-500">
                    {isProductMode
                      ? "设为商品主图时，旧主图会保留为参考图，方便继续创作和回看。"
                      : "只有点击保存后，图片才会加入商品素材。"}
                  </p>
                </div>
              ) : (
                <div className="text-sm text-zinc-500">先生成至少一张图，再决定是否保存到商品。</div>
              )}
            </div>
          </aside>
        </section>
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
    <div className="rounded-2xl border border-zinc-200 bg-zinc-50 p-4">
      <div className="mb-3 text-sm font-semibold text-zinc-900">商品上下文</div>
      {product ? (
        <>
          <div className="mb-3 text-xs text-zinc-500">生成时会参考商品主图和已添加的商品参考图。</div>
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
    <div className="rounded-2xl border border-zinc-200 bg-zinc-50 p-4">
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
