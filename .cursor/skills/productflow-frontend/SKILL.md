---
name: productflow-frontend
description: Design new or substantially restyled ProductFlow UI using existing tokens, workbench controls and user-facing language. Routine copy corrections, event wiring and nonvisual fixes use the relevant project rules without a design workflow.
---

# ProductFlow Frontend

用于新增界面或实质调整布局、层级和观感。现有工程与文案约定分别由 `web/AGENTS.md`、`.cursor/rules/ui-language.mdc` 维护；token 以 `web/src/index.css` 的相关定义为准。

按需要选择专项资料，不串读整个技能集：

| 当前工作 | 读取材料 |
|---|---|
| 运营工作台的布局或层级设计 | [frontend-app-ui](../frontend-app-ui/SKILL.md) |
| 用户明确要求新的视觉方向 | [PRODUCTFLOW.md](../frontend-design/PRODUCTFLOW.md)，再按该覆盖层使用 [frontend-design](../frontend-design/SKILL.md) |
| 用户要求 UI / 无障碍审查，或需解决具体可用性问题 | [web-design-guidelines](../web-design-guidelines/SKILL.md) 的相关检查项 |
| 画布、检查器、配方或侧栏的行为变化 | `.cursor/rules/workbench.mdc`，以及 `docs/USER_GUIDE.md` 中受影响的操作 |

已加载且未变化的资料可复用。仅修改文案时使用文案规则和相关 i18n 键；仅修接线时读取组件、调用链和测试。

## 这是什么产品

单商家商品视觉工作台。主生产面是画布，不是落地页。用户要扫节点、连边、跑图、选素材。Agent 是旁边的协作者。

## 设计边界

围绕用户在当前界面完成的任务安排信息和操作。只调整本次范围内影响该任务的内容，不为完成设计练习扩大清理范围。

不要新开色板、字体、布局壳、节点卡皮肤。accent 只有一套（亮 indigo / 暗 violet）。字体是 Inter + 中文回退。

## 文案

遵循 `.cursor/rules/ui-language.mdc`，复用已有术语和四语键。图标控件仍需可达名称，陌生图标需提示；必要标签和状态不能只靠颜色表达。

## 画面

- 节点：类型色 + 图标 + 预览；六类必须能在缩略图尺寸扫出来。
- 边：默认低噪声；关系文字只在 hover / 选中 / 详情。
- 卡片只在「卡片本身就是对象」（节点、素材、一次运行）时使用。面板用 layout，不要套一层卡。
- 动效只用于状态切换、抽屉、选中；尊重 `prefers-reduced-motion`。不要为工作台做 hero 入场。

## 完成

受影响的用户流程可用，界面复用当前视觉体系，并完成 `web/AGENTS.md` 中与改动风险相称的检查。报告可观察结果和未验证项；普通局部改动不要求独特性评审、额外设计稿或全站重设计。
