import type {
  ConfigResponse,
  ConfigUpdateRequest,
  CopySet,
  CopySetUpdateRequest,
  GalleryEntry,
  GalleryEntryListResponse,
  GenerationQueueOverview,
  ImageSessionDetail,
  ImageSessionListResponse,
  ImageToolOptions,
  JobRun,
  ProductDetail,
  ProductHistory,
  ProductWorkflow,
  ProductWritebackResponse,
  ProductListResponse,
  RuntimeConfig,
  SettingsLockState,
  SessionState,
} from "./types";

const API_BASE_URL = (import.meta.env.VITE_API_BASE_URL as string | undefined)?.replace(/\/$/, "") ?? "";

export class ApiError extends Error {
  status: number;
  detail: string;

  constructor(status: number, detail: string) {
    super(detail);
    this.status = status;
    this.detail = detail;
  }
}

function toApiUrl(path: string): string {
  if (path.startsWith("http://") || path.startsWith("https://")) {
    return path;
  }
  return `${API_BASE_URL}${path}`;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(toApiUrl(path), {
    credentials: "include",
    headers: {
      ...(init?.body instanceof FormData ? {} : { "Content-Type": "application/json" }),
      ...init?.headers,
    },
    ...init,
  });

  if (!response.ok) {
    let detail = "请求失败";
    try {
      const payload = (await response.json()) as { detail?: string };
      detail = payload.detail ?? detail;
    } catch {
      detail = response.statusText || detail;
    }
    throw new ApiError(response.status, detail);
  }

  if (response.status === 204) {
    return undefined as T;
  }
  return (await response.json()) as T;
}

