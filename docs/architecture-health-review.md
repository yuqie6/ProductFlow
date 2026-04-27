# ProductFlow 架构健康度审查

> 初始审查日期：2026-04-26
> 最近校准日期：2026-04-27
> 范围：当前仓库静态结构、真实文件大小、当前分层和测试/构建配置。
> 结论用途：指导后续分批改进任务；本文件不执行任何代码重构。

## 1. 总体评分与结论

**总体健康度：7.6 / 10。**

ProductFlow 的基础架构方向是健康的：后端保持 FastAPI presentation / application / domain / infrastructure 四层结构，provider、storage、queue、runtime settings 等外部依赖基本隔离在基础设施层；前端使用 React + TypeScript strict + TanStack Query，并通过 `web/src/lib/api.ts` 和 `web/src/lib/types.ts` 集中 API 与 DTO。

自 2026-04-26 初始审查后，几项最明显的结构债已经被处理：

- 后端 workflow 巨型模块已拆为 `product_workflow_context.py`、`product_workflow_artifacts.py`、`product_workflow_execution.py`、`product_workflow_mutations.py`、`product_workflow_graph.py`、`product_workflow_query.py`、`product_workflow_dependencies.py`，`product_workflows.py` 现在是 144 行 facade。
- 后端 workflow 测试已按主题拆分，原 `backend/tests/test_workflow.py` 已不存在。
- 前端 ProductDetail 已有 page-local 组件、hook 和 helper 拆分，`ProductDetailPage.tsx` 现在约 1024 行。
- 前端已有 ESLint / Vitest 基线，`web/package.json` 提供 `lint`、`test`、`test:run`。
- 后端已引入 typed business errors，`BusinessError` / `NotFoundError` / `ResourceBusyError` 已进入部分 use case 和 presentation 映射。

当前主要架构债不再是“缺少拆分”，而是**已拆模块的边界继续收敛、错误类型迁移未完成、前端质量入口尚未统一到 justfile、复杂交互测试仍偏少**。后续建议保持渐进节奏，不做一次性重写。

## 2. 审查方法与证据来源

本审查基于当前仓库 live inspection。用于校准的命令包括：

```bash
git status --short
find backend/src/productflow_backend backend/tests -type f -name '*.py' -print0 | xargs -0 wc -l | sort -nr
find web/src -type f \( -name '*.ts' -o -name '*.tsx' -o -name '*.css' \) -print0 | xargs -0 wc -l | sort -nr
find backend/alembic/versions -maxdepth 1 -type f -name '*.py' | wc -l
jq '.scripts' web/package.json
```

文档中的行数和文件数是 2026-04-27 的静态快照；后续继续引用本报告前应重新运行上述命令校准。

## 3. 当前结构快照

### 后端

| 区域 | 真实证据 | 观察 |
| --- | ---: | --- |
| 后端 Python 文件数 | `backend/src/productflow_backend` + `backend/tests` 共 81 个 `.py` 文件 | 拆分后文件数增加，但职责更清晰 |
| 后端源码总行数样本 | `backend/src/productflow_backend` + `backend/tests` Python 总计约 13,666 行 | 功能密度仍集中在 workflow execution 和主题测试 |
| Presentation 模块数 | `backend/src/productflow_backend/presentation/` 下 21 个 `.py` 文件 | 路由/schema 有基本拆分 |
| Infrastructure 模块数 | `backend/src/productflow_backend/infrastructure/` 下 21 个 `.py` 文件 | provider、storage、queue、DB 已有适配层 |
| Application 模块数 | `backend/src/productflow_backend/application/` 下 14 个 `.py` 文件 | workflow 相关职责已模块化 |
| Domain 模块数 | `domain/enums.py`、`domain/errors.py`、`domain/workflow_rules.py` | 已开始承载错误类型和纯规则，但领域层仍偏薄 |
| Alembic 迁移 | `backend/alembic/versions/` 下 14 个 migration | 迭代频繁但有迁移纪律 |

当前后端最大热点：

- `backend/src/productflow_backend/application/product_workflow_execution.py`：约 834 行。
- `backend/tests/test_product_workflow_dag.py`：约 1053 行。
- `backend/tests/test_provider_payloads.py`：约 763 行。
- `backend/tests/test_image_sessions.py`：约 644 行。

