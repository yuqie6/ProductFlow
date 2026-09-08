import { useRef, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, KeyRound, LogOut, ShieldCheck, UserRound, Palette, Store, Sun, Moon, Monitor, ChevronDown } from "lucide-react";
import { Link, useLocation, useNavigate } from "react-router-dom";

import "./account.css";

import { preferenceSaveMessage } from "../lib/preferenceSaveMessages";
import { accountPreferencesMessage } from "../lib/accountPreferencesMessages";
import { TopNav } from "../components/TopNav";
import { Button } from "../components/ui/button";
import { Select } from "../components/ui/select";
import { LOCALES, LOCALE_LABEL_KEYS, isLocale } from "../lib/i18n";
import { THEME_PREFERENCES } from "../lib/theme";
import { getAccountGeneration, isCurrentAccountGeneration } from "../lib/accountBoundary";
import { Input } from "../components/ui/field";
import { Dialog, DialogContent } from "../components/ui/dialog";
import { api, ApiError } from "../lib/api";
import { validateDisplayName, validateNewPassword } from "../lib/accountValidation";
import { useI18n, usePreferences } from "../lib/preferences";
import type { AccountProfile, AccountSession, SessionMerchant, SessionState } from "../lib/types";

export function AccountPage() {
  const { t, locale } = useI18n();
  const navigate = useNavigate();
  const { hash } = useLocation();
  const activeSection = hash === "#preferences" ? "preferences" : hash === "#security" ? "security" : "profile";
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
  const currentAccount = useCurrentAccountWrite();
  const finishSession = (generation: number) => {
    if (!currentAccount(generation)) return;
    queryClient.setQueryData<SessionState>(["session"], { authenticated: false, access_required: true });
    navigate("/login", { replace: true });
  };
  const logout = useMutation<{ ok: boolean }, Error, number>({ mutationFn: api.destroySession, onSuccess: (_, generation) => finishSession(generation) });
  const revoke = useMutation({
    mutationFn: ({ target }: { target: AccountSession; generation: number }) => api.revokeAccountSession(target.id),
    onSuccess: async (_, { target, generation }) => {
      if (!currentAccount(generation)) return;
      setRevokeTarget(null);
      if (target.current) { finishSession(generation); return; }
      setSessionNotice(t("account.revoked"));
      await queryClient.invalidateQueries({ queryKey: ["account-sessions", userId] });
    },
  });
  const formatDate = (value: string) => new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(new Date(value));

  return <div className="account-page min-h-screen bg-surface-raised text-text-primary">
    <TopNav breadcrumbs={t("account.title")} onHome={() => navigate("/home")} />
    <main className="account-layout mx-auto w-full max-w-[1200px] px-4 pt-7 pb-40 sm:px-8 lg:pb-20">
      <h1 className="sr-only">{t("account.title")}</h1>
      <div className="account-logout"><Button busy={logout.isPending} onClick={() => logout.mutate(getAccountGeneration())}><LogOut size={15} aria-hidden="true" />{t("nav.logout")}</Button></div>
      {logout.isError ? <p role="alert" className="mb-5 text-sm text-state-error">{errorText(logout.error)}</p> : null}
      <div className="account-columns grid items-start gap-6 lg:grid-cols-[220px_minmax(0,1fr)] lg:gap-12">
        <aside className="account-sidebar min-w-0 lg:sticky lg:top-6">
          <div className="mb-5 flex items-center gap-3 border-b border-border-l1 pb-5">
            <span aria-hidden="true" className="flex h-12 w-12 shrink-0 items-center justify-center rounded-full border border-accent/10 bg-accent-soft text-xl font-semibold text-accent">{[...(session.data?.user?.display_name || "")][0]?.toLocaleUpperCase(locale) || <UserRound size={22} />}</span>
            <div className="min-w-0"><p className="truncate text-sm font-semibold" title={session.data?.user?.display_name}>{session.data?.user?.display_name}</p><p className="mt-1 truncate text-xs text-text-muted" title={session.data?.user?.email}>{session.data?.user?.email}</p></div>
          </div>
          <nav aria-label={t("account.title")} className="flex gap-1 overflow-x-auto pb-1 lg:flex-col">
            {[
              {id:"profile", title:t("account.profile"), Icon:UserRound},
              {id:"preferences", title:accountPreferencesMessage(locale,"title"), Icon:Palette},
              {id:"security", title:t("account.security"), Icon:ShieldCheck},
            ].map(({id,title,Icon}) => <Link key={id} to={`#${id}`} aria-current={activeSection === id ? "page" : undefined} className="account-nav-link flex min-h-11 shrink-0 items-center gap-2.5 rounded-control px-3 text-sm text-text-secondary outline-none transition-colors motion-reduce:transition-none hover:bg-surface-subtle hover:text-text-primary focus-visible:ring-2 focus-visible:ring-focus-ring"><Icon size={16} className="shrink-0" aria-hidden="true" />{title}</Link>)}
          </nav>
        </aside>
        <div className="account-content min-w-0">
      <AccountSection hidden={activeSection !== "profile"} id="profile" title={t("account.profile")} icon={<UserRound size={18} />}>

        {profile.isPending ? <AccountLoading label={t("app.loading")} /> : profile.isError ? <div><p role="alert" className="mb-3 text-sm text-state-error">{errorText(profile.error)}</p><Button onClick={() => void profile.refetch()}>{t("account.retry")}</Button></div> : <ProfileForm key={userId} profile={profile.data} onSaved={(next) => {
          queryClient.setQueryData<AccountProfile>(profileKey, (current) => current ? { ...current, user: next.user } : current);
          queryClient.setQueryData<SessionState>(["session"], (current) => current?.authenticated ? { ...current, user: next.user } : current);
        }} />}
      </AccountSection>
      <AccountSection hidden={activeSection !== "preferences"} id="preferences" title={accountPreferencesMessage(locale, "title")} icon={<Palette size={18} />}>

        <PreferenceControls />
      </AccountSection>
      {profile.data?.merchant ? <AccountSection hidden={activeSection !== "profile"} compact id="merchant" title={accountPreferencesMessage(locale, "merchantTitle")} icon={<Store size={18} />}>

        <MerchantForm key={`${userId}:${profile.data.merchant.id}`} merchant={profile.data.merchant} onSaved={(merchant) => {
          queryClient.setQueryData<AccountProfile>(profileKey, (current) => current ? { ...current, merchant } : current);
          queryClient.setQueryData<SessionState>(["session"], (current) => current?.authenticated ? { ...current, merchant } : current);
        }} />
      </AccountSection> : null}
      <AccountSection hidden={activeSection !== "security"} id="password" title={t("account.security")} icon={<KeyRound size={18} />}>

        <details className="account-password" key={userId}>
          <summary className="flex min-h-12 cursor-pointer items-center gap-3 py-3 text-sm font-medium"><KeyRound size={17} className="text-text-muted" aria-hidden="true" />{t("login.password")}<ChevronDown size={16} className="account-password-chevron ml-auto text-text-muted" aria-hidden="true" /></summary>
          <PasswordForm onSuccess={finishSession} />
        </details>
      </AccountSection>
      <AccountSection hidden={activeSection !== "security"} compact id="sessions" title={t("account.sessions")} icon={<ShieldCheck size={18} />}>

        <div className="min-w-0">
          {sessionNotice ? <p role="status" className="mb-3 text-sm text-state-success">{sessionNotice}</p> : null}
          {sessions.isPending ? <AccountLoading label={t("app.loading")} /> : sessions.isError ? <div><p role="alert" className="mb-3 text-sm text-state-error">{errorText(sessions.error)}</p><Button onClick={() => void sessions.refetch()}>{t("account.retry")}</Button></div> : <>
            {sessions.data.items.length === 0 ? <p className="py-6 text-sm text-text-muted">{t("account.emptySessions")}</p> : <ul className="divide-y divide-border-l1">
              {sessions.data.items.map((item) => <li key={item.id} className="flex flex-wrap items-center justify-between gap-4 py-5 first:pt-0">
                <div className="flex min-w-0 flex-1 gap-3"><span className={`mt-1 flex h-10 w-10 shrink-0 items-center justify-center rounded-panel ${item.current ? "bg-accent-soft text-accent" : "border border-border-l1 bg-surface-base text-text-muted"}`}><ShieldCheck size={18} aria-hidden="true" /></span><div className="min-w-0"><p className="mb-2 flex items-center gap-2 text-sm font-semibold">{t(item.current ? "account.currentSession" : "account.otherSession")}{item.current ? <span className="rounded-full bg-state-success-soft p-1 text-state-success"><Check size={12} aria-hidden="true" /></span> : null}</p><dl className="grid gap-1.5 text-xs leading-5 text-text-muted"><div><dt className="mr-2 inline">{t("account.created")}</dt><dd className="inline"><time dateTime={item.created_at}>{formatDate(item.created_at)}</time></dd></div><div><dt className="mr-2 inline">{t("account.expires")}</dt><dd className="inline"><time dateTime={item.expires_at}>{formatDate(item.expires_at)}</time></dd></div></dl></div></div>
                <Button onClick={(event) => { revokeButton.current = event.currentTarget; revoke.reset(); setRevokeTarget(item); }} disabled={revoke.isPending}>{t(item.current ? "account.revokeCurrent" : "account.revoke")}</Button>
              </li>)}
            </ul>}
            <div className="mt-5 flex flex-wrap items-center justify-between gap-3 border-t border-border-l1 pt-4"><span className="text-xs text-text-muted">{t("account.page", { page: cursors.length + 1 })}</span><div className="flex gap-2"><Button disabled={!cursors.length} onClick={() => setCursors((values) => values.slice(0, -1))}>{t("account.previous")}</Button><Button disabled={!sessions.data.next_cursor} onClick={() => { if (sessions.data.next_cursor) setCursors((values) => [...values, sessions.data.next_cursor!]); }}>{t("account.next")}</Button></div></div>
          </>}
        </div>
      </AccountSection>
        </div>
      </div>
    </main>
    <Dialog open={Boolean(revokeTarget)} onOpenChange={(open) => { if (!open && !revoke.isPending) setRevokeTarget(null); }}>
      <DialogContent onCloseAutoFocus={(event) => { event.preventDefault(); revokeButton.current?.focus(); }} title={t("account.revokeConfirm")} description={t(revokeTarget?.current ? "account.revokeCurrentConfirm" : "account.sessionsNote")} closeLabel={t("account.cancel")} onClose={() => { if (!revoke.isPending) setRevokeTarget(null); }} footer={<><Button disabled={revoke.isPending} onClick={() => setRevokeTarget(null)}>{t("account.cancel")}</Button><Button variant="danger" busy={revoke.isPending} onClick={() => { if (revokeTarget) revoke.mutate({ target: revokeTarget, generation: getAccountGeneration() }); }}>{t(revokeTarget?.current ? "account.revokeCurrent" : "account.revoke")}</Button></>}>
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
  const currentAccount = useCurrentAccountWrite();
  const save = useMutation<AccountProfile, Error, number>({ mutationFn: () => api.updateAccount({ display_name: name.trim() }), onSuccess: (next, generation) => { if (!currentAccount(generation)) return; onSaved(next); setDraft(null); }, onError: (error, generation) => { if (currentAccount(generation)) setError(error instanceof ApiError ? error.detail : t("account.error")); } });
  return <form className="account-form space-y-5" onSubmit={(event) => { event.preventDefault(); if (save.isPending) return; const invalid = validateDisplayName(name); setError(invalid ? t(invalid) : ""); if (!invalid) save.mutate(getAccountGeneration()); }}>
    <div className="account-readonly"><span>{t("login.email")}</span><span className="break-all text-sm text-text-primary" aria-label={t("login.email")}>{profile.user.email}</span></div>
    <Input className="!h-11 text-sm" label={t("account.displayName")} value={name} onChange={(event) => { setDraft(event.target.value); setError(""); save.reset(); }} autoComplete="nickname" disabled={save.isPending} aria-invalid={Boolean(error)} />
    {profile.user.is_operator ? <p className="text-xs font-medium text-text-muted">{t("account.operator")}</p> : null}
    {error ? <p role="alert" className="text-sm text-state-error">{error}</p> : null}
    <div className="flex flex-wrap items-center justify-end gap-3 pt-3"><Button type="submit" variant="primary" busy={save.isPending} disabled={name.trim() === profile.user.display_name}>{t("account.save")}</Button>{save.isSuccess ? <p role="status" className="flex items-center gap-1.5 text-sm text-state-success"><Check size={15} aria-hidden="true" />{t("account.saved")}</p> : null}</div>
  </form>;
}

