# Go 业务后端

默认运行时是 `just go-api` / `just go-worker` / `just go-dispatcher`。schema 用 `just go-migrate`（GORM `CreateTable`/`AddColumn` + ExtraDDL，不使用 AutoMigrate）。竖切与当前形状见 [`docs/ARCHITECTURE.md`](../docs/ARCHITECTURE.md)。退休的 FastAPI 树在 `retired/python`。

```bash
just go-test
just go-migrate
just go-api
just go-worker
just go-dispatcher
```

`GET /healthz` 返回 `{"status":"ok"}`。`GET /healthz/ready` 额外 ping PostgreSQL，不是现有 Web 合同。

Session cookie 名是 `session`，签名用 Go cookie store。

`STORAGE_ROOT` 相对路径相对**仓库根**解析（`just go-api` 使用 `go run -C go`，cwd 留在仓库根；Go `config.Load` 也会按 `go.mod` 所在 `go/` 的上一级收绝对路径）。本地默认 `./storage-dev`。Compose 里是 `/app/storage`。

JSON 日志写滚动文件，终端默认是可读行。默认目录是 `STORAGE_ROOT/logs`（本地即 `storage-dev/logs/`）：`productflow-api.log`、`productflow-worker.log`、`productflow-dispatcher.log`。可用 `LOG_DIR` 改路径；`LOG_FORMAT=json` 让 stderr 也输出 JSON；`LOG_MAX_BYTES` / `LOG_BACKUP_COUNT` / `LOG_RETENTION_DAYS` 控制滚动与按天清理。空闲 dispatcher 周期、`/healthz` 和 Agent heartbeat 只进文件（Debug），终端默认不刷。

HTTP 只写业务行和 `async_dispatches` PENDING，不在请求里打 broker。dispatcher 先标 SENT 再 asynq 投递；worker `MaxRetry=0`。无法证明的供应商结果标 `unknown`，不自动当失败重试。

Compose 默认启动三个 Go 进程，占用 `APP_HOST_PORT`（默认 29280）。Go dispatcher 是唯一 durable scanner。

## 配方列表排障

`GET /api/v3/workflow-recipes` 只返回当前版本通过内容、节点合同及 hash 校验的配方；失效项不阻断其他配方，全部失效时返回 `[]`。`include_archived=true` 仍执行上述校验。过滤不删除、归档或改写数据库记录；API 终端日志中的 `recipe list excluded invalid current version` 包含配方 ID 和错误。失效配方的详情、预览与应用仍严格拒绝，不支持旧配置自动升级。

## 分类图片标注

`image-annotation.v1` 对已有图片做多模态标注。`reference` 评价真实商品图；`comparison` 使用显式绑定的候选与同商品、同图种质量对照。类别与图种共同决定展示重点，报告包含四维评分、优缺点、资产依据、不确定项和待验证建议。模型标注仍需抽查，原池中的分类与身份不是已核实的事实真值。历史 `image-evals-run` 的生成对照门保持独立。

在仓库根目录运行，输入和输出路径建议使用绝对路径：

```bash
just image-evals-prepare-annotations /tmp/productflow-reference-selection.json 8 1
just image-evals-annotate /tmp/productflow-reference-selection.json /tmp/productflow-reference-report
```

准备命令默认读取 `STORAGE_ROOT/image-evals/pool`，不调用模型。第四个可选位置参数为其他池路径；第五个为图种轮换列表，如 `hero,selling_point,scene,detail`，每个抽样商品按列表取一个图种。省略轮换列表时选择样本现有的生成图种。选择文件记录商品/图种、身份参考和真实图路径、SHA256 与来源；旧选择文件不能覆盖。

`annotate` 只调用评委，无需 API、worker 或生图供应商。上面的 just 命令显式开启 `PRODUCTFLOW_RUN_IMAGE_EVALS=1`；直接调用 Go CLI 时须自行设置。模型配置优先使用 `IMAGE_EVAL_JUDGE_API_KEY`、`IMAGE_EVAL_JUDGE_MODEL`、`IMAGE_EVAL_JUDGE_BASE_URL`；未提供完整 key/model 时从开发配置连接数据库，读取现有 prompt 绑定。密钥仅放环境，不写入选择或报告。

候选比较须复制为新的选择文件，设置 `mode=comparison`，在每个 case 的 `candidates` 明确填写候选资产身份、绝对路径、SHA256、尺寸、字节数、`image_type`、`variant`、`role=evaluated_result`、`source=external`，并保留身份参考和对应的 `quality_references`。不得按历史数组顺序猜测候选归属。缺候选会拒绝输入，缺质量对照单列不可比较；不把缺失变为通过或零分。

每次指定一个全新报告目录。输出 `report.json` 与 `report.md`，按类别、图种与候选分组保留分母、状态和分维度差值。未知、失败、不可比较和严重事实问题不会被解释为达标。报告保留模型、提示词、选择文件和图片指纹、代码版本与时间；不更新旧评测的 latest 指针，不覆盖原池或历史结果。

模型文本的 JSON/结构校验或输入证据校验失败时，`report.json` 的记录保留 `diagnostic.stage`、`reason` 与 `output_text`。结构错误归为 `invalid_output`，不会误记成供应商故障；无效结果仍无评分且不可比较。诊断只保存成功响应中提取的模型文本，不保存 HTTP 错误体、请求头或凭据；不能恢复旧报告已丢失的原文。

CLI 成功退出表示报告已写入。自动化读取报告的记录状态、覆盖率、关键问题和比较结论判断后续动作；不能把退出码 0 或 `complete` 标注状态当作图片质量通过。
