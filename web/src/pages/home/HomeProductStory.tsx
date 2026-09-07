import { ArrowRight, ArrowUpRight, ChevronDown, Folder, Images, Layers, ScanLine } from "lucide-react";
import { lazy, Suspense, useRef, useState } from "react";
import { Link } from "react-router-dom";

import { useHomeTour } from "./HomeTourContext";
import { useI18n } from "../../lib/preferences";
import { homeShots } from "./HomeStudioDemo";

const HomeCanvasScene = lazy(() => import("./HomeCanvasScene"));
const HomeEditorScene = lazy(() => import("./HomeEditorScene"));

export function HomeProductStory() {
  const { t } = useI18n();
  const { complete } = useHomeTour();
  const editorTrigger = useRef<HTMLButtonElement>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [editArea, setEditArea] = useState<"background" | "subject">("background");
  const [reuse, setReuse] = useState<"assets" | "recipes">("assets");

  return (
    <>
      <section id="home-canvas" className="home-story-section home-plan-section home-width" aria-labelledby="home-plan-title">
        <div className="home-story-copy">
          <span className="home-chapter" aria-hidden="true">01</span><span className="home-story-icon"><Layers size={22} aria-hidden="true" /></span>
          <h2 id="home-plan-title">{t("home.scene.chapter1")}</h2>
          <p>{t("home.story.planBody")}</p>
          <Link className="home-story-link" to="/products/new">{t("home.story.planAction")}<ArrowUpRight size={16} aria-hidden="true" /></Link>
        </div>
        <div className="home-workflow-illustration"><Suspense fallback={<p>{t("home.story.planTitle")}</p>}><HomeCanvasScene /></Suspense></div>
      </section>

      <section id="home-edit" className="home-story-band" aria-labelledby="home-edit-title">
        <div className="home-story-section home-story-reverse home-width">
          <div className="home-edit-illustration">
            <div className="home-edit-image" data-area={editArea}>
              <img src="/home-showcase/forma-scene.webp" alt={t("home.shot.sceneNote")} width={1134} height={1387} loading="lazy" />
              <div className={`home-edit-selection home-edit-selection-${editArea}`} aria-hidden="true"><span /><span /><span /><span /><ScanLine size={20} /></div>
              <span className="home-edit-badge">{t("home.demo.sample")}</span>
            </div>
            <div className="home-edit-request" key={editArea}><ScanLine size={16} aria-hidden="true" /><p>{t(`home.story.${editArea}Request`)}</p></div>
          </div>
          <div className="home-story-copy">
            <span className="home-chapter" aria-hidden="true">02</span><span className="home-story-icon"><ScanLine size={22} aria-hidden="true" /></span>
            <h2 id="home-edit-title">{t("home.scene.chapter2")}</h2>
            <p>{t("home.story.editBody")}</p>
            <div className="home-story-options home-edit-options" role="group" aria-label={t("home.story.editHint")}>
              {(["background", "subject"] as const).map((area) => <button key={area} type="button" aria-pressed={editArea === area} onClick={() => setEditArea(area)}>{t(`home.story.${area}`)}</button>)}
            </div>
            <button type="button" className="home-editor-launch" ref={editorTrigger} onClick={() => setEditorOpen(true)}>{t("home.scene.tryEdit")}<ArrowUpRight size={18} /></button><small>{t("home.story.editLimit")}</small>
          </div>
        </div>
      </section>

      <section id="home-reuse" className="home-story-section home-reuse-section home-width" aria-labelledby="home-reuse-title">
        <div className="home-story-copy">
          <span className="home-chapter" aria-hidden="true">03</span><span className="home-story-icon"><Folder size={22} aria-hidden="true" /></span>
          <h2 id="home-reuse-title">{t("home.scene.chapter3")}</h2>
          <p>{t("home.story.reuseBody")}</p>
          <Link className="home-story-link" to={reuse === "assets" ? "/media-library" : "/products/new"}>{t(reuse === "assets" ? "home.work.library" : "home.create")}<ArrowUpRight size={16} aria-hidden="true" /></Link>
        </div>
        <div className="home-reuse-illustration">
          <div className="home-reuse-tabs" role="group" aria-label={t("home.story.reuseTitle")}>
            {(["assets", "recipes"] as const).map((tab) => <button type="button" key={tab} aria-pressed={reuse === tab} onClick={() => { setReuse(tab); if (tab === "recipes") complete(3); }}>{tab === "assets" ? <Images size={15} /> : <Layers size={15} />}{t(`home.story.${tab}`)}</button>)}
          </div>
          <div className="home-reuse-content" key={reuse}>
            {reuse === "assets" ? (
              <>
                <div className="home-library-heading"><Folder size={18} aria-hidden="true" /><span>{t("home.collection")}</span></div>
                <div className="home-library-tiles">
                  {homeShots.map((shot) => <figure key={shot.id}><img src={`/home-showcase/forma-${shot.id}.webp`} alt={t(shot.title)} width={shot.width} height={shot.height} loading="lazy" /><figcaption>{t(shot.title)}</figcaption></figure>)}
                </div>
                <p className="home-illustration-label">{t("home.demo.sample")}</p>
              </>
            ) : (
              <>
                <div className="home-recipe-sheet">
                  <Layers size={26} aria-hidden="true" /><h3>{t("home.story.recipes")}</h3>
                  <div className="home-recipe-path"><span>{t("home.story.style")}</span><ArrowRight size={16} /><span>{t("home.story.prompt")}</span><ArrowRight size={16} /><span>{t("home.story.image")}</span></div>
                </div>
                <p className="home-recipe-note">{t("home.story.recipeNote")}</p>
              </>
            )}
          </div>
        </div>
      </section>
      {editorOpen && <Suspense fallback={null}><HomeEditorScene onClose={() => { setEditorOpen(false); requestAnimationFrame(() => editorTrigger.current?.focus({ preventScroll: true })); }} /></Suspense>}
    </>
  );
}

export function HomeQuestions() {
  const { t } = useI18n();
  return (
    <section className="home-questions home-width" aria-labelledby="home-questions-title">
      <h2 id="home-questions-title">{t("home.story.faqTitle")}</h2>
      <div>{(["Start", "Chat", "Reuse"] as const).map((topic) => (
        <details key={topic}>
          <summary>{t(`home.story.faq${topic}`)}<ChevronDown size={18} aria-hidden="true" /></summary>
          <p>{t(`home.story.faq${topic}Answer`)}</p>
        </details>
      ))}</div>
    </section>
  );
}
