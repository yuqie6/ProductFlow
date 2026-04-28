import { describe, expect, it } from "vitest";

import type { ImageSessionAsset, ImageSessionDetail, ImageSessionGenerationTask, ImageSessionRound, ImageSessionStatus } from "../../lib/types";
import {
  clampGenerationCount,
  compactImageToolOptions,
  groupImageSessionRounds,
  isImageSessionGenerationTaskActive,
  mergeImageSessionStatusIntoDetail,
  pruneSelectedReferenceIds,
  selectVisibleGenerationTasks,
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
      background: "transparent",
      action: "generate",
      input_fidelity: "high",
      partial_images: 3,
      n: 1,
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
});
