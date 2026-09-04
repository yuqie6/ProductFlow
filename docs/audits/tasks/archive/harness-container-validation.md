# 任务：核验冻结壳的完整容器构建与非 root 轨迹卷

状态：完成
类型：证据
认领者：主代理-agent-0905-0458
认领于：2026-09-05T06:29:24+08:00
业务组：Agent 能力
父账本：agent-self-harness.md
完成后可拆：无；不解锁 P3 或 G1

按 [Issue 协议](../README.md) 认领后调查和采证。

## 问题来源

[P1](harness-artifact.md) 的完整镜像构建曾受凭据 helper 与依赖下载阻塞，只有无网络工件加载通过。[P2b](harness-traces.md) 增加非 root 轨迹目录和独立卷，但只有 Compose 静态验证，尚未核验构建出的运行镜像。需补齐实际包装证据，不能将 TypeScript 构建通过当作容器验收。

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

认领确认：已在协调工作树登记并复核；Docker Server 29.3.1 可用，基线固定 `c87d8aca`，无源码写入范围。图片组占用保持不变。

## 采证结果

- 2026-09-05 固定输入：`git archive c87d8aca` 导出到 `/tmp/productflow-agent-container-xEbhdn/context`。Dockerfile、源码、lockfile 均未修改，无宿主机 node_modules 或真实 .env 挂载。
- 默认构建复现 Corepack 下载 `pnpm-10.32.1.tgz` 的 `ETIMEDOUT` / IPv6 `ENETUNREACH`，发生在源码编译前。空 `auths` 的临时 Docker config 避免失效的桌面 credential helper，不使用生产凭据。
- 原因定位：宿主机 curl 200；容器 DNS 与宿主机返回相同地址，改 host 网络仍超时。容器 Node 22.20.0 按 `250ms -> 2000ms -> 250ms -> 2000ms` 设置地址自动选择等待时间，结果依次为 `ETIMEDOUT 3070ms -> 200 1329ms -> ETIMEDOUT 3064ms -> 200 1075ms`。本环境的地址切换等待时间不足是已复现的下载触发因素，未据此修改生产请求超时。
- 临时指定当次 DNS 返回的单一 IPv4（保留 hostname 与 TLS 校验）后，完整原 Dockerfile build 成功：构建阶段安装 176 包，生产阶段安装 126 包，生成合同检查与 TypeScript 编译通过。`--add-host` 仅为此次构建环境条件，不写入 Dockerfile、不换 registry、不升级依赖、不关闭 TLS；不能把该 IP 作为长期配置。
- 镜像：`productflow-agent-audit:c87d8aca`，ID=`sha256:b9ce68ecc49995696c4539d7da1ff2264bc463be5a2306b09ff668c4d9bb7ffb`；Config.User=`productflow`，entrypoint=`node dist/main.js`。完整构建后另做缓存复验，日志在 `/tmp/productflow-agent-container-xEbhdn/cached-build.log`；该文件明确为缓存复验，不能冒充首次下载日志。
- 两次隔离断网 probe 均通过：实际 UID 10001，专用新卷目录 UID 10001 / 0700；默认关闭零轨迹；启用后一个 0600 文件，footer complete=true，I/O 错误=0。实际 `dist/main.js` 启动、`/healthz` 和 SIGTERM 正常退出通过。probe 输出在 `/tmp/productflow-agent-container-xEbhdn/probe-output.json`，脚本仅在临时诊断目录，不进生产镜像。
- health、加载工件与轨迹 header 的 hash 均为 `13e8e19ae0ba1cadc732f5f4ba6d0ffba0da08d5a859671ccb67d72bf52dae21`；容器只使用自身生产依赖、打包工件与 Skill，不依赖源码 checkout。
- probe 以 `--rm` 运行，匿名 data/trace 卷随测试容器移除。构建和 probe 均结束，没有新增常驻服务；原 PostgreSQL/Redis 与其他共享容器保持运行。审计镜像和临时诊断目录保留供复核，不包含真实业务数据。
- 自审：主代理-agent-0905-0458 核对固定提交、镜像身份、probe 断言及共享资源，证据满足本单包装合同；`just docs-check` / `git diff --check` 通过，随任务提交。未调用真实模型，未部署共享 dev，不宣称模型成功率或 G1/G2 通过。默认网络构建在本环境仍需运维处理地址选择条件，成功记录包含这项限制。

```bash
docker --config /tmp/productflow-agent-container-xEbhdn/docker-config build --progress=plain --pull=false --add-host registry.npmjs.org:104.16.6.34 -t productflow-agent-audit:c87d8aca -f agent-service/Dockerfile .
docker run --rm --network none --mount type=volume,dst=/app/storage/agent-evolution-traces --mount type=bind,src=/tmp/productflow-agent-container-xEbhdn/probe.mjs,dst=/tmp/probe.mjs,readonly --entrypoint node productflow-agent-audit:c87d8aca /tmp/probe.mjs
```
