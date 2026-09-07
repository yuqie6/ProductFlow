import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { PauseCircle, PlayCircle } from "lucide-react";

import { api } from "../../lib/api";
import { activeMerchantId } from "../../lib/merchantBoundary";
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
  const merchantId = activeMerchantId(sessionQuery.data);
  const membership = sessionQuery.data?.memberships?.find((item) => item.merchant_id === merchantId)
    ?? sessionQuery.data?.memberships?.[0];
  const suspended = membership?.merchant_status === "suspended";

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

  if (!merchantId || !membership) {
    return null;
  }

  return (
    <section
      aria-label={t("settings.merchantOps.title")}
      className="mx-5 mt-4 rounded-xl border border-slate-200 bg-slate-50/80 px-4 py-3 dark:border-slate-800 dark:bg-slate-900/50 lg:mx-8"
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="text-sm font-semibold text-slate-900 dark:text-slate-100">
            {t("settings.merchantOps.title")}
          </p>
          <p className="mt-0.5 truncate text-xs text-slate-500 dark:text-slate-400">
            {membership.merchant_name}
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
          className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-slate-200 bg-white px-3 text-xs font-semibold text-slate-700 transition-colors hover:border-indigo-200 hover:text-indigo-700 disabled:opacity-60 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-200"
        >
          {suspended ? <PlayCircle size={14} aria-hidden="true" /> : <PauseCircle size={14} aria-hidden="true" />}
          <span>{suspended ? t("settings.merchantOps.activate") : t("settings.merchantOps.suspend")}</span>
        </button>
      </div>
    </section>
  );
}
