import type {
  ApplyWorkflowTemplateGroupInput,
  CanvasTemplateSummary,
  CanvasTemplateListResponse,
  ConfigResponse,
  ConfigUpdateRequest,
  CopySet,
  CopySetUpdateRequest,
  DuplicateWorkflowNodeGroupInput,
  GalleryEntry,
  GalleryEntryListResponse,
  GenerationQueueOverview,
  CreateUserTemplateGroupInput,
  CreateProductInput,
  ImageSessionDetail,
  ImageSessionListResponse,
  ImageSessionStatus,
  ImageToolOptions,
  LaunchKitCreateRequest,
  LaunchKitDetail,
  LaunchKitFeedbackRequest,
  LaunchKitManualEditsRequest,
  LaunchKitListResponse,
  LaunchKitStatusResponse,
  ProductDetail,
  ProductHistory,
  ProviderBinding,
  ProviderBindingUpdateRequest,
  ProviderConfigResponse,
  ProviderProfile,
  ProviderProfileCreateRequest,
  ProviderProfileUpdateRequest,
  ProductWorkflow,
  ProductWorkflowStatus,
  ProductWritebackResponse,
  ProductListResponse,
  RuntimeConfig,
  SettingsLockState,
  SettingsExportPayload,
  SettingsImportCommitResponse,
  SettingsImportPreviewResponse,
  SessionState,
  UpdateUserTemplateGroupInput,
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

async function requestText(path: string, init?: RequestInit): Promise<string> {
  const response = await fetch(toApiUrl(path), {
    credentials: "include",
    headers: {
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

  return response.text();
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
  listLaunchKits(input?: { page?: number; page_size?: number }): Promise<LaunchKitListResponse> {
    const page = input?.page ?? 1;
    const pageSize = input?.page_size ?? 20;
    return request(`/api/launch-kits?page=${encodeURIComponent(page)}&page_size=${encodeURIComponent(pageSize)}`);
  },
  createLaunchKit(payload: LaunchKitCreateRequest): Promise<LaunchKitDetail> {
    return request("/api/launch-kits", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  },
  getLaunchKit(launchKitId: string): Promise<LaunchKitDetail> {
    return request(`/api/launch-kits/${launchKitId}`);
  },
  getLaunchKitStatus(launchKitId: string): Promise<LaunchKitStatusResponse> {
    return request(`/api/launch-kits/${launchKitId}/status`);
  },
  generateLaunchKit(launchKitId: string): Promise<LaunchKitDetail> {
    return request(`/api/launch-kits/${launchKitId}/generate`, { method: "POST" });
  },
  exportLaunchKitMarkdown(launchKitId: string): Promise<string> {
    return requestText(`/api/launch-kits/${launchKitId}/exports/markdown`);
  },
  saveLaunchKitManualEdits(launchKitId: string, payload: LaunchKitManualEditsRequest): Promise<LaunchKitDetail> {
    return request(`/api/launch-kits/${launchKitId}/manual-edits`, {
      method: "PATCH",
      body: JSON.stringify(payload),
    });
  },
  saveLaunchKitFeedback(launchKitId: string, payload: LaunchKitFeedbackRequest): Promise<LaunchKitDetail> {
    return request(`/api/launch-kits/${launchKitId}/feedback`, {
      method: "POST",
      body: JSON.stringify(payload),
    });
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
  getProviderConfig(): Promise<ProviderConfigResponse> {
    return request("/api/settings/provider-config");
  },
  createProviderProfile(payload: ProviderProfileCreateRequest): Promise<ProviderProfile> {
    return request("/api/settings/provider-profiles", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  },
  updateProviderProfile(profileId: string, payload: ProviderProfileUpdateRequest): Promise<ProviderProfile> {
    return request(`/api/settings/provider-profiles/${profileId}`, {
      method: "PATCH",
      body: JSON.stringify(payload),
    });
  },
  archiveProviderProfile(profileId: string): Promise<ProviderProfile> {
    return request(`/api/settings/provider-profiles/${profileId}`, { method: "DELETE" });
  },
  updateProviderBinding(purpose: "text" | "image", payload: ProviderBindingUpdateRequest): Promise<ProviderBinding> {
    return request(`/api/settings/provider-bindings/${purpose}`, {
      method: "PATCH",
      body: JSON.stringify(payload),
    });
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
  exportSettings(): Promise<SettingsExportPayload> {
    return request("/api/settings/export");
  },
  previewSettingsImport(payload: SettingsExportPayload): Promise<SettingsImportPreviewResponse> {
    return request("/api/settings/import/preview", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  },
  importSettings(payload: SettingsExportPayload): Promise<SettingsImportCommitResponse> {
    return request("/api/settings/import", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  },
  async createProduct(input: CreateProductInput): Promise<ProductDetail> {
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
    if (input.canvas_template_key !== undefined) {
      formData.set("canvas_template_key", input.canvas_template_key);
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
  updateCopySet(copySetId: string, payload: CopySetUpdateRequest): Promise<CopySet> {
    return request(`/api/copy-sets/${copySetId}`, {
      method: "PATCH",
      body: JSON.stringify(payload),
    });
  },
  confirmCopySet(copySetId: string): Promise<CopySet> {
    return request(`/api/copy-sets/${copySetId}/confirm`, { method: "POST" });
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
  getImageSessionStatus(sessionId: string): Promise<ImageSessionStatus> {
    return request(`/api/image-sessions/${sessionId}/status`);
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
  retryImageSessionGenerationTask(sessionId: string, taskId: string): Promise<ImageSessionDetail> {
    return request(`/api/image-sessions/${sessionId}/generation-tasks/${taskId}/retry`, { method: "POST" });
  },
  cancelImageSessionGenerationTask(sessionId: string, taskId: string): Promise<ImageSessionDetail> {
    return request(`/api/image-sessions/${sessionId}/generation-tasks/${taskId}/cancel`, { method: "POST" });
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
  getProductWorkflowStatus(productId: string): Promise<ProductWorkflowStatus> {
    return request(`/api/products/${productId}/workflow/status`);
  },
  listCanvasTemplates(): Promise<CanvasTemplateListResponse> {
    return request("/api/workflow/canvas-templates");
  },
  applyWorkflowTemplateGroup(productId: string, input: ApplyWorkflowTemplateGroupInput): Promise<ProductWorkflow> {
    return request(`/api/products/${productId}/workflow/template-groups`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  duplicateWorkflowNodeGroup(
    productId: string,
    input: DuplicateWorkflowNodeGroupInput,
  ): Promise<ProductWorkflow> {
    return request(`/api/products/${productId}/workflow/node-groups/duplicate`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  createUserTemplateGroup(productId: string, input: CreateUserTemplateGroupInput): Promise<CanvasTemplateSummary> {
    return request(`/api/products/${productId}/workflow/user-template-groups`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  updateUserTemplateGroup(templateId: string, input: UpdateUserTemplateGroupInput): Promise<CanvasTemplateSummary> {
    return request(`/api/workflow/user-template-groups/${templateId}`, {
      method: "PATCH",
      body: JSON.stringify(input),
    });
  },
  archiveUserTemplateGroup(templateId: string): Promise<void> {
    return request(`/api/workflow/user-template-groups/${templateId}`, {
      method: "DELETE",
    });
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
  cancelProductWorkflowRun(productId: string, runId: string): Promise<ProductWorkflow> {
    return request(`/api/products/${productId}/workflow/runs/${runId}/cancel`, { method: "POST" });
  },
  retryProductWorkflowRun(productId: string, runId: string): Promise<ProductWorkflow> {
    return request(`/api/products/${productId}/workflow/runs/${runId}/retry`, { method: "POST" });
  },
};
