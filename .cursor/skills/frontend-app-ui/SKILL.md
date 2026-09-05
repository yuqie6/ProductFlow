---
name: frontend-app-ui
description: Compose or substantially restyle ProductFlow operational UI with existing surfaces, icon-led controls and object cards. Use for layout and information hierarchy work, not routine wiring or copy fixes.
---

# Frontend App UI

运营界面靠层级、间距、预览和状态，不靠营销构图。改编自 Codex `frontend-skill` 的 Apps / Utility Copy；落地页、全幅 hero、海报动效不适用于 ProductFlow。

## 构图

- 主工作面、导航、次级 inspector，一个 accent 表示动作或状态。
- 平静的表面层级：`surface-base` / `raised` / `subtle`，边框用 `border-l1/l2/l3`。
- 信息密但可读。少 chrome。
- 默认无卡。面板能变成普通 layout 就不套卡。节点、素材、一次运行除外。
- 不要仪表盘式卡片拼贴、区域厚边框、装饰渐变、多套强调色、不帮忙扫读的装饰图标。

## 画面语言

- 第一眼应能扫到：我在哪、对象是什么、能不能点、现在忙不忙。
- 用预览、类型色、位置、选中环、失败描边代替说明段落。
- 工具条：图标 + `aria-label` / `title`。只有图标含义会撞车时才并排短标签。
- 长名字截断，不能压到相邻控件。关键操作不依赖 hover。

## 文案

- 工具语言：方向、状态、动作。不要承诺、氛围、品牌口号。
- 标题说这是什么区、能做什么。一句补充只解释范围、时效或后果。
- 能出现在首页 hero 里的句子，改到像产品 UI。
- 扫描标题、图标和数字就能懂这一面；否则字太多或寓意不对。

## 动效

- 快、克制、可打断。只做 `transform` / `opacity`。
- 必须提供 `motion-reduce` 关闭或静态替代。
- 不要 `transition: all`。不要为工作台做滚动叙事或 hero 入场。

## 拒绝

- 用卡片墙冒充信息架构
- 用内部 schema 名当标签
- 用一段话解释图标本可以说明的状态
- 为「好看」加第二套 accent 或新字体
