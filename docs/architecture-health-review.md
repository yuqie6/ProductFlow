# ProductFlow 架构健康度审查

> 审查日期：2026-04-26
> 范围：当前仓库静态结构、真实文件大小、当前分层和测试/构建配置。
> 结论用途：指导后续分批重构任务；本文件不执行任何代码重构。

## 1. 总体评分与结论

**总体健康度：7.0 / 10。**

ProductFlow 的基础架构方向是健康的：后端已经形成 FastAPI presentation / application / domain / infrastructure 四层结构，provider、storage、queue、runtime settings 等外部依赖大体被隔离在基础设施层；前端使用 React + TypeScript strict + TanStack Query，并通过 `web/src/lib/api.ts` 和 `web/src/lib/types.ts` 集中 API 与 DTO。当前系统已经能承载快速产品迭代。

主要架构债不在“方向错误”，而在**热点模块过大、领域层过薄、测试和错误边界过于集中**。这些问题短期不会阻断功能，但会让后续 DAG 工作台、图片会话、provider 扩展、错误分类和前端交互回归的迭代成本持续上升。

推荐路线是**渐进式模块化**：先补测试/质量护栏和错误类型边界，再拆最热的后端 workflow 与前端 ProductDetail workbench，最后再按真实业务热点引入 repository/domain service 抽象。不要一次性重写全项目领域模型或迁移历史。

## 2. 审查方法与证据来源

本审查基于当前仓库的 live inspection，而不是通用架构建议。使用过的主要命令包括：

```bash
git status --short
find backend/src/productflow_backend backend/tests -type f -name '*.py' -print0 | xargs -0 wc -l | sort -nr
find web/src -type f \( -name '*.ts' -o -name '*.tsx' \) -print0 | xargs -0 wc -l | sort -nr
find web -maxdepth 4 -type f \( -name '*test*' -o -name '*spec*' -o -name 'eslint.config.*' -o -name '.eslintrc*' -o -name '.prettierrc*' \)
rg -n "from sqlalchemy\.orm import Session|productflow_backend\.infrastructure" backend/src/productflow_backend/application backend/src/productflow_backend/presentation/routes
jq '.scripts' web/package.json
```

审查时的工作区基线由 Trellis PRD 记录为 clean；当前可见变更只新增本文档，未改动后端、前端或迁移代码。

## 3. 当前结构快照

### 后端

| 区域 | 真实证据 | 观察 |
| --- | ---: | --- |
| 后端 Python 文件数 | `backend/src/productflow_backend` + `backend/tests` 共 57 个 `.py` 文件 | 单体仓库规模仍可控 |
| 后端源码总行数样本 | `backend/src/productflow_backend` + `backend/tests` Python 总计约 10,892 行 | 功能密度集中在少数热点文件 |
| Presentation 模块数 | `backend/src/productflow_backend/presentation/` 下 21 个 `.py` 文件 | 路由/schema 有基本拆分 |
| Infrastructure 模块数 | `backend/src/productflow_backend/infrastructure/` 下 21 个 `.py` 文件 | provider、storage、queue、DB 已有适配层 |
| Application 模块数 | `backend/src/productflow_backend/application/` 下 7 个 `.py` 文件 | 业务编排明显集中 |
| Domain 模块数 | `backend/src/productflow_backend/domain/` 主要为 `enums.py`（84 行）和空 `__init__.py` | 领域层基本只是共享枚举 |
| Alembic 迁移 | `backend/alembic/versions/` 下 12 个 migration | 迭代频繁但有迁移纪律 |

### 前端

| 区域 | 真实证据 | 观察 |
| --- | ---: | --- |
| 前端源码文件数 | `web/src` 下 26 个 `.ts` / `.tsx` / `.css` 文件 | 文件数少，但页面承载较重 |
| 前端源码总行数样本 | `web/src` TypeScript/TSX 总计约 6,276 行 | `ProductDetailPage.tsx` 占比过高 |
| 顶层目录 | `web/src/components`、`web/src/lib`、`web/src/pages`、`web/src/pages/product-detail` | 已开始为 product detail 做 page-local 拆分 |
| package scripts | `web/package.json` 只有 `dev` / `build` / `preview` | 无前端 lint、format、test 脚本 |
| 测试/格式化配置 | 未发现 `*test*` / `*spec*` / `eslint.config.*` / `.prettierrc*` | 前端自动化回归几乎为空 |

