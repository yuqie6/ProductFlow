import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Check, KeyRound } from "lucide-react";
import { Link } from "react-router-dom";

import { Button } from "../components/ui/button";
import { Input } from "../components/ui/field";
import { api, ApiError } from "../lib/api";
import { validateNewPassword } from "../lib/accountValidation";
import { useI18n } from "../lib/preferences";
import type { PasswordRecoveryChallenge, SessionState } from "../lib/types";

export function PasswordRecoveryPage() {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [email, setEmail] = useState("");
  const [challenge, setChallenge] = useState<(PasswordRecoveryChallenge & { email: string; expiresAt: number }) | null>(null);
  const [code, setCode] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [error, setError] = useState("");
  const [retryAt, setRetryAt] = useState(0);
  const [now, setNow] = useState(Date.now());
  const [complete, setComplete] = useState(false);
  useEffect(() => {
    if (!retryAt && !challenge) return;
    const timer = window.setInterval(() => setNow(Date.now()), 250);
    return () => window.clearInterval(timer);
  }, [retryAt, challenge]);
  const remaining = Math.max(0, Math.ceil((retryAt - now) / 1000));
  const expired = Boolean(challenge && now >= challenge.expiresAt);
  const handleError = (error: Error) => {
    setError(error instanceof ApiError ? error.code === "invalid_recovery_code" ? t("recovery.invalidCode") : error.detail : t("account.error"));
    if (error instanceof ApiError && error.retryAfterSeconds !== null) {
      setNow(Date.now());
      setRetryAt(Date.now() + error.retryAfterSeconds * 1000);
    }
  };
  const request = useMutation({
    mutationFn: (address: string) => api.requestPasswordRecovery(address),
    onSuccess: (result, address) => {
      setChallenge({ ...result, email: address, expiresAt: Date.now() + result.expires_in_seconds * 1000 });
      setCode(""); setError(""); setNow(Date.now());
      setRetryAt(Date.now() + result.resend_after_seconds * 1000);
    },
    onError: handleError,
  });
  const confirm = useMutation({
    mutationFn: () => api.confirmPasswordRecovery({ email: challenge!.email, challenge_id: challenge!.challenge_id, code, new_password: password }),
    onSuccess: () => {
      setComplete(true); setChallenge(null); setPassword(""); setConfirmation(""); setCode("");
      queryClient.setQueryData<SessionState>(["session"], { authenticated: false, access_required: true });
    },
    onError: handleError,
  });
  const pending = request.isPending || confirm.isPending;
  const send = () => { if (pending || Date.now() < retryAt) return; setError(""); request.mutate((challenge?.email ?? email).trim().toLowerCase()); };
  return <main className="flex min-h-screen items-center justify-center bg-surface-base px-6 py-12 text-text-primary">
    <div className="w-full max-w-md">
      <Link to="/login" className="mb-8 flex w-fit min-h-11 items-center gap-2 rounded-control text-sm text-text-muted hover:text-text-primary focus-visible:ring-2 focus-visible:ring-focus-ring"><ArrowLeft size={16} aria-hidden="true" />{t("recovery.back")}</Link>
      <div className="mb-5 inline-flex h-11 w-11 items-center justify-center rounded-panel bg-accent-soft text-accent">{complete ? <Check size={22} aria-hidden="true" /> : <KeyRound size={22} aria-hidden="true" />}</div>
      <h1 className="text-2xl font-semibold tracking-tight">{t("recovery.title")}</h1>
      {complete ? <div className="mt-5"><p role="status" className="text-sm leading-6 text-text-secondary">{t("recovery.success")}</p><Link to="/login" className="mt-6 inline-flex min-h-11 items-center rounded-control bg-accent px-4 text-sm font-semibold text-accent-fg focus-visible:ring-2 focus-visible:ring-focus-ring">{t("recovery.back")}</Link></div> : <>
        <p className="mt-3 text-sm leading-6 text-text-muted">{t("recovery.subtitle")}</p>
        <form className="mt-8 space-y-5" onSubmit={(event) => {
          event.preventDefault(); if (pending) return;
          if (!challenge) { send(); return; }
          if (expired) { setError(t("recovery.expired")); return; }
          const invalid = validateNewPassword(password, confirmation);
          setError(invalid ? t(invalid) : ""); if (!invalid) confirm.mutate();
        }}>
          <Input label={t("login.email")} type="email" autoComplete="username" value={challenge?.email ?? email} onChange={(event) => setEmail(event.target.value)} readOnly={Boolean(challenge)} required disabled={pending} maxLength={320} />
          {challenge ? <>
            <p role="status" className="rounded-control border border-border-l1 bg-surface-panel p-3 text-sm leading-6 text-text-secondary">{t("recovery.accepted")}</p>
            <Input label={t("login.verificationCode")} value={code} onChange={(event) => setCode(event.target.value)} autoComplete="one-time-code" inputMode="numeric" pattern="[0-9]{6}" maxLength={6} required disabled={pending} aria-describedby="recovery-expiry" />
            <p id="recovery-expiry" className={`text-xs ${expired ? "text-state-error" : "text-text-muted"}`}>{expired ? t("recovery.expired") : t("recovery.expiry", { minutes: Math.ceil(challenge.expires_in_seconds / 60) })}</p>
            <div className="flex flex-wrap gap-2"><Button busy={request.isPending} disabled={pending || remaining > 0} onClick={send}>{remaining > 0 ? t("login.resendAfter", { seconds: remaining }) : t("recovery.resend")}</Button><Button variant="ghost" disabled={pending} onClick={() => { setChallenge(null); setCode(""); setError(""); }}>{t("recovery.changeEmail")}</Button></div>
            <Input label={t("account.newPassword")} type="password" autoComplete="new-password" value={password} onChange={(event) => setPassword(event.target.value)} required disabled={pending} aria-describedby="recovery-password-rule" />
            <p id="recovery-password-rule" className="text-xs text-text-muted">{t("account.passwordRule")}</p>
            <Input label={t("account.confirmPassword")} type="password" autoComplete="new-password" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} required disabled={pending} />
          </> : null}
          {error ? <p role="alert" className="break-words text-sm text-state-error">{error}</p> : null}
          {!challenge && remaining > 0 ? <p role="status" className="text-xs text-text-muted">{t("login.retryAfter", { seconds: remaining })}</p> : null}
          <Button type="submit" className="w-full" variant="primary" busy={pending} disabled={challenge ? expired : remaining > 0}>{t(challenge ? "recovery.confirm" : "recovery.request")}</Button>
        </form>
      </>}
    </div>
  </main>;
}