### 前端

| 区域 | 真实证据 | 观察 |
| --- | ---: | --- |
| 前端源码文件数 | `web/src` 下 44 个 `.ts` / `.tsx` / `.css` 文件 | 页面与 page-local 模块持续拆分，规模仍可控 |
| 前端源码总行数样本 | `web/src` TypeScript/TSX/CSS 总计约 7,719 行 | `ProductDetailPage.tsx` 仍是热点，但已明显降温 |
| 顶层目录 | `web/src/components`、`web/src/lib`、`web/src/pages`、`web/src/pages/product-detail`、`web/src/pages/image-chat` | product detail 与 image chat 都已有 page-local 拆分 |
| package scripts | `dev` / `build` / `lint` / `test` / `test:run` / `preview` | 前端已有基础 lint 与测试脚本 |
| 测试配置 | `web/eslint.config.js` + 6 个 `*.test.ts` | 已有 ESLint + Vitest 基线，但项目级入口仍可更统一 |

当前前端最大热点：

- `web/src/pages/ProductDetailPage.tsx`：约 1024 行。
- `web/src/pages/ImageChatPage.tsx`：仍是较大的页面级交互文件。
- `web/src/pages/product-detail/useWorkflowCanvas.ts`、`InspectorPanel.tsx` 等 page-local 模块承担复杂交互边界。

## 4. 已完成的健康度改进

1. **后端 workflow 模块化已落地**
   - `product_workflows.py` 已从巨型实现文件收敛为 facade。
   - context、artifact、execution、mutation、graph、query、dependency 已拆分为独立模块。
   - 后续重点应从“继续拆文件”转为“收敛 public/private 边界、减少跨模块隐式耦合”。

2. **后端测试已按主题拆分**
   - 原 `test_workflow.py` 已不存在。
   - 现在有 `test_product_workflow_dag.py`、`test_product_workflow_mutations.py`、`test_product_workflow_queue_recovery.py`、`test_image_sessions.py`、`test_provider_payloads.py` 等主题文件。
   - 后续重点是控制单个主题测试继续膨胀，并沉淀 helpers/fixtures。

3. **前端质量门禁已有基线**
   - `web/package.json` 已提供 lint 和 Vitest 脚本。
   - 现有测试覆盖 `imageSizes`、image-chat branching、product-detail canvas/utils/gallery/useWorkflowCanvas 等低成本高价值逻辑。
   - 下一步不是“从零引入工具”，而是把这些脚本接入统一命令和日常检查。

4. **错误类型迁移已开始**
   - `domain/errors.py` 已提供 `BusinessError`、`BusinessValidationError`、`NotFoundError`、`ResourceBusyError`。
   - `presentation/errors.py` 已优先识别 `BusinessError`，并保留旧 `ValueError` fallback。
   - 部分 use case 已改用 `NotFoundError` / `BusinessValidationError`。

5. **连续生图已异步化**
   - `/api/image-sessions/{image_session_id}/generate` 创建 `ImageSessionGenerationTask` 并返回 202。
   - 前端基于 `generation_tasks` 轮询展示排队、运行和失败状态。
   - 旧“HTTP 请求内同步等待 180 秒”的架构风险已经解除。

## 5. 当前问题严重度表

| ID | 严重度 | 问题 | 主要证据 | 影响 | 推荐优先级 |
| --- | --- | --- | --- | --- | --- |
| A1 | P1 | Workflow execution 仍是最大后端热点 | `product_workflow_execution.py` 约 834 行 | 节点执行、失败转移、provider 输入和产物写回仍有较高认知负担 | 第 1 阶段 |
| A2 | P1 | ProductDetail 页面已降温但仍偏大 | `ProductDetailPage.tsx` 约 1024 行 | 页面级状态、mutation、轮询和布局仍集中 | 第 1 阶段 |
| A3 | P2 | 前端 lint/test 未统一到根命令入口 | `justfile` 仍只有 `web-build`，lint/test 需直接用 pnpm | 贡献者容易漏跑 ESLint / Vitest | 第 1 阶段 |
| A4 | P2 | Typed errors 迁移未完成 | `presentation/errors.py` 仍保留 `detail.endswith("不存在")` fallback | 文案变更仍可能影响部分 HTTP status | 第 1-2 阶段 |
| A5 | P2 | 领域层仍偏薄 | domain 已有 errors/rules，但大量业务规则仍在 application | 纯规则测试和复用仍有继续提升空间 | 第 2 阶段 |
| A6 | P2 | 主题测试文件开始出现新热点 | `test_product_workflow_dag.py`、`test_provider_payloads.py` 较大 | 后续新增用例可能再次形成单文件压力 | 第 2 阶段 |
| A7 | P3 | 迁移历史继续增长 | `backend/alembic/versions/` 14 个 revision | 当前可接受；发布稳定化前需确认 fresh install / upgrade 质量 | 第 3 阶段 |

