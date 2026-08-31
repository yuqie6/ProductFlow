import { useCallback, useEffect, useId, useRef, useState } from "react";
import type { ChangeEvent, FormEvent, ReactNode, RefObject } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Bot,
  Box,
  Check,
  CheckCircle2,
  Download,
  FileJson,
  Image,
  KeyRound,
  Link2,
  Pencil,
  Plus,
  Loader2,
  LockKeyhole,
  MessageSquareText,
  RotateCcw,
  Save,
  Search,
  ServerCog,
  Settings as SettingsIcon,
  ShieldCheck,
  SlidersHorizontal,
  Trash2,
  UploadCloud,
  X,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { useNavigate, useSearchParams } from "react-router-dom";

import { ConfirmDialog } from "../components/ConfirmDialog";
import { TopNav } from "../components/TopNav";
import { Dialog, DialogClose, DialogContent, DialogTitle } from "../components/ui/dialog";
import { Select as SelectField } from "../components/ui/select";
import { api, ApiError } from "../lib/api";
import type { TranslationKey } from "../lib/i18n";
import { useI18n } from "../lib/preferences";
import type {
  ConfigItem,
  ConfigResponse,
  ProviderBinding,
  ProviderBindingUpdateRequest,
  ProviderCapability,
  ProviderConfigResponse,
  ProviderProfile,
  ProviderProfileCreateRequest,
  ProviderProfileUpdateRequest,
  ProviderType,
  SettingsExportPayload,
  SettingsImportPreviewResponse,
} from "../lib/types";

type DraftValue = string | boolean | string[];
export type SettingsSectionId =
  | "providers"
  | "prompt"
  | "agent"
  | "image"
  | "prompts"
  | "upload"
  | "queue"
  | "security"
  | "migration";

interface DraftSnapshot {
  value: DraftValue;
}

interface ConfigDraftState {
  drafts: Record<string, DraftValue>;
  snapshots: Record<string, DraftSnapshot>;
}

interface SettingsSection {
  id: SettingsSectionId;
  labelKey: TranslationKey;
  descriptionKey: TranslationKey;
  groupKey: TranslationKey;
  icon: LucideIcon;
}

export interface ProviderProfileFormState {
  name: string;
  provider_type: ProviderType;
  base_url: string;
  api_key: string;
  capabilities: ProviderCapability[];
  enabled: boolean;
}

export interface ProviderProfileUsage {
  prompt: boolean;
  agent: boolean;
  image: boolean;
}

export interface ProviderDrawerViewState {
  open: boolean;
  editingProfileId: string | null;
  form: ProviderProfileFormState;
}

export interface PromptBindingDraft {
  provider_kind: "mock" | "openai";
  provider_profile_id: string;
  model: string;
}

export interface ImageBindingDraft {
  provider_kind: "mock" | "openai_responses" | "openai_images" | "google_gemini_image";
  provider_profile_id: string;
  model: string;
  images_quality: string;
  images_style: string;
  responses_background_enabled: boolean;
  gemini_api_version: string;
  gemini_output_mime_type: string;
}

export interface AgentBindingDraft {
  provider_kind: "mock" | "openai";
  provider_profile_id: string;
  model: string;
  reasoning_effort: string;
  reasoning_summary: string;
  text_verbosity: string;
  service_tier: string;
}

const INPUT_CLASS =
  "h-11 w-full rounded-lg border border-slate-200 bg-slate-50 px-4 text-sm text-slate-950 " +
  "placeholder:text-slate-400 shadow-sm shadow-slate-200/35 focus:border-indigo-500 focus:bg-white " +
  "focus:outline-none focus:ring-1 focus:ring-indigo-500 " +
  "dark:border-slate-700 dark:bg-[#111b2d] dark:text-slate-100 dark:shadow-black/20 " +
  "dark:placeholder:text-slate-500 dark:focus:border-violet-400 dark:focus:bg-[#111b2d]";

const TEXTAREA_CLASS =
  "w-full rounded-lg border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-950 " +
  "placeholder:text-slate-400 shadow-sm shadow-slate-200/35 focus:border-indigo-500 focus:bg-white " +
  "focus:outline-none focus:ring-1 focus:ring-indigo-500 dark:border-slate-700 dark:bg-[#111b2d] " +
  "dark:text-slate-100 dark:shadow-black/20 dark:placeholder:text-slate-500 dark:focus:border-violet-400";

const PANEL_CLASS =
  "rounded-xl border border-slate-200 bg-white p-6 shadow-sm shadow-slate-200/60 " +
  "dark:border-slate-800 dark:bg-[#0f1726] dark:shadow-black/25";

const SETTINGS_MAIN_ACTION_CLASS =
  "inline-flex h-11 items-center justify-center rounded-lg bg-indigo-600 px-5 text-sm font-semibold text-white " +
  "shadow-sm shadow-indigo-500/25 hover:bg-indigo-500 disabled:opacity-50 dark:bg-violet-500 dark:hover:bg-violet-400";

const PROVIDER_DRAWER_INPUT_CLASS =
  "h-[43px] w-full rounded-xl border border-slate-300 bg-white px-4 text-sm font-medium text-slate-950 " +
  "placeholder:text-slate-400 outline-none transition focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/20 " +
  "dark:border-slate-700 dark:bg-[#192234] dark:text-slate-100 dark:placeholder:text-slate-500 " +
  "dark:focus:border-violet-500 dark:focus:ring-violet-500/35";

const SETTINGS_SECTIONS: SettingsSection[] = [
  {
    id: "providers",
    labelKey: "settings.section.providers",
    descriptionKey: "settings.section.providersDescription",
    groupKey: "settings.groupProviders",
    icon: ServerCog,
  },
  {
    id: "prompt",
    labelKey: "settings.section.prompt",
    descriptionKey: "settings.section.promptDescription",
    groupKey: "settings.groupProviders",
    icon: MessageSquareText,
  },
  {
    id: "agent",
    labelKey: "settings.section.agent",
    descriptionKey: "settings.section.agentDescription",
    groupKey: "settings.groupProviders",
    icon: Bot,
  },
  {
    id: "image",
    labelKey: "settings.section.image",
    descriptionKey: "settings.section.imageDescription",
    groupKey: "settings.groupProviders",
    icon: Image,
  },
  {
    id: "prompts",
    labelKey: "settings.section.prompts",
    descriptionKey: "settings.section.promptsDescription",
    groupKey: "settings.groupWorkflow",
    icon: SlidersHorizontal,
  },
  {
    id: "upload",
    labelKey: "settings.section.upload",
    descriptionKey: "settings.section.uploadDescription",
    groupKey: "settings.groupWorkflow",
    icon: UploadCloud,
  },
  {
    id: "queue",
    labelKey: "settings.section.queue",
    descriptionKey: "settings.section.queueDescription",
    groupKey: "settings.groupWorkflow",
    icon: SettingsIcon,
  },
  {
    id: "security",
    labelKey: "settings.section.security",
    descriptionKey: "settings.section.securityDescription",
    groupKey: "settings.groupSecurity",
    icon: ShieldCheck,
  },
  {
    id: "migration",
    labelKey: "settings.section.migration",
    descriptionKey: "settings.section.migrationDescription",
    groupKey: "settings.groupSecurity",
    icon: FileJson,
  },
];

const SETTINGS_GROUPS: TranslationKey[] = ["settings.groupProviders", "settings.groupWorkflow", "settings.groupSecurity"];

export function settingsSectionIds(): SettingsSectionId[] {
  return SETTINGS_SECTIONS.map((section) => section.id);
}

export function settingsSectionFromSearchParam(value: string | null): SettingsSectionId {
  return SETTINGS_SECTIONS.some((section) => section.id === value) ? (value as SettingsSectionId) : "providers";
}

export function shouldShowSettingsMigrationPanel(section: SettingsSectionId): boolean {
  return section === "migration";
}

const PROVIDER_CAPABILITY_OPTIONS: Array<{ value: ProviderCapability; labelKey: TranslationKey }> = [
  { value: "text_responses", labelKey: "settings.provider.capability.textResponses" },
  { value: "image_responses", labelKey: "settings.provider.capability.imageResponses" },
  { value: "image_images", labelKey: "settings.provider.capability.imageImages" },
  { value: "image_mask_edit", labelKey: "settings.provider.capability.imageMaskEdit" },
  { value: "image_google_gemini", labelKey: "settings.provider.capability.imageGoogleGemini" },
];

function providerCapabilityLabelKey(capability: ProviderCapability): TranslationKey {
  return (
    PROVIDER_CAPABILITY_OPTIONS.find((option) => option.value === capability)?.labelKey ??
    "settings.provider.capability.imageGoogleGemini"
  );
}

const EMPTY_PROVIDER_FORM: ProviderProfileFormState = {
  name: "",
  provider_type: "openai_compatible",
  base_url: "",
  api_key: "",
  capabilities: ["text_responses", "image_images"],
  enabled: true,
};

function multiSelectValue(value: ConfigItem["value"]): string[] {
  if (Array.isArray(value)) {
    return value.map(String);
  }
  if (typeof value === "string") {
    return value
      .split(",")
      .map((part) => part.trim())
      .filter(Boolean);
  }
  return [];
}

function draftFromItem(item: ConfigItem): DraftValue {
  if (item.input_type === "boolean") {
    return Boolean(item.value);
  }
  if (item.input_type === "multi_select") {
    return multiSelectValue(item.value);
  }
  if (item.secret) {
    return "";
  }
  return item.value === null || item.value === undefined ? "" : String(item.value);
}

function draftValuesEqual(a: DraftValue, b: DraftValue): boolean {
  if (Array.isArray(a) || Array.isArray(b)) {
    return (
      Array.isArray(a) &&
      Array.isArray(b) &&
      a.length === b.length &&
      a.every((value, index) => value === b[index])
    );
  }
  return a === b;
}

export function draftsFromConfig(config: ConfigResponse): ConfigDraftState {
  const nextDrafts: Record<string, DraftValue> = {};
  const snapshots: Record<string, DraftSnapshot> = {};
  for (const item of config.items) {
    const value = draftFromItem(item);
    nextDrafts[item.key] = value;
    snapshots[item.key] = { value };
  }
  return { drafts: nextDrafts, snapshots };
}

export function configValuesFromChangedDrafts(
  items: ConfigItem[],
  drafts: Record<string, DraftValue>,
  snapshots: Record<string, DraftSnapshot>,
  secretTouched: Record<string, boolean>,
): Record<string, string | number | boolean | string[] | null> {
  const values: Record<string, string | number | boolean | string[] | null> = {};
  for (const item of items) {
    if (item.secret && !secretTouched[item.key]) {
      continue;
    }
    const snapshot = snapshots[item.key];
    const nextValue = drafts[item.key] ?? "";
    if (snapshot && draftValuesEqual(nextValue, snapshot.value)) {
      continue;
    }
    values[item.key] = nextValue;
  }
  return values;
}

function sourceLabel(item: ConfigItem, t: ReturnType<typeof useI18n>["t"]): string {
  return item.source === "database" ? t("settings.database") : t("settings.envDefault");
}

function sourceClassName(item: ConfigItem): string {
  if (item.source === "database") {
    return "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-400/35 dark:bg-emerald-500/12";
  }
  return "border-zinc-200 bg-zinc-50 text-zinc-500 dark:border-slate-700 dark:bg-[#0b1220]";
}

function textValue(record: Record<string, unknown> | undefined, key: string): string {
  const value = record?.[key];
  return typeof value === "string" ? value : "";
}

function boolValue(record: Record<string, unknown> | undefined, key: string, fallback: boolean): boolean {
  const value = record?.[key];
  return typeof value === "boolean" ? value : fallback;
}

function defaultCapabilitiesForProviderType(providerType: ProviderType): ProviderCapability[] {
  return providerType === "google_gemini" ? ["image_google_gemini"] : ["text_responses", "image_images"];
}

export function providerCapabilityValuesForType(providerType: ProviderType): ProviderCapability[] {
  return PROVIDER_CAPABILITY_OPTIONS
    .filter((option) => providerType === "google_gemini"
      ? option.value === "image_google_gemini"
      : option.value !== "image_google_gemini")
    .map((option) => option.value);
}

function providerTypeLabelKey(providerType: ProviderType): TranslationKey {
  return providerType === "google_gemini"
    ? "settings.provider.type.googleGemini"
    : "settings.provider.type.openaiCompatible";
}

