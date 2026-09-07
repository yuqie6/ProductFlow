import { useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, KeyRound, LogOut, ShieldCheck, UserRound } from "lucide-react";
import { useNavigate } from "react-router-dom";

import { TopNav } from "../components/TopNav";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/field";
import { Dialog, DialogContent } from "../components/ui/dialog";
import { api, ApiError } from "../lib/api";
import { validateDisplayName, validateNewPassword } from "../lib/accountValidation";
import { useI18n } from "../lib/preferences";
import type { AccountProfile, AccountSession, SessionState } from "../lib/types";

export function AccountPage() {
  const { t, locale } = useI18n();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const session = useQuery({ queryKey: ["session"], queryFn: api.getSessionState });
  const userId = session.data?.user?.id;
  const profileKey = ["account", userId];
  const profile = useQuery({ queryKey: profileKey, queryFn: api.getAccount, enabled: Boolean(userId), retry: false });
  const [cursors, setCursors] = useState<string[]>([]);
  const after = cursors.at(-1);
  const sessions = useQuery({ queryKey: ["account-sessions", userId, after], queryFn: () => api.getAccountSessions({ after, limit: 20 }), enabled: Boolean(userId), retry: false });
  const revokeButton = useRef<HTMLButtonElement | null>(null);
  const [revokeTarget, setRevokeTarget] = useState<AccountSession | null>(null);
  const [sessionNotice, setSessionNotice] = useState("");
  const errorText = (error: Error | null) => error instanceof ApiError ? error.detail : t("account.error");
  const finishSession = () => {
    queryClient.setQueryData<SessionState>(["session"], { authenticated: false, access_required: true });
    navigate("/login", { replace: true });
  };
  const logout = useMutation({ mutationFn: api.destroySession, onSuccess: finishSession });
  const revoke = useMutation({
    mutationFn: (target: AccountSession) => api.revokeAccountSession(target.id),
    onSuccess: async (_, target) => {
      setRevokeTarget(null);
      if (target.current) { finishSession(); return; }
      setSessionNotice(t("account.revoked"));
      await queryClient.invalidateQueries({ queryKey: ["account-sessions", userId] });
    },
  });
  const formatDate = (value: string) => new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(new Date(value));

  return <div className="min-h-screen bg-surface-base text-text-primary">
    <TopNav breadcrumbs={t("account.title")} onHome={() => navigate("/home")} />
    <main className="mx-auto w-full max-w-5xl px-5 pt-8 pb-40 sm:px-8 sm:pt-12 lg:pb-20">
      <header className="mb-10 flex flex-wrap items-start justify-between gap-4">
        <div><h1 className="text-2xl font-semibold tracking-tight">{t("account.title")}</h1><p className="mt-2 text-sm text-text-muted">{t("account.subtitle")}</p></div>
        <Button busy={logout.isPending} onClick={() => logout.mutate()}><LogOut size={15} aria-hidden="true" />{t("nav.logout")}</Button>
      </header>
      {logout.isError ? <p role="alert" className="mb-5 text-sm text-state-error">{errorText(logout.error)}</p> : null}
      <section aria-labelledby="profile-heading" className="grid gap-6 border-t border-border-l1 py-8 md:grid-cols-[220px_minmax(0,1fr)]">
        <h2 id="profile-heading" className="flex items-start gap-2 font-semibold"><UserRound size={18} aria-hidden="true" />{t("account.profile")}</h2>
        {profile.isPending ? <p role="status" className="text-sm text-text-muted">{t("app.loading")}</p> : profile.isError ? <div><p role="alert" className="mb-3 text-sm text-state-error">{errorText(profile.error)}</p><Button onClick={() => void profile.refetch()}>{t("account.retry")}</Button></div> : <ProfileForm profile={profile.data} onSaved={(next) => {
          queryClient.setQueryData(profileKey, next);
          queryClient.setQueryData<SessionState>(["session"], (current) => current?.authenticated ? { ...current, ...next } : current);
        }} />}
      </section>
      <section aria-labelledby="password-heading" className="grid gap-6 border-t border-border-l1 py-8 md:grid-cols-[220px_minmax(0,1fr)]">
        <div><h2 id="password-heading" className="flex items-center gap-2 font-semibold"><KeyRound size={18} aria-hidden="true" />{t("account.security")}</h2><p className="mt-3 text-sm leading-6 text-text-muted">{t("account.passwordNote")}</p></div>
        <PasswordForm onSuccess={finishSession} />
      </section>
      <section aria-labelledby="sessions-heading" className="grid gap-6 border-t border-border-l1 py-8 md:grid-cols-[220px_minmax(0,1fr)]">
        <div><h2 id="sessions-heading" className="flex items-center gap-2 font-semibold"><ShieldCheck size={18} aria-hidden="true" />{t("account.sessions")}</h2><p className="mt-3 text-sm leading-6 text-text-muted">{t("account.sessionsNote")}</p></div>
        <div className="min-w-0">
          {sessionNotice ? <p role="status" className="mb-3 text-sm text-state-success">{sessionNotice}</p> : null}
          {sessions.isPending ? <p role="status" className="text-sm text-text-muted">{t("app.loading")}</p> : sessions.isError ? <div><p role="alert" className="mb-3 text-sm text-state-error">{errorText(sessions.error)}</p><Button onClick={() => void sessions.refetch()}>{t("account.retry")}</Button></div> : <>
            {sessions.data.items.length === 0 ? <p className="py-6 text-sm text-text-muted">{t("account.emptySessions")}</p> : <ul className="divide-y divide-border-l1">
              {sessions.data.items.map((item) => <li key={item.id} className="flex flex-wrap items-center justify-between gap-4 py-5 first:pt-0">
                <div className="min-w-0"><p className="mb-2 flex items-center gap-2 text-sm font-semibold">{item.current ? <Check size={15} className="text-accent" aria-hidden="true" /> : null}{t(item.current ? "account.currentSession" : "account.otherSession")}</p><dl className="grid gap-1 text-xs text-text-muted"><div><dt className="mr-2 inline">{t("account.created")}</dt><dd className="inline"><time dateTime={item.created_at}>{formatDate(item.created_at)}</time></dd></div><div><dt className="mr-2 inline">{t("account.expires")}</dt><dd className="inline"><time dateTime={item.expires_at}>{formatDate(item.expires_at)}</time></dd></div></dl></div>
                <Button onClick={(event) => { revokeButton.current = event.currentTarget; revoke.reset(); setRevokeTarget(item); }} disabled={revoke.isPending}>{t(item.current ? "account.revokeCurrent" : "account.revoke")}</Button>
              </li>)}
            </ul>}
            <div className="mt-5 flex flex-wrap items-center justify-between gap-3 border-t border-border-l1 pt-4"><span className="text-xs text-text-muted">{t("account.page", { page: cursors.length + 1 })}</span><div className="flex gap-2"><Button disabled={!cursors.length} onClick={() => setCursors((values) => values.slice(0, -1))}>{t("account.previous")}</Button><Button disabled={!sessions.data.next_cursor} onClick={() => { if (sessions.data.next_cursor) setCursors((values) => [...values, sessions.data.next_cursor!]); }}>{t("account.next")}</Button></div></div>
          </>}
        </div>
      </section>
    </main>
    <Dialog open={Boolean(revokeTarget)} onOpenChange={(open) => { if (!open && !revoke.isPending) setRevokeTarget(null); }}>
      <DialogContent onCloseAutoFocus={(event) => { event.preventDefault(); revokeButton.current?.focus(); }} title={t("account.revokeConfirm")} description={t(revokeTarget?.current ? "account.revokeCurrentConfirm" : "account.sessionsNote")} closeLabel={t("account.cancel")} onClose={() => { if (!revoke.isPending) setRevokeTarget(null); }} footer={<><Button disabled={revoke.isPending} onClick={() => setRevokeTarget(null)}>{t("account.cancel")}</Button><Button variant="danger" busy={revoke.isPending} onClick={() => { if (revokeTarget) revoke.mutate(revokeTarget); }}>{t(revokeTarget?.current ? "account.revokeCurrent" : "account.revoke")}</Button></>}>
        {revoke.isError ? <p role="alert" className="text-sm text-state-error">{errorText(revoke.error)}</p> : null}
      </DialogContent>
    </Dialog>
  </div>;
}

