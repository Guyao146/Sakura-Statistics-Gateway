# 部署与数据库运维

## 配置及检查

Compose 自动读取项目根 `.env`，Python 运维脚本不解析或执行该文件。基础 Compose 要求 `SAKURA_IMAGE` 为真实已发布镜像；即使只操作 postgres，Compose 配置插值也要求此值存在。镜像发布与访问权限见根 README；本项目已发布公开镜像，部署包配置固定镜像 digest，可直接拉取。不要在 shell 中 `source .env`。生产设置 `SESSION_SECURE=true`、`PUBLIC_ORIGIN=https://实际域名` 并从 HTTPS 域名访问；`PUBLIC_ORIGIN` 必须是浏览器看到的完整 origin（协议、主机及非默认端口，无结尾斜线），用于 TLS 反代后的管理请求同源校验。Caddy 配置的 DOMAIN 与其主机保持一致。本机 HTTP 开发显式设置 false。`.env.example` 是占位配置，必须替换密码，管理员密码要求至少 12 字符并包含大小写、数字和符号。数据库密码变更需同步数据库内角色密码，**仅修改 POSTGRES_PASSWORD 不会改变已有卷内的角色密码**。

`DATABASE_URL` 使用 Compose 网络主机 `postgres:5432`；特殊字符按 URL 规则编码。示例 `sslmode=disable` 仅适用于同机容器内部网络；连接外部数据库应按数据库服务要求开启并校验证书。

```sh
docker compose ps
docker compose logs --tail=100 postgres app
curl http://127.0.0.1:8080/readyz
```

`pg_isready` 仅表明数据库接受连接，`/readyz` 进一步检查应用数据库连接。应用迁移失败、管理员配置不符合要求或静态文件缺失需要检查应用日志。不要将包含密码的完整 Compose 展开输出发送到公共讨论区。

## 在线逻辑备份

脚本在任何工作目录均可运行；相对输出路径相对于当前终端目录，父目录必须存在。生产备份文件包含事件、管理员密码哈希和会话表等数据，应限制访问并加密保存到独立存储。文件不得提交版本库。

```sh
python scripts/backup.py /safe/location/sakura-2026-10-30.dump
```

实际通过容器内变量执行：

```sh
docker compose exec -T postgres sh -ec 'exec pg_dump --format=custom --no-owner --no-privileges -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
```

脚本将 stdout 直接写入新文件，独占创建避免覆盖；失败删除不完整文件。数据库一致性由 `pg_dump` 快照提供，通常可以在线备份。备份不包括 `.env`、镜像、源代码、Caddy 卷或数据库全局角色；须另外保护配置、记录版本并保留数据库凭据。pg_dump 成功不代表灾难恢复已验证，必须定期在独立空库演练恢复。

不要把运行中的 `/var/lib/postgresql/data` 目录直接打包作为一致性备份。命名卷负责持久化，不负责异地备份。需要物理备份或时间点恢复时应使用 PostgreSQL 专用方案，第一版脚本只提供逻辑备份。

## 安全恢复

恢复到空数据库，先停止应用，避免自动迁移抢先创建对象。建议使用独立目录、独立 `.env`、不同项目名/域名的恢复环境。Compose 脚本会继承终端的 `COMPOSE_PROJECT_NAME`，备份和恢复时确认其指向预期项目。

```sh
docker compose stop app
docker compose up -d postgres
python scripts/restore.py /safe/location/sakura-2026-10-30.dump --confirm-empty-database sakura
```

`--confirm-empty-database` 必须等于容器环境中的实际 `POSTGRES_DB`。脚本检查该 Compose 项目的 app 未运行、库内没有用户表/视图/序列等对象，使用 `pg_restore --single-transaction --exit-on-error`。不使用 `--clean`，不自动 drop 库或表。已有对象将导致拒绝；请另建数据库或新卷，保留原库以便核对。

单事务恢复失败会回滚。不要在检查与恢复之间连接其他写入程序，也不要同时运行多个恢复进程。恢复完成后先核对版本与数据，再启动应用：

```sh
docker compose up -d app
curl http://127.0.0.1:8080/readyz
```

启用生产 HTTPS 时使用 `docker compose --profile https up -d`。验证管理台登录、应用配置及历史统计。恢复旧数据库可能恢复当时尚未过期的会话，因此应在受控维护窗口执行，并按部署要求处理访问控制。

## 升级与回退

1. 记录当前代码版本、镜像标识和配置，完成数据库备份，确认独立恢复演练结果。
2. 计划维护窗口；如需回退后零数据差异，应先停止应用再备份，避免备份后新增写入在回退中丢失。
3. 将 `.env` 的 `SAKURA_IMAGE` 改成已经发布的新版本标签/digest，执行 `docker compose --profile https pull`，再执行 `docker compose --profile https up -d`，观察日志和 readiness。服务器不执行源码构建。
4. 后端启动会按文件名顺序、在事务和数据库锁保护下执行尚未应用的迁移。
5. 若需回退，不假设旧应用兼容新模式。用升级前备份恢复到新的空库，部署匹配旧版本，再验证。

没有自动 down migration，也无法保证数据库可原地降级。PostgreSQL 大版本升级需要单独的升级/导出导入方案，不能直接让新主版本挂载旧数据目录。当前 Compose 固定 PostgreSQL 16；镜像小版本更新也应先备份。

## 已知运维边界

没有自动保留清理、聚合重建、调度备份或备份加密功能。运营者应安排计划任务调用备份脚本、监控磁盘/表增长、错误率和恢复时间目标。不要仅凭进程启动成功或一次 pg_dump 成功就认定恢复方案可用。