> 当前没有 P0：未发现会立即导致系统不可运行或数据破坏的架构问题。风险主要是可维护性和回归成本。

## 6. 重点问题详解

### A1. Workflow execution 仍是最大后端热点

**证据**

- `backend/src/productflow_backend/application/product_workflow_execution.py` 约 834 行。
- 它仍负责 run/node claim、执行顺序、失败状态、文案节点、生图节点、provider/renderer 调用和部分产物衔接。

**建议方案**

- 不要再做纯粹“为了拆而拆”的移动。
- 优先寻找真实边界，例如：
  - 节点执行策略：copy node / image node / reference node 的 handler。
  - provider 输入构造：prompt/context/reference image payload 组装。
  - run 状态转移：claim / success / failure / retryable failure。
- 每次只抽一个稳定边界，保持 route、DTO 和数据库行为不变。

**验收**

- `uv run --directory backend ruff check .` 和 `just backend-test` 通过。
- workflow DAG、selected-node run、multi-target generation、reference fill、queue recovery 相关测试继续通过。
- 新抽出的模块有明确职责，避免只是把私有函数搬到另一个大文件。

### A2. ProductDetail 页面仍有继续拆分空间

**证据**

- `web/src/pages/ProductDetailPage.tsx` 约 1024 行。
- 已有 `product-detail/` 子目录，但页面仍集中部分 query/mutation、布局和状态协调。

**建议方案**

- 下一轮优先拆“页面协调”和“纯展示”之间的边界，而不是盲目增加 props。
- 可继续收敛：
  - workflow query/cache 更新 helper。
  - 运行按钮和 active run polling 的状态逻辑。
  - inspector 中与 API mutation 无关的表单展示。
- 对 canvas pointer/zoom 这类高风险交互，继续用现有 helper tests 扩展覆盖。

**验收**

- `ProductDetailPage.tsx` 最终目标降到 900 行以下。
- `pnpm --dir web lint`、`pnpm --dir web test:run`、`just web-build` 通过。
- 手动验收 canvas 拖拽、zoom、run selected、图片下载/填充、active run polling。

### A3. 前端质量入口需要统一

**证据**

- `web/package.json` 已有 `lint` / `test:run`。
- 根目录 `justfile` 暂无 `web-lint` / `web-test`。
- `CONTRIBUTING.md` 已提醒直接使用 `pnpm --dir web lint` 和 `pnpm --dir web test:run`。

**建议方案**

1. 在根目录 `justfile` 增加 `web-lint`、`web-test`。
2. README / CONTRIBUTING 统一优先引用 just 命令，并保留无 just 的 pnpm 原始命令。
3. 后续 CI 或 release check 可以按风险逐步纳入 lint/test。

### A4. Typed errors 应继续迁移

**证据**

`presentation/errors.py` 已支持：

```python
if isinstance(exc, BusinessError):
    raise HTTPException(status_code=exc.status_code, detail=detail) from exc
```

但仍保留：

```python
if detail.endswith("不存在"):
    raise HTTPException(status_code=404, detail=detail) from exc
```

**建议方案**

- 新增 not found / capacity / validation 场景优先使用 typed errors。
- 逐步把 `_get_*_or_raise` 和明确业务失败迁移为 `NotFoundError`、`BusinessValidationError`、`ResourceBusyError`。
- 在迁移完成前保留旧 fallback，避免一次性改变 API 行为。

**验收**

- 路由仍返回 `{"detail": "..."}`，不破坏前端。
- 不新增依赖中文后缀判断的新分支。
- `backend/tests/test_error_handling.py` 持续覆盖 typed error 和旧 fallback。

## 7. 推荐优先级排序

