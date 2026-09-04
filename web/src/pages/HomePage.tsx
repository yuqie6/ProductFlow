import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  ArrowDown, ArrowLeft, ArrowRight, ArrowUpRight, Check, Images,
  LayoutGrid, Maximize2, MessagesSquare, Plus, ScanLine,
} from "lucide-react";
import { useRef, useState } from "react";
import { Link, useNavigate } from "react-router-dom";

import { TopNav } from "../components/TopNav";
import { Dialog, DialogContent } from "../components/ui/dialog";
import { api } from "../lib/api";
import type { TranslationKey } from "../lib/i18n";
import { useI18n } from "../lib/preferences";
import "./HomePage.css";

const shots = [
  { id: "hero", title: "home.shot.hero", note: "home.shot.heroNote", width: 1254, height: 1254 },
  { id: "scene", title: "home.shot.scene", note: "home.shot.sceneNote", width: 1134, height: 1387 },
  { id: "detail", title: "home.shot.detail", note: "home.shot.detailNote", width: 1122, height: 1402 },
] as const;

const workspaces: { to: string; icon: typeof LayoutGrid; title: TranslationKey; note: TranslationKey }[] = [
  { to: "/products", icon: LayoutGrid, title: "home.work.products", note: "home.work.productsNote" },
  { to: "/image-chat", icon: MessagesSquare, title: "home.work.images", note: "home.work.imagesNote" },
  { to: "/media-library", icon: Images, title: "home.work.library", note: "home.work.libraryNote" },
];

export function HomePage() {
  const { t } = useI18n();
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
    <div className="home-studio">
      <TopNav onHome={() => navigate("/home")} onLogout={() => logoutMutation.mutate()} />
      <main>
        <section className="home-intro home-width" aria-labelledby="home-title">
          <div className="home-intro-copy">
            <p className="home-eyebrow"><span className="home-status-mark" />{t("home.studio")}</p>
            <h1 id="home-title">ProductFlow<span className="home-brand-period">.</span></h1>
            <p className="home-intro-line">{t("home.intro")}</p>
          </div>
          <div className="home-intro-actions">
            <Link className="home-create" to="/products/new">
              <Plus size={18} aria-hidden="true" />{t("home.create")}<ArrowUpRight size={18} aria-hidden="true" />
            </Link>
            <Link className="home-text-link" to="/products">{t("home.continue")}<ArrowRight size={16} aria-hidden="true" /></Link>
          </div>
        </section>

        <section className="home-showcase home-width" aria-labelledby="home-showcase-title">
          <div className="home-collection-heading">
            <div className="home-collection-name">
              <span className="home-index">01 /</span>
              <h2 id="home-showcase-title">{t("home.collection")}</h2>
              <span className="home-collection-category">{t("home.category")}</span>
            </div>
            <span className="home-proof"><Check size={13} aria-hidden="true" />{t("home.proof")}</span>
          </div>
          <div className="home-photo-grid">
            {shots.map((item, index) => (
              <figure key={item.id} className={`home-photo home-photo-${item.id}`}>
                <button
                  className="home-photo-button"
                  type="button"
                  aria-label={t("home.preview", { name: t(item.title) })}
                  onClick={(event) => {
                    previewTrigger.current = event.currentTarget;
                    setSelectedShot(index);
                    setShowReference(false);
                  }}
                >
                  <img
                    src={`/home-showcase/forma-${item.id}.webp`}
                    alt={t(item.note)}
                    width={item.width}
                    height={item.height}
                    fetchPriority={index === 0 ? "high" : "auto"}
                    decoding="async"
                  />
                  <span className="home-photo-number" aria-hidden="true">0{index + 1}</span>
                  <span className="home-photo-expand" aria-hidden="true"><Maximize2 size={18} /></span>
                </button>
                <figcaption>
                  <div><h3>{t(item.title)}</h3><p>{t(item.note)}</p></div>
                  <span className="home-image-spec">{item.width} × {item.height}</span>
                </figcaption>
              </figure>
            ))}
          </div>
          <div className="home-collection-footer">
            <p>{t("home.collectionNote")}</p>
            <a className="home-text-link" href="#home-workspaces">{t("home.workspace")}<ArrowDown size={14} aria-hidden="true" /></a>
          </div>
        </section>

        <section id="home-workspaces" className="home-workspaces" aria-labelledby="home-workspaces-title">
          <div className="home-width">
            <div className="home-section-heading">
              <h2 id="home-workspaces-title">{t("home.workspace")}</h2>
              <Link className="home-text-link" to="/help">{t("home.help")}<ArrowUpRight size={14} aria-hidden="true" /></Link>
            </div>
            <div className="home-workspace-grid">
              {workspaces.map(({ to, icon: Icon, title, note }, index) => (
                <Link className="home-workspace-link" to={to} key={to}>
                  <div className="home-workspace-top"><Icon size={23} strokeWidth={1.5} aria-hidden="true" /><span className="home-index">0{index + 1}</span></div>
                  <div className="home-workspace-bottom"><div><h3>{t(title)}</h3><p>{t(note)}</p></div><ArrowUpRight size={21} aria-hidden="true" /></div>
                </Link>
              ))}
            </div>
          </div>
        </section>

        <section className="home-start home-width" aria-labelledby="home-start-title">
          <div className="home-start-heading"><span className="home-eyebrow">{t("home.next")}</span><h2 id="home-start-title">{t("home.nextTitle")}</h2></div>
          <Link className="home-create" to="/products/new">{t("home.create")}<ArrowUpRight size={18} aria-hidden="true" /></Link>
        </section>
      </main>
      <footer className="home-footer home-width"><span>ProductFlow</span><span>{t("home.studio")}</span><Link to="/settings">{t("home.settings")}<ArrowUpRight size={13} aria-hidden="true" /></Link></footer>

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
              src={`/home-showcase/forma-${showReference ? "reference" : shot.id}.webp`}
              alt={showReference ? t("home.reference") : t(shot.note)}
              width={showReference ? 806 : shot.width}
              height={showReference ? 1475 : shot.height}
            />
          </div>
          <div className="home-preview-bottom">
            <button type="button" className="home-icon-button" aria-label={t("home.previous")} title={t("home.previous")} onClick={() => changeShot(-1)}><ArrowLeft size={19} /></button>
            <p aria-live="polite">{showReference ? t("home.reference") : t(shot.note)}</p>
            <button type="button" className="home-icon-button" aria-label={t("home.nextShot")} title={t("home.nextShot")} onClick={() => changeShot(1)}><ArrowRight size={19} /></button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