## 4. 现有优点

1. **后端分层方向正确**
   - `backend/src/productflow_backend/presentation/`、`application/`、`domain/`、`infrastructure/` 已按职责分层。
   - `backend/src/productflow_backend/main.py` 仅暴露 app entrypoint，实际装配在 `presentation/api.py`。

2. **外部依赖有基础设施边界**
   - 文本 provider 在 `backend/src/productflow_backend/infrastructure/text/`。
   - 图片 provider 在 `backend/src/productflow_backend/infrastructure/image/`。
   - Storage 边界在 `backend/src/productflow_backend/infrastructure/storage.py`。
   - Queue / recovery 边界在 `backend/src/productflow_backend/infrastructure/queue.py`。

3. **异步任务语义已经重视可恢复性**
   - 传统 `JobRun` 与 `WorkflowRun` 都以数据库为权威状态，Redis/Dramatiq 只是投递。
   - `backend/src/productflow_backend/infrastructure/queue.py` 已包含 unfinished job/workflow recovery 入口。
   - 后端 spec 已明确 active run 唯一约束、enqueue 失败回写失败状态、duplicate message no-op 等契约。

4. **API DTO 与前端类型集中管理**
   - 后端 response/request schema 在 `backend/src/productflow_backend/presentation/schemas/`。
   - 前端 DTO 在 `web/src/lib/types.ts`，API client 在 `web/src/lib/api.ts`。
   - 这为后续拆分页面和重构后端 application 提供了可依赖的边界。

5. **已有较强后端回归覆盖**
   - `backend/tests/test_workflow.py` 覆盖 auth、settings、workflow DAG、image session、provider payload、Alembic upgrade、queue recovery 等关键路径。
   - 虽然测试文件过大，但业务保护面是真实存在的。

## 5. 问题严重度表

| ID | 严重度 | 问题 | 主要证据 | 影响 | 推荐优先级 |
| --- | --- | --- | --- | --- | --- |
| A1 | P1 | 后端 workflow application 巨型模块 | `application/product_workflows.py` 1,675 行，约 55 个 class/function 顶层声明 | 修改 DAG 逻辑时回归面大，难定位状态/执行/产物边界 | 第 1 阶段 |
| A2 | P1 | 前端 ProductDetail workbench 页面过大 | `web/src/pages/ProductDetailPage.tsx` 2,430 行，顶层组件/函数/常量约 25 个 | 画布、sidebar、inspector、gallery、mutation 状态耦合，交互回归风险高 | 第 1 阶段 |
| A3 | P1 | 前端缺少自动化测试与 lint/format gate | `web/package.json` 仅 `dev/build/preview`；未发现测试或 ESLint/Prettier 配置 | UI 重构缺少行为护栏，review 只能依赖人工和 `tsc` | 第 1 阶段 |
| A4 | P2 | 领域层偏贫血，业务规则散落在 application | `domain/enums.py` 84 行；application 直接实现大量 DAG、copy、image 规则 | 规则复用和错误类型升级困难，application 文件继续膨胀 | 第 2 阶段 |
| A5 | P2 | Application 直接依赖 SQLAlchemy Session 与 infrastructure 细节 | `application/use_cases.py`、`image_sessions.py`、`product_workflows.py` 均导入 `Session` 和 infrastructure adapters | 单元测试必须带 DB/adapter 思维；难替换 persistence/provider 边界 | 第 2 阶段 |
| A6 | P2 | 错误处理依赖中文字符串后缀判断 | `presentation/errors.py:8-14` 用 `detail.endswith("不存在")` 决定 404 | 文案改动可能改变 HTTP 语义；多语言/错误码扩展脆弱 | 第 1-2 阶段 |
| A7 | P2 | 后端测试覆盖强但集中成单一巨型文件 | `backend/tests/test_workflow.py` 3,256 行，约 63 个测试函数 | 新增回归难发现归属，局部运行慢，fixture/辅助函数复用边界弱 | 第 2 阶段 |
| A8 | P3 | Alembic 迁移频繁，未来发布前需整理策略 | `backend/alembic/versions/` 12 个 revision，其中 2026-04-24 多个 DAG 修复迁移 | 当前可接受；发布前需要明确历史兼容和 fresh install 质量 | 第 3 阶段 |
| A9 | P3 | 前端 `web/src/pages/product-detail/` 已有拆分但仍偏工具化 | 该目录包含 `workflowConfig.ts`、`galleryImages.ts`、下载组件等，但核心组件仍在主页面 | 方向正确，尚未把状态ful UI 边界真正拆出 | 随 A2 一起处理 |

