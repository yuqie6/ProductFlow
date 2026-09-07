import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, LayoutGrid } from "lucide-react";
import { useNavigate } from "react-router-dom";

import { api, ApiError } from "../lib/api";
import { useI18n } from "../lib/preferences";

interface LoginPageProps {
  authenticated: boolean;
}

export function LoginPage({ authenticated }: LoginPageProps) {
  const { t } = useI18n();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [adminKey, setAdminKey] = useState("");
  const [merchantName, setMerchantName] = useState("开发商家");
  const [error, setError] = useState("");
  const [retryAt, setRetryAt] = useState(0);
  const [retrySeconds, setRetrySeconds] = useState(0);
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const sessionQuery = useQuery({
    queryKey: ["session"],
    queryFn: api.getSessionState,
  });
  const needsBootstrap = Boolean(sessionQuery.data?.needs_bootstrap);

  useEffect(() => {
    if (!retryAt) return;
    const timer = window.setInterval(() => {
      setRetrySeconds(Math.max(0, Math.ceil((retryAt - Date.now()) / 1000)));
      if (Date.now() >= retryAt) window.clearInterval(timer);
    }, 250);
    return () => window.clearInterval(timer);
  }, [retryAt]);

  useEffect(() => {
    if (authenticated) {
      navigate("/products", { replace: true });
    }
  }, [authenticated, navigate]);

  const finishLogin = async () => {
    queryClient.removeQueries({ queryKey: ["settings-lock-state"] });
    queryClient.removeQueries({ queryKey: ["config"] });
    await queryClient.invalidateQueries({ queryKey: ["session"] });
    navigate("/products", { replace: true });
  };

  const handleLoginError = (mutationError: Error) => {
    if (mutationError instanceof ApiError) {
      setError(mutationError.detail);
      if (mutationError.status === 429 && mutationError.retryAfterSeconds !== null) {
        setRetrySeconds(mutationError.retryAfterSeconds);
        setRetryAt(Date.now() + mutationError.retryAfterSeconds * 1000);
      }
      return;
    }
    setError(t("login.error"));
  };

  const loginMutation = useMutation({
    mutationFn: () => api.createSession({ email, password }),
    onSuccess: finishLogin,
    onError: handleLoginError,
  });

  const bootstrapMutation = useMutation({
    mutationFn: () =>
      api.bootstrapSession({
        admin_key: adminKey,
        email,
        password,
        merchant_name: merchantName,
      }),
    onSuccess: finishLogin,
    onError: handleLoginError,
  });

  const pending = loginMutation.isPending || bootstrapMutation.isPending;

  const handleSubmit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (pending || Date.now() < retryAt) return;
    setError("");
    if (needsBootstrap) {
      bootstrapMutation.mutate();
      return;
    }
    loginMutation.mutate();
  };

  return (
    <div className="relative flex min-h-screen flex-col items-center justify-center bg-zinc-50 dark:bg-[#060a12] dark:text-slate-100">
      <div className="absolute inset-0 bg-[linear-gradient(to_right,#e4e4e7_1px,transparent_1px),linear-gradient(to_bottom,#e4e4e7_1px,transparent_1px)] bg-[size:4rem_4rem] opacity-50 [mask-image:radial-gradient(ellipse_60%_60%_at_50%_50%,#000_70%,transparent_100%)] dark:bg-[linear-gradient(to_right,rgba(71,85,105,0.34)_1px,transparent_1px),linear-gradient(to_bottom,rgb(71,85,105,0.34)_1px,transparent_1px)] dark:opacity-70" />

      <div className="relative w-full max-w-sm px-6">
        <div className="mb-10">
          <div className="mb-4 flex h-10 w-10 items-center justify-center rounded-lg bg-zinc-900 shadow-sm shadow-zinc-900/20 dark:border dark:border-violet-400/35 dark:bg-violet-500/18 dark:shadow-violet-950/30">
            <LayoutGrid size={20} className="text-white" strokeWidth={2} />
          </div>
          <h1 className="text-2xl font-semibold tracking-tight text-zinc-900 dark:text-white">ProductFlow</h1>
          <p className="mt-1 text-sm text-zinc-500 dark:text-slate-400">
            {needsBootstrap ? t("login.bootstrapSubtitle") : t("login.subtitle")}
          </p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          {needsBootstrap ? (
            <>
              <div>
                <label className="mb-1.5 block text-[11px] font-semibold uppercase tracking-widest text-zinc-500 dark:text-slate-400">
                  {t("login.adminKey")}
                </label>
                <input
                  type="password"
                  value={adminKey}
                  onChange={(event) => setAdminKey(event.target.value)}
                  className="w-full rounded-md border border-zinc-200 bg-white px-3 py-2 text-sm text-zinc-950 transition-shadow placeholder:text-zinc-400 focus:border-zinc-900 focus:outline-none focus:ring-1 focus:ring-zinc-900 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-100 dark:placeholder:text-slate-500 dark:focus:border-violet-400 dark:focus:ring-violet-400/25"
                  placeholder={t("login.adminKeyPlaceholder")}
                  autoComplete="off"
                />
              </div>
              <div>
                <label className="mb-1.5 block text-[11px] font-semibold uppercase tracking-widest text-zinc-500 dark:text-slate-400">
                  {t("login.merchantName")}
                </label>
                <input
                  type="text"
                  value={merchantName}
                  onChange={(event) => setMerchantName(event.target.value)}
                  className="w-full rounded-md border border-zinc-200 bg-white px-3 py-2 text-sm text-zinc-950 transition-shadow placeholder:text-zinc-400 focus:border-zinc-900 focus:outline-none focus:ring-1 focus:ring-zinc-900 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-100 dark:placeholder:text-slate-500 dark:focus:border-violet-400 dark:focus:ring-violet-400/25"
                  placeholder={t("login.merchantNamePlaceholder")}
                  autoComplete="organization"
                />
              </div>
            </>
          ) : null}

          <div>
            <label className="mb-1.5 block text-[11px] font-semibold uppercase tracking-widest text-zinc-500 dark:text-slate-400">
              {t("login.email")}
            </label>
            <input
              type="email"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              className="w-full rounded-md border border-zinc-200 bg-white px-3 py-2 text-sm text-zinc-950 transition-shadow placeholder:text-zinc-400 focus:border-zinc-900 focus:outline-none focus:ring-1 focus:ring-zinc-900 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-100 dark:placeholder:text-slate-500 dark:focus:border-violet-400 dark:focus:ring-violet-400/25"
              placeholder={t("login.emailPlaceholder")}
              autoComplete="username"
            />
          </div>

          <div>
            <label className="mb-1.5 block text-[11px] font-semibold uppercase tracking-widest text-zinc-500 dark:text-slate-400">
              {t("login.password")}
            </label>
            <input
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              className="w-full rounded-md border border-zinc-200 bg-white px-3 py-2 text-sm text-zinc-950 transition-shadow placeholder:text-zinc-400 focus:border-zinc-900 focus:outline-none focus:ring-1 focus:ring-zinc-900 dark:border-slate-700 dark:bg-[#0b1220] dark:text-slate-100 dark:placeholder:text-slate-500 dark:focus:border-violet-400 dark:focus:ring-violet-400/25"
              placeholder={t("login.passwordPlaceholder")}
              autoComplete={needsBootstrap ? "new-password" : "current-password"}
            />
          </div>

          {error ? <div role="alert" className="break-words text-xs font-medium text-red-500 dark:text-red-300">{error}</div> : null}
          {retrySeconds > 0 ? (
            <p role="status" className="text-xs text-zinc-500 dark:text-slate-400">
              {t("login.retryAfter", { seconds: retrySeconds })}
            </p>
          ) : null}

          <button
            type="submit"
            disabled={pending || retrySeconds > 0}
            className="flex w-full items-center justify-center rounded-md bg-zinc-900 px-4 py-2 text-sm font-medium text-white shadow-sm shadow-zinc-900/20 transition-colors hover:bg-zinc-800 disabled:opacity-60 dark:bg-gradient-to-r dark:from-indigo-500 dark:to-violet-500 dark:shadow-violet-900/35 dark:ring-1 dark:ring-violet-300/35"
          >
            {needsBootstrap ? t("login.bootstrapSubmit") : t("login.submit")}{" "}
            <ArrowRight size={14} className="ml-2 opacity-70" />
          </button>
        </form>
      </div>
    </div>
  );
}
