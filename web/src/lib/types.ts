export type ProductWorkflowState = "draft" | "copy_ready" | "poster_ready" | "failed";
export type CopyStatus = "draft" | "confirmed";
export type PosterKind = "main_image" | "promo_poster";
export type JobStatus = "queued" | "running" | "succeeded" | "failed" | "cancelled";
export type SourceAssetKind = "original_image" | "reference_image" | "processed_product_image";
export type ImageSessionAssetKind = "reference_upload" | "generated_image";
export type WorkflowNodeType =
  | "product_context"
  | "reference_image"
  | "copy_generation"
  | "image_generation";
export type WorkflowNodeStatus = "idle" | "queued" | "running" | "succeeded" | "failed" | "cancelled";
export type WorkflowNodeRunStatusValue = WorkflowNodeStatus;
export type WorkflowRunStatus = "running" | "succeeded" | "failed" | "cancelled";
export type WorkflowRetryHint = "retry_later" | "revise_input" | "check_settings";
export type CanvasTemplateKind = "full_canvas" | "node_group";
export type CanvasTemplateScenario =
  | "main_image"
  | "taobao_main_image"
  | "xiaohongshu_image"
  | "multi_angle"
  | "sku_variant"
  | "feature_infographic"
  | "size_spec"
  | "scale_reference"
  | "package_checklist"
  | "usage_steps"
  | "comparison"
  | "model_lifestyle"
  | "scene_image"
  | "detail_material"
  | "campaign_promotion"
  | "short_video_cover"
  | "white_background";

export interface SessionState {
  authenticated: boolean;
  access_required: boolean;
}

export interface SourceAsset {
  id: string;
  kind: SourceAssetKind;
  original_filename: string;
  mime_type: string;
  source_poster_variant_id?: string | null;
  download_url: string;
  preview_url: string;
  thumbnail_url: string;
  created_at: string;
}

export interface CreativeBriefSummary {
  id: string;
  payload: {
    positioning?: string;
    audience?: string;
    selling_angles?: string[];
    taboo_phrases?: string[];
    poster_style_hint?: string;
    [key: string]: unknown;
  };
  provider_name: string;
  model_name: string;
  prompt_version: string;
  created_at: string;
}

export interface CopyBlock {
  id: string;
  role?: string | null;
  label?: string | null;
  text: string;
  note?: string | null;
  visual_hint?: string | null;
  priority?: number | null;
}

export interface CopySection {
  id: string;
  title?: string | null;
  body?: string | null;
  items: CopyBlock[];
  visual_hint?: string | null;
}

export type CopyContent =
  | { kind: "freeform"; text: string }
  | { kind: "blocks"; blocks: CopyBlock[] }
  | { kind: "layout_brief"; sections: CopySection[] };

export interface VisualGuidance {
  main_message?: string | null;
  hierarchy: string[];
  composition_hint?: string | null;
  text_density?: "none" | "low" | "medium" | "high" | null;
  avoid: string[];
}

export interface CopyPayloadV2 {
  version: 2;
  purpose?: string | null;
  summary: string;
  content: CopyContent;
  visual_guidance?: VisualGuidance | null;
}

export interface CopySet {
  id: string;
  creative_brief_id: string | null;
  status: CopyStatus;
  structured_payload: CopyPayloadV2;
  model_structured_payload: CopyPayloadV2 | null;
  provider_name: string;
  model_name: string;
  prompt_version: string;
  created_at: string;
  updated_at: string;
  edited_at: string | null;
  confirmed_at: string | null;
}

export interface PosterVariant {
  id: string;
  product_id: string;
  copy_set_id: string;
  kind: PosterKind;
  template_name: string;
  mime_type: string;
  width: number;
  height: number;
  download_url: string;
  preview_url: string;
  thumbnail_url: string;
  created_at: string;
}

export interface ProductSummary {
  id: string;
  name: string;
  category: string | null;
  price: string | null;
  workflow_state: ProductWorkflowState;
  latest_copy_status: CopyStatus | null;
  latest_poster_at: string | null;
  source_image_filename: string | null;
  source_image_download_url: string | null;
  source_image_preview_url: string | null;
  source_image_thumbnail_url: string | null;
  created_at: string;
  updated_at: string;
}

export interface ProductListResponse {
  items: ProductSummary[];
  total: number;
  page: number;
  page_size: number;
}

