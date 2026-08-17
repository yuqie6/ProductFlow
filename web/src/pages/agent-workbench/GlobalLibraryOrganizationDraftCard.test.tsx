import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

import type { LibraryOrganizationDraft } from "../../lib/types";
import { GlobalLibraryOrganizationDraftCard } from "./GlobalLibraryOrganizationDraftCard";

function draft(status: LibraryOrganizationDraft["status"]): LibraryOrganizationDraft {
  return {
    id: "draft-1",
    conversation_id: "conversation-1",
    status,
    current_revision: {
      id: "revision-1",
      version: 2,
      schema_version: 1,
      payload_hash: "a".repeat(64),
      source_turn_id: "turn-1",
      source_artifact_step_id: "step-1",
      confirmed_at: status === "confirmed" ? "2026-08-17T00:00:00Z" : null,
      created_at: "2026-08-17T00:00:00Z",
      payload: {
        schema_version: 1,
        confirmation_summary: "整理最近生成的场景图",
        operations: [
          {
            operation: "rename",
            asset_id: "asset-1",
            expected_revision: 4,
            before: {
              revision: 4,
              display_name: "scene-old.png",
              folder_id: null,
              tag_names: [],
              is_archived: false,
            },
            target: { display_name: "scene-hero.png" },
            reason: "统一主图名称",
          },
        ],
      },
    },
    confirmed_revision_id: status === "confirmed" ? "revision-1" : null,
    confirmation_result: null,
    confirmed_at: status === "confirmed" ? "2026-08-17T00:00:00Z" : null,
    created_at: "2026-08-17T00:00:00Z",
    updated_at: "2026-08-17T00:00:00Z",
  };
}

describe("GlobalLibraryOrganizationDraftCard", () => {
  it("shows the impact and explicit confirmation action while awaiting approval", () => {
    const markup = renderToStaticMarkup(
      createElement(GlobalLibraryOrganizationDraftCard, {
        draft: draft("awaiting_confirmation"),
        loading: false,
        error: null,
        busy: false,
        onConfirm: vi.fn(),
      }),
    );

    expect(markup).toContain("整理最近生成的场景图");
    expect(markup).toContain("确认整理");
    expect(markup).toContain("scene-old.png");
    expect(markup).toContain("改名");
  });

  it("keeps the completed state from rendering another confirmation action", () => {
    const markup = renderToStaticMarkup(
      createElement(GlobalLibraryOrganizationDraftCard, {
        draft: draft("confirmed"),
        loading: false,
        error: null,
        busy: false,
        onConfirm: vi.fn(),
      }),
    );

    expect(markup).toContain("整理已完成");
    expect(markup).not.toContain("确认整理");
  });
});
