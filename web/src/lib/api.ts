import type {
  GraphTextSettings,
  AgentProductWorkspaceCreateResponse,
  AgentProductWorkspaceOptions,
  AgentProductWorkspaceSnapshot,
  AgentSession,
  AgentSessionListResponse,
  AgentTask,
  AgentTaskListResponse,
  AgentQuestionAnswer,
  AgentQuestionAnswerResponse,
  AgentTurn,
  AgentTurnPage,
  AgentWorkflowRunRequest,
  AgentWorkbenchBootstrap,
  ConfigResponse,
  ConfigUpdateRequest,
  GalleryAsset,
  GalleryAssetPage,
  GalleryAssetSort,
  GalleryBootstrap,
  GalleryDeleteFolderResult,
  GalleryDirectoryKind,
  GalleryFolderMutation,
  MediaLibraryAsset,
  MediaLibraryAssetPage,
  MediaLibraryBootstrap,
  MediaLibraryFolder,
  MediaLibrarySourceType,
  MediaLibraryTag,
  WorkflowMediaLibraryAssetListResponse,
  GenerationQueueOverview,
  CreateAgentProductWorkspaceInput,
  CreateAgentProductDraftWorkspaceInput,
  DeliveryPresetCatalog,
  DeliveryRenditionJob,
  DeliveryRenditionJobListResponse,
  DeliveryAdoptionListResponse,
  DeliveryAdoptionVersion,
  DeliveryAdoptionPreview,
  ProductVisualSelection,
  VisualInheritanceView,
  VisualSystemSummary,
  VisualSystemVersion,
  ImageSessionDetail,
  ImageSessionHistoryPage,
  ImageSessionListResponse,
  ImageSessionStatus,
  ImageToolOptions,
  FinalizeAgentProductWorkspaceIntakeInput,
  LibraryOrganizationDraft,
  LocalImageEditCapability,
  LocalImageEditCreateInput,
  LocalImageEditUpdateInput,
  LocalImageEditTask,
  LocalImageEditTaskListResponse,
  ProductListSort,
  ProviderBinding,
  ProviderBindingUpdateRequest,
  ProviderConfigResponse,
  ProviderProfile,
  ProviderProfileCreateRequest,
  ProviderProfileUpdateRequest,
  DirectCreateProductResponse,
  RecipeCreateProductResponse,
  GeneratedSourceNote,
  GraphChangeSet,
  GraphDocumentCandidate,
  WorkflowGenerationSpec,
  GraphNodeCatalog,
  GraphProjection,
  GraphRun,
  GraphRunListResponse,
  GraphRunPreviewResponse,
  GraphRunSubmitInput,
  CanonicalProductDetail,
  ProductFactsResponse,
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
  UpdateProductFactsInput,
  WorkflowDeliverySpec,
  WorkflowRecipe,
  WorkflowRecipeApplicationResult,
  WorkflowRecipePreview,
  WorkflowRecipeSourceInput,
  WorkflowRecipeSummary,
} from "./types";
import { parseDeliveryPresetCatalog } from "./deliveryPresets";
import {
  parseLocalImageEditCapability,
  parseLocalImageEditTask,
  parseLocalImageEditTaskList,
} from "./localImageEdits";

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

