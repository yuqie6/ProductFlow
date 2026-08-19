import {
  ArrowRight,
  BookTemplate,
  Bot,
  Layers,
  Sparkles,
  Wand2,
} from "lucide-react";

import { useI18n } from "../../lib/preferences";

export interface WorkflowOnboardingHeroProps {
  productName: string;
  onOpenAgent: () => void;
  onOpenRecipes: () => void;
  onOpenAddPanel: () => void;
}

export function WorkflowOnboardingHero({
  productName,
  onOpenAgent,
  onOpenRecipes,
  onOpenAddPanel,
}: WorkflowOnboardingHeroProps) {
  const { t } = useI18n();

  return (
    <div
      data-workflow-onboarding-hero
      className="flex h-full min-h-0 w-full items-center justify-center overflow-y-auto bg-gradient-to-b from-slate-50 via-zinc-50 to-slate-100 p-6 dark:from-[#080c12] dark:via-[#090f19] dark:to-[#0a101b]"
    >
      <div className="flex w-full max-w-3xl flex-col items-center text-center">
        {/* 顶部标签与标题 */}
        <div className="inline-flex items-center gap-1.5 rounded-full border border-indigo-200/80 bg-indigo-50/80 px-3 py-1 text-xs font-semibold text-indigo-700 shadow-sm dark:border-violet-500/30 dark:bg-violet-950/40 dark:text-violet-300">
          <Sparkles size={13} />
          <span>{productName}</span>
        </div>

        <h2 className="mt-3 text-xl font-extrabold tracking-tight text-slate-900 sm:text-2xl dark:text-slate-100">
          {t("agentWorkbench.onboarding.title")}
        </h2>
        <p className="mt-1.5 max-w-xl text-xs leading-relaxed text-slate-500 sm:text-sm dark:text-slate-400">
          {t("agentWorkbench.onboarding.subtitle")}
        </p>

        {/* 3 大启动路径卡片矩阵 */}
        <div className="mt-8 grid w-full grid-cols-1 gap-4 sm:grid-cols-3">
          {/* 路径 1: Agent 智能规划 */}
          <div className="group relative flex flex-col justify-between rounded-2xl border border-violet-200/80 bg-gradient-to-b from-white to-violet-50/40 p-5 text-left shadow-sm transition-all hover:-translate-y-1 hover:border-violet-400 hover:shadow-lg dark:border-violet-500/30 dark:from-[#0e1626] dark:to-violet-950/20 dark:hover:border-violet-400/70">
            <div>
              <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-violet-600 text-white shadow-md shadow-violet-600/25 group-hover:scale-105 transition-transform">
                <Bot size={20} />
              </div>
              <h3 className="mt-3.5 text-sm font-bold text-slate-900 dark:text-slate-100">
                {t("agentWorkbench.onboarding.agentTitle")}
              </h3>
              <p className="mt-1.5 text-xs leading-relaxed text-slate-500 dark:text-slate-400">
                {t("agentWorkbench.onboarding.agentDesc")}
              </p>
            </div>
            <button
              type="button"
              onClick={onOpenAgent}
              className="mt-5 inline-flex items-center justify-between rounded-xl bg-violet-600 px-3.5 py-2 text-xs font-bold text-white shadow-sm shadow-violet-600/20 transition-colors hover:bg-violet-700 active:scale-95"
            >
              <span>{t("agentWorkbench.onboarding.agentAction")}</span>
              <Wand2 size={13} />
            </button>
          </div>

          {/* 路径 2: 行业成熟配方 */}
          <div className="group relative flex flex-col justify-between rounded-2xl border border-indigo-200/80 bg-gradient-to-b from-white to-indigo-50/40 p-5 text-left shadow-sm transition-all hover:-translate-y-1 hover:border-indigo-400 hover:shadow-lg dark:border-indigo-500/30 dark:from-[#0e1626] dark:to-indigo-950/20 dark:hover:border-indigo-400/70">
            <div>
              <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-indigo-600 text-white shadow-md shadow-indigo-600/25 group-hover:scale-105 transition-transform">
                <BookTemplate size={20} />
              </div>
              <h3 className="mt-3.5 text-sm font-bold text-slate-900 dark:text-slate-100">
                {t("agentWorkbench.onboarding.recipeTitle")}
              </h3>
              <p className="mt-1.5 text-xs leading-relaxed text-slate-500 dark:text-slate-400">
                {t("agentWorkbench.onboarding.recipeDesc")}
              </p>
            </div>
            <button
              type="button"
              onClick={onOpenRecipes}
              className="mt-5 inline-flex items-center justify-between rounded-xl bg-indigo-600 px-3.5 py-2 text-xs font-bold text-white shadow-sm shadow-indigo-600/20 transition-colors hover:bg-indigo-700 active:scale-95"
            >
              <span>{t("agentWorkbench.onboarding.recipeAction")}</span>
              <ArrowRight size={13} />
            </button>
          </div>

          {/* 路径 3: 人工自由空白建图 */}
          <div className="group relative flex flex-col justify-between rounded-2xl border border-slate-200/90 bg-gradient-to-b from-white to-slate-50/70 p-5 text-left shadow-sm transition-all hover:-translate-y-1 hover:border-slate-400 hover:shadow-lg dark:border-slate-800 dark:from-[#0e1626] dark:to-slate-900/30 dark:hover:border-slate-600">
            <div>
              <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-slate-800 text-white shadow-md shadow-slate-900/20 dark:bg-slate-700 group-hover:scale-105 transition-transform">
                <Layers size={20} />
              </div>
              <h3 className="mt-3.5 text-sm font-bold text-slate-900 dark:text-slate-100">
                {t("agentWorkbench.onboarding.manualTitle")}
              </h3>
              <p className="mt-1.5 text-xs leading-relaxed text-slate-500 dark:text-slate-400">
                {t("agentWorkbench.onboarding.manualDesc")}
              </p>
            </div>
            <button
              type="button"
              onClick={onOpenAddPanel}
              className="mt-5 inline-flex items-center justify-between rounded-xl border border-slate-300 bg-white px-3.5 py-2 text-xs font-bold text-slate-700 shadow-sm transition-colors hover:border-slate-400 hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200 dark:hover:bg-slate-700 active:scale-95"
            >
              <span>{t("agentWorkbench.onboarding.manualAction")}</span>
              <ArrowRight size={13} />
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
