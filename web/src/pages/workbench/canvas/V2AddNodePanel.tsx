import {
  Boxes,
  CopyPlus,
  FolderPlus,
  ImagePlus,
} from "lucide-react";

import { useI18n } from "../../../lib/preferences";

export interface V2AddNodePanelProps {
  busy: boolean;
  onCreateReference: () => void;
  onCreateFolder?: () => void;
  hasSelectedNode?: boolean;
  onDuplicateSelected?: () => void;
  onOpenRecipesTab?: () => void;
  onOpenLibraryTab?: () => void;
}

export function V2AddNodePanel({
  busy,
  onCreateReference,
  onCreateFolder,
  hasSelectedNode = false,
  onDuplicateSelected,
  onOpenRecipesTab,
}: V2AddNodePanelProps) {
  const { t } = useI18n();

  return (
    <div className="space-y-4 p-3.5 pb-6 text-left" data-v2-add-node-panel>
      {/* 快捷克隆选中节点 */}
      {hasSelectedNode && onDuplicateSelected ? (
        <button
          type="button"
          onClick={onDuplicateSelected}
          disabled={busy}
          className="group flex w-full items-center gap-2.5 rounded-xl border border-sky-200 bg-sky-50/70 p-2.5 text-left text-sky-800 shadow-sm transition-all hover:bg-sky-100 hover:border-sky-300 dark:border-sky-500/30 dark:bg-sky-950/30 dark:text-sky-200"
        >
          <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-sky-200/80 text-sky-800 dark:bg-sky-900/60 dark:text-sky-200">
            <CopyPlus size={15} />
          </span>
          <div className="min-w-0 flex-1">
            <div className="text-xs font-bold">{t("workflowV2.palette.duplicateSelected")}</div>
            <div className="text-[10px] opacity-80">{t("workflowV2.palette.duplicateSelectedHint")}</div>
          </div>
        </button>
      ) : null}

      {/* 头部标题与定位说明 */}
      <div>
        <h3 className="text-xs font-bold uppercase tracking-wider text-slate-500 dark:text-slate-400">
          {t("workflowV2.palette.baseNodes")}
        </h3>
        <p className="mt-0.5 text-[11px] text-slate-400 dark:text-slate-500">
          {t("workflowV2.palette.baseNodesSubtitle")}
        </p>
      </div>

      {/* 基础节点与分组卡片列表 */}
      <div className="flex flex-col gap-2.5">
        {/* 1. 参考图节点 */}
        <button
          type="button"
          onClick={onCreateReference}
          disabled={busy}
          className="group relative flex items-start gap-3 rounded-xl border border-indigo-200/80 bg-gradient-to-r from-indigo-50/70 via-white to-white p-3 text-left shadow-sm transition-all hover:-translate-y-0.5 hover:border-indigo-300 hover:shadow-md disabled:cursor-not-allowed disabled:opacity-50 dark:border-violet-500/30 dark:from-violet-950/30 dark:via-[#0e1726] dark:to-[#0e1726] dark:hover:border-violet-400/60"
        >
          <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-indigo-200 bg-indigo-100/80 text-indigo-700 shadow-sm dark:border-violet-500/40 dark:bg-violet-900/60 dark:text-violet-200 group-hover:scale-105 transition-transform">
            <ImagePlus size={18} />
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex items-center justify-between">
              <span className="text-xs font-bold text-slate-900 dark:text-slate-100 group-hover:text-indigo-600 dark:group-hover:text-violet-300 transition-colors">
                {t("workflowV2.palette.refNode")}
              </span>
              <span className="rounded bg-indigo-100 px-1.5 py-0.5 text-[9px] font-semibold text-indigo-700 dark:bg-violet-900/60 dark:text-violet-300">
                + {t("workflowV2.palette.add")}
              </span>
            </div>
            <p className="mt-1 text-[11px] leading-4 text-slate-500 dark:text-slate-400">
              {t("workflowV2.palette.refNodeDesc")}
            </p>
          </div>
        </button>

        {/* 2. 新建画布分组/文件夹 */}
        {onCreateFolder ? (
          <button
            type="button"
            onClick={onCreateFolder}
            disabled={busy}
            className="group relative flex items-start gap-3 rounded-xl border border-purple-200/80 bg-gradient-to-r from-purple-50/70 via-white to-white p-3 text-left shadow-sm transition-all hover:-translate-y-0.5 hover:border-purple-300 hover:shadow-md disabled:cursor-not-allowed disabled:opacity-50 dark:border-purple-500/30 dark:from-purple-950/30 dark:via-[#0e1726] dark:to-[#0e1726] dark:hover:border-purple-400/60"
          >
            <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-purple-200 bg-purple-100/80 text-purple-700 shadow-sm dark:border-purple-500/40 dark:bg-purple-900/60 dark:text-purple-200 group-hover:scale-105 transition-transform">
              <FolderPlus size={18} />
            </span>
            <div className="min-w-0 flex-1">
              <div className="flex items-center justify-between">
                <span className="text-xs font-bold text-slate-900 dark:text-slate-100 group-hover:text-purple-600 dark:group-hover:text-purple-300 transition-colors">
                  {t("workflowV2.palette.folderNode")}
                </span>
                <span className="rounded bg-purple-100 px-1.5 py-0.5 text-[9px] font-semibold text-purple-700 dark:bg-purple-900/60 dark:text-purple-300">
                  + {t("workflowV2.palette.add")}
                </span>
              </div>
              <p className="mt-1 text-[11px] leading-4 text-slate-500 dark:text-slate-400">
                {t("workflowV2.palette.folderNodeDesc")}
              </p>
            </div>
          </button>
        ) : null}
      </div>

      {/* 预设与模板快捷引导区 */}
      {onOpenRecipesTab ? (
        <div className="pt-2">
          <div className="rounded-2xl border border-slate-200/90 bg-gradient-to-br from-indigo-50/50 via-white to-slate-50 p-3.5 dark:border-slate-800 dark:from-violet-950/20 dark:via-[#0c121e] dark:to-[#0c121e]">
            <div className="flex items-center gap-2">
              <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-indigo-100 text-indigo-700 dark:bg-violet-900/50 dark:text-violet-300">
                <Boxes size={15} />
              </span>
              <div className="min-w-0 flex-1">
                <div className="text-xs font-bold text-slate-900 dark:text-slate-100">
                  {t("workflowV2.sidebar.recipes")}
                </div>
                <div className="text-[10px] text-slate-500 dark:text-slate-400">
                  {t("workflowV2.palette.presetsSubtitle")}
                </div>
              </div>
            </div>
            <button
              type="button"
              onClick={onOpenRecipesTab}
              className="mt-3 flex w-full items-center justify-center gap-1.5 rounded-xl border border-indigo-200 bg-white py-1.5 text-xs font-semibold text-indigo-700 shadow-sm transition-all hover:bg-indigo-50 hover:border-indigo-300 active:scale-95 dark:border-violet-500/30 dark:bg-slate-900 dark:text-violet-300 dark:hover:bg-violet-950/50"
            >
              <span>{t("workflowV2.palette.exploreRecipes")}</span>
              <span>&rarr;</span>
            </button>
          </div>
        </div>
      ) : null}
    </div>
  );
}