> 当前没有 P0：未发现会立即导致系统不可运行或数据破坏的架构问题。风险主要是可维护性与后续改造成本。

## 6. 重点问题详解

### A1. `product_workflows.py` 同时承担过多职责

**证据**

- `backend/src/productflow_backend/application/product_workflows.py`：1,675 行。
- 顶层声明约 55 个，覆盖 CRUD、node copy edit、image upload/bind、edge create/delete、run kickoff、run execution、node execution、concurrency、context collection、reference fill、history 查询。
- 同一文件内既有 public use case，例如 `start_product_workflow_run(...)`，也有大量私有执行细节，例如 `_execute_image_generation(...)`、`_collect_incoming_context(...)`、`_fill_reference_node(...)`。

**影响**

- DAG 规则、执行恢复、产物写回、provider 输入构造在一个文件内互相影响。
- 后续增加节点级 retry、复制节点、更多 provider 参数、运行日志时，容易继续堆入同一模块。
- review 时难以判断一个改动是在改“图结构持久化”、“运行调度”还是“节点业务执行”。

**建议方案**

先不改外部 API，不改数据库 schema，把文件拆成同 application 包内的低风险模块：

```text
application/product_workflows.py              # 保留对外 use case facade，导入子模块
application/product_workflow_mutations.py     # node/edge/upload/bind/copy edit/delete
application/product_workflow_runs.py          # kickoff、active run、enqueue failure、run history
application/product_workflow_execution.py     # execute run/node、claim、failure transition
application/product_workflow_context.py       # incoming context、product context、reference assets
application/product_workflow_artifacts.py     # SourceAsset/PosterVariant/CopySet 写回与 slot fill
```

每次只移动一类函数，并保持 public function 名称和 route import 不变。第一轮可只抽 `context` 和 `artifacts`，因为它们通常最少影响 API 入口。

**验收**

- `backend/src/productflow_backend/application/product_workflows.py` 降到 600 行以下，作为 facade/聚合入口。
- route import 不变或只做一次可审查的 import 调整。
- `uv run --directory backend ruff check .` 通过。
- `just backend-test` 通过，尤其 workflow DAG、selected-node run、image-generation、queue recovery 相关测试通过。

**风险与回滚**

- 风险：循环 import、helper visibility 调整、SQLAlchemy relationship stale 行为被误改。
- 降低风险：先纯移动函数，不重命名、不改逻辑；每个 PR/commit 只拆一个职责区。
- 回滚：因为不改 schema 和 API，可通过 revert 拆分 commit 直接回滚。

### A2. `ProductDetailPage.tsx` 是前端最大迭代瓶颈

**证据**

- `web/src/pages/ProductDetailPage.tsx`：2,430 行，是前端最大文件。
- 顶层声明包括 canvas path、pan/zoom guard、sidebar tab、runs panel、image preview modal、images panel、workflow node card、inspector panels、多个具体 inspector、TextArea 等。
- `web/src/pages/product-detail/` 已存在局部拆分目录，但主页面仍承担大部分 stateful UI 和组件实现。

**影响**

- Workbench 的画布拖拽、边连接、右侧 tab、图片预览、节点 inspector、autosave/run mutation 的状态耦合在一个文件里。
- 缺少前端测试时，任何拆分都容易引入拖拽闪回、polling 停止、缓存未刷新、下载/填充误触发等回归。
- 新功能很容易继续追加在主页面，进一步放大技术债。

