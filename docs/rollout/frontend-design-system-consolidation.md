# 前端设计体系统一整理 — rollout 记录

对应方案：`docs/specs/frontend-design-system-consolidation.md`（Approved，2026-08-16）。
目标约束：每个切片视觉零倒退；只提交本工作流拥有的路径。

## 阶段 0（2026-08-16 完成）

### 交付内容

- `web/src/index.css`：新增 `--color-accent-violet`、`--color-border-control`、`--shadow-accent-sm`、`--shadow-accent`，新增 `.bg-accent-gradient`（两端固定 `#6366f1 → #8b5cf6`，亮暗一致，与存量手写渐变逐像素等价）。
- `web/src/pages/product-create/AgentProductCreateForm.tsx`：2 处手写渐变迁移到 `bg-accent-gradient`；复选框边框迁移到 `border-border-control`；删除与 `dark:border-border-l1` 等值的 `dark:border-slate-700`；`!` 补丁保留并加注释（覆盖 `glass-empty-state` 激活态）。
- `web/AGENTS.md`：新增「Design System And Styling」六条规则。
- `scripts/check-web-design.sh` + `scripts/web-design-allowlist.txt`（59 个存量文件）+ `just web-design-check`。
- `docs/specs/frontend-design-system-consolidation.md` 状态改为 Approved 并记录批准证据。

### 验证证据

- `pnpm --dir web test:run`：248/248 通过。
- `pnpm --dir web lint`：干净。
- `pnpm --dir web build`：通过（仅存量 chunk-size 警告）。
- `scripts/check-web-design.sh <切片文件>`：errors=0、warns=1（`!border-accent` 一行，代码内有理由注释）。
- 产物 CSS 确认包含 `bg-accent-gradient` 与 `shadow-accent-sm`。
- 像素级前后对比（1280×800，threshold=16）：
  - 暗色：`differingPixels=0, diffRatio=0`。
  - 亮色：`differingPixels=0, diffRatio=0`。

### 遗留与顺延

- `AgentProductCreatePage.tsx` 内 2 处手写渐变：该文件存在他人未提交改动，顺延到其 owner 工作落定后迁移。
- `dark:bg-[#0b1424]`（表单 footer）：保留现有观感；阶段 2 评估是否沉淀为 surface token。

## 阶段 1（进行中）

components/ 12 个共享组件的迁移调用矩阵与逐组件证据，待每个组件切片完成后补入本节。

## 阶段 2 / 3 / 4

尚未开始。49 条 `:root.dark` 补丁退役与 pages/ 迁移的证据随后补入。
