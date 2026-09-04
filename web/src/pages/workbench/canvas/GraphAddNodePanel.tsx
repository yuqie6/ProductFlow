import { AlertCircle, BookmarkPlus, Boxes, CopyPlus, FolderPlus, RotateCcw, Ungroup } from "lucide-react";
import { useState, type ReactNode } from "react";

import { Button } from "../../../components/ui/button";
import { Select } from "../../../components/ui/select";

import { generatingImageTypeKeys } from "../../../lib/imageTypeFamilies";
import type { TranslationKey } from "../../../lib/i18n";
import { useI18n } from "../../../lib/preferences";
import type { AgentProductImageTypeKey, GraphNodeCatalog, GraphNodeType } from "../../../lib/types";
import { workflowNodeKindTheme } from "../chrome/WorkflowNodeCard";
import { AGENT_IMAGE_TYPE_TRANSLATIONS } from "../../product-create/imageTypeSelection";
import { graphNodeTypeOrder } from "./graphCatalog";
import { graphNodeTitleKey } from "./graphLayout";
import { humanizeCatalogKey } from "./CatalogConfigFields";

const NODE_DESCRIPTIONS: Record<GraphNodeType, TranslationKey> = {
  product_source: "graph.palette.productSourceDesc",
  image_asset: "graph.palette.imageAssetDesc",
  creative_brief: "graph.palette.creativeBriefDesc",
  visual_system: "graph.palette.visualSystemDesc",
  image_prompt: "graph.palette.promptGenerationDesc",
  image_generation: "graph.palette.imageGenerationDesc",
};

export function GraphAddNodePanel({
  catalog = null,
  catalogError = null,
  onRetryCatalog,
  busy,
  onCreate,
  onCreateShot,
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
  onCreateShot?: (imageTypeKey: AgentProductImageTypeKey) => void;
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
  catalogError?: string | null;
  onRetryCatalog?: () => void;
  onOpenRecipesTab?: () => void;
}) {
  const { t } = useI18n();
  const shotKeys = generatingImageTypeKeys();
  const [shotKey, setShotKey] = useState<AgentProductImageTypeKey>(shotKeys[0] ?? "hero");
  const paletteTypes = graphNodeTypeOrder(catalog);
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

      {onCreateShot ? (
        <div data-add-shot="">
          <h3 className="text-xs font-bold text-text-muted">
            {t("graph.palette.addShot")}
          </h3>
          <p className="mt-0.5 text-[11px] text-text-muted">
            {t("graph.palette.addShotHint")}
          </p>
          <div className="mt-2 flex gap-2">
            <div className="min-w-0 flex-1">
              <Select
                value={shotKey}
                disabled={busy}
                ariaLabel={t("graph.palette.addShot")}
                size="sm"
                onChange={(value) => setShotKey(value as AgentProductImageTypeKey)}
                options={shotKeys.map((key) => {
                  const translations = AGENT_IMAGE_TYPE_TRANSLATIONS[key];
                  return { value: key, label: translations ? t(translations.title) : humanizeCatalogKey(key) };
                })}
              />
            </div>
            <Button variant="secondary" size="md" disabled={busy} onClick={() => onCreateShot(shotKey)}>
              {t("graph.palette.addShotAction")}
            </Button>
          </div>
        </div>
      ) : null}

      <div>
        <h3 className="text-xs font-bold text-text-muted">
          {t("graph.palette.title")}
        </h3>
        <p className="mt-0.5 text-[11px] text-text-muted">
          {t("graph.palette.subtitle")}
        </p>
      </div>

      <div className="flex flex-col gap-2.5">
        {paletteTypes.length === 0 ? (
          <div
            role="alert"
            className="flex items-start gap-2 rounded-xl border border-state-error/30 bg-state-error-soft px-3 py-2.5 text-xs leading-5 text-state-error"
          >
            <AlertCircle size={14} className="mt-0.5 shrink-0" aria-hidden="true" />
            <div className="min-w-0 flex-1">
              <p>{catalogError || t("graph.connect.catalogMissing")}</p>
              {onRetryCatalog ? (
                <Button
                  variant="dangerSoft"
                  size="sm"
                  className="mt-2"
                  onClick={onRetryCatalog}
                  disabled={busy}
                  aria-label={t("workbench.retry")}
                >
                  <RotateCcw size={12} aria-hidden="true" />
                  {t("workbench.retry")}
                </Button>
              ) : null}
            </div>
          </div>
        ) : paletteTypes.map((nodeType) => {
          const theme = workflowNodeKindTheme(nodeType);
          const Icon = theme.icon;
          const descriptionKey = NODE_DESCRIPTIONS[nodeType];
          return (
            <button
              key={nodeType}
              type="button"
              disabled={busy}
              onClick={() => onCreate(nodeType)}
              className="group relative flex items-start gap-3 rounded-panel border border-border-l1 bg-surface-raised p-3 text-left transition-colors duration-fast hover:border-border-l3 hover:bg-surface-subtle disabled:cursor-not-allowed disabled:opacity-45"
            >
              <span className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border ${theme.iconBox}`}>
                <Icon size={16} />
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex items-center justify-between gap-2">
                  <span className="text-xs font-semibold text-text-primary">
                    {t(graphNodeTitleKey(nodeType))}
                  </span>
                  <span className="rounded bg-surface-subtle px-1.5 py-0.5 text-[9px] font-medium text-text-secondary">
                    + {t("graph.palette.add")}
                  </span>
                </div>
                <p className="mt-1 text-[11px] leading-4 text-text-muted">
                  {descriptionKey ? t(descriptionKey) : humanizeCatalogKey(nodeType)}
                </p>
              </div>
            </button>
          );
        })}
      </div>

      {onOpenRecipesTab ? (
        <div className="rounded-panel border border-border-l1 bg-surface-raised p-3.5">
          <div className="flex items-center gap-2">
            <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-control border border-border-l1 bg-surface-subtle text-text-secondary">
              <Boxes size={15} />
            </span>
            <div className="min-w-0 flex-1">
              <div className="text-xs font-bold text-text-primary">{t("workbench.sidebar.recipes")}</div>
              <div className="text-[10px] text-text-muted">{t("graph.palette.recipesHint")}</div>
            </div>
          </div>
          <button
            type="button"
            onClick={onOpenRecipesTab}
            className="mt-3 flex min-h-11 w-full items-center justify-center gap-1.5 rounded-panel border border-border-l1 bg-surface-raised py-1.5 text-xs font-semibold text-text-primary hover:bg-surface-subtle lg:min-h-9"
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
      className="group flex w-full items-center gap-2.5 rounded-panel border border-border-l1 bg-surface-raised p-2.5 text-left text-text-primary hover:bg-surface-subtle disabled:opacity-45"
    >
      <span className="flex h-11 w-11 shrink-0 items-center justify-center rounded-control bg-surface-subtle text-text-secondary lg:h-9 lg:w-9">
        {icon}
      </span>
      <div className="min-w-0 flex-1">
        <div className="text-xs font-bold">{title}</div>
        <div className="text-[10px] opacity-80">{hint}</div>
      </div>
    </button>
  );
}