**建议方案**

先按 page-local 目录拆，不提升到全局 `components/`，避免过早泛化：

```text
web/src/pages/product-detail/
  canvas/
    WorkflowCanvas.tsx
    WorkflowNodeCard.tsx
    edges.ts
    pointerGuards.ts
    zoom.ts
  sidebar/
    ProductDetailSidebar.tsx
    RunsPanel.tsx
    ImagesPanel.tsx
    ImagePreviewModal.tsx
  inspectors/
    InspectorPanel.tsx
    ProductContextInspector.tsx
    ReferenceImageInspector.tsx
    CopyNodeInspector.tsx
    ImageGenerationInspector.tsx
  mutations/
    workflowCache.ts
    useWorkflowMutations.ts   # 只有真实复用后再抽
```

第一轮只抽纯展示或低状态组件，例如 `RunsPanel`、`ImagePreviewModal`、`TextArea`、部分 inspector；第二轮再抽 canvas pointer/zoom 逻辑。

**验收**

- `ProductDetailPage.tsx` 第一阶段降到 1,500 行以下，最终目标 900 行以下。
- `just web-build` 通过。
- 手动或未来自动化验证：节点拖拽不闪回、wheel zoom 坐标正确、run selected 会先保存草稿、workflow 完成刷新 product/history/list、图片下载和填充不会触发节点选择/拖拽。

**风险与回滚**

- 风险：拆出组件后 props 过多，形成“假拆分”；或 stale closure 影响 mutations/cache。
- 降低风险：先拆 presentational 组件，再抽 hook；每次拆分后只跑 build 和关键手测。
- 回滚：按组件拆分 commit 回滚，不影响后端数据。

### A3. 前端质量门禁不足

**证据**

- `web/package.json` scripts 只有：
  - `dev`: `vite`
  - `build`: `tsc --noEmit -p tsconfig.app.json && tsc --noEmit -p tsconfig.node.json && vite build`
  - `preview`: `vite preview`
- 未发现 `eslint.config.*`、`.eslintrc*`、`.prettierrc*`、`*test*`、`*spec*`。

**影响**

- TypeScript 能抓类型错误，但抓不到 hook dependency、无障碍、未使用变量风格、格式漂移、交互回归。
- ProductDetail 拆分时缺少最小回归测试，风险集中到人工验收。

**建议方案**

先补轻量质量 gate，不要一次性引入过重工具链：

1. 配置 ESLint：React hooks、TypeScript、import 基本规则即可。
2. 配置 Prettier 或明确只用 ESLint format，不要两个工具规则互相打架。
3. 引入 Vitest + Testing Library 的最小样例，优先覆盖纯 helper 与少数关键 UI：
   - `galleryImages.ts` 去重逻辑。
   - `imageDownloads.ts` 文件名/URL 逻辑。
   - workflow polling / active run helper 若抽成纯函数。
4. 对 canvas pointer 行为暂时先保留手动验收清单，等组件边界稳定后再加高成本测试。

**验收**

- `web/package.json` 新增 `lint`、`format:check` 或等价脚本。
- CI/本地质量命令至少包含 `just web-build` + 前端 lint。
- 有 3-5 个低成本单元测试覆盖 product-detail 纯函数。

**风险与回滚**

- 风险：初次引入 lint 造成大量风格噪音。
- 降低风险：规则先宽后严；首个 PR 只修自动发现的低风险问题。
- 回滚：配置和脚本可独立 revert，不影响运行功能。

### A4/A5. 领域层和 repository 抽象应渐进引入

**证据**

- `backend/src/productflow_backend/domain/enums.py` 84 行，领域层主要承载 enum 值。
- `application/use_cases.py`、`application/image_sessions.py`、`application/product_workflows.py` 均直接导入 SQLAlchemy `Session` 和 `productflow_backend.infrastructure.*`。
- 当前 application 既做业务规则，又做 ORM 查询、relationship 选择、adapter 构造和部分运行时配置读取。

**影响**

- 当前测试多以 DB fixture / TestClient 方式覆盖；纯规则级单元测试不易落地。
- 后续如果要接对象存储、多 provider 能力探测、多用户权限，application 的 dependency surface 会继续膨胀。

