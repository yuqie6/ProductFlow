import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, LayoutGrid, Mail } from "lucide-react";
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
  const [merchantName, setMerchantName] = useState("");
  const [mode, setMode] = useState<"login" | "register">("login");
  const [code, setCode] = useState("");
  const [challenge, setChallenge] = useState<{ id: string; email: string } | null>(null);
  const [sendRetryAt, setSendRetryAt] = useState(0);
  const [sendSeconds, setSendSeconds] = useState(0);
  const emailInput = useRef<HTMLInputElement>(null);
  const formVersion = useRef(0);
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
  const registrationAvailable = Boolean(sessionQuery.data?.registration_available) && !needsBootstrap;
  const registering = mode === "register" && !needsBootstrap;

  useEffect(() => {
    if (!retryAt && !sendRetryAt) return;
    const timer = window.setInterval(() => {
      setRetrySeconds(Math.max(0, Math.ceil((retryAt - Date.now()) / 1000)));
      setSendSeconds(Math.max(0, Math.ceil((sendRetryAt - Date.now()) / 1000)));
      if (Date.now() >= Math.max(retryAt, sendRetryAt)) window.clearInterval(timer);
    }, 250);
    return () => window.clearInterval(timer);
  }, [retryAt, sendRetryAt]);

  useEffect(() => {
    if (authenticated) {
      navigate("/products", { replace: true });
    }
  }, [authenticated, navigate]);

  const finishLogin = async () => {
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

  const registrationMutation = useMutation({
    mutationFn: () => api.registerAccount({
      email,
      challenge_id: challenge?.id ?? "",
      code,
      password,
      merchant_name: merchantName,
    }),
    onSuccess: finishLogin,
    onError: handleLoginError,
  });

  const sendCodeMutation = useMutation({
    mutationFn: (input: { email: string; version: number }) => api.requestRegistrationCode(input.email),
    onSuccess: (result, input) => {
      if (input.version !== formVersion.current) return;
      setChallenge({ id: result.challenge_id, email: input.email });
      setCode("");
      setSendSeconds(result.retry_after_seconds);
      setSendRetryAt(Date.now() + result.retry_after_seconds * 1000);
    },
    onError: (mutationError, input) => {
      if (input.version === formVersion.current) handleLoginError(mutationError);
    },
  });

  const pending = loginMutation.isPending || bootstrapMutation.isPending || registrationMutation.isPending;

  const handleSubmit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (pending || Date.now() < retryAt) return;
    setError("");
    if (needsBootstrap) {
      bootstrapMutation.mutate();
      return;
    }
    if (registering) {
      if (!registrationAvailable || challenge?.email !== email.trim().toLowerCase() || sendCodeMutation.isPending) return;
      registrationMutation.mutate();
      return;
    }
    loginMutation.mutate();
  };

  return (
    <div className="relative flex min-h-screen flex-col items-center justify-center bg-surface-base dark:bg-surface-base dark:text-text-primary">

      <div className="relative w-full max-w-sm px-6 py-8">
        <div className="mb-10">
          <div className="mb-4 flex h-10 w-10 items-center justify-center rounded-lg bg-text-muted shadow-sm dark:border dark:border-accent/35 dark:bg-accent/18">
            <LayoutGrid size={20} className="text-white" strokeWidth={2} />
          </div>
          <h1 className="text-2xl font-semibold tracking-tight text-text-primary dark:text-white">ProductFlow</h1>
          <p className="mt-1 text-sm text-text-muted dark:text-text-muted">
            {needsBootstrap ? t("login.bootstrapSubtitle") : registering ? t("login.registerSubtitle") : t("login.subtitle")}
          </p>
        </div>

        {!needsBootstrap ? (
          <div role="tablist" aria-label={t("login.accountAccess")} className="mb-6 grid grid-cols-2 border-b border-border-l1 dark:border-border-l1">
            {(["login", "register"] as const).map((value) => (
              <button key={value} type="button" role="tab" aria-selected={mode === value} disabled={pending}
                onClick={() => { formVersion.current += 1; setMode(value); setChallenge(null); setCode(""); setError(""); }}
                className={`min-h-11 border-b-2 px-3 text-sm ${mode === value ? "border-border-l3 text-text-primary dark:border-white dark:text-white" : "border-transparent text-text-muted dark:text-text-muted"}`}>
                {t(value === "login" ? "login.submit" : "login.register")}
              </button>
            ))}
          </div>
        ) : null}

        <form onSubmit={handleSubmit} className="space-y-4">
          {registering && !registrationAvailable ? (
            <p role="status" className="text-sm text-state-warning dark:text-state-warning">{t("login.registrationUnavailable")}</p>
          ) : null}
          {needsBootstrap ? (
              <div>
                <label htmlFor="auth-admin-key" className="mb-1.5 block text-[11px] font-semibold uppercase tracking-widest text-text-muted dark:text-text-muted">
                  {t("login.adminKey")}
                </label>
                <input
                  id="auth-admin-key"
                  type="password"
                  value={adminKey}
                  onChange={(event) => setAdminKey(event.target.value)}
                  className="w-full rounded-md border border-border-l1 bg-surface-raised px-3 py-2 text-sm text-text-primary transition-shadow placeholder:text-text-muted focus:border-border-l3 focus:outline-none focus:ring-1 focus:ring-border-l3 dark:border-border-l1 dark:bg-surface-panel dark:text-text-primary dark:placeholder:text-text-muted dark:focus:border-accent dark:focus:ring-accent/25"
                  placeholder={t("login.adminKeyPlaceholder")}
                  autoComplete="off"
                  required
                  disabled={pending}
                />
              </div>
          ) : null}
          {needsBootstrap || registering ? (
              <div>
                <label htmlFor="auth-merchant-name" className="mb-1.5 block text-[11px] font-semibold uppercase tracking-widest text-text-muted dark:text-text-muted">
                  {t("login.merchantName")}
                </label>
                <input
                  id="auth-merchant-name"
                  type="text"
                  value={merchantName}
                  onChange={(event) => setMerchantName(event.target.value)}
                  className="w-full rounded-md border border-border-l1 bg-surface-raised px-3 py-2 text-sm text-text-primary transition-shadow placeholder:text-text-muted focus:border-border-l3 focus:outline-none focus:ring-1 focus:ring-border-l3 dark:border-border-l1 dark:bg-surface-panel dark:text-text-primary dark:placeholder:text-text-muted dark:focus:border-accent dark:focus:ring-accent/25"
                  placeholder={t("login.merchantNamePlaceholder")}
                  autoComplete="organization"
                  required
                  disabled={pending}
                />
              </div>
          ) : null}

          <div>
            <label htmlFor="auth-email" className="mb-1.5 block text-[11px] font-semibold uppercase tracking-widest text-text-muted dark:text-text-muted">
              {t("login.email")}
            </label>
            <input
              id="auth-email"
              ref={emailInput}
              type="email"
              value={email}
              onChange={(event) => { formVersion.current += 1; setEmail(event.target.value); setChallenge(null); setCode(""); }}
              className="w-full rounded-md border border-border-l1 bg-surface-raised px-3 py-2 text-sm text-text-primary transition-shadow placeholder:text-text-muted focus:border-border-l3 focus:outline-none focus:ring-1 focus:ring-border-l3 dark:border-border-l1 dark:bg-surface-panel dark:text-text-primary dark:placeholder:text-text-muted dark:focus:border-accent dark:focus:ring-accent/25"
              placeholder={t("login.emailPlaceholder")}
              autoComplete="username"
              required
              disabled={pending}
            />
          </div>

          {registering ? (
            <div>
              <label htmlFor="registration-code" className="mb-1.5 block text-xs font-medium text-text-muted dark:text-text-muted">{t("login.verificationCode")}</label>
              <div className="flex min-w-0 gap-2">
                <input id="registration-code" value={code} onChange={(event) => setCode(event.target.value)}
                  inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" maxLength={6} required disabled={pending}
                  className="min-w-0 flex-1 rounded-md border border-border-l1 bg-surface-raised px-3 py-2 text-sm text-text-primary dark:border-border-l1 dark:bg-surface-panel dark:text-text-primary" />
                <button type="button" disabled={!registrationAvailable || pending || sendCodeMutation.isPending || sendSeconds > 0 || retrySeconds > 0}
                  onClick={() => {
                    if (!registrationAvailable || !emailInput.current?.reportValidity() || Date.now() < Math.max(sendRetryAt, retryAt)) return;
                    setError("");
                    sendCodeMutation.mutate({ email: email.trim().toLowerCase(), version: formVersion.current });
                  }}
                  className="inline-flex min-h-11 max-w-[55%] items-center justify-center gap-2 rounded-md border border-border-l3 px-3 text-xs text-text-secondary disabled:opacity-50 dark:border-border-l3 dark:text-text-primary">
                  <Mail size={14} className="shrink-0" aria-hidden="true" />
                  <span>{sendSeconds > 0 ? t("login.resendAfter", { seconds: sendSeconds }) : t("login.sendCode")}</span>
                </button>
              </div>
              {challenge ? <p role="status" className="mt-2 text-xs text-state-success dark:text-state-success">{t("login.codeSent")}</p> : null}
            </div>
          ) : null}

          <div>
            <label htmlFor="auth-password" className="mb-1.5 block text-[11px] font-semibold uppercase tracking-widest text-text-muted dark:text-text-muted">
              {t("login.password")}
            </label>
            <input
              id="auth-password"
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              className="w-full rounded-md border border-border-l1 bg-surface-raised px-3 py-2 text-sm text-text-primary transition-shadow placeholder:text-text-muted focus:border-border-l3 focus:outline-none focus:ring-1 focus:ring-border-l3 dark:border-border-l1 dark:bg-surface-panel dark:text-text-primary dark:placeholder:text-text-muted dark:focus:border-accent dark:focus:ring-accent/25"
              placeholder={t("login.passwordPlaceholder")}
              autoComplete={needsBootstrap || registering ? "new-password" : "current-password"}
              required
              disabled={pending}
            />
          </div>

          {error ? <div role="alert" className="break-words text-xs font-medium text-state-error dark:text-state-error">{error}</div> : null}
          {retrySeconds > 0 ? (
            <p role="status" className="text-xs text-text-muted dark:text-text-muted">
              {t("login.retryAfter", { seconds: retrySeconds })}
            </p>
          ) : null}

          <button
            type="submit"
            disabled={pending || retrySeconds > 0 || (registering && (!registrationAvailable || !challenge || sendCodeMutation.isPending))}
            className="flex w-full items-center justify-center rounded-md bg-accent px-4 py-2 text-sm font-medium text-accent-fg shadow-sm transition-colors hover:bg-accent-strong disabled:opacity-60 dark:ring-1 dark:ring-accent/35"
          >
            {needsBootstrap ? t("login.bootstrapSubmit") : registering ? t("login.registerSubmit") : t("login.submit")}{" "}
            <ArrowRight size={14} className="ml-2 opacity-70" />
          </button>
        </form>
      </div>
    </div>
  );
}
