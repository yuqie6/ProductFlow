# 任务：核验冻结壳的完整容器构建与非 root 轨迹卷

状态：开放
类型：证据
认领者：—
认领于：—
业务组：Agent 能力
父账本：agent-self-harness.md
完成后可拆：无；不解锁 P3 或 G1

按 [Issue 协议](README.md) 认领后调查和采证。

## 问题来源

[P1](archive/harness-artifact.md) 的完整镜像构建曾受凭据 helper 与依赖下载阻塞，只有无网络工件加载通过。[P2b](archive/harness-traces.md) 增加非 root 轨迹目录和独立卷，但只有 Compose 静态验证，尚未核验构建出的运行镜像。需补齐实际包装证据，不能将 TypeScript 构建通过当作容器验收。

## 结果与边界

从固定 Git 提交的干净导出目录构建现有 Agent Dockerfile；用隔离、一次性容器验证 UID 10001、冻结 harness hash、默认关闭不写轨迹，以及启用后对专用卷的写入权限。保存命令、固定提交、镜像 ID、结果及限制。无需启动 Go API、访问真实 provider 或改变共享 dev。

本单为证据任务。若发现实现缺陷，记录具体触发条件，由维护者另发实现单；不在本单改源码或 Dockerfile。镜像构建依赖失败时记录原始错误并阻塞，不伪装通过。

## 前置与并行

- Docker daemon 可用；依赖下载需现有公开 registry 网络，不提供或读取生产凭据。
- 运行前冻结基线为当前已提交 Agent 实现，使用 `git archive` 导出到临时目录，不包含其他会话未提交 diff、真实 .env、storage 或缓存。
- 独占本任务命名的临时构建上下文、镜像 tag 和一次性卷；不占用共享容器、端口、DB、provider、worker 或浏览器，不执行 compose up/down。
- 其他会话仍拥有图片池任务；其路径和运行资源不变。评测题目校正如另获认领，可在自己的边界进行，容器验证只消费固定提交导出。

## 修改范围与锚点

- 只改本 issue、Agent 父账本及必要归档索引。不改源码、部署文件、真实配置或其他已关闭 issue 的历史结论。
- 读取锚点：`agent-service/Dockerfile`、`.dockerignore`、`docker-compose.yml`、`agent-service/src/config.ts`、`src/harness.ts`、`src/evolution-traces.ts`。
- 原始构建输出在临时产物目录；不提交构建输出或敏感配置。所有临时资源完成后明确结束，不留下测试服务器。

## 验收

- 完整 Dockerfile build 成功，登记固定提交与 image ID；不只验证宿主机 node_modules 挂载。
- 从构建镜像读取 `DEPLOYED_HARNESS.hash`，与当前冻结工件身份一致，不依赖仓库 Go 源码路径。
- 实际运行用户为非 root UID 10001；新专用卷可写 0600 的结构轨迹，默认关闭无轨迹文件。容器无网络即可完成验证。
- `just docs-check` 与专属 diff 自审通过；不宣称生产部署、模型成功率提升或 G1/G2。

## 发布依据

2026-09-05：主代理-agent-0905-0458 核对看板无同类验证占用。P1/P2b 归档仍明确完整镜像和运行卷缺口，本任务只补包装证据，不绕过 P3 的独立评测前置。
