# Sakura Statistics Gateway

Go 后端、Vue + Vite 管理台和 PostgreSQL 16 组成的轻量统计服务。第一版在采集请求的数据库事务中写入事件并增量维护汇总，提交成功后返回确认；没有后台消费队列。管理台通过 HttpOnly Cookie 登录，应用 ID 用于选择统计应用，不是密钥。

## 首次部署

代码已公开于 [GitHub 仓库](https://github.com/Guyao146/Sakura-Statistics-Gateway)。镜像 `ghcr.io/guyao146/sakura-statistics-gateway` 已发布，包含 `linux/amd64`、`linux/arm64`，可匿名拉取，无需登录 GHCR。首次发布和实际 Compose 集成验收均已成功，详见[验证记录](docs/validation.md)。

固定首版 `v0.1.0` 的部署入口：[Release 页面](https://github.com/Guyao146/Sakura-Statistics-Gateway/releases/tag/v0.1.0)、[部署包下载](https://github.com/Guyao146/Sakura-Statistics-Gateway/releases/download/v0.1.0/sakura-deployment.tar.gz)。部署包配置使用对应镜像的固定 digest，避免后续标签变化影响部署。

本次计划发布 `v0.1.1`，新增可单独拉取的 `ghcr.io/guyao146/sakura-statistics-gateway-web` 前端镜像与站内接入指南。**以下首版发布记录不代表 v0.1.1 已发布或验收通过**；确认新版本发布成功后再使用它的部署包/实际镜像 digest。源码中的 `.env.example` 已指向计划版本，不能在发布前当作可拉取承诺。默认部署继续使用内置管理台的本体镜像，不强制启用独立前端。

服务器只需 Docker Engine 和 Compose v2，**不需要下载源码、安装 Go/Node 或执行 build**。备份/恢复脚本另需 Python 3.9+。部署包包含 `compose.yaml`、`.env.example`、`docs/` 和 `scripts/`，不含 `examples/`、源码或 `compose.build.yaml`。

1. 下载并解压首版部署包，在解压目录复制配置：

```sh
mkdir -p sakura-deployment
cd sakura-deployment
curl -fL https://github.com/Guyao146/Sakura-Statistics-Gateway/releases/download/v0.1.0/sakura-deployment.tar.gz -o sakura-deployment.tar.gz
tar -xzf sakura-deployment.tar.gz
cp .env.example .env
```

2. 编辑 `.env`，保留包内 `SAKURA_IMAGE` 的固定镜像 digest；新双镜像部署包也保留 `SAKURA_FRONTEND_IMAGE` 的固定 digest。设置 `DOMAIN=stats.example.com`、`PUBLIC_ORIGIN=https://stats.example.com`（与浏览器访问地址完全一致，无结尾斜线）、`SESSION_SECURE=true`。为数据库和管理员设置不同的长随机密码；管理员密码至少 12 字符，包含大写、小写、数字和符号。`DATABASE_URL` 的用户、密码、库名须与 `POSTGRES_*` 一致，URL 密码中的特殊字符须百分号编码。不要提交 `.env`。
3. 将域名 DNS A/AAAA 指向服务器，开放 TCP 80/443，确认端口未被其他服务占用。
4. 拉取镜像并启动 HTTPS 服务：

```sh
docker compose --profile https pull
docker compose --profile https up -d
docker compose --profile https logs --tail=100 app caddy
curl https://stats.example.com/readyz
```

访问 `https://stats.example.com`，使用 `.env` 中的管理员账户登录并创建应用。客户端示例位于完整源码仓库，不在部署包中；接入说明见文末。Caddy 真实域名签证等未完成项目见[验证记录](docs/validation.md)。

无事件时页面显示空数据是正常状态。`/healthz` 表示进程可响应，`/readyz` 和 `/api/status` 还检查数据库连接。Postgres 的 Compose healthcheck 只判断数据库是否接受连接，不能替代应用 readiness。

数据存储在 `postgres_data` 命名卷中。`docker compose down` 保留卷；**`docker compose down -v` 会删除数据库及 Caddy 卷，不能用于常规升级或停机。**

## 本机 HTTP 试用

也可用部署包在本机试用，无需构建。将 `.env` 改为 `SESSION_SECURE=false`、`PUBLIC_ORIGIN=http://127.0.0.1:8080`、`DOMAIN=localhost`，不启用 HTTPS profile：

```sh
docker compose pull
docker compose up -d
curl http://127.0.0.1:8080/readyz
```

使用 `http://127.0.0.1:8080` 访问，避免换成不同 origin。生产应使用 HTTPS 和安全 Cookie。

## 可选独立前端与指南更新

`frontend` profile 适用于已由宿主机 Nginx 等服务管理 HTTPS 的服务器，或需要独立更新管理台/接入指南的部署。它从发布镜像提供静态站点，并将 `/api/`、`/sdk.js`、`/readyz` 代理到 `app:8080`，不是第二套后端或数据库。默认 `docker compose up -d` 不启动它，现有本体 `127.0.0.1:8080` 与 `https` profile 的 Caddy → `app:8080` 保持不变；启用 frontend 不会自动改写 Caddy。

确认 `v0.1.1` 或后续对应版本的两个镜像均已发布，采用该版本部署包的 `compose.yaml`，并将包内实际 `SAKURA_FRONTEND_IMAGE=ghcr.io/guyao146/sakura-statistics-gateway-web@sha256:…` 复制到现有 `.env`；省略号不是可执行 digest。保留已有密码、数据库配置和卷，不要用新模板覆盖生产 `.env`。前端变量有计划版本回退值，避免关闭 profile 时 Compose 插值失败，但生产仍应显式固定已发布 digest。

本机 HTTP 使用前端时将 `.env` 设为 `PUBLIC_ORIGIN=http://127.0.0.1:8081`、`SESSION_SECURE=false`，然后执行：

```sh
docker compose --profile frontend pull frontend
docker compose --profile frontend up -d --wait
curl http://127.0.0.1:8081/readyz
curl http://127.0.0.1:8081/api/status
```

通过 `http://127.0.0.1:8081` 打开管理台与接入指南。`PUBLIC_ORIGIN` 只支持一个完整 origin，不能同时允许 8080 和 8081 的管理写请求。已有宿主机 Nginx 的生产服务器保持公网 `PUBLIC_ORIGIN=https://stats.example.com`、`SESSION_SECURE=true`，把该域名的整站反代上游改为 `127.0.0.1:8081` 并保留 Host/Origin；本节不覆盖默认 Caddy HTTPS 方案，也不要直接公网暴露 8081。

仅更新指南/静态前端时，先备份并将 `.env` 中的 `SAKURA_FRONTEND_IMAGE` 替换为已经发布、与现有 API 兼容的固定 digest，再执行：

```sh
docker compose --profile frontend pull frontend
docker compose --profile frontend up -d --no-deps --wait frontend
docker compose --profile frontend logs --tail=100 frontend
curl http://127.0.0.1:8081/readyz
```

仅前端更新不需要重新构建或重建 app/Postgres；需要更新本体或配置时按运维手册执行完整升级。共享主机先检查磁盘余量，保留回退镜像；不要在线 build、执行无差别 prune 或清理其他服务目录。前端以非 root 用户监听容器端口 80，Compose 保留该端口的非特权绑定设置，并限制为 128 MiB 内存、0.5 CPU、64 个进程，日志最多 3 × 5 MiB。healthcheck 调用后端 `/api/status`，`/readyz` 返回代理的 JSON，不将 SPA HTML 当成健康结果。

## 镜像发布

以下仅供维护者或 fork 后自行发布，**不是使用本项目公开镜像的必需步骤**。

仓库的 `.github/workflows/publish-image.yml` 由 `v[0-9]*` 标签推送触发，也支持手动选择版本标签运行（不从普通分支发布）。发布前先执行 CI（Go、真实 PostgreSQL 16 集成测试、race、前端与 Python 测试），通过后才构建并推送本体和独立前端两个 `linux/amd64`、`linux/arm64` 镜像。工作流使用 GitHub 自带的 `GITHUB_TOKEN`（`contents: read`、`packages: write`）登录 GHCR，不需要额外 registry 密码；自行发布时须确保仓库或组织允许 Actions/Packages 写入。

本体路径取 `github.repository` 的小写形式，前端路径在其后加 `-web`；两者均发布版本标签（如 `v0.1.1`）和 `sha-完整提交SHA` 标签，没有默认 `latest`。Actions 的 `sakura-deployment` 产物内含 `sakura-deployment.tar.gz`，包内 `.env.example` 将 `SAKURA_IMAGE`、`SAKURA_FRONTEND_IMAGE` 分别固定为该次发布的实际 digest，仅含密码占位符，不打包机器 `.env`、真实密码或机器配置。以实际发布输出为准，不要猜测标签或 digest。

发布后可手动运行 `.github/workflows/verify-deployment.yml`，填写同一版本的两个镜像 tag/digest。它在独立 Actions 项目/临时配置中先验收 8080 单体的 readiness、SDK、Cookie 登录、采集去重与统计，再把 `PUBLIC_ORIGIN` 切到 8081，启用 frontend profile 检查实际 JS 资产中的指南内容、API/SDK 代理、JSON readiness、Cookie 登录及错误 Origin 拒绝；最终删除该隔离环境的卷。此项仅为 HTTP/容器验收，**不是浏览器渲染、点击或隐藏 GUI 验证**，不能在生产目录运行或据此提前宣称新版本通过。

本项目的 GHCR 镜像已公开，可匿名拉取。自行发布的包若为私有，可改为公开或为自己的服务器另配读取凭据；不要把 Actions 的临时 `GITHUB_TOKEN` 当作长期服务器登录密码。服务器使用部署包执行前文的 `docker compose --profile https pull` / `up -d` 即可，基础 Compose 没有 build 字段。

## 本地容器构建

本节仅供开发者在**完整源码仓库**中操作，部署包不含 `compose.build.yaml`。把本地 `.env` 的 `SAKURA_IMAGE` 设为 `sakura-statistics-gateway:local`，再执行：

```sh
docker compose -f compose.yaml -f compose.build.yaml up -d --build
```

覆盖文件显式指定本地镜像名和构建上下文。Compose 会先插值基础文件，即使覆盖 image 也必须填写 SAKURA_IMAGE。本地构建时不要套用服务器的 `docker compose pull` 步骤；需要启用 Caddy 时在上述命令中加入 `--profile https`。

需要本地构建独立前端时，可设 `SAKURA_FRONTEND_IMAGE=sakura-statistics-gateway-web:local` 并使用：

```sh
docker compose -f compose.yaml -f compose.build.yaml --profile frontend up -d --build
```

本体和 `web/Dockerfile` 都以项目根为 build context，在 Node.js 22 Alpine 的构建阶段安装依赖、编译 web，并复制 `docs/` 到 `/src/docs` 以支持指南跨 web 根目录导入。不再依赖宿主机已有 `web/dist`；不要把 `web/` 作为独立前端镜像的 build context。本地访问仍须匹配 8081 的 `PUBLIC_ORIGIN`。

## 本地开发

仅适用于**完整源码仓库**，不是服务器部署步骤。模块声明 Go 1.23+，推荐 Go 1.27（当前协作验证版本 1.27.3，容器和 CI 对齐 1.27），Node.js 22（Vite 需要 22.12+）和 PostgreSQL 16。可只启动数据库：

```sh
docker compose up -d postgres
```

基础 Compose 没有发布数据库端口。开发时可以在单独的本地数据库创建库，或使用下述临时覆盖文件 `compose.dev.yaml`（包含凭据的文件不要提交）：

```yaml
services:
  postgres:
    ports:
      - "127.0.0.1:5432:5432"
```

```sh
docker compose -f compose.yaml -f compose.dev.yaml up -d postgres
```

在启动 Go 的终端设置 `DATABASE_URL`，主机名用 `127.0.0.1` 而不是容器服务名 `postgres`，同时设置 `ADMIN_USERNAME`、`ADMIN_PASSWORD`、`SESSION_SECURE=false`、`STATISTICS_TIMEZONE=Asia/Shanghai`、`PORT=8080`、`PUBLIC_ORIGIN=http://localhost:5173`（与你实际使用的 Vite 地址完全一致，无结尾斜线）。后端不会自动加载根目录 `.env`，请用终端或 IDE 环境变量设置。

```sh
cd backend
go mod download
go run .
```

另开终端启动管理台，Vite 将 `/api` 代理到后端：

```sh
cd web
npm ci
npm run dev
```

使用 Vite 输出的地址访问，默认 `http://localhost:5173`。生产静态构建使用 `npm run build`；从项目根运行编译好的后端时，将 `STATIC_DIR` 设为 `web/dist` 的绝对路径更明确。数据库迁移在后端启动时自动执行，不需要独立初始化 SQL 命令。

## 生产 HTTPS

1. 将域名的 DNS A/AAAA 指向服务器，开放 TCP 80/443（UDP 443 可选），检查无其他服务占用。
2. 设置 `.env` 的 `DOMAIN=stats.example.com`、`PUBLIC_ORIGIN=https://stats.example.com`、`SESSION_SECURE=true`，设置强密码。`DOMAIN` 使用纯主机名即可，Caddy 自动申请和续期证书。
3. 启用 HTTPS profile：

```sh
docker compose --profile https pull
docker compose --profile https up -d
curl https://stats.example.com/readyz
```

Caddy 通过内部地址 `app:8080` 反代；应用发布端口仅 `127.0.0.1:8080`，数据库不向主机发布端口。不要额外将应用 HTTP 端口暴露到公网。生产管理台从 HTTPS 域名访问。Caddy 的证书和账户存储在 `caddy_data`；保护并持久保存这些卷。Caddy 在 app readiness 健康检查通过后启动；运行中遇到 502 时结合 `/readyz` 和日志排查。

`DOMAIN=localhost` 配合 HTTPS profile 时，同时设置 `PUBLIC_ORIGIN=https://localhost`；它使用 Caddy 本地 CA：宿主机/浏览器不会自动信任容器内的根证书。需将容器的 `/data/caddy/pki/authorities/local/root.crt` 导出并按本机系统/浏览器规则信任；客户端也必须信任该 CA。不要通过关闭 TLS 校验解决证书问题。最简单的本机开发方式仍是回环 HTTP 配合 `SESSION_SECURE=false`。生产始终开启安全 Cookie。

配置修改后执行 `docker compose --profile https up -d` 更新服务；镜像版本变化时先执行 `docker compose --profile https pull`。直接进程部署需给后端加载系统时区库；容器已安装 `tzdata` 和 `ca-certificates`，以 UID 10001 的非 root 用户运行。静态文件位于 `/app/web/dist`。

## 备份、恢复与升级

详见 [运维手册](docs/operations.md)。升级前先完成备份，并在独立空库实际演练恢复：

```sh
python scripts/backup.py backups-before-upgrade.dump
```

备份脚本输出 PostgreSQL custom 格式，拒绝覆盖已有文件，失败时删除不完整输出；它通过 `docker compose exec -T postgres pg_dump` 使用容器环境，不执行或 `source` `.env`。将备份复制到独立存储并控制访问权限。仅备份数据库卷的文件不能替代一致的在线数据库备份。

```sh
docker compose stop app
python scripts/restore.py backups-before-upgrade.dump --confirm-empty-database sakura
```

恢复仅接受已停止应用的空数据库，不删除已有对象；应优先在独立 Compose 项目或新卷演练。恢复成功且版本匹配后再启动应用。应用启动会自动迁移；第一版没有 down migration，**回滚镜像不保证能降级数据库**。涉及模式变化时，必须准备升级前备份及对应版本的完整恢复方案。

## 接入与指标

- [完整接入指南：网页、桌面客户端与验收排错](docs/integration.md)
- [事件协议和 Cookie 管理 API](docs/protocol.md)
- [指标口径及限制](docs/metrics.md)
- [Python 客户端示例（源码仓库）](https://github.com/Guyao146/Sakura-Statistics-Gateway/tree/v0.1.0/examples/python)，部署包不包含 `examples/`
- [网页 SDK 与停用接口](docs/web-sdk.md)
- [首版范围与后续计划](docs/roadmap.md)

客户端自报事件不能证明真实用户、真实安装或授权状态。UUID、防重复和 Origin 校验只解决部分工程问题，无法鉴别伪造的客户端事件。不要据此计算计费、考勤或其他需要可信证明的记录。

身份按应用和匿名安装 ID 统计，不做跨应用、跨端或跨设备合并。重装、清除数据、更换状态目录会产生新的身份；复制状态目录会共享身份。已包含网页 SDK `/sdk.js` 和 Python 通用 HTTP 示例；其他语言/桌面框架尚待接入。当前没有自动保留期、删除清理任务、同意管理 UI 或导出工作流。原始事件和汇总会持续增长，运维需监控磁盘和数据库容量。不要擅自删除原始事件：这会破坏去重、版本/页面明细或重建能力。第一版未实现聚合重建与保留期管理。
