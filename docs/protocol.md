# 事件及管理协议

以下描述 v0.2.0 实际接口。管理接口使用同源 Cookie 登录；采集接口不需要管理员 Cookie，不要把管理凭据嵌入客户端。

## 采集

`POST /api/collect`，`Content-Type: application/json`：

```json
{
  "app_id": "管理台创建后返回的应用ID",
  "events": [
    {
      "schema_version": 1,
      "event_id": "5f4296b0-6800-4fb4-a93e-d16cc121e47e",
      "event_type": "active",
      "occurred_at": "2026-10-30T08:00:00Z",
      "anonymous_id": "5a156bcb-fb44-491d-8072-1d597239df6f",
      "session_id": "3b60038e-8f43-4e4f-9c62-3c80a9c3338e",
      "app_version": "1.0.0",
      "platform": "windows",
      "sdk_version": "python-example/1.0"
    }
  ]
}
```

示例时间必须替换为当前真实 UTC 时间。`app_version`、`platform`、`sdk_version`、`path` 可省略。批次包含 1–500 条事件，请求体上限 1 MiB。客户端应用 ID 和事件类型必须匹配：

| 类型 | 允许事件 | 来源要求 |
| --- | --- | --- |
| `desktop` | `app_start`、`active`、`custom` | 无 Origin 的通用 HTTP 客户端；有 Origin 时须在白名单 |
| `web` | `page_view`、`active`、`custom` | 必须提供精确匹配白名单的 Origin |

`custom` 事件在 web/desktop 应用均可发送，用于业务自定义计数，需要额外字段 `event_name`：1–64 字节、不含控制字符且非全空白字符串。`custom` 事件不携带 `path`，不计入页面浏览；有效条数计入日活，并按应用按日累计到 `daily_metrics.custom_events`。不同 `event_id` 的同名事件各计一次，服务端不做合并或采样。事件名校验失败按单条拒绝，拒绝码为 `invalid_event_name`。

`event_id` 在生成时固定；网络失败、超时、HTTP 错误后重试整个原批次，不能重新生成 ID 或时间。后端按 `(app_id, event_id)` 去重。UUID 是推荐客户端约定；后端标识符校验为 1–128 字节、非空、无控制字符。`session_id` 在桌面进程内固定，每次重新启动产生新值；`anonymous_id` 保存在用户安装状态目录。

`occurred_at` 为带时区的 RFC3339/ISO8601 时间，推荐 UTC `Z`；仅接受过去 7 天内、未来不超过 5 分钟的事件。延迟超过窗口的事件会被明确拒绝。时间以事件发生时间归属统计日，而非到达时间。

`page_view` 需要 `path`，如 `/settings`；必须以 `/` 开始且不带主机名，最大 2048 字节。后端按固定顺序处理路径：先去除 query 和 fragment 仅保留路径，再按应用配置 `normalize_numeric_paths` 把纯数字路径段归一化为 `:id`（如 `/users/123` → `/users/:id`），最后在应用配置 `allowed_path_prefixes` 非空时要求归一化后的路径前缀匹配，未匹配按 `invalid_path` 拒绝；白名单为空表示不限制。客户端仍应避免在路径中加入邮箱、令牌、用户 ID 等信息：归一化只处理纯数字段，不会清理其他敏感值。版本字符串上限 128 字节，platform/sdk_version 上限 64 字节。不要在可选字段中加入个人信息。

应用配置新增两个字段，创建应用和 PATCH 均按整体替换校验：`normalize_numeric_paths` 为布尔值；`allowed_path_prefixes` 为字符串数组，每项 1–256 字节、以 `/` 开头、不为 `//`，最多 50 项。任一字段不合法时修改被拒绝，错误码为 `invalid_path_rules`。

事务提交成功返回 HTTP 200，即使部分或全部事件校验失败：

```json
{
  "accepted": 1,
  "duplicates": 1,
  "rejected": [
    {"event_id": "original-id", "code": "invalid_time", "message": "..."}
  ]
}
```

`accepted`、`duplicates` 是数量；`rejected` 为逐条列表。三者数量之和应等于提交数。只有取得完整成功确认后客户端才能删除本地批次；逐条拒绝表示对应数据不应继续原样重试。采集直接在请求事务中写原始事件、维护汇总和去重记录，没有异步投递状态或后台消费状态。

