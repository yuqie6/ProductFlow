# 旧工作流冻结与切换维护手册

本文用于 schema-v1 工作流退出生产环境时的演练和维护窗口。当前候选源码已经移除在线 v1 编辑、执行、模板和
`text` binding 路径，并保留冻结、只读预检、归档导出、回填和持久化证据闸门。旧源表与归档表仍然保留；源码状态不能替代
具体部署的备份恢复、数据对账和真实运行验证。

## 1. 操作约束

- 操作对象必须是经过恢复验证的生产只读副本，或已批准维护窗口内的生产实例。
- 所有命令使用同一候选版本和同一组部署环境变量。不要在终端输出、报告或工单中展开 `.env`。
- 冻结命令返回后，旧画布仍可读取；旧商品创建、默认 DAG 创建、节点和连线修改、旧图片和文案修改、模板写入与应用、
  新运行、重试和取消请求均返回 HTTP `409`。
- 冻结不会取消已有运行，也不会阻止 worker 排空已经持久化的 v1 run。active 或 unknown 状态清零前保持 worker 运行。
- active/unknown run、未知 schema profile、引用对账失败、provider 配置失败、备份不可恢复或 archive hash 漂移均为停止条件。
- 不允许通过手工 SQL 修改 run 状态、删除异常 workflow、`alembic stamp` 或跳过失败记录来获得绿色报告。

## 2. 证据目录

在不会被 Git 跟踪且空间充足的受限目录中保存证据：

```bash
set -o pipefail
export CUTOVER_ID="$(date -u +%Y%m%dT%H%M%SZ)"
export EVIDENCE_DIR="/var/lib/productflow-cutover/$CUTOVER_ID"
install -d -m 0700 "$EVIDENCE_DIR"
git rev-parse HEAD | tee "$EVIDENCE_DIR/release-commit.txt"
docker compose ps > "$EVIDENCE_DIR/compose-ps.txt"
docker compose exec -T productflow-backend alembic current > "$EVIDENCE_DIR/alembic-current.txt"
curl --fail --silent http://127.0.0.1:${APP_HOST_PORT:-29280}/healthz \
  > "$EVIDENCE_DIR/backend-health.json"
```

配置预检报告可以保存哈希和配置来源，不得保存数据库 URL、session cookie、API key 或旧系统提示词明文。archive page
包含用户历史文案和节点配置，只能保存在该受限证据目录中，不能附到公开工单或日志。

下文应用维护命令都在候选 `productflow-backend` 容器内执行。这样命令使用 Compose 网络中的数据库地址和容器内
`STORAGE_ROOT=/app/storage`。若操作隔离恢复项目，应从该项目的 Compose 目录执行，或为每条 `docker compose` 命令增加
对应的 `--project-name`/`--file` 参数；禁止让命令误连正式生产项目。

## 3. 维护窗口前恢复演练

使用最近一次生产备份创建隔离数据库和隔离 storage。恢复目标不能与生产数据库、生产 storage 或生产 Redis 共用写路径。

数据库备份与目录校验示例：

```bash
docker compose exec -T productflow-postgres \
  pg_dump -U productflow -d productflow --format=custom \
  > "$EVIDENCE_DIR/preflight-database.dump"
docker compose exec -T productflow-postgres pg_restore --list \
  < "$EVIDENCE_DIR/preflight-database.dump" \
  > "$EVIDENCE_DIR/preflight-database.list"
sha256sum "$EVIDENCE_DIR/preflight-database.dump" \
  > "$EVIDENCE_DIR/preflight-database.dump.sha256"
```

Compose storage 备份示例：

```bash
docker compose exec -T productflow-backend tar -C /app/storage -cf - . \
  > "$EVIDENCE_DIR/preflight-storage.tar"
tar -tf "$EVIDENCE_DIR/preflight-storage.tar" > "$EVIDENCE_DIR/preflight-storage.list"
sha256sum "$EVIDENCE_DIR/preflight-storage.tar" \
  > "$EVIDENCE_DIR/preflight-storage.tar.sha256"
```

