export type ProductListSort = "updated_desc" | "created_desc" | "name_asc";
export type JobStatus = "queued" | "running" | "succeeded" | "failed" | "cancelled" | "unknown";
export type DeliveryRenditionStatus = Exclude<JobStatus, "cancelled" | "unknown">;
export type ImageSessionAssetKind = "reference_upload" | "generated_image";
export type MediaVerificationStatus = "verified" | "legacy_pending" | "missing";
export type ProductImageOriginType =
  | "upload"
  | "workflow_generation"
  | "image_session_attach"
  | "legacy_import";
export type AgentProductImageTypeKey =
  | "hero"
  | "selling_point"
  | "scene"
  | "detail"
  | "sku"
  | "dimensions"
  | "specifications"
  | "after_sales"
  | "brand_story"
  | "precautions"
  | "certification"
  | "faq"
  | "factory"
  | "packaging"
  | "shipping";
export type GalleryDirectoryKind =
  | "all"
  | "recent_generated"
  | "uploads"
  | "generated"
  | "image_type"
  | "source"
  | "unorganized"
  | "user_folder";
export type GalleryAssetSort = "created_desc" | "created_asc" | "name_asc" | "name_desc";
export type MediaLibrarySourceType = "legacy_gallery" | "image_session_generated" | "product_asset" | "direct_upload";
export type WorkflowNodeTypeV2 =
  | "product_context"
  | "reference_image"
  | "prompt_generation"
  | "image_generation";
export type WorkflowNodeStatus = "idle" | "queued" | "running" | "succeeded" | "failed" | "cancelled" | "unknown";
export type WorkflowRunStatus = "running" | "succeeded" | "failed" | "cancelled" | "unknown";

export interface SessionState {
  authenticated: boolean;
  access_required: boolean;
}

export interface ProductSummary {
  id: string;
  name: string;
  category: string | null;
  price: string | null;
  cover_image_asset_id: string | null;
  cover_image_filename: string | null;
  cover_image_download_url: string | null;
  cover_image_preview_url: string | null;
  cover_image_thumbnail_url: string | null;
  created_at: string;
  updated_at: string;
}

export interface ProductListResponse {
  items: ProductSummary[];
  total: number;
  page: number;
  page_size: number;
}