export const api = {
  toApiUrl,
  getSessionState(): Promise<SessionState> {
    return request<SessionState>("/api/auth/session");
  },
  createSession(adminKey: string): Promise<{ ok: boolean }> {
    return request("/api/auth/session", {
      method: "POST",
      body: JSON.stringify({ admin_key: adminKey }),
    });
  },
  destroySession(): Promise<{ ok: boolean }> {
    return request("/api/auth/session", { method: "DELETE" });
  },
  listProducts(input?: { page?: number; page_size?: number }): Promise<ProductListResponse> {
    const page = input?.page ?? 1;
    const pageSize = input?.page_size ?? 20;
    return request(`/api/products?page=${encodeURIComponent(page)}&page_size=${encodeURIComponent(pageSize)}`);
  },
  getProduct(productId: string): Promise<ProductDetail> {
    return request(`/api/products/${productId}`);
  },
  deleteProduct(productId: string): Promise<void> {
    return request(`/api/products/${productId}`, { method: "DELETE" });
  },
  getProductHistory(productId: string): Promise<ProductHistory> {
    return request(`/api/products/${productId}/history`);
  },
  getConfig(): Promise<ConfigResponse> {
    return request("/api/settings");
  },
  getSettingsLockState(): Promise<SettingsLockState> {
    return request("/api/settings/lock-state");
  },
  getRuntimeConfig(): Promise<RuntimeConfig> {
    return request("/api/settings/runtime");
  },
  getGenerationQueueOverview(): Promise<GenerationQueueOverview> {
    return request("/api/generation-queue");
  },
  unlockSettings(token: string): Promise<SettingsLockState> {
    return request("/api/settings/unlock", {
      method: "POST",
      body: JSON.stringify({ token }),
    });
  },
  updateConfig(payload: ConfigUpdateRequest): Promise<ConfigResponse> {
    return request("/api/settings", {
      method: "PATCH",
      body: JSON.stringify(payload),
    });
  },
  async createProduct(input: {
    name: string;
    category?: string;
    price?: string;
    source_note?: string;
    file: File;
    referenceFiles?: File[];
  }): Promise<ProductDetail> {
    const formData = new FormData();
    formData.set("name", input.name);
    formData.set("image", input.file);
    input.referenceFiles?.forEach((referenceFile) => {
      formData.append("reference_images", referenceFile);
    });
    if (input.category) {
      formData.set("category", input.category);
    }
    if (input.price) {
      formData.set("price", input.price);
    }
    if (input.source_note) {
      formData.set("source_note", input.source_note);
    }
    return request("/api/products", {
      method: "POST",
      body: formData,
    });
  },
  async addReferenceImages(productId: string, files: File[]): Promise<ProductDetail> {
    const formData = new FormData();
    files.forEach((file) => {
      formData.append("reference_images", file);
    });
    return request(`/api/products/${productId}/reference-images`, {
      method: "POST",
      body: formData,
    });
  },
  deleteSourceAsset(assetId: string): Promise<ProductDetail> {
    return request(`/api/source-assets/${assetId}`, { method: "DELETE" });
  },
  createCopyJob(productId: string): Promise<JobRun> {
    return request(`/api/products/${productId}/copy-jobs`, { method: "POST" });
  },
  updateCopySet(copySetId: string, payload: CopySetUpdateRequest): Promise<CopySet> {
    return request(`/api/copy-sets/${copySetId}`, {
      method: "PATCH",
      body: JSON.stringify(payload),
    });
  },
  confirmCopySet(copySetId: string): Promise<CopySet> {
    return request(`/api/copy-sets/${copySetId}/confirm`, { method: "POST" });
  },
  createPosterJob(productId: string): Promise<JobRun> {
    return request(`/api/products/${productId}/poster-jobs`, { method: "POST" });
  },
  regeneratePoster(posterId: string): Promise<JobRun> {
    return request(`/api/posters/${posterId}/regenerate`, { method: "POST" });
  },
  getJob(jobId: string): Promise<JobRun> {
    return request(`/api/jobs/${jobId}`);
  },
  listImageSessions(productId?: string): Promise<ImageSessionListResponse> {
    const query = productId ? `?product_id=${encodeURIComponent(productId)}` : "";
    return request(`/api/image-sessions${query}`);
  },
  createImageSession(input: { product_id?: string; title?: string }): Promise<ImageSessionDetail> {
    return request("/api/image-sessions", {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  getImageSession(sessionId: string): Promise<ImageSessionDetail> {
    return request(`/api/image-sessions/${sessionId}`);
  },
  updateImageSession(sessionId: string, input: { title: string }): Promise<ImageSessionDetail> {
    return request(`/api/image-sessions/${sessionId}`, {
      method: "PATCH",
      body: JSON.stringify(input),
    });
  },
  deleteImageSession(sessionId: string): Promise<void> {
    return request(`/api/image-sessions/${sessionId}`, { method: "DELETE" });
  },
  async addImageSessionReferenceImages(sessionId: string, files: File[]): Promise<ImageSessionDetail> {
    const formData = new FormData();
    files.forEach((file) => {
      formData.append("reference_images", file);
    });
    return request(`/api/image-sessions/${sessionId}/reference-images`, {
      method: "POST",
      body: formData,
    });
  },
  deleteImageSessionReferenceImage(sessionId: string, assetId: string): Promise<ImageSessionDetail> {
    return request(`/api/image-sessions/${sessionId}/reference-images/${assetId}`, { method: "DELETE" });
  },
  generateImageSessionRound(
    sessionId: string,
    input: {
      prompt: string;
      size: string;
      base_asset_id?: string | null;
      selected_reference_asset_ids?: string[];
      generation_count?: number;
      tool_options?: ImageToolOptions | null;
    },
  ): Promise<ImageSessionDetail> {
    return request(`/api/image-sessions/${sessionId}/generate`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  attachImageSessionAssetToProduct(
    sessionId: string,
    assetId: string,
    input: { product_id?: string; target: "reference" | "main_source" },
  ): Promise<ProductWritebackResponse> {
    return request(`/api/image-sessions/${sessionId}/assets/${assetId}/attach-to-product`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  listGalleryEntries(): Promise<GalleryEntryListResponse> {
    return request("/api/gallery");
  },
  saveGalleryEntry(imageSessionAssetId: string): Promise<GalleryEntry> {
    return request("/api/gallery", {
      method: "POST",
      body: JSON.stringify({ image_session_asset_id: imageSessionAssetId }),
    });
  },
  getProductWorkflow(productId: string): Promise<ProductWorkflow> {
    return request(`/api/products/${productId}/workflow`);
  },
  createWorkflowNode(
    productId: string,
    input: {
      node_type: ProductWorkflow["nodes"][number]["node_type"];
      title: string;
      position_x: number;
      position_y: number;
      config_json: Record<string, unknown>;
    },
  ): Promise<ProductWorkflow> {
    return request(`/api/products/${productId}/workflow/nodes`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  updateWorkflowNode(
    nodeId: string,
    input: {
      title?: string;
      position_x?: number;
      position_y?: number;
      config_json?: Record<string, unknown>;
    },
  ): Promise<ProductWorkflow> {
    return request(`/api/workflow-nodes/${nodeId}`, {
      method: "PATCH",
      body: JSON.stringify(input),
    });
  },
  updateWorkflowNodeCopy(nodeId: string, payload: CopySetUpdateRequest): Promise<ProductWorkflow> {
    return request(`/api/workflow-nodes/${nodeId}/copy`, {
      method: "PATCH",
      body: JSON.stringify(payload),
    });
  },
  async uploadWorkflowNodeImage(
    nodeId: string,
    input: { file: File; role?: string; label?: string },
  ): Promise<ProductWorkflow> {
    const formData = new FormData();
    formData.set("image", input.file);
    if (input.role) {
      formData.set("role", input.role);
    }
    if (input.label) {
      formData.set("label", input.label);
    }
    return request(`/api/workflow-nodes/${nodeId}/image`, {
      method: "POST",
      body: formData,
    });
  },
  bindWorkflowNodeImage(
    nodeId: string,
    input: { source_asset_id?: string; poster_variant_id?: string },
  ): Promise<ProductWorkflow> {
    return request(`/api/workflow-nodes/${nodeId}/image-source`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  createWorkflowEdge(
    productId: string,
    input: { source_node_id: string; target_node_id: string; source_handle?: string; target_handle?: string },
  ): Promise<ProductWorkflow> {
    return request(`/api/products/${productId}/workflow/edges`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  deleteWorkflowEdge(edgeId: string): Promise<ProductWorkflow> {
    return request(`/api/workflow-edges/${edgeId}`, { method: "DELETE" });
  },
  deleteWorkflowNode(nodeId: string): Promise<ProductWorkflow> {
    return request(`/api/workflow-nodes/${nodeId}`, { method: "DELETE" });
  },
  runProductWorkflow(productId: string, input?: { start_node_id?: string }): Promise<ProductWorkflow> {
    return request(`/api/products/${productId}/workflow/run`, {
      method: "POST",
      body: JSON.stringify(input ?? {}),
    });
  },
};
