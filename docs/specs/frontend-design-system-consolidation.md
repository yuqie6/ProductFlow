# 前端设计体系统一整理方案

状态：Approved。批准证据：2026-08-16 本会话维护者批准，附加验收条件为「视觉效果零倒退」；执行与证据记录在 `docs/rollout/frontend-design-system-consolidation.md`。

## 1. 背景与现状证据

ProductFlow 前端已有集中式语义 token 层，位于 `web/src/index.css`：

- 表面：`surface-base` / `surface-raised` / `surface-subtle` / `surface-inverse`
- 边框：`border-l1` / `border-l2` / `border-l3`
- 文本：`text-primary` / `text-secondary` / `text-muted`
- 主色：`accent` / `accent-strong` / `accent-soft` / `accent-fg`
- 状态：`state-success` / `state-warning` / `state-error`
- 动效：`ease-in-out` / `ease-spring` / `duration-fast` / `duration-slow`

该层的 rollout 方式是刻意增量的：先建 token 层，同阶段不迁移存量组件。目前迁移停滞在很小的范围。基线采集自 `web/src`（命令可复现，数字不含测试文件）：

| 指标 | 数值 | 复现命令 |
|---|---|---|
| 非测试 `.tsx` 文件总数 | 65 | `find web/src -name '*.tsx' ! -name '*.test.tsx' \| wc -l` |
| 使用 `surface-*` 语义 token 的文件 | 3 | `grep -rlE 'surface-raised\|surface-subtle\|surface-base' web/src --include='*.tsx'` |
| 使用 raw `slate-` / `zinc-` 的文件 | 60 | `grep -rlE 'slate-\|zinc-' web/src --include='*.tsx'` |
| `.tsx` 内 `!border-*` / `!bg-*` 等 `!important` 工具类行 | 68 | `grep -rn '![a-z][a-z0-9]*-' web/src --include='*.tsx'` |
| `index.css` 内 `:root.dark .*` 全局深色补丁 | 49 条 | `grep -cE '^:root.dark \.' web/src/index.css` |
| 硬编码 `#6366f1` | `index.css` 6 处 + `.tsx` 7 处 | `grep -rn '#6366f1' web/src` |
| 手写 indigo→violet 渐变 | 4 处，分布在 2 个文件 | `grep -rn 'from-\[#6366f1\].*to-\[#8b5cf6\]' web/src` |
| `btn-primary-spring` / `input-premium` 复用点 | 4 处 / 3 处 | `grep -rn 'btn-primary-spring\|input-premium' web/src` |

`components/` 下 12 个共享组件全部还在使用 raw 色系；`pages/` 内 raw 色系文件分布为：`product-detail` 13、`product-workflow-v2` 9、`agent-workbench` 7、`image-chat` 6、`product-create` / `product-list` / `legacy-history` 各 1。

## 2. 问题定性

1. 颜色体系双轨。新迁移的 3 个文件使用语义 token，其余 60 个文件手写 `slate-` / `zinc-` 并在 className 里堆 `dark:` 分支和 `!` 补丁。同一种控件存在两种写法。
2. 深色模式依赖补偿层。`index.css` 的 49 条 `:root.dark .bg-white`、`:root.dark .text-zinc-*` 规则是为 raw 工具类兜底。存量组件不迁移，该层只能继续增长，且与 token 变量同时生效，排查主题问题需要同时检查两套规则。
3. 品牌渐变没有单一来源。token 层缺少 violet 端点和渐变/阴影定义，组件只能硬编码 `#6366f1` / `#8b5cf6`，换品牌色必须全局搜索 hex。
4. 已有复用类未制度化。`btn-primary-spring`、`input-premium`、`glass-*` 等类存在，但规则没有写入 `web/AGENTS.md`，也没有机械检查，新代码继续重复手写。

## 3. 目标与非目标

目标：

- 新写和改动的 markup 只用语义 token 与 `index.css` 内声明的共享类。
- 用 allowlist 白名单把 60 个存量文件变成可度量的烧毁清单。
- 49 条全局深色补丁逐条退役，退役前必须有零消费者的 grep 证据。
- `web/AGENTS.md` 成为前端设计规则的可执行 owner 文档。

非目标：

- 不引入第三方 UI 组件库，不新建平行组件体系。
- 不在单个 PR 里重写全部存量文件。
- token 替换迁移是视觉等价操作，不允许借迁移顺带改变视觉或交互。
- 不修改第三方组件样式（React Flow、vaul 等）的既有补偿规则，除非该规则指向的消费者已经清零。
- `legacy-history` 只读历史页最后处理，允许在 allowlist 中长期豁免并注明原因。

