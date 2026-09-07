/**
 * 商品工作台本地操作态：侧栏、选中节点、画布收起。
 *
 * 会话切换或刷新不应把人正在看的面打回空闲。视口仍由 canvasState 单独保存。
 */

import type { GraphProjection } from "../../../lib/types";

export const WORKBENCH_SIDEBAR_TOOLS = ["agent", "add", "details", "runs", "library", "recipes"] as const;
export type WorkbenchSidebarToolId = (typeof WORKBENCH_SIDEBAR_TOOLS)[number];

export type WorkbenchMainViewPreference = "flow" | "results";

export interface WorkbenchUiState {
  sidebarTool?: WorkbenchSidebarToolId;
  selectedNodeIds?: string[];
  chromeCollapsed?: boolean;
  inspectorCollapsed?: boolean;
  enteredGroupId?: string | null;
  filmstripVisible?: boolean;
  /** 用户显式切换后的主视图；缺省时走条件默认，不把打开初值写成偏好。 */
  mainView?: WorkbenchMainViewPreference;
}

const STORAGE_PREFIX = "productflow.workbench.ui.v1:";
const MAX_SELECTED_NODE_IDS = 20;

export function workbenchUiStorageKey(productId: string): string {
  return `${STORAGE_PREFIX}${productId}`;
}

export function parseWorkbenchSidebarTool(value: unknown): WorkbenchSidebarToolId | null {
  return typeof value === "string" && (WORKBENCH_SIDEBAR_TOOLS as readonly string[]).includes(value)
    ? value as WorkbenchSidebarToolId
    : null;
}

export function parseWorkbenchMainView(value: unknown): WorkbenchMainViewPreference | null {
  return value === "flow" || value === "results" ? value : null;
}

export function parseWorkbenchUiState(raw: string | null): WorkbenchUiState {
  if (!raw) return {};
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return {};
    const record = parsed as Record<string, unknown>;
    const selectedNodeIds = parseSelectedNodeIds(record.selectedNodeIds);
    const sidebarTool = parseWorkbenchSidebarTool(record.sidebarTool) ?? undefined;
    const enteredGroupId = parseOptionalId(record.enteredGroupId);
    const mainView = parseWorkbenchMainView(record.mainView) ?? undefined;
    return {
      ...(sidebarTool ? { sidebarTool } : {}),
      ...(selectedNodeIds.length ? { selectedNodeIds } : {}),
      ...(typeof record.chromeCollapsed === "boolean" ? { chromeCollapsed: record.chromeCollapsed } : {}),
      ...(typeof record.inspectorCollapsed === "boolean" ? { inspectorCollapsed: record.inspectorCollapsed } : {}),
      ...(enteredGroupId !== undefined ? { enteredGroupId } : {}),
      ...(typeof record.filmstripVisible === "boolean" ? { filmstripVisible: record.filmstripVisible } : {}),
      ...(mainView ? { mainView } : {}),
    };
  } catch {
    return {};
  }
}

export function readWorkbenchUiState(productId: string): WorkbenchUiState {
  if (typeof window === "undefined" || !productId) return {};
  try {
    return parseWorkbenchUiState(window.localStorage.getItem(workbenchUiStorageKey(productId)));
  } catch {
    return {};
  }
}

export function patchWorkbenchUiState(productId: string, patch: WorkbenchUiState): void {
  if (typeof window === "undefined" || !productId) return;
  try {
    const next = { ...readWorkbenchUiState(productId), ...patch };
    window.localStorage.setItem(workbenchUiStorageKey(productId), JSON.stringify(next));
  } catch {
    // 隐私模式或配额错误只影响本地写入
  }
}

/** 清除主视图显式偏好，下次打开回退条件默认。 */
export function clearWorkbenchMainViewPreference(productId: string): void {
  if (typeof window === "undefined" || !productId) return;
  try {
    const current = readWorkbenchUiState(productId);
    if (current.mainView === undefined) return;
    const rest: WorkbenchUiState = { ...current };
    delete rest.mainView;
    window.localStorage.setItem(workbenchUiStorageKey(productId), JSON.stringify(rest));
  } catch {
    // 隐私模式或配额错误只影响本地写入
  }
}

export function existingWorkbenchNodeIds(
  graph: Pick<GraphProjection, "nodes"> | null | undefined,
  nodeIds: readonly string[] | undefined,
): string[] {
  if (!nodeIds?.length) return [];
  if (!graph) return [...nodeIds].slice(0, MAX_SELECTED_NODE_IDS);
  const known = new Set(graph.nodes.map((node) => node.id));
  return nodeIds.filter((nodeId) => known.has(nodeId)).slice(0, MAX_SELECTED_NODE_IDS);
}

export function existingWorkbenchGroupId(
  graph: Pick<GraphProjection, "groups"> | null | undefined,
  groupId: string | null | undefined,
): string | null {
  if (!groupId || !graph) return null;
  return graph.groups.some((group) => group.id === groupId) ? groupId : null;
}

export function sameWorkbenchIds(left: readonly string[], right: readonly string[]): boolean {
  return left.length === right.length && left.every((id, index) => id === right[index]);
}

export function keepAgentWorkbenchPlaceholder<T>(
  previousData: T | undefined,
  previousQuery: { queryKey: readonly unknown[] } | undefined,
  productId: string,
): T | undefined {
  if (!previousData || !previousQuery || !productId) return undefined;
  return previousQuery.queryKey[1] === productId ? previousData : undefined;
}

function parseSelectedNodeIds(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  const ids = value.filter((item): item is string => typeof item === "string" && item.trim().length > 0);
  return ids.length === value.length ? ids.slice(0, MAX_SELECTED_NODE_IDS) : [];
}

function parseOptionalId(value: unknown): string | null | undefined {
  if (value === null) return null;
  if (typeof value === "string" && value.trim()) return value;
  return undefined;
}
