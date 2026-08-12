import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api } from "../../../lib/api";
import type {
  GalleryAsset,
  GalleryAssetSort,
  GalleryDirectorySelection,
} from "../../../lib/types";
import {
  DEFAULT_GALLERY_DIRECTORY,
  flattenGalleryAssetPages,
  galleryAssetsQueryKey,
  IMAGE_EXPLORER_PAGE_SIZE,
  readImageExplorerView,
  selectLoadedAssets,
  toggleAssetSelection,
  writeImageExplorerView,
  type ImageExplorerView,
} from "./explorerState";

interface MoveAssetsInput {
  assets: GalleryAsset[];
  folderId: string | null;
}

export function useProductImageExplorer(productId: string) {
  const queryClient = useQueryClient();
  const [directory, setDirectory] = useState<GalleryDirectorySelection>(DEFAULT_GALLERY_DIRECTORY);
  const [searchInput, setSearchInput] = useState("");
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState<GalleryAssetSort>("created_desc");
  const [view, setViewState] = useState<ImageExplorerView>(() => readImageExplorerView(productId));
  const [selectedIds, setSelectedIds] = useState<Set<string>>(() => new Set());
  const assetsQueryKey = useMemo(
    () => galleryAssetsQueryKey(productId, directory, query, sort),
    [directory, productId, query, sort],
  );
  const assetsQueryIdentity = `${productId}\u0000${directory.kind}\u0000${directory.key ?? ""}\u0000${query}\u0000${sort}`;
  const previousAssetsQueryRef = useRef({ identity: assetsQueryIdentity, key: assetsQueryKey });

  useEffect(() => {
    const timer = window.setTimeout(() => setQuery(searchInput.trim()), 250);
    return () => window.clearTimeout(timer);
  }, [searchInput]);

  useEffect(() => {
    setSelectedIds(new Set());
  }, [directory.kind, directory.key, query, sort]);

  useEffect(() => {
    const previous = previousAssetsQueryRef.current;
    if (previous.identity === assetsQueryIdentity) {
      return;
    }
    queryClient.removeQueries({ queryKey: previous.key, exact: true });
    previousAssetsQueryRef.current = { identity: assetsQueryIdentity, key: assetsQueryKey };
  }, [assetsQueryIdentity, assetsQueryKey, queryClient]);

  useEffect(() => {
    setViewState(readImageExplorerView(productId));
    setDirectory(DEFAULT_GALLERY_DIRECTORY);
    setSearchInput("");
    setQuery("");
    setSelectedIds(new Set());
  }, [productId]);

  const bootstrapQuery = useQuery({
    queryKey: ["product-image-library", productId],
    queryFn: () => api.getProductImageLibrary(productId),
    enabled: Boolean(productId),
  });

  const assetsQuery = useInfiniteQuery({
    queryKey: assetsQueryKey,
    queryFn: ({ pageParam }) =>
      api.listGalleryAssets(productId, {
        directory_kind: directory.kind,
        directory_key: directory.key,
        q: query,
        sort,
        after: pageParam,
        limit: IMAGE_EXPLORER_PAGE_SIZE,
      }),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    enabled: Boolean(productId),
  });

  const assets = useMemo(
    () => flattenGalleryAssetPages(assetsQuery.data?.pages),
    [assetsQuery.data],
  );
  const selectedAssets = useMemo(
    () => assets.filter((asset) => selectedIds.has(asset.id)),
    [assets, selectedIds],
  );

  const invalidate = useCallback(async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["product-image-library", productId] }),
      queryClient.invalidateQueries({ queryKey: ["product-image-library-assets", productId] }),
    ]);
  }, [productId, queryClient]);

  const uploadMutation = useMutation({
    mutationFn: (files: File[]) => api.addCanonicalProductImages(productId, files),
    onSuccess: invalidate,
  });
  const createFolderMutation = useMutation({
    mutationFn: (name: string) => api.createGalleryFolder(productId, name),
    onSuccess: async (folder) => {
      await invalidate();
      setDirectory({ kind: "user_folder", key: folder.id });
    },
  });
  const renameFolderMutation = useMutation({
    mutationFn: (input: { id: string; expectedName: string; name: string }) =>
      api.renameGalleryFolder(productId, input.id, {
        expected_name: input.expectedName,
        name: input.name,
      }),
    onSuccess: invalidate,
  });
  const deleteFolderMutation = useMutation({
    mutationFn: (input: { id: string; expectedName: string }) =>
      api.deleteGalleryFolder(productId, input.id, input.expectedName),
    onSuccess: async (_result, input) => {
      if (directory.kind === "user_folder" && directory.key === input.id) {
        setDirectory({ kind: "unorganized", key: null });
      }
      setSelectedIds(new Set());
      await invalidate();
    },
  });
  const renameAssetMutation = useMutation({
    mutationFn: (input: { asset: GalleryAsset; displayName: string }) =>
      api.renameGalleryAsset(productId, input.asset.id, {
        expected_display_name: input.asset.display_name,
        display_name: input.displayName,
      }),
    onSuccess: invalidate,
  });
  const moveAssetsMutation = useMutation({
    mutationFn: (input: MoveAssetsInput) =>
      api.moveGalleryAssets(productId, {
        items: input.assets.map((asset) => ({
          asset_id: asset.id,
          expected_folder_id: asset.user_folder_id,
        })),
        folder_id: input.folderId,
      }),
    onSuccess: async () => {
      setSelectedIds(new Set());
      await invalidate();
    },
  });
  const archiveMutation = useMutation({
    mutationFn: (assetIds: string[]) => api.downloadGalleryArchive(productId, assetIds),
  });

  const setView = useCallback((next: ImageExplorerView) => {
    setViewState(next);
    writeImageExplorerView(productId, next);
  }, [productId]);

  const operationError = [
    uploadMutation.error,
    createFolderMutation.error,
    renameFolderMutation.error,
    deleteFolderMutation.error,
    renameAssetMutation.error,
    moveAssetsMutation.error,
    archiveMutation.error,
  ].find((error): error is Error => error instanceof Error) ?? null;

  return {
    bootstrapQuery,
    assetsQuery,
    assets,
    directory,
    setDirectory,
    searchInput,
    setSearchInput,
    sort,
    setSort,
    view,
    setView,
    selectedIds,
    selectedAssets,
    toggleSelected: (assetId: string) => setSelectedIds((current) => toggleAssetSelection(current, assetId)),
    selectAllLoaded: () => setSelectedIds(selectLoadedAssets(assets)),
    clearSelection: () => setSelectedIds(new Set()),
    uploadMutation,
    createFolderMutation,
    renameFolderMutation,
    deleteFolderMutation,
    renameAssetMutation,
    moveAssetsMutation,
    archiveMutation,
    operationError,
  };
}
