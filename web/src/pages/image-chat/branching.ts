import type {
  ImageSessionDetail,
  ImageSessionGenerationTask,
  ImageSessionRound,
  ImageSessionStatus,
  ImageToolOptions,
} from "../../lib/types";
import { compactImageToolOptions, pruneSelectedReferenceIds } from "../../lib/imageToolOptions";

export { compactImageToolOptions, pruneSelectedReferenceIds };

export interface ImageRoundGroup {
  id: string;
  base_asset_id: string | null;
  prompt: string;
  rounds: ImageSessionRound[];
}

export type ImageHistoryPlaceholderStatus = "queued" | "running" | "completed" | "failed" | "cancelled";

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

export interface ImageGenerationRetryMetadata {
  last_failure_reason?: string;
  last_failure_category?: string;
  last_failure_retryable?: boolean;
  retry_hint?: "retry_later" | "revise_input" | "check_settings";
  auto_retry_attempt?: number;
  max_attempts?: number;
}

export interface ImageSessionSelectionState {
  selectedGeneratedAssetId: string | null;
  selectedTaskPlaceholderId: string | null;
  branchBaseAssetId: string | null;
  selectedReferenceAssetIds: string[];
  pendingGeneratedRoundCount: number | null;
}

export interface ImageSessionSelectionReconciliationInput extends ImageSessionSelectionState {
  rounds: ImageSessionRound[];
  generationTasks: ImageSessionGenerationTask[];
  historyBranches: ImageHistoryBranch[];
  availableReferenceAssetIds: string[];
  maxSelectedReferenceCount: number;
}

export interface ImageSessionSelectionReconciliation extends ImageSessionSelectionState {
  generatedRoundCompleted: boolean;
}

const IMAGE_CHAT_GENERATION_COUNT_MAX = 10;
const IMAGE_CHAT_TASK_CANDIDATE_COUNT_MAX = 10;

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

function taskMatchesSubmitPayload(task: ImageSessionGenerationTask, payload: ImageGenerationSubmitPayload): boolean {
  return (
    buildImageGenerationSubmitSignature({
      prompt: task.prompt,
      size: task.size,
      base_asset_id: task.base_asset_id,
      selected_reference_asset_ids: task.selected_reference_asset_ids,
      generation_count: task.generation_count,
      tool_options: task.tool_options,
    }) === buildImageGenerationSubmitSignature(payload)
  );
}

export function selectImageGenerationTaskNextPlaceholderId(task: ImageSessionGenerationTask): string {
  const candidateIndex = Math.min(
    clampImageGenerationTaskCandidateCount(task.generation_count || 1),
    task.active_candidate_index ?? Math.max(1, task.completed_candidates + 1),
  );
  return getImageGenerationTaskPlaceholderId(task, candidateIndex);
}

export function imageGenerationTaskSubmitPayload(task: ImageSessionGenerationTask): ImageGenerationSubmitPayload {
  return {
    prompt: task.prompt,
    size: task.size,
    base_asset_id: task.base_asset_id,
    selected_reference_asset_ids: task.selected_reference_asset_ids,
    generation_count: clampGenerationCount(task.generation_count),
    tool_options: task.tool_options,
  };
}

export function selectSubmittedImageGenerationTaskPlaceholderId(
  tasks: ImageSessionGenerationTask[],
  payload: ImageGenerationSubmitPayload,
): string | null {
  const newestTasks = [...tasks].sort((left, right) => Date.parse(right.created_at) - Date.parse(left.created_at));
  const matchingTasks = newestTasks.filter((item) => taskMatchesSubmitPayload(item, payload));
  const task =
    matchingTasks.find(isImageSessionGenerationTaskActive) ??
    matchingTasks[0] ??
    newestTasks.find(isImageSessionGenerationTaskActive) ??
    newestTasks[0];
  return task ? selectImageGenerationTaskNextPlaceholderId(task) : null;
}