在隔离目标执行 `pg_restore` 和 storage 解包后，从该隔离 Compose 项目运行：

```bash
docker compose exec -T productflow-backend \
  python -m productflow_backend.commands.audit_legacy_retirement --compact \
  > "$EVIDENCE_DIR/restored-source-audit.json"
docker compose exec -T productflow-backend \
  python -m productflow_backend.commands.preflight_legacy_cutover --compact \
  > "$EVIDENCE_DIR/restored-cutover-preflight.json"
```

恢复演练允许因为“尚未冻结”或 active run 返回退出码 `2`；schema profile、表数量、媒体数量、缺失引用和 provider 指纹必须可读。
记录恢复开始时间、结束时间、数据库大小、storage 大小和所有 blocking issue。恢复过程未经实际执行时，不得填写“备份可恢复”。

## 4. 启用冻结

若线上当前仍运行包含 v1 写路径的过渡版本，维护窗口开始时保留 backend 和 worker 服务，执行：

```bash
docker compose exec -T productflow-backend \
  python -m productflow_backend.commands.manage_legacy_v1_freeze status \
  | tee "$EVIDENCE_DIR/freeze-before.json"
docker compose exec -T productflow-backend \
  python -m productflow_backend.commands.manage_legacy_v1_freeze \
  enable --confirm FREEZE_V1_WRITES \
  | tee "$EVIDENCE_DIR/freeze-enabled.json"
docker compose exec -T productflow-backend \
  python -m productflow_backend.commands.manage_legacy_v1_freeze status \
  | tee "$EVIDENCE_DIR/freeze-verified.json"
```

`freeze-verified.json` 必须同时满足 `configured=true`、`frozen=true`、`valid=true`。生产 PostgreSQL 使用事务级 advisory lock；
已经通过冻结检查的在线 mutation 会在 enable 提交前结束，之后到达检查点的 mutation 会被拒绝。已有持久化 run 的 worker 不走该锁，
仍可在冻结后写入执行终态，直到预检确认排空。

对一个已存在旧画布执行只读 GET，应返回 `200`。对旧节点修改和旧运行提交执行认证请求，应返回 `409`，且数据库行数、
Redis 队列和 provider 请求计数不发生变化。不要在证据中保存认证 cookie。

## 5. 等待运行排空并执行预检

执行脱敏预检：

```bash
docker compose exec -T productflow-backend \
  python -m productflow_backend.commands.preflight_legacy_cutover --compact \
  > "$EVIDENCE_DIR/cutover-preflight-draining.json"
```

退出码 `2` 表示存在 blocker。检查以下字段：

- `freeze.frozen` 和 `freeze.valid`；
- `execution.active_workflow_run_count`；
- `execution.active_node_run_count`；
- `execution.unknown_workflow_run_count`；
- `execution.unknown_node_run_count`；
- `execution.active_canvas_agent_run_count`；
- `execution.unknown_canvas_agent_run_count`；
- `execution.blocking_workflow_ids` 和 `execution.blocking_canvas_agent_thread_ids`；
- `provider_configuration.required_purposes` 和 `provider_configuration.missing_purposes`；
- `provider_configuration.bindings[*].valid` 和 `issue_codes`；
- `issues` 中的 blocking 项。

保持 worker 运行并重复预检，直到 active/unknown 计数和两个 blocking ID 列表全部为空。未知状态需要定位对应源记录和代码版本；
禁止把未知状态直接改成成功或失败。
活跃的 schema-v2 run 不计入 v1 drain blocker。旧 run 无法自然结束时停止维护窗口，在部署退役候选版本前解除冻结并通过过渡版本的正常应用命令处理；处理后重新冻结，
重新生成备份、source audit、archive 和 final preflight，旧的 final snapshot 不再有效。