function ProfileForm({ profile, onSaved }: { profile: AccountProfile; onSaved: (profile: AccountProfile) => void }) {
  const { t } = useI18n();
  const [draft, setDraft] = useState<string | null>(null);
  const [error, setError] = useState("");
  const name = draft ?? profile.user.display_name;
  const save = useMutation({ mutationFn: () => api.updateAccount({ display_name: name.trim() }), onSuccess: (next) => { onSaved(next); setDraft(null); }, onError: (error) => setError(error instanceof ApiError ? error.detail : t("account.error")) });
  return <form className="max-w-lg space-y-5" onSubmit={(event) => { event.preventDefault(); if (save.isPending) return; const invalid = validateDisplayName(name); setError(invalid ? t(invalid) : ""); if (!invalid) save.mutate(); }}>
    <Input label={t("login.email")} type="email" value={profile.user.email} readOnly autoComplete="username" />
    <Input label={t("account.displayName")} value={name} onChange={(event) => { setDraft(event.target.value); setError(""); save.reset(); }} autoComplete="nickname" disabled={save.isPending} aria-invalid={Boolean(error)} />
    {profile.merchant ? <div><p className="text-xs font-semibold text-text-muted">{t("account.merchant")}</p><p className="mt-1 break-words text-sm">{profile.merchant.name}</p></div> : null}
    {profile.user.is_operator ? <p className="text-xs font-medium text-text-muted">{t("account.operator")}</p> : null}
    {error ? <p role="alert" className="text-sm text-state-error">{error}</p> : null}
    <div className="flex flex-wrap items-center gap-3"><Button type="submit" variant="primary" busy={save.isPending} disabled={name.trim() === profile.user.display_name}>{t("account.save")}</Button>{save.isSuccess ? <p role="status" className="text-sm text-state-success">{t("account.saved")}</p> : null}</div>
  </form>;
}