function getPlaceholderCandidateStatus(
  task: ImageSessionGenerationTask,
  candidateIndex: number,
): ImageHistoryPlaceholderStatus {
  if (task.status === "failed") {
    return "failed";
  }
  if (task.status === "cancelled") {
    return "cancelled";
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
    const total = clampImageGenerationTaskCandidateCount(task.generation_count || 1);
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

function sameStringList(left: readonly string[], right: readonly string[]): boolean {
  return left.length === right.length && left.every((item, index) => item === right[index]);
}

function latestGeneratedAssetId(rounds: ImageSessionRound[]): string | null {
  return rounds.at(-1)?.generated_asset.id ?? null;
}

function generatedAssetIds(rounds: ImageSessionRound[]): Set<string> {
  return new Set(rounds.map((round) => round.generated_asset.id));
}

export function reconcileImageSessionSelection({
  rounds,
  generationTasks,
  historyBranches,
  selectedGeneratedAssetId,
  selectedTaskPlaceholderId,
  branchBaseAssetId,
  selectedReferenceAssetIds,
  availableReferenceAssetIds,
  maxSelectedReferenceCount,
  pendingGeneratedRoundCount,
}: ImageSessionSelectionReconciliationInput): ImageSessionSelectionReconciliation {
  const roundAssetIds = generatedAssetIds(rounds);
  const latestAssetId = latestGeneratedAssetId(rounds);
  const selectedPlaceholderStillExists = Boolean(
    selectedTaskPlaceholderId && findImageHistoryPlaceholder(historyBranches, selectedTaskPlaceholderId),
  );
  const selectedPlaceholderReplacementRound =
    selectedTaskPlaceholderId && !selectedPlaceholderStillExists
      ? findImageGenerationTaskPlaceholderRound(rounds, generationTasks, selectedTaskPlaceholderId)
      : null;
  const selectedPlaceholderWasReplaced = Boolean(selectedTaskPlaceholderId && !selectedPlaceholderStillExists);

  let nextSelectedGeneratedAssetId = selectedGeneratedAssetId;
  let nextSelectedTaskPlaceholderId = selectedTaskPlaceholderId;
  let nextBranchBaseAssetId = branchBaseAssetId;
  let nextPendingGeneratedRoundCount = pendingGeneratedRoundCount;
  let generatedRoundCompleted = false;

  if (selectedTaskPlaceholderId && !selectedPlaceholderStillExists) {
    nextSelectedTaskPlaceholderId = null;
    nextSelectedGeneratedAssetId = selectedPlaceholderReplacementRound?.generated_asset.id ?? latestAssetId;
  } else if (
    !selectedTaskPlaceholderId &&
    (!selectedGeneratedAssetId || !roundAssetIds.has(selectedGeneratedAssetId))
  ) {
    nextSelectedGeneratedAssetId = latestAssetId;
  }

  if (nextBranchBaseAssetId && !roundAssetIds.has(nextBranchBaseAssetId)) {
    nextBranchBaseAssetId = null;
  }
  if (nextSelectedGeneratedAssetId && !nextSelectedTaskPlaceholderId && roundAssetIds.has(nextSelectedGeneratedAssetId)) {
    nextBranchBaseAssetId = nextSelectedGeneratedAssetId;
  }

  const prunedReferenceAssetIds = pruneSelectedReferenceIds(
    selectedReferenceAssetIds,
    availableReferenceAssetIds,
    maxSelectedReferenceCount,
  );

  if (pendingGeneratedRoundCount !== null && rounds.length > pendingGeneratedRoundCount) {
    nextPendingGeneratedRoundCount = null;
    generatedRoundCompleted = true;
    if (!selectedTaskPlaceholderId || selectedPlaceholderWasReplaced) {
      nextSelectedTaskPlaceholderId = null;
      if (!selectedPlaceholderReplacementRound) {
        nextSelectedGeneratedAssetId = latestAssetId;
        if (latestAssetId) {
          nextBranchBaseAssetId = latestAssetId;
        }
      }
    }
  }

  return {
    selectedGeneratedAssetId: nextSelectedGeneratedAssetId,
    selectedTaskPlaceholderId: nextSelectedTaskPlaceholderId,
    branchBaseAssetId: nextBranchBaseAssetId,
    selectedReferenceAssetIds: sameStringList(selectedReferenceAssetIds, prunedReferenceAssetIds)
      ? selectedReferenceAssetIds
      : prunedReferenceAssetIds,
    pendingGeneratedRoundCount: nextPendingGeneratedRoundCount,
    generatedRoundCompleted,
  };
}

export function clampGenerationCount(value: number): number {
  if (!Number.isFinite(value)) {
    return 1;
  }
  return Math.min(IMAGE_CHAT_GENERATION_COUNT_MAX, Math.max(1, Math.round(value)));
}

export function clampImageGenerationTaskCandidateCount(value: number): number {
  if (!Number.isFinite(value)) {
    return 1;
  }
  return Math.min(IMAGE_CHAT_TASK_CANDIDATE_COUNT_MAX, Math.max(1, Math.round(value)));
}

export function effectiveImageGenerationSubmitCount(
  generationCount: number,
  toolOptions: ImageToolOptions | null | undefined,
): number {
  void toolOptions;
  return clampGenerationCount(generationCount);
}

export function isImageSessionGenerationTaskActive(task: ImageSessionGenerationTask): boolean {
  return task.status === "queued" || task.status === "running";
}

export function isImageSessionGenerationTaskRetryable(task: ImageSessionGenerationTask): boolean {
  return task.status === "failed" && task.is_retryable;
}

export function isImageSessionGenerationTaskRegeneratable(task: ImageSessionGenerationTask): boolean {
  return task.status === "cancelled";
}

export function imageGenerationRetryMetadata(task: ImageSessionGenerationTask): ImageGenerationRetryMetadata | null {
  const metadata = task.progress_metadata;
  if (!metadata || typeof metadata !== "object") {
    return null;
  }
  const output: ImageGenerationRetryMetadata = {};
  if (typeof metadata.last_failure_reason === "string" && metadata.last_failure_reason.trim()) {
    output.last_failure_reason = metadata.last_failure_reason;
  }
  if (typeof metadata.last_failure_category === "string" && metadata.last_failure_category.trim()) {
    output.last_failure_category = metadata.last_failure_category;
  }
  if (typeof metadata.last_failure_retryable === "boolean") {
    output.last_failure_retryable = metadata.last_failure_retryable;
  }
  if (
    metadata.retry_hint === "retry_later" ||
    metadata.retry_hint === "revise_input" ||
    metadata.retry_hint === "check_settings"
  ) {
    output.retry_hint = metadata.retry_hint;
  }
  if (typeof metadata.auto_retry_attempt === "number" && Number.isFinite(metadata.auto_retry_attempt)) {
    output.auto_retry_attempt = metadata.auto_retry_attempt;
  }
  if (typeof metadata.max_attempts === "number" && Number.isFinite(metadata.max_attempts)) {
    output.max_attempts = metadata.max_attempts;
  }
  return Object.keys(output).length ? output : null;
}

export function isImageSessionGenerationTaskAutoRetrying(task: ImageSessionGenerationTask): boolean {
  return task.status === "queued" && task.progress_phase === "auto_retry_queued";
}

export function isImageSessionGenerationTaskCancelable(task: ImageSessionGenerationTask): boolean {
  return (task.status === "queued" || task.status === "running") && task.is_cancelable;
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
    generation_count: effectiveImageGenerationSubmitCount(payload.generation_count, payload.tool_options),
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