function providerDefaultEndpointLabelKey(profile: ProviderProfile): TranslationKey {
  return profile.provider_type === "google_gemini"
    ? "settings.provider.defaultGoogleEndpoint"
    : "settings.provider.defaultBaseUrl";
}

export function providerFormFromProfile(profile?: ProviderProfile | null): ProviderProfileFormState {
  if (!profile) {
    return EMPTY_PROVIDER_FORM;
  }
  return {
    name: profile.name,
    provider_type: profile.provider_type,
    base_url: profile.base_url ?? "",
    api_key: "",
    capabilities: profile.capabilities,
    enabled: profile.enabled,
  };
}

export function providerDrawerCreateState(): ProviderDrawerViewState {
  return {
    open: true,
    editingProfileId: null,
    form: EMPTY_PROVIDER_FORM,
  };
}

export function providerDrawerEditState(profile: ProviderProfile): ProviderDrawerViewState {
  return {
    open: true,
    editingProfileId: profile.id,
    form: providerFormFromProfile(profile),
  };
}

export function providerUsageFromBindings(bindings: ProviderBinding[], profileId: string): ProviderProfileUsage {
  return {
    prompt: bindings.some((binding) => binding.purpose === "prompt" && binding.provider_profile_id === profileId),
    agent: bindings.some((binding) => binding.purpose === "agent" && binding.provider_profile_id === profileId),
    image: bindings.some((binding) => binding.purpose === "image" && binding.provider_profile_id === profileId),
  };
}

export function providerUsageLabelKeys(usage: ProviderProfileUsage): TranslationKey[] {
  const labels: TranslationKey[] = [];
  if (usage.prompt) {
    labels.push("settings.provider.usagePrompt");
  }
  if (usage.agent) {
    labels.push("settings.provider.usageAgent");
  }
  if (usage.image) {
    labels.push("settings.provider.usageImage");
  }
  return labels;
}

export function providerDisableBlocked(profile: ProviderProfile, usage: ProviderProfileUsage): boolean {
  return profile.enabled && (usage.prompt || usage.agent || usage.image);
}

export function providerProfileCreatePayload(form: ProviderProfileFormState): ProviderProfileCreateRequest {
  return {
    name: form.name.trim(),
    provider_type: form.provider_type,
    base_url: form.provider_type === "google_gemini" ? null : form.base_url.trim() || null,
    api_key: form.api_key.trim() || null,
    capabilities: form.capabilities,
    enabled: form.enabled,
  };
}

export function providerProfileUpdatePayload(form: ProviderProfileFormState): ProviderProfileUpdateRequest {
  return {
    name: form.name.trim(),
    provider_type: form.provider_type,
    base_url: form.provider_type === "google_gemini" ? null : form.base_url.trim() || null,
    api_key: form.api_key,
    capabilities: form.capabilities,
    enabled: form.enabled,
  };
}

function getBinding(
  data: ProviderConfigResponse | undefined,
  purpose: ProviderBinding["purpose"],
): ProviderBinding | undefined {
  return data?.bindings.find((binding) => binding.purpose === purpose);
}

function promptBindingDraft(binding: ProviderBinding | undefined): PromptBindingDraft {
  return {
    provider_kind: binding?.provider_kind === "openai" ? "openai" : "mock",
    provider_profile_id: binding?.provider_profile_id ?? "",
    model: textValue(binding?.model_settings, "model"),
  };
}

export function promptBindingPayloadFromDraft(draft: PromptBindingDraft): ProviderBindingUpdateRequest {
  return {
    provider_kind: draft.provider_kind,
    provider_profile_id: draft.provider_kind === "mock" ? null : draft.provider_profile_id,
    model_settings: draft.model.trim() ? { model: draft.model.trim() } : {},
    config: {},
  };
}

function agentBindingDraft(binding: ProviderBinding | undefined): AgentBindingDraft {
  return {
    provider_kind: binding?.provider_kind === "openai" ? "openai" : "mock",
    provider_profile_id: binding?.provider_profile_id ?? "",
    model: textValue(binding?.model_settings, "model"),
    reasoning_effort: textValue(binding?.config, "reasoning_effort"),
    reasoning_summary: textValue(binding?.config, "reasoning_summary"),
    text_verbosity: textValue(binding?.config, "text_verbosity"),
    service_tier: textValue(binding?.config, "service_tier"),
  };
}

export function agentBindingPayloadFromDraft(draft: AgentBindingDraft): ProviderBindingUpdateRequest {
  return {
    provider_kind: draft.provider_kind,
    provider_profile_id: draft.provider_kind === "mock" ? null : draft.provider_profile_id,
    model_settings: draft.model.trim() ? { model: draft.model.trim() } : {},
    config: Object.fromEntries(
      [
        ["reasoning_effort", draft.reasoning_effort],
        ["reasoning_summary", draft.reasoning_summary],
        ["text_verbosity", draft.text_verbosity],
        ["service_tier", draft.service_tier],
      ]
        .map(([key, value]) => [key, value.trim()])
        .filter(([, value]) => value),
    ),
  };
}

function imageBindingDraft(binding: ProviderBinding | undefined): ImageBindingDraft {
  const providerKind =
    binding?.provider_kind === "openai_responses" ||
    binding?.provider_kind === "openai_images" ||
    binding?.provider_kind === "google_gemini_image"
      ? binding.provider_kind
      : "mock";
  return {
    provider_kind: providerKind,
    provider_profile_id: binding?.provider_profile_id ?? "",
    model: textValue(binding?.model_settings, "model"),
    images_quality: textValue(binding?.config, "images_quality"),
    images_style: textValue(binding?.config, "images_style"),
    responses_background_enabled: boolValue(binding?.config, "responses_background_enabled", true),
    gemini_api_version: textValue(binding?.config, "gemini_api_version") || "v1beta",
    gemini_output_mime_type: textValue(binding?.config, "gemini_output_mime_type"),
  };
}

export function imageBindingPayloadFromDraft(draft: ImageBindingDraft): ProviderBindingUpdateRequest {
  const config =
    draft.provider_kind === "openai_responses"
      ? { responses_background_enabled: draft.responses_background_enabled }
      : draft.provider_kind === "openai_images"
        ? {
            ...(draft.images_quality.trim() ? { images_quality: draft.images_quality.trim() } : {}),
            ...(draft.images_style.trim() ? { images_style: draft.images_style.trim() } : {}),
          }
        : draft.provider_kind === "google_gemini_image"
          ? {
              gemini_api_version: draft.gemini_api_version || "v1beta",
              ...(draft.gemini_output_mime_type.trim()
                ? { gemini_output_mime_type: draft.gemini_output_mime_type.trim() }
                : {}),
            }
          : {};
  return {
    provider_kind: draft.provider_kind,
    provider_profile_id: draft.provider_kind === "mock" ? null : draft.provider_profile_id,
    model_settings: draft.model.trim() ? { model: draft.model.trim() } : {},
    config,
  };
}

export function itemsForSection(config: ConfigResponse | undefined, section: SettingsSectionId): ConfigItem[] {
  const items = config?.items ?? [];
  if (section === "prompts") {
    return items.filter((item) => item.category === "提示词");
  }
  if (section === "upload") {
    return items.filter((item) => item.category === "海报与上传" || item.category === "图片工具参数");
  }
  if (section === "queue") {
    return items.filter((item) => item.category === "生成队列");
  }
  if (section === "security") {
    return items.filter((item) => item.category === "安全与运维");
  }
  return [];
}

export function settingsExportFilename(exportedAt: string | null | undefined): string {
  if (!exportedAt) {
    return "productflow-settings.json";
  }
  const date = new Date(exportedAt);
  if (Number.isNaN(date.getTime())) {
    return "productflow-settings.json";
  }
  return `productflow-settings-${date.toISOString().slice(0, 19).replace("T", "-").replace(/:/g, "")}.json`;
}

export function settingsImportSummaryCounts(preview: SettingsImportPreviewResponse): {
  runtimeConfigCount: number;
  providerProfileCount: number;
  providerBindingCount: number;
  providerProfilesWithApiKeyCount: number;
} {
  return {
    runtimeConfigCount: preview.runtime_config_count,
    providerProfileCount: preview.provider_profile_count,
    providerBindingCount: preview.provider_binding_count,
    providerProfilesWithApiKeyCount: preview.provider_profiles_with_api_key_count,
  };
}

