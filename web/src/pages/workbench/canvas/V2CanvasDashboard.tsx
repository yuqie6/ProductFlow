import {
  ImagePlus,
  Layers,
  Loader2,
  Play,
  Plus,
} from "lucide-react";

import { useI18n } from "../../../lib/preferences";
import type {
  CanonicalProductDetail,
  ProductWorkflowV2,
  WorkflowDraftProductFact,
} from "../../../lib/types";
import { formatFactValue, humanizeFactKey } from "./nodeEditorDrafts";

export interface V2CanvasDashboardProps {
  product: CanonicalProductDetail;
  workflow: ProductWorkflowV2;
  facts: WorkflowDraftProductFact[];
  onOpenAddPanel?: () => void;
  onOpenLibraryPanel?: () => void;
  onRunWorkflow?: () => void;
  runWorkflowBusy?: boolean;
}

export function V2CanvasDashboard({
  product,
  workflow,
  facts,
  onOpenAddPanel,
  onOpenLibraryPanel,
  onRunWorkflow,
  runWorkflowBusy = false,
}: V2CanvasDashboardProps) {
  const { t } = useI18n();

  const factCount = facts.length;
  const refCount = workflow.nodes.filter((n) => n.node_type === "reference_image").length;
  const promptCount = workflow.nodes.filter((n) => n.node_type === "prompt_generation").length;
  const imageCount = workflow.nodes.filter((n) => n.node_type === "image_generation").length;
  const folderCount = workflow.folders.length;

  const succeededCount = workflow.nodes.filter((n) => n.status === "succeeded").length;
  const activeCount = workflow.nodes.filter((n) => n.status === "running" || n.status === "queued").length;
  const failedCount = workflow.nodes.filter((n) => n.status === "failed").length;
  const unknownCount = workflow.nodes.filter((n) => n.status === "unknown").length;
  const idleCount = workflow.nodes.filter((n) => n.status === "idle").length;

  return (
    <div className="space-y-4 p-3.5 pb-6 text-left" data-v2-canvas-dashboard>
      {/* 头部：工作流标题与版本 */}
      <div className="rounded-2xl border border-slate-200/90 bg-gradient-to-br from-indigo-50/60 via-white to-slate-50 p-4 shadow-sm dark:border-slate-800 dark:from-violet-950/20 dark:via-[#0c121e] dark:to-[#0c121e]">
        <div className="flex items-start justify-between gap-2">
          <div className="flex items-center gap-2.5">
            <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-xl bg-indigo-600 text-white shadow-md shadow-indigo-600/20">
              <Layers size={16} />
            </span>
            <div className="min-w-0">
              <h3 className="truncate text-sm font-bold text-slate-900 dark:text-slate-100" title={workflow.title}>
                {workflow.title}
              </h3>
              <p className="text-[10px] text-slate-500 dark:text-slate-400">
                {t("workflowV2.dashboard.subtitle")}
              </p>
            </div>
          </div>
          <span className="shrink-0 rounded-full border border-indigo-200 bg-indigo-50 px-2 py-0.5 text-[10px] font-semibold text-indigo-700 dark:border-violet-500/30 dark:bg-violet-950/50 dark:text-violet-300">
            v{workflow.revision} · rev {workflow.edit_version}
          </span>
        </div>

        {/* 4 格全图拓扑指标卡片 */}
        <div className="mt-3.5 grid grid-cols-4 gap-1.5 text-center">
          <div className="rounded-xl border border-purple-200/80 bg-purple-50/70 p-2 dark:border-purple-500/30 dark:bg-purple-950/30">
            <div className="text-[10px] font-medium text-purple-700 dark:text-purple-300">{t("workflowV2.dashboard.factCount")}</div>
            <div
              data-v2-canvas-dashboard-fact-count={factCount}
              className="mt-0.5 text-sm font-black text-purple-900 dark:text-purple-100"
            >
              {factCount}
            </div>
          </div>
          <div className="rounded-xl border border-indigo-200/80 bg-indigo-50/70 p-2 dark:border-indigo-500/30 dark:bg-indigo-950/30">
            <div className="text-[10px] font-medium text-indigo-700 dark:text-indigo-300">{t("workflowV2.dashboard.refCount")}</div>
            <div className="mt-0.5 text-sm font-black text-indigo-900 dark:text-indigo-100">{refCount}</div>
          </div>
          <div className="rounded-xl border border-amber-200/80 bg-amber-50/70 p-2 dark:border-amber-500/30 dark:bg-amber-950/30">
            <div className="text-[10px] font-medium text-amber-700 dark:text-amber-300">{t("workflowV2.dashboard.promptCount")}</div>
            <div className="mt-0.5 text-sm font-black text-amber-900 dark:text-amber-100">{promptCount}</div>
          </div>
          <div className="rounded-xl border border-cyan-200/80 bg-cyan-50/70 p-2 dark:border-cyan-500/30 dark:bg-cyan-950/30">
            <div className="text-[10px] font-medium text-cyan-700 dark:text-cyan-300">{t("workflowV2.dashboard.imageCount")}</div>
            <div className="mt-0.5 text-sm font-black text-cyan-900 dark:text-cyan-100">{imageCount}</div>
          </div>
        </div>
      </div>

      {/* 运行状态实时分布 */}
      <div className="rounded-xl border border-slate-200/80 bg-white p-3 shadow-sm dark:border-slate-800 dark:bg-[#0d1424]">
        <div className="flex items-center justify-between text-xs font-bold text-slate-800 dark:text-slate-200">
          <span>{t("workflowV2.dashboard.runStatus")}</span>
          <span className="text-[11px] font-medium text-slate-400">
            {t("workflowV2.dashboard.nodeFolderSummary", {
              nodes: workflow.nodes.length,
              folders: folderCount,
            })}
          </span>
        </div>
        <div className="mt-2.5 grid grid-cols-5 gap-1.5 text-center text-[10px]">
          <div className="rounded-lg bg-emerald-50 py-1.5 font-semibold text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300">
            <div>{succeededCount}</div>
            <div className="text-[9px] opacity-80">{t("workflowV2.dashboard.succeeded")}</div>
          </div>
          <div className="rounded-lg bg-blue-50 py-1.5 font-semibold text-blue-700 dark:bg-blue-950/40 dark:text-blue-300">
            <div>{activeCount}</div>
            <div className="text-[9px] opacity-80">{t("workflowV2.dashboard.running")}</div>
          </div>
          <div className="rounded-lg bg-red-50 py-1.5 font-semibold text-red-700 dark:bg-red-950/40 dark:text-red-300">
            <div>{failedCount}</div>
            <div className="text-[9px] opacity-80">{t("workflowV2.dashboard.failed")}</div>
          </div>
          <div className="rounded-lg bg-orange-50 py-1.5 font-semibold text-orange-700 dark:bg-orange-950/40 dark:text-orange-300">
            <div>{unknownCount}</div>
            <div className="text-[9px] opacity-80">{t("workflowV2.dashboard.unknown")}</div>
          </div>
          <div className="rounded-lg bg-slate-100 py-1.5 font-semibold text-slate-600 dark:bg-slate-800 dark:text-slate-400">
            <div>{idleCount}</div>
            <div className="text-[9px] opacity-80">{t("workflowV2.dashboard.idle")}</div>
          </div>
        </div>
      </div>

      {/* 全局快捷行动区 */}
      <div className="rounded-xl border border-slate-200/80 bg-white p-3 shadow-sm dark:border-slate-800 dark:bg-[#0d1424]">
        <div className="text-xs font-bold text-slate-800 dark:text-slate-200">
          {t("workflowV2.dashboard.quickActions")}
        </div>
        <div className="mt-2 flex flex-col gap-2">
          {onRunWorkflow ? (
            <button
              type="button"
              onClick={onRunWorkflow}
              disabled={runWorkflowBusy}
              className="flex items-center justify-center gap-2 rounded-xl bg-gradient-to-r from-blue-600 to-indigo-600 px-3 py-2 text-xs font-bold text-white shadow-md shadow-blue-600/20 hover:from-blue-700 hover:to-indigo-700 disabled:opacity-50 transition-all"
            >
              {runWorkflowBusy ? <Loader2 size={13} className="animate-spin" /> : <Play size={13} fill="currentColor" />}
              {t("workflowV2.dashboard.runAll")}
            </button>
          ) : null}

          <div className="grid grid-cols-2 gap-2">
            {onOpenAddPanel ? (
              <button
                type="button"
                onClick={onOpenAddPanel}
                className="flex items-center justify-center gap-1.5 rounded-lg border border-slate-200 bg-slate-50 px-2.5 py-1.5 text-xs font-medium text-slate-700 hover:border-indigo-300 hover:bg-indigo-50/70 hover:text-indigo-700 dark:border-slate-700 dark:bg-slate-800/80 dark:text-slate-200 transition-colors"
              >
                <Plus size={13} />
                {t("workflowV2.dashboard.addNode")}
              </button>
            ) : null}

            {onOpenLibraryPanel ? (
              <button
                type="button"
                onClick={onOpenLibraryPanel}
                className="flex items-center justify-center gap-1.5 rounded-lg border border-slate-200 bg-slate-50 px-2.5 py-1.5 text-xs font-medium text-slate-700 hover:border-indigo-300 hover:bg-indigo-50/70 hover:text-indigo-700 dark:border-slate-700 dark:bg-slate-800/80 dark:text-slate-200 transition-colors"
              >
                <ImagePlus size={13} />
                {t("workflowV2.dashboard.openLibrary")}
              </button>
            ) : null}
          </div>
        </div>
      </div>

      {/* 当前商品核心事实摘要 */}
      {facts && facts.length > 0 ? (
        <div className="rounded-xl border border-slate-200/80 bg-white p-3 shadow-sm dark:border-slate-800 dark:bg-[#0d1424]">
          <div className="flex items-center justify-between text-xs font-bold text-slate-800 dark:text-slate-200">
            <span>{t("workflowV2.dashboard.productFacts")}</span>
            <span className="rounded bg-purple-100 px-1.5 py-0.2 text-[9px] font-semibold text-purple-700 dark:bg-purple-950/60 dark:text-purple-300">
              {product.name}
            </span>
          </div>
          <div className="mt-2.5 space-y-1.5">
            {facts.slice(0, 4).map((fact) => (
              <div
                key={fact.key}
                className="flex items-start justify-between gap-2 rounded-lg border border-slate-100 bg-slate-50/70 px-2.5 py-1.5 text-[11px] dark:border-slate-800/60 dark:bg-slate-900/50"
              >
                <span className="font-semibold text-slate-600 dark:text-slate-300">
                  {humanizeFactKey(fact.key)}
                </span>
                <span
                  className="truncate text-right text-slate-500 dark:text-slate-400"
                  title={formatFactValue(fact.value, "—")}
                >
                  {formatFactValue(fact.value, "—")}
                </span>
              </div>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  );
}
