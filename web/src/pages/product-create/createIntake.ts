import type { WorkflowGenerationSpec } from "../../lib/types";
import { parseWorkflowGenerationSpec } from "../workbench/canvas/generationSpec";

export const CREATE_DEFAULT_TEXT_POLICY = "required" as const;
export const CREATE_DEFAULT_TEXT_LANGUAGE = "zh-CN";
export const CREATE_BRIEF_MAX_LENGTH = 4000;
export const CREATE_SHARED_ASPECT_FALLBACK = "1:1";

export const CREATE_TEXT_POLICIES = ["required", "allow", "none"] as const;
export const CREATE_TEXT_LANGUAGE_OPTIONS = [
  { value: "zh-CN", label: "简体中文" },
  { value: "en-US", label: "English" },
  { value: "ja-JP", label: "日本語" },
  { value: "vi-VN", label: "Tiếng Việt" },
] as const;

export type CreateTextPolicy = WorkflowGenerationSpec["text_policy"];

export interface CreateOutputDraft {
  textPolicy: CreateTextPolicy;
  textLanguage: string;
}

export function defaultCreateOutputDraft(): CreateOutputDraft {
  return {
    textPolicy: CREATE_DEFAULT_TEXT_POLICY,
    textLanguage: CREATE_DEFAULT_TEXT_LANGUAGE,
  };
}

export function buildCreateGenerationSpec(draft: CreateOutputDraft): WorkflowGenerationSpec | null {
  const language = draft.textLanguage.trim();
  return parseWorkflowGenerationSpec({
    aspect_ratio: CREATE_SHARED_ASPECT_FALLBACK,
    resolution_tier: "high",
    quality_intent: "high",
    reference_fidelity: "high",
    background_intent: "auto",
    text_policy: draft.textPolicy,
    text_language: draft.textPolicy === "none" ? null : language,
  });
}

export function createOutputSummary(draft: CreateOutputDraft): {
  textPolicy: CreateTextPolicy;
  textLanguage: string | null;
} {
  return {
    textPolicy: draft.textPolicy,
    textLanguage: draft.textPolicy === "none" ? null : draft.textLanguage.trim() || null,
  };
}

export function isCreateBriefReady(brief: string): boolean {
  return brief.trim().length > 0;
}

export function isCreateOutputReady(draft: CreateOutputDraft): boolean {
  return buildCreateGenerationSpec(draft) !== null;
}
