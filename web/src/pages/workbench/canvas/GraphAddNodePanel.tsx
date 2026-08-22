import { BookmarkPlus, Boxes, CopyPlus, FolderPlus, Ungroup } from "lucide-react";
import type { ReactNode } from "react";

import type { TranslationKey } from "../../../lib/i18n";
import { useI18n } from "../../../lib/preferences";
import type { GraphNodeCatalog, GraphNodeType } from "../../../lib/types";
import { workflowNodeKindTheme } from "../chrome/WorkflowNodeCard";
import { graphNodeTypeOrder } from "./graphCatalog";
import { graphNodeTitleKey } from "./graphLayout";

const NODE_DESCRIPTIONS: Record<GraphNodeType, TranslationKey> = {
  product_source: "graph.palette.productSourceDesc",
  image_asset: "graph.palette.imageAssetDesc",
  creative_brief: "graph.palette.creativeBriefDesc",
  visual_system: "graph.palette.visualSystemDesc",
  prompt_generation: "graph.palette.promptGenerationDesc",
  image_generation: "graph.palette.imageGenerationDesc",
};

export function GraphAddNodePanel({
  catalog = null,
  busy,
  onCreate,
  canDuplicate = false,
  canGroup = false,
  canDissolve = false,
  onDuplicate,
  onGroup,
  onDissolve,
  onSaveFull,
  onSaveGroup,
  onSaveSelection,
  canSaveFull = false,
  canSaveGroup = false,
  canSaveSelection = false,
  onOpenRecipesTab,
}: {
  busy: boolean;
  onCreate: (nodeType: GraphNodeType) => void;
  canDuplicate?: boolean;
  canGroup?: boolean;
  canDissolve?: boolean;
  onDuplicate?: () => void;
  onGroup?: () => void;
  onDissolve?: () => void;
  canSaveFull?: boolean;
  canSaveGroup?: boolean;
  canSaveSelection?: boolean;
  onSaveFull?: () => void;
  onSaveGroup?: () => void;
  onSaveSelection?: () => void;
  catalog?: GraphNodeCatalog | null;
  onOpenRecipesTab?: () => void;
}) {
  const { t } = useI18n();
  return (
    <div className="space-y-4 p-3.5 pb-6 text-left" data-graph-add-node-panel>
      {canDuplicate && onDuplicate ? (
        <SelectionAction
          icon={<CopyPlus size={15} />}
          title={t("graph.palette.duplicate")}
          hint={t("graph.palette.duplicateHint")}
          disabled={busy}
          onClick={onDuplicate}
        />
      ) : null}
      {canGroup && onGroup ? (
        <SelectionAction
          icon={<FolderPlus size={15} />}
          title={t("graph.palette.group")}
          hint={t("graph.palette.groupHint")}
          disabled={busy}
          onClick={onGroup}
        />
      ) : null}
      {canDissolve && onDissolve ? (
        <SelectionAction
          icon={<Ungroup size={15} />}
          title={t("graph.palette.dissolve")}
          hint={t("graph.palette.dissolveHint")}
          disabled={busy}
          onClick={onDissolve}
        />
      ) : null}
      {canSaveFull && onSaveFull ? (
        <SelectionAction
          icon={<BookmarkPlus size={15} />}
          title={t("workbench.recipe.saveFull")}
          hint={t("workbench.recipe.sourceFull")}
          disabled={busy}
          onClick={onSaveFull}
        />
      ) : null}
      {canSaveGroup && onSaveGroup ? (
        <SelectionAction
          icon={<BookmarkPlus size={15} />}
          title={t("workbench.recipe.saveGroup")}
          hint={t("workbench.recipe.saveGroupHint")}
          disabled={busy}
          onClick={onSaveGroup}
        />
      ) : null}
      {canSaveSelection && onSaveSelection ? (
        <SelectionAction
          icon={<BookmarkPlus size={15} />}
          title={t("workbench.recipe.saveSelection")}
          hint={t("workbench.recipe.saveSelection")}
          disabled={busy}
          onClick={onSaveSelection}
        />
      ) : null}

      <div>
        <h3 className="text-xs font-bold uppercase tracking-wider text-slate-500 dark:text-slate-400">
          {t("graph.palette.title")}
        </h3>
        <p className="mt-0.5 text-[11px] text-slate-400 dark:text-slate-500">
          {t("graph.palette.subtitle")}
        </p>
      </div>

      <div className="flex flex-col gap-2.5">
        {graphNodeTypeOrder(catalog).map((nodeType) => {
          const theme = workflowNodeKindTheme(nodeType);
          const Icon = theme.icon;
          return (
            <button
              key={nodeType}
              type="button"
              disabled={busy}
              onClick={() => onCreate(nodeType)}
              className="group relative flex items-start gap-3 rounded-xl border border-slate-200 bg-white p-3 text-left transition-colors hover:border-slate-300 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-800 dark:bg-[#0e1726] dark:hover:border-slate-600 dark:hover:bg-slate-900/60"
            >
              <span className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border ${theme.iconBox}`}>
                <Icon size={16} />
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex items-center justify-between gap-2">
                  <span className="text-xs font-semibold text-slate-900 dark:text-slate-100">
                    {t(graphNodeTitleKey(nodeType))}
                  </span>
                  <span className="rounded bg-slate-100 px-1.5 py-0.5 text-[9px] font-medium text-slate-600 dark:bg-slate-800 dark:text-slate-300">
                    + {t("graph.palette.add")}
                  </span>
                </div>
                <p className="mt-1 text-[11px] leading-4 text-slate-500 dark:text-slate-400">
                  {t(NODE_DESCRIPTIONS[nodeType])}
                </p>
              </div>
            </button>
          );
        })}
      </div>

      {onOpenRecipesTab ? (
        <div className="rounded-xl border border-slate-200 bg-white p-3.5 dark:border-slate-800 dark:bg-[#0c121e]">
          <div className="flex items-center gap-2">
            <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg border border-slate-200 bg-slate-50 text-slate-600 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-300">
              <Boxes size={15} />
            </span>
            <div className="min-w-0 flex-1">
              <div className="text-xs font-bold text-slate-900 dark:text-slate-100">{t("workbench.sidebar.recipes")}</div>
              <div className="text-[10px] text-slate-500 dark:text-slate-400">{t("graph.palette.recipesHint")}</div>
            </div>
          </div>
          <button
            type="button"
            onClick={onOpenRecipesTab}
            className="mt-3 flex w-full items-center justify-center gap-1.5 rounded-xl border border-slate-200 bg-white py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
          >
            <span>{t("graph.palette.recipes")}</span>
            <span aria-hidden="true">&rarr;</span>
          </button>
        </div>
      ) : null}
    </div>
  );
}

function SelectionAction({
  icon,
  title,
  hint,
  disabled,
  onClick,
}: {
  icon: ReactNode;
  title: string;
  hint: string;
  disabled: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className="group flex w-full items-center gap-2.5 rounded-xl border border-slate-200 bg-white p-2.5 text-left text-slate-800 hover:bg-slate-50 disabled:opacity-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100 dark:hover:bg-slate-800"
    >
      <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300">
        {icon}
      </span>
      <div className="min-w-0 flex-1">
        <div className="text-xs font-bold">{title}</div>
        <div className="text-[10px] opacity-80">{hint}</div>
      </div>
    </button>
  );
}
