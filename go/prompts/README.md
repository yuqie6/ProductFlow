# 发给模型的固定文案

改画风、图种任务、Provider 指令或 Agent 口吻时只改这里的 markdown。Go 用 `go:embed` 编译进去；agent-service 在构建期把 `agent/runtime-policy.md` 打进 `runtime-policy.generated.ts`。

不要把 JSON schema、图种 family 映射、seed 组装分支、工具 description 或 Skill 正文放进本目录。Skill 仍在 `agent-service/.pi/skills`。文/图生图模板是设置项 `prompt_image_chat_template`。

| 目录 | 消费者 |
|---|---|
| `agent/` | Go Agent 合同 `system_prompt`；`runtime-policy.md` 由 Pi adapter 拼进 static prompt |
| `listing/` | 图编译、seed 保真规则、图种目录 |
| `providers/` | prompt provider 的 Responses `instructions` |

分节文件用 `## name` 标题。整文件即 prompt 的 md 不要加标题，trim 后原文发给模型。缺节或空文件会让进程启动失败。

## 节点职责与设计依据

商品资料提取只记录可观察内容，待确认项与事实分开。创作要求拥有商业目标、必需内容和明确禁用要求；系列风格拥有风格与配色；画面方案拥有单图构图、内容和文字；最终生图编译器接收已经合并的单图输入。参考图片节点不自动改写或生成证据。

2026-09-05 的指令调整参考公开的一手文档，不引用未公开的产品 system prompt：

- [Adobe Firefly composition reference](https://helpx.adobe.com/firefly/web/work-with-images/generate-images/match-image-composition-to-reference-image.html)：结构参考与风格参考分工。本项目用 reference role 区分商品、环境、风格与证据，不把风格图当商品事实。
- [Adobe Firefly style reference](https://developer.adobe.com/firefly-services/docs/firefly-api/guides/concepts/style-image-reference/)：跨图片共享视觉语言。本项目将系列风格与单图场景分开，不强制全套同一背景。
- [FLORA text-to-image](https://docs.flora.ai/nodes/image-node/text-to-image)：连接文字输入和显式提示词改进。本项目保留可编辑文稿及候选采用，不在出图时暗改共享内容。
- [Google image prompting tips](https://blog.google/products-and-platforms/products/gemini/image-generation-prompting-tips/)：明确主体、构图、环境、风格和编辑目标。本项目要求字段分工、保留用户指定文字、区分新增文字与商品原有标签。

这些来源支持设计取舍，不证明本项目的实图质量。确定性测试验证字段与指令传递；真实模型质量仍需独立、授权的图片评测。