function PasswordForm({ onSuccess }: { onSuccess: (generation: number) => void }) {
  const { t } = useI18n();
  const [current, setCurrent] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [error, setError] = useState("");
  const currentAccount = useCurrentAccountWrite();
  const mutation = useMutation<{ ok: boolean }, Error, number>({ mutationFn: () => api.changePassword({ current_password: current, new_password: password }), onSuccess: (_, generation) => onSuccess(generation), onError: (error, generation) => { if (currentAccount(generation)) setError(error instanceof ApiError ? error.detail : t("account.error")); } });
  return <form className="account-form space-y-5" onSubmit={(event) => { event.preventDefault(); if (mutation.isPending) return; const invalid = validateNewPassword(password, confirmation); setError(invalid ? t(invalid) : ""); if (!invalid) mutation.mutate(getAccountGeneration()); }}>
    <Input className="!h-11 text-sm" label={t("account.currentPassword")} type="password" autoComplete="current-password" value={current} onChange={(event) => setCurrent(event.target.value)} required disabled={mutation.isPending} />
    <Input className="!h-11 text-sm" label={t("account.newPassword")} type="password" autoComplete="new-password" value={password} onChange={(event) => setPassword(event.target.value)} required disabled={mutation.isPending} aria-describedby="account-password-rule" />
    <p id="account-password-rule" className="text-xs text-text-muted">{t("account.passwordRule")}</p>
    <Input className="!h-11 text-sm" label={t("account.confirmPassword")} type="password" autoComplete="new-password" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} required disabled={mutation.isPending} />
    {error ? <p role="alert" className="text-sm text-state-error">{error}</p> : null}
    <div className="flex flex-wrap items-center justify-between gap-4 pt-3"><p className="max-w-sm text-xs leading-5 text-text-muted">{t("account.passwordNote")}</p><Button type="submit" variant="primary" busy={mutation.isPending}>{t("account.changePassword")}</Button></div>
  </form>;
}

