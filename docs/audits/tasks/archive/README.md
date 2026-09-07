# 已关闭 Issue

这里保存完成或取消的 issue，不再占用 [看板](../README.md)。文件名保持稳定，历史证据不覆写；回归或后续工作新建关联 issue。任务关闭不等于业务组验收通过。归档步骤见看板协议。

| Issue | 关闭结果 | 说明 |
|---|---|---|
| [saas-account-team.md](saas-account-team.md) | 完成 | 本人资料、密码/会话、SMTP恢复与防重放；真实隔离PG/API及邮件验证 |
| [saas-account-interface.md](saas-account-interface.md) | 完成 | 四语账户与恢复页面；35项浏览器mock及真实Go接线验证 |
| [category-annotation-rubric.md](category-annotation-rubric.md) | 完成 | 评委适用性与量表修正；47/48有效、比较一致6/8、错商品负控3/3，保留剩余误判 |
| [category-annotation-repeatability.md](category-annotation-repeatability.md) | 完成 | 48项重复标注，候选胜负仅2/8一致；定位评委适用性和身份判读问题 |
| [category-image-annotation.md](category-image-annotation.md) | 完成 | 分类多模态标注与显式候选对照；16/16 有效真实标注，非生成质量达标门 |
| [workbench-continuity.md](workbench-continuity.md) | 完成 | 明确预览入口、采用版本差异与导出连续路径，四语及窄屏浏览器验证 |
| [saas-identity-convergence.md](saas-identity-convergence.md) | 完成 | 账号自有商家与独立管理员、删除成员模型和默认归属，迁移与前后端验证 |
| [saas-policy-alignment.md](saas-policy-alignment.md) | 完成 | 未知调用到期释放用户额度；有限质量检查允许确认采用，保留实际状态并下载 |
| [saas-design-alignment.md](saas-design-alignment.md) | 完成 | 纠正账号与管理员范围、六组现行合同，删除支持占位；收费/采用政策待独立裁定 |
| [saas-workspace-context.md](saas-workspace-context.md) | 取消 | 用户明确普通账号自有一个商家，撤销多组织切换；本轮未提交增量已清除 |
| [smtp-public-registration.md](smtp-public-registration.md) | 完成 | SMTP 环境默认与公开验证码注册；退役邀请和设置二次解锁；整仓 Go 保留已复现的基线失败 |
| [saas-auth-entry-security.md](saas-auth-entry-security.md) | 完成 | 邀请身份证明、Redis 原子准入、来源校验和登录冷却；多商家体验待交付 |
| [delivery-inspector-confirm-facts-null.md](delivery-inspector-confirm-facts-null.md) | 完成 | 确认事实 impact-preview null.length 兜底；≠R2 |
| [merchant-mp-c-price-display-workbench.md](merchant-mp-c-price-display-workbench.md) | 完成 | 工作台 Agent 确认展示 graph.image_generation；≠R5 |
| [delivery-merchant-image-loop-gate.md](delivery-merchant-image-loop-gate.md) | 完成 | 商家成图闭环本门 PASS（mock）；≠R2 |
| [merchant-mp-c-unknown-expiry-b0.md](merchant-mp-c-unknown-expiry-b0.md) | 完成 | unknown TTL 全额 Settle + Op resolve；≠R5 |
| [merchant-mp-c-price-display-b0.md](merchant-mp-c-price-display-b0.md) | 完成 | 图会话 Generate 前展示目录单价；≠R5/支付 |
| [merchant-mp-c-bootstrap-quota.md](merchant-mp-c-bootstrap-quota.md) | 完成 | QUOTA_TRIAL_UNITS 首次建账试用种子；≠R5/支付 |
| [image-quality-subject-compose-deliver.md](image-quality-subject-compose-deliver.md) | 完成 | compose Pass 合成 PNG 交付；≠R3/像素保真 |
| [roadmap-r2-r5-refresh.md](roadmap-r2-r5-refresh.md) | 完成 | ROADMAP R2/R5 事实对齐；两门仍未通过 |
| [image-quality-subject-preserve-honesty.md](image-quality-subject-preserve-honesty.md) | 完成 | 生成式出图不得宣称外观不变；≠R3 |
| [merchant-brand-product-select-b1.md](merchant-brand-product-select-b1.md) | 完成 | 商品选定 Brand + 继承合并；≠R2 |
| [merchant-mp-c-price-catalog-b0.md](merchant-mp-c-price-catalog-b0.md) | 完成 | 价格版本目录+Reserve 校验；≠R5/支付 |
| [canvas-brand-placeholder-web.md](canvas-brand-placeholder-web.md) | 完成 | Brand 占位文案对齐 B0；≠R2 |
| [image-quality-ocr-adoption-scope.md](image-quality-ocr-adoption-scope.md) | 完成 | 采用 OCR 只硬核期望字；≠R3 |
| [merchant-brand-entity-b0.md](merchant-brand-entity-b0.md) | 完成 | brands CRUD + 继承占位改写；≠R2/跨商 |
| [merchant-mp-c-wire-source-note.md](merchant-mp-c-wire-source-note.md) | 完成 | source-note generate Reserve；≠R5 |
| [merchant-mp-c-wire-localedit.md](merchant-mp-c-wire-localedit.md) | 完成 | localedit Edit 前 Reserve；≠R5 |
| [image-quality-subject-compose-b1.md](image-quality-subject-compose-b1.md) | 完成 | cutout+背景/阴影占位/安全区合成；≠R3 |
| [merchant-r5-close-ruling.md](merchant-r5-close-ruling.md) | 完成 | 总纲 R5 **未通过**；全入口/价格/unknown 策略缺口 |
| [delivery-r2-local-edit-retest.md](delivery-r2-local-edit-retest.md) | 完成 | §5.4 局部修图复验本门 PASS；≠R2 全过 |
| [merchant-mp-c-balance-http-b4.md](merchant-mp-c-balance-http-b4.md) | 完成 | 商家/Op 余额 HTTP + Adjust；≠R5/支付 |
| [image-quality-subject-extract-apply.md](image-quality-subject-extract-apply.md) | 完成 | image_generation 自动 Apply 主体提取；≠R3 |
| [delivery-export-overlay-fix.md](delivery-export-overlay-fix.md) | 完成 | 成果态收起工具条；UI 真实点击导出；≠R2 |
| [image-quality-subject-extract-b0.md](image-quality-subject-extract-b0.md) | 完成 | corner_chroma_local 蒙版/抠图闸；≠R3/像素保真 |
| [merchant-mp-c-wire-b3-agent.md](merchant-mp-c-wire-b3-agent.md) | 完成 | Agent before_model_request 额度接线；≠R5/HTTP 余额 |
| [image-quality-ocr-adoption-wire.md](image-quality-ocr-adoption-wire.md) | 完成 | CreateAdoption 默认 OCR；缺字/空期望拒绝；≠R3 |
| [merchant-mp-c-wire-b2-graph.md](merchant-mp-c-wire-b2-graph.md) | 完成 | Graph image_generation 额度接线；≠R5/Agent |
| [image-quality-ocr-trace-b0.md](image-quality-ocr-trace-b0.md) | 完成 | 字形 OCR 对照+失败不得 text_qualified；≠R3/采用默认接线 |
| [delivery-r2-core-path-gate.md](delivery-r2-core-path-gate.md) | 完成 | 本门 PASS；R2 仍未通过；Brand/批跑等缺口（§5.4/导出叠层已另归档） |
| [merchant-mp-c-wire-b1.md](merchant-mp-c-wire-b1.md) | 完成 | 图会话 Generate 额度接线；≠R5/Graph/Agent |
| [image-quality-selling-point-live.md](image-quality-selling-point-live.md) | 完成 | 炸锅 selling_point live k=1；闸门改善保真平；≠R3 |
| [merchant-mp-c-quota-b0.md](merchant-mp-c-quota-b0.md) | 完成 | MP-C B0 预留/结算骨架；入口未接线；≠R5 |
| [image-quality-selling-point-fidelity.md](image-quality-selling-point-fidelity.md) | 完成 | 一理由+本体证据；拼装只发一句；无 live；≠R3 |
| [image-quality-content-pilot.md](image-quality-content-pilot.md) | 完成 | 两商品 16/16 对照；闸门 2/8；卖点保真退步单列；≠R3 |
| [compete-adoption-hard-gate.md](compete-adoption-hard-gate.md) | 完成 | CreateAdoption 强制 text/route_qualified；无元数据禁 pass；≠R3 |
| [merchant-r1-close-ruling.md](merchant-r1-close-ruling.md) | 完成 | 总纲 R1 通过；夹具双商；CreateMerchant 仍 409；≠MP-C/D |
| [release-r6-close-ruling.md](release-r6-close-ruling.md) | 完成 | 总纲 R6 通过；残余非宣称已列；≠R1–R5/SLA |
| [release-r6-resource-budget.md](release-r6-resource-budget.md) | 完成 | 空闲/轻负载足迹+对应表；非 SLA；本门≠自动关 R6 |
| [release-r6-pin-and-formal-d4.md](release-r6-pin-and-formal-d4.md) | 完成 | pin `0.0.0-67b0f3092158`+正式 D4 PASS；资源预算未测；R6 未通过 |
| [release-r6-clean-candidate-gate.md](release-r6-clean-candidate-gate.md) | 完成 | G-07 干净候选 PASS；正式 D4/pin 仍缺；R6 未通过 |
| [release-r6-readiness-gate.md](release-r6-readiness-gate.md) | 完成 | B6 汇总；R6 未通过（G-07 FAIL）；诚实 FAIL |
| [workbench-remember-last-view.md](workbench-remember-last-view.md) | 完成 | 商品级主视图偏好；无偏好仍条件默认；≠全局成果 |
| [merchant-isolation-gate.md](merchant-isolation-gate.md) | 完成 | B10/MP-B 自动化门通过；≠开放第二商/R1 |
| [merchant-ops-surface.md](merchant-ops-surface.md) | 完成 | B9 Op settings/启停/A8草案；≠MP-B/MP-D |
| [merchant-queue-frontend.md](merchant-queue-frontend.md) | 完成 | B8 队列 merchant 快照+前端切换边界；≠MP-B |
| [merchant-agent-tools.md](merchant-agent-tools.md) | 完成 | B7 Agent 全链商家字段+25工具 harness；≠MP-B |
| [compete-facts-brand-inherit.md](compete-facts-brand-inherit.md) | 完成 | CF-B5 四级风格链+清身份；Brand 占位；≠R3 |
| [compete-facts-controlled-layout.md](compete-facts-controlled-layout.md) | 完成 | CF-B4 自研 layout 组合器+选型备忘；≠HTTP/编辑器/R3 |
| [compete-facts-produce-route.md](compete-facts-produce-route.md) | 完成 | CF-B3 produce_route+禁令审计；≠主体提取/采用硬闸/R3 |
| [merchant-delivery-localedit.md](merchant-delivery-localedit.md) | 完成 | B6 交付/局部编辑跨商404；≠MP-B |
| [workbench-conditional-results-default.md](workbench-conditional-results-default.md) | 完成 | 有产出默认results否则flow；≠全局默认/记忆 |
| [compete-facts-impact-preview.md](compete-facts-impact-preview.md) | 完成 | CF-B2 impact-preview+多选采用；≠OCR/自动跑图/R3 |
| [release-n-to-n1-upgrade.md](release-n-to-n1-upgrade.md) | 完成 | B5 upgrade脚本+等价夹具；≠R6/冻结稳定对 |
| [merchant-library-binding.md](merchant-library-binding.md) | 完成 | B4 图库绑定跨商404零写入；≠MP-B |
| [merchant-image-session.md](merchant-image-session.md) | 完成 | B5 会话生图跨商404；≠MP-B |
| [workbench-default-entry-walkthrough.md](workbench-default-entry-walkthrough.md) | 完成 | 建议暂不全局默认成果；采用/视觉共享API缺口已记 |
| [merchant-graph-recipe.md](merchant-graph-recipe.md) | 完成 | B3 Graph/SSE/recipe 跨商404；≠MP-B |
| [release-d3-restore-drill.md](release-d3-restore-drill.md) | 完成 | B4 隔离全栈 D3；登录/媒体/Agent/CHECKSUMS；在途 UNKNOWN；≠R6 |
| [compete-facts-text-trace.md](compete-facts-text-trace.md) | 完成 | CF-B1 text_trace+卖点一图一理由；≠OCR/采用硬闸/R3 |
| [brand-visual-reuse.md](brand-visual-reuse.md) | 完成 | 视觉方案版本选择+IQ-CF-07 继承预览；Brand 占位；≠跨商分享 |
| [merchant-product-chain.md](merchant-product-chain.md) | 完成 | B2 商品子链隔离+跨商404；≠MP-B |
| [merchant-root-ownership.md](merchant-root-ownership.md) | 完成 | B1 根表 merchant_id+过滤+跨商404；≠MP-B |
| [release-backup-restore.md](release-backup-restore.md) | 完成 | B3 备份/恢复脚本+runbook；隔离数据面冒烟；≠D3全栈/R6 |
| [delivery-adoption-snapshot.md](delivery-adoption-snapshot.md) | 完成 | 不可变采用版本+成果视图接线；Go/Vitest 通过；≠R2/浏览器整链 |
| [release-versioned-artifact.md](release-versioned-artifact.md) | 完成 | 不可变 tag + release/ 包 + pack 脚本；隔离探活；≠全量 build/R6 |
| [compete-facts-layer-gate.md](compete-facts-layer-gate.md) | 完成 | layer/确认门/营销闸 + 资料分栏；IQ-CF-01/CF-B0；≠R3 |
| [merchant-identity-skeleton.md](merchant-identity-skeleton.md) | 完成 | User/Merchant/Membership/邀请/可撤销会话；bootstrap；MP-A；≠业务表隔离 |
| [delivery-workbench-projection.md](delivery-workbench-projection.md) | 完成 | 成果/流程切换；全量生图投影；默认仍流程；Vitest 16；≠采用快照 |
| [compete-facts-layout-contract.md](compete-facts-layout-contract.md) | 完成 | IQ-CF-01…08、CF-B0…B5；与采用快照 IQ-CF-08 交接；≠R3 |
| [release-compose-proxy-overlay.md](release-compose-proxy-overlay.md) | 完成 | nginx→go-api；prod-ports overlay；隔离四项探活通过；≠R6/全量 build |
| [merchant-isolation-contract.md](merchant-isolation-contract.md) | 完成 | 归并 69 / 展开 214 路由等；B0–B10；无未裁定边界；隔离未实现 |
| [release-readiness-baseline.md](release-readiness-baseline.md) | 完成 | 只读冻结 Compose 发行/恢复差距、D1–D4 演练与 B1–B6；nginx 上游名错误已核实；R6 未通过 |
| [eval-graph-observation-authority.md](eval-graph-observation-authority.md) | 完成 | L1 graph-editing 写入走隔离 testdb Go 宿主；非法配置与多步 apply 由生产合同拒绝。catalog/intake 仍为 fixtures，未跑付费 L1 |
| [eval-node-snapshot-sync.md](eval-node-snapshot-sync.md) | 完成 | 按已提交节点合同刷新 Catalog/intake 观察；无 update 模式 Go/PG 一致性与 Node 消费回归通过。图操作真实语义仍由原任务处理 |
| [image-quality-content-candidate.md](image-quality-content-candidate.md) | 完成 | 单张卖点聚焦、图种内容分工、策划标签与成稿分离；生成策略候选通过相关回归，实图改善待固定新批次 |
| [image-eval-product-input.md](image-eval-product-input.md) | 完成 | 复用商品 AI 表单、冻结 source_note 与参考身份；整批预检、请求/报告链路及失败不重试回归通过，未新增真实质量分数 |
| [node-detail-contract-completion.md](node-detail-contract-completion.md) | 完成 | 六类详情合同、章节候选、能力选项、图片历史与独立导出；非法配置拒绝运行；Go/PG、Web 663、隔离浏览器 14 passed，评测快照另单刷新 |
| [image-pool-disjoint.md](image-pool-disjoint.md) | 完成 | 参考/金标按下载内容隔离，排除 5 个旧污染 SKU；有效池 195，局部回归通过，正式 live 未完成 |
| [node-detail-redesign.md](node-detail-redesign.md) | 完成 | 六类侧栏、方案文字与单图继承合同；角色化 prompt；PG、Web 655、隔离浏览器 19 passed，实图质量未评测 |
| [eval-unobservable-trial-boundary.md](eval-unobservable-trial-boundary.md) | 完成 | 未知工具结果显式不可测，按原始 trial 拒绝整批开发导出；115 passed / 5 skipped，结构观察与有效基线仍待补齐 |
| [eval-clarification-read-obligations.md](eval-clarification-read-obligations.md) | 完成 | 五题去除无用必需读取，复查 20 道澄清题；安全提问与写入前置事实回归，297 passed / 6 skipped |
| [eval-restart-batch-expectation.md](eval-restart-batch-expectation.md) | 完成 | ACK 丢失恢复测试固定上下文返回窗口并精确比较已提交批次；消除调度相关假失败，不改运行时 |
| [eval-graph-clear-intent.md](eval-graph-clear-intent.md) | 完成 | 三种删节点问法明确保留分组；120 种删除排列及额外写入拒绝回归；新身份须完整重采开发基线 |
| [canvas-local-edit-flow.md](canvas-local-edit-flow.md) | 完成 | mock 绑定下检查器局部编辑可提交、保留谱系、采用/撤销；失败不覆盖节点当前图；隔离 Chromium 2 passed |
| [perf-imagesession-active-status.md](perf-imagesession-active-status.md) | 完成 | 活动 Status/SSE 保持全量 queued/running；省略 prompt 后 300 条固定夹具 584,030B / p95 18.16ms，查询次数恒为 8 |
| [perf-imagesession-recovery-visible.md](perf-imagesession-recovery-visible.md) | 完成 | 连续生图故障可见状态：取消 ctx 仍落 unknown，心跳未过期不恢复，晚到 writer 拒绝；崩溃等待默认 90 分钟 |
| [canvas-full-recipe-entry.md](canvas-full-recipe-entry.md) | 完成 | 创建页完整配方预览/取消/事务确认与丢响应重试；PG 59、Web 650、浏览器 7 passed，24 组语言主题布局 |
| [eval-library-observation-refresh.md](eval-library-observation-refresh.md) | 完成 | Go 素材快照与 L1/L3 观察对齐，修正标签错题，12 条输入阻塞解除；独立审核与哈希一致，开发基线开放待采证 |
| [canvas-asset-recipe-proof.md](canvas-asset-recipe-proof.md) | 完成 | 固定资产、拖入参考、片段确认及完整配方拒绝覆盖通过；actions 78 passed；无图商品入口有效 FAIL 交 canvas-full-recipe-entry |
| [agent-library-read-contract.md](agent-library-read-contract.md) | 完成 | 素材真实 before/revision、目录分页、归档读取和工作流关联观察；六类确认及错误事实回归通过，评测冻结另单独立验收 |
| [canvas-delivery-proof.md](canvas-delivery-proof.md) | 完成 | 浏览器原图/交付图下载与 ZIP 谱系/hash，失败反馈，2 passed；Go delivery 24 tests passed |
| [eval-observable-input-contract.md](eval-observable-input-contract.md) | 完成 | 75 条 L1 逐题审核，成功/未知、真实状态、授权时序与攻击行为评分修复；素材读取缺口仍阻塞完整能力测量与自进化基线 |
| [canvas-run-recovery-proof.md](canvas-run-recovery-proof.md) | 完成 | 隔离 mock 浏览器四条通过：场景选点、修复后重试、只重试失败、保存失败阻止运行 |
| [canvas-workflow-coverage.md](canvas-workflow-coverage.md) | 完成 | 完整操作链核查与 274 条前端回归通过；场景/重试、交付图、资产/配方浏览器证据另单补齐 |
| [perf-imagesession-http-load.md](perf-imagesession-http-load.md) | 完成 | 隔离 25k 会话 / 10k 轮次 / 1k 任务，七条真实 HTTP 路径各 100 样本；详情 258KB、p95 16.33ms，生产并发仍未验 |
| [agent-question-answer-identity.md](agent-question-answer-identity.md) | 完成 | Node/PG 按问题绑定答案、并发幂等与第二问 SIGKILL 恢复通过；固定 live 只问一问，缺上下文读取而 FAIL |
| [eval-contract-alignment.md](eval-contract-alignment.md) | 完成 | 按生产意图路由校正 8 道失真题并冻结 L1 hash `406dc178…`；旧 `71d48f47…` 不可跨题集比较 |
| [eval-user-sim.md](eval-user-sim.md) | 完成 | 独立用户模型、answer/resume 接线与跨 turn 观测；真实五条 2/5 pass，第二问题答案冲突交独立生产修复单 |
| [eval-l2-provenance.md](eval-l2-provenance.md) | 完成 | L2 内容快照、工作树与实际 SDK 请求身份归因；缺样本/漂移不可 complete，未补写历史或重跑真实批次 |
| [eval-l2-terminal-observation.md](eval-l2-terminal-observation.md) | 完成 | L2 等待 PG 终态后评分；观察失败单列并保留双侧状态，重复/race 回归通过，未重跑旧批次 |
| [harness-container-validation.md](harness-container-validation.md) | 完成 | 固定提交原 Dockerfile 完整构建；断网 UID 10001、服务启停和新轨迹卷验证通过，构建临时固定 registry IPv4 |
| [eval-collection-isolation.md](eval-collection-isolation.md) | 完成 | 场景清单可哈希，L1 单集合运行与只含开发材料的导出边界通过；没有真实隐藏/独立验收集证据 |
| [harness-traces.md](harness-traces.md) | 完成 | 默认关闭的结构化轨迹；独立有界队列、敏感内容排除与写盘失败不影响业务的回归通过，未采生产样本 |
| [harness-attribution.md](harness-attribution.md) | 完成 | 同一冻结 hash 贯穿 Pi、checkpoint、invocation、健康与评测；新请求严格验证，历史 NULL 不回填 |
| [harness-artifact.md](harness-artifact.md) | 完成 | 可哈希冻结指令工件由现有 Pi 加载；默认测试、构建与无网络容器加载通过，归因与进化待后续阶段 |
| [perf-dispatcher-latency.md](perf-dispatcher-latency.md) | 完成 | 三轮 500 条真实投递采证；单副本 p95 2.54–2.59s 未达建议目标，双副本 0.77–0.78s；无重复信封 |
| [perf-imagesession-detail.md](perf-imagesession-detail.md) | 完成 | 详情三组任务各 LIMIT 20；队列位置只返回所需 ID，包内测试与目标规模 query-plan 通过 |
| [eval-live-layers.md](eval-live-layers.md) | 取消 | L2、L5、生产 mine 已拆分为三个独立 issue；保留历史 FAIL 与 dev mine 记录，未宣称完成 |
| [eval-adversarial-live.md](eval-adversarial-live.md) | 完成 | 244 条攻击矩阵 ASR=0、效用未下降；D-07/L5-04 通过。L5-05 与 D-08 仍缺 |
| [arch-journal-assessment.md](arch-journal-assessment.md) | 完成 | AR-02 调查结论为保留现状；在线发布与重启确认收拢不能实质减少 TurnRuntime 协议知识，不发实现单 |
| [eval-go-loader.md](eval-go-loader.md) | 完成 | Go loader extra=forbid；共享非法 fixture 覆盖多余字段、未知 enum、重复 id、缺失 world、L2 层级不匹配 |
| [perf-capacity-metrics.md](perf-capacity-metrics.md) | 完成 | `/metrics` 增加 admission running 与 denied；容量满路径按 graph/imagesession 计数 |
| [perf-dispatcher-backlog.md](perf-dispatcher-backlog.md) | 完成 | watch 满批后续投；单副本 500 条 PENDING→SENT p95 0.438s |
| [perf-graph-adopt-concurrent.md](perf-graph-adopt-concurrent.md) | 完成 | 自动采用与 cancel/recovery/mutate 并发 `-count=20` 无死锁；succeeded 则三文稿 generated+ready |
| [perf-imagesession-enqueue-admission.md](perf-imagesession-enqueue-admission.md) | 完成 | 连续生图入队不再持 capacity advisory 或误计 denied；claim 满容量才 later |
| [canvas-inspector-midrun.md](canvas-inspector-midrun.md) | 完成 | 运行中检查器保存与 AR-01 基线贯通；mock 浏览器门 3 passed |
| [canvas-graph-run-undo.md](canvas-graph-run-undo.md) | 完成 | 整图跑中途 HTTP undo 具名钉死；live 回到撤销后文稿 |
| [canvas-c4-remainder.md](canvas-c4-remainder.md) | 完成 | 浏览器整图跑中途撤销与文稿 409 停止；mock 门 5 passed |
| [canvas-c4-run-controls.md](canvas-c4-run-controls.md) | 完成 | 检查器运行该节点、运行到这里与运行中取消；mock 门 8 passed |