1. **统一前端质量入口**
   - 新增 `just web-lint` / `just web-test`，并同步 README / CONTRIBUTING。
   - 这是低风险、高确定性的维护体验改进。

2. **继续迁移 typed business errors**
   - 优先消除新增代码对字符串后缀的依赖。
   - 保留旧 fallback，分批迁移。

3. **收敛 ProductDetail 页面协调逻辑**
   - 目标是让页面文件更像 orchestrator，而不是继续承载细节。
   - 保持 page-local 组件，不把 ProductDetail 专用组件过早放进全局 `components/`。

4. **拆细 workflow execution 的真实业务边界**
   - 只在边界足够清晰时抽模块。
   - 不做大规模 repository 化。

5. **控制测试新热点**
   - 对过大的主题测试继续提取 helpers/fixtures。
   - 保持主题测试可单独运行。

6. **发布稳定化前再审迁移历史**
   - 当前 14 个 migration 不是问题本身。
   - 发布前重点验证 fresh install 和历史 DB upgrade。

## 8. 后续改进验收清单

每个后续结构改进任务都应至少满足：

- **行为不变证明**：说明 public API、DTO、数据库 schema 是否变更；若不变，写明“不改行为”。
- **目标文件指标**：记录改动前后关键文件行数，例如 `ProductDetailPage.tsx`、`product_workflow_execution.py`、主题测试文件。
- **测试/构建**：
  - 后端代码变更：`uv run --directory backend ruff check .` + `just backend-test`。
  - 前端代码变更：`pnpm --dir web lint` + `pnpm --dir web test:run` + `just web-build`。
  - 文档-only：`git diff --check` + 路径/行数 sanity check。
- **关键手动验收**（涉及 ProductDetail/workflow 时）：
  - 打开商品详情，workflow 加载成功。
  - 拖拽节点后不闪回。
  - run selected 会先保存当前 draft。
  - active workflow polling 到 terminal 后刷新商品详情/历史/列表。
  - 图片下载、预览、填充不会触发错误的节点选择或拖拽。
- **回滚路径**：说明是否可直接 revert；若有 migration，说明 downgrade/forward-fix 策略。

## 9. 风险说明

1. **已拆模块仍可能出现“分散的大泥球”**
   - 文件变小不等于边界变清晰。
   - 后续每次抽取都要说明职责边界和调用方向。

2. **前端继续拆分容易产生 props drilling**
   - 如果抽出组件后 props 过多，应暂停并重新设计边界。
   - 优先拆纯展示组件和稳定 helper，再抽 hook。

3. **Repository 抽象过早会增加跳转成本**
   - 当前项目仍是单商户自托管，复杂权限/多租户不是近期目标。
   - 只在 workflow DAG、provider、storage 等真实复杂热点上试点。

4. **错误类型迁移要保持 API 兼容**
   - 前端当前依赖 `ApiError(status, detail)`。
   - 可以新增内部 `code`，但不要立即改变响应 shape，除非同步更新前端和测试。

5. **迁移历史不要在活跃迭代中贸然 squash**
   - 当前 14 个 migration 不是问题本身。
   - 发布前稳定化时再评估 fresh install 与历史 DB upgrade 的维护成本。

## 10. 明确非目标

以下事项不是本报告建议的短期目标：

- 不一次性重写全项目领域模型。
- 不一次性为所有 application use case 引入 repository。
- 不改变现有数据库 schema 或 Alembic 历史。
- 不改变 API response shape、DTO 字段名或前端路由。
- 不引入 Redux/Zustand 等全局状态库来替代 TanStack Query。
- 不把 ProductDetail 专用组件过早移动到全局 `web/src/components/`。

## 11. 建议的下一批 Trellis 任务

1. `frontend-quality-just-commands`：为 `web/` 增加 `just web-lint` / `just web-test` 并同步文档。
2. `continue-typed-business-errors`：继续迁移明确 not found / validation / busy 场景，减少字符串 fallback 覆盖面。
3. `product-detail-orchestrator-cleanup`：继续收敛 ProductDetail 页面级状态协调和 mutation/cache helper。
4. `workflow-execution-boundaries`：按真实业务边界拆细 workflow execution，而不是继续纯移动函数。
5. `test-helper-consolidation`：控制主题测试文件膨胀，提取通用 helpers/fixtures。
