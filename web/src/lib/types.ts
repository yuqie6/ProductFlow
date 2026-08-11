export type ProductWorkflowState = "draft" | "copy_ready" | "poster_ready" | "failed";
export type ProductListSort = "updated_desc" | "created_desc" | "name_asc";
export type CopyStatus = "draft" | "confirmed";
export type PosterKind = "main_image" | "promo_poster";
export type JobStatus = "queued" | "running" | "succeeded" | "failed" | "cancelled";
export type SourceAssetKind = "original_image" | "reference_image" | "processed_product_image";
export type ImageSessionAssetKind = "reference_upload" | "generated_image";
export type MediaVerificationStatus = "verified" | "legacy_pending" | "missing";
export type ProductImageOriginType =
  | "upload"
  | "workflow_generation"
  | "image_session_attach"
  | "legacy_import";
export type WorkflowNodeType =
  | "product_context"
  | "reference_image"
  | "copy_generation"
  | "image_generation";
export type WorkflowNodeTypeV2 =
  | "product_context"
  | "reference_image"
  | "prompt_generation"
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

export interface ProductImageAsset {
  id: string;
  product_id: string;
  media_object_id: string;
  origin_type: ProductImageOriginType;
  display_name: string;
  original_filename: string;
  parent_asset_id: string | null;
  source_image_session_asset_id: string | null;
  mime_type: string;
  byte_size: number | null;
  width: number | null;
  height: number | null;
  verification_status: MediaVerificationStatus;
  download_url: string;
  preview_url: string;
  thumbnail_url: string;
  created_at: string;
  updated_at: string;
}

export interface ProductImageAssetListResponse {
  items: ProductImageAsset[];
}

export interface CanonicalProductDetail {
  id: string;
  name: string;
  category: string | null;
  price: string | null;
  source_note: string | null;
  cover_image_asset_id: string | null;
  image_assets: ProductImageAsset[];
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
  template_language?: string;
  file: File;
  referenceFiles?: File[];
}