**建议方案**

不要一次性“Clean Architecture 重写”。先按热点抽小边界：

1. **Domain errors**：先引入 typed business exception，替代字符串后缀判断。
2. **Workflow domain helpers**：把纯图规则抽到 domain/application 边界，例如 cycle check、target counting、execution plan selection。
3. **Repository protocol 只服务一个热点**：例如先为 workflow run/node 查询引入 `WorkflowRepository` 或内部 query service，保留 SQLAlchemy 实现。
4. **Provider dependency injection**：对难测的 provider/renderer 构造引入可替换 factory 参数，减少 monkeypatch 范围。

**验收**

- 新增纯规则模块有独立单元测试，不需要 FastAPI TestClient。
- application public use case 签名尽量稳定。
- 迁移期间不要求所有 use case 都通过 repository；只要求新抽象在一个业务热点上证明收益。

### A6. 错误处理依赖字符串后缀，语义脆弱

**证据**

`backend/src/productflow_backend/presentation/errors.py:8-14`：

```python
def raise_value_error_as_http(exc: ValueError) -> NoReturn:
    detail = str(exc)
    if detail == "海报文件不存在":
        raise HTTPException(status_code=400, detail=detail) from exc
    if detail.endswith("不存在"):
        raise HTTPException(status_code=404, detail=detail) from exc
    raise HTTPException(status_code=400, detail=detail) from exc
```

**影响**

- 文案变更会改变 HTTP status。
- 未来同一中文文案需要不同 status 时难表达。
- 前端只拿 `ApiError(status, detail)`，没有稳定业务错误码。

**建议方案**

分两步：

1. 引入最小 typed exception：

```python
class BusinessError(ValueError):
    status_code: int = 400
    code: str = "business_error"

class NotFoundError(BusinessError):
    status_code = 404
    code = "not_found"
```

2. `raise_value_error_as_http(...)` 先兼容 `BusinessError`，旧 `ValueError` fallback 保留一段时间。后续逐步把 `_get_*_or_raise` 和明确业务失败迁移为 typed error。

**验收**

- 路由仍返回 `{"detail": "..."}`，不破坏前端。
- 新增测试覆盖 typed not found -> 404、普通 business error -> 400、旧 ValueError fallback 仍工作。
- 禁止新增依赖 `endswith("不存在")` 的分支。

### A7. 后端测试需要拆分，但不要削弱 workflow 保护面

**证据**

- `backend/tests/test_workflow.py`：3,256 行。
- 约 56 个测试函数，覆盖从 settings 到 DAG、queue recovery、Alembic upgrade 的多个主题。
- `backend/tests/conftest.py` 只有 50 行，辅助函数大量留在主测试文件中。

**影响**

- 添加/定位测试成本增加。
- 局部执行某类测试不直观。
- 辅助函数难复用，也不利于按主题演进 fixture。

**建议方案**

按主题拆测试文件，保持 fixture 和测试语义不变：

```text
backend/tests/test_auth_settings.py
backend/tests/test_product_workflow_dag.py
backend/tests/test_product_jobs.py
backend/tests/test_image_sessions.py
backend/tests/test_storage_downloads.py
backend/tests/test_logging.py
backend/tests/test_migrations.py
backend/tests/helpers.py
```

先移动测试，不改断言；移动后再考虑 shared fixtures。

**验收**

- `just backend-test` 通过。
- 原 `test_workflow.py` 降到 800 行以下或消失。
- 每个主题文件可以用 `pytest backend/tests/test_product_workflow_dag.py` 单独运行。

## 7. 推荐优先级排序

1. **先补安全网：前端 lint/test 最小 gate + 后端 typed error 兼容层。**
   - 这是后续拆巨型文件的保险。
   - 影响面可控，能快速提高 review 和回归信心。

2. **拆前端 ProductDetail 的低状态组件与纯 helper。**
   - 先拆 `RunsPanel` / `ImagePreviewModal` / inspector 展示组件。
   - 再拆 canvas pointer/zoom/stateful mutations。

