# 事件及管理协议

以下描述第一版实际接口。管理接口使用同源 Cookie 登录；采集接口不需要管理员 Cookie，不要把管理凭据嵌入客户端。

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
| `desktop` | `app_start`、`active` | 无 Origin 的通用 HTTP 客户端；有 Origin 时须在白名单 |
| `web` | `page_view`、`active` | 必须提供精确匹配白名单的 Origin |

`event_id` 在生成时固定；网络失败、超时、HTTP 错误后重试整个原批次，不能重新生成 ID 或时间。后端按 `(app_id, event_id)` 去重。UUID 是推荐客户端约定；后端标识符校验为 1–128 字节、非空、无控制字符。`session_id` 在桌面进程内固定，每次重新启动产生新值；`anonymous_id` 保存在用户安装状态目录。

`occurred_at` 为带时区的 RFC3339/ISO8601 时间，推荐 UTC `Z`；仅接受过去 7 天内、未来不超过 5 分钟的事件。延迟超过窗口的事件会被明确拒绝。时间以事件发生时间归属统计日，而非到达时间。

`page_view` 需要 `path`，如 `/settings`；必须以 `/` 开始且不带主机名，最大 2048 字节。后端去除 query 和 fragment，仅保留路径。客户端仍应避免在路径中加入邮箱、令牌、用户 ID 等信息。版本字符串上限 128 字节，platform/sdk_version 上限 64 字节。不要在可选字段中加入个人信息。

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

`accepted`、`duplicates` 是数量；`rejected` 为逐条列表。三者数量之和应等于提交数。只有取得完整成功确认后客户端才能删除本地批次；逐条拒绝表示对应数据不应继续原样重试。第一版直接在请求事务中写原始事件、维护汇总和去重记录，没有异步投递状态或后台消费状态。

整批失败使用 `{"error":{"code":"...","message":"..."}}`：400 参数/JSON 错误，403 应用关闭或 Origin 不允许，404 应用不存在，429 限流，500 内部错误。当前按应用限制令牌桶约 2 请求/秒、突发 60；返回失败时保留批次并退避，配置错误需人工修正。Python 示例对整批 HTTP 错误统一保留重试，观察 `last_error` 避免长期无效积压。可用网络断开会导致服务端已提交但客户端未收到确认，重复计数是正常的恢复路径。

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
| `PATCH /api/apps/{id}` | 修改应用 |
| `GET /api/stats/overview?from=YYYY-MM-DD&to=YYYY-MM-DD` | 总览 |
| `GET /api/apps/{id}/stats?from=YYYY-MM-DD&to=YYYY-MM-DD` | 应用明细 |

会话为 HttpOnly、SameSite=Strict，默认 Secure，24 小时有效。所有管理写操作及登录要求同源 Origin/Referer；反代和 Vite 开发通过 `PUBLIC_ORIGIN` 显式配置浏览器 origin，不能只改 DOMAIN 而漏改 PUBLIC_ORIGIN。浏览器前端使用同源 Cookie；没有面向管理 API 的 bearer token。不要把 `sakura_session` 保存进 SDK 或发布包。管理员由启动环境初始化/更新，修改环境中的密码后需要重启后端使其生效。

查询默认最近 30 个统计日，日期范围含首尾，最多 366 天，返回统计时区。服务探针为 `GET /healthz`、`GET /readyz`、`GET /api/status`。
