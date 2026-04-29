export const LEFT_PANEL_DEFAULT_WIDTH = 288;
export const LEFT_PANEL_MIN_WIDTH = 240;
export const LEFT_PANEL_MAX_WIDTH = 384;
export const RIGHT_PANEL_DEFAULT_WIDTH = 320;
export const RIGHT_PANEL_MIN_WIDTH = 300;
export const RIGHT_PANEL_MAX_WIDTH = 440;
export const CENTER_PANEL_MIN_WIDTH = 360;
export const HISTORY_PANEL_DEFAULT_HEIGHT = 176;
export const HISTORY_PANEL_MIN_HEIGHT = 132;
export const HISTORY_PANEL_MAX_HEIGHT = 288;
export const HISTORY_PANEL_MAX_VIEWPORT_RATIO = 0.38;

export interface ImageChatPanelLayout {
  leftPanelWidth: number;
  rightPanelWidth: number;
  historyPanelHeight: number;
}

export function clampPanelSize(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value));
}

export function wheelDeltaToPixels(delta: number, deltaMode: number, pageSize: number) {
  if (deltaMode === 1) {
    return delta * 16;
  }
  if (deltaMode === 2) {
    return delta * pageSize;
  }
  return delta;
}

export function getLeftPanelMaxWidth(viewportWidth: number, rightPanelWidth: number) {
  return Math.max(
    LEFT_PANEL_MIN_WIDTH,
    Math.min(LEFT_PANEL_MAX_WIDTH, viewportWidth - rightPanelWidth - CENTER_PANEL_MIN_WIDTH),
  );
}

export function getRightPanelMaxWidth(viewportWidth: number, leftPanelWidth: number) {
  return Math.max(
    RIGHT_PANEL_MIN_WIDTH,
    Math.min(RIGHT_PANEL_MAX_WIDTH, viewportWidth - leftPanelWidth - CENTER_PANEL_MIN_WIDTH),
  );
}

export function getHistoryPanelMaxHeight(viewportHeight: number) {
  return Math.max(
    HISTORY_PANEL_MIN_HEIGHT,
    Math.min(HISTORY_PANEL_MAX_HEIGHT, Math.round(viewportHeight * HISTORY_PANEL_MAX_VIEWPORT_RATIO)),
  );
}

function clampImageChatSidePanels(leftPanelWidth: number, rightPanelWidth: number, viewportWidth: number) {
  const availableSideWidth = Math.max(
    LEFT_PANEL_MIN_WIDTH + RIGHT_PANEL_MIN_WIDTH,
    viewportWidth - CENTER_PANEL_MIN_WIDTH,
  );
  let nextLeftPanelWidth = clampPanelSize(leftPanelWidth, LEFT_PANEL_MIN_WIDTH, LEFT_PANEL_MAX_WIDTH);
  let nextRightPanelWidth = clampPanelSize(rightPanelWidth, RIGHT_PANEL_MIN_WIDTH, RIGHT_PANEL_MAX_WIDTH);
  let overflow = nextLeftPanelWidth + nextRightPanelWidth - availableSideWidth;

  if (overflow > 0) {
    const rightPanelReduction = Math.min(overflow, nextRightPanelWidth - RIGHT_PANEL_MIN_WIDTH);
    nextRightPanelWidth -= rightPanelReduction;
    overflow -= rightPanelReduction;
  }

  if (overflow > 0) {
    const leftPanelReduction = Math.min(overflow, nextLeftPanelWidth - LEFT_PANEL_MIN_WIDTH);
    nextLeftPanelWidth -= leftPanelReduction;
  }

  return {
    leftPanelWidth: nextLeftPanelWidth,
    rightPanelWidth: nextRightPanelWidth,
  };
}

export function clampImageChatPanelLayout(
  layout: ImageChatPanelLayout,
  viewport: { viewportWidth: number; viewportHeight: number },
): ImageChatPanelLayout {
  const sidePanels = clampImageChatSidePanels(layout.leftPanelWidth, layout.rightPanelWidth, viewport.viewportWidth);

  return {
    ...sidePanels,
    historyPanelHeight: clampPanelSize(
      layout.historyPanelHeight,
      HISTORY_PANEL_MIN_HEIGHT,
      getHistoryPanelMaxHeight(viewport.viewportHeight),
    ),
  };
}
