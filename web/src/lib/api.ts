import type {
  AgentProductWorkspaceCreateResponse,
  AgentProductWorkspaceOptions,
  AgentWorkbenchBootstrap,
  ActiveProductWorkflowV2,
  AppendWorkflowDraftRevisionInput,
  ApplyWorkflowTemplateGroupInput,
  CanvasTemplateSummary,
  CanvasTemplateListResponse,
  CanonicalProductCreateResponse,
  CanonicalProductDetail,
  ConfigResponse,
  ConfigUpdateRequest,
  CopySet,
  CopySetUpdateRequest,
  DuplicateWorkflowNodeGroupInput,
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
  CreateUserTemplateGroupInput,
  CreateProductInput,
  CreateCanonicalProductInput,
  CreateAgentProductWorkspaceInput,
  CreateWorkflowDraftInput,
  DeliveryRenditionJob,
  DeliveryRenditionJobListResponse,
  ImageSessionDetail,
  ImageSessionListResponse,
  ImageSessionStatus,
  ImageToolOptions,
  ProductDetail,
  ProductHistory,
  ProductListSort,
  ProviderBinding,
  ProviderBindingUpdateRequest,
  ProviderConfigResponse,
  ProviderProfile,
  ProviderProfileCreateRequest,
  ProviderProfileUpdateRequest,
  ProductWorkflow,
  MaterializeWorkflowDraftInput,
  ProductWorkflowState,
  ProductWorkflowStatus,
  ProductWritebackResponse,
  ProductListResponse,
  ProductImageAsset,
  ProductImageAssetListResponse,
  RuntimeConfig,
  SettingsLockState,
  SettingsExportPayload,
  SettingsImportCommitResponse,
  SettingsImportPreviewResponse,
  SessionState,
  SubmitWorkflowNodeRunV2Result,
  UpdateUserTemplateGroupInput,
  WorkflowDraft,
  WorkflowDeliverySpec,
  WorkflowCanvasMutationResult,
  WorkflowMaterializationResult,
  WorkflowReferenceBindingResult,
  WorkflowNodeRunV2,
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
  listProducts(input?: {
    page?: number;
    page_size?: number;
    status?: ProductWorkflowState;
    q?: string;
    sort?: ProductListSort;
  }): Promise<ProductListResponse> {
    const params = new URLSearchParams({
      page: String(input?.page ?? 1),
      page_size: String(input?.page_size ?? 20),
    });
    if (input?.status) {
      params.set("status", input.status);
    }
    const query = input?.q?.trim();
    if (query) {
      params.set("q", query);
    }
    if (input?.sort && input.sort !== "updated_desc") {
      params.set("sort", input.sort);
    }
    return request(`/api/products?${params.toString()}`);
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
    if (input.template_language !== undefined) {
      formData.set("template_language", input.template_language);
    }
    return request("/api/products", {
      method: "POST",
      body: formData,
    });
  },
  async createCanonicalProduct(input: CreateCanonicalProductInput): Promise<CanonicalProductCreateResponse> {
    const formData = new FormData();
    formData.set("name", input.name);
    input.images.forEach((image) => {
      formData.append("images", image);
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
    return request("/api/v2/products", {
      method: "POST",
      body: formData,
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
  getAgentWorkbench(productId: string): Promise<AgentWorkbenchBootstrap> {
    return request(`/api/v2/products/${encodeURIComponent(productId)}/agent-workbench`);
  },
  getCanonicalProduct(productId: string): Promise<CanonicalProductDetail> {
    return request(`/api/v2/products/${productId}`);
  },
  listProductImageAssets(productId: string): Promise<ProductImageAssetListResponse> {
    return request(`/api/v2/products/${productId}/image-assets`);
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
  deleteProductImageAsset(assetId: string): Promise<void> {
    return request(`/api/v2/product-image-assets/${assetId}`, { method: "DELETE" });
  },
  setProductCover(productId: string, assetId: string): Promise<CanonicalProductDetail> {
    return request(`/api/v2/products/${productId}/cover`, {
      method: "PUT",
      body: JSON.stringify({ asset_id: assetId }),
    });
  },
  clearProductCover(productId: string): Promise<CanonicalProductDetail> {
    return request(`/api/v2/products/${productId}/cover`, { method: "DELETE" });
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
  attachImageSessionAssetToProduct(
    sessionId: string,
    assetId: string,
    input: { product_id: string; target: "reference" | "main_source" },
  ): Promise<ProductWritebackResponse> {
    return request(`/api/image-sessions/${sessionId}/assets/${assetId}/attach-to-product`, {
      method: "POST",
      body: JSON.stringify(input),
    });
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
  getProductWorkflow(productId: string): Promise<ProductWorkflow> {
    return request(`/api/products/${productId}/workflow`);
  },
  getActiveProductWorkflowV2(productId: string): Promise<ActiveProductWorkflowV2> {
    return request(`/api/v2/products/${productId}/workflow`);
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
    return request(`/api/v2/workflow-nodes/${nodeId}/run`, {
      method: "POST",
    });
  },
  getWorkflowNodeRunV2(nodeRunId: string): Promise<WorkflowNodeRunV2> {
    return request(`/api/v2/workflow-node-runs/${nodeRunId}`);
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
    const path = `/api/v2/workflow-materializations/${materializationId}/reveal-events`;
    return toApiUrl(after && after > 0 ? `${path}?after=${after}` : path);
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