## 4. 设计规范（阶段 0 写入 `web/AGENTS.md`）

1. 颜色只允许语义 token：`surface-*`、`border-l*`、`text-*`、`accent*`、`state-*`。新代码禁止 `slate-`、`zinc-`、`gray-`、`neutral-` 色系工具类和品牌 hex。
2. 深色模式通过 `.dark` 覆盖同名 token 变量生效。禁止新增 `!border-*` / `!bg-*` / `!text-*` 颜色补丁，禁止新增 `:root.dark .*` 全局兜底规则。
3. indigo→violet 渐变统一使用 `bg-accent-gradient` 类；accent 阴影统一使用 `shadow-accent` / `shadow-accent-sm` 工具类。
4. 按钮、输入框、玻璃面板优先复用 `btn-primary-spring`、`btn-secondary-spring`、`btn-danger-spring`、`input-premium`、`textarea-premium`、`glass-empty-state`、`glass-inspector`。同一非平凡样式组合出现 3 个及以上调用点时才在 `index.css` 新增共享类，不提前抽象。
5. `components/ui/` 原语只在出现 3 个及以上重复非平凡行为时提取，提取前先搜索现有实现；原语只收 props，不自行请求数据。
6. 任何涉及样式的改动，验证包含亮/暗色、桌面/窄桌面/移动端（断点 `max-width: 1023px`，移动端验证壳 390px）真实浏览器截图。

### 4.1 例外：营销落地页（Atelier Zero）

2026-08-16 维护者在会话中要求用 Atelier Zero 编辑拼贴视觉语言实现公开营销落地页。`web/src/pages/landing/` 是该视觉族的唯一 owner：允许在页面级 `landing.css` 内声明 `--atelier-*` 自有 token 与页面级类；`index.css` 语义 token、`bg-accent-gradient` 和共享类仍是应用侧单一来源。该页面 markup 仍通过 `scripts/check-web-design.sh` 的禁止色扫描，其余 60 个存量文件的迁移规则不变。

## 5. 阶段计划

### 阶段 0：立规则、补 token、建检查（一次交付）

1. `index.css` 增加：
   - `@theme` 内 `--color-accent-violet`（`rgb(139 92 246)`，亮暗色一致）。
   - `--shadow-accent-sm`、`--shadow-accent`。
   - `.bg-accent-gradient` 类。为满足「视觉零倒退」，该类两端固定为 `#6366f1 → #8b5cf6`，亮暗主题一致，与存量手写渐变的实际渲染逐像素相同；不按原草稿做暗色变亮覆盖。
2. 迁移现有 4 处手写渐变到 `bg-accent-gradient` + shadow token。其中 `AgentProductCreatePage.tsx` 的 2 处因该文件存在他人未提交改动，顺延到其 owner 工作落定后处理，先行迁移 `product-create/AgentProductCreateForm.tsx` 的 2 处。
3. `web/AGENTS.md` 写入第 4 节六条规则。
4. 新增 `scripts/check-web-design.sh` 与 `scripts/web-design-allowlist.txt`，并注册 `just web-design-check`：
   - 扫描本次改动涉及的文件：`slate-` / `zinc-` / `#6366f1` / `#8b5cf6` 为 ERROR；`!` 颜色补丁为 WARN，要求注释说明理由；`index.css` 新增 `:root.dark .*` 为 ERROR。
   - allowlist 以 60 个存量文件为初始内容，文件迁出白名单时同步删除对应行。
   - 输出违规行号和 `web/AGENTS.md` 指针。

验收：`just web-design-check` 通过；`pnpm --dir web test:run`、`lint`、`build` 通过；渐变迁移前后截图视觉等价。

### 阶段 1 迁移方法修订（2026-08-16，TopNav 首切回滚后生效）

1. 生效值规则：存量 raw 类的暗色实际渲染值以 `index.css` 内 `:root.dark .*` 覆盖层为准。这些覆盖规则是非分层 CSS，优先级高于 Tailwind 分层生成的 `dark:*` 工具类。推导映射的顺序固定为：先查 `index.css` 覆盖层，覆盖层没有该 raw 类时才采用组件里显式写的 `dark:*` 值。
2. 一比一替换后，新 token 不再经过旧覆盖层，因此 token 的亮/暗值必须等于上一步推导出的实际生效值；组件中「被覆盖层压制而从未生效」的显式 `dark:*` 类直接删除，不保留等价副本。
3. A/B 基线验证：每个切片建立基线快照（`git archive <parent-commit>` 解包到 `/tmp`，复用 `node_modules` 软链，以相同 env 起第二个 Vite 端口，连同一后端），在基线服务与工作区服务上以相同主题、locale、页面状态截图，逐像素对比，目标 diffRatio=0。数据会变化的列表页按改动区域裁剪比较。出现非零差异先归因；无法解释的差异立即回滚该切片，禁止带疑提交。
4. 首切回滚记录：TopNav 切片曾因违反生效值规则被回滚（工作区文件恢复到父提交，allowlist 恢复 59 项，导航头像素对比 0 差异确认恢复），作为后续切片的反例引用。

