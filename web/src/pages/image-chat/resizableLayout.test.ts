import { describe, expect, it } from "vitest";

import {
  HISTORY_PANEL_MAX_HEIGHT,
  HISTORY_PANEL_MIN_HEIGHT,
  LEFT_PANEL_MIN_WIDTH,
  RIGHT_PANEL_MIN_WIDTH,
  clampImageChatPanelLayout,
  clampPanelSize,
  getHistoryPanelMaxHeight,
  getLeftPanelMaxWidth,
  getRightPanelMaxWidth,
  wheelDeltaToPixels,
} from "./resizableLayout";

describe("image chat resizable layout helpers", () => {
  it("keeps direct drag values inside their min and max bounds", () => {
    expect(clampPanelSize(120, 240, 384)).toBe(240);
    expect(clampPanelSize(320, 240, 384)).toBe(320);
    expect(clampPanelSize(460, 240, 384)).toBe(384);
  });

  it("preserves the center canvas minimum when calculating side panel drag limits", () => {
    expect(getLeftPanelMaxWidth(1024, 320)).toBe(344);
    expect(getRightPanelMaxWidth(1024, 288)).toBe(376);
  });

  it("re-clamps side panels when the desktop viewport shrinks after a resize", () => {
    const layout = clampImageChatPanelLayout(
      {
        leftPanelWidth: 384,
        rightPanelWidth: 440,
        historyPanelHeight: 176,
      },
      {
        viewportWidth: 1024,
        viewportHeight: 900,
      },
    );

    expect(layout.leftPanelWidth).toBe(364);
    expect(layout.rightPanelWidth).toBe(RIGHT_PANEL_MIN_WIDTH);
    expect(layout.leftPanelWidth + layout.rightPanelWidth).toBe(1024 - 360);
  });

  it("keeps panel sizes at their minimums when the viewport cannot provide extra space", () => {
    const layout = clampImageChatPanelLayout(
      {
        leftPanelWidth: 320,
        rightPanelWidth: 360,
        historyPanelHeight: 120,
      },
      {
        viewportWidth: 860,
        viewportHeight: 260,
      },
    );

    expect(layout.leftPanelWidth).toBe(LEFT_PANEL_MIN_WIDTH);
    expect(layout.rightPanelWidth).toBe(RIGHT_PANEL_MIN_WIDTH);
    expect(layout.historyPanelHeight).toBe(HISTORY_PANEL_MIN_HEIGHT);
  });

  it("bounds the history panel height by viewport ratio and hard maximum", () => {
    expect(getHistoryPanelMaxHeight(600)).toBe(228);
    expect(getHistoryPanelMaxHeight(1200)).toBe(HISTORY_PANEL_MAX_HEIGHT);

    expect(
      clampImageChatPanelLayout(
        {
          leftPanelWidth: 288,
          rightPanelWidth: 320,
          historyPanelHeight: 288,
        },
        {
          viewportWidth: 1280,
          viewportHeight: 600,
        },
      ).historyPanelHeight,
    ).toBe(228);
  });

  it("normalizes wheel delta units before vertical wheel-to-horizontal scrolling", () => {
    expect(wheelDeltaToPixels(3, 0, 500)).toBe(3);
    expect(wheelDeltaToPixels(3, 1, 500)).toBe(48);
    expect(wheelDeltaToPixels(1, 2, 500)).toBe(500);
  });
});
