import {
  ArrowLeft,
  ChevronDown,
  FolderInput,
  FolderPlus,
  Loader2,
  MoreHorizontal,
  Pencil,
  Play,
  Save,
  Trash2,
  Workflow,
} from "lucide-react";

import { useI18n } from "../../lib/preferences";
import type { ProductWorkflowV2 } from "../../lib/types";
import {
  labelRecipeVersionSource,
  type RecipeSourceSelection,
  type RecipeVersionSource,
} from "./recipeSource";

interface V2WorkflowCommandBarProps {
  workflow: ProductWorkflowV2 | null;
  openFolderId: string | null;
  selectedNodeIds: string[];
  structureBusy: boolean;
  workflowRunBusy?: boolean;
  variant: "header" | "overlay";
  activityLabel?: string | null;
  onOpenFolder: (folderId: string | null) => void;
  onCreateFolder: () => void;
  onMoveSelection: (folderId: string | null) => void;
  onSaveRecipe: (source: RecipeSourceSelection) => void;
  onRenameFolder: () => void;
  onDissolveFolder: () => void;
  onRunWorkflow?: () => void;
}

export function V2WorkflowCommandBar({
  workflow,
  openFolderId,
  selectedNodeIds,
  structureBusy,
  workflowRunBusy = false,
  variant,
  activityLabel,
  onOpenFolder,
  onCreateFolder,
  onMoveSelection,
  onSaveRecipe,
  onRenameFolder,
  onDissolveFolder,
  onRunWorkflow,
}: V2WorkflowCommandBarProps) {
  const { t } = useI18n();
  const openFolder = workflow?.folders.find((folder) => folder.id === openFolderId) ?? null;
  const selectedNodes = workflow?.nodes.filter((node) => selectedNodeIds.includes(node.id)) ?? [];
  const canUngroup = Boolean(
    openFolder && selectedNodes.some((node) => node.folder_id === openFolder.id),
  );
  const saveRecipe = (source: RecipeVersionSource) => {
    if (workflow) {
      onSaveRecipe(labelRecipeVersionSource(source, workflow, t));
    }
  };

  const identity = (
    <div className="flex min-w-0 items-center gap-2">
      {openFolder ? (
        <CommandIconButton
          label={t("workflowV2.canvas.back")}
          disabled={structureBusy}
          onClick={() => onOpenFolder(null)}
        >
          <ArrowLeft size={16} />
        </CommandIconButton>
      ) : (
        <span className="hidden h-9 w-9 shrink-0 items-center justify-center text-slate-400 sm:inline-flex dark:text-slate-500">
          <Workflow size={16} />
        </span>
      )}
      <div className="min-w-0">
        <div className="flex min-w-0 items-center gap-1.5 text-sm font-semibold text-slate-950 dark:text-slate-100">
          <span className="truncate">{workflow?.title ?? t("workflowV2.canvas.title")}</span>
          {openFolder ? (
            <>
              <span className="text-slate-300 dark:text-slate-700">/</span>
              <span className="truncate text-indigo-700 dark:text-violet-300">{openFolder.title}</span>
            </>
          ) : null}
        </div>
        <div className="mt-0.5 flex items-center gap-2 text-[10px] font-medium text-slate-400 dark:text-slate-500">
          <span>
            {workflow
              ? t("workflowV2.canvas.version", {
                  revision: workflow.revision,
                  editVersion: workflow.edit_version,
                })
              : t("workflowV2.canvas.noWorkflowShort")}
          </span>
          {activityLabel ? (
            <span className="inline-flex items-center gap-1 text-indigo-700 dark:text-violet-300">
              <Loader2 size={10} className="animate-spin" />
              {activityLabel}
            </span>
          ) : null}
        </div>
      </div>
    </div>
  );

  const actions = workflow ? (
    <div className="flex min-w-max items-center justify-end gap-1.5">
      {onRunWorkflow ? (
        <CommandIconButton
          label={t(workflowRunBusy ? "detail.workflowRunning" : "detail.runWorkflow")}
          disabled={structureBusy || workflowRunBusy}
          onClick={onRunWorkflow}
        >
          {workflowRunBusy ? <Loader2 size={15} className="animate-spin" /> : <Play size={15} />}
        </CommandIconButton>
      ) : null}

      {selectedNodeIds.length ? (
        <span className="hidden rounded-md bg-indigo-50 px-2 py-1.5 text-[11px] font-semibold text-indigo-700 dark:bg-violet-500/15 dark:text-violet-200 md:inline-flex">
          {t("workflowV2.canvas.selected", { count: selectedNodeIds.length })}
        </span>
      ) : null}

      {selectedNodeIds.length ? (
        <CommandIconButton
          label={t("workflowV2.folder.create")}
          disabled={structureBusy}
          onClick={onCreateFolder}
        >
          <FolderPlus size={15} />
        </CommandIconButton>
      ) : null}

      {selectedNodeIds.length ? (
        <label className="relative block shrink-0">
          <span className="sr-only">{t("workflowV2.folder.move")}</span>
          <FolderInput
            size={14}
            className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-slate-500"
          />
          <select
            value=""
            disabled={structureBusy}
            onChange={(event) => {
              const value = event.target.value;
              if (value === "__ungrouped__") onMoveSelection(null);
              else if (value) onMoveSelection(value);
            }}
            className="h-11 max-w-[152px] appearance-none rounded-lg border border-slate-200 bg-white py-0 pl-8 pr-7 text-xs font-semibold text-slate-700 outline-none hover:border-indigo-300 disabled:opacity-40 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 lg:h-9 lg:rounded-md"
            aria-label={t("workflowV2.folder.move")}
          >
            <option value="">{t("workflowV2.folder.move")}</option>
            {canUngroup ? (
              <option value="__ungrouped__">{t("workflowV2.folder.ungroup")}</option>
            ) : null}
            {workflow.folders
              .filter((folder) => folder.id !== openFolder?.id)
              .map((folder) => (
                <option key={folder.id} value={folder.id}>{folder.title}</option>
              ))}
          </select>
          <ChevronDown
            size={13}
            className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 text-slate-400"
          />
        </label>
      ) : null}

      <details className="group relative shrink-0">
        <summary
          className="flex h-11 w-11 cursor-pointer list-none items-center justify-center rounded-lg bg-slate-950 text-white marker:hidden hover:bg-indigo-700 dark:bg-violet-500 dark:hover:bg-violet-400 lg:h-9 lg:w-9 lg:rounded-md [&::-webkit-details-marker]:hidden"
          aria-label={t("workflowV2.recipe.save")}
          title={t("workflowV2.recipe.save")}
        >
          <Save size={14} />
        </summary>
        <div className="absolute right-0 top-full z-50 mt-1.5 w-56 overflow-hidden rounded-md border border-slate-200 bg-white p-1.5 shadow-xl dark:border-slate-700 dark:bg-[#10151d]">
          <RecipeSourceButton
            label={t("workflowV2.recipe.saveFull")}
            onClick={() => saveRecipe({ source_type: "workflow" })}
          />
          {openFolder ? (
            <RecipeSourceButton
              label={t("workflowV2.recipe.saveFolder")}
              onClick={() => saveRecipe({ source_type: "folder", folder_id: openFolder.id })}
            />
          ) : null}
          {selectedNodeIds.length ? (
            <RecipeSourceButton
              label={t("workflowV2.recipe.saveSelection")}
              onClick={() => saveRecipe({ source_type: "selection", node_ids: selectedNodeIds })}
            />
          ) : null}
        </div>
      </details>

      {openFolder ? (
        <details className="group relative shrink-0">
          <summary
            className="flex h-11 w-11 cursor-pointer list-none items-center justify-center rounded-lg border border-slate-200 text-slate-600 marker:hidden hover:border-indigo-300 hover:text-indigo-700 dark:border-slate-700 dark:text-slate-300 dark:hover:border-violet-400 dark:hover:text-violet-200 lg:h-9 lg:w-9 lg:rounded-md [&::-webkit-details-marker]:hidden"
            aria-label={t("workflowV2.folder.actions")}
            title={t("workflowV2.folder.actions")}
          >
            <MoreHorizontal size={16} />
          </summary>
          <div className="absolute right-0 top-full z-50 mt-1.5 w-44 overflow-hidden rounded-md border border-slate-200 bg-white p-1.5 shadow-xl dark:border-slate-700 dark:bg-[#10151d]">
            <button
              type="button"
              onClick={onRenameFolder}
              className="flex h-9 w-full items-center gap-2 rounded px-2.5 text-left text-xs font-medium text-slate-700 hover:bg-slate-100 dark:text-slate-200 dark:hover:bg-slate-800"
            >
              <Pencil size={13} />{t("workflowV2.folder.rename")}
            </button>
            <button
              type="button"
              onClick={onDissolveFolder}
              className="flex h-9 w-full items-center gap-2 rounded px-2.5 text-left text-xs font-medium text-red-600 hover:bg-red-50 dark:text-red-300 dark:hover:bg-red-500/10"
            >
              <Trash2 size={13} />{t("workflowV2.folder.dissolve")}
            </button>
          </div>
        </details>
      ) : null}
    </div>
  ) : null;

  if (variant === "overlay") {
    if (!workflow) return null;
    return (
      <div
        data-canvas-control
        className="pointer-events-none absolute inset-x-3 top-[4.5rem] z-40 flex justify-center xl:top-4"
      >
        <div
          role="toolbar"
          aria-label={t("workflowV2.canvas.title")}
          className="pointer-events-auto flex max-w-full items-center gap-2 overflow-x-auto rounded-xl border border-slate-200/90 bg-white/95 p-1.5 shadow-lg shadow-slate-950/10 backdrop-blur dark:border-slate-700/90 dark:bg-[#111827]/95 dark:shadow-black/30"
        >
          <div className="hidden min-w-0 max-w-56 border-r border-slate-200 px-1 pr-3 xl:block dark:border-slate-700">
            {identity}
          </div>
          {activityLabel ? (
            <span className="inline-flex min-w-max items-center gap-1 px-1 text-[11px] font-semibold text-indigo-700 xl:hidden dark:text-violet-300">
              <Loader2 size={11} className="animate-spin" />{activityLabel}
            </span>
          ) : null}
          {openFolder ? (
            <CommandIconButton
              label={t("workflowV2.canvas.back")}
              disabled={structureBusy}
              onClick={() => onOpenFolder(null)}
              className="xl:hidden"
            >
              <ArrowLeft size={16} />
            </CommandIconButton>
          ) : null}
          {actions}
        </div>
      </div>
    );
  }

  return (
    <header className="flex min-h-[58px] flex-wrap items-center gap-2 border-b border-slate-200 bg-white px-3 py-2 dark:border-slate-800 dark:bg-[#0d1118] sm:px-4">
      {identity}
      <div className="ml-auto max-w-full overflow-x-auto">{actions}</div>
    </header>
  );
}

function CommandIconButton({
  label,
  disabled,
  onClick,
  className = "",
  children,
}: {
  label: string;
  disabled?: boolean;
  onClick: () => void;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={`inline-flex h-11 w-11 shrink-0 items-center justify-center rounded-lg border border-slate-200 bg-white text-slate-600 hover:border-indigo-300 hover:text-indigo-700 disabled:opacity-40 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300 dark:hover:border-violet-400 dark:hover:text-violet-200 lg:h-9 lg:w-9 lg:rounded-md ${className}`}
      aria-label={label}
      title={label}
    >
      {children}
    </button>
  );
}

function RecipeSourceButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex h-9 w-full items-center gap-2 rounded px-2.5 text-left text-xs font-medium text-slate-700 hover:bg-slate-100 dark:text-slate-200 dark:hover:bg-slate-800"
    >
      <Save size={13} />{label}
    </button>
  );
}