整批失败使用 `{"error":{"code":"...","message":"..."}}`：400 参数/JSON 错误，403 应用关闭或 Origin 不允许，404 应用不存在，429 限流，500 内部错误。采集按应用限制令牌桶，默认约 2 请求/秒、突发 60，可通过 `RATE_LIMIT_PER_SECOND`（默认 2）与 `RATE_LIMIT_BURST`（默认 60）调整，修改后需重启生效；返回失败时保留批次并退避，配置错误需人工修正。Python 示例对整批 HTTP 错误统一保留重试，观察 `last_error` 避免长期无效积压。可用网络断开会导致服务端已提交但客户端未收到确认，重复计数是正常的恢复路径。

浏览器请求会触发 `OPTIONS /api/collect` 预检；仅 `Content-Type` 被允许。Origin 白名单是浏览器访问控制，不是客户端身份凭证。通用 HTTP 客户端可以自行构造请求，因此本系统不能保证事件真实性。

## 管理 Cookie

| 方法和路径 | 用途 |
| --- | --- |
| `POST /api/auth/login` | JSON `{"username":"...","password":"..."}`；设置 `sakura_session` |
| `GET /api/auth/me` | 返回当前 `username`，未登录返回 401 |
| `POST /api/auth/logout` | 删除当前会话并清除 Cookie |
| `GET /api/apps` | 列出应用 |
| `POST /api/apps` | 创建应用 |
| `GET /api/apps/{id}` | 获取应用配置 |
| `PATCH /api/apps/{id}` | 修改应用（整体替换配置） |
| `DELETE /api/apps/{id}` | 删除应用并级联删除其全部数据 |
| `POST /api/apps/{id}/rebuild?from=YYYY-MM-DD&to=YYYY-MM-DD` | 重算范围内聚合 |
| `GET /api/apps/{id}/retention?weeks=N` | 周留存（N 为 1–12） |
| `GET /api/stats/overview?from=YYYY-MM-DD&to=YYYY-MM-DD` | 总览 |
| `GET /api/apps/{id}/stats?from=YYYY-MM-DD&to=YYYY-MM-DD` | 应用明细 |
| `GET /api/metrics?from=YYYY-MM-DD&to=YYYY-MM-DD` | 采集量与拒收/限流计数 |
| `GET /api/settings` | 查看服务端生效配置 |

`DELETE /api/apps/{id}` 级联删除该应用的原始事件、聚合、去重与指标记录，不可恢复，成功返回 `{"ok":true}`。`POST /api/apps/{id}/rebuild` 按仍保留的原始事件重算指定范围，返回 `{"rebuilt_days":...,"events":...,"visitors":...}`；早于当前保留窗口或已记录清理水位的范围返回 HTTP 400、`rebuild_outside_retention`，不执行重建。`GET /api/apps/{id}/retention?weeks=N`（1–12）按 ISO 周（周一起始）分桶，每行包含 cohort（访客首次出现所在周）、size（该周去重访客数）与 retained 数组；`retained[0]` 是首访周，之后的元素是后续各周活跃访客数。响应中的 `timezone` 和 `today` 表示服务端统计时区与统计日，可用于标记进行中或尚未开始的周；`weeks` 不合法返回 `invalid_weeks`。`GET /api/metrics` 返回每应用 `requests`、`accepted`、`duplicates`、`rejected`、`rate_limited` 及合计与按日序列，由 `operation_metrics` 表按应用按日记录；应用不存在导致的 404 请求不计入、也不记录。`GET /api/settings` 返回 `version`、`statistics_timezone`、`retention_days` 与限流参数；`/api/status` 响应也可包含 `version`。

服务端新增 `RETENTION_DAYS` 配置：默认 `0` 表示永久保留；大于 0 时启动校验要求至少 7，否则拒绝启动。启用后后台任务每 6 小时运行一次，按统计时区判断事件归属日，分批删除超出保留窗口的原始 `events` 旧行；聚合与去重表不清理，因此汇总数字保留，明细与 rebuild 范围只覆盖保留窗口。被清理的更旧事件本就超出 7 天接收窗口，超窗重放仍会被 `invalid_time` 拒绝。

会话为 HttpOnly、SameSite=Strict，默认 Secure，24 小时有效。所有管理写操作及登录要求同源 Origin/Referer；反代和 Vite 开发通过 `PUBLIC_ORIGIN` 显式配置浏览器 origin，不能只改 DOMAIN 而漏改 PUBLIC_ORIGIN。浏览器前端使用同源 Cookie；没有面向管理 API 的 bearer token。不要把 `sakura_session` 保存进 SDK 或发布包。管理员由启动环境初始化/更新，修改环境中的密码后需要重启后端使其生效。

查询默认最近 30 个统计日，日期范围含首尾，最多 366 天，返回统计时区。服务探针为 `GET /healthz`、`GET /readyz`、`GET /api/status`。
