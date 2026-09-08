import { describe, expect, it } from "vitest";
import { imageChatRouteScope, parseImageSessionRoute, readImageChatRouteState, writeImageChatRouteState, type ImageChatRouteState } from "./routeState";
describe("explicit image session route and account cache", () => {
  it("distinguishes absent and invalid explicit addresses, preserving opaque ids", () => {
    expect(parseImageSessionRoute(new URLSearchParams())).toEqual({ explicit: false, sessionId: null });
    for (const query of ["image_session_id=", "image_session_id=%20", "image_session_id=a&image_session_id=b"]) expect(parseImageSessionRoute(new URLSearchParams(query))).toEqual({ explicit: true, sessionId: null });
    expect(parseImageSessionRoute(new URLSearchParams("image_session_id=second%2Fpage"))).toEqual({ explicit: true, sessionId: "second/page" });
  });
  it("isolates account, merchant and login generation and clears image selection across targets", () => {
    const scope = imageChatRouteScope("alice", "shop-a", 1);
    const draft: ImageChatRouteState = { leftPanelWidth: 328, rightPanelWidth: 320, historyPanelHeight: 176, selectedSessionId: "a", selectedGeneratedAssetId: "image", selectedTaskPlaceholderId: "placeholder", branchBaseAssetId: "base", selectedReferenceAssetIds: ["ref"], generationCount: 2, draft: "private", size: "1024x1024", toolOptions: {}, settingsTab: "basic", targetProductId: "p" };
    writeImageChatRouteState(scope, draft);
    expect(readImageChatRouteState(scope, "a")).toEqual(draft);
    expect(readImageChatRouteState(scope, "b")).toMatchObject({ selectedSessionId: "b", selectedGeneratedAssetId: null, selectedTaskPlaceholderId: null, branchBaseAssetId: null, selectedReferenceAssetIds: [] });
    for (const other of [imageChatRouteScope("bob", "shop-a", 1), imageChatRouteScope("alice", "shop-b", 1), imageChatRouteScope("alice", "shop-a", 2)]) expect(readImageChatRouteState(other)).toBeUndefined();
    readImageChatRouteState(scope)!.selectedReferenceAssetIds.push("external");
    expect(readImageChatRouteState(scope)!.selectedReferenceAssetIds).toEqual(["ref"]);
    const nextLogin = imageChatRouteScope("alice", "shop-a", 2);
    writeImageChatRouteState(nextLogin, { ...draft, draft: "new login" });
    expect(readImageChatRouteState(scope)).toBeUndefined();
    expect(readImageChatRouteState(nextLogin)?.draft).toBe("new login");
  });
});