export interface CreateCanonicalProductInput {
  name: string;
  category?: string;
  price?: string;
  source_note?: string;
  images: File[];
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

export type JsonValue = string | number | boolean | null | JsonValue[] | { [key: string]: JsonValue };
export type ProductFactStatus = "observed" | "user_declared" | "confirmed" | "conflicted";
export type ProductFactSourceType = "user" | "image_observation" | "agent_inference" | "legacy_product";
export type WorkflowDraftStatus =
  | "collecting"
  | "awaiting_confirmation"
  | "confirmed"
  | "materializing"
  | "ready"
  | "failed"
  | "cancelled";

export interface WorkflowDraftFactConflict {
  value: JsonValue;
  source_type: ProductFactSourceType;
  evidence_asset_ids: string[];
}

export interface WorkflowDraftProductFact {
  key: string;
  value: JsonValue;
  source_type: ProductFactSourceType;
  status: ProductFactStatus;
  requires_confirmation: boolean;
  evidence_asset_ids: string[];
  conflicts: WorkflowDraftFactConflict[];
}

export interface WorkflowDraftReferenceBinding {
  key: string;
  asset_id: string;
  role: string;
  label: string;
}

export type WorkflowVisualLockedField =
  | "style"
  | "colors"
  | "typography"
  | "spacing"
  | "decorations"
  | "photography"
  | "quality"
  | "product_fidelity"
  | "prohibitions";

export interface WorkflowVisualColor {
  role: string;
  value: string;
  label: string;
}

export interface WorkflowVisualTypography {
  title_font: string;
  body_font: string;
  scale: {
    headline: number;
    subtitle: number;
    body: number;
  };
}

export interface WorkflowVisualSpacing {
  min_edge_whitespace_percent: number;
  principles: string[];
}

export interface WorkflowVisualDecorations {
  elements: string[];
  icon_style: string;
}

export interface WorkflowVisualPhotography {
  lighting: string;
  depth_of_field: string;
  camera_parameters: string[];
}

export interface WorkflowVisualQuality {
  resolution: string;
  commercial_grade: string;
  realism: string;
  minimum_quality: "draft" | "standard" | "high";
}

export interface WorkflowVisualProductFidelity {
  preserve_shape: boolean;
  preserve_proportions: boolean;
  preserve_materials: boolean;
  requirements: string[];
}

export interface WorkflowVisualSystemPayloadV1 {
  name: string;
  style: string[];
  colors: WorkflowVisualColor[];
  typography: WorkflowVisualTypography;
  spacing: WorkflowVisualSpacing;
  decorations: WorkflowVisualDecorations;
  photography: WorkflowVisualPhotography;
  quality: WorkflowVisualQuality;
  product_fidelity: WorkflowVisualProductFidelity;
  locked_fields: WorkflowVisualLockedField[];
  variants: Array<{ key: string; title: string; guidance: string[] }>;
  prohibitions: string[];
  reference_assets: Array<{ asset_id: string; role: string; label: string }>;
}

export type WorkflowDraftVisualSystem =
  | {
      mode: "draft";
      version_id?: null;
      payload: WorkflowVisualSystemPayloadV1;
      source_markdown?: string | null;
    }
  | {
      mode: "confirmed_version";
      version_id: string;
      payload?: null;
      source_markdown?: string | null;
    };

export type WorkflowVisualFieldOverride =
  | { field: "style"; value: string[] }
  | { field: "colors"; value: WorkflowVisualColor[] }
  | { field: "typography"; value: WorkflowVisualTypography }
  | { field: "spacing"; value: WorkflowVisualSpacing }
  | { field: "decorations"; value: WorkflowVisualDecorations }
  | { field: "photography"; value: WorkflowVisualPhotography }
  | { field: "quality"; value: WorkflowVisualQuality }
  | { field: "product_fidelity"; value: WorkflowVisualProductFidelity }
  | { field: "prohibitions"; value: string[] };

export interface WorkflowVisualExceptionPlan {
  key: string;
  scope:
    | { type: "workflow"; key?: null }
    | { type: "image_type" | "image_plan"; key: string };
  overrides: WorkflowVisualFieldOverride[];
  reason: string;
}

export interface WorkflowGenerationSpec {
  aspect_ratio: string;
  resolution_tier: "standard" | "high" | "ultra";
  quality_intent: "draft" | "standard" | "high";
  reference_fidelity: "low" | "medium" | "high";
  background_intent: "auto" | "opaque" | "transparent";
  text_policy: "none" | "allow" | "required";
  text_language?: string | null;
}

export interface WorkflowDeliverySpec {
  width: number;
  height: number;
  format: "png" | "jpeg" | "webp";
  max_byte_size?: number | null;
  fit: "contain" | "cover";
  background_color?: string | null;
  crop_anchor?: "center" | "top" | "bottom" | "left" | "right" | null;
}

export interface WorkflowDraftPlannedImage {
  key: string;
  order: number;
  variation_instruction?: string | null;
  generation_spec: WorkflowGenerationSpec;
  delivery_spec?: WorkflowDeliverySpec | null;
}

export interface WorkflowDraftImageTypePlan {
  key: string;
  title: string;
  order: number;
  quantity: number;
  prompt_plan_key: string;
  images: WorkflowDraftPlannedImage[];
}

export interface WorkflowDraftPromptPlan {
  key: string;
  image_type_key: string;
  title: string;
  payload: WorkflowImagePromptPayloadV1;
}

export interface WorkflowPromptProductFidelity {
  complex_structure: boolean;
  product_present: boolean;
  picture_in_picture: "none" | "allowed" | "required";
  requirements: string[];
}

export interface WorkflowPromptComposition {
  viewpoint: string;
  product_share_percent: number;
  layout: string;
  copy_regions: string[];
}

export interface WorkflowPromptContentElements {
  focus: string[];
  selling_points: string[];
  background: string;
  decorations: string[];
}

export interface WorkflowPromptTextContent {
  headline: string | null;
  subtitle: string | null;
  body: string | null;
}

export interface WorkflowPerImagePromptPlan {
  image_plan_key: string;
  instruction: string;
  viewpoint: string | null;
  composition_adjustments: string[];
  lighting: string | null;
}

export interface WorkflowImagePromptPayloadV1 {
  schema_version: 1;
  shared_rules: string[];
  design_goal: string;
  product_fidelity: WorkflowPromptProductFidelity;
  creative_boundary: string[];
  composition: WorkflowPromptComposition;
  content: WorkflowPromptContentElements;
  text: WorkflowPromptTextContent;
  atmosphere: { keywords: string[]; lighting: string };
  visual_variant_key: string | null;
  fact_keys: string[];
  evidence_asset_ids: string[];
  images: WorkflowPerImagePromptPlan[];
}

export interface WorkflowDraftFolderPlan {
  key: string;
  title: string;
  order: number;
  position_x: number;
  position_y: number;
  width: number;
  height: number;
}

interface WorkflowDraftNodePlanBase {
  key: string;
  title: string;
  position_x: number;
  position_y: number;
  folder_key?: string | null;
}

export type WorkflowDraftNodePlan =
  | (WorkflowDraftNodePlanBase & { node_type: "product_context" })
  | (WorkflowDraftNodePlanBase & { node_type: "reference_image"; reference_key: string })
  | (WorkflowDraftNodePlanBase & { node_type: "prompt_generation"; prompt_plan_key: string })
  | (WorkflowDraftNodePlanBase & { node_type: "image_generation"; image_plan_key: string });

export interface WorkflowDraftEdgePlan {
  key: string;
  source_node_key: string;
  target_node_key: string;
  source_handle?: string | null;
  target_handle?: string | null;
}

export interface WorkflowDraftPayloadV1 {
  schema_version: 1;
  title: string;
  facts: WorkflowDraftProductFact[];
  required_fact_keys: string[];
  missing_fact_keys: string[];
  reference_bindings: WorkflowDraftReferenceBinding[];
  visual_system: WorkflowDraftVisualSystem;
  visual_exceptions: WorkflowVisualExceptionPlan[];
  prompt_plans: WorkflowDraftPromptPlan[];
  image_types: WorkflowDraftImageTypePlan[];
  folders: WorkflowDraftFolderPlan[];
  nodes: WorkflowDraftNodePlan[];
  edges: WorkflowDraftEdgePlan[];
  confirmation_summary: string;
}

export interface WorkflowDraftLimits {
  min_image_types: number;
  min_images_per_type: number;
  max_images_per_type: number;
  max_total_images: number;
  max_reference_assets: number;
}

export interface WorkflowDraftRevision {
  id: string;
  draft_id: string;
  version: number;
  schema_version: 1;
  payload: WorkflowDraftPayloadV1;
  payload_hash: string;
  source_turn_id: string | null;
  source_artifact_step_id: string | null;
  confirmed_at: string | null;
  fact_set_version_id: string | null;
  visual_system_version_id: string | null;
  created_at: string;
}

export interface WorkflowDraft {
  id: string;
  product_id: string;
  status: WorkflowDraftStatus;
  current_revision_id: string;
  current_revision: WorkflowDraftRevision;
  revisions: WorkflowDraftRevision[];
  final_workflow_id: string | null;
  limits: WorkflowDraftLimits;
  created_at: string;
  updated_at: string;
}

export interface CreateWorkflowDraftInput {
  payload: WorkflowDraftPayloadV1;
  ready_for_confirmation?: boolean;
  source_turn_id?: string | null;
  source_artifact_step_id?: string | null;
}

export interface AppendWorkflowDraftRevisionInput extends CreateWorkflowDraftInput {
  expected_draft_version: number;
}

export interface WorkflowFolderV2 {
  id: string;
  workflow_id: string;
  key: string;
  title: string;
  order: number;
  position_x: number;
  position_y: number;
  width: number;
  height: number;
  config_json: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export interface WorkflowNodeV2 {
  id: string;
  workflow_id: string;
  schema_version: 2;
  key: string;
  node_type: WorkflowNodeTypeV2;
  title: string;
  position_x: number;
  position_y: number;
  folder_id: string | null;
  bound_image_asset_id: string | null;
  current_prompt_artifact_version_id: string | null;
  config_json: Record<string, unknown>;
  status: WorkflowNodeStatus;
  output_json: Record<string, unknown> | null;
  failure_reason: string | null;
  created_at: string;
  updated_at: string;
}

export interface WorkflowEdgeV2 {
  id: string;
  workflow_id: string;
  key: string;
  source_node_id: string;
  target_node_id: string;
  source_handle: string | null;
  target_handle: string | null;
  created_at: string;
}

export interface ProductWorkflowV2 {
  id: string;
  product_id: string;
  title: string;
  active: boolean;
  schema_version: 2;
  revision: number;
  source_draft_revision_id: string;
  visual_system_version_id: string;
  materialization_id: string;
  folders: WorkflowFolderV2[];
  nodes: WorkflowNodeV2[];
  edges: WorkflowEdgeV2[];
  created_at: string;
  updated_at: string;
}

export interface ActiveProductWorkflowV2 {
  latest_revision: number;
  workflow: ProductWorkflowV2 | null;
}

export interface WorkflowMaterializationResult {
  id: string;
  created: boolean;
  workflow: ProductWorkflowV2;
  reveal_events_url: string;
}

export interface MaterializeWorkflowDraftInput {
  expected_draft_version: number;
  expected_workflow_revision: number;
  idempotency_key: string;
}

export interface WorkflowActualMedia {
  mime_type: "image/png" | "image/jpeg" | "image/webp";
  width: number;
  height: number;
  byte_size: number;
  sha256: string;
}

export interface WorkflowNodeRunV2 {
  id: string;
  schema_version: 2;
  workflow_run_id: string;
  node_id: string;
  node_type: WorkflowNodeTypeV2;
  status: WorkflowNodeStatus;
  output_json: Record<string, JsonValue> | null;
  failure_reason: string | null;
  visual_system_version_id: string;
  prompt_artifact_version_id: string | null;
  generation_record_id: string | null;
  result_asset_id: string | null;
  requested_spec: WorkflowGenerationSpec | null;
  effective_parameters: Record<string, JsonValue> | null;
  actual_media: WorkflowActualMedia | null;
  compiled_prompt: string | null;
  compiled_prompt_hash: string | null;
  reference_asset_ids: string[];
  provider_name: string | null;
  provider_model: string | null;
  provider_response_id: string | null;
  provider_status: string | null;
  started_at: string;
  finished_at: string | null;
}

export interface SubmitWorkflowNodeRunV2Result {
  created: boolean;
  node_run: WorkflowNodeRunV2;
}

export interface WorkflowRevealEvent {
  schema_version: 1;
  materialization_id: string;
  sequence: number;
  kind: "folder" | "node" | "edge" | "completed";
  entity_type: string | null;
  entity_id: string | null;
  payload: Record<string, JsonValue>;
  created_at: string;
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
  template_language?: string;
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
  title: string;
  rounds_count: number;
  latest_generated_asset: ImageSessionAsset | null;
  created_at: string;
  updated_at: string;
}

export interface ImageSessionDetail {
  id: string;
  title: string;
  assets: ImageSessionAsset[];
  rounds: ImageSessionRound[];
  generation_tasks: ImageSessionGenerationTask[];
  created_at: string;
  updated_at: string;
}

export interface ImageSessionStatus {
  id: string;
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