3. **拆后端 `product_workflows.py` 的 context/artifact/execution 子模块。**
   - 保持 route 和 API 不变。
   - 每次只移动一类职责。

4. **拆后端测试文件，保持覆盖不变。**
   - 测试拆分最好跟后端 workflow 模块拆分错开，避免同一 PR 同时改生产代码和大量测试路径。

5. **按热点引入 domain/repository 抽象。**
   - 从 typed errors、纯规则、workflow repository/query service 开始。
   - 不做全项目一次性 repository 化。

6. **发布稳定化前再审 migration 历史和 open-source release hygiene。**
   - 当前迁移数量可接受；等功能稳定后再决定是否 squash 或只保留兼容链。

## 8. 分阶段重构路线图

### Phase 0：文档和基线确认（本任务）

**目标**：形成可执行的架构审查报告，不改代码行为。

**交付物**

- `docs/architecture-health-review.md`。
- Trellis PRD checklist 勾选文档完成项。

**验收**

- 文档包含真实路径、真实行数、问题分级和路线。
- `git diff --check` 通过。
- 变更只包含 docs / Trellis task checklist。

### Phase 1：质量护栏与错误边界（低风险）

**建议任务**

1. 前端引入 ESLint/format check 的最小配置。
2. 前端为 product-detail 纯 helper 增加 Vitest 基线。
3. 后端引入 typed business errors，保留旧 `ValueError` fallback。

**验收**

- 新增命令可在本地稳定执行。
- `just web-build`、前端 lint/test、`uv run --directory backend ruff check .`、`just backend-test` 通过。
- API error response shape 对前端兼容。

**回滚**

- 工具链配置和 typed error 兼容层均可独立 revert。
- 不涉及 schema migration。

### Phase 2：前端 ProductDetail 渐进拆分

**建议任务**

1. 拆 `RunsPanel`、`ImagePreviewModal`、`TextArea`、inspector 组件。
2. 拆 `ImagesPanel` 与 gallery/fill/download props 边界。
3. 拆 canvas pointer guard、edge path、zoom 纯逻辑。
4. 最后再考虑 `useWorkflowMutations` 或 `useWorkflowCanvas`，避免一开始抽过深 hook。

**验收**

- `ProductDetailPage.tsx` 逐步降到 1,500 行以下，最终目标 900 行以下。
- 保留 page-local 目录，不把只服务 ProductDetail 的组件放到全局 `components/`。
- `just web-build` 通过；新增测试覆盖能测试的纯函数。
- 手动验收 canvas 拖拽、zoom、run selected、图片下载/填充、active run polling。

**回滚**

- 每个组件拆分一个 commit，发现交互回归可按 commit 回滚。
- 不改 API 和数据库。

### Phase 3：后端 workflow 模块化

**建议任务**

1. 抽 `product_workflow_context.py`：product context、incoming context、reference asset input 收集。
2. 抽 `product_workflow_artifacts.py`：copy/poster/source asset materialization、reference slot fill。
3. 抽 `product_workflow_execution.py`：node run claim、execute node/run、failure transition。
4. 抽 `product_workflow_mutations.py`：node/edge/upload/bind/copy edit/delete。
5. 保留 `product_workflows.py` 作为 facade，直到 route import 稳定后再进一步收敛。

**验收**

- 对外 use case 名称和 HTTP API 行为不变。
- `product_workflows.py` 降到 600 行以下。
- `uv run --directory backend ruff check .` 和 `just backend-test` 通过。
- workflow DAG 相关测试仍覆盖 selected-node run、multi-target generation、reference fill、enqueue recovery、duplicate message no-op。

**回滚**

- 纯移动优先，避免和行为变更混在一起。
- 如果出现循环 import 或 session 生命周期问题，回滚最近一次模块抽取 commit。

### Phase 4：测试结构拆分

**建议任务**

1. 提取 `backend/tests/helpers.py`。
2. 按主题拆分 `test_workflow.py`。
3. 只在拆分完成后再优化 fixture 命名和复用。

**验收**

- `just backend-test` 通过。
- 单主题测试可独立运行。
- 测试移动 PR 不夹带生产代码行为变更。

**回滚**

- 纯测试路径移动可直接 revert。

