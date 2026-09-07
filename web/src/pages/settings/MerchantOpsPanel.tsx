import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { PauseCircle, PlayCircle } from "lucide-react";

import { api } from "../../lib/api";
import { ownMerchantId } from "../../lib/accountBoundary";
import { useI18n } from "../../lib/preferences";

/** Op 面最小接线：当前商家启停；完整管理员跨商管理另行交付。 */
export function MerchantOpsPanel() {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const sessionQuery = useQuery({
    queryKey: ["session"],
    queryFn: api.getSessionState,
    retry: false,
  });
  const merchant = sessionQuery.data?.merchant;
  const merchantId = ownMerchantId(sessionQuery.data);
  const suspended = merchant?.status === "suspended";

  const statusMutation = useMutation({
    mutationFn: (status: "active" | "suspended") => {
      if (!merchantId) {
        return Promise.reject(new Error("missing merchant"));
      }
      return api.setMerchantStatus(merchantId, status);
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["session"] });
    },
  });

  if (!merchantId || !merchant || !sessionQuery.data?.user?.is_operator) {
    return null;
  }

  return (
    <section
      aria-label={t("settings.merchantOps.title")}
      className="mx-5 mt-4 rounded-xl border border-border-l1 bg-surface-base/80 px-4 py-3 dark:border-border-l2 dark:bg-surface-base/50 lg:mx-8"
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="text-sm font-semibold text-text-primary dark:text-text-primary">
            {t("settings.merchantOps.title")}
          </p>
          <p className="mt-0.5 truncate text-xs text-text-muted dark:text-text-muted">
            {merchant.name}
            {" · "}
            {suspended ? t("settings.merchantOps.statusSuspended") : t("settings.merchantOps.statusActive")}
          </p>
        </div>
        <button
          type="button"
          disabled={statusMutation.isPending}
          onClick={() => statusMutation.mutate(suspended ? "active" : "suspended")}
          aria-label={suspended ? t("settings.merchantOps.activate") : t("settings.merchantOps.suspend")}
          title={suspended ? t("settings.merchantOps.activate") : t("settings.merchantOps.suspend")}
          className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-border-l1 bg-surface-raised px-3 text-xs font-semibold text-text-secondary transition-colors hover:border-accent hover:text-accent disabled:opacity-60 dark:border-border-l1 dark:bg-surface-base dark:text-text-primary"
        >
          {suspended ? <PlayCircle size={14} aria-hidden="true" /> : <PauseCircle size={14} aria-hidden="true" />}
          <span>{suspended ? t("settings.merchantOps.activate") : t("settings.merchantOps.suspend")}</span>
        </button>
      </div>
    </section>
  );
}