function globalAgentConversationPath(conversationId: string): string {
  return `/api/v2/agent-conversations/${encodeURIComponent(conversationId)}`;
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

async function requestBlob(path: string, init?: RequestInit): Promise<Blob> {
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
  return response.blob();
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

export const api = {
  toApiUrl,
  getSessionState(): Promise<SessionState> {
    return request<SessionState>("/api/auth/session");
  },
  createSession(input: { email: string; password: string }): Promise<{ ok: boolean }> {
    return request("/api/auth/session", {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  bootstrapSession(input: {
    admin_key: string;
    email: string;
    password: string;
    display_name?: string;
    merchant_name: string;
  }): Promise<{ ok: boolean; user_id: string; merchant_id: string }> {
    return request("/api/auth/bootstrap", {
      method: "POST",
      body: JSON.stringify(input),
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
  getProduct(productId: string): Promise<CanonicalProductDetail> {
    return request(`/api/v2/products/${encodeURIComponent(productId)}`);
  },
  getProductFacts(productId: string): Promise<ProductFactsResponse> {
    return request(`/api/v3/products/${encodeURIComponent(productId)}/facts`);
  },
  updateProductFacts(productId: string, input: UpdateProductFactsInput): Promise<ProductFactsResponse> {
    return request(`/api/v3/products/${encodeURIComponent(productId)}/facts`, {
      method: "PUT",
      body: JSON.stringify(input),
    });
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
  async getImageGenerationOptions(): Promise<Record<string, string[]>> {
    const value = await request<unknown>("/api/v3/image-generation-options");
    if (typeof value !== "object" || value === null || Array.isArray(value)) throw new ApiError(502, "图片生成选项无效");
    const options: Record<string, string[]> = {};
    for (const [key, choices] of Object.entries(value)) {
      if (!Array.isArray(choices) || !choices.every((choice): choice is string => typeof choice === "string")) throw new ApiError(502, "图片生成选项无效");
      options[key] = choices;
    }
    return options;
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
    if (input.agent_session_id) {
      formData.set("agent_session_id", input.agent_session_id);
    }
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
      body: JSON.stringify({
        name: input.name,
        ...(input.agent_session_id ? { agent_session_id: input.agent_session_id } : {}),
      }),
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
    if (input.task_id) {
      formData.set("task_id", input.task_id);
    }
    if (input.source_note?.trim()) {
      formData.set("source_note", input.source_note.trim());
    }
    return request(
      `/api/v2/agent-product-workspaces/${encodeURIComponent(input.conversation_id)}/intake`,
      {
        method: "POST",
        headers: { "Idempotency-Key": input.idempotency_key },
        body: formData,
      },
    );
  },
  getAgentWorkbench(
    productId: string,
    agentSessionId?: string | null,
    agentTaskId?: string | null,
  ): Promise<AgentWorkbenchBootstrap> {
    const params = new URLSearchParams();
    if (agentSessionId) {
      params.set("agent_session_id", agentSessionId);
    }
    if (agentTaskId) {
      params.set("agent_task_id", agentTaskId);
    }
    const query = params.size ? `?${params}` : "";
    return request(`/api/v2/products/${encodeURIComponent(productId)}/agent-workbench${query}`);
  },
  ensureAgentWorkbench(
    productId: string,
    agentSessionId?: string | null,
    options?: { newSession?: boolean },
  ): Promise<AgentWorkbenchBootstrap> {
    const params = new URLSearchParams();
    if (agentSessionId) {
      params.set("agent_session_id", agentSessionId);
    }
    if (options?.newSession) {
      params.set("new_session", "true");
    }
    const query = params.size ? `?${params}` : "";
    const idempotencyKey = options?.newSession
      ? `agent-workbench:${productId}:${globalThis.crypto.randomUUID()}`
      : `agent-workbench:${productId}`;
    return request(`/api/v2/products/${encodeURIComponent(productId)}/agent-workbench${query}`, {
      method: "POST",
      headers: { "Idempotency-Key": idempotencyKey },
    });
  },
  listAgentSessions(
    includeArchived = false,
    productId?: string | null,
    options?: { after?: string | null; limit?: number },
  ): Promise<AgentSessionListResponse> {
    const params = new URLSearchParams({
      include_archived: String(includeArchived),
      limit: String(options?.limit ?? 20),
    });
    if (productId) {
      params.set("product_id", productId);
    }
    if (options?.after) {
      params.set("after", options.after);
    }
    return request(`/api/v2/agent-sessions?${params}`);
  },
  createAgentSession(): Promise<AgentSession> {
    return request("/api/v2/agent-sessions", {
      method: "POST",
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
  agentControlEventsUrl(): string {
    return toApiUrl("/api/v2/agent-control/events");
  },
  listAgentTasks(input: {
    sessionId?: string | null;
    includeTerminal?: boolean;
    limit?: number;
    after?: string | null;
  } = {}): Promise<AgentTaskListResponse> {
    const params = new URLSearchParams({
      include_terminal: String(input.includeTerminal ?? true),
      limit: String(input.limit ?? 50),
    });
    if (input.sessionId) {
      params.set("session_id", input.sessionId);
    }
    if (input.after) {
      params.set("after", input.after);
    }
    return request(`/api/v2/agent-tasks?${params}`);
  },
  createAgentTask(input: {
    session_id: string;
    title: string;
    goal: string;
    conversation_id?: string | null;
  }): Promise<AgentTask> {
    return request("/api/v2/agent-tasks", {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  renameAgentTask(taskId: string, title: string): Promise<AgentTask> {
    return request(`/api/v2/agent-tasks/${encodeURIComponent(taskId)}`, {
      method: "PATCH",
      body: JSON.stringify({ title }),
    });
  },
  cancelAgentTask(taskId: string): Promise<AgentTask> {
    return request(`/api/v2/agent-tasks/${encodeURIComponent(taskId)}/cancel`, {
      method: "POST",
    });
  },
  pauseAgentTask(taskId: string): Promise<AgentTask> {
    return request(`/api/v2/agent-tasks/${encodeURIComponent(taskId)}/pause`, {
      method: "POST",
    });
  },
  resumeAgentTask(taskId: string): Promise<AgentTask> {
    return request(`/api/v2/agent-tasks/${encodeURIComponent(taskId)}/resume`, {
      method: "POST",
    });
  },
  getAgentTask(taskId: string): Promise<AgentTask> {
    return request(`/api/v2/agent-tasks/${encodeURIComponent(taskId)}`);
  },
  completeAgentTask(taskId: string): Promise<AgentTask> {
    return request(`/api/v2/agent-tasks/${encodeURIComponent(taskId)}/complete`, {
      method: "POST",
    });
  },
  listAgentTurns(
    productId: string,
    conversationId: string,
    input?: { after?: string | null; limit?: number; taskId?: string | null },
  ): Promise<AgentTurnPage> {
    const params = new URLSearchParams({ limit: String(input?.limit ?? 20) });
    if (input?.after) {
      params.set("after", input.after);
    }
    if (input?.taskId) {
      params.set("task_id", input.taskId);
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
  getAgentWorkflowRunRequest(
    productId: string,
    conversationId: string,
  ): Promise<AgentWorkflowRunRequest | null> {
    return request(
      `${agentConversationPath(productId, conversationId)}/workflow-run-request`,
    );
  },
  confirmAgentWorkflowRunRequest(
    productId: string,
    conversationId: string,
    requestId: string,
  ): Promise<AgentWorkflowRunRequest> {
    return request(
      `${agentConversationPath(productId, conversationId)}/workflow-run-request/${encodeURIComponent(requestId)}/confirm`,
      { method: "POST" },
    );
  },
  cancelAgentWorkflowRunRequest(
    productId: string,
    conversationId: string,
    requestId: string,
  ): Promise<AgentWorkflowRunRequest> {
    return request(
      `${agentConversationPath(productId, conversationId)}/workflow-run-request/${encodeURIComponent(requestId)}/cancel`,
      { method: "POST" },
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
  ): Promise<AgentQuestionAnswerResponse> {
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
  listGlobalAgentTurns(
    conversationId: string,
    input?: { after?: string | null; limit?: number; taskId?: string | null },
  ): Promise<AgentTurnPage> {
    const params = new URLSearchParams({ limit: String(input?.limit ?? 20) });
    if (input?.after) {
      params.set("after", input.after);
    }
    if (input?.taskId) {
      params.set("task_id", input.taskId);
    }
    return request(`${globalAgentConversationPath(conversationId)}/turns?${params}`);
  },
  submitGlobalAgentTurn(
    conversationId: string,
    input: SubmitAgentTurnInput,
  ): Promise<SubmitAgentTurnResponse> {
    return request(`${globalAgentConversationPath(conversationId)}/turns`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  getGlobalAgentTurn(conversationId: string, projectionId: string): Promise<AgentTurn> {
    return request(
      `${globalAgentConversationPath(conversationId)}/turns/${encodeURIComponent(projectionId)}`,
    );
  },
  getGlobalAgentTurnEventsUrl(conversationId: string, projectionId: string, after = 0): string {
    const params = new URLSearchParams({ after: String(after) });
    return toApiUrl(
      `${globalAgentConversationPath(conversationId)}/turns/${encodeURIComponent(projectionId)}/events?${params}`,
    );
  },
  getGlobalWorkflowRunRequest(
    conversationId: string,
    taskId?: string | null,
  ): Promise<AgentWorkflowRunRequest | null> {
    const params = new URLSearchParams();
    if (taskId) {
      params.set("task_id", taskId);
    }
    const query = params.toString();
    return request(
      `${globalAgentConversationPath(conversationId)}/workflow-run-request${query ? `?${query}` : ""}`,
    );
  },
  confirmGlobalWorkflowRunRequest(
    conversationId: string,
    requestId: string,
  ): Promise<AgentWorkflowRunRequest> {
    return request(
      `${globalAgentConversationPath(conversationId)}/workflow-run-request/${encodeURIComponent(requestId)}/confirm`,
      { method: "POST" },
    );
  },
  cancelGlobalWorkflowRunRequest(
    conversationId: string,
    requestId: string,
  ): Promise<AgentWorkflowRunRequest> {
    return request(
      `${globalAgentConversationPath(conversationId)}/workflow-run-request/${encodeURIComponent(requestId)}/cancel`,
      { method: "POST" },
    );
  },
  cancelGlobalAgentTurn(conversationId: string, projectionId: string): Promise<AgentTurn> {
    return request(
      `${globalAgentConversationPath(conversationId)}/turns/${encodeURIComponent(projectionId)}/cancel`,
      { method: "POST" },
    );
  },
  resumeGlobalAgentTurn(conversationId: string, projectionId: string): Promise<AgentTurn> {
    return request(
      `${globalAgentConversationPath(conversationId)}/turns/${encodeURIComponent(projectionId)}/resume`,
      { method: "POST" },
    );
  },
  answerGlobalAgentQuestion(
    conversationId: string,
    projectionId: string,
    questionId: string,
    answer: AgentQuestionAnswer,
  ): Promise<AgentQuestionAnswerResponse> {
    return request(
      `${globalAgentConversationPath(conversationId)}/turns/${encodeURIComponent(projectionId)}/questions/${encodeURIComponent(questionId)}/answer`,
      { method: "POST", body: JSON.stringify(answer) },
    );
  },
  getGlobalLibraryOrganizationDraft(conversationId: string): Promise<LibraryOrganizationDraft> {
    return request(
      `${globalAgentConversationPath(conversationId)}/library-organization-draft`,
    );
  },
  confirmGlobalLibraryOrganizationDraft(
    conversationId: string,
    expectedDraftVersion: number,
    idempotencyKey: string,
  ): Promise<LibraryOrganizationDraft> {
    return request(
      `${globalAgentConversationPath(conversationId)}/library-organization-draft/confirm`,
      {
        method: "POST",
        body: JSON.stringify({
          expected_draft_version: expectedDraftVersion,
          idempotency_key: idempotencyKey,
        }),
      },
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
  getMediaLibraryAssetMediaUrl(
    assetId: string,
    variant?: "thumbnail" | "preview",
  ): string {
    const params = new URLSearchParams();
    if (variant) {
      params.set("variant", variant);
    }
    const query = params.size ? `?${params}` : "";
    return toApiUrl(`/api/media-library/${encodeURIComponent(assetId)}/download${query}`);
  },
  getProductImageLibrary(productId: string): Promise<GalleryBootstrap> {
    return request(`/api/v2/products/${productId}/image-library`);
  },
  listGalleryAssets(
    productId: string,
    input: {
      node_id?: string;
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
    if (input.node_id) params.set("node_id", input.node_id);
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
        // 服务端没返回 JSON 时沿用 HTTP 状态文本
      }
      throw new ApiError(response.status, detail);
    }
    return response.blob();
  },
  downloadDeliveryExport(
    productId: string,
    renditionJobIds: string[],
    allowPartial = false,
  ): Promise<Blob> {
    return requestBlob(`/api/v3/products/${encodeURIComponent(productId)}/delivery-exports`, {
      method: "POST",
      body: JSON.stringify({ rendition_job_ids: renditionJobIds, allow_partial: allowPartial }),
    });
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
  listImageSessions(options: { after?: string; limit?: number } = {}): Promise<ImageSessionListResponse> {
    const params = new URLSearchParams();
    if (options.after) params.set("after", options.after);
    if (options.limit !== undefined) params.set("limit", String(options.limit));
    const query = params.toString();
    return request(`/api/image-sessions${query ? `?${query}` : ""}`);
  },
  createImageSession(input: { title?: string }): Promise<ImageSessionDetail> {
    return request("/api/image-sessions", {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  getImageSession(sessionId: string): Promise<ImageSessionDetail> {
    return request(`/api/image-sessions/${encodeURIComponent(sessionId)}`);
  },
  getImageSessionHistory(
    sessionId: string,
    options: { after?: string; limit?: number } = {},
  ): Promise<ImageSessionHistoryPage> {
    const params = new URLSearchParams();
    if (options.after) params.set("after", options.after);
    if (options.limit !== undefined) params.set("limit", String(options.limit));
    const query = params.toString();
    return request(`/api/image-sessions/${encodeURIComponent(sessionId)}/history${query ? `?${query}` : ""}`);
  },
  getImageSessionStatus(sessionId: string): Promise<ImageSessionStatus> {
    return request(`/api/image-sessions/${sessionId}/status`);
  },
  imageSessionEventsUrl(sessionId: string): string {
    return `/api/image-sessions/${encodeURIComponent(sessionId)}/events`;
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
  saveMediaLibraryAssetFromSession(imageSessionAssetId: string): Promise<MediaLibraryAsset> {
    return request("/api/media-library/from-session", {
      method: "POST",
      body: JSON.stringify({ image_session_asset_id: imageSessionAssetId }),
    });
  },
  collectMediaLibraryAssetsToProduct(
    productId: string,
    mediaLibraryAssetIds: string[],
    idempotencyKey: string,
  ): Promise<ProductImageAsset[]> {
    return request("/api/media-library/collect", {
      method: "POST",
      headers: { "Idempotency-Key": idempotencyKey },
      body: JSON.stringify({ product_id: productId, media_library_asset_ids: mediaLibraryAssetIds }),
    });
  },
  getMediaLibraryBootstrap(): Promise<MediaLibraryBootstrap> {
    return request("/api/media-library/bootstrap");
  },
  getMediaLibraryAsset(assetId: string): Promise<MediaLibraryAsset> {
    return request(`/api/media-library/${encodeURIComponent(assetId)}`);
  },
  listMediaLibraryAssets(input?: {
    limit?: number;
    cursor?: string | null;
    includeArchived?: boolean;
    q?: string;
    sourceType?: MediaLibrarySourceType | null;
    folderId?: string | null;
    tag?: string | null;
  }): Promise<MediaLibraryAssetPage> {
    const params = new URLSearchParams({
      limit: String(input?.limit ?? 48),
      include_archived: String(input?.includeArchived ?? false),
    });
    if (input?.cursor) params.set("cursor", input.cursor);
    if (input?.q?.trim()) params.set("q", input.q.trim());
    if (input?.sourceType) params.set("source_type", input.sourceType);
    if (input?.folderId) params.set("folder_id", input.folderId);
    if (input?.tag?.trim()) params.set("tag", input.tag.trim());
    return request(`/api/media-library?${params}`);
  },
  uploadMediaLibraryAsset(file: File, folderId?: string | null): Promise<MediaLibraryAsset> {
    const formData = new FormData();
    formData.append("files", file);
    if (folderId) {
      formData.append("folder_id", folderId);
    }
    return request<MediaLibraryAsset[]>("/api/media-library/upload", {
      method: "POST",
      body: formData,
    }).then((items) => items[0]);
  },
  uploadMediaLibraryAssets(files: File[], folderId?: string | null): Promise<MediaLibraryAsset[]> {
    const formData = new FormData();
    for (const file of files) {
      formData.append("files", file);
    }
    if (folderId) {
      formData.append("folder_id", folderId);
    }
    return request("/api/media-library/upload", {
      method: "POST",
      body: formData,
    });
  },
  createMediaLibraryFolder(name: string): Promise<MediaLibraryFolder> {
    return request("/api/media-library/folders", {
      method: "POST",
      body: JSON.stringify({ name }),
    });
  },
  renameMediaLibraryFolder(folderId: string, expectedName: string, name: string): Promise<MediaLibraryFolder> {
    return request(`/api/media-library/folders/${encodeURIComponent(folderId)}`, {
      method: "PATCH",
      body: JSON.stringify({ expected_name: expectedName, name }),
    });
  },
  deleteMediaLibraryFolder(folderId: string): Promise<{ folder_id: string; unorganized_count: number }> {
    return request(`/api/media-library/folders/${encodeURIComponent(folderId)}`, {
      method: "DELETE",
    });
  },
  createMediaLibraryTag(name: string): Promise<MediaLibraryTag> {
    return request("/api/media-library/tags", {
      method: "POST",
      body: JSON.stringify({ name }),
    });
  },
  renameMediaLibraryTag(tagId: string, expectedName: string, name: string): Promise<MediaLibraryTag> {
    return request(`/api/media-library/tags/${encodeURIComponent(tagId)}`, {
      method: "PATCH",
      body: JSON.stringify({ expected_name: expectedName, name }),
    });
  },
  deleteMediaLibraryTag(tagId: string): Promise<{ tag_id: string; removed_assignment_count: number }> {
    return request(`/api/media-library/tags/${encodeURIComponent(tagId)}`, {
      method: "DELETE",
    });
  },
  moveMediaLibraryAssets(input: {
    assetIds: string[];
    folderId: string | null;
    expectedRevisions: Record<string, number>;
  }): Promise<MediaLibraryAsset[]> {
    return request("/api/media-library/organize/move", {
      method: "POST",
      body: JSON.stringify({
        asset_ids: input.assetIds,
        folder_id: input.folderId,
        expected_revisions: input.expectedRevisions,
      }),
    });
  },
  setMediaLibraryAssetTags(input: {
    assetIds: string[];
    tagNames: string[];
    expectedRevisions: Record<string, number>;
  }): Promise<MediaLibraryAsset[]> {
    return request("/api/media-library/organize/tags", {
      method: "POST",
      body: JSON.stringify({
        asset_ids: input.assetIds,
        tag_names: input.tagNames,
        expected_revisions: input.expectedRevisions,
      }),
    });
  },
  archiveMediaLibraryAsset(assetId: string, expectedRevision: number): Promise<MediaLibraryAsset> {
    const params = new URLSearchParams({ expected_revision: String(expectedRevision) });
    return request(`/api/media-library/${encodeURIComponent(assetId)}/archive?${params}`, { method: "POST" });
  },
  restoreMediaLibraryAsset(assetId: string, expectedRevision: number): Promise<MediaLibraryAsset> {
    const params = new URLSearchParams({ expected_revision: String(expectedRevision) });
    return request(`/api/media-library/${encodeURIComponent(assetId)}/restore?${params}`, { method: "POST" });
  },
  listWorkflowMediaLibraryAssets(
    productId: string,
    workflowId: string,
  ): Promise<WorkflowMediaLibraryAssetListResponse> {
    const params = new URLSearchParams({ product_id: productId });
    return request(`/api/media-library/workflows/${encodeURIComponent(workflowId)}/media-library?${params}`);
  },
  syncWorkflowMediaLibraryAssets(
    productId: string,
    workflowId: string,
    mediaLibraryAssetIds: string[],
  ): Promise<WorkflowMediaLibraryAssetListResponse> {
    const params = new URLSearchParams({ product_id: productId });
    return request(`/api/media-library/workflows/${encodeURIComponent(workflowId)}/media-library/sync?${params}`, {
      method: "POST",
      body: JSON.stringify({ media_library_asset_ids: mediaLibraryAssetIds }),
    });
  },
  removeWorkflowMediaLibraryAsset(productId: string, workflowId: string, mediaLibraryAssetId: string): Promise<void> {
    const params = new URLSearchParams({ product_id: productId });
    return request(
      `/api/media-library/workflows/${encodeURIComponent(workflowId)}/media-library/${encodeURIComponent(mediaLibraryAssetId)}?${params}`,
      { method: "DELETE" },
    );
  },
  createProductDirect(input: {
    name: string;
    images: File[];
    imageTypes: Array<{ key: string; quantity: number; aspect_ratio?: string }>;
    category?: string;
    price?: string;
    sourceNote?: string;
    generationSpec?: WorkflowGenerationSpec;
    textSettings?: GraphTextSettings;
    deliveryPresetKey?: string;
  }): Promise<DirectCreateProductResponse> {
    const body = new FormData();
    body.append("name", input.name);
    body.append("image_types", JSON.stringify(input.imageTypes));
    if (input.category) body.append("category", input.category);
    if (input.price) body.append("price", input.price);
    if (input.sourceNote) body.append("source_note", input.sourceNote);
    if (input.generationSpec) body.append("generation_spec", JSON.stringify(input.generationSpec));
    if (input.textSettings) body.append("text_settings", JSON.stringify(input.textSettings));
    if (input.deliveryPresetKey) body.append("delivery_preset_key", input.deliveryPresetKey);
    for (const image of input.images) {
      body.append("images", image);
    }
    return request("/api/v3/products", { method: "POST", body });
  },
  previewRecipeCreation(recipeId: string, expectedRecipeVersion: number): Promise<WorkflowRecipePreview> {
    return request(`/api/v3/workflow-recipes/${encodeURIComponent(recipeId)}/creation-preview`, {
      method: "POST", body: JSON.stringify({ expected_recipe_version: expectedRecipeVersion }),
    });
  },
  createProductFromRecipe(input: {
    name: string;
    images: File[];
    sourceNote: string;
    recipeId: string;
    expectedRecipeVersion: number;
    previewDigest: string;
    idempotencyKey: string;
  }): Promise<RecipeCreateProductResponse> {
    const body = new FormData();
    body.append("name", input.name);
    body.append("source_note", input.sourceNote);
    body.append("recipe_id", input.recipeId);
    body.append("expected_recipe_version", String(input.expectedRecipeVersion));
    body.append("preview_digest", input.previewDigest);
    for (const file of input.images) body.append("images", file);
    return request("/api/v3/products/from-recipe", {
      method: "POST", body, headers: { "Idempotency-Key": input.idempotencyKey },
    });
  },
  generateProductSourceNote(input: {
    images: File[];
    productName?: string;
    currentNote?: string;
  }): Promise<GeneratedSourceNote> {
    const body = new FormData();
    if (input.productName) body.append("product_name", input.productName);
    if (input.currentNote) body.append("current_note", input.currentNote);
    for (const image of input.images) {
      body.append("images", image);
    }
    return request("/api/v2/product-source-notes/generate", { method: "POST", body });
  },
  getGraphNodeCatalog(): Promise<GraphNodeCatalog> {
    return request("/api/v3/node-catalog");
  },
  async getDeliveryPresets(): Promise<DeliveryPresetCatalog> {
    const parsed = parseDeliveryPresetCatalog(await request<unknown>("/api/v3/delivery-presets"));
    if (!parsed) {
      throw new ApiError(502, "交付预设目录响应无效");
    }
    return parsed;
  },
  getCurrentWorkflowGraph(productId: string): Promise<GraphProjection> {
    return request(`/api/v3/products/${encodeURIComponent(productId)}/workflows/current`);
  },
  getWorkflowGraph(productId: string, workflowId: string): Promise<GraphProjection> {
    return request(
      `/api/v3/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}`,
    );
  },
  getGraphDocumentCandidate(productId: string, workflowId: string, nodeId: string): Promise<GraphDocumentCandidate> {
    return request(
      `/api/v3/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/nodes/${encodeURIComponent(nodeId)}/candidate`,
    );
  },
  applyGraphDocumentCandidate(
    productId: string,
    workflowId: string,
    nodeId: string,
    input: { artifact_id: string; base_graph_revision: number; section_keys?: string[] },
  ): Promise<GraphProjection> {
    return request(
      `/api/v3/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/nodes/${encodeURIComponent(nodeId)}/candidate/apply`,
      { method: "POST", body: JSON.stringify(input) },
    );
  },
  discardGraphDocumentCandidate(productId: string, workflowId: string, nodeId: string, artifactId: string): Promise<GraphProjection> {
    return request(
      `/api/v3/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/nodes/${encodeURIComponent(nodeId)}/candidate/discard`,
      { method: "POST", body: JSON.stringify({ artifact_id: artifactId }) },
    );
  },
  applyWorkflowChangeSet(
    productId: string,
    workflowId: string,
    changeSet: GraphChangeSet,
  ): Promise<GraphProjection> {
    return request(
      `/api/v3/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/changesets`,
      { method: "POST", body: JSON.stringify(changeSet) },
    );
  },
  undoWorkflowChangeSet(productId: string, workflowId: string): Promise<GraphProjection> {
    return request(
      `/api/v3/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/undo`,
      { method: "POST" },
    );
  },
  redoWorkflowChangeSet(productId: string, workflowId: string): Promise<GraphProjection> {
    return request(
      `/api/v3/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/redo`,
      { method: "POST" },
    );
  },
  submitGraphRun(
    productId: string,
    workflowId: string,
    input: GraphRunSubmitInput,
  ): Promise<GraphRun> {
    return request(
      `/api/v3/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/runs`,
      { method: "POST", body: JSON.stringify(input) },
    );
  },
  previewGraphRun(
    productId: string,
    workflowId: string,
    input: GraphRunSubmitInput,
  ): Promise<GraphRunPreviewResponse> {
    return request(
      `/api/v3/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/runs/preview`,
      { method: "POST", body: JSON.stringify(input) },
    );
  },
  graphRunEventsUrl(productId: string, workflowId: string, runId: string): string {
    return `/api/v3/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/runs/${encodeURIComponent(runId)}/events`;
  },
  listGraphRuns(productId: string, workflowId: string): Promise<GraphRunListResponse> {
    return request(
      `/api/v3/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/runs`,
    );
  },
  getGraphRun(productId: string, workflowId: string, runId: string): Promise<GraphRun> {
    return request(
      `/api/v3/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/runs/${encodeURIComponent(runId)}`,
    );
  },
  cancelGraphRun(productId: string, workflowId: string, runId: string): Promise<GraphRun> {
    return request(
      `/api/v3/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/runs/${encodeURIComponent(runId)}/cancel`,
      { method: "POST" },
    );
  },
  retryGraphRun(productId: string, workflowId: string, runId: string): Promise<GraphRun> {
    return request(
      `/api/v3/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/runs/${encodeURIComponent(runId)}/retry`,
      { method: "POST" },
    );
  },
  listWorkflowRecipes(
    includeArchived = false,
  ): Promise<WorkflowRecipeSummary[]> {
    const params = new URLSearchParams({
      include_archived: String(includeArchived),
    });
    return request(`/api/v3/workflow-recipes?${params}`);
  },
  getWorkflowRecipe(recipeId: string): Promise<WorkflowRecipe> {
    return request(`/api/v3/workflow-recipes/${encodeURIComponent(recipeId)}`);
  },
  createWorkflowRecipe(
    productId: string,
    workflowId: string,
    input: WorkflowRecipeSourceInput,
  ): Promise<WorkflowRecipe> {
    return request(
      `/api/v3/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/recipes`,
      { method: "POST", body: JSON.stringify(input) },
    );
  },
  appendWorkflowRecipeVersion(
    productId: string,
    workflowId: string,
    recipeId: string,
    input: WorkflowRecipeSourceInput & { expected_recipe_version: number },
  ): Promise<WorkflowRecipe> {
    return request(
      `/api/v3/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/recipes/${encodeURIComponent(recipeId)}/versions`,
      { method: "POST", body: JSON.stringify(input) },
    );
  },
  archiveWorkflowRecipe(recipeId: string, expectedRecipeVersion: number): Promise<{ changed: boolean; recipe: WorkflowRecipe }> {
    const params = new URLSearchParams({ expected_recipe_version: String(expectedRecipeVersion) });
    return request(`/api/v3/workflow-recipes/${encodeURIComponent(recipeId)}?${params}`, { method: "DELETE" });
  },
  previewWorkflowRecipe(
    productId: string,
    recipeId: string,
    input: { expected_recipe_version: number },
  ): Promise<WorkflowRecipePreview> {
    return request(`/api/v3/products/${encodeURIComponent(productId)}/workflow-recipes/${encodeURIComponent(recipeId)}/preview`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  applyWorkflowRecipe(
    productId: string,
    recipeId: string,
    input: {
      expected_recipe_version: number;
      expected_graph_revision: number;
      preview_digest: string;
      idempotency_key: string;
    },
  ): Promise<WorkflowRecipeApplicationResult> {
    return request(`/api/v3/products/${encodeURIComponent(productId)}/workflow-recipes/${encodeURIComponent(recipeId)}/apply`, {
      method: "POST",
      body: JSON.stringify(input),
    });
  },
  createEmptyWorkflowGraph(productId: string): Promise<GraphProjection> {
    return request(`/api/v3/products/${encodeURIComponent(productId)}/workflows`, { method: "POST" });
  },
  confirmGraphProposal(productId: string, workflowId: string, proposalId: string): Promise<GraphProjection> {
    return request(
      `/api/v3/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/proposals/${encodeURIComponent(proposalId)}/confirm`,
      { method: "POST" },
    );
  },
  discardGraphProposal(productId: string, workflowId: string, proposalId: string): Promise<GraphProjection> {
    return request(
      `/api/v3/products/${encodeURIComponent(productId)}/workflows/${encodeURIComponent(workflowId)}/proposals/${encodeURIComponent(proposalId)}/discard`,
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
  listDeliveryAdoptions(productId: string): Promise<DeliveryAdoptionListResponse> {
    return request(`/api/v3/products/${encodeURIComponent(productId)}/delivery-adoptions`);
  },
  getCurrentDeliveryAdoption(productId: string): Promise<DeliveryAdoptionVersion> {
    return request(`/api/v3/products/${encodeURIComponent(productId)}/delivery-adoptions/current`);
  },
  getDeliveryAdoption(productId: string, versionId: string): Promise<DeliveryAdoptionVersion> {
    return request(
      `/api/v3/products/${encodeURIComponent(productId)}/delivery-adoptions/${encodeURIComponent(versionId)}`,
    );
  },
  createDeliveryAdoption(
    productId: string,
    body: {
      slots: Array<{
        slot_key: string;
        sort_order: number;
        image_type_key?: string | null;
        source_asset_id: string;
        source_node_id?: string | null;
        delivery_spec: WorkflowDeliverySpec;
        quality_status?: "pass" | "fail" | "unchecked";
        quality_detail?: string | null;
        text_overflow?: boolean;
      }>;
      graph_id?: string | null;
      graph_revision?: number | null;
      fact_set_version_id?: string | null;
      visual_system_version_id?: string | null;
      notes?: string | null;
    },
  ): Promise<DeliveryAdoptionVersion> {
    return request(`/api/v3/products/${encodeURIComponent(productId)}/delivery-adoptions`, {
      method: "POST",
      body: JSON.stringify(body),
    });
  },
  listVisualSystems(includeArchived = false): Promise<VisualSystemSummary[]> {
    const query = new URLSearchParams();
    if (includeArchived) query.set("include_archived", "true");
    const suffix = query.toString() ? `?${query}` : "";
    return request(`/api/v3/visual-systems${suffix}`);
  },
  createVisualSystem(body: {
    name: string;
    payload: Record<string, unknown>;
  }): Promise<VisualSystemSummary> {
    return request("/api/v3/visual-systems", {
      method: "POST",
      body: JSON.stringify(body),
    });
  },
  appendVisualSystemVersion(
    systemId: string,
    payload: Record<string, unknown>,
  ): Promise<VisualSystemVersion> {
    return request(`/api/v3/visual-systems/${encodeURIComponent(systemId)}/versions`, {
      method: "POST",
      body: JSON.stringify({ payload }),
    });
  },
  getVisualInheritance(
    productId: string,
    productOverride?: Record<string, unknown>,
  ): Promise<VisualInheritanceView> {
    return request(`/api/v3/products/${encodeURIComponent(productId)}/visual-inheritance`, {
      method: "POST",
      body: JSON.stringify({ product_override: productOverride ?? {} }),
    });
  },
  selectProductVisualVersion(
    productId: string,
    visualSystemVersionId: string,
  ): Promise<ProductVisualSelection> {
    return request(`/api/v3/products/${encodeURIComponent(productId)}/visual-selection`, {
      method: "PUT",
      body: JSON.stringify({ visual_system_version_id: visualSystemVersionId }),
    });
  },
  previewDeliveryAdoption(
    productId: string,
    versionId: string,
    options: { allow_partial?: boolean; qualified_only?: boolean } = {},
  ): Promise<DeliveryAdoptionPreview> {
    return request(
      `/api/v3/products/${encodeURIComponent(productId)}/delivery-adoptions/${encodeURIComponent(versionId)}/preview`,
      { method: "POST", body: JSON.stringify(options) },
    );
  },
  ensureDeliveryAdoptionRenditions(
    productId: string,
    versionId: string,
    options: { qualified_only?: boolean } = {},
  ): Promise<{ preview: DeliveryAdoptionPreview }> {
    return request(
      `/api/v3/products/${encodeURIComponent(productId)}/delivery-adoptions/${encodeURIComponent(versionId)}/renditions`,
      { method: "POST", body: JSON.stringify(options) },
    );
  },
  downloadDeliveryAdoptionExport(
    productId: string,
    versionId: string,
    options: { allow_partial?: boolean; qualified_only?: boolean } = {},
  ): Promise<Blob> {
    return requestBlob(
      `/api/v3/products/${encodeURIComponent(productId)}/delivery-adoptions/${encodeURIComponent(versionId)}/export`,
      { method: "POST", body: JSON.stringify(options) },
    );
  },
  async getLocalImageEditCapability(): Promise<LocalImageEditCapability> {
    const parsed = parseLocalImageEditCapability(await request<unknown>("/api/v3/local-image-edits/capability"));
    if (!parsed) throw new ApiError(502, "局部编辑 capability 响应无效");
    return parsed;
  },
  async listLocalImageEdits(productId: string, limit = 50): Promise<LocalImageEditTaskListResponse> {
    const params = new URLSearchParams({ limit: String(Math.min(100, Math.max(1, limit))) });
    const parsed = parseLocalImageEditTaskList(
      await request<unknown>(`/api/v3/products/${encodeURIComponent(productId)}/image-edits?${params}`),
    );
    if (!parsed) throw new ApiError(502, "局部编辑任务列表响应无效");
    return parsed;
  },
  async getLocalImageEdit(productId: string, taskId: string): Promise<LocalImageEditTask> {
    const parsed = parseLocalImageEditTask(
      await request<unknown>(
        `/api/v3/products/${encodeURIComponent(productId)}/image-edits/${encodeURIComponent(taskId)}`,
      ),
    );
    if (!parsed) throw new ApiError(502, "局部编辑任务响应无效");
    return parsed;
  },
  async createLocalImageEdit(productId: string, input: LocalImageEditCreateInput): Promise<LocalImageEditTask> {
    const body = new FormData();
    body.append("source_asset_id", input.source_asset_id);
    body.append("operation", input.operation);
    body.append("mask", input.mask, "local-edit-mask.png");
    body.append("mask_geometry_json", JSON.stringify(input.mask_geometry));
    body.append("reference_asset_ids_json", JSON.stringify(input.reference_asset_ids ?? []));
    if (input.instruction != null) body.append("instruction", input.instruction);
    if (input.source_text != null) body.append("source_text", input.source_text);
    if (input.replacement_text != null) body.append("replacement_text", input.replacement_text);
    if (input.target_node_id != null) body.append("target_node_id", input.target_node_id);
    const parsed = parseLocalImageEditTask(
      await request<unknown>(`/api/v3/products/${encodeURIComponent(productId)}/image-edits`, {
        method: "POST",
        body,
      }),
    );
    if (!parsed) throw new ApiError(502, "局部编辑创建响应无效");
    return parsed;
  },
  async updateLocalImageEdit(
    productId: string,
    taskId: string,
    input: LocalImageEditUpdateInput,
  ): Promise<LocalImageEditTask> {
    const body = new FormData();
    body.append("expected_revision", String(input.expected_revision));
    body.append("operation", input.operation);
    body.append("mask", input.mask, "local-edit-mask.png");
    body.append("mask_geometry_json", JSON.stringify(input.mask_geometry));
    body.append("reference_asset_ids_json", JSON.stringify(input.reference_asset_ids ?? []));
    if (input.instruction != null) body.append("instruction", input.instruction);
    if (input.source_text != null) body.append("source_text", input.source_text);
    if (input.replacement_text != null) body.append("replacement_text", input.replacement_text);
    const parsed = parseLocalImageEditTask(
      await request<unknown>(
        `/api/v3/products/${encodeURIComponent(productId)}/image-edits/${encodeURIComponent(taskId)}`,
        { method: "PATCH", body },
      ),
    );
    if (!parsed) throw new ApiError(502, "局部编辑更新响应无效");
    return parsed;
  },
  async submitLocalImageEdit(productId: string, taskId: string, idempotencyKey: string): Promise<LocalImageEditTask> {
    const parsed = parseLocalImageEditTask(
      await request<unknown>(
        `/api/v3/products/${encodeURIComponent(productId)}/image-edits/${encodeURIComponent(taskId)}/submit`,
        { method: "POST", body: JSON.stringify({ idempotency_key: idempotencyKey }) },
      ),
    );
    if (!parsed) throw new ApiError(502, "局部编辑提交响应无效");
    return parsed;
  },
  async cancelLocalImageEdit(productId: string, taskId: string, expectedRevision?: number): Promise<LocalImageEditTask> {
    const parsed = parseLocalImageEditTask(
      await request<unknown>(
        `/api/v3/products/${encodeURIComponent(productId)}/image-edits/${encodeURIComponent(taskId)}/cancel`,
        {
          method: "POST",
          ...(expectedRevision == null ? {} : { body: JSON.stringify({ expected_revision: expectedRevision }) }),
        },
      ),
    );
    if (!parsed) throw new ApiError(502, "局部编辑取消响应无效");
    return parsed;
  },
  async retryLocalImageEdit(productId: string, taskId: string, expectedRevision?: number): Promise<LocalImageEditTask> {
    const parsed = parseLocalImageEditTask(
      await request<unknown>(
        `/api/v3/products/${encodeURIComponent(productId)}/image-edits/${encodeURIComponent(taskId)}/retry`,
        {
          method: "POST",
          ...(expectedRevision == null ? {} : { body: JSON.stringify({ expected_revision: expectedRevision }) }),
        },
      ),
    );
    if (!parsed) throw new ApiError(502, "局部编辑重试响应无效");
    return parsed;
  },
  async adoptLocalImageEdit(
    productId: string,
    taskId: string,
    expectedCurrentArtifactId: string,
  ): Promise<LocalImageEditTask> {
    const parsed = parseLocalImageEditTask(
      await request<unknown>(
        `/api/v3/products/${encodeURIComponent(productId)}/image-edits/${encodeURIComponent(taskId)}/adopt`,
        {
          method: "POST",
          body: JSON.stringify({ expected_current_artifact_id: expectedCurrentArtifactId }),
        },
      ),
    );
    if (!parsed) throw new ApiError(502, "局部编辑 adoption 响应无效");
    return parsed;
  },
  async revertLocalImageEdit(
    productId: string,
    taskId: string,
    adoptionEventId: string,
    expectedCurrentArtifactId: string,
  ): Promise<LocalImageEditTask> {
    const parsed = parseLocalImageEditTask(
      await request<unknown>(
        `/api/v3/products/${encodeURIComponent(productId)}/image-edits/${encodeURIComponent(taskId)}/adoptions/${encodeURIComponent(adoptionEventId)}/revert`,
        {
          method: "POST",
          body: JSON.stringify({ expected_current_artifact_id: expectedCurrentArtifactId }),
        },
      ),
    );
    if (!parsed) throw new ApiError(502, "局部编辑 revert 响应无效");
    return parsed;
  },
};
