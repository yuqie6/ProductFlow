# 发给模型的固定文案

改画风、图种任务、Provider 指令或 Agent 口吻时只改这里的 markdown。Go 用 `go:embed` 编译进去；agent-service 在构建期把 `agent/runtime-policy.md` 打进 `runtime-policy.generated.ts`。

不要把 JSON schema、图种 family 映射、seed 组装分支、工具 description 或 Skill 正文放进本目录。Skill 仍在 `agent-service/.pi/skills`。文/图生图模板是设置项 `prompt_image_chat_template`。

| 目录 | 消费者 |
|---|---|
| `agent/` | Go Agent 合同 `system_prompt`；`runtime-policy.md` 由 Pi adapter 拼进 static prompt |
| `listing/` | 图编译、seed 保真规则、图种目录 |
| `providers/` | prompt provider 的 Responses `instructions` |

分节文件用 `## name` 标题。整文件即 prompt 的 md 不要加标题，trim 后原文发给模型。缺节或空文件会让进程启动失败。
