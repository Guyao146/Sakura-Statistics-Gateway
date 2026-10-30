# Sakura Statistics Gateway

Go 后端、Vue + Vite 管理台和 PostgreSQL 16 组成的轻量统计服务。第一版在采集请求的数据库事务中写入事件并增量维护汇总，提交成功后返回确认；没有后台消费队列。管理台通过 HttpOnly Cookie 登录，应用 ID 用于选择统计应用，不是密钥。

## 首次启动

服务器只需要 Docker Engine / Docker Desktop 和 Compose v2，无需 Go、Node 或本地构建。备份工具和客户端示例需要 Python 3.9+。准备本仓库的 `compose.yaml`、`.env.example`、`docs/Caddyfile`（HTTPS 时需要）及 `scripts/`（备份时需要），在该部署目录操作。

**当前模板没有发布真实镜像，不能直接填一个猜测的 GHCR 地址拉取。** 先按[镜像发布](#镜像发布)在你自己的 GitHub 仓库运行 Actions，从发布记录取得真实镜像地址；代码由此交付不代表镜像已发布。也可以使用后文的本地构建流程。

1. 复制 `.env.example` 为 `.env`，填写真实 `SAKURA_IMAGE`（推荐固定版本标签或 digest），为数据库和管理员设置不同的随机密码。管理员密码至少 12 字符，包含大写、小写、数字和符号。`DATABASE_URL` 的用户、密码、库名须与 `POSTGRES_*` 一致；URL 密码中的特殊字符要百分号编码。不要提交 `.env`。
2. 本机 HTTP 开发把 `.env` 的 `SESSION_SECURE` 改成 `false`。保持 `DOMAIN=localhost`。
3. 启动并访问管理台。

```sh
docker compose pull
docker compose up -d
docker compose logs --tail=100 app
curl http://127.0.0.1:8080/readyz
```

打开 `http://127.0.0.1:8080`，使用 `.env` 中的管理员账户登录，创建 `desktop` 应用，再复制其 ID 运行示例：

```sh
python examples/python/demo.py --app-id YOUR_APP_ID
python -m unittest discover -s examples/python -v
```

示例首次构造客户端时记录一次启动；只有在提示符实际输入交互时调用 `mark_active()`。无事件时页面显示空数据是正常状态。`/healthz` 表示进程可响应，`/readyz` 和 `/api/status` 还检查数据库连接。Postgres 的 Compose healthcheck 只判断数据库是否接受连接，不能替代应用 readiness。

```sh
docker compose stop
docker compose up -d
```

数据存储在 `postgres_data` 命名卷中。`docker compose down` 保留卷；**`docker compose down -v` 会删除数据库及 Caddy 卷，不能用于常规升级或停机。**

本地已完成的检查及尚未验证项目见[验证记录](docs/validation.md)。

## 镜像发布

仓库包含 `.github/workflows/publish-image.yml`，由版本标签 `v*` 推送触发，也支持手动运行。需要你先把完整代码推到自己的 GitHub 仓库，再从仓库 Actions 运行；本交付没有创建仓库或推送代码，也没有已发布镜像地址。

发布任务先调用仓库 CI（Go、真实 PostgreSQL 16 集成测试、前端与 Python 测试），验证通过才构建推送；这里尚未实际运行 GitHub Actions 或 PostgreSQL 16 容器验证。工作流使用 GitHub 自带的 `GITHUB_TOKEN`（`contents: read`、`packages: write`）登录 GHCR，无需预先配置额外 registry 密码。组织若限制 Actions/Packages 写入，需要仓库所有者调整其策略。工作流以实际 `github.repository` 的小写路径命名镜像，构建并发布 `linux/amd64`、`linux/arm64`；版本标签保留为 `v1.0.0` 这样的标签，并发布 `sha-完整提交SHA` 标签。没有默认 `latest` 标签。以工作流输出的标签/digest 为准填写 `SAKURA_IMAGE`，此处不猜测 OWNER。

发布成功后，Actions 同时提供 `sakura-deployment` 下载产物，内含 `sakura-deployment.tar.gz`。部署包包含 Compose、配置示例、HTTPS 配置和备份脚本，配置示例已填写本次镜像的固定 digest；服务器解压后补充密码、域名等配置即可使用，无需下载源码。

GHCR 包首次发布可能是私有：在 GitHub Packages 将它设为公开后服务器可匿名拉取。保持私有时，服务器需要先 `docker login ghcr.io`，使用拥有该包读取权限的凭据（通常为带 `read:packages` 的 PAT；组织可能还要求 SSO 授权）。Actions 的临时 GITHUB_TOKEN 不能当作长期服务器登录密码。

配置完真实镜像地址后，服务器操作只有：

```sh
docker compose pull
docker compose up -d
```

生产 HTTPS 使用相同命令加 `--profile https`。基础 Compose 没有 build 字段，不会要求服务器编译源码。

## 本地容器构建

开发机器有完整源码时，把本地 `.env` 的 `SAKURA_IMAGE` 设为 `sakura-statistics-gateway:local`，再执行：

```sh
docker compose -f compose.yaml -f compose.build.yaml up -d --build
```

覆盖文件显式指定本地镜像名和构建上下文。Compose 会先插值基础文件，即使覆盖 image 也必须填写 SAKURA_IMAGE。本地构建时不要套用服务器的 `docker compose pull` 步骤；需要启用 Caddy 时在上述命令中加入 `--profile https`。

## 本地开发

模块声明 Go 1.23+，推荐 Go 1.27（当前协作验证版本 1.27.3，容器和 CI 对齐 1.27），Node.js 22（Vite 需要 22.12+）和 PostgreSQL 16。可只启动数据库：

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

- [事件协议和 Cookie 管理 API](docs/protocol.md)
- [指标口径及限制](docs/metrics.md)
- [Python 客户端示例](examples/python/README.md)
- [网页 SDK 与停用接口](docs/web-sdk.md)
- [首版范围与后续计划](docs/roadmap.md)

客户端自报事件不能证明真实用户、真实安装或授权状态。UUID、防重复和 Origin 校验只解决部分工程问题，无法鉴别伪造的客户端事件。不要据此计算计费、考勤或其他需要可信证明的记录。

身份按应用和匿名安装 ID 统计，不做跨应用、跨端或跨设备合并。重装、清除数据、更换状态目录会产生新的身份；复制状态目录会共享身份。已包含网页 SDK `/sdk.js` 和 Python 通用 HTTP 示例；其他语言/桌面框架尚待接入。当前没有自动保留期、删除清理任务、同意管理 UI 或导出工作流。原始事件和汇总会持续增长，运维需监控磁盘和数据库容量。不要擅自删除原始事件：这会破坏去重、版本/页面明细或重建能力。第一版未实现聚合重建与保留期管理。
