# 业务命令用 GORM 模型写库

命令路径用 schema 模型 `Create` / `Updates` / `Take` 加行锁，不再手写 INSERT 列清单。漏列（例如 `last_checkpoint_sequence`）由模型 default 和结构体字段承担。schema 仍是 `productflow-migrate`：GORM `CreateTable`/`AddColumn` + ExtraDDL。不使用 AutoMigrate。

## 状态

Accepted。生产命令路径已用 GORM 模型写库；`pfdb.Exec`/`Query`/`QueryRow` 仅测试夹具使用。ExtraDDL 与 `pg_advisory_*` 锁仍是 SQL。

## 考虑过的方案

- 继续手写 SQL，只补测试。拒绝：每个新 NOT NULL 列都要人手抄进 INSERT。
- 抽出共用 SQL 文件给 Python/Go。拒绝：Python 已封印，Go 是唯一运行时。
- GORM association 级联删除。拒绝：删除路径保持显式，见 `go/AGENTS.md`。

## 后果

- 部分更新必须用 `map[string]any` 或 `Select`，禁止零值 struct `Updates`。
- `FOR UPDATE` / `SKIP LOCKED` 走 `platform/db` 的 locking clause。
- ExtraDDL（CHECK、enum、部分唯一索引、FK）仍是 SQL。
- 测试夹具可以暂时留 INSERT；生产命令路径不得再加 raw SQL。
