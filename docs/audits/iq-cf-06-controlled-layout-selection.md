# IQ-CF-06 / CF-B4 受控二维排版选型备忘

日期：2026-09-07  
任务：[compete-facts-controlled-layout.md](tasks/archive/compete-facts-controlled-layout.md)  
父章程：[image-quality.md](image-quality.md)  
范围：选定或否决候选；**未经验证不得写入主依赖**。本备忘不选定后置 PSD 级编辑器。

## 候选

| ID | 候选 | 形态 |
|---|---|---|
| A | 自研最小组合器 | Go `golang.org/x/image` + `opentype` 导出；浏览器消费同输入 Document / LayoutPlan 预览；字体自托管 |
| B | Fabric.js / Konva | 浏览器编辑+导出；服务端另找栅格化 |
| C | Skia / CanvasKit WASM | 双端同引擎 |
| D | HTML/CSS + headless 截图 | 浏览器排版权威，服务端 Playwright/Chromium 导出 |
| E | 扩展 `delivery.Render` | 在缩放裁切路径上叠字 |

## 六维比较

| 维度 | A 自研最小集 | B Fabric/Konva | C Skia/CanvasKit | D HTML+headless | E delivery.Render |
|---|---|---|---|---|---|
| **能力** | 多图层、文本框、对齐、安全区、PNG 导出可测；中文依赖自托管字体；缺字可显式失败。字级编辑器/任意图片分层不做。 | 编辑能力强；服务端一致导出需第二栈；缺字行为依赖浏览器。 | 能力足；中文/字体子集需自管。 | 中文换行贴近浏览器；导出依赖整浏览器。 | 仅有缩放裁切；无层/字体/安全区结构。 |
| **一致性** | Document + LayoutPlan 同输入；权威像素在 Go；预览消费 Plan/同 Compose 输出。夹具上限：**层外接框 ≤1px**；同输入 Compose 字节级确定。 | 预览易做；与 Go/服务端像素对齐未验证。 | 理想双端同引擎，但 WASM 体积与 Go 绑定未在本仓库验证。 | 预览=导出引擎，但运维与 CI 重，未验证。 | 与排版无关。 |
| **许可** | 引擎：BSD-3（`x/image`）。夹具字体 Liberation Sans：SIL OFL 1.1，允许自托管商用。 | MIT 常见；字体仍须自托管。 | BSD-style；体积与分发需另核。 | Chromium 等许可复杂。 | 已有依赖。 |
| **维护** | 无新主依赖；包体小；边界在 `go/internal/layout` + `web/src/lib/layout`。 | 新增 web 主依赖；双端一致性长期成本高。 | 新增重型依赖；与 Gin/worker 边界大。 | 引入浏览器二进制与 CI 镜像。 | 会污染交付缩放语义。 |
| **归属** | 排版状态 = Document JSON（产物/元数据）；导出图 `parent_asset_id`→主体资产；失败 → `layout_qualified=false` + unresolved，不进「静默合格」。不新建第二媒体仓库。 | 状态易留在浏览器；服务端权威不清。 | 同左，需自建持久化。 | 同左。 | 无排版状态。 |
| **非目标** | **不**调度 Graph DAG；**不**第二工作流编辑器；**不**任意图片 PSD 分层。 | 易滑向通用设计器。 | 同上。 | 同上。 | 达不到合同能力。 |

## 结论

- **选定：A（自研最小组合器）**。主依赖不新增第三方排版 SDK；仅使用仓库已有 `golang.org/x/image`。
- **否决 B/C/D**：能力或一致性未在本任务用固定夹具验证，不得写入 `go.mod` / `web` 主依赖。
- **否决 E**：`delivery.Render` 合同是缩放裁切，不是图内排版。

## 一致性上限（夹具）

| 项 | 上限 |
|---|---|
| 同 Document 两次 Go Compose | PNG SHA-256 相同；LayoutPlan 深等 |
| 预览层外接框 vs 导出 LayoutPlan | 各边 ≤1px（整数坐标期望 0） |
| 规格字改字号 | 主体位图资产 id 与内容 SHA-256 不变；仅 Document/导出图变化 |
| 缺字 | `layout_qualified=false`；不得当合格 |

## 归属与接线

- 包：`go/internal/layout`（导出权威）；`web/src/lib/layout`（同 schema 预览/Plan 消费）。
- Lineage：`subject_asset_id` + 导出结果 `parent_asset_id=subject`；不改 `productimageorigintype` 枚举（本批不迁库）。
- 失败进入现有不合格/未解决项合同（`layout_qualified`），不调用 Graph 调度。