function useCurrentAccountWrite() {
  const queryClient = useQueryClient();
  const userId = queryClient.getQueryData<SessionState>(["session"])?.user?.id;
  return (generation: number) => isCurrentAccountGeneration(generation) && userId === queryClient.getQueryData<SessionState>(["session"])?.user?.id;
}

function PreferenceControls() {
  const { t, locale, setLocale, themePreference, setThemePreference, saving, saved } = usePreferences();
  return <div className="account-preferences space-y-5">
    <div><label htmlFor="account-locale" className="mb-2 block text-xs font-semibold text-text-muted">{t("nav.language")}</label><Select id="account-locale" ariaLabel={t("nav.language")} value={locale} disabled={saving} options={LOCALES.map((value) => ({ value, label: t(LOCALE_LABEL_KEYS[value]) }))} onChange={(value) => { if (isLocale(value)) setLocale(value); }} /></div>
    <fieldset disabled={saving}>
      <legend className="mb-3 text-xs font-semibold text-text-muted">{t("nav.theme")}</legend>
      <div className="grid max-w-[420px] grid-cols-3 gap-2 sm:gap-3">
        {THEME_PREFERENCES.map(value => {
          const Icon = value === "light" ? Sun : value === "dark" ? Moon : Monitor;
          return <label key={value} className="group relative min-w-0 cursor-pointer">
            <input type="radio" name="account-theme" value={value} checked={themePreference === value} onChange={() => setThemePreference(value)} className="peer absolute inset-0 z-10 h-full w-full cursor-pointer opacity-0 disabled:cursor-not-allowed" />
            <span className="block overflow-hidden rounded-panel border-2 border-border-l1 p-2 transition-colors motion-reduce:transition-none peer-checked:border-accent peer-focus-visible:ring-2 peer-focus-visible:ring-focus-ring peer-focus-visible:ring-offset-2 peer-disabled:cursor-not-allowed peer-disabled:opacity-50">
              <svg viewBox="0 0 180 110" className="mb-2 w-full rounded-control" aria-hidden="true">
                <rect width="180" height="110" fill={value === "dark" ? "#0f172a" : "#f8fafc"} />
                {value === "system" ? <path d="M90 0H180V110H90Z" fill="#0f172a" /> : null}
                <rect x="8" y="8" width="35" height="94" rx="3" fill={value === "dark" ? "#1e293b" : "#e2e8f0"} />
                <rect x="51" y="12" width="78" height="5" rx="2" fill={value === "dark" ? "#94a3b8" : "#64748b"} />
                <rect x="51" y="26" width="118" height="34" rx="4" fill={value === "dark" ? "#1e293b" : "#e2e8f0"} />
                <rect x="51" y="69" width="55" height="31" rx="4" fill={value === "dark" ? "#1e293b" : "#e2e8f0"} />
                <rect x="114" y="69" width="55" height="31" rx="4" fill={value === "dark" ? "#1e293b" : "#e2e8f0"} />
              </svg>
              <span className="flex min-h-8 flex-wrap items-center justify-center gap-1.5 text-center text-xs font-medium"><Icon size={14} aria-hidden="true" />{t(`theme.${value}`)}</span>
            </span>
          </label>;
        })}
      </div>
    </fieldset>
    <p className="text-xs text-text-muted">{accountPreferencesMessage(locale, "automatic")}</p>
    {saving || saved ? <p role="status" className="text-sm text-state-success">{preferenceSaveMessage(locale, saving ? "saving" : "saved")}</p> : null}
  </div>;
}