function PasswordForm({ onSuccess }: { onSuccess: () => void }) {
  const { t } = useI18n();
  const [current, setCurrent] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [error, setError] = useState("");
  const mutation = useMutation({ mutationFn: () => api.changePassword({ current_password: current, new_password: password }), onSuccess, onError: (error) => setError(error instanceof ApiError ? error.detail : t("account.error")) });
  return <form className="max-w-lg space-y-5" onSubmit={(event) => { event.preventDefault(); if (mutation.isPending) return; const invalid = validateNewPassword(password, confirmation); setError(invalid ? t(invalid) : ""); if (!invalid) mutation.mutate(); }}>
    <Input label={t("account.currentPassword")} type="password" autoComplete="current-password" value={current} onChange={(event) => setCurrent(event.target.value)} required disabled={mutation.isPending} />
    <Input label={t("account.newPassword")} type="password" autoComplete="new-password" value={password} onChange={(event) => setPassword(event.target.value)} required disabled={mutation.isPending} aria-describedby="account-password-rule" />
    <p id="account-password-rule" className="text-xs text-text-muted">{t("account.passwordRule")}</p>
    <Input label={t("account.confirmPassword")} type="password" autoComplete="new-password" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} required disabled={mutation.isPending} />
    {error ? <p role="alert" className="text-sm text-state-error">{error}</p> : null}
    <Button type="submit" busy={mutation.isPending}>{t("account.changePassword")}</Button>
  </form>;
}
