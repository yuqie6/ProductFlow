import type {
  AgentProductWorkspaceCreateResponse,
  AgentProductWorkspaceOptions,
  AgentProductWorkspaceSnapshot,
  AgentSession,
  AgentSessionListResponse,
  AgentQuestionAnswer,
  AgentTurn,
  AgentTurnPage,
  AgentWorkbenchBootstrap,
  ActiveProductWorkflowV2,
  AppendWorkflowDraftRevisionInput,
  ConfigResponse,
  ConfigUpdateRequest,
  CreateReferenceWorkflowNodeV2Input,
  CreateWorkflowEdgeV2Input,
  GalleryEntry,
  GalleryEntryListResponse,
  GalleryAsset,
  GalleryAssetPage,
  GalleryAssetSort,
  GalleryBootstrap,
  GalleryDeleteFolderResult,
  GalleryDirectoryKind,
  GalleryFolderMutation,
  GenerationQueueOverview,
  CreateAgentProductWorkspaceInput,
  CreateAgentProductDraftWorkspaceInput,
  CreateWorkflowDraftInput,
  DeliveryRenditionJob,
  DeliveryRenditionJobListResponse,
  ImageSessionDetail,
  ImageSessionListResponse,
  ImageSessionStatus,
  ImageToolOptions,
  FinalizeAgentProductWorkspaceIntakeInput,
  LegacyArchiveAgentRebuildResult,
  LegacyArchiveDetail,
  LegacyArchiveKind,
  LegacyArchivePage,
  ProductListSort,
  ProviderBinding,
  ProviderBindingUpdateRequest,
  ProviderConfigResponse,
  ProviderProfile,
  ProviderProfileCreateRequest,
  ProviderProfileUpdateRequest,
  MaterializeWorkflowDraftInput,
  ProductListResponse,
  ProductImageAsset,
  ProductImageAssetListResponse,
  RuntimeConfig,
  SettingsLockState,
  SettingsExportPayload,
  SettingsImportCommitResponse,
  SettingsImportPreviewResponse,
  SessionState,
  SubmitAgentTurnInput,
  SubmitAgentTurnResponse,
  SubmitWorkflowNodeRunV2Result,
  SubmitWorkflowRunV2Result,
  WorkflowDraft,
  WorkflowDeliverySpec,
  WorkflowCanvasMutationResult,
  WorkflowMaterializationResult,
  WorkflowReferenceBindingResult,
  WorkflowNodeRunV2,
  WorkflowNodeRunListV2Response,
  WorkflowRunDetailV2Response,
  WorkflowRunListV2Response,
  WorkflowNodeDetailV2,
  UpdateWorkflowNodeV2Input,
  WorkflowRecipe,
  WorkflowRecipeApplicationResult,
  WorkflowRecipeSourceInput,
  WorkflowRecipeSummary,
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

function agentConversationPath(productId: string, conversationId: string): string {
  return `/api/v2/products/${encodeURIComponent(productId)}/agent-conversations/${encodeURIComponent(conversationId)}`;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(toApiUrl(path), {
    ...init,
    credentials: "include",
    headers: {
      ...(init?.body instanceof FormData ? {} : { "Content-Type": "application/json" }),
      ...init?.headers,
    },
  });

  if (!response.ok) {
    throw await responseApiError(response);
  }

  if (response.status === 204) {
    return undefined as T;
  }
  return (await response.json()) as T;
}

async function responseApiError(response: Response): Promise<ApiError> {
  let detail = "请求失败";
  try {
    const payload = (await response.json()) as { detail?: string };
    detail = payload.detail ?? detail;
  } catch {
    detail = response.statusText || detail;
  }
  return new ApiError(response.status, detail);
}

async function streamText(
  path: string,
  input: {
    signal?: AbortSignal;
    headers?: Record<string, string>;
    onChunk: (chunk: string) => void;
  },
): Promise<void> {
  const response = await fetch(toApiUrl(path), {
    credentials: "include",
    headers: { Accept: "text/event-stream", ...input.headers },
    signal: input.signal,
  });
  if (!response.ok) {
    throw await responseApiError(response);
  }
  if (!response.body) {
    const body = await response.text();
    if (body) input.onChunk(body);
    return;
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      const chunk = decoder.decode(value, { stream: true });
      if (chunk) input.onChunk(chunk);
    }
    const remainder = decoder.decode();
    if (remainder) input.onChunk(remainder);
  } catch (error) {
    try {
      await reader.cancel(error);
    } catch {
      // Preserve the original stream or parser failure.
    }
    throw error;
  } finally {
    reader.releaseLock();
  }
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
  listProducts(input?: {
    page?: number;
    page_size?: number;
    q?: string;
    sort?: ProductListSort;
  }): Promise<ProductListResponse> {
    const params = new URLSearchParams({
      page: String(input?.page ?? 1),
      page_size: String(input?.page_size ?? 20),
    });
    const query = input?.q?.trim();
    if (query) {
      params.set("q", query);
    }
    if (input?.sort && input.sort !== "updated_desc") {
      params.set("sort", input.sort);
    }
    return request(`/api/v2/products?${params.toString()}`);
  },
  deleteProduct(productId: string): Promise<void> {
    return request(`/api/v2/products/${productId}`, { method: "DELETE" });
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
  updateProviderBinding(purpose: ProviderBinding["purpose"], payload: ProviderBindingUpdateRequest): Promise<ProviderBinding> {
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
  getAgentProductWorkspaceOptions(): Promise<AgentProductWorkspaceOptions> {
    return request("/api/v2/agent-product-workspaces/options");
  },
  async createAgentProductWorkspace(
    input: CreateAgentProductWorkspaceInput,
  ): Promise<AgentProductWorkspaceCreateResponse> {
    const formData = new FormData();
    formData.set("name", input.name);
    formData.set("selection", JSON.stringify(input.selection));
    input.images.forEach((image) => {
      formData.append("images", image);
    });
    return request("/api/v2/agent-product-workspaces", {
      method: "POST",
      headers: { "Idempotency-Key": input.idempotency_key },
      body: formData,
    });
  },
  createAgentProductDraftWorkspace(
    input: CreateAgentProductDraftWorkspaceInput,
  ): Promise<AgentProductWorkspaceSnapshot> {
    return request("/api/v2/agent-product-workspaces/drafts", {
      method: "POST",
      headers: { "Idempotency-Key": input.idempotency_key },
      body: JSON.stringify({ name: input.name }),
    });
  },
  getAgentProductWorkspace(conversationId: string): Promise<AgentProductWorkspaceSnapshot> {
    return request(`/api/v2/agent-product-workspaces/${encodeURIComponent(conversationId)}`);
  },
  async finalizeAgentProductWorkspaceIntake(
    input: FinalizeAgentProductWorkspaceIntakeInput,
  ): Promise<AgentProductWorkspaceSnapshot> {
    const formData = new FormData();
    formData.set("selection", JSON.stringify(input.selection));
    input.images.forEach((image) => {
      formData.append("images", image);
    });
    return request(
      `/api/v2/agent-product-workspaces/${encodeURIComponent(input.conversation_id)}/intake`,
      {
        method: "POST",
        headers: { "Idempotency-Key": input.idempotency_key },
        body: formData,
      },
    );
  },
  getAgentWorkbench(productId: string, agentSessionId?: string | null): Promise<AgentWorkbenchBootstrap> {
    const params = new URLSearchParams();
    if (agentSessionId) {
      params.set("agent_session_id", agentSessionId);
    }
    const query = params.size ? `?${params}` : "";
    return request(`/api/v2/products/${encodeURIComponent(productId)}/agent-workbench${query}`);
  },
  listAgentSessions(includeArchived = false): Promise<AgentSessionListResponse> {
    const params = new URLSearchParams({ include_archived: String(includeArchived) });
    return request(`/api/v2/agent-sessions?${params}`);
  },
  createAgentSession(input: { title: string }): Promise<AgentSession> {
    return request("/api/v2/agent-sessions", {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  renameAgentSession(sessionId: string, title: string): Promise<AgentSession> {
    return request(`/api/v2/agent-sessions/${encodeURIComponent(sessionId)}`, {
      method: "PATCH",
      body: JSON.stringify({ title }),
    });
  },
  archiveAgentSession(sessionId: string): Promise<AgentSession> {
    return request(`/api/v2/agent-sessions/${encodeURIComponent(sessionId)}/archive`, {
      method: "POST",
    });
  },
  listAgentTurns(
    productId: string,
    conversationId: string,
    input?: { after?: string | null; limit?: number },
  ): Promise<AgentTurnPage> {
    const params = new URLSearchParams({ limit: String(input?.limit ?? 20) });
    if (input?.after) {
      params.set("after", input.after);
    }
    return request(`${agentConversationPath(productId, conversationId)}/turns?${params}`);
  },
  submitAgentTurn(
    productId: string,
    conversationId: string,
    input: SubmitAgentTurnInput,
  ): Promise<SubmitAgentTurnResponse> {
    return request(`${agentConversationPath(productId, conversationId)}/turns`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  getAgentTurn(productId: string, conversationId: string, projectionId: string): Promise<AgentTurn> {
    return request(
      `${agentConversationPath(productId, conversationId)}/turns/${encodeURIComponent(projectionId)}`,
    );
  },
  cancelAgentTurn(productId: string, conversationId: string, projectionId: string): Promise<AgentTurn> {
    return request(
      `${agentConversationPath(productId, conversationId)}/turns/${encodeURIComponent(projectionId)}/cancel`,
      { method: "POST" },
    );
  },
  resumeAgentTurn(productId: string, conversationId: string, projectionId: string): Promise<AgentTurn> {
    return request(
      `${agentConversationPath(productId, conversationId)}/turns/${encodeURIComponent(projectionId)}/resume`,
      { method: "POST" },
    );
  },
  answerAgentQuestion(
    productId: string,
    conversationId: string,
    projectionId: string,
    questionId: string,
    answer: AgentQuestionAnswer,
  ): Promise<AgentTurn> {
    return request(
      `${agentConversationPath(productId, conversationId)}/turns/${encodeURIComponent(projectionId)}/questions/${encodeURIComponent(questionId)}/answer`,
      { method: "POST", body: JSON.stringify(answer) },
    );
  },
  getAgentTurnEventsUrl(
    productId: string,
    conversationId: string,
    projectionId: string,
    after = 0,
  ): string {
    const params = new URLSearchParams({ after: String(after) });
    return toApiUrl(
      `${agentConversationPath(productId, conversationId)}/turns/${encodeURIComponent(projectionId)}/events?${params}`,
    );
  },
  getProductImageAssetMediaUrl(
    assetId: string,
    variant?: "thumbnail" | "preview",
  ): string {
    const params = new URLSearchParams();
    if (variant) {
      params.set("variant", variant);
    }
    const query = params.size ? `?${params}` : "";
    return toApiUrl(
      `/api/v2/product-image-assets/${encodeURIComponent(assetId)}/download${query}`,
    );
  },
  listLegacyArchives(input?: {
    kind?: LegacyArchiveKind;
    product_id?: string;
    q?: string;
    after?: string | null;
    limit?: number;
  }): Promise<LegacyArchivePage> {
    const params = new URLSearchParams({ limit: String(input?.limit ?? 30) });
    if (input?.kind) {
      params.set("kind", input.kind);
    }
    if (input?.product_id) {
      params.set("product_id", input.product_id);
    }
    if (input?.q?.trim()) {
      params.set("q", input.q.trim());
    }
    if (input?.after) {
      params.set("after", input.after);
    }
    return request(`/api/v2/legacy-archives?${params.toString()}`);
  },
  getLegacyArchive(kind: LegacyArchiveKind, archiveId: string): Promise<LegacyArchiveDetail> {
    return request(
      `/api/v2/legacy-archives/${encodeURIComponent(kind)}/${encodeURIComponent(archiveId)}`,
    );
  },
  async downloadLegacyArchive(kind: LegacyArchiveKind, archiveId: string): Promise<Blob> {
    const path = `/api/v2/legacy-archives/${encodeURIComponent(kind)}/${encodeURIComponent(archiveId)}/export`;
    const response = await fetch(toApiUrl(path), { credentials: "include" });
    if (!response.ok) {
      throw await responseApiError(response);
    }
    return response.blob();
  },
  createLegacyArchiveAgentRebuild(
    kind: LegacyArchiveKind,
    archiveId: string,
    input: { target_product_id: string; idempotency_key: string },
  ): Promise<LegacyArchiveAgentRebuildResult> {
    return request(
      `/api/v2/legacy-archives/${encodeURIComponent(kind)}/${encodeURIComponent(archiveId)}/agent-rebuilds`,
      {
        method: "POST",
        body: JSON.stringify(input),
      },
    );
  },
  getProductImageLibrary(productId: string): Promise<GalleryBootstrap> {
    return request(`/api/v2/products/${productId}/image-library`);
  },
  listGalleryAssets(
    productId: string,
    input: {
      directory_kind: GalleryDirectoryKind;
      directory_key?: string | null;
      q?: string;
      sort?: GalleryAssetSort;
      after?: string | null;
      limit?: number;
    },
  ): Promise<GalleryAssetPage> {
    const params = new URLSearchParams({
      directory_kind: input.directory_kind,
      sort: input.sort ?? "created_desc",
      limit: String(input.limit ?? 50),
    });
    if (input.directory_key) {
      params.set("directory_key", input.directory_key);
    }
    if (input.q?.trim()) {
      params.set("q", input.q.trim());
    }
    if (input.after) {
      params.set("after", input.after);
    }
    return request(`/api/v2/products/${productId}/image-assets?${params.toString()}`);
  },
  getGalleryAsset(productId: string, assetId: string): Promise<GalleryAsset> {
    return request(`/api/v2/products/${productId}/image-assets/${assetId}`);
  },
  createGalleryFolder(productId: string, name: string): Promise<GalleryFolderMutation> {
    return request(`/api/v2/products/${productId}/image-folders`, {
      method: "POST",
      body: JSON.stringify({ name }),
    });
  },
  renameGalleryFolder(
    productId: string,
    folderId: string,
    input: { expected_name: string; name: string },
  ): Promise<GalleryFolderMutation> {
    return request(`/api/v2/products/${productId}/image-folders/${folderId}`, {
      method: "PATCH",
      body: JSON.stringify(input),
    });
  },
  deleteGalleryFolder(productId: string, folderId: string, expectedName: string): Promise<GalleryDeleteFolderResult> {
    const params = new URLSearchParams({ expected_name: expectedName });
    return request(`/api/v2/products/${productId}/image-folders/${folderId}?${params.toString()}`, {
      method: "DELETE",
    });
  },
  renameGalleryAsset(
    productId: string,
    assetId: string,
    input: { expected_display_name: string; display_name: string },
  ): Promise<GalleryAsset> {
    return request(`/api/v2/products/${productId}/image-assets/${assetId}`, {
      method: "PATCH",
      body: JSON.stringify(input),
    });
  },
  moveGalleryAssets(
    productId: string,
    input: {
      items: Array<{ asset_id: string; expected_folder_id: string | null }>;
      folder_id: string | null;
    },
  ): Promise<GalleryAssetPage> {
    return request(`/api/v2/products/${productId}/image-assets/move`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  async downloadGalleryArchive(productId: string, assetIds: string[]): Promise<Blob> {
    const response = await fetch(toApiUrl(`/api/v2/products/${productId}/image-assets/download-archive`), {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ asset_ids: assetIds }),
    });
    if (!response.ok) {
      let detail = response.statusText || "请求失败";
      try {
        const payload = (await response.json()) as { detail?: string };
        detail = payload.detail ?? detail;
      } catch {
        // Keep the status text when the server does not return JSON.
      }
      throw new ApiError(response.status, detail);
    }
    return response.blob();
  },
  async addCanonicalProductImages(productId: string, images: File[]): Promise<ProductImageAssetListResponse> {
    const formData = new FormData();
    images.forEach((image) => {
      formData.append("images", image);
    });
    return request(`/api/v2/products/${productId}/image-assets`, {
      method: "POST",
      body: formData,
    });
  },
  listImageSessions(): Promise<ImageSessionListResponse> {
    return request("/api/image-sessions");
  },
  createImageSession(input: { title?: string }): Promise<ImageSessionDetail> {
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
  attachImageSessionAssetToProductCanonical(
    sessionId: string,
    assetId: string,
    productId: string,
  ): Promise<ProductImageAsset> {
    return request(`/api/v2/image-sessions/${sessionId}/assets/${assetId}/attach-to-product`, {
      method: "POST",
      body: JSON.stringify({ product_id: productId }),
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
  getActiveProductWorkflowV2(productId: string): Promise<ActiveProductWorkflowV2> {
    return request(`/api/v2/products/${productId}/workflow`);
  },
  createWorkflowReferenceNodeV2(
    productId: string,
    workflowId: string,
    input: CreateReferenceWorkflowNodeV2Input,
  ): Promise<WorkflowCanvasMutationResult> {
    return request(`/api/v2/products/${productId}/workflows/${workflowId}/reference-nodes`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  duplicateWorkflowNodeV2(
    productId: string,
    workflowId: string,
    nodeId: string,
    expectedEditVersion: number,
  ): Promise<WorkflowCanvasMutationResult> {
    return request(
      `/api/v2/products/${productId}/workflows/${workflowId}/nodes/${encodeURIComponent(nodeId)}/duplicate`,
      {
        method: "POST",
        body: JSON.stringify({ expected_edit_version: expectedEditVersion }),
      },
    );
  },
  deleteWorkflowNodeV2(
    productId: string,
    workflowId: string,
    nodeId: string,
    expectedEditVersion: number,
  ): Promise<WorkflowCanvasMutationResult> {
    const params = new URLSearchParams({ expected_edit_version: String(expectedEditVersion) });
    return request(
      `/api/v2/products/${productId}/workflows/${workflowId}/nodes/${encodeURIComponent(nodeId)}?${params}`,
      { method: "DELETE" },
    );
  },
  createWorkflowEdgeV2(
    productId: string,
    workflowId: string,
    input: CreateWorkflowEdgeV2Input,
  ): Promise<WorkflowCanvasMutationResult> {
    return request(`/api/v2/products/${productId}/workflows/${workflowId}/edges`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  deleteWorkflowEdgeV2(
    productId: string,
    workflowId: string,
    edgeId: string,
    expectedEditVersion: number,
  ): Promise<WorkflowCanvasMutationResult> {
    const params = new URLSearchParams({ expected_edit_version: String(expectedEditVersion) });
    return request(
      `/api/v2/products/${productId}/workflows/${workflowId}/edges/${encodeURIComponent(edgeId)}?${params}`,
      { method: "DELETE" },
    );
  },
  createWorkflowFolder(
    productId: string,
    workflowId: string,
    input: { title: string; node_ids: string[]; expected_edit_version: number },
  ): Promise<WorkflowCanvasMutationResult> {
    return request(`/api/v2/products/${productId}/workflows/${workflowId}/folders`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  renameWorkflowFolder(
    productId: string,
    workflowId: string,
    folderId: string,
    input: { title: string; expected_edit_version: number },
  ): Promise<WorkflowCanvasMutationResult> {
    return request(`/api/v2/products/${productId}/workflows/${workflowId}/folders/${folderId}`, {
      method: "PATCH",
      body: JSON.stringify(input),
    });
  },
  setWorkflowFolderMembers(
    productId: string,
    workflowId: string,
    folderId: string,
    input: { node_ids: string[]; expected_edit_version: number },
  ): Promise<WorkflowCanvasMutationResult> {
    return request(`/api/v2/products/${productId}/workflows/${workflowId}/folders/${folderId}/members`, {
      method: "PUT",
      body: JSON.stringify(input),
    });
  },
  dissolveWorkflowFolder(
    productId: string,
    workflowId: string,
    folderId: string,
    expectedEditVersion: number,
  ): Promise<WorkflowCanvasMutationResult> {
    const params = new URLSearchParams({ expected_edit_version: String(expectedEditVersion) });
    return request(`/api/v2/products/${productId}/workflows/${workflowId}/folders/${folderId}?${params}`, {
      method: "DELETE",
    });
  },
  translateWorkflowFolder(
    productId: string,
    workflowId: string,
    folderId: string,
    input: { delta_x: number; delta_y: number; expected_edit_version: number },
  ): Promise<WorkflowCanvasMutationResult> {
    return request(`/api/v2/products/${productId}/workflows/${workflowId}/folders/${folderId}/translate`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  updateWorkflowNodeLayoutV2(
    productId: string,
    workflowId: string,
    input: {
      positions: Array<{ node_id: string; position_x: number; position_y: number }>;
      expected_edit_version: number;
    },
  ): Promise<WorkflowCanvasMutationResult> {
    return request(`/api/v2/products/${productId}/workflows/${workflowId}/layout`, {
      method: "PATCH",
      body: JSON.stringify(input),
    });
  },
  getWorkflowNodeDetailV2(
    productId: string,
    workflowId: string,
    nodeId: string,
  ): Promise<WorkflowNodeDetailV2> {
    return request(
      `/api/v2/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/nodes/${encodeURIComponent(nodeId)}`,
    );
  },
  updateWorkflowNodeV2(
    productId: string,
    workflowId: string,
    nodeId: string,
    input: UpdateWorkflowNodeV2Input,
  ): Promise<WorkflowCanvasMutationResult> {
    return request(
      `/api/v2/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/nodes/${encodeURIComponent(nodeId)}`,
      {
        method: "PATCH",
        body: JSON.stringify(input),
      },
    );
  },
  listWorkflowRecipes(includeArchived = false): Promise<WorkflowRecipeSummary[]> {
    const params = new URLSearchParams({ include_archived: String(includeArchived) });
    return request(`/api/v2/workflow-recipes?${params}`);
  },
  getWorkflowRecipe(recipeId: string): Promise<WorkflowRecipe> {
    return request(`/api/v2/workflow-recipes/${recipeId}`);
  },
  createWorkflowRecipe(
    productId: string,
    workflowId: string,
    input: WorkflowRecipeSourceInput,
  ): Promise<WorkflowRecipe> {
    return request(`/api/v2/products/${productId}/workflows/${workflowId}/recipes`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  appendWorkflowRecipeVersion(
    productId: string,
    workflowId: string,
    recipeId: string,
    input: WorkflowRecipeSourceInput & { expected_recipe_version: number },
  ): Promise<WorkflowRecipe> {
    return request(`/api/v2/products/${productId}/workflows/${workflowId}/recipes/${recipeId}/versions`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  archiveWorkflowRecipe(recipeId: string, expectedRecipeVersion: number): Promise<{ changed: boolean; recipe: WorkflowRecipe }> {
    const params = new URLSearchParams({ expected_recipe_version: String(expectedRecipeVersion) });
    return request(`/api/v2/workflow-recipes/${recipeId}?${params}`, { method: "DELETE" });
  },
  applyWorkflowRecipe(
    productId: string,
    recipeId: string,
    input: { expected_recipe_version: number; idempotency_key: string },
  ): Promise<WorkflowRecipeApplicationResult> {
    return request(`/api/v2/products/${productId}/workflow-recipes/${recipeId}/apply`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  bindWorkflowReferenceAsset(
    productId: string,
    workflowId: string,
    nodeId: string,
    input: {
      asset_id: string;
      expected_workflow_revision: number;
      expected_bound_asset_id: string | null;
    },
  ): Promise<WorkflowReferenceBindingResult> {
    return request(`/api/v2/products/${productId}/workflows/${workflowId}/reference-nodes/${nodeId}`, {
      method: "PATCH",
      body: JSON.stringify(input),
    });
  },
  createWorkflowDraft(productId: string, input: CreateWorkflowDraftInput): Promise<WorkflowDraft> {
    return request(`/api/v2/products/${productId}/workflow-drafts`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  getWorkflowDraft(productId: string, draftId: string): Promise<WorkflowDraft> {
    return request(`/api/v2/products/${productId}/workflow-drafts/${draftId}`);
  },
  appendWorkflowDraftRevision(
    productId: string,
    draftId: string,
    input: AppendWorkflowDraftRevisionInput,
  ): Promise<WorkflowDraft> {
    return request(`/api/v2/products/${productId}/workflow-drafts/${draftId}/revisions`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  confirmWorkflowDraft(productId: string, draftId: string, expectedDraftVersion: number): Promise<WorkflowDraft> {
    return request(`/api/v2/products/${productId}/workflow-drafts/${draftId}/confirm`, {
      method: "POST",
      body: JSON.stringify({ expected_draft_version: expectedDraftVersion }),
    });
  },
  materializeWorkflowDraft(
    productId: string,
    draftId: string,
    input: MaterializeWorkflowDraftInput,
  ): Promise<WorkflowMaterializationResult> {
    return request(`/api/v2/products/${productId}/workflow-drafts/${draftId}/materialize`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  runWorkflowNodeV2(nodeId: string): Promise<SubmitWorkflowNodeRunV2Result> {
    return request(`/api/v2/workflow-nodes/${encodeURIComponent(nodeId)}/run`, {
      method: "POST",
    });
  },
  getWorkflowNodeRunV2(nodeRunId: string): Promise<WorkflowNodeRunV2> {
    return request(`/api/v2/workflow-node-runs/${encodeURIComponent(nodeRunId)}`);
  },
  listWorkflowNodeRunsV2(nodeId: string, limit = 20): Promise<WorkflowNodeRunListV2Response> {
    const params = new URLSearchParams({ limit: String(limit) });
    return request(`/api/v2/workflow-nodes/${encodeURIComponent(nodeId)}/runs?${params}`);
  },
  cancelWorkflowNodeRunV2(nodeRunId: string): Promise<WorkflowNodeRunV2> {
    return request(`/api/v2/workflow-node-runs/${encodeURIComponent(nodeRunId)}/cancel`, {
      method: "POST",
    });
  },
  runWorkflowV2(productId: string, workflowId: string): Promise<SubmitWorkflowRunV2Result> {
    return request(
      `/api/v2/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/runs`,
      { method: "POST" },
    );
  },
  getWorkflowRunV2(
    productId: string,
    workflowId: string,
    runId: string,
  ): Promise<WorkflowRunDetailV2Response> {
    return request(
      `/api/v2/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/runs/${encodeURIComponent(runId)}`,
    );
  },
  listWorkflowRunsV2(
    productId: string,
    workflowId: string,
    limit = 20,
  ): Promise<WorkflowRunListV2Response> {
    const params = new URLSearchParams({ limit: String(limit) });
    return request(
      `/api/v2/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/runs?${params}`,
    );
  },
  cancelWorkflowRunV2(
    productId: string,
    workflowId: string,
    runId: string,
  ): Promise<WorkflowRunDetailV2Response> {
    return request(
      `/api/v2/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/runs/${encodeURIComponent(runId)}/cancel`,
      { method: "POST" },
    );
  },
  retryWorkflowRunV2(
    productId: string,
    workflowId: string,
    runId: string,
  ): Promise<SubmitWorkflowRunV2Result> {
    return request(
      `/api/v2/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/runs/${encodeURIComponent(runId)}/retry`,
      { method: "POST" },
    );
  },
  createDeliveryRendition(
    sourceAssetId: string,
    deliverySpec: WorkflowDeliverySpec,
  ): Promise<DeliveryRenditionJob> {
    return request(`/api/v2/product-image-assets/${encodeURIComponent(sourceAssetId)}/renditions`, {
      method: "POST",
      body: JSON.stringify(deliverySpec),
    });
  },
  listDeliveryRenditions(sourceAssetId: string): Promise<DeliveryRenditionJobListResponse> {
    return request(`/api/v2/product-image-assets/${encodeURIComponent(sourceAssetId)}/renditions`);
  },
  getDeliveryRenditionJob(jobId: string): Promise<DeliveryRenditionJob> {
    return request(`/api/v2/delivery-rendition-jobs/${encodeURIComponent(jobId)}`);
  },
  retryDeliveryRenditionJob(jobId: string): Promise<DeliveryRenditionJob> {
    return request(`/api/v2/delivery-rendition-jobs/${encodeURIComponent(jobId)}/retry`, {
      method: "POST",
    });
  },
  workflowRevealEventsUrl(materializationId: string, after?: number): string {
    const path = `/api/v2/workflow-materializations/${encodeURIComponent(materializationId)}/reveal-events`;
    return toApiUrl(after && after > 0 ? `${path}?after=${after}` : path);
  },
  streamWorkflowRevealEvents(
    materializationId: string,
    input: {
      after?: number;
      signal?: AbortSignal;
      onChunk: (chunk: string) => void;
    },
  ): Promise<void> {
    const after = input.after ?? 0;
    return streamText(api.workflowRevealEventsUrl(materializationId, after), {
      signal: input.signal,
      headers: after > 0 ? { "Last-Event-ID": String(after) } : undefined,
      onChunk: input.onChunk,
    });
  },
};
