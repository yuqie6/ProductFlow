import type { ImageSessionGenerationTask, ImageSessionRound, ImageToolOptions } from "../../lib/types";

export interface ImageRoundGroup {
  id: string;
  base_asset_id: string | null;
  prompt: string;
  rounds: ImageSessionRound[];
}

export function groupImageSessionRounds(rounds: ImageSessionRound[]): ImageRoundGroup[] {
  const groups = new Map<string, ImageRoundGroup>();
  for (const round of rounds) {
    const groupId = round.generation_group_id ?? round.id;
    const existing = groups.get(groupId);
    if (existing) {
      existing.rounds.push(round);
      continue;
    }
    groups.set(groupId, {
      id: groupId,
      base_asset_id: round.base_asset_id,
      prompt: round.prompt,
      rounds: [round],
    });
  }
  return [...groups.values()].map((group) => ({
    ...group,
    rounds: [...group.rounds].sort((a, b) => a.candidate_index - b.candidate_index),
  }));
}

export function clampGenerationCount(value: number): number {
  if (!Number.isFinite(value)) {
    return 1;
  }
  return Math.min(4, Math.max(1, Math.round(value)));
}

function compactString(value: string | null | undefined): string | undefined {
  const normalized = value?.trim();
  return normalized || undefined;
}

function clampOptionalInteger(
  value: number | null | undefined,
  minimum: number,
  maximum: number,
): number | undefined {
  if (value === null || value === undefined || !Number.isFinite(value)) {
    return undefined;
  }
  return Math.min(maximum, Math.max(minimum, Math.round(value)));
}

export function compactImageToolOptions(options: ImageToolOptions): ImageToolOptions | undefined {
  const compacted: ImageToolOptions = {
    model: compactString(options.model),
    quality: options.quality || undefined,
    output_format: options.output_format || undefined,
    output_compression: clampOptionalInteger(options.output_compression, 0, 100),
    background: options.background || undefined,
    moderation: options.moderation || undefined,
    action: options.action || undefined,
    input_fidelity: options.input_fidelity || undefined,
    partial_images: clampOptionalInteger(options.partial_images, 0, 3),
    n: clampOptionalInteger(options.n, 1, 10),
  };
  const entries = Object.entries(compacted).filter(([, value]) => value !== undefined);
  return entries.length ? (Object.fromEntries(entries) as ImageToolOptions) : undefined;
}

export function pruneSelectedReferenceIds(
  selectedIds: string[],
  availableIds: string[],
  maxCount = Number.POSITIVE_INFINITY,
): string[] {
  const available = new Set(availableIds);
  return selectedIds
    .filter((id, index) => available.has(id) && selectedIds.indexOf(id) === index)
    .slice(0, maxCount);
}

function generationTaskPriority(task: ImageSessionGenerationTask): number {
  if (task.status === "running") {
    return 0;
  }
  if (task.status === "queued") {
    return 1;
  }
  if (task.status === "failed") {
    return 2;
  }
  return 3;
}

export function selectVisibleGenerationTasks(
  tasks: ImageSessionGenerationTask[],
  limit = 4,
): ImageSessionGenerationTask[] {
  return [...tasks]
    .sort((left, right) => {
      const priorityDelta = generationTaskPriority(left) - generationTaskPriority(right);
      if (priorityDelta !== 0) {
        return priorityDelta;
      }
      return Date.parse(right.created_at) - Date.parse(left.created_at);
    })
    .slice(0, limit);
}
