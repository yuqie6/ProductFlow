import type {
  ImageSessionDetail,
  ImageSessionGenerationTask,
  ImageSessionRound,
  ImageSessionStatus,
  ImageToolOptions,
} from "../../lib/types";
export { compactImageToolOptions } from "../../lib/imageToolOptions";

export interface ImageRoundGroup {
  id: string;
  base_asset_id: string | null;
  prompt: string;
  rounds: ImageSessionRound[];
}

export type ImageHistoryPlaceholderStatus = "queued" | "running" | "completed" | "failed";

export interface ImageHistoryRoundCandidate {
  id: string;
  kind: "round";
  group_id: string;
  candidate_index: number;
  candidate_count: number;
  status: "succeeded";
  round: ImageSessionRound;
  prompt: string;
  size: string;
  base_asset_id: string | null;
  provider_notes: string[];
  failure_reason: null;
  created_at: string;
}

export interface ImageHistoryPlaceholderCandidate {
  id: string;
  kind: "placeholder";
  group_id: string;
  task_id: string;
  candidate_index: number;
  candidate_count: number;
  status: ImageHistoryPlaceholderStatus;
  task_status: ImageSessionGenerationTask["status"];
  task: ImageSessionGenerationTask;
  prompt: string;
  size: string;
  base_asset_id: string | null;
  provider_notes: string[];
  failure_reason: string | null;
  created_at: string;
}

export type ImageHistoryCandidate = ImageHistoryRoundCandidate | ImageHistoryPlaceholderCandidate;

export interface ImageHistoryBranch {
  id: string;
  base_asset_id: string | null;
  parent_group_id: string | null;
  depth: number;
  prompt: string;
  created_at: string;
  candidates: ImageHistoryCandidate[];
}

export interface ImageGenerationSubmitPayload {
  prompt: string;
  size: string;
  base_asset_id: string | null;
  selected_reference_asset_ids: string[];
  generation_count: number;
  tool_options?: ImageToolOptions | null;
}

