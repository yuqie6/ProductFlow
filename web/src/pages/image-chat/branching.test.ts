import { describe, expect, it } from "vitest";

import type { ImageSessionAsset, ImageSessionDetail, ImageSessionGenerationTask, ImageSessionRound, ImageSessionStatus } from "../../lib/types";
import {
  buildImageGenerationSubmitSignature,
  buildImageSessionHistoryTree,
  clampGenerationCount,
  compactImageToolOptions,
  findImageGenerationTaskPlaceholderRound,
  findImageHistoryPlaceholder,
  groupImageSessionRounds,
  isImageSessionGenerationTaskActive,
  mergeImageSessionStatusIntoDetail,
  pruneSelectedReferenceIds,
  requiresImageSessionGenerationBase,
  selectVisibleGenerationTasks,
  shouldBlockDuplicateGenerationSubmit,
  shouldRefreshImageSessionDetailFromStatus,
} from "./branching";

const createdAt = "2026-04-27T00:00:00Z";

function asset(id: string): ImageSessionAsset {
  return {
    id,
    kind: "generated_image",
    original_filename: `${id}.png`,
    mime_type: "image/png",
    download_url: `/download/${id}`,
    preview_url: `/preview/${id}`,
    thumbnail_url: `/thumb/${id}`,
    created_at: createdAt,
  };
}

function round(overrides: Partial<ImageSessionRound>): ImageSessionRound {
  return {
    id: "round-1",
    prompt: "prompt",
    assistant_message: "done",
    size: "1024x1024",
    model_name: "mock",
    provider_name: "mock",
    prompt_version: "v1",
    provider_response_id: null,
    previous_response_id: null,
    image_generation_call_id: null,
    generation_group_id: null,
    candidate_index: 1,
    candidate_count: 1,
    base_asset_id: null,
    selected_reference_asset_ids: [],
    actual_size: null,
    provider_notes: [],
    generated_asset: asset("asset-1"),
    created_at: createdAt,
    ...overrides,
  };
}

function task(overrides: Partial<ImageSessionGenerationTask>): ImageSessionGenerationTask {
  return {
    id: "task-1",
    session_id: "session-1",
    status: "succeeded",
    prompt: "prompt",
    size: "1024x1024",
    base_asset_id: null,
    selected_reference_asset_ids: [],
    generation_count: 1,
    completed_candidates: 0,
    active_candidate_index: null,
    progress_phase: null,
    progress_updated_at: null,
    provider_response_id: null,
    provider_response_status: null,
    progress_metadata: null,
    failure_reason: null,
    result_generation_group_id: null,
    tool_options: null,
    provider_notes: [],
    created_at: createdAt,
    started_at: null,
    finished_at: null,
    queue_active_count: 0,
    queue_running_count: 0,
    queue_queued_count: 0,
    queue_max_concurrent_tasks: 3,
    queued_ahead_count: null,
    queue_position: null,
    ...overrides,
  };
}

function detail(overrides: Partial<ImageSessionDetail>): ImageSessionDetail {
  return {
    id: "session-1",
    product_id: null,
    title: "会话",
    assets: [],
    rounds: [],
    generation_tasks: [],
    created_at: createdAt,
    updated_at: createdAt,
    ...overrides,
  };
}

function status(overrides: Partial<ImageSessionStatus>): ImageSessionStatus {
  return {
    id: "session-1",
    product_id: null,
    title: "会话",
    rounds_count: 0,
    latest_round_id: null,
    latest_generation_group_id: null,
    has_active_generation_task: false,
    generation_tasks: [],
    created_at: createdAt,
    updated_at: createdAt,
    ...overrides,
  };
}

