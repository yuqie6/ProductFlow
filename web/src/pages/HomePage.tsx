import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  ArrowRight,
  Bot,
  Check,
  ChevronRight,
  Eye,
  FileCode,
  FolderKanban,
  GitBranch,
  Image as ImageIcon,
  Images,
  Layers,
  LayoutGrid,
  MessageSquare,
  Network,
  RotateCw,
  Settings2,
  ShieldCheck,
  Sliders,
  Sparkles,
  Wand2,
  X,
  Zap,
} from "lucide-react";
import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";

import { TopNav } from "../components/TopNav";
import { api } from "../lib/api";
import type { TranslationKey } from "../lib/i18n";
import { useI18n } from "../lib/preferences";
import "./HomePage.css";

interface ShowcaseShot {
  id: string;
  type: string;
  titleKey: TranslationKey;
  image: string;
  aspectRatio: string;
  resolution: string;
  promptSnippet: string;
  deliveryFormat: string;
}

const showcaseShots: ShowcaseShot[] = [
  {
    id: "hero",
    type: "hero",
    titleKey: "home.showcase.hero",
    image: "/home-showcase/forma-hero.jpg",
    aspectRatio: "1:1",
    resolution: "1254 x 1254",
    promptSnippet: "晨光柔和漫射 · 天然洞石台面 · 简约轻奢商用摄影",
    deliveryFormat: "PNG · 1254x1254",
  },
  {
    id: "scene",
    type: "scene",
    titleKey: "home.showcase.scene",
    image: "/home-showcase/forma-scene.jpg",
    aspectRatio: "4:5",
    resolution: "1134 x 1387",
    promptSnippet: "现代北欧卫浴场景 · 浅橡木台面 · 柔和环境光与植物光影",
    deliveryFormat: "PNG · 1134x1387",
  },
  {
    id: "detail",
    type: "detail",
    titleKey: "home.showcase.detail",
    image: "/home-showcase/forma-detail.jpg",
    aspectRatio: "4:5",
    resolution: "1122 x 1402",
    promptSnippet: "喷头与琥珀色玻璃瓶口微距特写 · 通透液体折射 · 细腻玻璃质感",
    deliveryFormat: "PNG · 1122x1402",
  },
];

interface GraphStep {
  id: string;
  nameKey: TranslationKey;
  descKey: TranslationKey;
  icon: typeof GitBranch;
  role: string;
  meta: string;
}

const graphSteps: GraphStep[] = [
  {
    id: "step-1",
    nameKey: "home.graphGuide.step1.name",
    descKey: "home.graphGuide.step1.desc",
    icon: FolderKanban,
    role: "输入商品与实拍照片",
    meta: "绑定 1 张参考实拍",
  },
  {
    id: "step-2",
    nameKey: "home.graphGuide.step2.name",
    descKey: "home.graphGuide.step2.desc",
    icon: FileCode,
    role: "规划首图、场景与特写",
    meta: "3 组关键拍摄机位",
  },
  {
    id: "step-3",
    nameKey: "home.graphGuide.step3.name",
    descKey: "home.graphGuide.step3.desc",
    icon: Sliders,
    role: "设定影棚光影与环境氛围",
    meta: "柔和漫射光 · 浅色基调",
  },
  {
    id: "step-4",
    nameKey: "home.graphGuide.step4.name",
    descKey: "home.graphGuide.step4.desc",
    icon: Wand2,
    role: "生成专业摄影级描述",
    meta: "结构化构图与材质指令",
  },
  {
    id: "step-5",
    nameKey: "home.graphGuide.step5.name",
    descKey: "home.graphGuide.step5.desc",
    icon: Sparkles,
    role: "多镜头同步出图",
    meta: "高清商业成片输出",
  },
  {
    id: "step-6",
    nameKey: "home.graphGuide.step6.name",
    descKey: "home.graphGuide.step6.desc",
    icon: Layers,
    role: "自动适配电商平台尺寸",
    meta: "1:1 / 4:5 / 16:9 标准尺寸",
  },
];