export interface ImageGenerationSubmitGuard {
  signature: string;
  submittedAt: number;
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

function compareCreatedAt(left: string, right: string): number {
  return Date.parse(left) - Date.parse(right);
}

function getRoundGroupId(round: ImageSessionRound): string {
  return round.generation_group_id ?? round.id;
}

function getTaskGroupId(task: ImageSessionGenerationTask): string {
  return task.result_generation_group_id ?? `task:${task.id}`;
}

export function getImageGenerationTaskPlaceholderId(task: ImageSessionGenerationTask, candidateIndex: number): string {
  return `task:${task.id}:candidate:${candidateIndex}`;
}

function getPlaceholderCandidateStatus(
  task: ImageSessionGenerationTask,
  candidateIndex: number,
): ImageHistoryPlaceholderStatus {
  if (task.status === "failed") {
    return "failed";
  }
  if (task.status === "succeeded") {
    return "completed";
  }
  if (task.status === "queued") {
    return "queued";
  }
  if (candidateIndex <= task.completed_candidates) {
    return "completed";
  }
  return "running";
}

function sortCandidates(left: ImageHistoryCandidate, right: ImageHistoryCandidate): number {
  const indexDelta = left.candidate_index - right.candidate_index;
  if (indexDelta !== 0) {
    return indexDelta;
  }
  if (left.kind === right.kind) {
    return left.id.localeCompare(right.id);
  }
  return left.kind === "round" ? -1 : 1;
}

function calculateBranchDepth(
  branchId: string,
  branchesById: Map<string, Omit<ImageHistoryBranch, "depth" | "candidates"> & { candidates: ImageHistoryCandidate[] }>,
  seen = new Set<string>(),
): number {
  if (seen.has(branchId)) {
    return 0;
  }
  seen.add(branchId);
  const branch = branchesById.get(branchId);
  if (!branch?.parent_group_id) {
    return 0;
  }
  return calculateBranchDepth(branch.parent_group_id, branchesById, seen) + 1;
}

function sortBranches(branches: ImageHistoryBranch[]): ImageHistoryBranch[] {
  const childrenByParent = new Map<string | null, ImageHistoryBranch[]>();
  for (const branch of branches) {
    const siblings = childrenByParent.get(branch.parent_group_id) ?? [];
    siblings.push(branch);
    childrenByParent.set(branch.parent_group_id, siblings);
  }
  for (const siblings of childrenByParent.values()) {
    siblings.sort((left, right) => compareCreatedAt(left.created_at, right.created_at) || left.id.localeCompare(right.id));
  }

  const ordered: ImageHistoryBranch[] = [];
  const appendBranch = (branch: ImageHistoryBranch) => {
    ordered.push(branch);
    for (const child of childrenByParent.get(branch.id) ?? []) {
      appendBranch(child);
    }
  };
  for (const root of childrenByParent.get(null) ?? []) {
    appendBranch(root);
  }

  const orderedIds = new Set(ordered.map((branch) => branch.id));
  for (const branch of branches) {
    if (!orderedIds.has(branch.id)) {
      ordered.push(branch);
    }
  }
  return ordered;
}

export function buildImageSessionHistoryTree(
  rounds: ImageSessionRound[],
  tasks: ImageSessionGenerationTask[],
): ImageHistoryBranch[] {
  const branchesById = new Map<
    string,
    Omit<ImageHistoryBranch, "depth" | "candidates"> & { candidates: ImageHistoryCandidate[] }
  >();
  const assetGroupByAssetId = new Map<string, string>();

  const ensureBranch = (input: {
    id: string;
    base_asset_id: string | null;
    prompt: string;
    created_at: string;
  }) => {
    const existing = branchesById.get(input.id);
    if (existing) {
      return existing;
    }
    const parentGroupId = input.base_asset_id ? (assetGroupByAssetId.get(input.base_asset_id) ?? null) : null;
    const branch = {
      id: input.id,
      base_asset_id: input.base_asset_id,
      parent_group_id: parentGroupId,
      prompt: input.prompt,
      created_at: input.created_at,
      candidates: [],
    };
    branchesById.set(input.id, branch);
    return branch;
  };

  for (const round of [...rounds].sort((left, right) => compareCreatedAt(left.created_at, right.created_at))) {
    const groupId = getRoundGroupId(round);
    const branch = ensureBranch({
      id: groupId,
      base_asset_id: round.base_asset_id,
      prompt: round.prompt,
      created_at: round.created_at,
    });
    branch.candidates.push({
      id: round.generated_asset.id,
      kind: "round",
      group_id: groupId,
      candidate_index: round.candidate_index,
      candidate_count: round.candidate_count,
      status: "succeeded",
      round,
      prompt: round.prompt,
      size: round.size,
      base_asset_id: round.base_asset_id,
      provider_notes: round.provider_notes,
      failure_reason: null,
      created_at: round.created_at,
    });
    assetGroupByAssetId.set(round.generated_asset.id, groupId);
  }

  for (const task of [...tasks].sort((left, right) => compareCreatedAt(left.created_at, right.created_at))) {
    if (task.status === "succeeded" && !task.result_generation_group_id) {
      continue;
    }
    const groupId = getTaskGroupId(task);
    const branch = ensureBranch({
      id: groupId,
      base_asset_id: task.base_asset_id,
      prompt: task.prompt,
      created_at: task.created_at,
    });
    const existingCandidateIndexes = new Set(
      branch.candidates
        .filter((candidate) => candidate.kind === "round")
        .map((candidate) => candidate.candidate_index),
    );
    const total = clampGenerationCount(task.generation_count || 1);
    for (let candidateIndex = 1; candidateIndex <= total; candidateIndex += 1) {
      if (existingCandidateIndexes.has(candidateIndex)) {
        continue;
      }
      branch.candidates.push({
        id: getImageGenerationTaskPlaceholderId(task, candidateIndex),
        kind: "placeholder",
        group_id: groupId,
        task_id: task.id,
        candidate_index: candidateIndex,
        candidate_count: total,
        status: getPlaceholderCandidateStatus(task, candidateIndex),
        task_status: task.status,
        task,
        prompt: task.prompt,
        size: task.size,
        base_asset_id: task.base_asset_id,
        provider_notes: task.provider_notes,
        failure_reason: task.failure_reason,
        created_at: task.created_at,
      });
    }
  }

  const branches = [...branchesById.entries()].map(([id, branch]) => ({
    ...branch,
    id,
    depth: calculateBranchDepth(id, branchesById),
    candidates: [...branch.candidates].sort(sortCandidates),
  }));
  return sortBranches(branches);
}

export function requiresImageSessionGenerationBase(
  rounds: ImageSessionRound[],
  tasks: ImageSessionGenerationTask[],
): boolean {
  return rounds.length > 0 || tasks.length > 0;
}

export function findImageHistoryPlaceholder(
  branches: ImageHistoryBranch[],
  placeholderId: string | null,
): ImageHistoryPlaceholderCandidate | null {
  if (!placeholderId) {
    return null;
  }
  for (const branch of branches) {
    for (const candidate of branch.candidates) {
      if (candidate.kind === "placeholder" && candidate.id === placeholderId) {
        return candidate;
      }
    }
  }
  return null;
}

function parseImageGenerationTaskPlaceholderId(
  placeholderId: string | null,
): { taskId: string; candidateIndex: number } | null {
  const match = placeholderId?.match(/^task:([^:]+):candidate:(\d+)$/);
  if (!match) {
    return null;
  }
  const candidateIndex = Number(match[2]);
  if (!Number.isInteger(candidateIndex) || candidateIndex < 1) {
    return null;
  }
  return { taskId: match[1], candidateIndex };
}

export function findImageGenerationTaskPlaceholderRound(
  rounds: ImageSessionRound[],
  tasks: ImageSessionGenerationTask[],
  placeholderId: string | null,
): ImageSessionRound | null {
  const parsed = parseImageGenerationTaskPlaceholderId(placeholderId);
  if (!parsed) {
    return null;
  }
  const task = tasks.find((item) => item.id === parsed.taskId);
  if (!task?.result_generation_group_id) {
    return null;
  }
  return (
    rounds.find(
      (round) =>
        getRoundGroupId(round) === task.result_generation_group_id &&
        round.candidate_index === parsed.candidateIndex,
    ) ?? null
  );
}

export function clampGenerationCount(value: number): number {
  if (!Number.isFinite(value)) {
    return 1;
  }
  return Math.min(4, Math.max(1, Math.round(value)));
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

export function isImageSessionGenerationTaskActive(task: ImageSessionGenerationTask): boolean {
  return task.status === "queued" || task.status === "running";
}

export function isImageSessionGenerationTaskRetryable(task: ImageSessionGenerationTask): boolean {
  return task.status === "failed" && task.is_retryable;
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

export function mergeImageSessionStatusIntoDetail(
  detail: ImageSessionDetail,
  status: ImageSessionStatus,
): ImageSessionDetail {
  return {
    ...detail,
    title: status.title,
    updated_at: status.updated_at,
    generation_tasks: status.generation_tasks,
  };
}

export function shouldRefreshImageSessionDetailFromStatus(
  detail: ImageSessionDetail | undefined,
  status: ImageSessionStatus,
): boolean {
  if (!detail) {
    return false;
  }
  if (status.rounds_count > detail.rounds.length) {
    return true;
  }
  if (status.latest_round_id && !detail.rounds.some((round) => round.id === status.latest_round_id)) {
    return true;
  }
  const previousTasksById = new Map(detail.generation_tasks.map((task) => [task.id, task]));
  return status.generation_tasks.some((task) => {
    const previousTask = previousTasksById.get(task.id);
    return Boolean(previousTask && isImageSessionGenerationTaskActive(previousTask) && !isImageSessionGenerationTaskActive(task));
  });
}

function normalizeSubmitToolOptions(toolOptions: ImageToolOptions | null | undefined): Record<string, unknown> | null {
  if (!toolOptions) {
    return null;
  }
  const entries = Object.entries(toolOptions)
    .filter(([, value]) => value !== undefined && value !== null && value !== "")
    .sort(([leftKey], [rightKey]) => leftKey.localeCompare(rightKey));
  return entries.length ? Object.fromEntries(entries) : null;
}

export function buildImageGenerationSubmitSignature(payload: ImageGenerationSubmitPayload): string {
  return JSON.stringify({
    prompt: payload.prompt.trim(),
    size: payload.size,
    base_asset_id: payload.base_asset_id ?? null,
    selected_reference_asset_ids: payload.selected_reference_asset_ids,
    generation_count: clampGenerationCount(payload.generation_count),
    tool_options: normalizeSubmitToolOptions(payload.tool_options),
  });
}

export function shouldBlockDuplicateGenerationSubmit(
  previous: ImageGenerationSubmitGuard | null,
  nextSignature: string,
  now: number,
  windowMs = 1800,
): boolean {
  if (!previous || previous.signature !== nextSignature) {
    return false;
  }
  return now - previous.submittedAt < windowMs;
}
