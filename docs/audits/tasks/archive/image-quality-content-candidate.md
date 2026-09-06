# 任务：修正卖点表达与图种内容分工并冻结质量候选

状态：完成
类型：实现
认领者：主代理-image-quality-0906-0132
认领于：2026-09-06T01:42:06+08:00
业务组：图片质量
父账本：image-quality.md
完成后可拆：固定商品输入的原版/候选/直调/商家原图比较

## 问题来源

原始诊断见 [质量对照](../image-quality-comparison.md)：卖点图对直调 2 胜 4 负；空气炸锅文稿把“卖点1/卖点2”送入最终图片；洁面和空气炸锅多个图位重复摆拍。耳机场景有三只耳机，提示方案未分清佩戴、手持与仓内状态。全局系列风格是否是主因尚未得到控制实验支持。

## 做成什么样

在现有生成合同内修正具体提示决策：卖点围绕有商品证据的一个核心购买理由，文案可直接面向买家；场景有实际使用关系，细节展示可见结构/材质，整组内容分工不依赖换背景实现。复用已有字段和生产 prompt，不新增系列表、节点或持久化任务数组。候选提交只表示实现和确定性检查完成，实图改善必须另有固定 live 对照。

## 前置与并行

- 前置：商品输入冻结实现 `61280364`；旧金标、直调和评委保持冻结。
- 独占 `go/prompts/providers/`、`go/prompts/listing/` 中必要生成文案及 `go/prompts/prompts_test.go`；允许只读追踪 graph/provider 装配以找到真实决策点。
- 当前没有其它任务占用上述目录。Agent Skill 任务持有 `agent-service/.pi/skills/`，不修改其冻结输入。
- 无共享运行服务或模型调用；先交付可复核候选及新试验合同，新增付费运行尚未开始。

## 只改这些文件

- 真实因果所需的 `go/prompts/` 生成文案与现有相关测试。
- 本任务、看板、归档和本组文档。

## 合同与验证

- 不改 judge、naive、金标、质量门槛或既有 source_note 事实输入。
- 不编造未确认的功效、参数或内部结构；全套要求不变成每图重复文案。
- 检查请求到已编译生图提示的读取路径，并运行受影响 prompt/provider/graph 回归与静态检查。
- 明确哪些属于提示策略候选；确定性检查不证明图片质量提升。
- 最终 diff 自审、`just docs-check`、提交；新实测范围与费用边界另行固定。

## 阻塞与交接

- 原因：无实现阻塞。
- 跟进者：主代理-image-quality-0906-0132。
- 交接：本单不重跑已有 unknown 调用，不覆盖原始证据。

## 证据

- 确认决策冲突：`providers/prompt-generation.md` 与 `listing/image-types.md` 要求单张卖点图包含 3–5 条利益，`listing/compile-image.md` 则要求一页一个钩子。已统一为一个购买理由、最多两条同主题事实标注，取消无事实依据时仍要信任底栏的默认要求。
- `creative-brief.md` 复用 key_messages 按图种分配买家问题和可见证据；必需内容与套图覆盖信息分开。画面方案只选择本图对应消息，生成文字与策划标签分开。场景交代使用关系和部件状态；细节选择可确认部位。
- 读取链路：`graph.AssemblePromptRequest` 加载图种职责 → `providers.GeneratePrompt` 使用固定 instructions → `graph.CompileImageModelPrompt` 拼接已确认方案及图种默认指令。未新增字段、节点、自动采用、重试或持久化行为，未修改系列风格节点。
- `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./prompts ./internal/providers -count=1 -p 1'` 通过；graph 的 CompileImageModelPrompt、ImageInstructions、LocalSupplement、AssemblePrompt、SeedPrompt 相关 11 条回归通过。`go vet` 对 prompts/providers/graph 通过。没有把字符串检查当作模型服从度证据。
- 后续两商品试验材料已复制到 `storage-dev/image-quality-content-pilot-0906/`，两臂共用原校正参考/金标；10 张源图大小和 SHA-256 均匹配，无参考/金标重叠。试验文件 SHA-256 `9a923d536accdf8405a93849b451ed936ae24ab4cd47e50623f168ca112b9e76`；新增运行见 [内容策略试验](../image-quality-content-pilot.md)。
- 审核者：主代理-image-quality-0906-0132，自审。移除的是默认冲突，用户已指定的精确文案与创作限制仍有优先权；完整任务 diff 与文档检查在提交前复核。
- Issue 结果：生成内容策略候选交付。组内实图改善未验证，原 32 图位诊断仍缺 8 个有效评分，多系列作用域与独立单图任务结构尚未实现。
- 交付定位：随本任务提交；试验原版使用该交付的父提交，候选使用该交付本身，保证生成策略是代码的唯一行为差异。