export interface ProductDetail {
  id: string;
  name: string;
  category: string | null;
  price: string | null;
  source_note: string | null;
  workflow_state: ProductWorkflowState;
  source_assets: SourceAsset[];
  latest_brief: CreativeBriefSummary | null;
  current_confirmed_copy_set: CopySet | null;
  copy_sets: CopySet[];
  poster_variants: PosterVariant[];
  created_at: string;
  updated_at: string;
}

export interface ProductHistory {
  copy_sets: CopySet[];
  poster_variants: PosterVariant[];
}

export interface CreateProductInput {
  name: string;
  category?: string;
  price?: string;
  source_note?: string;
  canvas_template_key?: string;
  file: File;
  referenceFiles?: File[];
}

export interface WorkflowNode {
  id: string;
  workflow_id: string;
  node_type: WorkflowNodeType;
  title: string;
  position_x: number;
  position_y: number;
  config_json: Record<string, unknown>;
  status: WorkflowNodeStatus;
  output_json: Record<string, unknown> | null;
  failure_reason: string | null;
  is_retryable: boolean;
  attempt_count: number;
  retry_count: number;
  non_retryable_reason: string | null;
  retry_hint: WorkflowRetryHint | null;
  last_run_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface WorkflowEdge {
  id: string;
  workflow_id: string;
  source_node_id: string;
  target_node_id: string;
  source_handle: string | null;
  target_handle: string | null;
  created_at: string;
}

export interface WorkflowNodeRun {
  id: string;
  workflow_run_id: string;
  node_id: string;
  status: WorkflowNodeRunStatusValue;
  output_json: Record<string, unknown> | null;
  failure_reason: string | null;
  copy_set_id: string | null;
  poster_variant_id: string | null;
  image_session_asset_id: string | null;
  started_at: string;
  finished_at: string | null;
}

export interface WorkflowNodeRunStatus {
  id: string;
  workflow_run_id: string;
  node_id: string;
  status: WorkflowNodeRunStatusValue;
  failure_reason: string | null;
  started_at: string;
  finished_at: string | null;
}

export interface WorkflowRun {
  id: string;
  workflow_id: string;
  status: WorkflowRunStatus;
  started_at: string;
  finished_at: string | null;
  failure_reason: string | null;
  progress_metadata: Record<string, unknown> | null;
  is_retryable: boolean;
  is_cancelable: boolean;
  queue_active_count: number;
  queue_running_count: number;
  queue_queued_count: number;
  queue_max_concurrent_tasks: number;
  queued_ahead_count: number | null;
  queue_position: number | null;
  node_runs: WorkflowNodeRun[];
}

export interface WorkflowRunStatusSummary {
  id: string;
  workflow_id: string;
  status: WorkflowRunStatus;
  started_at: string;
  finished_at: string | null;
  failure_reason: string | null;
  progress_metadata: Record<string, unknown> | null;
  is_retryable: boolean;
  is_cancelable: boolean;
  queue_active_count: number;
  queue_running_count: number;
  queue_queued_count: number;
  queue_max_concurrent_tasks: number;
  queued_ahead_count: number | null;
  queue_position: number | null;
  node_runs: WorkflowNodeRunStatus[];
}

export interface WorkflowNodeStatusSummary {
  id: string;
  workflow_id: string;
  status: WorkflowNodeStatus;
  failure_reason: string | null;
  is_retryable: boolean;
  attempt_count: number;
  retry_count: number;
  non_retryable_reason: string | null;
  retry_hint: WorkflowRetryHint | null;
  last_run_at: string | null;
  updated_at: string;
}

export interface ProductWorkflow {
  id: string;
  product_id: string;
  title: string;
  active: boolean;
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
  runs: WorkflowRun[];
  created_at: string;
  updated_at: string;
}

export interface ProductWorkflowStatus {
  id: string;
  product_id: string;
  title: string;
  active: boolean;
  has_active_workflow: boolean;
  nodes: WorkflowNodeStatusSummary[];
  runs: WorkflowRunStatusSummary[];
  created_at: string;
  updated_at: string;
}

export interface CanvasTemplateScenarioMetadata {
  scenario: CanvasTemplateScenario;
  title: string;
  description: string;
  ecommerce_stage: string;
  tags: string[];
}

export interface CanvasTemplateOutputSlot {
  node_key: string;
  label: string;
  description: string;
}

export interface CanvasTemplateReferenceInputHint {
  node_key: string;
  role: string;
  label: string;
  required: boolean;
  description: string;
}

export interface CanvasTemplateSuggestedConnection {
  source_node_key: string;
  target_node_key: string;
  reason: string;
}

export interface CanvasTemplateDefaultExternalConnection {
  source: "existing_product_context";
  target_node_key: string;
  label: string;
}

export interface CanvasTemplatePreviewNode {
  key: string;
  node_type: WorkflowNodeType;
  title: string;
  position_x: number;
  position_y: number;
  size: string | null;
}

export interface CanvasTemplatePreviewEdge {
  source_node_key: string;
  target_node_key: string;
}

export interface CanvasTemplateSummary {
  key: string;
  version: number;
  kind: CanvasTemplateKind;
  title: string;
  description: string;
  source: "builtin" | "user";
  user_template_id: string | null;
  scenario: CanvasTemplateScenarioMetadata;
  preview_nodes: CanvasTemplatePreviewNode[];
  preview_edges: CanvasTemplatePreviewEdge[];
  output_slots: CanvasTemplateOutputSlot[];
  reference_input_hints: CanvasTemplateReferenceInputHint[];
  suggested_connections: CanvasTemplateSuggestedConnection[];
  default_external_connections: CanvasTemplateDefaultExternalConnection[];
}

export interface CanvasTemplateListResponse {
  items: CanvasTemplateSummary[];
}

export interface ApplyWorkflowTemplateGroupInput {
  template_key: string;
  position_x: number;
  position_y: number;
}

export interface CreateUserTemplateGroupInput {
  title: string;
  description?: string;
  node_ids: string[];
}

export interface UpdateUserTemplateGroupInput {
  title?: string;
  description?: string;
}

export interface CopySetUpdateRequest {
  structured_payload: CopyPayloadV2;
}

export interface ImageSessionAsset {
  id: string;
  kind: ImageSessionAssetKind;
  original_filename: string;
  mime_type: string;
  download_url: string;
  preview_url: string;
  thumbnail_url: string;
  created_at: string;
}

export interface ImageSessionRound {
  id: string;
  prompt: string;
  assistant_message: string;
  size: string;
  model_name: string;
  provider_name: string;
  prompt_version: string;
  provider_response_id: string | null;
  previous_response_id: string | null;
  image_generation_call_id: string | null;
  generation_group_id: string | null;
  candidate_index: number;
  candidate_count: number;
  base_asset_id: string | null;
  selected_reference_asset_ids: string[];
  actual_size: string | null;
  provider_notes: string[];
  generated_asset: ImageSessionAsset;
  created_at: string;
}

export interface ImageToolOptions {
  model?: string | null;
  quality?: "auto" | "low" | "medium" | "high" | null;
  output_format?: "png" | "jpeg" | "webp" | null;
  output_compression?: number | null;
  background?: "auto" | "opaque" | "transparent" | null;
  moderation?: "auto" | "low" | null;
  action?: "auto" | "generate" | "edit" | null;
  input_fidelity?: "low" | "high" | null;
  partial_images?: number | null;
  n?: number | null;
}

export type ImageToolOptionKey = keyof ImageToolOptions;

export interface ImageSessionGenerationTask {
  id: string;
  session_id: string;
  status: JobStatus;
  prompt: string;
  size: string;
  base_asset_id: string | null;
  selected_reference_asset_ids: string[];
  generation_count: number;
  completed_candidates: number;
  active_candidate_index: number | null;
  progress_phase: string | null;
  progress_updated_at: string | null;
  provider_response_id: string | null;
  provider_response_status: string | null;
  progress_metadata: Record<string, unknown> | null;
  failure_reason: string | null;
  result_generation_group_id: string | null;
  tool_options: ImageToolOptions | null;
  provider_notes: string[];
  attempts: number;
  is_retryable: boolean;
  is_cancelable: boolean;
  created_at: string;
  started_at: string | null;
  finished_at: string | null;
  queue_active_count: number;
  queue_running_count: number;
  queue_queued_count: number;
  queue_max_concurrent_tasks: number;
  queued_ahead_count: number | null;
  queue_position: number | null;
}

export interface ImageSessionSummary {
  id: string;
  product_id: string | null;
  title: string;
  rounds_count: number;
  latest_generated_asset: ImageSessionAsset | null;
  created_at: string;
  updated_at: string;
}

export interface ImageSessionDetail {
  id: string;
  product_id: string | null;
  title: string;
  assets: ImageSessionAsset[];
  rounds: ImageSessionRound[];
  generation_tasks: ImageSessionGenerationTask[];
  created_at: string;
  updated_at: string;
}

export interface ImageSessionStatus {
  id: string;
  product_id: string | null;
  title: string;
  rounds_count: number;
  latest_round_id: string | null;
  latest_generation_group_id: string | null;
  has_active_generation_task: boolean;
  generation_tasks: ImageSessionGenerationTask[];
  created_at: string;
  updated_at: string;
}

export interface ImageSessionListResponse {
  items: ImageSessionSummary[];
}

export interface ProductWritebackResponse {
  product_id: string;
  message: string;
}

export interface GalleryEntry {
  id: string;
  image_session_asset_id: string;
  image_session_round_id: string | null;
  image_session_id: string;
  image_session_title: string;
  product_id: string | null;
  product_name: string | null;
  image: ImageSessionAsset;
  prompt: string | null;
  size: string | null;
  actual_size: string | null;
  model_name: string | null;
  provider_name: string | null;
  prompt_version: string | null;
  provider_response_id: string | null;
  image_generation_call_id: string | null;
  generation_group_id: string | null;
  candidate_index: number | null;
  candidate_count: number | null;
  base_asset_id: string | null;
  selected_reference_asset_ids: string[];
  provider_notes: string[];
  created_at: string;
}

export interface GalleryEntryListResponse {
  items: GalleryEntry[];
}

export type ConfigSource = "database" | "env_default";
export type ConfigInputType = "text" | "password" | "number" | "boolean" | "select" | "multi_select" | "textarea";

export interface ConfigOption {
  value: string;
  label: string;
}

export interface ConfigItem {
  key: string;
  label: string;
  category: string;
  input_type: ConfigInputType;
  description: string;
  value: string | number | boolean | string[] | null;
  source: ConfigSource;
  secret: boolean;
  has_value: boolean;
  options: ConfigOption[];
  minimum: number | null;
  maximum: number | null;
  updated_at: string | null;
}

export interface ConfigResponse {
  items: ConfigItem[];
}

export interface RuntimeConfig {
  image_generation_max_dimension: number;
  image_tool_allowed_fields: ImageToolOptionKey[];
  admin_access_required: boolean;
  deletion_enabled: boolean;
}

export interface GenerationQueueOverview {
  active_count: number;
  running_count: number;
  queued_count: number;
  max_concurrent_tasks: number;
}

export interface ConfigUpdateRequest {
  values?: Record<string, string | number | boolean | string[] | null>;
  reset_keys?: string[];
}

export interface SettingsLockState {
  unlocked: boolean;
  configured: boolean;
}

export type ProviderCapability = "text_responses" | "image_responses" | "image_images" | "image_google_gemini";
export type ProviderPurpose = "text" | "image";
export type ProviderType = "openai_compatible" | "google_gemini";

export interface ProviderProfile {
  id: string;
  name: string;
  provider_type: ProviderType;
  base_url: string | null;
  capabilities: ProviderCapability[];
  default_models: Record<string, unknown>;
  config: Record<string, unknown>;
  enabled: boolean;
  archived_at: string | null;
  has_api_key: boolean;
  created_at: string;
  updated_at: string;
}

export interface ProviderProfileCreateRequest {
  name: string;
  provider_type?: ProviderType;
  base_url?: string | null;
  api_key?: string | null;
  capabilities: ProviderCapability[];
  default_models?: Record<string, unknown>;
  config?: Record<string, unknown>;
  enabled?: boolean;
}

export interface ProviderProfileUpdateRequest {
  name?: string | null;
  provider_type?: ProviderType | null;
  base_url?: string | null;
  api_key?: string | null;
  capabilities?: ProviderCapability[] | null;
  default_models?: Record<string, unknown> | null;
  config?: Record<string, unknown> | null;
  enabled?: boolean | null;
}

export interface ProviderBinding {
  id: string;
  purpose: ProviderPurpose;
  provider_kind: string;
  provider_profile_id: string | null;
  model_settings: Record<string, unknown>;
  config: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export interface ProviderBindingUpdateRequest {
  provider_kind: string;
  provider_profile_id?: string | null;
  model_settings?: Record<string, unknown>;
  config?: Record<string, unknown>;
}

export interface ProviderConfigResponse {
  profiles: ProviderProfile[];
  bindings: ProviderBinding[];
}

export interface SettingsExportMetadata {
  schema_version: number;
  exported_at: string;
  app: string;
  compatibility: string;
  app_version: string;
}

export interface SettingsExportProviderProfile {
  id: string;
  name: string;
  provider_type: ProviderType;
  base_url: string | null;
  api_key: string | null;
  capabilities: ProviderCapability[];
  default_models: Record<string, unknown>;
  config: Record<string, unknown>;
  enabled: boolean;
}

export interface SettingsExportProviderBinding {
  purpose: ProviderPurpose;
  provider_kind: string;
  provider_profile_name?: string | null;
  provider_profile_id?: string | null;
  model_settings: Record<string, unknown>;
  config: Record<string, unknown>;
}

export interface SettingsExportPayload {
  metadata: SettingsExportMetadata;
  runtime_config: Record<string, string | number | boolean | string[] | null>;
  provider_profiles: SettingsExportProviderProfile[];
  provider_bindings: SettingsExportProviderBinding[];
}

export interface SettingsImportPreviewResponse {
  schema_version: number;
  runtime_config_count: number;
  provider_profile_count: number;
  provider_binding_count: number;
  provider_profile_names: string[];
  provider_binding_purposes: ProviderPurpose[];
  includes_api_keys: boolean;
  provider_profiles_with_api_key_count: number;
}

export interface SettingsImportCommitResponse {
  preview: SettingsImportPreviewResponse;
  config: ConfigResponse;
  provider_config: ProviderConfigResponse;
}

export interface DuplicateWorkflowNodeGroupInput {
  node_ids: string[];
  offset_x?: number;
  offset_y?: number;
  position_x?: number;
  position_y?: number;
}

export type LaunchKitStatus = "draft" | "generating" | "ready" | "failed" | "archived";
export type LaunchKitPlatform = "shopee" | "tiktok_shop";
export type LaunchKitProgressStage =
  | "extracting_facts"
  | "applying_playbook"
  | "applying_store_profile"
  | "generating_angles"
  | "generating_copy"
  | "planning_images"
  | "scoring"
  | "exporting_optional_snapshot";

export interface SourceReferenceRequest {
  pasted_reference_text?: string | null;
  reference_urls?: string[];
  notes?: string | null;
}


export interface LaunchKitFeedbackRequest {
  used?: boolean | null;
  edited?: boolean | null;
  would_reuse?: boolean | null;
  would_pay?: boolean | null;
  notes?: string | null;
  metrics?: Record<string, unknown>;
}

export interface LaunchKitCreateRequest {
  product_name: string;
  category_key: string;
  target_platforms: LaunchKitPlatform[];
  source_references?: SourceReferenceRequest | null;
}

export interface LaunchKitTaskStatus {
  id: string;
  status: JobStatus;
  progress_stage: LaunchKitProgressStage | null;
  attempt_count: number;
  failure_category: string | null;
  failure_detail: string | null;
  is_retryable: boolean;
  is_cancelable: boolean;
  started_at: string | null;
  progress_updated_at: string | null;
  finished_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface LaunchKitSummary {
  id: string;
  product_id: string;
  product_name: string;
  category_key: string;
  target_platforms: string[];
  status: LaunchKitStatus;
  latest_task: LaunchKitTaskStatus | null;
  quality_score_summary: Record<string, unknown> | null;
  created_at: string;
  updated_at: string;
}

export interface LaunchKitListResponse {
  items: LaunchKitSummary[];
  total: number;
  page: number;
  page_size: number;
}

export interface LaunchKitDetail extends LaunchKitSummary {
  buyer_angle_key: string | null;
  source_references: Record<string, unknown>;
  generated_summary: Record<string, unknown> | null;
  selected_angle: Record<string, unknown> | null;
  export_snapshot: Record<string, unknown> | null;
  seller_feedback: Record<string, unknown> | null;
  variants: Record<string, unknown>[];
  exports: Record<string, unknown>[];
}

export interface LaunchKitStatusResponse {
  id: string;
  status: LaunchKitStatus;
  latest_task: LaunchKitTaskStatus | null;
  updated_at: string;
}
