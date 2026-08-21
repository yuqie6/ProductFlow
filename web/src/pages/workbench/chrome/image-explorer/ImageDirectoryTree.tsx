import {
  Clock3,
  Folder,
  FolderOpen,
  Image as ImageIcon,
  Pencil,
  Plus,
  Sparkles,
  Trash2,
  Upload,
} from "lucide-react";
import { useState, type DragEvent, type ReactNode } from "react";

import { useI18n } from "../../../../lib/preferences";
import type { TranslationKey } from "../../../../lib/i18n";
import type {
  GalleryBootstrap,
  GalleryDirectorySelection,
  GalleryFolder,
  ProductImageOriginType,
} from "../../../../lib/types";
import {
  decodeAssetDragPayload,
  galleryDirectoryEquals,
  IMAGE_EXPLORER_DRAG_MIME,
} from "./explorerState";

interface ImageDirectoryTreeProps {
  bootstrap: GalleryBootstrap;
  directory: GalleryDirectorySelection;
  onSelect: (directory: GalleryDirectorySelection) => void;
  onCreateFolder: () => void;
  onRenameFolder: (folder: GalleryFolder) => void;
  onDeleteFolder: (folder: GalleryFolder) => void;
  onDropAssets: (assetIds: string[], folderId: string | null) => void;
  busy?: boolean;
}

function originLabelKey(origin: ProductImageOriginType): TranslationKey {
  switch (origin) {
    case "upload":
      return "detail.library.source.upload" as const;
    case "workflow_generation":
      return "detail.library.source.workflow" as const;
    case "image_session_attach":
      return "detail.library.source.session";
    case "legacy_import":
      return "detail.library.source.legacy";
  }
}

export function ImageDirectoryTree({
  bootstrap,
  directory,
  onSelect,
  onCreateFolder,
  onRenameFolder,
  onDeleteFolder,
  onDropAssets,
  busy = false,
}: ImageDirectoryTreeProps) {
  const { t } = useI18n();
  const systemCounts = new Map(bootstrap.system_directories.map((item) => [item.kind, item.count]));
  const systemItems: Array<{
    selection: GalleryDirectorySelection;
    label: string;
    icon: ReactNode;
  }> = [
    { selection: { kind: "all", key: null }, label: t("detail.library.all"), icon: <ImageIcon size={14} /> },
    { selection: { kind: "recent_generated", key: null }, label: t("detail.library.recent"), icon: <Clock3 size={14} /> },
    { selection: { kind: "uploads", key: null }, label: t("detail.library.uploads"), icon: <Upload size={14} /> },
    { selection: { kind: "generated", key: null }, label: t("detail.library.generated"), icon: <Sparkles size={14} /> },
  ];

  return (
    <nav aria-label={t("detail.library.openDirectories")} className="min-w-0 space-y-4">
      <div className="space-y-1">
        {systemItems.map((item) => (
          <DirectoryButton
            key={item.selection.kind}
            active={galleryDirectoryEquals(directory, item.selection)}
            count={systemCounts.get(item.selection.kind) ?? 0}
            icon={item.icon}
            label={item.label}
            onClick={() => onSelect(item.selection)}
          />
        ))}
        <DropDirectoryButton
          active={directory.kind === "unorganized"}
          count={bootstrap.unorganized_count}
          icon={<FolderOpen size={14} />}
          label={t("detail.library.unorganized")}
          targetFolderId={null}
          onClick={() => onSelect({ kind: "unorganized", key: null })}
          onDropAssets={onDropAssets}
          disabled={busy}
        />
      </div>

      {bootstrap.image_types.length ? (
        <DirectorySection title={t("detail.library.types")}>
          {bootstrap.image_types.map((item) => (
            <DirectoryButton
              key={item.directory_key}
              active={directory.kind === "image_type" && directory.key === item.directory_key}
              count={item.count}
              icon={<ImageIcon size={13} />}
              label={item.title}
              onClick={() => onSelect({ kind: "image_type", key: item.directory_key })}
            />
          ))}
        </DirectorySection>
      ) : null}

      {bootstrap.origins.length ? (
        <DirectorySection title={t("detail.library.sources")}>
          {bootstrap.origins.map((item) => (
            <DirectoryButton
              key={item.origin_type}
              active={directory.kind === "source" && directory.key === item.origin_type}
              count={item.count}
              icon={<Upload size={13} />}
              label={t(originLabelKey(item.origin_type))}
              onClick={() => onSelect({ kind: "source", key: item.origin_type })}
            />
          ))}
        </DirectorySection>
      ) : null}

      <DirectorySection
        title={t("detail.library.folders")}
        action={(
          <button
            type="button"
            onClick={onCreateFolder}
            disabled={busy}
            className="inline-flex h-7 w-7 items-center justify-center rounded-md text-slate-500 hover:bg-slate-100 hover:text-slate-950 disabled:opacity-50 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-white"
            title={t("detail.library.createFolder")}
            aria-label={t("detail.library.createFolder")}
          >
            <Plus size={14} />
          </button>
        )}
      >
        {bootstrap.user_folders.map((folder) => (
          <div key={folder.id} className="group flex min-w-0 items-center gap-0.5">
            <div className="min-w-0 flex-1">
              <DropDirectoryButton
                active={directory.kind === "user_folder" && directory.key === folder.id}
                count={folder.count}
                icon={<Folder size={14} />}
                label={folder.name}
                targetFolderId={folder.id}
                onClick={() => onSelect({ kind: "user_folder", key: folder.id })}
                onDropAssets={onDropAssets}
                disabled={busy}
              />
            </div>
            <button
              type="button"
              onClick={() => onRenameFolder(folder)}
              disabled={busy}
              className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-slate-400 opacity-100 transition-opacity hover:bg-slate-100 hover:text-slate-900 focus:opacity-100 disabled:opacity-40 dark:hover:bg-slate-800 dark:hover:text-white"
              title={t("detail.library.renameFolder")}
              aria-label={t("detail.library.renameFolder")}
            >
              <Pencil size={12} />
            </button>
            <button
              type="button"
              onClick={() => onDeleteFolder(folder)}
              disabled={busy}
              className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-slate-400 opacity-100 transition-opacity hover:bg-red-50 hover:text-red-600 focus:opacity-100 disabled:opacity-40 dark:hover:bg-red-500/10 dark:hover:text-red-300"
              title={t("detail.library.deleteFolder")}
              aria-label={t("detail.library.deleteFolder")}
            >
              <Trash2 size={12} />
            </button>
          </div>
        ))}
      </DirectorySection>
    </nav>
  );
}

