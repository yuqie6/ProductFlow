import type { AgentProductWorkspaceLimits, WorkflowGenerationSpec } from "../../lib/types";
import {
  validateAgentProductWorkspaceInput,
  type AgentImageTypeSelectionDraft,
} from "./imageTypeSelection";

export const CREATE_DEFAULT_TEXT_POLICY = "none" as const;
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
  if (draft.textPolicy !== "none" && !language) return null;
  return {
    aspect_ratio: CREATE_SHARED_ASPECT_FALLBACK,
    resolution_tier: "high",
    quality_intent: "high",
    reference_fidelity: "high",
    background_intent: "auto",
    text_policy: draft.textPolicy,
    text_language: language || CREATE_DEFAULT_TEXT_LANGUAGE,
  };
}

export function createOutputSummary(draft: CreateOutputDraft): {
  textPolicy: CreateTextPolicy;
  textLanguage: string | null;
} {
  return {
    textPolicy: draft.textPolicy,
    textLanguage: draft.textLanguage.trim() || CREATE_DEFAULT_TEXT_LANGUAGE,
  };
}

export function isCreateBriefReady(brief: string): boolean {
  return brief.trim().length > 0;
}

export function isCreateOutputReady(draft: CreateOutputDraft): boolean {
  return buildCreateGenerationSpec(draft) !== null;
}

export type CreateSubmitAction = "full-canvas" | "conversation" | "blocked";

export function isCreateCanvasReady(input: {
  name: string;
  brief: string;
  selections: readonly AgentImageTypeSelectionDraft[];
  referenceImageCount: number;
  limits: AgentProductWorkspaceLimits | null;
  outputDraft: CreateOutputDraft;
}): boolean {
  if (!isCreateBriefReady(input.brief) || !isCreateOutputReady(input.outputDraft)) return false;
  return validateAgentProductWorkspaceInput({
    name: input.name,
    selections: input.selections,
    referenceImageCount: input.referenceImageCount,
    limits: input.limits,
  }) === null;
}

export function hasCreateImagePlan(input: {
  selections: readonly AgentImageTypeSelectionDraft[];
  referenceImageCount: number;
}): boolean {
  return input.selections.length > 0 || input.referenceImageCount > 0;
}

export function resolveCreateSubmitAction(input: {
  name: string;
  brief: string;
  selections: readonly AgentImageTypeSelectionDraft[];
  referenceImageCount: number;
  limits: AgentProductWorkspaceLimits | null;
  outputDraft: CreateOutputDraft;
}): CreateSubmitAction {
  if (isCreateCanvasReady(input)) return "full-canvas";
  if (hasCreateImagePlan(input)) return "blocked";
  return "conversation";
}