export interface ProductImageAsset {
  id: string;
  product_id: string;
  media_object_id: string;
  origin_type: ProductImageOriginType;
  display_name: string;
  original_filename: string;
  image_type_key: string | null;
  user_folder_id: string | null;
  parent_asset_id: string | null;
  source_image_session_asset_id: string | null;
  source_library_asset_id: string | null;
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

export type LegacyArchiveKind = "workflow" | "canvas_agent_thread" | "user_template";

export interface LegacyArchiveListItem {
  kind: LegacyArchiveKind;
  id: string;
  source_id: string;
  source_key: string | null;
  product_id: string | null;
  product_name: string | null;
  title: string;
  description: string | null;
  source_status: string | null;
  archive_schema_version: number;
  payload_sha256: string;
  source_updated_at: string | null;
  created_at: string;
  counts: Record<string, number>;
}

export interface LegacyArchivePage {
  items: LegacyArchiveListItem[];
  next_cursor: string | null;
  total: number;
  kind_counts: Record<LegacyArchiveKind, number>;
}

export interface LegacyArchiveAsset {
  product_image_asset_id: string;
  role: string;
  legacy_source_type: string;
  legacy_source_id: string;
  display_name: string;
  original_filename: string;
  mime_type: string;
  byte_size: number | null;
  width: number | null;
  height: number | null;
  verification_status: MediaVerificationStatus;
  download_url: string;
  preview_url: string;
  thumbnail_url: string;
  created_at: string;
}

export interface LegacyArchiveDetail {
  item: LegacyArchiveListItem;
  source_profile: string;
  source_fingerprint_sha256: string;
  payload: Record<string, unknown>;
  diagnostics: Array<Record<string, unknown>>;
  assets: LegacyArchiveAsset[];
}

export interface GalleryGenerationSummary {
  workflow_id: string;
  node_id: string;
  node_run_id: string;
  prompt_artifact_version_id: string;
  visual_system_version_id: string;
}

export interface GalleryRenditionSummary {
  job_id: string;
  source_asset_id: string;
  delivery_spec: WorkflowDeliverySpec;
  status: DeliveryRenditionStatus;
}

export interface AgentAttachment {
  id: string;
  display_name: string;
  original_filename: string;
  download_url: string;
  preview_url: string;
  thumbnail_url: string;
}

export interface GalleryAsset extends ProductImageAsset {
  user_folder_name: string | null;
  image_type_title: string | null;
  generation: GalleryGenerationSummary | null;
  rendition: GalleryRenditionSummary | null;
}

export interface GalleryAssetPage {
  items: GalleryAsset[];
  next_cursor: string | null;
}

export interface GallerySystemDirectory {
  kind: GalleryDirectoryKind;
  count: number;
}

export interface GalleryImageTypeDirectory {
  directory_key: string;
  image_type_key: string | null;
  title: string;
  count: number;
}

export interface GalleryOriginDirectory {
  origin_type: ProductImageOriginType;
  count: number;
}

export interface GalleryFolder {
  id: string;
  name: string;
  sort_order: number;
  count: number;
}

export interface GalleryBootstrap {
  product_id: string;
  cover_image_asset_id: string | null;
  system_directories: GallerySystemDirectory[];
  image_types: GalleryImageTypeDirectory[];
  origins: GalleryOriginDirectory[];
  user_folders: GalleryFolder[];
  unorganized_count: number;
}

export interface GalleryFolderMutation {
  id: string;
  name: string;
  sort_order: number;
}

export interface GalleryDeleteFolderResult {
  folder_id: string;
  moved_to_unorganized_count: number;
}

export interface GalleryDirectorySelection {
  kind: GalleryDirectoryKind;
  key: string | null;
}

export interface MediaLibraryFolder {
  id: string;
  name: string;
  count: number;
}

export interface MediaLibraryTag {
  id: string;
  name: string;
  count: number;
}

export interface MediaLibraryAsset {
  id: string;
  media_object_id: string;
  source_type: MediaLibrarySourceType;
  source_id: string;
  display_name: string;
  folder_id: string | null;
  folder_name: string | null;
  tags: MediaLibraryTag[];
  original_filename: string;
  revision: number;
  is_archived: boolean;
  archived_at: string | null;
  provenance_hash: string;
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

export interface MediaLibraryAssetPage {
  items: MediaLibraryAsset[];
  next_cursor: string | null;
}

export interface MediaLibraryBootstrap {
  total_count: number;
  active_count: number;
  archived_count: number;
  unorganized_count: number;
  folders: MediaLibraryFolder[];
  tags: MediaLibraryTag[];
}

export interface WorkflowMediaLibraryAsset {
  asset: MediaLibraryAsset;
  product_image_asset_id: string | null;
  linked_at: string;
}

export interface WorkflowMediaLibraryAssetListResponse {
  workflow_id: string;
  items: WorkflowMediaLibraryAsset[];
}

export interface CanonicalProductDetail {
  id: string;
  name: string;
  category: string | null;
  price: string | null;
  source_note: string | null;
  cover_image_asset_id: string | null;
  created_at: string;
  updated_at: string;
}

/** The product projection attached to a schema-v3 product source node. */
export interface GraphSourceProduct {
  id: string;
  name: string;
  category: string | null;
  price: string | null;
  source_note: string | null;
}

export interface GraphProductFact {
  key: string;
  value: JsonValue;
  source_type?: ProductFactSourceType;
  status?: ProductFactStatus;
  requires_confirmation?: boolean;
  evidence_asset_ids?: string[];
  conflicts?: Array<Record<string, JsonValue>>;
}

export interface GraphProductFactSet {
  id: string;
  version: number;
  facts: GraphProductFact[];
}

export interface ProductFactsResponse {
  product: CanonicalProductDetail;
  fact_set: GraphProductFactSet | null;
}

export interface UpdateProductFactsInput {
  expected_fact_version: number | null;
  name: string;
  category: string | null;
  price: string | null;
  source_note: string | null;
  facts: GraphProductFact[];
}

export interface AgentProductImageTypeOption {
  key: AgentProductImageTypeKey;
  title: string;
  description: string;
  order: number;
}

export interface AgentProductWorkspaceLimits {
  min_image_types: number;
  default_images_per_type: number;
  min_images_per_type: number;
  max_images_per_type: number;
  max_total_images: number;
  min_reference_images: number;
  max_reference_images: number;
  allowed_image_mime_types: Array<"image/png" | "image/jpeg" | "image/webp">;
}

export interface AgentProductWorkspaceOptions {
  schema_version: 1;
  image_types: AgentProductImageTypeOption[];
  limits: AgentProductWorkspaceLimits;
}

export interface AgentProductImageTypeSelection {
  key: AgentProductImageTypeKey;
  quantity: number;
  order: number;
}

export interface AgentProductSelectionV1 {
  schema_version: 1;
  image_types: AgentProductImageTypeSelection[];
}

export interface WorkflowIntakeV1 extends AgentProductSelectionV1 {
  reference_asset_ids: string[];
}

export interface CreateAgentProductWorkspaceInput {
  name: string;
  selection: AgentProductSelectionV1;
  images: File[];
  idempotency_key: string;
  agent_session_id?: string | null;
}

export interface CreateAgentProductDraftWorkspaceInput {
  name: string;
  idempotency_key: string;
  agent_session_id?: string | null;
}

export interface FinalizeAgentProductWorkspaceIntakeInput {
  conversation_id: string;
  selection: AgentProductSelectionV1;
  images: File[];
  idempotency_key: string;
  task_id?: string | null;
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

export interface DeliveryRenditionJob {
  id: string;
  product_id: string;
  source_asset_id: string;
  result_asset: ProductImageAsset | null;
  delivery_spec: WorkflowDeliverySpec;
  status: DeliveryRenditionStatus;
  attempts: number;
  is_retryable: boolean;
  failure_reason: string | null;
  created_at: string;
  started_at: string | null;
  finished_at: string | null;
  updated_at: string;
}

export interface DeliveryRenditionJobListResponse {
  items: DeliveryRenditionJob[];
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
  current_revision_id: string | null;
  current_revision: WorkflowDraftRevision | null;
  current_version: number;
  revisions: WorkflowDraftRevision[];
  intake: WorkflowIntakeV1 | null;
  recipe_seed: WorkflowDraftRecipeSeed | null;
  legacy_archive_seed: WorkflowDraftLegacyArchiveSeed | null;
  limits: WorkflowDraftLimits;
  created_at: string;
  updated_at: string;
}

export interface WorkflowDraftRecipeSeed {
  id: string;
  workflow_draft_id: string;
  recipe_version_id: string;
  recipe_id: string;
  recipe_version: number;
  recipe_title: string;
  product_id: string;
  schema_version: 1;
  created_at: string;
}

export interface WorkflowDraftLegacyArchiveSeed {
  id: string;
  workflow_draft_id: string;
  product_id: string;
  archive_kind: LegacyArchiveKind;
  archive_id: string;
  archive_title: string;
  archive_status: string | null;
  source_product_id: string | null;
  source_profile: string;
  archive_schema_version: number;
  payload_sha256: string;
  counts: Record<string, number>;
  schema_version: 1;
  created_at: string;
}

export interface LegacyArchiveAgentRebuildResult {
  created: boolean;
  archive_kind: LegacyArchiveKind;
  archive_id: string;
  target_product_id: string;
  draft: WorkflowDraft;
  conversation: AgentConversation;
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

export type WorkflowRecipeKind = "workflow_recipe" | "recipe_fragment";
export type WorkflowRecipeSourceType = "workflow" | "folder" | "selection";

export interface WorkflowRecipePayloadV1 {
  schema_version: 1;
  folders: Array<{ key: string; title: string; order: number }>;
  nodes: Array<{
    key: string;
    node_type: WorkflowNodeTypeV2;
    position_x: number;
    position_y: number;
    folder_key: string | null;
    reference_requirement_key?: string;
    image_type_key?: string;
    image_plan_key?: string;
  }>;
  edges: Array<{
    key: string;
    source_node_key: string;
    target_node_key: string;
    source_handle: string | null;
    target_handle: string | null;
  }>;
  boundary_requirements: Array<{
    key: string;
    direction: "inbound" | "outbound";
    local_node_key: string;
    external_node_type: WorkflowNodeTypeV2;
    role: string;
  }>;
  reference_requirements: Array<{ key: string; role: string; label: string; required: boolean }>;
  image_types: Array<{
    key: string;
    title: string;
    order: number;
    default_quantity: number;
    images: Array<{
      key: string;
      order: number;
      generation_spec: WorkflowGenerationSpec;
      delivery_spec: WorkflowDeliverySpec | null;
    }>;
  }>;
  prompt_shapes: Array<{
    image_type_key: string;
    product_present: boolean;
    picture_in_picture: "none" | "allowed" | "required";
    product_share_percent: number;
    text_slots: Array<"headline" | "subtitle" | "body">;
    fact_keys: string[];
    visual_variant_key: string | null;
    per_image_slots: Array<{
      image_plan_key: string;
      viewpoint: boolean;
      composition_adjustments: boolean;
      lighting: boolean;
    }>;
  }>;
  visual_requirements: {
    required_locked_fields: string[];
    required_variant_keys: string[];
  };
}

export interface WorkflowRecipeVersion {
  id: string;
  recipe_id: string;
  version: number;
  schema_version: 1;
  title: string;
  description: string | null;
  payload: WorkflowRecipePayloadV1;
  payload_hash: string;
  preferred_visual_system_version_id: string | null;
  created_at: string;
}

export interface WorkflowRecipeSummary {
  id: string;
  kind: WorkflowRecipeKind;
  current_version_id: string;
  current_version: WorkflowRecipeVersion;
  archived_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface WorkflowRecipe extends WorkflowRecipeSummary {
  versions: WorkflowRecipeVersion[];
}

export type AgentConversationScope = "product_workflow" | "global";

export interface AgentConversation {
  id: string;
  scope_type: AgentConversationScope;
  session_id: string | null;
  product_id: string | null;
  workflow_draft_id: string | null;
  harness_run_id: string;
  status: "collecting" | "awaiting_confirmation" | "completed" | "failed" | "canceled" | "unknown";
  created_at: string;
  updated_at: string;
}

export type AgentSessionStatus = "active" | "archived";

export interface AgentSessionConversation {
  conversation_id: string;
  scope_type: AgentConversationScope;
  product_id: string | null;
  product_name: string;
  conversation_status: AgentConversation["status"];
  updated_at: string;
}

export interface AgentSession {
  id: string;
  title: string;
  summary: string | null;
  status: AgentSessionStatus;
  archived_at: string | null;
  conversation_count: number;
  conversations: AgentSessionConversation[];
  created_at: string;
  updated_at: string;
}

export interface AgentSessionListResponse {
  items: AgentSession[];
}

export type AgentTaskStatus =
  | "queued"
  | "running"
  | "waiting_user"
  | "awaiting_confirmation"
  | "succeeded"
  | "failed"
  | "canceled"
  | "paused"
  | "unknown";

export interface AgentTask {
  id: string;
  session_id: string;
  conversation_id: string | null;
  product_id: string | null;
  workflow_id: string | null;
  workflow_draft_id: string | null;
  title: string;
  goal: string;
  summary: string | null;
  status: AgentTaskStatus;
  waiting_reason: string | null;
  failure_reason: string | null;
  current_turn_id: string | null;
  created_at: string;
  updated_at: string;
  started_at: string | null;
  finished_at: string | null;
  canceled_at: string | null;
}

export interface AgentTaskListResponse {
  items: AgentTask[];
  next_cursor: string | null;
}

export interface CreateAgentTaskInput {
  session_id: string;
  title: string;
  goal: string;
  conversation_id?: string | null;
}

export interface AgentPageContextSnapshotInput {
  route: string;
  page_type: string;
  product_id?: string | null;
  workflow_id?: string | null;
  selected_asset_ids: string[];
  visible_asset_ids: string[];
  filters: Record<string, string>;
  workflow_revision?: number | null;
  library_revision?: number | null;
  captured_at: string;
}

export type AgentTurnStatus =
  | "queued"
  | "running"
  | "requires_input"
  | "awaiting_confirmation"
  | "succeeded"
  | "failed"
  | "cancel_requested"
  | "canceled"
  | "unknown";

export type AgentToolStepKind =
  | "load_skill"
  | "inject_context"
  | "ask_question"
  | "inspect_image"
  | "propose_draft"
  | "inspect_context"
  | "read_history"
  | "organize_assets"
  | "request_workflow_run"
  | "create_product";

export type AgentToolStepStatus = "running" | "succeeded" | "failed" | "unknown";

export type AgentToolStepDetailPhase = "skill_load" | "context_injection" | "question" | "tool_result";

export interface AgentToolStepValidationIssue {
  path: string;
  message: string;
}

export interface AgentToolStepDetails {
  phase?: AgentToolStepDetailPhase;
  skill_name?: string;
  resource_path?: string;
  instruction_excerpt?: string;
  instruction_truncated?: boolean;
  context_sections?: string[];
  runtime_context_keys?: string[];
  contract_fields?: string[];
  page_route?: string;
  page_type?: string;
  selected_asset_count?: number;
  visible_asset_count?: number;
  context_bytes?: number;
  input_summary?: string;
  output_summary?: string;
  error_code?: string;
  error_message?: string;
  retryable?: boolean;
  validation_issues?: AgentToolStepValidationIssue[];
  question_id?: string;
  question_header?: string;
  question_text?: string;
  option_labels?: string[];
}

export interface AgentToolStep {
  step_id: string;
  kind: AgentToolStepKind;
  summary: string;
  status: AgentToolStepStatus;
  tool_name?: string;
  details?: AgentToolStepDetails;
}

export interface AgentQuestionOption {
  label: string;
  description?: string;
}

export interface AgentQuestion {
  id: string;
  header: string;
  question: string;
  options: AgentQuestionOption[];
}

export interface AgentTurn {
  id: string;
  conversation_id: string;
  task_id: string | null;
  harness_turn_id: string | null;
  idempotency_key: string;
  input_text: string;
  input_asset_ids: string[];
  status: AgentTurnStatus;
  resume_required: boolean;
  output_text: string | null;
  error_text: string | null;
  tool_steps?: AgentToolStep[];
  question: AgentQuestion | null;
  question_answer: AgentQuestionAnswer | null;
  continuation_turn_id: string | null;
  artifact_name: string | null;
  artifact_step_id: string | null;
  workflow_draft_revision_id: string | null;
  library_organization_draft_revision_id: string | null;
  workflow_run_request_id: string | null;
  page_context_snapshot_id: string | null;
  sync_error: string | null;
  finished_at: string | null;
  created_at: string;
  updated_at: string;
}

export type AgentTurnEffectResult = "applied" | "failed" | "unknown";
export type AgentTurnReconciliationState = "applied" | "not_applied" | "conflict" | "unknown";

export interface AgentTurnEffectReconciliation {
  schema_version: 1;
  id: string;
  projection_id: string;
  tool_call_id: string;
  tool_name: "request_workflow_run_v1" | "create_product_workspace_v1";
  idempotency_key: string;
  effect_result: AgentTurnEffectResult;
  reconciliation_state: AgentTurnReconciliationState;
  result: Record<string, unknown> | null;
  detail: string | null;
  created_at: string;
  updated_at: string;
}

export type AgentWorkflowRunRequestStatus =
  | "awaiting_confirmation"
  | "confirmed"
  | "succeeded"
  | "failed"
  | "cancelled";

export interface AgentWorkflowRunRequest {
  id: string;
  conversation_id: string;
  task_id: string | null;
  product_id: string;
  product_name: string;
  workflow_id: string;
  workflow_title: string;
  expected_workflow_revision: number;
  status: AgentWorkflowRunRequestStatus;
  source_run_id?: string | null;
  workflow_run_id: string | null;
  workflow_run_status: WorkflowRunStatus | null;
  source_step_id: string;
  failure_reason: string | null;
  confirmed_at: string | null;
  finished_at: string | null;
  created_at: string;
  updated_at: string;
}

export type LibraryOrganizationDraftStatus =
  | "awaiting_confirmation"
  | "confirmed"
  | "failed"
  | "cancelled";

export type LibraryOrganizationOperationKind =
  | "rename"
  | "move"
  | "set_tags"
  | "archive"
  | "restore"
  | "link_workflow";

export interface LibraryOrganizationAssetBefore {
  revision: number;
  display_name: string;
  folder_id: string | null;
  tag_names: string[];
  is_archived: boolean;
}

export type LibraryOrganizationOperation = {
  operation: LibraryOrganizationOperationKind;
  asset_id: string;
  expected_revision: number;
  before: LibraryOrganizationAssetBefore;
  reason: string;
  target:
    | { display_name: string }
    | { folder_id: string | null }
    | { tag_names: string[] }
    | { is_archived: true }
    | { is_archived: false }
    | {
        workflow_id: string;
        workflow_title: string;
        expected_workflow_revision: number;
        expected_linked: boolean;
      };
};

export interface LibraryOrganizationDraftPayload {
  schema_version: 1;
  confirmation_summary: string;
  operations: LibraryOrganizationOperation[];
}

export interface LibraryOrganizationDraftRevision {
  id: string;
  version: number;
  schema_version: number;
  payload: LibraryOrganizationDraftPayload;
  payload_hash: string;
  source_turn_id: string | null;
  source_artifact_step_id: string | null;
  confirmed_at: string | null;
  created_at: string;
}

export interface LibraryOrganizationDraft {
  id: string;
  conversation_id: string;
  status: LibraryOrganizationDraftStatus;
  current_revision: LibraryOrganizationDraftRevision | null;
  confirmed_revision_id: string | null;
  confirmation_result: Record<string, unknown> | null;
  confirmed_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface GlobalWorkflowDraftReview {
  schema_version: 1;
  conversation_id: string;
  product_id: string;
  product_name: string;
  product_conversation_id: string;
  workflow_draft_id: string;
  draft: WorkflowDraft;
}

export interface AgentTurnPage {
  items: AgentTurn[];
  next_cursor: string | null;
}

export interface SubmitAgentTurnInput {
  input_text: string;
  asset_ids: string[];
  idempotency_key: string;
  task_id?: string | null;
  page_context?: AgentPageContextSnapshotInput | null;
}

export interface SubmitAgentTurnResponse {
  created: boolean;
  turn: AgentTurn;
}

export interface AgentQuestionAnswerResponse extends AgentTurn {
  answered_turn: AgentTurn;
  continuation_turn: AgentTurn;
}

export type AgentQuestionAnswer =
  | { option: number; text?: never }
  | { option?: never; text: string };

export interface AgentTurnEvent {
  schema_version: 1;
  run_id: string;
  turn_id: string;
  sequence: number;
  created_at: string;
  kind: string;
  payload: Record<string, unknown>;
}

export interface AgentProductWorkspaceCreateResponse {
  task_id: string | null;
  product: CanonicalProductDetail;
  created_assets: ProductImageAsset[];
  workflow_draft: WorkflowDraft;
  conversation: AgentConversation;
}

export interface AgentProductWorkspaceSnapshot extends AgentProductWorkspaceCreateResponse {
  created: boolean;
  intake_finalized: boolean;
}

export interface AgentWorkbenchBootstrap {
  mode: "agent";
  product: CanonicalProductDetail;
  conversation: AgentConversation;
  workflow_draft: WorkflowDraft;
  graph: GraphProjection | null;
  latest_workflow_revision: number;
}

export interface WorkflowRecipeApplicationResult {
  created: boolean;
  recipe_id: string;
  recipe_version_id: string;
  recipe_version: number;
  draft: WorkflowDraft;
  conversation: AgentConversation;
}

export interface WorkflowRecipeSourceInput {
  source_type: WorkflowRecipeSourceType;
  folder_id?: string | null;
  node_ids?: string[];
  expected_edit_version: number;
  title: string;
  description?: string | null;
  preferred_visual_system_version_id?: string | null;
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
export type ProviderPurpose = "prompt" | "image" | "agent";
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

export type GraphNodeType =
  | "product_source"
  | "image_asset"
  | "creative_brief"
  | "visual_system"
  | "prompt_generation"
  | "image_generation";
export type GraphEdgeDataType = "product_facts" | "image_asset" | "creative_brief" | "visual_system" | "prompt";
export type GraphEdgeRole = "facts" | "reference" | "brief" | "visual_guidance" | "prompt";
export type GraphConfigStatus = "incomplete" | "ready" | "stale";
export type GraphRunScope = "node" | "to_node" | "graph";
export type GraphNodeKind = "source" | "processing";

export interface GraphCatalogInputContract {
  data_type: GraphEdgeDataType;
  role: GraphEdgeRole;
  max_count: number | null;
  required_to_run: boolean;
}

export interface GraphCatalogNode {
  node_type: GraphNodeType;
  output_data_type: GraphEdgeDataType;
  kind: GraphNodeKind;
  accepts: GraphCatalogInputContract[];
}

export interface GraphNodeCatalog {
  version: number;
  nodes: GraphCatalogNode[];
}

export interface GraphEdgeSummary {
  id: string;
  node_id: string;
  data_type: GraphEdgeDataType;
  role: GraphEdgeRole;
  order: number;
}

export interface GraphNode {
  id: string;
  node_type: GraphNodeType;
  title: string;
  position_x: number;
  position_y: number;
  config: Record<string, unknown>;
  bound_asset_id: string | null;
  group_id: string | null;
  preview_asset_id: string | null;
  config_status: GraphConfigStatus;
  unused: boolean;
  source_product?: GraphSourceProduct | null;
  product_fact_set?: GraphProductFactSet | null;
  incoming: GraphEdgeSummary[];
  outgoing: GraphEdgeSummary[];
}

export interface GraphEdge {
  id: string;
  source_node_id: string;
  target_node_id: string;
  data_type: GraphEdgeDataType;
  role: GraphEdgeRole;
  order: number;
}

export interface GraphGroup {
  id: string;
  title: string;
  member_ids: string[];
}

export interface GraphProjection {
  id: string;
  product_id: string;
  title: string;
  schema_version: number;
  revision: number;
  source_draft_revision_id: string | null;
  last_operation_group_id: string | null;
  nodes: GraphNode[];
  edges: GraphEdge[];
  groups: GraphGroup[];
}

export interface DirectCreateProductResponse {
  product: CanonicalProductDetail;
  created_assets: ProductImageAsset[];
  graph: GraphProjection;
}

export interface GraphChangeSet {
  base_graph_revision: number;
  summary: string;
  operations: Array<Record<string, unknown>>;
}

export interface DraftGraphPersistResponse {
  created: boolean;
  graph: GraphProjection;
}

export interface GraphNodeRun {
  id: string;
  node_id: string | null;
  status: WorkflowNodeStatus;
  sort_order: number;
  compiled_context: Record<string, unknown> | null;
  output: Record<string, unknown> | null;
  failure_reason: string | null;
  started_at: string;
  finished_at: string | null;
}

export interface GraphRun {
  id: string;
  graph_id: string;
  status: WorkflowRunStatus;
  scope: GraphRunScope;
  requested_node_id: string | null;
  graph_revision: number;
  failure_reason: string | null;
  is_retryable: boolean;
  node_runs: GraphNodeRun[];
  started_at: string;
  finished_at: string | null;
}

export interface GraphRunListResponse {
  items: GraphRun[];
}
