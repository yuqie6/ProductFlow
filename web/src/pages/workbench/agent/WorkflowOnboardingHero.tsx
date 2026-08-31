import {
  ArrowRight,
  BookTemplate,
  Bot,
  Layers,
  Sparkles,
  Wand2,
} from "lucide-react";

import { Button } from "../../../components/ui/button";
import { useI18n } from "../../../lib/preferences";

export interface WorkflowOnboardingHeroProps {
  productName: string;
  onOpenAgent: () => void;
  onOpenRecipes: () => void;
  onOpenAddPanel: () => void;
}

const CARD_CLASS =
  "group relative flex flex-col justify-between rounded-surface p-5 text-left shadow-elev-1 transition-[transform,box-shadow,border-color] duration-fast hover:-translate-y-1 hover:shadow-elev-2 motion-reduce:transform-none motion-reduce:transition-none";
const ICON_CLASS =
  "flex h-10 w-10 items-center justify-center rounded-panel transition-transform group-hover:scale-105 motion-reduce:transform-none";

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
      className="flex h-full min-h-0 w-full items-center justify-center overflow-y-auto bg-surface-base p-6"
    >
      <div className="flex w-full max-w-3xl flex-col items-center text-center">
        <div className="inline-flex items-center gap-1.5 rounded-full border border-accent/30 bg-accent-soft px-3 py-1 text-xs font-semibold text-accent shadow-elev-1">
          <Sparkles size={13} />
          <span>{productName}</span>
        </div>

        <h2 className="mt-3 text-xl font-extrabold tracking-tight text-text-primary sm:text-2xl">
          {t("agentWorkbench.onboarding.title")}
        </h2>
        <p className="mt-1.5 max-w-xl text-xs leading-relaxed text-text-muted sm:text-sm">
          {t("agentWorkbench.onboarding.subtitle")}
        </p>

        <div className="mt-8 grid w-full grid-cols-1 gap-4 sm:grid-cols-3">
          <div className={`${CARD_CLASS} border border-accent bg-accent-soft`}>
            <div>
              <div className={`${ICON_CLASS} bg-accent text-accent-fg`}>
                <Bot size={20} />
              </div>
              <h3 className="mt-3.5 text-sm font-bold text-text-primary">
                {t("agentWorkbench.onboarding.agentTitle")}
              </h3>
              <p className="mt-1.5 text-xs leading-relaxed text-text-muted">
                {t("agentWorkbench.onboarding.agentDesc")}
              </p>
            </div>
            <Button
              type="button"
              variant="primary"
              size="lg"
              onClick={onOpenAgent}
              className="mt-5 w-full justify-between"
            >
              <span>{t("agentWorkbench.onboarding.agentAction")}</span>
              <Wand2 size={13} />
            </Button>
          </div>

          <div className={`${CARD_CLASS} border border-border-l1 bg-surface-raised hover:border-border-l3`}>
            <div>
              <div className={`${ICON_CLASS} bg-surface-subtle text-text-secondary`}>
                <BookTemplate size={20} />
              </div>
              <h3 className="mt-3.5 text-sm font-bold text-text-primary">
                {t("agentWorkbench.onboarding.recipeTitle")}
              </h3>
              <p className="mt-1.5 text-xs leading-relaxed text-text-muted">
                {t("agentWorkbench.onboarding.recipeDesc")}
              </p>
            </div>
            <Button
              type="button"
              variant="secondary"
              size="lg"
              onClick={onOpenRecipes}
              className="mt-5 w-full justify-between"
            >
              <span>{t("agentWorkbench.onboarding.recipeAction")}</span>
              <ArrowRight size={13} />
            </Button>
          </div>

          <div className={`${CARD_CLASS} border border-border-l1 bg-surface-raised hover:border-border-l3`}>
            <div>
              <div className={`${ICON_CLASS} bg-surface-subtle text-text-secondary`}>
                <Layers size={20} />
              </div>
              <h3 className="mt-3.5 text-sm font-bold text-text-primary">
                {t("agentWorkbench.onboarding.manualTitle")}
              </h3>
              <p className="mt-1.5 text-xs leading-relaxed text-text-muted">
                {t("agentWorkbench.onboarding.manualDesc")}
              </p>
            </div>
            <Button
              type="button"
              variant="secondary"
              size="lg"
              onClick={onOpenAddPanel}
              className="mt-5 w-full justify-between"
            >
              <span>{t("agentWorkbench.onboarding.manualAction")}</span>
              <ArrowRight size={13} />
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
