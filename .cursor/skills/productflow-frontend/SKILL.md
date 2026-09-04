---
name: productflow-frontend
description: ProductFlow 工作台 UI 的项目前端技能入口。画面先于文字、图标替代正文、后端词转译成用户结果语言；并在现有 token 内做克制的运营界面。Use when changing web UI, workbench, inspector, i18n copy, empty/error states, buttons, or visual design.
paths: web/src/**/*.tsx,web/src/**/*.ts,web/src/**/*.css
---

# ProductFlow Frontend

改用户能看见的界面时先读本技能，再读专项技能。权威顺序：

1. `.cursor/rules/ui-language.mdc` — 文案、图标、转译（项目法）
2. `web/src/index.css` `@theme` — 已有 token 与工作台结构
3. `web/AGENTS.md` — 工程合同（无障碍、状态、复用 chrome）
4. [frontend-app-ui](../frontend-app-ui/SKILL.md) — 运营工作台构图
5. [frontend-design](../frontend-design/SKILL.md) — 反模板审美，但必须先读 [PRODUCTFLOW.md](../frontend-design/PRODUCTFLOW.md)
6. [web-design-guidelines](../web-design-guidelines/SKILL.md) — 无障碍 / 焦点 / 触控审计

画布、检查器、配方、侧栏还要跟 `.cursor/rules/workbench.mdc`。操作说明以 `docs/USER_GUIDE.md` 为准。

## 这是什么产品

单商家商品视觉工作台。主生产面是画布，不是落地页。用户要扫节点、连边、跑图、选素材。Agent 是旁边的协作者。

## 开工前

用一句话写下：这一面的唯一工作是什么。然后删掉不服务这句话的字、卡、色、动效。

不要新开色板、字体、布局壳、节点卡皮肤。accent 只有一套（亮 indigo / 暗 violet）。字体是 Inter + 中文回退。

## 文案

- 先问：删掉这句，画面还能否被扫懂？能就删。
- 工具条、节点类型、状态：图标 / 色 / 预览；字进 `aria-label`。
- 空态、确认、失败：一句结果 + 下一步。不解释架构。
- 禁止把 schema、revision、asset id、digest、payload 校验值画在常规 chrome 上。调试信息进开发者工具或明确的「技术详情」折叠。

## 画面

- 节点：类型色 + 图标 + 预览；六类必须能在缩略图尺寸扫出来。
- 边：默认低噪声；关系文字只在 hover / 选中 / 详情。
- 卡片只在「卡片本身就是对象」（节点、素材、一次运行）时使用。面板用 layout，不要套一层卡。
- 动效只用于状态切换、抽屉、选中；尊重 `prefers-reduced-motion`。不要为工作台做 hero 入场。