### 阶段 1：迁移 `components/` 12 个共享组件

每个组件一个独立 commit，迁移顺序按调用面从大到小排（执行时用 `grep -rn` 生成调用矩阵作为 PR 描述）：

`CompactFormFields`、`ConfirmDialog`、`GalleryImagePreviewDialog`、`ImageAspectRatioPicker`、`ImageDropZone`、`ImageGenerationSettingsPanel`、`ImageGenerationSettingsTabs`、`ImageSizePicker`、`ImageToolControls`、`PromptPreviewDialog`、`SelectField`、`TopNav`。

单个组件迁出 allowlist 的条件：

- 该文件扫描零违规；
- 所有调用页亮/暗色截图视觉等价；
- 对应测试（如存在）通过。

### 阶段 2：退役 `index.css` 的 49 条深色补丁

按类别分组处理：`bg-white` 组、`bg-slate/zinc` 组、`border` 组、`text` 组、`shadow` 组、feature-specific 组（`product-create-scrollbar`、`workflow-canvas-*`、`animate-shimmer`）。

每条规则的退役提交必须附带全仓 grep 证据：`web/src` 内 `.tsx` / `.ts` / `.css` 零消费者。仍被第三方组件样式引用的规则保留并在 `index.css` 注释注明所有者。

### 阶段 3：按 feature owner 迁移 `pages/`

批次按目录和依赖顺序：

1. `product-create` + `product-list`（intake 入口面，已部分迁移）。
2. `agent-workbench`（7 个文件）。
3. `product-workflow-v2`（9 个文件）与 `product-detail`（13 个文件），按 `web/AGENTS.md` 的 feature owner 拆分。
4. `image-chat`（6 个文件）。
5. `legacy-history` 最后，允许长期豁免，豁免原因记录在 allowlist 注释。

### 阶段 4：按数据决定 `components/ui/` 原语提取

阶段 1–3 完成后，统计按钮、输入框、卡片、徽章、步进器的重复模式。满足「3 个以上重复非平凡行为」的才提取为原语；每个原语列出调用点与 owner，API 保持 `size` / `variant` / `radius` 最小集合。

## 6. 验证要求（每个阶段共用）

- `pnpm --dir web test:run`、`pnpm --dir web lint`、`pnpm --dir web build`。
- `just web-design-check` 零 ERROR。
- 真实浏览器：亮/暗色、桌面 1280、窄桌面、移动端 390px；截图存档。
- token 替换 commit 必须视觉等价；有意改变视觉的改动单独成 commit 并说明理由。
- 阶段 2 每条补丁删除前：`grep -rn` 证明零消费者，结果放入 commit message 或 rollout 记录。

## 7. 风险与停止条件

- 全局补丁删除影响未覆盖页面：删除提交单独成 commit；出现回归立即 revert 该 commit，保留补丁并记录例外。
- 视觉等价无法验证（如无现网截图基线）：该文件暂缓迁移，留在 allowlist 并记录原因。
- 工作区存在其他未提交改动：迁移只 touch 本工作流负责的文件，不 `reset` / `clean` / 历史改写。
- `docs/adr/0005-agent-workbench-ui.md` 已有 UI 决策：本方案只整理样式体系，不改变该 ADR 已接受的工作台信息架构。

## 8. 所有权与后续记录

| 事项 | 唯一 owner |
|---|---|
| 本方案与范围 | `docs/specs/frontend-design-system-consolidation.md`（本文） |
| 前端可执行设计规则 | `web/AGENTS.md`（阶段 0 写入） |
| token 与共享类定义 | `web/src/index.css` |
| 机械检查与白名单 | `scripts/check-web-design.sh`、`scripts/web-design-allowlist.txt`、`just web-design-check` |
| 迁移进度与证据 | 阶段 0 后新建 `docs/rollout/frontend-design-system-consolidation.md` |
| 知识页 | 阶段 0 后更新 Hindsight「Conventions and patterns」的 frontend styling 小节 |

## 9. 批准证据

2026-08-16 维护者会话批准（批准原话附带条件：「只要你能保证视觉效果不倒退就行」）。执行证据见 `docs/rollout/frontend-design-system-consolidation.md`。