## 6. 最终备份、归档与对账

运行清空后创建最终数据库和 storage 备份，重复第 3 节的可读取检查，并使用 `final-` 文件名前缀。随后生成固定 source audit：

```bash
docker compose exec -T productflow-backend \
  python -m productflow_backend.commands.audit_legacy_retirement --compact \
  > "$EVIDENCE_DIR/final-source-audit.json"
FINAL_AUDIT_EXIT=$?
docker compose exec -T productflow-backend \
  python -m productflow_backend.commands.preflight_legacy_cutover --compact \
  > "$EVIDENCE_DIR/final-cutover-preflight.json"
FINAL_PREFLIGHT_EXIT=$?
test "$FINAL_AUDIT_EXIT" -eq 0
test "$FINAL_PREFLIGHT_EXIT" -eq 0
```

`final-cutover-preflight.json` 必须满足 `ready_for_cutover=true`。保存其中的 `source_report_sha256`、`report_sha256` 和 provider 指纹。

分别导出 workflow、user template 和 Canvas Agent archive page。每页保存 JSON/CSV；存在 `next_cursor` 时使用该值继续，直到为空。
先创建容器临时目录：

```bash
export CONTAINER_CUTOVER_DIR="/tmp/productflow-cutover-$CUTOVER_ID"
docker compose exec -T productflow-backend \
  install -d -m 0700 "$CONTAINER_CUTOVER_DIR"
docker compose exec -T productflow-backend \
  python -m productflow_backend.commands.export_legacy_archives \
  --kind workflow \
  --output-json "$CONTAINER_CUTOVER_DIR/workflow-page-001.json" \
  --output-csv "$CONTAINER_CUTOVER_DIR/workflow-page-001.csv" \
  --compact >/dev/null
docker compose cp \
  "productflow-backend:$CONTAINER_CUTOVER_DIR/workflow-page-001.json" \
  "$EVIDENCE_DIR/workflow-page-001.json"
docker compose cp \
  "productflow-backend:$CONTAINER_CUTOVER_DIR/workflow-page-001.csv" \
  "$EVIDENCE_DIR/workflow-page-001.csv"
```

每页先执行目标映射 dry-run：

```bash
docker compose exec -T productflow-backend \
  python -m productflow_backend.commands.backfill_legacy_archives \
  --input "$CONTAINER_CUTOVER_DIR/workflow-page-001.json" \
  --expected-source-report-sha256 '<final source_report_sha256>' \
  --output-json "$CONTAINER_CUTOVER_DIR/workflow-page-001-plan.json" \
  --output-csv "$CONTAINER_CUTOVER_DIR/workflow-page-001-plan.csv" \
  --compact >/dev/null
docker compose cp \
  "productflow-backend:$CONTAINER_CUTOVER_DIR/workflow-page-001-plan.json" \
  "$EVIDENCE_DIR/workflow-page-001-plan.json"
docker compose cp \
  "productflow-backend:$CONTAINER_CUTOVER_DIR/workflow-page-001-plan.csv" \
  "$EVIDENCE_DIR/workflow-page-001-plan.csv"
```

所有 page 的 `global_blocking_issue_codes` 为空且 `blocked_count=0` 后，使用容器内完全相同的 page 和 source hash 增加 `--apply`。
重复执行相同 page 必须全部返回 `unchanged`。对账内容包括 source/archive 数量、每项 payload hash、节点/边/run/node-run 数量、
资产关系数量、missing/unavailable 诊断和报告 SHA-256。

三类 page 及其所有 cursor 完成并复制证据后，删除容器临时目录：

```bash
docker compose exec -T productflow-backend rm -rf "$CONTAINER_CUTOVER_DIR"
```

最终再次运行 source audit 和 cutover preflight。source 数据、provider 指纹或 archive payload 出现未解释漂移时停止切换。