export function HomePage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const [activeShotIndex, setActiveShotIndex] = useState(0);
  const [isCompareOriginal, setIsCompareOriginal] = useState(false);
  const [activeGraphStep, setActiveGraphStep] = useState(0);
  const [isDetailModalOpen, setIsDetailModalOpen] = useState(false);

  const logoutMutation = useMutation({
    mutationFn: api.destroySession,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["session"] });
      navigate("/login", { replace: true });
    },
  });

  useEffect(() => {
    const revealElements = document.querySelectorAll<HTMLElement>(".home-reveal");
    const observer = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (entry.isIntersecting) {
            entry.target.classList.add("is-visible");
            observer.unobserve(entry.target);
          }
        });
      },
      { rootMargin: "0px 0px -6%", threshold: 0.1 },
    );
    revealElements.forEach((el) => observer.observe(el));
    return () => observer.disconnect();
  }, []);

  const currentShot = showcaseShots[activeShotIndex];
  const currentStep = graphSteps[activeGraphStep];

  return (
    <div className="home-container">
      <TopNav onHome={() => navigate("/home")} onLogout={() => logoutMutation.mutate()} />

      <main className="home-main">
        {/* ── 1. Hero Section ── */}
        <section className="home-hero-section">
          <div className="home-hero-backdrop" aria-hidden="true">
            <div className="home-hero-glow-1" />
            <div className="home-hero-glow-2" />
            <div className="home-hero-grid-pattern" />
          </div>

          <div className="home-hero-content">
            {/* Top Kicker & Honest Badges */}
            <div className="home-hero-badges">
              <span className="home-badge-live">
                <span className="home-badge-dot" />
                {t("home.hero.kicker")}
              </span>
              <span className="home-badge-pill">
                <Sparkles size={13} className="text-indigo-400" />
                {t("home.hero.badge.agent")}
              </span>
              <span className="home-badge-pill">
                <LayoutGrid size={13} className="text-violet-400" />
                {t("home.hero.badge.graph")}
              </span>
              <span className="home-badge-pill">
                <Layers size={13} className="text-emerald-400" />
                {t("home.hero.badge.delivery")}
              </span>
            </div>

            {/* Main Headline */}
            <h1 className="home-hero-title">{t("home.hero.titleNew")}</h1>
            <p className="home-hero-subtitle">{t("home.hero.descriptionNew")}</p>

            {/* Primary Action Buttons */}
            <div className="home-hero-cta-group">
              <Link to="/products/new" className="home-btn-primary">
                <Sparkles size={18} className="shrink-0" />
                <span>{t("home.hero.start")}</span>
                <ChevronRight size={16} className="shrink-0 opacity-80" />
              </Link>
              <Link to="/products" className="home-btn-secondary">
                <LayoutGrid size={16} className="shrink-0 text-slate-400" />
                <span>{t("home.hero.open")}</span>
              </Link>
            </div>

            {/* Hero Interactive Showcase Stage */}
            <div className="home-hero-stage-card">
              {/* Header Bar */}
              <div className="home-stage-header">
                <div className="home-stage-info">
                  <span className="home-stage-tag">{t("home.hero.demo.category")}</span>
                  <strong className="home-stage-name">{t("home.hero.demo.productName")}</strong>
                </div>

                {/* Shot selector tabs */}
                <div className="home-stage-shot-tabs">
                  {showcaseShots.map((shot, idx) => (
                    <button
                      key={shot.id}
                      type="button"
                      onClick={() => {
                        setActiveShotIndex(idx);
                        setIsCompareOriginal(false);
                      }}
                      className={`home-shot-tab ${idx === activeShotIndex && !isCompareOriginal ? "active" : ""}`}
                    >
                      <span>0{idx + 1}</span>
                      <strong>{t(shot.titleKey)}</strong>
                    </button>
                  ))}
                  <button
                    type="button"
                    onClick={() => setIsCompareOriginal(!isCompareOriginal)}
                    className={`home-shot-tab compare-toggle ${isCompareOriginal ? "active" : ""}`}
                    title={t("home.hero.demo.comparePrompt")}
                  >
                    <RotateCw size={13} className="shrink-0" />
                    <span>{isCompareOriginal ? "查看生成大片" : t("home.hero.demo.comparePrompt")}</span>
                  </button>
                </div>
              </div>

              {/* Showcase Visual Area */}
              <div className="home-stage-viewport">
                <div className="home-stage-image-wrapper">
                  <img
                    src={isCompareOriginal ? "/home-showcase/forma-reference.jpg" : currentShot.image}
                    alt={t(currentShot.titleKey)}
                    className="home-stage-main-img"
                  />

                  {/* Top-Right Resolution & Spec Overlay */}
                  <div className="home-stage-spec-tag">
                    <span className="home-spec-badge">
                      {isCompareOriginal ? "手机实拍参考" : `${currentShot.resolution} · ${currentShot.aspectRatio}`}
                    </span>
                    {!isCompareOriginal && (
                      <span className="home-spec-status">
                        <Check size={12} className="text-emerald-400" />
                        {t("home.hero.demo.status")}
                      </span>
                    )}
                  </div>

                  {/* Bottom Prompt Banner */}
                  <div className="home-stage-bottom-bar">
                    <div className="home-stage-prompt-preview">
                      <Wand2 size={14} className="shrink-0 text-amber-400" />
                      <p className="truncate text-xs text-slate-200">
                        {isCompareOriginal ? "原始实拍图：自然光反射与日常环境" : currentShot.promptSnippet}
                      </p>
                    </div>
                    <button
                      type="button"
                      onClick={() => setIsDetailModalOpen(true)}
                      className="home-stage-inspect-btn"
                    >
                      <Eye size={13} />
                      <span>{t("home.compare.viewDetail")}</span>
                    </button>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </section>

        {/* ── 2. Feature Hub Navigation ── */}
        <section className="home-section home-hub-section home-reveal">
          <div className="home-section-header text-center">
            <span className="home-section-kicker">{t("home.hub.eyebrow")}</span>
            <h2 className="home-section-title">{t("home.hub.title")}</h2>
            <p className="home-section-desc">{t("home.hub.description")}</p>
          </div>

          <div className="home-hub-grid">
            {/* Card 1: Product Intake */}
            <Link to="/products/new" className="home-hub-card">
              <div className="home-hub-card-top">
                <div className="home-hub-icon-box bg-indigo-500/10 text-indigo-500 border-indigo-500/20">
                  <Bot size={22} />
                </div>
                <span className="home-hub-pill">{t("home.hub.create.badge")}</span>
              </div>
              <h3 className="home-hub-card-title">{t("home.hub.create.title")}</h3>
              <p className="home-hub-card-desc">{t("home.hub.create.desc")}</p>
              <div className="home-hub-card-action">
                <span>{t("home.hub.create.action")}</span>
                <ArrowRight size={14} />
              </div>
            </Link>

            {/* Card 2: Visual Workbench */}
            <Link to="/products" className="home-hub-card highlighted">
              <div className="home-hub-card-top">
                <div className="home-hub-icon-box bg-violet-500/10 text-violet-400 border-violet-500/20">
                  <Network size={22} />
                </div>
                <span className="home-hub-pill highlight">{t("home.hub.workbench.badge")}</span>
              </div>
              <h3 className="home-hub-card-title">{t("home.hub.workbench.title")}</h3>
              <p className="home-hub-card-desc">{t("home.hub.workbench.desc")}</p>
              <div className="home-hub-card-action">
                <span>{t("home.hub.workbench.action")}</span>
                <ArrowRight size={14} />
              </div>
            </Link>

            {/* Card 3: Freeform Chat & Inpaint */}
            <Link to="/image-chat" className="home-hub-card">
              <div className="home-hub-card-top">
                <div className="home-hub-icon-box bg-amber-500/10 text-amber-500 border-amber-500/20">
                  <MessageSquare size={22} />
                </div>
                <span className="home-hub-pill">{t("home.hub.chat.badge")}</span>
              </div>
              <h3 className="home-hub-card-title">{t("home.hub.chat.title")}</h3>
              <p className="home-hub-card-desc">{t("home.hub.chat.desc")}</p>
              <div className="home-hub-card-action">
                <span>{t("home.hub.chat.action")}</span>
                <ArrowRight size={14} />
              </div>
            </Link>

            {/* Card 4: Media & Delivery Library */}
            <Link to="/media-library" className="home-hub-card">
              <div className="home-hub-card-top">
                <div className="home-hub-icon-box bg-emerald-500/10 text-emerald-500 border-emerald-500/20">
                  <Images size={22} />
                </div>
                <span className="home-hub-pill">{t("home.hub.library.badge")}</span>
              </div>
              <h3 className="home-hub-card-title">{t("home.hub.library.title")}</h3>
              <p className="home-hub-card-desc">{t("home.hub.library.desc")}</p>
              <div className="home-hub-card-action">
                <span>{t("home.hub.library.action")}</span>
                <ArrowRight size={14} />
              </div>
            </Link>

            {/* Card 5: Settings & Model Hub */}
            <Link to="/settings" className="home-hub-card">
              <div className="home-hub-card-top">
                <div className="home-hub-icon-box bg-sky-500/10 text-sky-500 border-sky-500/20">
                  <Settings2 size={22} />
                </div>
                <span className="home-hub-pill">{t("home.hub.settings.badge")}</span>
              </div>
              <h3 className="home-hub-card-title">{t("home.hub.settings.title")}</h3>
              <p className="home-hub-card-desc">{t("home.hub.settings.desc")}</p>
              <div className="home-hub-card-action">
                <span>{t("home.hub.settings.action")}</span>
                <ArrowRight size={14} />
              </div>
            </Link>
          </div>
        </section>

        {/* ── 3. Production Workflow Walkthrough ── */}
        <section className="home-section home-graph-guide home-reveal">
          <div className="home-section-header">
            <span className="home-section-kicker">{t("home.graphGuide.eyebrow")}</span>
            <h2 className="home-section-title">{t("home.graphGuide.title")}</h2>
            <p className="home-section-desc">{t("home.graphGuide.description")}</p>
          </div>

          <div className="home-guide-surface">
            {/* Step Selector Horizontal Bar */}
            <div className="home-guide-stepper" role="tablist">
              {graphSteps.map((step, idx) => {
                const StepIcon = step.icon;
                const isCurrent = idx === activeGraphStep;
                return (
                  <button
                    key={step.id}
                    type="button"
                    role="tab"
                    aria-selected={isCurrent}
                    onClick={() => setActiveGraphStep(idx)}
                    className={`home-guide-step-btn ${isCurrent ? "active" : ""}`}
                  >
                    <span className="step-num">0{idx + 1}</span>
                    <StepIcon size={15} className="step-icon" />
                    <span className="step-label">{t(step.nameKey)}</span>
                  </button>
                );
              })}
            </div>

            {/* Active Step Panel */}
            <div className="home-guide-viewer">
              <div className="home-guide-info-pane">
                <div className="home-guide-node-tag">
                  <span className="node-role">{currentStep.role}</span>
                </div>
                <h3 className="home-guide-step-title">{t(currentStep.nameKey)}</h3>
                <p className="home-guide-step-desc">{t(currentStep.descKey)}</p>

                <div className="home-guide-meta-box">
                  <span className="meta-label">环节目标</span>
                  <span className="meta-val">{currentStep.meta}</span>
                </div>

                <div className="home-guide-actions">
                  <Link to="/products" className="home-guide-btn">
                    <LayoutGrid size={14} />
                    <span>{t("home.hub.workbench.action")}</span>
                  </Link>
                </div>
              </div>

              {/* Visual Preview */}
              <div className="home-guide-visual-pane">
                <img
                  src="/home-showcase/ui-workbench.png"
                  alt="ProductFlow Visual Workbench"
                  className={`workbench-preview-img zoom-step-${activeGraphStep}`}
                />
                <div className="workbench-preview-overlay" />
              </div>
            </div>
          </div>
        </section>

        {/* ── 4. Quality Comparison (Before vs After) ── */}
        <section className="home-section home-compare-section home-reveal">
          <div className="home-section-header text-center">
            <span className="home-section-kicker">{t("home.compare.eyebrow")}</span>
            <h2 className="home-section-title">{t("home.compare.title")}</h2>
            <p className="home-section-desc">{t("home.compare.description")}</p>
          </div>

          <div className="home-compare-container">
            {/* Split Comparison Frame */}
            <div className="home-compare-frame">
              {/* Left: Original Reference */}
              <div className="home-compare-side original">
                <img src="/home-showcase/forma-reference.jpg" alt="Original Reference" />
                <div className="home-compare-caption left">
                  <span className="badge">{t("home.compare.original")}</span>
                  <p>{t("home.compare.originalDesc")}</p>
                </div>
              </div>

              {/* Center Divider Sparkle */}
              <div className="home-compare-divider">
                <div className="divider-line" />
                <div className="divider-badge">
                  <Sparkles size={16} className="text-amber-400" />
                  <span>AI 真实流转</span>
                </div>
              </div>

              {/* Right: Generated Master */}
              <div className="home-compare-side generated">
                <img src="/home-showcase/forma-hero.jpg" alt="Generated Masterpiece" />
                <div className="home-compare-caption right">
                  <span className="badge highlight">{t("home.compare.generated")}</span>
                  <p>{t("home.compare.generatedDesc")}</p>
                </div>
              </div>
            </div>

            {/* Quick 3-Angle Output Strip */}
            <div className="home-compare-strip">
              {showcaseShots.map((shot, idx) => (
                <div
                  key={shot.id}
                  className={`home-strip-card ${idx === activeShotIndex ? "active" : ""}`}
                  onClick={() => setActiveShotIndex(idx)}
                >
                  <img src={shot.image} alt={t(shot.titleKey)} />
                  <div className="strip-info">
                    <span className="strip-index">0{idx + 1}</span>
                    <strong className="strip-title">{t(shot.titleKey)}</strong>
                    <span className="strip-res">{shot.resolution}</span>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </section>

        {/* ── 5. Core Advantages ── */}
        <section className="home-section home-tech-section home-reveal">
          <div className="home-section-header">
            <span className="home-section-kicker">{t("home.tech.eyebrow")}</span>
            <h2 className="home-section-title">{t("home.tech.title")}</h2>
            <p className="home-section-desc">{t("home.tech.description")}</p>
          </div>

          <div className="home-tech-grid">
            <div className="home-tech-card">
              <div className="tech-icon-circle bg-indigo-500/10 text-indigo-400 border-indigo-500/20">
                <Sparkles size={24} />
              </div>
              <h3 className="tech-card-title">{t("home.tech.card1.title")}</h3>
              <p className="tech-card-desc">{t("home.tech.card1.desc")}</p>
            </div>

            <div className="home-tech-card">
              <div className="tech-icon-circle bg-violet-500/10 text-violet-400 border-violet-500/20">
                <ShieldCheck size={24} />
              </div>
              <h3 className="tech-card-title">{t("home.tech.card2.title")}</h3>
              <p className="tech-card-desc">{t("home.tech.card2.desc")}</p>
            </div>

            <div className="home-tech-card">
              <div className="tech-icon-circle bg-emerald-500/10 text-emerald-400 border-emerald-500/20">
                <Layers size={24} />
              </div>
              <h3 className="tech-card-title">{t("home.tech.card3.title")}</h3>
              <p className="tech-card-desc">{t("home.tech.card3.desc")}</p>
            </div>

            <div className="home-tech-card">
              <div className="tech-icon-circle bg-sky-500/10 text-sky-400 border-sky-500/20">
                <Zap size={24} />
              </div>
              <h3 className="tech-card-title">{t("home.tech.card4.title")}</h3>
              <p className="tech-card-desc">{t("home.tech.card4.desc")}</p>
            </div>
          </div>
        </section>

        {/* ── 6. Footer CTA ── */}
        <section className="home-cta-section home-reveal">
          <div className="home-cta-box">
            <div className="home-cta-content">
              <h2 className="home-cta-title">{t("home.footerCta.title")}</h2>
              <p className="home-cta-desc">{t("home.footerCta.desc")}</p>
              <div className="home-cta-buttons">
                <Link to="/products/new" className="home-btn-primary">
                  <Sparkles size={18} />
                  <span>{t("home.footerCta.start")}</span>
                </Link>
                <Link to="/products" className="home-btn-secondary">
                  <span>{t("home.footerCta.docs")}</span>
                  <ChevronRight size={16} />
                </Link>
              </div>
            </div>
          </div>
        </section>
      </main>

      {/* ── Detail Inspect Modal ── */}
      {isDetailModalOpen && (
        <div className="home-modal-overlay" onClick={() => setIsDetailModalOpen(false)}>
          <div className="home-modal-dialog" onClick={(e) => e.stopPropagation()}>
            <div className="home-modal-header">
              <div className="flex items-center gap-2">
                <ImageIcon size={18} className="text-indigo-400" />
                <h3 className="text-base font-semibold text-slate-100">
                  {t(currentShot.titleKey)} · 商业成片效果
                </h3>
              </div>
              <button
                type="button"
                onClick={() => setIsDetailModalOpen(false)}
                className="home-modal-close"
                aria-label={t("home.compare.closeModal")}
              >
                <X size={18} />
              </button>
            </div>

            <div className="home-modal-body">
              <div className="modal-img-container">
                <img src={currentShot.image} alt={t(currentShot.titleKey)} />
              </div>
              <div className="modal-info-sidebar">
                <div>
                  <span className="sidebar-subtitle">{t("home.compare.promptTitle")}</span>
                  <div className="sidebar-prompt-box text-xs text-slate-300">
                    {currentShot.promptSnippet}
                  </div>
                </div>

                <div>
                  <span className="sidebar-subtitle">{t("home.compare.specsTitle")}</span>
                  <div className="space-y-2 text-xs">
                    <div className="flex justify-between border-b border-slate-800 pb-1.5">
                      <span className="text-slate-400">输出分辨率</span>
                      <span className="font-mono text-slate-200">{currentShot.resolution}</span>
                    </div>
                    <div className="flex justify-between border-b border-slate-800 pb-1.5">
                      <span className="text-slate-400">画幅比例</span>
                      <span className="font-mono text-slate-200">{currentShot.aspectRatio}</span>
                    </div>
                    <div className="flex justify-between border-b border-slate-800 pb-1.5">
                      <span className="text-slate-400">交付格式</span>
                      <span className="font-mono text-indigo-400">{currentShot.deliveryFormat}</span>
                    </div>
                  </div>
                </div>

                <div className="pt-2">
                  <Link
                    to="/products"
                    className="flex w-full items-center justify-center gap-2 rounded-xl bg-indigo-600 px-4 py-2.5 text-xs font-semibold text-white transition hover:bg-indigo-500"
                  >
                    <LayoutGrid size={14} />
                    <span>查看全部商品</span>
                  </Link>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