### Phase 5：领域规则与 repository/query service 试点

**建议任务**

1. 把 workflow execution plan、cycle/target 规则抽成纯函数或 domain service。
2. 对 workflow 查询/持久化引入一个小型 repository/query service，先服务 DAG 热点。
3. 对 provider/renderer 依赖构造提供显式注入点，降低 monkeypatch 和全局 factory 耦合。

**验收**

- 至少一组业务规则可脱离数据库做单元测试。
- application public use case 不因抽象而显著变复杂。
- 不要求全项目统一 repository 化；收益明确后再复制模式。

**回滚**

- 抽象层必须以一个业务热点为边界；如果抽象只增加跳转成本，回滚并保留已经验证有价值的纯函数。

## 9. 后续重构验收清单

每个后续 refactor 任务都应至少满足：

- **行为不变证明**：说明 public API、DTO、数据库 schema 是否变更；若不变，写明“不改行为”。
- **目标文件指标**：记录重构前后关键文件行数，例如 `ProductDetailPage.tsx`、`product_workflows.py`、`test_workflow.py`。
- **测试/构建**：
  - 后端代码变更：`uv run --directory backend ruff check .` + `just backend-test`。
  - 前端代码变更：`just web-build`；若引入 lint/test，也运行对应命令。
  - 文档-only：`git diff --check` + 路径/行数 sanity check。
- **关键手动验收**（涉及 ProductDetail/workflow 时）：
  - 打开商品详情，workflow 加载成功。
  - 拖拽节点后不闪回。
  - run selected 会先保存当前 draft。
  - active workflow polling 到 terminal 后刷新商品详情/历史/列表。
  - 图片下载、预览、填充不会触发错误的节点选择或拖拽。
- **回滚路径**：说明是否可直接 revert；若有 migration，说明 downgrade/forward-fix 策略。

## 10. 风险说明

1. **拆分巨型文件时最大风险是“假重构 + 行为漂移”**
   - 只移动代码但同时顺手改逻辑，会使回归定位困难。
   - 建议每个 commit 只有一种动作：移动、重命名、或行为修复，不混合。

2. **前端拆分容易产生 props drilling**
   - 如果抽出组件后 props 超过 15 个，应暂停并重新设计边界。
   - 先拆纯展示组件，再决定是否抽 hook。

3. **Repository 抽象过早会增加跳转成本**
   - 当前项目仍是单商户自托管，复杂权限/多租户不是近期目标。
   - 只在 workflow DAG、provider、storage 等真实复杂热点上试点。

4. **错误类型迁移要保持 API 兼容**
   - 前端当前依赖 `ApiError(status, detail)`。
   - 可以新增内部 `code`，但不要立即改变响应 shape，除非同步更新前端和测试。

5. **迁移历史不要在活跃迭代中贸然 squash**
   - 当前 12 个 migration 不是问题本身。
   - 发布前稳定化时再评估 fresh install 与历史 DB upgrade 的维护成本。

## 11. 明确非目标

本轮和建议路线中，以下事项不是目标：

- 不在本任务中执行任何实际 refactor。
- 不一次性重写全项目领域模型。
- 不一次性为所有 application use case 引入 repository。
- 不改变现有数据库 schema 或 Alembic 历史。
- 不改变 API response shape、DTO 字段名或前端路由。
- 不引入 Redux/Zustand 等全局状态库来替代 TanStack Query。
- 不把 ProductDetail 专用组件过早移动到全局 `web/src/components/`。
- 不因为前端缺少测试就暂停产品迭代；先补低成本护栏，再拆热点。

## 12. 建议的下一批 Trellis 任务

1. `frontend-quality-gate`：为 `web/` 增加 ESLint/format check 和少量 Vitest helper 测试。
2. `typed-business-errors`：后端引入 typed business error，兼容旧 `ValueError` 映射。
3. `split-product-detail-low-risk-components`：拆 ProductDetail 的 runs/images/inspector 展示组件。
4. `split-product-workflow-context-artifacts`：拆后端 workflow context 与 artifact helper。
5. `split-backend-workflow-tests`：按主题拆 `backend/tests/test_workflow.py`。