对 canonical mapping 指向但仍为 `legacy_pending` 的媒体先执行只读核验。报告出现 missing、invalid 或 path error 时命令返回退出码 `2`，
必须定位 storage 恢复或路径映射问题：

```bash
docker compose exec -T productflow-backend \
  python -m productflow_backend.commands.verify_legacy_media --compact \
  > "$EVIDENCE_DIR/legacy-media-dry-run.json"
```

确认报告与最终 storage 备份属于同一快照后才允许写入核验元数据；写入后重复 dry-run，`scanned_count` 必须为 `0`：

```bash
docker compose exec -T productflow-backend \
  python -m productflow_backend.commands.verify_legacy_media --apply --compact \
  > "$EVIDENCE_DIR/legacy-media-applied.json"
docker compose exec -T productflow-backend \
  python -m productflow_backend.commands.verify_legacy_media --compact \
  > "$EVIDENCE_DIR/legacy-media-final.json"
```

媒体报告是 canonical 对账证据的一部分，不能单独替代商品、资产、节点、封面和 lineage 映射报告。

## 7. 持久化证据闸门与源码状态

当前候选源码已经移除在线 v1 代码和配置，迁移 `20260816_0042` 只新增 `legacy_cutover_gates` 证据表，不删除旧源表、
归档表或历史媒体。Alembic upgrade、应用启动和 provider bootstrap 都不得隐式清理历史数据。

完成最终 source audit、全部 archive page 回填、canonical 商品/图片对账和备份恢复演练后，才可批准证据闸门。以下哈希必须来自
同一候选 commit、同一冻结快照和受限证据目录，不得使用测试夹具、空库或人工拼接值：

```bash
docker compose exec -T productflow-backend \
  python -m productflow_backend.commands.manage_legacy_cutover_gate status \
  | tee "$EVIDENCE_DIR/cutover-gate-before.json"
docker compose exec -T productflow-backend \
  python -m productflow_backend.commands.manage_legacy_cutover_gate approve \
  --source-profile '<verified source profile>' \
  --source-report-sha256 '<final source report sha256>' \
  --archive-report-sha256 '<complete archive reconciliation sha256>' \
  --canonical-report-sha256 '<canonical product/image reconciliation sha256>' \
  --backup-restore-verified-at '<UTC ISO-8601 timestamp>' \
  | tee "$EVIDENCE_DIR/cutover-gate-approved.json"
docker compose exec -T productflow-backend \
  python -m productflow_backend.commands.manage_legacy_cutover_gate assert-cleanup-ready \
  | tee "$EVIDENCE_DIR/cutover-cleanup-ready.json"
```

源码合并、自动化测试通过或 `status` 能读取空闸门都不构成批准。真实 provider、PostgreSQL、Redis 和浏览器 E2E 未完成时，
rollout 状态继续保持“生产证据未完成”。当前版本没有 destructive cleanup 命令；未来删除旧表或历史媒体必须单独评审迁移、
保留策略和恢复方案，且只能在 `assert-cleanup-ready` 成功后执行。

## 8. 解除冻结与故障恢复

证据闸门尚未批准、退役候选版本尚未部署且线上过渡版本仍需要 v1 写路径时，可以解除冻结：

```bash
docker compose exec -T productflow-backend \
  python -m productflow_backend.commands.manage_legacy_v1_freeze \
  disable --confirm UNFREEZE_V1_WRITES \
  | tee "$EVIDENCE_DIR/freeze-disabled.json"
```

解除后重新生成 source audit；此前批准的 final snapshot 视为过期，下次切换必须重新冻结、备份、归档和对账。

退役候选版本已部署、首个正式 v2 写入已发生或证据闸门已批准后，灾难恢复使用最终数据库与 storage 备份，不允许通过解除冻结恢复旧执行器。
任何恢复都要同时校验数据库备份 SHA、storage 备份 SHA、Alembic revision、媒体可读性和 archive hash。