function downloadSettingsExport(payload: SettingsExportPayload): void {
  const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json" });
  const url = window.URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = settingsExportFilename(payload.metadata.exported_at);
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  window.URL.revokeObjectURL(url);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isSettingsExportPayload(value: unknown): value is SettingsExportPayload {
  if (!isRecord(value)) {
    return false;
  }
  return (
    isRecord(value.metadata) &&
    isRecord(value.runtime_config) &&
    Array.isArray(value.provider_profiles) &&
    Array.isArray(value.provider_bindings)
  );
}

interface SettingsMigrationPanelProps {
  importInputRef: RefObject<HTMLInputElement | null>;
  importFileName: string;
  importPreview: SettingsImportPreviewResponse | null;
  exportBusy: boolean;
  importPreviewBusy: boolean;
  importCommitBusy: boolean;
  onRequestExport: () => void;
  onChooseImportFile: () => void;
  onImportFileChange: (event: ChangeEvent<HTMLInputElement>) => void;
  onCommitImport: () => void;
  onCancelImport: () => void;
}

function SettingsMigrationPanel({
  importInputRef,
  importFileName,
  importPreview,
  exportBusy,
  importPreviewBusy,
  importCommitBusy,
  onRequestExport,
  onChooseImportFile,
  onImportFileChange,
  onCommitImport,
  onCancelImport,
}: SettingsMigrationPanelProps) {
  const { t } = useI18n();
  const counts = importPreview ? settingsImportSummaryCounts(importPreview) : null;
  return (
    <section className={`${PANEL_CLASS} mb-8`}>
      <div className="flex flex-col gap-5 lg:flex-row lg:items-start lg:justify-between">
        <div className="max-w-2xl">
          <div className="inline-flex items-center rounded-full border border-amber-200 bg-amber-50 px-3 py-1 text-xs font-semibold text-amber-700 dark:border-amber-400/35 dark:bg-amber-500/12 dark:text-amber-200">
            <KeyRound size={13} className="mr-1.5" />
            {t("settings.migration.sensitiveLabel")}
          </div>
          <h2 className="mt-3 text-lg font-semibold text-slate-950 dark:text-white">
            {t("settings.migration.title")}
          </h2>
          <p className="mt-2 text-sm leading-6 text-slate-500 dark:text-slate-400">
            {t("settings.migration.description")}
          </p>
          <p className="mt-3 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800 dark:border-amber-400/35 dark:bg-amber-500/10 dark:text-amber-100">
            {t("settings.migration.sensitiveWarning")}
          </p>
        </div>
        <div className="flex shrink-0 flex-col gap-2 sm:flex-row lg:flex-col">
          <button
            type="button"
            onClick={onRequestExport}
            disabled={exportBusy}
            className={SETTINGS_MAIN_ACTION_CLASS}
          >
            {exportBusy ? <Loader2 size={14} className="mr-2 animate-spin" /> : <Download size={14} className="mr-2" />}
            {t("settings.migration.export")}
          </button>
          <button
            type="button"
            onClick={onChooseImportFile}
            disabled={importPreviewBusy || importCommitBusy}
            className="inline-flex h-11 items-center justify-center rounded-lg border border-slate-200 bg-white px-5 text-sm font-semibold text-slate-700 shadow-sm hover:bg-slate-50 disabled:opacity-50 dark:border-slate-700 dark:bg-[#111b2d] dark:text-slate-100 dark:hover:bg-slate-800"
          >
            {importPreviewBusy ? (
              <Loader2 size={14} className="mr-2 animate-spin" />
            ) : (
              <UploadCloud size={14} className="mr-2" />
            )}
            {t("settings.migration.import")}
          </button>
          <input
            ref={importInputRef}
            type="file"
            accept="application/json,.json"
            className="hidden"
            onChange={onImportFileChange}
          />
        </div>
      </div>

      {importPreview && counts ? (
        <div className="mt-5 rounded-xl border border-indigo-200 bg-indigo-50/70 p-4 dark:border-violet-400/35 dark:bg-violet-500/10">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div>
              <div className="flex items-center text-sm font-semibold text-indigo-800 dark:text-violet-100">
                <FileJson size={15} className="mr-2" />
                {t("settings.migration.previewTitle")}
              </div>
              <p className="mt-1 text-xs text-indigo-700/80 dark:text-violet-100/75">
                {t("settings.migration.previewFile", { file: importFileName })}
              </p>
            </div>
            <div className="flex gap-2">
              <button
                type="button"
                onClick={onCancelImport}
                disabled={importCommitBusy}
                className="h-9 rounded-lg px-3 text-sm font-medium text-slate-600 hover:bg-white/70 disabled:opacity-50 dark:text-slate-200 dark:hover:bg-white/10"
              >
                {t("common.cancel")}
              </button>
              <button
                type="button"
                onClick={onCommitImport}
                disabled={importCommitBusy}
                className={SETTINGS_MAIN_ACTION_CLASS}
              >
                {importCommitBusy ? <Loader2 size={14} className="mr-2 animate-spin" /> : <Check size={14} className="mr-2" />}
                {t("settings.migration.commitImport")}
              </button>
            </div>
          </div>
          <div className="mt-4 grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
            <div className="rounded-lg bg-white px-3 py-2 text-xs text-slate-600 shadow-sm dark:bg-[#101827] dark:text-slate-300">
              {t("settings.migration.runtimeCount", { count: counts.runtimeConfigCount })}
            </div>
            <div className="rounded-lg bg-white px-3 py-2 text-xs text-slate-600 shadow-sm dark:bg-[#101827] dark:text-slate-300">
              {t("settings.migration.profileCount", { count: counts.providerProfileCount })}
            </div>
            <div className="rounded-lg bg-white px-3 py-2 text-xs text-slate-600 shadow-sm dark:bg-[#101827] dark:text-slate-300">
              {t("settings.migration.bindingCount", { count: counts.providerBindingCount })}
            </div>
            <div className="rounded-lg bg-white px-3 py-2 text-xs text-slate-600 shadow-sm dark:bg-[#101827] dark:text-slate-300">
              {t("settings.migration.keyCount", { count: counts.providerProfilesWithApiKeyCount })}
            </div>
          </div>
        </div>
      ) : null}
    </section>
  );
}

interface SettingsFormFieldProps {
  label: string;
  children: ReactNode;
  className?: string;
}

function SettingsFormField({ label, children, className = "" }: SettingsFormFieldProps) {
  return (
    <label className={`block space-y-2 ${className}`}>
      <span className="text-xs font-medium text-slate-600 dark:text-slate-300">{label}</span>
      {children}
    </label>
  );
}

interface ConfigFieldProps {
  item: ConfigItem;
  value: DraftValue;
  secretTouched: boolean;
  isResetting: boolean;
  layout?: "row" | "card";
  onChange: (value: DraftValue, touchedSecret?: boolean) => void;
  onReset: () => void;
}

function ConfigField({
  item,
  value,
  secretTouched,
  isResetting,
  layout = "row",
  onChange,
  onReset,
}: ConfigFieldProps) {
  const { t } = useI18n();
  const selectedMultiValues = Array.isArray(value) ? value : [];
  const toggleMultiValue = (optionValue: string) => {
    const selected = new Set(selectedMultiValues);
    if (selected.has(optionValue)) {
      selected.delete(optionValue);
    } else {
      selected.add(optionValue);
    }
    onChange(item.options.filter((option) => selected.has(option.value)).map((option) => option.value));
  };

  const control =
    item.input_type === "multi_select" ? (
      <div className="grid gap-2 sm:grid-cols-2">
        {item.options.map((option) => (
          <label
            key={`${item.key}-${option.value}`}
            className="inline-flex items-center gap-2 rounded-lg border border-slate-200 bg-white px-2.5 py-2 text-xs font-medium text-slate-700 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300"
          >
            <input
              type="checkbox"
              checked={selectedMultiValues.includes(option.value)}
              onChange={() => toggleMultiValue(option.value)}
              className="h-3.5 w-3.5 accent-indigo-600"
            />
            <span>{option.label}</span>
          </label>
        ))}
      </div>
    ) : item.input_type === "select" ? (
      <SelectField id={item.key} value={String(value)} options={item.options} onChange={onChange} />
    ) : item.input_type === "textarea" ? (
      <textarea
        id={item.key}
        value={String(value)}
        onChange={(event) => onChange(event.target.value)}
        rows={item.key.startsWith("prompt_") ? 8 : 3}
        className={`${TEXTAREA_CLASS} resize-y leading-6`}
      />
    ) : item.input_type === "boolean" ? (
      <label className="inline-flex cursor-pointer items-center gap-3 rounded-lg border border-zinc-200 bg-white px-3 py-2 text-sm text-zinc-700 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-300">
        <input
          id={item.key}
          type="checkbox"
          checked={Boolean(value)}
          onChange={(event) => onChange(event.target.checked)}
          className="h-4 w-4 accent-zinc-900"
        />
        <span>{Boolean(value) ? t("settings.enabled") : t("settings.disabled")}</span>
      </label>
    ) : (
      <input
        id={item.key}
        type={item.input_type === "password" ? "password" : item.input_type === "number" ? "number" : "text"}
        value={String(value)}
        min={item.minimum ?? undefined}
        max={item.maximum ?? undefined}
        placeholder={item.secret && item.has_value ? t("settings.secretPlaceholder") : item.description || undefined}
        onChange={(event) => onChange(event.target.value, item.secret)}
        className={INPUT_CLASS}
        autoComplete={item.secret ? "new-password" : undefined}
      />
    );

  if (layout === "card") {
    return (
      <div className="rounded-xl border border-slate-200 bg-white p-3 shadow-sm shadow-slate-200/50 dark:border-slate-800 dark:bg-[#0f1726] dark:shadow-black/20">
        <div className="flex items-start justify-between gap-3">
          <label htmlFor={item.key} className="min-w-0 text-sm font-semibold text-zinc-950 dark:text-white">
            {item.label}
          </label>
          <span className={`shrink-0 rounded-full border px-2 py-0.5 text-[11px] font-medium ${sourceClassName(item)}`}>
            {sourceLabel(item, t)}
          </span>
        </div>
        <div className="mt-3">{control}</div>
        <div className="mt-2 flex min-h-5 items-start justify-between gap-3">
          <div>
            <div className="font-mono text-[11px] text-zinc-400 dark:text-slate-500">{item.key}</div>
            {item.secret && secretTouched ? (
              <div className="mt-1 text-xs text-amber-600 dark:text-amber-300">{t("settings.writeNewSecret")}</div>
            ) : null}
          </div>
          {item.source === "database" ? (
            <button
              type="button"
              onClick={onReset}
              disabled={isResetting}
              className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-zinc-400 hover:bg-slate-100 hover:text-zinc-900 disabled:opacity-50 dark:text-slate-500 dark:hover:bg-slate-800 dark:hover:text-white"
              aria-label={t("settings.restoreDefault")}
              title={t("settings.restoreDefault")}
            >
              {isResetting ? <Loader2 size={13} className="animate-spin" /> : <RotateCcw size={13} />}
            </button>
          ) : null}
        </div>
      </div>
    );
  }

  return (
    <div className="grid gap-3 border-t border-slate-100 py-5 first:border-t-0 dark:border-slate-800 md:grid-cols-[220px_minmax(0,1fr)]">
      <div>
        <div className="flex flex-wrap items-center gap-2">
          <label htmlFor={item.key} className="text-sm font-medium text-zinc-900 dark:text-white">
            {item.label}
          </label>
          <span className={`rounded-full border px-2 py-0.5 text-[11px] font-medium ${sourceClassName(item)}`}>
            {sourceLabel(item, t)}
          </span>
        </div>
        <div className="mt-1 font-mono text-[11px] text-zinc-400 dark:text-slate-500">{item.key}</div>
      </div>
      <div className="space-y-2">
        {control}
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="min-h-4 text-xs leading-5 text-zinc-500 dark:text-slate-400">
            {item.description}
            {item.secret && secretTouched ? (
              <span className="ml-2 text-amber-600 dark:text-amber-300">{t("settings.writeNewSecret")}</span>
            ) : null}
          </p>
          {item.source === "database" ? (
            <button
              type="button"
              onClick={onReset}
              disabled={isResetting}
              className="inline-flex items-center text-xs font-medium text-zinc-500 hover:text-zinc-900 disabled:opacity-50 dark:text-slate-400 dark:hover:text-white"
            >
              {isResetting ? <Loader2 size={13} className="mr-1 animate-spin" /> : <RotateCcw size={13} className="mr-1" />}
              {t("settings.restoreDefault")}
            </button>
          ) : null}
        </div>
      </div>
    </div>
  );
}

interface ProvidersSectionProps {
  data: ProviderConfigResponse | undefined;
  profileForm: ProviderProfileFormState;
  editingProfileId: string | null;
  drawerOpen: boolean;
  pending: boolean;
  togglingProfileId: string | null;
  onProfileFormChange: (next: ProviderProfileFormState) => void;
  onOpenCreate: () => void;
  onEditProfile: (profile: ProviderProfile) => void;
  onCloseDrawer: () => void;
  onSubmitProfile: () => void;
  onDeleteProfile: (profile: ProviderProfile) => void;
  onToggleProfileEnabled: (profileId: string, enabled: boolean) => void;
}

function ProvidersSection({
  data,
  profileForm,
  editingProfileId,
  drawerOpen,
  pending,
  togglingProfileId,
  onProfileFormChange,
  onOpenCreate,
  onEditProfile,
  onCloseDrawer,
  onSubmitProfile,
  onDeleteProfile,
  onToggleProfileEnabled,
}: ProvidersSectionProps) {
  const { t } = useI18n();
  const profiles = (data?.profiles ?? []).filter((profile) => !profile.archived_at);
  const editingProfile = editingProfileId
    ? profiles.find((profile) => profile.id === editingProfileId)
    : undefined;
  const editingProfileUsage = editingProfile
    ? providerUsageFromBindings(data?.bindings ?? [], editingProfile.id)
    : undefined;
  const editingProfileDisableBlocked =
    editingProfile && editingProfileUsage ? providerDisableBlocked(editingProfile, editingProfileUsage) : false;

  return (
    <div className="space-y-5">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-base font-semibold text-slate-950 dark:text-white">
            {t("settings.provider.listTitle")}
          </h2>
          <p className="mt-1 text-sm leading-6 text-slate-500 dark:text-slate-400">
            {t("settings.provider.listDescription")}
          </p>
        </div>
        <button type="button" onClick={onOpenCreate} className={SETTINGS_MAIN_ACTION_CLASS}>
          <Plus size={14} className="mr-2" />
          {t("settings.provider.create")}
        </button>
      </div>

      {profiles.length ? (
        <div className="grid gap-4 lg:grid-cols-2">
          {profiles.map((profile) => {
            const usage = providerUsageFromBindings(data?.bindings ?? [], profile.id);
            return (
              <ProviderProfileCard
                key={profile.id}
                profile={profile}
                usage={usage}
                pending={pending}
                toggling={togglingProfileId === profile.id}
                onEdit={() => onEditProfile(profile)}
                onDelete={() => onDeleteProfile(profile)}
                onToggleEnabled={(enabled) => onToggleProfileEnabled(profile.id, enabled)}
              />
            );
          })}
        </div>
      ) : (
        <div className="flex min-h-[320px] flex-col items-center justify-center rounded-xl border border-dashed border-slate-300 bg-white px-6 text-center shadow-sm shadow-slate-200/60 dark:border-slate-700 dark:bg-[#0f1726] dark:shadow-black/25">
          <Box size={42} className="text-slate-500" />
          <div className="mt-5 text-base font-semibold text-slate-950 dark:text-white">
            {t("settings.provider.emptyTitle")}
          </div>
          <p className="mt-3 max-w-sm text-sm leading-6 text-slate-500 dark:text-slate-400">
            {t("settings.provider.emptyDescription")}
          </p>
          <button type="button" onClick={onOpenCreate} className={`${SETTINGS_MAIN_ACTION_CLASS} mt-6`}>
            <Plus size={14} className="mr-2" />
            {t("settings.provider.create")}
          </button>
        </div>
      )}

      <ProviderProfileDrawer
        open={drawerOpen}
        form={profileForm}
        editingProfileId={editingProfileId}
        pending={pending}
        enableToggleBlocked={editingProfileDisableBlocked}
        onFormChange={onProfileFormChange}
        onClose={onCloseDrawer}
        onSubmit={onSubmitProfile}
      />
    </div>
  );
}

interface ProviderEnabledSwitchProps {
  checked: boolean;
  disabled: boolean;
  loading?: boolean;
  title?: string;
  ariaLabel: string;
  describedBy?: string;
  onToggle: (checked: boolean) => void;
}

function ProviderEnabledSwitch({
  checked,
  disabled,
  loading = false,
  title,
  ariaLabel,
  describedBy,
  onToggle,
}: ProviderEnabledSwitchProps) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={ariaLabel}
      aria-describedby={describedBy}
      title={title}
      disabled={disabled}
      onClick={(event) => {
        event.stopPropagation();
        onToggle(!checked);
      }}
      onKeyDown={(event) => event.stopPropagation()}
      className={`relative inline-flex h-6 w-11 shrink-0 items-center rounded-full border transition ${
        checked
          ? "border-indigo-500 bg-indigo-600 dark:border-violet-400 dark:bg-violet-500"
          : "border-slate-300 bg-slate-200 dark:border-slate-700 dark:bg-slate-800"
      } ${disabled ? "cursor-not-allowed opacity-55" : "hover:brightness-105"}`}
    >
      <span
        className={`inline-flex h-5 w-5 items-center justify-center rounded-full bg-white text-slate-500 shadow-sm transition ${
          checked ? "translate-x-5" : "translate-x-0.5"
        }`}
      >
        {loading ? <Loader2 size={12} className="animate-spin" /> : null}
      </span>
    </button>
  );
}