describe("image chat branching helpers", () => {
  it("groups multi-candidate rounds by generation group and sorts candidates", () => {
    const groups = groupImageSessionRounds([
      round({ id: "r1", generation_group_id: "g1", candidate_index: 2, generated_asset: asset("a2") }),
      round({ id: "r0", generation_group_id: "g1", candidate_index: 1, generated_asset: asset("a1") }),
      round({ id: "r2", generation_group_id: null, generated_asset: asset("a3") }),
    ]);

    expect(groups.map((group) => group.id)).toEqual(["g1", "r2"]);
    expect(groups[0].rounds.map((item) => item.generated_asset.id)).toEqual(["a1", "a2"]);
  });

  it("keeps generation count in the MVP range", () => {
    expect(clampGenerationCount(0)).toBe(1);
    expect(clampGenerationCount(3.6)).toBe(4);
    expect(clampGenerationCount(10)).toBe(4);
  });

  it("builds a lightweight branch tree with task-derived placeholders", () => {
    const branches = buildImageSessionHistoryTree(
      [
        round({
          id: "root-round",
          generation_group_id: "root-group",
          generated_asset: asset("root-asset"),
          candidate_index: 1,
          candidate_count: 1,
        }),
      ],
      [
        task({
          id: "task-branch",
          status: "running",
          base_asset_id: "root-asset",
          generation_count: 4,
          completed_candidates: 1,
          active_candidate_index: 2,
          created_at: "2026-04-27T00:01:00Z",
        }),
      ],
    );

    expect(branches).toHaveLength(2);
    expect(branches.map((branch) => [branch.id, branch.depth, branch.parent_group_id])).toEqual([
      ["root-group", 0, null],
      ["task:task-branch", 1, "root-group"],
    ]);
    expect(branches[1].candidates).toHaveLength(4);
    expect(branches[1].candidates.map((candidate) => candidate.status)).toEqual([
      "completed",
      "running",
      "running",
      "running",
    ]);
  });

  it("keeps completed rounds in the same task branch and only fills missing candidates with placeholders", () => {
    const branches = buildImageSessionHistoryTree(
      [
        round({
          id: "root-round",
          generation_group_id: "root-group",
          generated_asset: asset("root-asset"),
        }),
        round({
          id: "branch-round-1",
          generation_group_id: "branch-group",
          base_asset_id: "root-asset",
          generated_asset: asset("branch-asset-1"),
          candidate_index: 1,
          candidate_count: 3,
          created_at: "2026-04-27T00:02:00Z",
        }),
      ],
      [
        task({
          id: "task-branch",
          status: "running",
          base_asset_id: "root-asset",
          generation_count: 3,
          completed_candidates: 1,
          result_generation_group_id: "branch-group",
          created_at: "2026-04-27T00:01:00Z",
        }),
      ],
    );

    const branch = branches.find((item) => item.id === "branch-group");
    expect(branch?.depth).toBe(1);
    expect(branch?.candidates.map((candidate) => [candidate.kind, candidate.candidate_index])).toEqual([
      ["round", 1],
      ["placeholder", 2],
      ["placeholder", 3],
    ]);
    expect(findImageHistoryPlaceholder(branches, "task:task-branch:candidate:2")?.candidate_index).toBe(2);
  });

  it("creates placeholders for queued and failed generation tasks", () => {
    const branches = buildImageSessionHistoryTree([], [
      task({
        id: "queued-task",
        status: "queued",
        generation_count: 2,
        created_at: "2026-04-27T00:01:00Z",
      }),
      task({
        id: "failed-task",
        status: "failed",
        generation_count: 3,
        failure_reason: "provider failed",
        created_at: "2026-04-27T00:02:00Z",
      }),
    ]);

    expect(branches.map((branch) => branch.id)).toEqual(["task:queued-task", "task:failed-task"]);
    expect(branches[0].candidates.map((candidate) => candidate.status)).toEqual(["queued", "queued"]);
    expect(branches[1].candidates.map((candidate) => candidate.status)).toEqual(["failed", "failed", "failed"]);
    expect(findImageHistoryPlaceholder(branches, "task:failed-task:candidate:3")?.failure_reason).toBe(
      "provider failed",
    );
  });

  it("requires a generated base after any prior round or generation task", () => {
    expect(requiresImageSessionGenerationBase([], [])).toBe(false);
    expect(
      requiresImageSessionGenerationBase(
        [
          round({
            id: "root-round",
            generated_asset: asset("root-asset"),
          }),
        ],
        [],
      ),
    ).toBe(true);
    expect(
      requiresImageSessionGenerationBase(
        [],
        [
          task({
            id: "queued-task",
            status: "queued",
          }),
        ],
      ),
    ).toBe(true);
  });

  it("finds the generated round that replaces a selected task placeholder", () => {
    const rounds = [
      round({
        id: "branch-round-2",
        generation_group_id: "branch-group",
        generated_asset: asset("branch-asset-2"),
        candidate_index: 2,
        candidate_count: 3,
      }),
    ];
    const tasks = [
      task({
        id: "task-branch",
        generation_count: 3,
        result_generation_group_id: "branch-group",
      }),
    ];

    expect(findImageGenerationTaskPlaceholderRound(rounds, tasks, "task:task-branch:candidate:2")?.generated_asset.id).toBe(
      "branch-asset-2",
    );
    expect(findImageGenerationTaskPlaceholderRound(rounds, tasks, "task:task-branch:candidate:3")).toBeNull();
    expect(findImageGenerationTaskPlaceholderRound(rounds, tasks, "not-a-placeholder")).toBeNull();
  });

  it("compacts optional provider fields before submitting a generation", () => {
    expect(
      compactImageToolOptions({
        model: "  gpt-image-2 ",
        quality: "high",
        output_format: null,
        output_compression: 101,
        background: "transparent",
        moderation: null,
        action: "generate",
        input_fidelity: "high",
        partial_images: 4,
        n: 0,
      }),
    ).toEqual({
      model: "gpt-image-2",
      quality: "high",
      output_compression: 100,
      action: "generate",
      input_fidelity: "high",
      partial_images: 3,
      n: 1,
    });
    expect(compactImageToolOptions({ background: "transparent" }, ["background"] as const)).toEqual({
      background: "transparent",
    });
    expect(compactImageToolOptions({})).toBeUndefined();
  });

  it("prunes deleted reference ids and removes duplicates", () => {
    expect(pruneSelectedReferenceIds(["ref-1", "ref-2", "ref-1", "gone"], ["ref-1", "ref-2"])).toEqual([
      "ref-1",
      "ref-2",
    ]);
    expect(pruneSelectedReferenceIds(["ref-1", "ref-2", "ref-3"], ["ref-1", "ref-2", "ref-3"], 2)).toEqual([
      "ref-1",
      "ref-2",
    ]);
  });

  it("keeps active and failed generation tasks visible before old succeeded tasks", () => {
    const tasks = selectVisibleGenerationTasks([
      task({ id: "old-success-1", status: "succeeded", created_at: "2026-04-27T00:00:01Z" }),
      task({ id: "old-success-2", status: "succeeded", created_at: "2026-04-27T00:00:02Z" }),
      task({ id: "old-success-3", status: "succeeded", created_at: "2026-04-27T00:00:03Z" }),
      task({ id: "old-success-4", status: "succeeded", created_at: "2026-04-27T00:00:04Z" }),
      task({ id: "queued-new", status: "queued", created_at: "2026-04-27T00:00:05Z" }),
      task({ id: "running-new", status: "running", created_at: "2026-04-27T00:00:06Z" }),
      task({ id: "failed-new", status: "failed", created_at: "2026-04-27T00:00:07Z" }),
    ]);

    expect(tasks.map((item) => item.id)).toEqual([
      "running-new",
      "queued-new",
      "failed-new",
      "old-success-4",
    ]);
  });

  it("detects active generation tasks", () => {
    expect(isImageSessionGenerationTaskActive(task({ status: "queued" }))).toBe(true);
    expect(isImageSessionGenerationTaskActive(task({ status: "running" }))).toBe(true);
    expect(isImageSessionGenerationTaskActive(task({ status: "succeeded" }))).toBe(false);
    expect(isImageSessionGenerationTaskActive(task({ status: "failed" }))).toBe(false);
  });

  it("merges lightweight session status into cached detail without replacing rounds and assets", () => {
    const cached = detail({
      title: "旧标题",
      assets: [asset("asset-1")],
      rounds: [round({ id: "round-1" })],
      generation_tasks: [task({ id: "task-1", status: "queued" })],
      updated_at: "2026-04-27T00:00:00Z",
    });
    const merged = mergeImageSessionStatusIntoDetail(
      cached,
      status({
        title: "新标题",
        generation_tasks: [task({ id: "task-1", status: "running", queue_running_count: 1 })],
        updated_at: "2026-04-27T00:00:10Z",
      }),
    );

    expect(merged.title).toBe("新标题");
    expect(merged.assets).toBe(cached.assets);
    expect(merged.rounds).toBe(cached.rounds);
    expect(merged.generation_tasks[0].status).toBe("running");
    expect(merged.updated_at).toBe("2026-04-27T00:00:10Z");
  });

  it("refreshes full detail when lightweight status reaches terminal state or new rounds", () => {
    const cached = detail({
      rounds: [round({ id: "round-1" })],
      generation_tasks: [task({ id: "task-1", status: "running" })],
    });

    expect(
      shouldRefreshImageSessionDetailFromStatus(
        cached,
        status({ rounds_count: 1, latest_round_id: "round-1", generation_tasks: [task({ id: "task-1", status: "running" })] }),
      ),
    ).toBe(false);
    expect(
      shouldRefreshImageSessionDetailFromStatus(
        cached,
        status({ rounds_count: 1, latest_round_id: "round-1", generation_tasks: [task({ id: "task-1", status: "failed" })] }),
      ),
    ).toBe(true);
    expect(
      shouldRefreshImageSessionDetailFromStatus(
        cached,
        status({ rounds_count: 2, latest_round_id: "round-2", generation_tasks: [task({ id: "task-1", status: "succeeded" })] }),
      ),
    ).toBe(true);
  });

  it("builds duplicate-submit signatures from every generation input that changes output", () => {
    const payload = {
      prompt: "  prompt  ",
      size: "1024x1024",
      base_asset_id: "base-1",
      selected_reference_asset_ids: ["ref-1", "ref-2"],
      generation_count: 2,
      tool_options: {
        quality: "high" as const,
        output_format: "png" as const,
      },
    };

    const signature = buildImageGenerationSubmitSignature(payload);
    expect(signature).toBe(
      buildImageGenerationSubmitSignature({
        ...payload,
        prompt: "prompt",
        tool_options: {
          output_format: "png",
          quality: "high",
        },
      }),
    );
    expect(signature).not.toBe(buildImageGenerationSubmitSignature({ ...payload, prompt: "changed" }));
    expect(signature).not.toBe(buildImageGenerationSubmitSignature({ ...payload, size: "1536x1024" }));
    expect(signature).not.toBe(buildImageGenerationSubmitSignature({ ...payload, base_asset_id: null }));
    expect(signature).not.toBe(
      buildImageGenerationSubmitSignature({ ...payload, selected_reference_asset_ids: ["ref-2", "ref-1"] }),
    );
    expect(signature).not.toBe(buildImageGenerationSubmitSignature({ ...payload, generation_count: 3 }));
    expect(signature).not.toBe(
      buildImageGenerationSubmitSignature({ ...payload, tool_options: { quality: "low", output_format: "png" } }),
    );
  });

  it("blocks identical duplicate submits only inside the short guard window", () => {
    const signature = buildImageGenerationSubmitSignature({
      prompt: "prompt",
      size: "1024x1024",
      base_asset_id: null,
      selected_reference_asset_ids: [],
      generation_count: 1,
      tool_options: null,
    });

    expect(shouldBlockDuplicateGenerationSubmit({ signature, submittedAt: 1_000 }, signature, 2_000, 1_800)).toBe(true);
    expect(shouldBlockDuplicateGenerationSubmit({ signature, submittedAt: 1_000 }, signature, 3_000, 1_800)).toBe(false);
    expect(shouldBlockDuplicateGenerationSubmit({ signature: "other", submittedAt: 1_000 }, signature, 1_200, 1_800)).toBe(
      false,
    );
  });
});