function MerchantForm({ merchant, onSaved }: { merchant: SessionMerchant; onSaved: (merchant: SessionMerchant) => void }) {
  const { t, locale } = useI18n();
  const [draft, setDraft] = useState<string | null>(null);
  const [error, setError] = useState("");
  const currentAccount = useCurrentAccountWrite();
  const name = draft ?? merchant.name;
  const suspended = merchant.status === "suspended";
  const save = useMutation<SessionMerchant, Error, number>({ mutationFn: () => api.updateAccountMerchant({ name: name.trim() }), onSuccess: (next, generation) => { if (!currentAccount(generation)) return; onSaved(next); setDraft(null); }, onError: (error, generation) => { if (currentAccount(generation)) setError(error instanceof ApiError ? error.detail : t("account.error")); } });
  return <form className="account-form space-y-5" onSubmit={(event) => {
    event.preventDefault();
    if (save.isPending || suspended) return;
    const length = [...name.trim()].length;
    setError(length < 1 || length > 160 ? accountPreferencesMessage(locale, "merchantInvalid") : "");
    if (length >= 1 && length <= 160) save.mutate(getAccountGeneration());
  }}>
    <Input className="!h-11 text-sm" label={t("account.merchant")} value={name} onChange={(event) => { setDraft(event.target.value); setError(""); save.reset(); }} autoComplete="organization" disabled={suspended || save.isPending} aria-invalid={Boolean(error)} />
    {suspended ? <p className="text-sm text-text-muted">{accountPreferencesMessage(locale, "merchantSuspended")}</p> : null}
    {error ? <p role="alert" className="text-sm text-state-error">{error}</p> : null}
    <div className="flex flex-wrap items-center justify-end gap-3 pt-3"><Button type="submit" variant="primary" busy={save.isPending} disabled={suspended || name.trim() === merchant.name}>{t("account.save")}</Button>{save.isSuccess ? <p role="status" className="flex items-center gap-1.5 text-sm text-state-success"><Check size={15} aria-hidden="true" />{t("account.saved")}</p> : null}</div>
  </form>;
}


function AccountSection({id, title, icon, children, hidden = false, compact = false}: {id: string; title: string; icon: ReactNode; children: ReactNode; hidden?: boolean; compact?: boolean}) {
  return <section id={id} hidden={hidden} aria-labelledby={`${id}-heading`} className={compact ? "account-section account-section-compact" : "account-section"}>
    <header className="flex items-center gap-2.5"><span className="text-text-muted" aria-hidden="true">{icon}</span><h2 id={`${id}-heading`} className={compact ? "text-sm font-semibold" : "text-xl font-semibold tracking-tight"}>{title}</h2></header>
    <div className="account-section-body">{children}</div>
  </section>;
}


function AccountLoading({label}: {label: string}) {
  return <div role="status" aria-label={label} className="space-y-4">
    <span className="sr-only">{label}</span>
    <div aria-hidden="true" className="space-y-4 motion-safe:animate-pulse"><div className="h-3 w-24 rounded bg-surface-subtle" /><div className="h-11 rounded-control bg-surface-subtle" /><div className="h-3 w-32 rounded bg-surface-subtle" /><div className="h-11 rounded-control bg-surface-subtle" /></div>
  </div>;
}