function DirectorySection({ title, action, children }: { title: string; action?: ReactNode; children: ReactNode }) {
  return (
    <section>
      <div className="mb-1 flex h-7 items-center justify-between gap-2 px-1">
        <h3 className="min-w-0 truncate text-[10px] font-bold uppercase text-slate-400">{title}</h3>
        {action}
      </div>
      <div className="space-y-1">{children}</div>
    </section>
  );
}

function DirectoryButton({
  active,
  count,
  icon,
  label,
  onClick,
}: {
  active: boolean;
  count: number;
  icon: ReactNode;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`flex h-8 w-full min-w-0 items-center gap-2 rounded-md px-2 text-left text-xs transition-colors ${
        active
          ? "bg-indigo-50 font-semibold text-indigo-700 dark:bg-violet-500/15 dark:text-violet-100"
          : "text-slate-600 hover:bg-slate-100 hover:text-slate-950 dark:text-slate-300 dark:hover:bg-slate-800 dark:hover:text-white"
      }`}
      title={label}
    >
      <span className="shrink-0">{icon}</span>
      <span className="min-w-0 flex-1 truncate">{label}</span>
      <span className="shrink-0 tabular-nums text-[10px] text-slate-400">{count}</span>
    </button>
  );
}

function DropDirectoryButton({
  active,
  count,
  icon,
  label,
  targetFolderId,
  onClick,
  onDropAssets,
  disabled,
}: {
  active: boolean;
  count: number;
  icon: ReactNode;
  label: string;
  targetFolderId: string | null;
  onClick: () => void;
  onDropAssets: (assetIds: string[], folderId: string | null) => void;
  disabled: boolean;
}) {
  const [dragOver, setDragOver] = useState(false);
  const handleDrop = (event: DragEvent<HTMLButtonElement>) => {
    event.preventDefault();
    setDragOver(false);
    if (disabled) {
      return;
    }
    const assetIds = decodeAssetDragPayload(event.dataTransfer.getData(IMAGE_EXPLORER_DRAG_MIME));
    if (assetIds.length) {
      onDropAssets(assetIds, targetFolderId);
    }
  };
  return (
    <button
      type="button"
      onClick={onClick}
      onDragEnter={(event) => {
        if (event.dataTransfer.types.includes(IMAGE_EXPLORER_DRAG_MIME)) {
          setDragOver(true);
        }
      }}
      onDragLeave={() => setDragOver(false)}
      onDragOver={(event) => {
        if (!disabled && event.dataTransfer.types.includes(IMAGE_EXPLORER_DRAG_MIME)) {
          event.preventDefault();
          event.dataTransfer.dropEffect = "move";
        }
      }}
      onDrop={handleDrop}
      className={`flex h-8 w-full min-w-0 items-center gap-2 rounded-md px-2 text-left text-xs transition-colors ${
        dragOver
          ? "bg-emerald-50 text-emerald-700 ring-1 ring-emerald-400 dark:bg-emerald-500/15 dark:text-emerald-200"
          : active
            ? "bg-indigo-50 font-semibold text-indigo-700 dark:bg-violet-500/15 dark:text-violet-100"
            : "text-slate-600 hover:bg-slate-100 hover:text-slate-950 dark:text-slate-300 dark:hover:bg-slate-800 dark:hover:text-white"
      }`}
      title={label}
    >
      <span className="shrink-0">{icon}</span>
      <span className="min-w-0 flex-1 truncate">{label}</span>
      <span className="shrink-0 tabular-nums text-[10px] text-slate-400">{count}</span>
    </button>
  );
}
