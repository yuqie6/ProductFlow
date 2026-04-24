export type ProductWorkflowState = "draft" | "copy_ready" | "poster_ready" | "failed";
export type CopyStatus = "draft" | "confirmed";
export type PosterKind = "main_image" | "promo_poster";
export type JobKind = "copy_generation" | "poster_generation";
export type JobStatus = "queued" | "running" | "succeeded" | "failed";
export type SourceAssetKind = "original_image" | "reference_image" | "processed_product_image";
export type ImageSessionAssetKind = "reference_upload" | "generated_image";
export type WorkflowNodeType =
  | "product_context"
  | "reference_image"
  | "copy_generation"
  | "image_generation";
export type WorkflowNodeStatus = "idle" | "queued" | "running" | "succeeded" | "failed";
export type WorkflowRunStatus = "running" | "succeeded" | "failed";

export interface SessionState {
  authenticated: boolean;
}

export interface SourceAsset {
  id: string;
  kind: SourceAssetKind;
  original_filename: string;
  mime_type: string;
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

export interface CopySet {
  id: string;
  creative_brief_id: string | null;
  status: CopyStatus;
  title: string;
  selling_points: string[];
  poster_headline: string;
  cta: string;
  model_title: string;
  model_selling_points: string[];
  model_poster_headline: string;
  model_cta: string;
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

export interface JobRun {
  id: string;
  product_id: string;
  kind: JobKind;
  status: JobStatus;
  target_poster_kind: PosterKind | null;
  failure_reason: string | null;
  attempts: number;
  created_at: string;
  started_at: string | null;
  finished_at: string | null;
  copy_set_id: string | null;
  poster_variant_id: string | null;
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
  recent_jobs: JobRun[];
  created_at: string;
  updated_at: string;
}

export interface ProductHistory {
  copy_sets: CopySet[];
  poster_variants: PosterVariant[];
  jobs: JobRun[];
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
  status: WorkflowNodeStatus;
  output_json: Record<string, unknown> | null;
  failure_reason: string | null;
  copy_set_id: string | null;
  poster_variant_id: string | null;
  image_session_asset_id: string | null;
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
  node_runs: WorkflowNodeRun[];
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

export interface CopySetUpdateRequest {
  title?: string;
  selling_points?: string[];
  poster_headline?: string;
  cta?: string;
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
  generated_asset: ImageSessionAsset;
  created_at: string;
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

export type ConfigSource = "database" | "env_default";
export type ConfigInputType = "text" | "password" | "number" | "boolean" | "select" | "textarea";

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
  value: string | number | boolean | null;
  source: ConfigSource;
  secret: boolean;
  has_value: boolean;
  options: ConfigOption[];
  minimum: number | null;
  maximum: number | null;
}

export interface ConfigResponse {
  items: ConfigItem[];
}

export interface ConfigUpdateRequest {
  values?: Record<string, string | number | boolean | null>;
  reset_keys?: string[];
}
