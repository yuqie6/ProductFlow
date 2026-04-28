# ProductFlow Roadmap

这个路线图描述开源自托管版本的演进方向，不代表托管服务承诺。

## 当前阶段：开源自托管可运行

已完成的基础能力：

- FastAPI 后端、React/Vite 前端、PostgreSQL、Redis、Dramatiq worker。
- 单管理员登录和私有工作台。
- 商品创建、图片上传、参考图管理。
- 文案生成、编辑、确认和历史记录。
- 模板海报生成、AI 图片 provider 海报生成、海报下载。
- 连续图片会话和生成图挂回商品。
- 生成图画廊：连续生图结果可收藏到 `/gallery`，保留来源会话、商品、提示词、尺寸、模型和下载入口。
- 商品 DAG 工作流编辑、执行、持久化状态和恢复。
- 产品内 guided onboarding 和共享顶部导航。
- ProductFlow workbench 画布交互：滚轮缩放、左键平移、节点拖拽定位、连接线拖拽创建/删除。
- 参考图单槽位语义、图片拖拽上传、右侧 Details / Runs / Images 精简侧栏和素材填充。
- 提示词配置：商品理解、文案、工作台生图和连续生图模板可在设置页覆盖。
- 初版产品品牌资产、README 展示图和 Web favicon/metadata。
- 设置页管理 provider、模型、上传限制、任务重试等业务配置。
- 运行中轻量状态轮询：连续生图和商品工作流运行时只轮询 status 响应，完成后再刷新完整详情。
- 移动端连续生图页面适配：顶部操作、状态、预览、设置、参考图和参数区按移动端单栏组织。
- Docker Compose 一键自托管路径：`docker compose up -d --build` 可启动 PostgreSQL、Redis、后端 API、Dramatiq worker 和 Web 静态站点；`just release` 已切到 Compose 生产更新和健康检查链路。
- 基础开源文件、MIT License、贡献/安全说明、issue/PR 模板。

## 近期优先级

### 1. 开发体验

- 补充更完整的本地部署截图和 troubleshooting。
- 增加一键 seed/demo 数据脚本。
- 继续打磨 Compose 自托管 troubleshooting、端口冲突说明、storage 迁移提示和升级/回滚示例。

### 2. 测试与质量

- 扩展商品工作流 DAG 的端到端测试样例。
- 扩展前端 Vitest 覆盖，从当前 helper/canvas/cache 测试推进到关键组件交互。
- 为 provider mock、OpenAI Responses provider 和失败分类补更多边界测试。
- 为设置页 secret 更新和不回显行为补独立测试。

### 3. 工作流体验

- 改善 DAG 节点运行日志和失败原因展示。
- 增加节点级重试/跳过/复制能力。
- 继续优化大型商品详情页的局部 loading 和组件拆分；运行中完整 workflow 轮询已改为轻量 status 轮询。
- 继续优化图片会话和商品工作流之间的素材复用入口，例如批量回写、版本对比和更清晰的来源标识。
- 为画布缩放、拖拽、连接、图片填充补自动化回归测试。

### 4. 文档与产品化

- 补充 README / 用户指南截图，让 ProductFlow workbench 的节点、侧栏和引导流程更直观。
- 沉淀轻量品牌使用说明，说明 logo、favicon、README hero 的推荐尺寸和使用边界。
- 补充 provider 配置示例和常见错误排查，而不是扩写依赖清单。

## 中期方向

### 更丰富的输入

- 多来源商品信息导入。
- 商品 URL / 表格导入。
- 更结构化的品牌、受众、卖点输入。

### 更强的素材管理

- 素材收藏、标签、归档。
- 更多尺寸和平台适配。
- 可配置模板和品牌色/Logo。
- 更清晰的生成版本对比。

### Provider 扩展

- 更明确的 OpenAI 兼容 provider 配置示例。
- Provider 能力探测和健康检查。
- 按节点选择不同模型或 provider。
- 可插拔视频 provider 的接口探索。

## 长期探索

- 视频脚本、配音、字幕和模板渲染。
- 多成员协作和权限模型。
- 对象存储适配层。
- 发布容器镜像、Helm chart 或其他生产编排方案。当前已有本仓库内 Docker Compose 自托管路径，但没有发布托管镜像或 Helm chart。
- 与外部店铺/投放平台的受控集成。

## 暂不计划

- 内置托管账号或代管模型密钥。
- 内置支付计费。
- 默认公开注册。
- 无人工确认的全自动投放链路。
