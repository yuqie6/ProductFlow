import type { ImageGenerationSettingsTab } from "../../components/ImageGenerationSettingsTabs";
import type { ImageChatPanelLayout } from "./resizableLayout";
import type { ImageToolOptions } from "../../lib/types";

export function parseImageSessionRoute(params: URLSearchParams) {
  const values = params.getAll("image_session_id");
  return { explicit: values.length > 0, sessionId: values.length === 1 && values[0].trim() ? values[0] : null };
}

export interface ImageChatRouteState extends ImageChatPanelLayout {
  selectedSessionId: string | null;
  selectedGeneratedAssetId: string | null;
  selectedTaskPlaceholderId: string | null;
  branchBaseAssetId: string | null;
  selectedReferenceAssetIds: string[];
  generationCount: number;
  draft: string;
  size: string;
  toolOptions: ImageToolOptions;
  settingsTab: ImageGenerationSettingsTab;
  targetProductId: string;
}

const routeStateCache = new Map<string, ImageChatRouteState>();
export function imageChatRouteScope(userId: string, merchantId: string, generation: number) {
  return JSON.stringify([userId, merchantId, generation]);
}
export function readImageChatRouteState(scope: string, explicitSessionId?: string | null): ImageChatRouteState | undefined {
  const cached = routeStateCache.get(scope);
  if (!cached) return undefined;
  const sameSession = explicitSessionId === undefined || explicitSessionId === cached.selectedSessionId;
  return {
    ...cached,
    selectedSessionId: explicitSessionId === undefined ? cached.selectedSessionId : explicitSessionId,
    selectedGeneratedAssetId: sameSession ? cached.selectedGeneratedAssetId : null,
    selectedTaskPlaceholderId: sameSession ? cached.selectedTaskPlaceholderId : null,
    branchBaseAssetId: sameSession ? cached.branchBaseAssetId : null,
    selectedReferenceAssetIds: sameSession ? [...cached.selectedReferenceAssetIds] : [],
    toolOptions: { ...cached.toolOptions },
  };
}
export function writeImageChatRouteState(scope: string, state: ImageChatRouteState) {
  // Only the current account's navigation draft is useful; obsolete login scopes never return.
  routeStateCache.clear();
  routeStateCache.set(scope, { ...state, selectedReferenceAssetIds: [...state.selectedReferenceAssetIds], toolOptions: { ...state.toolOptions } });
}
export function clearImageChatRouteState(scope: string) { routeStateCache.delete(scope); }
