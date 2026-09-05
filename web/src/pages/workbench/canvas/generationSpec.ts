/**
 * 从节点配置解析 GenerationSpec。
 *
 * GenerationSpec 是模型生成意图。DeliverySpec 是另一套确定性派生；改它不等于重跑模型。
 */

import type { WorkflowGenerationSpec } from "../../../lib/types";
import { parseAspectRatio } from "../../../components/ImageRatioFrame";

const RESOLUTION_TIERS = new Set<WorkflowGenerationSpec["resolution_tier"]>(["standard", "high", "ultra"]);
const QUALITY_INTENTS = new Set<WorkflowGenerationSpec["quality_intent"]>(["draft", "standard", "high"]);
const REFERENCE_FIDELITIES = new Set<WorkflowGenerationSpec["reference_fidelity"]>(["low", "medium", "high"]);
const BACKGROUND_INTENTS = new Set<WorkflowGenerationSpec["background_intent"]>(["auto", "opaque", "transparent"]);

export function parseWorkflowGenerationSpec(value: unknown): WorkflowGenerationSpec | null {
  if (!isRecord(value)) return null;
  const aspectRatio = value.aspect_ratio;
  const resolutionTier = value.resolution_tier;
  const qualityIntent = value.quality_intent;
  const referenceFidelity = value.reference_fidelity;
  const backgroundIntent = value.background_intent;
  if (
    typeof aspectRatio !== "string"
    || !parseAspectRatio(aspectRatio)
    || typeof resolutionTier !== "string"
    || !RESOLUTION_TIERS.has(resolutionTier as WorkflowGenerationSpec["resolution_tier"])
    || typeof qualityIntent !== "string"
    || !QUALITY_INTENTS.has(qualityIntent as WorkflowGenerationSpec["quality_intent"])
    || typeof referenceFidelity !== "string"
    || !REFERENCE_FIDELITIES.has(referenceFidelity as WorkflowGenerationSpec["reference_fidelity"])
    || typeof backgroundIntent !== "string"
    || !BACKGROUND_INTENTS.has(backgroundIntent as WorkflowGenerationSpec["background_intent"])
    || "text_policy" in value || "text_language" in value
  ) {
    return null;
  }
  return {
    aspect_ratio: aspectRatio.trim(),
    resolution_tier: resolutionTier as WorkflowGenerationSpec["resolution_tier"],
    quality_intent: qualityIntent as WorkflowGenerationSpec["quality_intent"],
    reference_fidelity: referenceFidelity as WorkflowGenerationSpec["reference_fidelity"],
    background_intent: backgroundIntent as WorkflowGenerationSpec["background_intent"],
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
