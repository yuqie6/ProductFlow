# ProductFlow frontend skills

项目前端技能集。新增界面或实质调整布局、观感时从 `productflow-frontend` 选择相关资料；普通文案、接线和非视觉修复使用 `web/AGENTS.md` 及适用项目规则，不加载整套设计技能。

| 技能 | 来源 | 用途 |
|---|---|---|
| `productflow-frontend` | 本仓库 | 设计任务入口与按需路由 |
| `frontend-app-ui` | Codex `frontend-skill` 改编 | 运营工作台构图、少卡、工具文案 |
| `frontend-design` | [anthropics/skills](https://github.com/anthropics/skills/tree/main/skills/frontend-design) Apache-2.0 | 用户要求新视觉方向时探索；先读其中 `PRODUCTFLOW.md` |
| `web-design-guidelines` | [vercel-labs/web-interface-guidelines](https://github.com/vercel-labs/web-interface-guidelines) MIT | 无障碍、焦点、触控、动效审计 |

文案 / 图标 / 转译的项目法在 `.cursor/rules/ui-language.mdc`，不是技能可选项。

技能支持开发任务，不自行授权写文件、发布或修改产品合同。`agent-service/.pi/skills/` 是产品运行时技能，其确认、工具与评测合同独立维护。

未纳入：Vercel `react-best-practices`（Next.js SSR 瀑布，和本仓库 Vite + TanStack Query 冲突）、`writing-guidelines`（文档手册，不是产品 UI 语言）。
