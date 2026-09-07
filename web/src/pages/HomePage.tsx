import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft, ArrowRight, ArrowUpRight, Images,
  LayoutGrid, MessagesSquare, Plus, ScanLine,
} from "lucide-react";
import { useRef, useState } from "react";
import { Link, useNavigate } from "react-router-dom";

import { TopNav } from "../components/TopNav";
import { buttonVariants } from "../components/ui/button";
import { Dialog, DialogContent } from "../components/ui/dialog";
import { api } from "../lib/api";
import type { TranslationKey } from "../lib/i18n";
import { useI18n } from "../lib/preferences";
import { HomeStudioDemo, homeReference as reference, homeShots as shots } from "./home/HomeStudioDemo";
import { HomeProductStory, HomeQuestions } from "./home/HomeProductStory";
import { HomeTourProvider } from "./home/HomeTourContext";
import { HomeGuideEntry } from "./home/HomeGuideEntry";
import { useHomeMotion } from "./home/useHomeMotion";
import "./HomePage.css";

const workspaces: { to: string; icon: typeof LayoutGrid; title: TranslationKey; note: TranslationKey }[] = [
  { to: "/products", icon: LayoutGrid, title: "home.work.products", note: "home.work.productsNote" },
  { to: "/image-chat", icon: MessagesSquare, title: "home.work.images", note: "home.work.imagesNote" },
  { to: "/media-library", icon: Images, title: "home.work.library", note: "home.work.libraryNote" },
];

export function HomePage() {
  const { t } = useI18n();
  const root = useRef<HTMLDivElement>(null);
  useHomeMotion(root);
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [selectedShot, setSelectedShot] = useState<number | null>(null);
  const [showReference, setShowReference] = useState(false);
  const previewTrigger = useRef<HTMLButtonElement | null>(null);
  const shot = shots[selectedShot ?? 0];
  const logoutMutation = useMutation({
    mutationFn: api.destroySession,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["session"] });
      navigate("/login", { replace: true });
    },
  });

  function changeShot(direction: number) {
    setSelectedShot((current) => ((current ?? 0) + direction + shots.length) % shots.length);
    setShowReference(false);
  }

  return (
    <HomeTourProvider><div className="home-studio" ref={root}>
      <TopNav onHome={() => navigate("/home")} onLogout={() => logoutMutation.mutate()} />
      <main>
        <section className="home-intro home-width" aria-labelledby="home-title">
          <div className="home-intro-copy">
            <p className="home-eyebrow">ProductFlow / {t("home.studio")}</p>
            <h1 id="home-title">{t("home.headline")}</h1>
            <p className="home-intro-line">{t("home.subtitle")}</p>
          </div>
          <div className="home-intro-actions">
            <Link className={`home-create ${buttonVariants({ variant: "primary", size: "lg" })}`} to="/products/new">
              <Plus size={18} aria-hidden="true" />{t("home.create")}
            </Link>
            <Link className={`home-continue ${buttonVariants({ variant: "secondary", size: "lg" })}`} to="/products">{t("home.continue")}<ArrowRight size={16} aria-hidden="true" /></Link>
          </div>
          <HomeGuideEntry />
        </section>

        <HomeStudioDemo onPreview={(index, isReference, trigger) => {
          previewTrigger.current = trigger;
          setSelectedShot(index);
          setShowReference(isReference);
        }} />

        <HomeProductStory />

        <section id="home-workspaces" className="home-workspaces" aria-labelledby="home-workspaces-title">
          <div className="home-width">
            <div className="home-section-heading">
              <h2 id="home-workspaces-title">{t("home.workspaceTitle")}</h2>
              <Link className="home-text-link" to="/help">{t("home.help")}<ArrowUpRight size={14} aria-hidden="true" /></Link>
            </div>
            <div className="home-workspace-grid">
              {workspaces.map(({ to, icon: Icon, title, note }) => (
                <Link className="home-workspace-link" to={to} key={to}>
                  <div className="home-workspace-top"><Icon size={23} strokeWidth={1.5} aria-hidden="true" /></div>
                  <div className="home-workspace-bottom"><div><h3>{t(title)}</h3><p>{t(note)}</p></div><ArrowUpRight size={21} aria-hidden="true" /></div>
                </Link>
              ))}
            </div>
          </div>
        </section>

        <HomeQuestions />

        <section className="home-start home-width" aria-labelledby="home-start-title">
          <p className="home-eyebrow">{t("home.next")}</p>
          <h2 id="home-start-title">{t("home.nextTitle")}</h2>
          <Link className={`home-create ${buttonVariants({ variant: "primary", size: "lg" })}`} to="/products/new">
            {t("home.create")}<ArrowRight size={16} aria-hidden="true" />
          </Link>
        </section>
      </main>
      <footer className="home-footer home-width">
        <span>ProductFlow</span>
        <Link className="home-text-link" to="/help">{t("home.help")}<ArrowUpRight size={14} aria-hidden="true" /></Link>
      </footer>

      <Dialog open={selectedShot !== null} onOpenChange={(open) => { if (!open) setSelectedShot(null); }}>
        <DialogContent
          title={`${t("home.collection")} / ${t(shot.title)}`}
          description={t("home.previewNote")}
          closeLabel={t("home.close")}
          className="home-preview"
          bodyClassName="home-preview-body"
          onKeyDown={(event) => {
            if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
              event.preventDefault();
              changeShot(event.key === "ArrowLeft" ? -1 : 1);
            }
          }}
          onCloseAutoFocus={(event) => { event.preventDefault(); previewTrigger.current?.focus(); }}
        >
          <div className="home-preview-toolbar">
            <div className="home-view-switch" role="group" aria-label={t("home.compare")}>
              <button type="button" aria-pressed={!showReference} onClick={() => setShowReference(false)}>{t("home.result")}</button>
              <button type="button" aria-pressed={showReference} onClick={() => setShowReference(true)}><ScanLine size={15} aria-hidden="true" />{t("home.reference")}</button>
            </div>
            <span className="home-index" aria-live="polite">0{(selectedShot ?? 0) + 1} / 03</span>
          </div>
          <div className="home-preview-stage">
            <img
              key={showReference ? "reference" : shot.id}
              src={showReference ? reference.src : `/home-showcase/forma-${shot.id}.webp`}
              alt={showReference ? t("home.reference") : t(shot.note)}
              width={showReference ? reference.width : shot.width}
              height={showReference ? reference.height : shot.height}
            />
          </div>
          <div className="home-preview-bottom">
            <button type="button" className="home-icon-button" aria-label={t("home.previous")} title={t("home.previous")} onClick={() => changeShot(-1)}><ArrowLeft size={19} /></button>
            <p aria-live="polite">{showReference ? t("home.reference") : t(shot.note)}</p>
            <button type="button" className="home-icon-button" aria-label={t("home.nextShot")} title={t("home.nextShot")} onClick={() => changeShot(1)}><ArrowRight size={19} /></button>
          </div>
        </DialogContent>
      </Dialog>
    </div></HomeTourProvider>
  );
}