interface ProviderProfileCardProps {
  profile: ProviderProfile;
  usage: ProviderProfileUsage;
  pending: boolean;
  toggling: boolean;
  onEdit: () => void;
  onDelete: () => void;
  onToggleEnabled: (enabled: boolean) => void;
}

function ProviderProfileCard({
  profile,
  usage,
  pending,
  toggling,
  onEdit,
  onDelete,
  onToggleEnabled,
}: ProviderProfileCardProps) {
  const { t } = useI18n();
  const usageLabelKeys = providerUsageLabelKeys(usage);
  const disableBlocked = providerDisableBlocked(profile, usage);
  const blockHelpId = `${profile.id}-disable-help`;
  const switchHelp = disableBlocked ? t("settings.provider.disableBlocked") : undefined;

  return (
    <div className="group relative flex min-h-[230px] flex-col justify-between rounded-xl border border-slate-200 bg-white p-5 shadow-sm shadow-slate-200/60 transition hover:border-indigo-200 hover:shadow-md dark:border-slate-800 dark:bg-[#0f1726] dark:shadow-black/25 dark:hover:border-violet-400/45">
      <button
        type="button"
        onClick={onEdit}
        className="-m-2 block w-full space-y-4 rounded-lg p-2 text-left outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:focus-visible:ring-violet-400"
      >
        <span className="flex items-start justify-between gap-4">
          <span className="min-w-0 pr-20">
            <span className="flex flex-wrap items-center gap-2">
              <span className="truncate text-base font-semibold text-slate-950 dark:text-white">{profile.name}</span>
              <span
                className={`rounded-full border px-2 py-0.5 text-[11px] font-medium ${
                  profile.enabled
                    ? "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-400/35 dark:bg-emerald-500/12 dark:text-emerald-200"
                    : "border-slate-200 bg-slate-50 text-slate-500 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300"
                }`}
              >
                {profile.enabled ? t("settings.provider.enabled") : t("settings.provider.disabled")}
              </span>
            </span>
            <span className="mt-2 flex items-center gap-1.5 truncate font-mono text-xs text-slate-500 dark:text-slate-400">
              <ServerCog size={13} className="shrink-0" />
              <span className="truncate">{profile.base_url || t(providerDefaultEndpointLabelKey(profile))}</span>
            </span>
          </span>
        </span>

        <span className="flex flex-wrap gap-1.5">
          <span className="rounded-md bg-indigo-50 px-2 py-1 text-[11px] font-medium text-indigo-700 dark:bg-violet-500/12 dark:text-violet-100">
            {t(providerTypeLabelKey(profile.provider_type))}
          </span>
          {profile.capabilities.map((capability) => (
            <span
              key={`${profile.id}-${capability}`}
              className="rounded-md bg-slate-100 px-2 py-1 text-[11px] font-medium text-slate-600 dark:bg-slate-800 dark:text-slate-300"
            >
              {t(providerCapabilityLabelKey(capability))}
            </span>
          ))}
        </span>

        <span className="flex flex-wrap gap-2">
          <span
            className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-[11px] font-medium ${
              profile.has_api_key
                ? "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-400/35 dark:bg-emerald-500/12 dark:text-emerald-200"
                : "border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-400/35 dark:bg-amber-500/12 dark:text-amber-200"
            }`}
          >
            <KeyRound size={12} />
            {profile.has_api_key ? t("settings.provider.keyConfigured") : t("settings.provider.keyMissing")}
          </span>
          {usageLabelKeys.length ? (
            usageLabelKeys.map((labelKey) => (
              <span
                key={labelKey}
                className="rounded-full border border-indigo-200 bg-indigo-50 px-2.5 py-1 text-[11px] font-medium text-indigo-700 dark:border-violet-400/35 dark:bg-violet-500/12 dark:text-violet-100"
              >
                {t(labelKey)}
              </span>
            ))
          ) : (
            <span className="rounded-full border border-slate-200 bg-slate-50 px-2.5 py-1 text-[11px] font-medium text-slate-500 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300">
              {t("settings.provider.usageNone")}
            </span>
          )}
        </span>
      </button>

      <div className="absolute right-5 top-5 flex shrink-0 items-center gap-2">
        <button
          type="button"
          onClick={onEdit}
          className="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-slate-200 bg-white text-slate-500 hover:border-indigo-200 hover:text-indigo-700 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-400 dark:hover:border-violet-300/50 dark:hover:text-violet-100"
          aria-label={t("settings.provider.editAria")}
          title={t("settings.provider.edit")}
        >
          <Pencil size={14} />
        </button>
        <button
          type="button"
          onClick={onDelete}
          disabled={pending}
          className="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-slate-200 bg-white text-slate-500 hover:border-red-200 hover:text-red-600 disabled:opacity-50 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-400 dark:hover:border-red-300/50 dark:hover:text-red-200"
          aria-label={t("settings.provider.deleteAria")}
          title={t("settings.provider.deleteAria")}
        >
          <Trash2 size={14} />
        </button>
      </div>

      <div className="mt-5 flex items-start justify-between gap-4 border-t border-slate-100 pt-4 dark:border-slate-800">
        <div>
          <div className="text-xs font-semibold text-slate-700 dark:text-slate-200">
            {t("settings.provider.enabledSwitchLabel")}
          </div>
          {switchHelp ? (
            <p id={blockHelpId} className="mt-1 text-xs leading-5 text-amber-700 dark:text-amber-200">
              {switchHelp}
            </p>
          ) : (
            <p className="mt-1 text-xs leading-5 text-slate-500 dark:text-slate-400">
              {t("settings.provider.enabledSwitchHelp")}
            </p>
          )}
        </div>
        <ProviderEnabledSwitch
          checked={profile.enabled}
          disabled={pending || toggling || disableBlocked}
          loading={toggling}
          title={switchHelp}
          ariaLabel={t("settings.provider.enabledSwitchAria")}
          describedBy={switchHelp ? blockHelpId : undefined}
          onToggle={onToggleEnabled}
        />
      </div>
    </div>
  );
}

interface ProviderCapabilityToggleProps {
  option: (typeof PROVIDER_CAPABILITY_OPTIONS)[number];
  selected: boolean;
  onToggle: () => void;
}

function ProviderCapabilityToggle({ option, selected, onToggle }: ProviderCapabilityToggleProps) {
  const { t } = useI18n();
  return (
    <button
      type="button"
      aria-pressed={selected}
      onClick={onToggle}
      className={`flex h-[46px] items-center gap-3 rounded-xl border px-3 text-left text-sm font-semibold transition ${
        selected
          ? "border-indigo-500 bg-indigo-50 text-indigo-700 dark:border-violet-500 dark:bg-violet-500/12 dark:text-violet-50"
          : "border-slate-200 bg-slate-50 text-slate-600 hover:border-slate-300 dark:border-slate-700 dark:bg-[#171f30] dark:text-slate-300 dark:hover:border-slate-500"
      }`}
    >
      <span
        className={`grid h-5 w-5 shrink-0 place-items-center rounded-[5px] transition ${
          selected
            ? "bg-indigo-600 text-white dark:bg-violet-500"
            : "bg-slate-200 dark:bg-slate-600"
        }`}
      >
        {selected ? <Check size={13} strokeWidth={3} /> : null}
      </span>
      {t(option.labelKey)}
    </button>
  );
}

interface ProviderDrawerTextInputProps {
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
  type?: "text" | "password";
  icon?: ReactNode;
  autoComplete?: string;
}

function ProviderDrawerTextInput({
  value,
  onChange,
  placeholder,
  type = "text",
  icon,
  autoComplete,
}: ProviderDrawerTextInputProps) {
  return (
    <div className="relative">
      {icon ? (
        <span className="pointer-events-none absolute left-4 top-1/2 -translate-y-1/2 text-slate-400 dark:text-slate-500">
          {icon}
        </span>
      ) : null}
      <input
        type={type}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className={`${PROVIDER_DRAWER_INPUT_CLASS} ${icon ? "pl-11" : ""}`}
        placeholder={placeholder}
        autoComplete={autoComplete}
      />
    </div>
  );
}

interface ProviderDrawerEnableToggleProps {
  checked: boolean;
  disabled: boolean;
  blocked?: boolean;
  onToggle: (checked: boolean) => void;
}

function ProviderDrawerEnableToggle({ checked, disabled, blocked = false, onToggle }: ProviderDrawerEnableToggleProps) {
  const { t } = useI18n();
  const helpId = useId();
  return (
    <div>
      <button
        type="button"
        disabled={disabled || blocked}
        aria-pressed={checked}
        aria-describedby={blocked ? helpId : undefined}
        onClick={() => onToggle(!checked)}
        className={`flex h-[46px] w-full items-center gap-3 rounded-xl border px-3 text-left text-sm font-semibold transition ${
          checked
            ? "border-indigo-300 bg-indigo-50 text-slate-900 dark:border-slate-700 dark:bg-[#171f30] dark:text-slate-100"
            : "border-slate-200 bg-slate-50 text-slate-600 dark:border-slate-700 dark:bg-[#171f30] dark:text-slate-300"
        } ${
          disabled || blocked
            ? "cursor-not-allowed opacity-60"
            : "hover:border-indigo-300 dark:hover:border-violet-500/60"
        }`}
      >
        <span
          className={`grid h-5 w-5 shrink-0 place-items-center rounded-md transition ${
            checked ? "bg-indigo-600 text-white dark:bg-violet-500" : "bg-slate-200 dark:bg-slate-600"
          }`}
        >
          {checked ? <Check size={13} strokeWidth={3} /> : null}
        </span>
        {t("settings.provider.enable")}
      </button>
      {blocked ? (
        <p id={helpId} className="mt-2 text-xs leading-5 text-amber-700 dark:text-amber-200">
          {t("settings.provider.disableBlocked")}
        </p>
      ) : null}
    </div>
  );
}

interface ProviderProfileDrawerProps {
  open: boolean;
  form: ProviderProfileFormState;
  editingProfileId: string | null;
  pending: boolean;
  enableToggleBlocked: boolean;
  onFormChange: (next: ProviderProfileFormState) => void;
  onClose: () => void;
  onSubmit: () => void;
}

function ProviderProfileDrawer({
  open,
  form,
  editingProfileId,
  pending,
  enableToggleBlocked,
  onFormChange,
  onClose,
  onSubmit,
}: ProviderProfileDrawerProps) {
  const { t } = useI18n();
  const titleId = useId();
  const openerRef = useRef<HTMLElement | null>(null);
  const capabilityValues = providerCapabilityValuesForType(form.provider_type);
  const capabilityOptions = PROVIDER_CAPABILITY_OPTIONS.filter((option) => capabilityValues.includes(option.value));
  const handleProviderTypeChange = (provider_type: ProviderType) => {
    onFormChange({
      ...form,
      provider_type,
      base_url: provider_type === "google_gemini" ? "" : form.base_url,
      capabilities: defaultCapabilitiesForProviderType(provider_type),
    });
  };
  const toggleCapability = (capability: ProviderCapability) => {
    const selected = new Set(form.capabilities);
    if (selected.has(capability)) {
      selected.delete(capability);
    } else {
      selected.add(capability);
    }
    onFormChange({
      ...form,
      capabilities: capabilityOptions.map((option) => option.value).filter((value) => selected.has(value)),
    });
  };

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => !nextOpen && onClose()}>
      <DialogContent
        placement="right"
        hideClose
        aria-labelledby={titleId}
        className="flex flex-col"
        bodyClassName="contents"
        onOpenAutoFocus={() => {
          openerRef.current = document.activeElement instanceof HTMLElement
            ? document.activeElement
            : null;
        }}
        onCloseAutoFocus={(event) => {
          event.preventDefault();
          openerRef.current?.focus();
        }}
      >
        <div className="flex h-[74px] items-center justify-between border-b border-slate-200 px-6 dark:border-slate-800">
          <div className="flex min-w-0 items-center gap-3">
            <span className="text-indigo-600 dark:text-violet-400">
              {editingProfileId ? <Pencil size={17} /> : <Plus size={18} />}
            </span>
            <DialogTitle id={titleId} className="truncate text-lg font-bold text-slate-950 dark:text-white">
              {editingProfileId ? t("settings.provider.edit") : t("settings.provider.create")}
            </DialogTitle>
          </div>
          <DialogClose asChild>
            <button
              type="button"
              onClick={onClose}
              className="inline-flex h-11 w-11 shrink-0 items-center justify-center rounded-lg border border-slate-200 text-slate-500 transition hover:border-slate-300 hover:text-slate-950 dark:border-slate-700 dark:text-slate-400 dark:hover:border-slate-500 dark:hover:text-white lg:h-9 lg:w-9"
              aria-label={t("settings.provider.closeDrawer")}
              title={t("settings.provider.closeDrawer")}
            >
              <X size={16} />
            </button>
          </DialogClose>
        </div>

        <form
          className="flex min-h-0 flex-1 flex-col"
          onSubmit={(event) => {
            event.preventDefault();
            onSubmit();
          }}
        >
          <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-6 py-6">
            <div className="text-xs font-semibold text-slate-500 dark:text-slate-400">
              {t("settings.provider.basicInfo")}
            </div>
            <SettingsFormField label={t("settings.provider.nameLabel")}>
              <ProviderDrawerTextInput
                value={form.name}
                onChange={(name) => onFormChange({ ...form, name })}
                placeholder={t("settings.provider.namePlaceholder")}
              />
            </SettingsFormField>
            <SettingsFormField label={t("settings.provider.typeLabel")}>
              <SelectField
                value={form.provider_type}
                options={[
                  { value: "openai_compatible", label: t("settings.provider.type.openaiCompatible") },
                  { value: "google_gemini", label: t("settings.provider.type.googleGemini") },
                ]}
                onChange={(value) =>
                  handleProviderTypeChange(value === "google_gemini" ? "google_gemini" : "openai_compatible")
                }
              />
            </SettingsFormField>
            {form.provider_type === "openai_compatible" ? (
              <SettingsFormField label={t("settings.provider.baseUrlLabel")}>
                <ProviderDrawerTextInput
                  value={form.base_url}
                  onChange={(base_url) => onFormChange({ ...form, base_url })}
                  placeholder={t("settings.provider.baseUrlPlaceholder")}
                  icon={<Link2 size={16} />}
                />
              </SettingsFormField>
            ) : (
              <div className="rounded-xl border border-slate-200 bg-slate-50 px-4 py-3 text-xs leading-5 text-slate-600 dark:border-slate-700 dark:bg-[#171f30] dark:text-slate-300">
                {t("settings.provider.googleBaseUrlUnsupported")}
              </div>
            )}
            <SettingsFormField label={t("settings.provider.apiKeyLabel")}>
              <ProviderDrawerTextInput
                type="password"
                value={form.api_key}
                onChange={(api_key) => onFormChange({ ...form, api_key })}
                placeholder={
                  editingProfileId
                    ? t("settings.provider.keepKeyPlaceholder")
                    : t("settings.provider.apiKeyPlaceholder")
                }
                icon={<KeyRound size={16} />}
                autoComplete="new-password"
              />
            </SettingsFormField>
            <div className="grid gap-2">
              <div className="text-xs font-medium text-slate-600 dark:text-slate-300">
                {t("settings.provider.capabilitiesLabel")}
              </div>
              <div className="grid grid-cols-2 gap-3">
                {capabilityOptions.map((option) => (
                  <ProviderCapabilityToggle
                    key={option.value}
                    option={option}
                    selected={form.capabilities.includes(option.value)}
                    onToggle={() => toggleCapability(option.value)}
                  />
                ))}
              </div>
            </div>

            <div className="border-t border-slate-200 pt-4 dark:border-slate-800">
              <ProviderDrawerEnableToggle
                checked={form.enabled}
                disabled={pending}
                blocked={enableToggleBlocked}
                onToggle={(enabled) => onFormChange({ ...form, enabled })}
              />
            </div>
          </div>

          <div className="shrink-0 border-t border-slate-200 bg-white px-6 py-5 dark:border-slate-800 dark:bg-[#121722]">
            <button
              type="submit"
              disabled={pending || !form.name.trim() || !form.capabilities.length}
              className="inline-flex h-12 w-full items-center justify-center rounded-xl bg-indigo-600 px-5 text-sm font-bold text-white shadow-lg shadow-indigo-500/25 transition hover:bg-indigo-500 disabled:opacity-50 dark:bg-violet-500 dark:shadow-violet-950/30 dark:hover:bg-violet-400"
            >
              {pending ? <Loader2 size={14} className="mr-2 animate-spin" /> : <Save size={14} className="mr-2" />}
              {t("detail.save")}
            </button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

interface PromptBindingSectionProps {
  data: ProviderConfigResponse | undefined;
  draft: PromptBindingDraft;
  pending: boolean;
  onChange: (next: PromptBindingDraft) => void;
  onSave: () => void;
}

function PromptBindingSection({ data, draft, pending, onChange, onSave }: PromptBindingSectionProps) {
  const { t } = useI18n();
  const profiles = (data?.profiles ?? []).filter(
    (profile) => profile.enabled && !profile.archived_at && profile.capabilities.includes("text_responses"),
  );
  return (
    <div className={`${PANEL_CLASS} max-w-3xl space-y-5`}>
      <SettingsFormField label={t("settings.provider.apiInterfaceLabel")}>
        <SelectField
          value={draft.provider_kind}
          options={[
            { value: "mock", label: t("settings.provider.interface.mock") },
            { value: "openai", label: t("settings.provider.interface.openaiResponses") },
          ]}
          onChange={(value) => onChange({ ...draft, provider_kind: value === "openai" ? "openai" : "mock" })}
        />
      </SettingsFormField>
      {draft.provider_kind !== "mock" ? (
        <SettingsFormField label={t("settings.provider.compatibleProviderLabel")}>
          <SelectField
            value={draft.provider_profile_id}
            options={[
              { value: "", label: t("settings.provider.selectProfile") },
              ...profiles.map((profile) => ({ value: profile.id, label: profile.name })),
            ]}
            onChange={(value) => onChange({ ...draft, provider_profile_id: value })}
          />
        </SettingsFormField>
      ) : null}
      <SettingsFormField label={t("settings.provider.promptModelLabel")}>
        <input
          value={draft.model}
          onChange={(event) => onChange({ ...draft, model: event.target.value })}
          className={INPUT_CLASS}
          placeholder={t("settings.provider.promptModelPlaceholder")}
        />
      </SettingsFormField>
      <div className="flex justify-end border-t border-slate-100 pt-5 dark:border-slate-800">
        <button
          type="button"
          onClick={onSave}
          disabled={
            pending ||
            !draft.model.trim() ||
            (draft.provider_kind !== "mock" && !draft.provider_profile_id)
          }
          className={SETTINGS_MAIN_ACTION_CLASS}
        >
          {pending ? <Loader2 size={14} className="mr-2 animate-spin" /> : <Save size={14} className="mr-2" />}
          {t("settings.provider.savePrompt")}
        </button>
      </div>
    </div>
  );
}

interface AgentBindingSectionProps {
  data: ProviderConfigResponse | undefined;
  draft: AgentBindingDraft;
  pending: boolean;
  onChange: (next: AgentBindingDraft) => void;
  onSave: () => void;
}

function AgentBindingSection({ data, draft, pending, onChange, onSave }: AgentBindingSectionProps) {
  const { t } = useI18n();
  const profiles = (data?.profiles ?? []).filter(
    (profile) => profile.enabled && !profile.archived_at && profile.capabilities.includes("text_responses"),
  );
  return (
    <div className={`${PANEL_CLASS} max-w-3xl space-y-5`}>
      <SettingsFormField label={t("settings.provider.apiInterfaceLabel")}>
        <SelectField
          value={draft.provider_kind}
          options={[
            { value: "mock", label: t("settings.provider.interface.agentDisabled") },
            { value: "openai", label: t("settings.provider.interface.openaiResponses") },
          ]}
          onChange={(value) =>
            onChange({
              ...draft,
              provider_kind: value === "openai" ? "openai" : "mock",
              provider_profile_id: value === "openai" ? draft.provider_profile_id : "",
            })
          }
        />
      </SettingsFormField>
      {draft.provider_kind === "openai" ? (
        <SettingsFormField label={t("settings.provider.providerProfileLabel")}>
          <SelectField
            value={draft.provider_profile_id}
            options={[
              { value: "", label: t("settings.provider.selectProfile") },
              ...profiles.map((profile) => ({ value: profile.id, label: profile.name })),
            ]}
            onChange={(value) => onChange({ ...draft, provider_profile_id: value })}
          />
        </SettingsFormField>
      ) : (
        <div className="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm leading-6 text-amber-800 dark:border-amber-400/35 dark:bg-amber-500/10 dark:text-amber-100">
          {t("settings.provider.agentDisabledDescription")}
        </div>
      )}
      <SettingsFormField label={t("settings.provider.agentModelLabel")}>
        <input
          value={draft.model}
          onChange={(event) => onChange({ ...draft, model: event.target.value })}
          className={INPUT_CLASS}
          placeholder={t("settings.provider.agentModelPlaceholder")}
        />
      </SettingsFormField>
      {draft.provider_kind === "openai" ? (
        <div className="grid gap-3 border-t border-slate-100 pt-5 sm:grid-cols-2 dark:border-slate-800">
          <SettingsFormField label={t("settings.provider.reasoningEffortLabel")}>
            <input
              value={draft.reasoning_effort}
              onChange={(event) => onChange({ ...draft, reasoning_effort: event.target.value })}
              className={INPUT_CLASS}
              placeholder={t("settings.provider.providerDefaultPlaceholder")}
            />
          </SettingsFormField>
          <SettingsFormField label={t("settings.provider.reasoningSummaryLabel")}>
            <input
              value={draft.reasoning_summary}
              onChange={(event) => onChange({ ...draft, reasoning_summary: event.target.value })}
              className={INPUT_CLASS}
              placeholder={t("settings.provider.providerDefaultPlaceholder")}
            />
          </SettingsFormField>
          <SettingsFormField label={t("settings.provider.textVerbosityLabel")}>
            <input
              value={draft.text_verbosity}
              onChange={(event) => onChange({ ...draft, text_verbosity: event.target.value })}
              className={INPUT_CLASS}
              placeholder={t("settings.provider.providerDefaultPlaceholder")}
            />
          </SettingsFormField>
          <SettingsFormField label={t("settings.provider.serviceTierLabel")}>
            <input
              value={draft.service_tier}
              onChange={(event) => onChange({ ...draft, service_tier: event.target.value })}
              className={INPUT_CLASS}
              placeholder={t("settings.provider.providerDefaultPlaceholder")}
            />
          </SettingsFormField>
        </div>
      ) : null}
      <div className="flex justify-end border-t border-slate-100 pt-5 dark:border-slate-800">
        <button
          type="button"
          onClick={onSave}
          disabled={
            pending ||
            !draft.model.trim() ||
            (draft.provider_kind === "openai" && !draft.provider_profile_id)
          }
          className={SETTINGS_MAIN_ACTION_CLASS}
        >
          {pending ? <Loader2 size={14} className="mr-2 animate-spin" /> : <Save size={14} className="mr-2" />}
          {t("settings.provider.saveAgent")}
        </button>
      </div>
    </div>
  );
}

interface ImageBindingSectionProps {
  data: ProviderConfigResponse | undefined;
  draft: ImageBindingDraft;
  pending: boolean;
  onChange: (next: ImageBindingDraft) => void;
  onSave: () => void;
}

function ImageBindingSection({ data, draft, pending, onChange, onSave }: ImageBindingSectionProps) {
  const { t } = useI18n();
  const requiredCapability =
    draft.provider_kind === "openai_responses"
      ? "image_responses"
      : draft.provider_kind === "google_gemini_image"
        ? "image_google_gemini"
        : "image_images";
  const profiles = (data?.profiles ?? []).filter(
    (profile) => profile.enabled && !profile.archived_at && profile.capabilities.includes(requiredCapability),
  );
  return (
    <div className={`${PANEL_CLASS} max-w-3xl space-y-5`}>
      <SettingsFormField label={t("settings.provider.apiInterfaceLabel")}>
        <SelectField
          value={draft.provider_kind}
          options={[
            { value: "mock", label: t("settings.provider.interface.mock") },
            { value: "openai_responses", label: t("settings.provider.interface.openaiResponses") },
            { value: "openai_images", label: t("settings.provider.interface.openaiImages") },
            { value: "google_gemini_image", label: t("settings.provider.interface.googleGeminiImage") },
          ]}
          onChange={(value) =>
            onChange({
              ...draft,
              provider_kind:
                value === "openai_responses" || value === "openai_images" || value === "google_gemini_image"
                  ? value
                  : "mock",
              provider_profile_id: "",
            })
          }
        />
      </SettingsFormField>
      {draft.provider_kind !== "mock" ? (
        <SettingsFormField label={t("settings.provider.providerProfileLabel")}>
          <SelectField
            value={draft.provider_profile_id}
            options={[
              { value: "", label: t("settings.provider.selectProfile") },
              ...profiles.map((profile) => ({ value: profile.id, label: profile.name })),
            ]}
            onChange={(value) => onChange({ ...draft, provider_profile_id: value })}
          />
        </SettingsFormField>
      ) : null}
      <SettingsFormField label={t("settings.provider.imageModelLabel")}>
        <input
          value={draft.model}
          onChange={(event) => onChange({ ...draft, model: event.target.value })}
          className={INPUT_CLASS}
          placeholder={t("settings.provider.imageModelPlaceholder")}
        />
      </SettingsFormField>
      {draft.provider_kind === "google_gemini_image" ? (
        <div className="grid gap-3 sm:grid-cols-2">
          <SettingsFormField label={t("settings.provider.geminiApiVersionLabel")}>
            <SelectField
              value={draft.gemini_api_version}
              options={[
                { value: "v1beta", label: "v1beta" },
                { value: "v1", label: "v1" },
              ]}
              onChange={(value) =>
                onChange({ ...draft, gemini_api_version: value === "v1" ? "v1" : "v1beta" })
              }
            />
          </SettingsFormField>
          <SettingsFormField label={t("settings.provider.geminiOutputMimeTypeLabel")}>
            <SelectField
              value={draft.gemini_output_mime_type}
              options={[
                { value: "", label: t("settings.provider.geminiOutputMimeTypeDefault") },
                { value: "image/png", label: "image/png" },
                { value: "image/jpeg", label: "image/jpeg" },
                { value: "image/webp", label: "image/webp" },
              ]}
              onChange={(value) => onChange({ ...draft, gemini_output_mime_type: value })}
            />
          </SettingsFormField>
        </div>
      ) : null}
      {draft.provider_kind === "openai_images" ? (
        <div className="grid gap-3 sm:grid-cols-2">
          <SettingsFormField label={t("settings.provider.imagesQualityLabel")}>
            <input
              value={draft.images_quality}
              onChange={(event) => onChange({ ...draft, images_quality: event.target.value })}
              className={INPUT_CLASS}
              placeholder={t("settings.provider.imagesQualityPlaceholder")}
            />
          </SettingsFormField>
          <SettingsFormField label={t("settings.provider.imagesStyleLabel")}>
            <input
              value={draft.images_style}
              onChange={(event) => onChange({ ...draft, images_style: event.target.value })}
              className={INPUT_CLASS}
              placeholder={t("settings.provider.imagesStylePlaceholder")}
            />
          </SettingsFormField>
        </div>
      ) : null}
      <div className="flex flex-col gap-5 border-t border-slate-100 pt-5 dark:border-slate-800 sm:flex-row sm:items-center sm:justify-between">
        {draft.provider_kind === "openai_responses" ? (
          <label className="inline-flex items-center gap-3 text-sm font-medium text-slate-700 dark:text-slate-300">
            <input
              type="checkbox"
              checked={draft.responses_background_enabled}
              onChange={(event) => onChange({ ...draft, responses_background_enabled: event.target.checked })}
              className="h-4 w-4 rounded border-slate-300 accent-indigo-600 dark:border-slate-600"
            />
            {t("settings.provider.responsesBackground")}
          </label>
        ) : (
          <span />
        )}
        <button
          type="button"
          onClick={onSave}
          disabled={
            pending ||
            !draft.model.trim() ||
            (draft.provider_kind !== "mock" && !draft.provider_profile_id)
          }
          className={SETTINGS_MAIN_ACTION_CLASS}
        >
          {pending ? <Loader2 size={14} className="mr-2 animate-spin" /> : <Save size={14} className="mr-2" />}
          {t("settings.provider.saveImage")}
        </button>
      </div>
    </div>
  );
}

export function SettingsPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [settingsSearchParams, setSettingsSearchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const importInputRef = useRef<HTMLInputElement | null>(null);
  const [drafts, setDrafts] = useState<Record<string, DraftValue>>({});
  const [draftSnapshots, setDraftSnapshots] = useState<Record<string, DraftSnapshot>>({});
  const [secretTouched, setSecretTouched] = useState<Record<string, boolean>>({});
  const [resettingKey, setResettingKey] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [savedMessage, setSavedMessage] = useState("");
  const [unlockToken, setUnlockToken] = useState("");
  const activeSection = settingsSectionFromSearchParam(settingsSearchParams.get("section"));
  const setActiveSection = useCallback(
    (section: SettingsSectionId) => {
      const next = new URLSearchParams(settingsSearchParams);
      if (section === "providers") {
        next.delete("section");
      } else {
        next.set("section", section);
      }
      setSettingsSearchParams(next, { replace: true });
    },
    [settingsSearchParams, setSettingsSearchParams],
  );
  const [sectionSearch, setSectionSearch] = useState("");
  const [providerProfileForm, setProviderProfileForm] = useState<ProviderProfileFormState>(EMPTY_PROVIDER_FORM);
  const [editingProviderProfileId, setEditingProviderProfileId] = useState<string | null>(null);
  const [providerDrawerOpen, setProviderDrawerOpen] = useState(false);
  const [pendingDeleteProviderProfile, setPendingDeleteProviderProfile] = useState<ProviderProfile | null>(null);
  const [togglingProviderProfileId, setTogglingProviderProfileId] = useState<string | null>(null);
  const [promptDraft, setPromptDraft] = useState<PromptBindingDraft>(promptBindingDraft(undefined));
  const [agentDraft, setAgentDraft] = useState<AgentBindingDraft>(agentBindingDraft(undefined));
  const [imageDraft, setImageDraft] = useState<ImageBindingDraft>(imageBindingDraft(undefined));
  const [exportConfirmOpen, setExportConfirmOpen] = useState(false);
  const [importPayload, setImportPayload] = useState<SettingsExportPayload | null>(null);
  const [importPreview, setImportPreview] = useState<SettingsImportPreviewResponse | null>(null);
  const [importFileName, setImportFileName] = useState("");

  const lockStateQuery = useQuery({
    queryKey: ["settings-lock-state"],
    queryFn: api.getSettingsLockState,
  });

  const configQuery = useQuery({
    queryKey: ["config"],
    queryFn: api.getConfig,
    enabled: Boolean(lockStateQuery.data?.unlocked),
  });

  const providerConfigQuery = useQuery({
    queryKey: ["provider-config"],
    queryFn: api.getProviderConfig,
    enabled: Boolean(lockStateQuery.data?.unlocked),
  });

  const resetDraftsFromConfig = useCallback((config: ConfigResponse | undefined) => {
    if (!config) {
      return;
    }
    const next = draftsFromConfig(config);
    setDrafts(next.drafts);
    setDraftSnapshots(next.snapshots);
    setSecretTouched({});
  }, []);

  useEffect(() => {
    resetDraftsFromConfig(configQuery.data);
  }, [configQuery.data, resetDraftsFromConfig]);

  useEffect(() => {
    setPromptDraft(promptBindingDraft(getBinding(providerConfigQuery.data, "prompt")));
    setAgentDraft(agentBindingDraft(getBinding(providerConfigQuery.data, "agent")));
    setImageDraft(imageBindingDraft(getBinding(providerConfigQuery.data, "image")));
  }, [providerConfigQuery.data]);

  useEffect(() => {
    if (configQuery.error instanceof ApiError && configQuery.error.status === 403) {
      queryClient.setQueryData(["settings-lock-state"], { unlocked: false, configured: true });
      queryClient.removeQueries({ queryKey: ["config"] });
      queryClient.removeQueries({ queryKey: ["provider-config"] });
    }
  }, [configQuery.error, queryClient]);

  const activeMeta = SETTINGS_SECTIONS.find((section) => section.id === activeSection) ?? SETTINGS_SECTIONS[0];
  const activeItems = itemsForSection(configQuery.data, activeSection);
  const normalizedSectionSearch = sectionSearch.trim().toLowerCase();
  const visibleSections = normalizedSectionSearch
    ? SETTINGS_SECTIONS.filter(
        (section) =>
          t(section.labelKey).toLowerCase().includes(normalizedSectionSearch) ||
          t(section.descriptionKey).toLowerCase().includes(normalizedSectionSearch),
      )
    : SETTINGS_SECTIONS;

  const saveMutation = useMutation({
    mutationFn: () => {
      const values = configValuesFromChangedDrafts(activeItems, drafts, draftSnapshots, secretTouched);
      return api.updateConfig({ values });
    },
    onSuccess: (data) => {
      queryClient.setQueryData(["config"], data);
      void queryClient.invalidateQueries({ queryKey: ["runtime-config"] });
      void queryClient.invalidateQueries({ queryKey: ["session"] });
      setError("");
      setSavedMessage(t("settings.saved"));
    },
    onError: (mutationError) => {
      setSavedMessage("");
      setError(mutationError instanceof ApiError ? mutationError.detail : t("settings.saveFailed"));
    },
  });

  const resetMutation = useMutation({
    mutationFn: (key: string) => api.updateConfig({ reset_keys: [key] }),
    onMutate: (key) => {
      setResettingKey(key);
      setError("");
      setSavedMessage("");
    },
    onSuccess: (data) => {
      queryClient.setQueryData(["config"], data);
      void queryClient.invalidateQueries({ queryKey: ["runtime-config"] });
      void queryClient.invalidateQueries({ queryKey: ["session"] });
      setSavedMessage(t("settings.restored"));
    },
    onError: (mutationError) => {
      setError(mutationError instanceof ApiError ? mutationError.detail : t("settings.restoreFailed"));
    },
    onSettled: () => setResettingKey(null),
  });

  const exportSettingsMutation = useMutation({
    mutationFn: api.exportSettings,
    onSuccess: (payload) => {
      downloadSettingsExport(payload);
      setExportConfirmOpen(false);
      setError("");
      setSavedMessage(t("settings.migration.exported"));
    },
    onError: (mutationError) => {
      setExportConfirmOpen(false);
      setSavedMessage("");
      setError(mutationError instanceof ApiError ? mutationError.detail : t("settings.migration.exportFailed"));
    },
  });

  const previewImportMutation = useMutation({
    mutationFn: async (file: File) => {
      const text = await file.text();
      let payload: unknown;
      try {
        payload = JSON.parse(text) as unknown;
      } catch {
        throw new Error(t("settings.migration.invalidJson"));
      }
      if (!isSettingsExportPayload(payload)) {
        throw new Error(t("settings.migration.invalidFile"));
      }
      const preview = await api.previewSettingsImport(payload);
      return { fileName: file.name, payload, preview };
    },
    onSuccess: ({ fileName, payload, preview }) => {
      setImportFileName(fileName);
      setImportPayload(payload);
      setImportPreview(preview);
      setError("");
      setSavedMessage("");
    },
    onError: (mutationError) => {
      setImportFileName("");
      setImportPayload(null);
      setImportPreview(null);
      setSavedMessage("");
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : mutationError instanceof Error
            ? mutationError.message
            : t("settings.migration.previewFailed"),
      );
    },
  });

  const commitImportMutation = useMutation({
    mutationFn: () => {
      if (!importPayload) {
        throw new Error(t("settings.migration.missingPreview"));
      }
      return api.importSettings(importPayload);
    },
    onSuccess: async (data) => {
      if (data.config) {
        queryClient.setQueryData(["config"], data.config);
      }
      if (data.provider_config) {
        queryClient.setQueryData(["provider-config"], data.provider_config);
      }
      await queryClient.invalidateQueries({ queryKey: ["config"] });
      await queryClient.invalidateQueries({ queryKey: ["provider-config"] });
      await queryClient.invalidateQueries({ queryKey: ["runtime-config"] });
      await queryClient.invalidateQueries({ queryKey: ["session"] });
      setImportPayload(null);
      setImportPreview(null);
      setImportFileName("");
      setError("");
      setSavedMessage(t("settings.migration.imported"));
    },
    onError: (mutationError) => {
      setSavedMessage("");
      setError(
        mutationError instanceof ApiError
          ? mutationError.detail
          : mutationError instanceof Error
            ? mutationError.message
            : t("settings.migration.importFailed"),
      );
    },
  });

  const unlockMutation = useMutation({
    mutationFn: () => api.unlockSettings(unlockToken),
    onSuccess: (data) => {
      queryClient.setQueryData(["settings-lock-state"], data);
      setUnlockToken("");
      setError("");
      setSavedMessage(t("settings.unlocked"));
      void queryClient.invalidateQueries({ queryKey: ["config"] });
      void queryClient.invalidateQueries({ queryKey: ["provider-config"] });
    },
    onError: (mutationError) => {
      setSavedMessage("");
      setError(mutationError instanceof ApiError ? mutationError.detail : t("settings.unlockFailed"));
    },
  });

  const createProviderProfileMutation = useMutation({
    mutationFn: () => api.createProviderProfile(providerProfileCreatePayload(providerProfileForm)),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["provider-config"] });
      setProviderProfileForm(EMPTY_PROVIDER_FORM);
      setEditingProviderProfileId(null);
      setProviderDrawerOpen(false);
      setError("");
      setSavedMessage(t("settings.provider.saved"));
    },
    onError: (mutationError) => {
      setSavedMessage("");
      setError(mutationError instanceof ApiError ? mutationError.detail : t("settings.provider.saveFailed"));
    },
  });

  const updateProviderProfileMutation = useMutation({
    mutationFn: () => {
      if (!editingProviderProfileId) {
        throw new Error(t("settings.provider.missingId"));
      }
      return api.updateProviderProfile(editingProviderProfileId, providerProfileUpdatePayload(providerProfileForm));
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["provider-config"] });
      setProviderProfileForm(EMPTY_PROVIDER_FORM);
      setEditingProviderProfileId(null);
      setProviderDrawerOpen(false);
      setError("");
      setSavedMessage(t("settings.provider.saved"));
    },
    onError: (mutationError) => {
      setSavedMessage("");
      setError(mutationError instanceof ApiError ? mutationError.detail : t("settings.provider.saveFailed"));
    },
  });

  const deleteProviderProfileMutation = useMutation({
    mutationFn: (profileId: string) => api.archiveProviderProfile(profileId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["provider-config"] });
      setPendingDeleteProviderProfile(null);
      setError("");
      setSavedMessage(t("settings.provider.deletedMessage"));
    },
    onError: (mutationError) => {
      setPendingDeleteProviderProfile(null);
      setSavedMessage("");
      setError(mutationError instanceof ApiError ? mutationError.detail : t("settings.provider.deleteFailed"));
    },
  });

  const updateProviderProfileEnabledMutation = useMutation({
    mutationFn: ({ profileId, enabled }: { profileId: string; enabled: boolean }) =>
      api.updateProviderProfile(profileId, { enabled }),
    onMutate: ({ profileId }) => {
      setTogglingProviderProfileId(profileId);
      setError("");
      setSavedMessage("");
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["provider-config"] });
      setError("");
      setSavedMessage(t("settings.provider.saved"));
    },
    onError: (mutationError) => {
      setSavedMessage("");
      setError(mutationError instanceof ApiError ? mutationError.detail : t("settings.provider.saveFailed"));
    },
    onSettled: () => setTogglingProviderProfileId(null),
  });

  const updatePromptBindingMutation = useMutation({
    mutationFn: () => api.updateProviderBinding("prompt", promptBindingPayloadFromDraft(promptDraft)),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["provider-config"] });
      setError("");
      setSavedMessage(t("settings.provider.promptSaved"));
    },
    onError: (mutationError) => {
      setSavedMessage("");
      setError(mutationError instanceof ApiError ? mutationError.detail : t("settings.provider.promptSaveFailed"));
    },
  });

  const updateImageBindingMutation = useMutation({
    mutationFn: () => {
      return api.updateProviderBinding("image", imageBindingPayloadFromDraft(imageDraft));
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["provider-config"] });
      setError("");
      setSavedMessage(t("settings.provider.imageSaved"));
    },
    onError: (mutationError) => {
      setSavedMessage("");
      setError(mutationError instanceof ApiError ? mutationError.detail : t("settings.provider.imageSaveFailed"));
    },
  });

  const updateAgentBindingMutation = useMutation({
    mutationFn: () => api.updateProviderBinding("agent", agentBindingPayloadFromDraft(agentDraft)),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["provider-config"] });
      setError("");
      setSavedMessage(t("settings.provider.agentSaved"));
    },
    onError: (mutationError) => {
      setSavedMessage("");
      setError(mutationError instanceof ApiError ? mutationError.detail : t("settings.provider.agentSaveFailed"));
    },
  });

  const logoutMutation = useMutation({
    mutationFn: api.destroySession,
    onSuccess: async () => {
      queryClient.removeQueries({ queryKey: ["settings-lock-state"] });
      queryClient.removeQueries({ queryKey: ["config"] });
      queryClient.removeQueries({ queryKey: ["provider-config"] });
      await queryClient.invalidateQueries({ queryKey: ["session"] });
      navigate("/login", { replace: true });
    },
  });

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setError("");
    setSavedMessage("");
    saveMutation.mutate();
  };

  const handleUnlock = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setError("");
    setSavedMessage("");
    unlockMutation.mutate();
  };

  const handleImportFileChange = (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0] ?? null;
    event.currentTarget.value = "";
    if (!file) {
      return;
    }
    setError("");
    setSavedMessage("");
    previewImportMutation.mutate(file);
  };

  const providerProfilePending =
    createProviderProfileMutation.isPending ||
    updateProviderProfileMutation.isPending ||
    deleteProviderProfileMutation.isPending ||
    updateProviderProfileEnabledMutation.isPending;
  const providerPending =
    providerProfilePending ||
    updatePromptBindingMutation.isPending ||
    updateAgentBindingMutation.isPending ||
    updateImageBindingMutation.isPending;

  const isCheckingLockState = lockStateQuery.isLoading || lockStateQuery.isFetching;
  const loadingMain = configQuery.isLoading || providerConfigQuery.isLoading;
  const genericSection = ["prompts", "upload", "queue", "security"].includes(activeSection);
  const isUnlocked = Boolean(lockStateQuery.data?.unlocked);

  return (
    <div className="flex min-h-screen flex-col bg-white dark:bg-[#060a12] dark:text-slate-100">
      <TopNav
        breadcrumbs={t("settings.breadcrumb")}
        onHome={() => navigate("/products")}
        onLogout={() => logoutMutation.mutate()}
      />

      <main className="mx-auto flex w-full max-w-[1440px] flex-1">
        <div className="w-full">
          {!lockStateQuery.data?.unlocked ? (
            <div className="mb-6 flex flex-col gap-3 px-5 py-8 md:flex-row md:items-end md:justify-between lg:px-8 lg:py-10">
              <div>
                <div className="mb-2 inline-flex items-center rounded-full border border-indigo-100 bg-indigo-50 px-3 py-1 text-xs font-semibold text-indigo-700 dark:border-violet-400/35 dark:bg-violet-500/15 dark:text-violet-100">
                  <SettingsIcon size={13} className="mr-1.5" />
                  {t("settings.runtimeConfig")}
                </div>
                <h1 className="text-2xl font-semibold tracking-tight text-zinc-900 dark:text-white">
                  {t("settings.title")}
                </h1>
                <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">{t("settings.description")}</p>
              </div>
              <button
                type="button"
                onClick={() => navigate("/products")}
                className="text-sm font-medium text-zinc-500 hover:text-zinc-900 dark:text-slate-400 dark:hover:text-white"
              >
                {t("settings.back")}
              </button>
            </div>
          ) : null}

          {isCheckingLockState ? (
            <div className="flex justify-center py-20 text-zinc-400 dark:text-slate-500">
              <Loader2 size={22} className="animate-spin" />
            </div>
          ) : lockStateQuery.isError ? (
            <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
              {t("settings.lockLoadFailed")}
            </div>
          ) : !lockStateQuery.data?.configured ? (
            <div className="rounded-lg border border-amber-200 bg-amber-50 px-5 py-4 text-sm text-amber-800">
              {t("settings.tokenMissing")}
            </div>
          ) : !lockStateQuery.data.unlocked ? (
            <form
              onSubmit={handleUnlock}
              className="mx-auto max-w-xl rounded-lg border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-700 dark:bg-[#0f1726]"
            >
              <div className="mb-5 flex items-start gap-3">
                <span className="inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-indigo-50 text-indigo-600 dark:bg-violet-500/15 dark:text-violet-100">
                  <LockKeyhole size={18} />
                </span>
                <div>
                  <h2 className="text-base font-semibold text-slate-950 dark:text-white">
                    {t("settings.unlockTitle")}
                  </h2>
                  <p className="mt-1 text-sm leading-6 text-slate-500 dark:text-slate-400">
                    {t("settings.unlockDescription")}
                  </p>
                </div>
              </div>
              <input
                type="password"
                value={unlockToken}
                onChange={(event) => setUnlockToken(event.target.value)}
                className={INPUT_CLASS}
                placeholder={t("settings.unlockPlaceholder")}
                autoComplete="current-password"
              />
              {error ? (
                <div className="mt-4 rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
                  {error}
                </div>
              ) : null}
              <div className="mt-5 flex justify-end">
                <button
                  type="submit"
                  disabled={unlockMutation.isPending || !unlockToken.trim()}
                  className="inline-flex items-center rounded-lg bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-500 disabled:opacity-50 dark:bg-violet-500"
                >
                  {unlockMutation.isPending ? (
                    <Loader2 size={14} className="mr-2 animate-spin" />
                  ) : (
                    <LockKeyhole size={14} className="mr-2" />
                  )}
                  {t("settings.unlock")}
                </button>
              </div>
            </form>
          ) : loadingMain ? (
            <div className="flex justify-center py-20 text-zinc-400 dark:text-slate-500">
              <Loader2 size={22} className="animate-spin" />
            </div>
          ) : configQuery.isError ? (
            <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
              {configQuery.error instanceof ApiError ? configQuery.error.detail : t("settings.loadFailed")}
            </div>
          ) : (
            <div className="grid min-h-full lg:grid-cols-[280px_minmax(0,1fr)]">
              <aside className="border-b border-slate-200 bg-slate-50/70 dark:border-slate-800 dark:bg-[#0f1726] lg:sticky lg:top-0 lg:h-screen lg:border-b-0 lg:border-r">
                <div className="border-b border-slate-200 px-5 py-7 dark:border-slate-800">
                  <div className="flex items-center gap-3 text-lg font-semibold text-slate-950 dark:text-white">
                    <span className="inline-flex h-9 w-9 items-center justify-center rounded-lg bg-indigo-50 text-indigo-600 dark:bg-violet-500/15 dark:text-violet-200">
                      <SettingsIcon size={20} />
                    </span>
                    {t("settings.title")}
                  </div>
                  <label className="mt-6 flex h-10 items-center gap-2 rounded-lg border border-slate-200 bg-slate-50 px-3 text-sm text-slate-400 shadow-sm shadow-slate-200/30 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-500 dark:shadow-black/20">
                    <Search size={16} />
                    <input
                      value={sectionSearch}
                      onChange={(event) => setSectionSearch(event.target.value)}
                      placeholder={t("settings.searchPlaceholder")}
                      className="min-w-0 flex-1 bg-transparent text-sm text-slate-900 outline-none placeholder:text-slate-400 dark:text-slate-100 dark:placeholder:text-slate-500"
                    />
                  </label>
                </div>
                <nav className="hidden space-y-6 px-3 py-5 lg:block" aria-label={t("settings.navLabel")}>
                  {SETTINGS_GROUPS.map((group) => {
                    const sections = visibleSections.filter((section) => section.groupKey === group);
                    if (!sections.length) {
                      return null;
                    }
                    return (
                      <div key={group}>
                        <div className="px-2 text-xs font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-400">
                          {t(group)}
                        </div>
                        <div className="mt-2 space-y-1">
                          {sections.map((section) => {
                            const Icon = section.icon;
                            const active = section.id === activeSection;
                            return (
                              <button
                                key={section.id}
                                type="button"
                                onClick={() => setActiveSection(section.id)}
                                className={`flex w-full items-center gap-2 rounded-lg px-2 py-2 text-left text-sm transition-colors ${
                                  active
                                    ? "bg-indigo-50 font-semibold text-indigo-700 ring-1 ring-indigo-200 dark:bg-violet-500/18 dark:text-violet-100 dark:ring-violet-400/35"
                                    : "text-slate-600 hover:bg-slate-50 hover:text-slate-950 dark:text-slate-300 dark:hover:bg-violet-500/12 dark:hover:text-white"
                                }`}
                              >
                                <Icon size={15} className={active ? "shrink-0 text-indigo-600 dark:text-violet-200" : "shrink-0 text-slate-400 dark:text-slate-500"} />
                                <span className="truncate">{t(section.labelKey)}</span>
                              </button>
                            );
                          })}
                        </div>
                      </div>
                    );
                  })}
                </nav>
                <div className="p-4 lg:hidden">
                  <label htmlFor="settings-section" className="mb-2 block text-xs font-semibold text-slate-500 dark:text-slate-400">
                    {t("settings.mobileSectionLabel")}
                  </label>
                  <SelectField
                    id="settings-section"
                    value={activeSection}
                    groups={SETTINGS_GROUPS.map((group) => ({
                      label: t(group),
                      options: SETTINGS_SECTIONS.filter((section) => section.groupKey === group).map((section) => ({
                        value: section.id,
                        label: t(section.labelKey),
                      })),
                    }))}
                    onChange={(value) => setActiveSection(value as SettingsSectionId)}
                  />
                </div>
              </aside>

              <section className="min-w-0 bg-white px-5 pb-28 pt-8 dark:bg-[#0b1220] sm:px-8 lg:px-12 lg:py-12">
                <div className="mx-auto max-w-4xl">
                  <div className="mb-10">
                    <div className="mb-3 flex items-center gap-2 text-sm font-medium text-slate-500 dark:text-slate-400">
                      <span>{t("settings.title")}</span>
                      <span>/</span>
                      <span>{t(activeMeta.labelKey)}</span>
                    </div>
                    <h1 className="text-3xl font-semibold tracking-tight text-slate-950 dark:text-white">
                      {t(activeMeta.labelKey)}
                    </h1>
                    <p className="mt-3 max-w-3xl text-sm leading-6 text-slate-500 dark:text-slate-400">
                      {t(activeMeta.descriptionKey)}
                    </p>
                  </div>
                  {shouldShowSettingsMigrationPanel(activeSection) ? (
                    <SettingsMigrationPanel
                      importInputRef={importInputRef}
                      importFileName={importFileName}
                      importPreview={importPreview}
                      exportBusy={exportSettingsMutation.isPending}
                      importPreviewBusy={previewImportMutation.isPending}
                      importCommitBusy={commitImportMutation.isPending}
                      onRequestExport={() => {
                        setError("");
                        setSavedMessage("");
                        setExportConfirmOpen(true);
                      }}
                      onChooseImportFile={() => importInputRef.current?.click()}
                      onImportFileChange={handleImportFileChange}
                      onCommitImport={() => commitImportMutation.mutate()}
                      onCancelImport={() => {
                        setImportPayload(null);
                        setImportPreview(null);
                        setImportFileName("");
                      }}
                    />
                  ) : null}
                  {error ? (
                    <div className="mb-5 rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-400/35 dark:bg-red-500/10 dark:text-red-200">
                      {error}
                    </div>
                  ) : null}
                  {savedMessage ? (
                    <div className="mb-5 flex items-center rounded-md border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700 dark:border-emerald-400/35 dark:bg-emerald-500/10 dark:text-emerald-200">
                      <CheckCircle2 size={16} className="mr-2" />
                      {savedMessage}
                    </div>
                  ) : null}
                  <div>
                    {activeSection === "providers" ? (
                      <ProvidersSection
                        data={providerConfigQuery.data}
                        profileForm={providerProfileForm}
                        editingProfileId={editingProviderProfileId}
                        drawerOpen={providerDrawerOpen}
                        pending={providerProfilePending}
                        togglingProfileId={togglingProviderProfileId}
                        onProfileFormChange={setProviderProfileForm}
                        onOpenCreate={() => {
                          const next = providerDrawerCreateState();
                          setEditingProviderProfileId(next.editingProfileId);
                          setProviderProfileForm(next.form);
                          setProviderDrawerOpen(next.open);
                          setError("");
                          setSavedMessage("");
                        }}
                        onEditProfile={(profile) => {
                          const next = providerDrawerEditState(profile);
                          setEditingProviderProfileId(next.editingProfileId);
                          setProviderProfileForm(next.form);
                          setProviderDrawerOpen(next.open);
                          setError("");
                          setSavedMessage("");
                        }}
                        onCloseDrawer={() => {
                          setEditingProviderProfileId(null);
                          setProviderProfileForm(EMPTY_PROVIDER_FORM);
                          setProviderDrawerOpen(false);
                        }}
                        onSubmitProfile={() => {
                          setError("");
                          setSavedMessage("");
                          if (editingProviderProfileId) {
                            updateProviderProfileMutation.mutate();
                            return;
                          }
                          createProviderProfileMutation.mutate();
                        }}
                        onDeleteProfile={(profile) => {
                          setError("");
                          setSavedMessage("");
                          setPendingDeleteProviderProfile(profile);
                        }}
                        onToggleProfileEnabled={(profileId, enabled) => {
                          updateProviderProfileEnabledMutation.mutate({ profileId, enabled });
                        }}
                      />
                    ) : null}

                    {activeSection === "prompt" ? (
                      <PromptBindingSection
                        data={providerConfigQuery.data}
                        draft={promptDraft}
                        pending={providerPending}
                        onChange={(next) => {
                          setPromptDraft(next);
                          setSavedMessage("");
                        }}
                        onSave={() => {
                          setError("");
                          setSavedMessage("");
                          updatePromptBindingMutation.mutate();
                        }}
                      />
                    ) : null}

                    {activeSection === "agent" ? (
                      <AgentBindingSection
                        data={providerConfigQuery.data}
                        draft={agentDraft}
                        pending={providerPending}
                        onChange={(next) => {
                          setAgentDraft(next);
                          setSavedMessage("");
                        }}
                        onSave={() => {
                          setError("");
                          setSavedMessage("");
                          updateAgentBindingMutation.mutate();
                        }}
                      />
                    ) : null}

                    {activeSection === "image" ? (
                      <ImageBindingSection
                        data={providerConfigQuery.data}
                        draft={imageDraft}
                        pending={providerPending}
                        onChange={(next) => {
                          setImageDraft(next);
                          setSavedMessage("");
                        }}
                        onSave={() => {
                          setError("");
                          setSavedMessage("");
                          updateImageBindingMutation.mutate();
                        }}
                      />
                    ) : null}

                    {genericSection ? (
                      <form onSubmit={handleSubmit} className={`${PANEL_CLASS} space-y-2`}>
                        {activeItems.length ? (
                          activeSection === "upload" ? (
                            <div className="grid gap-3 lg:grid-cols-2">
                              {activeItems.map((item) => (
                                <ConfigField
                                  key={item.key}
                                  item={item}
                                  value={drafts[item.key] ?? draftFromItem(item)}
                                  secretTouched={Boolean(secretTouched[item.key])}
                                  isResetting={resettingKey === item.key}
                                  layout="card"
                                  onChange={(nextValue, touchedSecret) => {
                                    setDrafts((current) => ({ ...current, [item.key]: nextValue }));
                                    setSavedMessage("");
                                    if (touchedSecret) {
                                      setSecretTouched((current) => ({ ...current, [item.key]: true }));
                                    }
                                  }}
                                  onReset={() => resetMutation.mutate(item.key)}
                                />
                              ))}
                            </div>
                          ) : (
                            activeItems.map((item) => (
                              <ConfigField
                                key={item.key}
                                item={item}
                                value={drafts[item.key] ?? draftFromItem(item)}
                                secretTouched={Boolean(secretTouched[item.key])}
                                isResetting={resettingKey === item.key}
                                onChange={(nextValue, touchedSecret) => {
                                  setDrafts((current) => ({ ...current, [item.key]: nextValue }));
                                  setSavedMessage("");
                                  if (touchedSecret) {
                                    setSecretTouched((current) => ({ ...current, [item.key]: true }));
                                  }
                                }}
                                onReset={() => resetMutation.mutate(item.key)}
                              />
                            ))
                          )
                        ) : (
                          <div className="rounded-lg border border-dashed border-slate-300 px-4 py-10 text-center text-sm text-slate-500 dark:border-slate-700 dark:text-slate-400">
                            {t("settings.section.empty")}
                          </div>
                        )}
                        <div className="flex justify-end gap-3 border-t border-slate-100 pt-5 dark:border-slate-800">
                          <button
                            type="button"
                            onClick={() => resetDraftsFromConfig(configQuery.data)}
                            className="px-4 py-2 text-sm font-medium text-zinc-600 hover:text-zinc-900 dark:text-slate-300 dark:hover:text-white"
                          >
                            {t("settings.discard")}
                          </button>
                          <button
                            type="submit"
                            disabled={saveMutation.isPending}
                            className={SETTINGS_MAIN_ACTION_CLASS}
                          >
                            {saveMutation.isPending ? (
                              <Loader2 size={14} className="mr-2 animate-spin" />
                            ) : (
                              <Save size={14} className="mr-2" />
                            )}
                            {t("settings.save")}
                          </button>
                        </div>
                      </form>
                    ) : null}
                  </div>
                </div>
              </section>
            </div>
          )}

          {!isUnlocked && error ? (
            <div className="mx-8 mt-5 rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-400/35 dark:bg-red-500/10 dark:text-red-200">
              {error}
            </div>
          ) : null}
          {!isUnlocked && savedMessage ? (
            <div className="mx-8 mt-5 flex items-center rounded-md border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700 dark:border-emerald-400/35 dark:bg-emerald-500/10 dark:text-emerald-200">
              <CheckCircle2 size={16} className="mr-2" />
              {savedMessage}
            </div>
          ) : null}
        </div>
        <ConfirmDialog
          open={exportConfirmOpen}
          title={t("settings.migration.exportConfirmTitle")}
          description={t("settings.migration.exportConfirm")}
          confirmLabel={t("settings.migration.exportConfirmLabel")}
          cancelLabel={t("common.cancel")}
          busy={exportSettingsMutation.isPending}
          destructive={false}
          onClose={() => setExportConfirmOpen(false)}
          onConfirm={() => exportSettingsMutation.mutate()}
        />
        <ConfirmDialog
          open={Boolean(pendingDeleteProviderProfile)}
          title={t("settings.provider.deleteConfirmTitle")}
          description={
            pendingDeleteProviderProfile
              ? t("settings.provider.deleteConfirm", { name: pendingDeleteProviderProfile.name })
              : ""
          }
          confirmLabel={t("settings.provider.deleteConfirmLabel")}
          cancelLabel={t("common.cancel")}
          busy={deleteProviderProfileMutation.isPending}
          onClose={() => setPendingDeleteProviderProfile(null)}
          onConfirm={() => {
            if (pendingDeleteProviderProfile) {
              deleteProviderProfileMutation.mutate(pendingDeleteProviderProfile.id);
            }
          }}
        />
      </main>
    </div>
  );
}
