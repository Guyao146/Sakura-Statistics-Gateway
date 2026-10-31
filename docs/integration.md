# Sakura Statistics Gateway 接入指南

本文说明如何把网站和桌面客户端接入统一统计服务，适用于当前 `v0.1.0` 采集协议和 SDK。核对日期：2026-10-31。

> **统计服务地址：`https://web.mcylyr.cn`。** 接入端只需要应用 ID，不需要后台管理员密码。不要把管理员密码、服务器密码或后台 Cookie 放进网页、客户端配置或上报请求。

## 1. 接入流程

```text
检查统计域名可访问
       ↓
在后台创建 Web / Desktop 应用
       ↓
网页加载 SDK，或客户端接入 HTTP / Python 示例
       ↓
验证上报响应和后台统计
```

| 用途 | 地址 |
| --- | --- |
| 管理后台 | `https://web.mcylyr.cn/` |
| 网页 SDK | `https://web.mcylyr.cn/sdk.js` |
| 事件上报 | `POST https://web.mcylyr.cn/api/collect` |
| 服务状态 | `GET https://web.mcylyr.cn/api/status` |

### 1.1 先检查域名

服务状态接口应返回 HTTP 200 和类似内容：

```json
{"status":"ok","database":"ok","time":"..."}
```

可在终端检查：

```bash
curl -sS https://web.mcylyr.cn/api/status
```

**当前部署提醒：** 服务器 IPv4 的 HTTPS 登录及统计接口已验证通过，但此前发现本域名存在指向另一套入口的错误 AAAA 记录。尚未确认这些 DNS 记录已修正。如果跳转到其他系统的 `/auth/login`，或 `/api/status` 返回 404，请先检查 A/AAAA，而不是关闭来源校验。

已知正确 A 记录是 `206.237.30.190`。临时使用 `curl -4` 可以帮助判断是否为 IPv6 分流，但不能修复浏览器访问或替代 DNS 修正。只有下面状态接口正常后，再验收实际接入。

### 1.2 创建应用

登录管理后台，点击「新建应用」：

1. 填写易识别的名称，例如「官网」「下载站」「Windows 客户端」。
2. 网站选择 **Web 应用**，桌面程序选择 **桌面应用（Desktop）**。
3. Web 应用配置「允许的来源」，填写实际访问网站的 Origin。
4. 保存后复制应用 ID，并确认应用已启用。

建议每个网站、每个独立客户端产品分别创建应用，这样可以独立查看数据。同一客户端的不同版本通常使用同一个应用 ID，通过 `app_version` 区分。

**允许的来源示例：**

```text
https://www.example.com
https://example.com
http://localhost:5173
```

来源由「协议＋主机名＋非默认端口」组成：

- 不带页面路径、查询参数或结尾 `/`。
- 不支持 `*`、`*.example.com` 等通配符。
- `https://example.com` 与 `https://www.example.com` 是两个来源。
- `http://localhost:5173` 与 `http://127.0.0.1:5173` 是两个来源。
- 填写的是**接入网站的来源**，不是默认填写统计后台域名。
- Desktop 客户端使用无 `Origin` 头的 HTTP 请求时，来源可留空；带 `Origin` 时也需要精确匹配白名单。

## 2. 网页接入

### 2.1 普通网站：添加一段脚本

将下面代码加入网站的 `<head>`，把 `YOUR_WEB_APP_ID` 替换为刚创建的 **Web 应用 ID**：

```html
<script
  defer
  src="https://web.mcylyr.cn/sdk.js"
  data-app-id="YOUR_WEB_APP_ID"
  data-endpoint="https://web.mcylyr.cn/api/collect"
  data-timezone="Asia/Shanghai"
></script>
```

每个页面加载一次即可。多页网站可以放在公共布局模板中；单页应用放在入口 HTML 中，不要在每个组件反复插入。

SDK 自动完成：

- 在浏览器本地保存随机访客标识。
- 记录可见页面浏览、网站会话和有效活跃。
- 合并活跃事件、批量发送、失败重试。
- 跟踪受支持的单页应用路由变化。

默认每约 10 秒尝试发送，看到页面后不一定立刻在后台出现数据。等待一次发送周期，再点击后台「查询」。浏览器关闭、拦截器和禁用存储等情况可能导致数据缺失，不保证所有访问完整送达。

### 2.2 支持的脚本属性

| 属性 | 必填 | 说明 |
| --- | --- | --- |
| `data-app-id` | 是 | Web 应用的公开 ID |
| `data-endpoint` | 否 | 默认按 SDK 来源解析 `/api/collect`，建议使用上面的明确地址 |
| `data-timezone` | 否 | 默认 `Asia/Shanghai`，应与服务器统计时区一致；无效值回退默认时区 |
| `data-hash-mode` | 否 | 仅值 `route` 开启 `#/...` hash 路由统计 |

当前网页 SDK 固定上报 `platform="web"`、`sdk_version="0.1.0"`，**没有** `data-app-version`、`data-platform`、自定义事件或纯手动采集配置。不要添加不存在的属性来统计版本或业务按钮。

### 2.3 Vue / React 等 History 路由

使用 `/dashboard`、`/settings` 形式的 History 路由时，保留 2.1 中的脚本即可。SDK 监听 `pushState`、`replaceState` 和 `popstate`，并对连续相同的规范化路径去重。

**不要再在路由钩子中手动统计同一页面：**

```js
// SDK 已自动监听路由，不要再添加：
// router.afterEach(() => window.SakuraStats.trackPage());
```

自动统计和手动调用可能生成不同事件 ID，后端的重试去重不能消除这种业务上的重复 PV。A → B → A 仍然是三次页面浏览；仅查询参数变化默认不算新页面。

### 2.4 Hash 路由

网站使用 `/#/settings` 时，增加 `data-hash-mode="route"`：

```html
<script
  defer
  src="https://web.mcylyr.cn/sdk.js"
  data-app-id="YOUR_WEB_APP_ID"
  data-endpoint="https://web.mcylyr.cn/api/collect"
  data-timezone="Asia/Shanghai"
  data-hash-mode="route"
></script>
```

后台中的路径编码示例：

| 页面地址 | 统计路径 |
| --- | --- |
| `https://example.com/#/settings` | `/~hash/settings` |
| `https://example.com/home#/settings?token=secret` | `/home/~hash/settings` |
| `https://example.com/home#section` | `/home` |

普通锚点和查询参数不进入路径。开启 hash 自动统计后，同样不要重复添加手动路由钩子。

### 2.5 可用的网页方法

确保脚本已加载后，再调用 `window.SakuraStats`：

```js
window.SakuraStats.trackPage('/virtual/help'); // 追加一次虚拟页面浏览
window.SakuraStats.disable();                 // 停止采集，清理本地队列和身份
window.SakuraStats.enable();                  // 从停用恢复，生成新身份并记录可见当前页
await window.SakuraStats.flush();             // 尝试发送一批事件
```

注意：

- `trackPage(path?)` 是**手动追加**，不对重复手动调用去重；页面隐藏或已停用时不会记录。只用于确实需要额外统计的虚拟页。
- SDK 没有关闭自动 PV 的开关。手动虚拟路径不同于实际 URL 时，后续自动检查仍可能记录实际页面。
- `enable()` 从停用恢复时会记录可见当前页，不要紧接着再手动记录同一页。
- `flush()` 每次最多处理一批 30 条，不是一次清空整个队列；Promise 完成不代表服务端一定收到。
- `disable()` 不删除服务器历史，也不能撤回已经发送的请求。停用标记尽力保存在当前网站的本地存储中。

### 2.6 告知、同意与退出

如果产品要求用户同意后才能统计，**先获得同意，再插入 SDK 脚本**。先加载脚本、再调用 `disable()`，不能保证同意前没有事件产生。

下面函数应由产品现有的「用户已同意」回调触发，而不是页面加载时直接调用：

```js
function loadStatisticsAfterConsent() {
  if (window.SakuraStats) {
    window.SakuraStats.enable();
    return;
  }
  if (document.getElementById('sakura-statistics-sdk')) return;

  const script = document.createElement('script');
  script.id = 'sakura-statistics-sdk';
  script.src = 'https://web.mcylyr.cn/sdk.js';
  script.dataset.appId = 'YOUR_WEB_APP_ID';
  script.dataset.endpoint = 'https://web.mcylyr.cn/api/collect';
  script.dataset.timezone = 'Asia/Shanghai';
  script.onload = () => window.SakuraStats?.enable();
  document.head.appendChild(script);
}
```

退出统计时可调用 `disable()`，但产品自己的同意状态也必须同步保存。SDK 不提供同意管理界面，不会自动处理所有法规、DNT 或 GPC 要求。

### 2.7 内容安全策略 CSP

如果网站已设置 CSP，将统计域名加入现有策略，而不是直接覆盖整份策略：

```text
script-src 'self' https://web.mcylyr.cn;
connect-src 'self' https://web.mcylyr.cn;
```

若单独设置了 `script-src-elem`，也须允许脚本来源。内联的同意加载器或方法调用仍需遵守网站自己的 nonce/hash 策略；SDK 本身不要求打开 `unsafe-inline` 或 `unsafe-eval`。

### 2.8 网页日活与会话的边界

- 页面隐藏时不记录新的 PV；后台期间的每次路由变化不会逐一补录，恢复可见后检查当前页。
- 活跃要求页面可见、最近 60 秒有交互；首次加载视作一次最近活动。
- 同会话、同统计日通常五分钟最多一条 `active`；新统计日的首次有效活动可以立即上报。
- 会话标识由 SDK 管理，超过 30 分钟没有有效会话活动后更新；后台并不自动推导会话超时。
- 会话不因午夜自动拆分，后台将其归属到最早事件日。
- 清除本地存储、换浏览器或访问不同 Origin 会得到不同访客身份；多个标签页共享存储但不存在原子事务，不能保证并发下完全不分裂会话。

## 3. 桌面客户端接入

### 3.1 Python 示例：最短接入方式

当前仓库提供 Python 3.9+ 标准库示例，无第三方运行依赖。将 [sakura_sdk.py](../examples/python/sakura_sdk.py) 放进你的客户端项目；在后台创建 **Desktop 应用**。

下面代码用于说明调用位置，**直接运行后立即退出不会等待网络发送完成**。实际程序需让统计对象持续存在；如需立即验证 HTTP 接入，请使用 4.4 的协议测试。

```python
import sys
from pathlib import Path
from sakura_sdk import Client

# 示例目录用于演示；正式应用应使用自己的用户数据目录。
state_dir = Path.home() / '.my-client' / 'statistics'

client = Client(
    endpoint='https://web.mcylyr.cn',  # 只传服务根地址，不加 /api/collect
    app_id='YOUR_DESKTOP_APP_ID',
    state_dir=state_dir,
    app_version='1.0.0',
    platform=sys.platform,
)

try:
    # 构造 Client 时已自动记录一次启动。
    # 在用户真正操作应用的处理函数中调用，而不是每次定时器触发都调用。
    client.mark_active()
finally:
    client.close()
```

上面展示调用位置，不是完整业务程序。真实客户端应让 `Client` 对象随主程序长期存在，在菜单、按钮、编辑等真实交互回调中调用 `mark_active()`，退出时 `close()`。

**启动顺序建议：** 在主程序初始化完成、进入可使用状态后再创建客户端统计对象。统计初始化及本地写入也可能失败，业务代码应捕获并按需停用统计，不能让统计故障阻止应用启动。

```python
import logging
import sys
from pathlib import Path
from sakura_sdk import Client

logger = logging.getLogger(__name__)
statistics = None

# 主程序准备就绪后执行一次。
try:
    statistics = Client(
        'https://web.mcylyr.cn',
        'YOUR_DESKTOP_APP_ID',
        Path.home() / '.my-client' / 'statistics',
        app_version='1.0.0',
        platform=sys.platform,
    )
except Exception:
    logger.warning('统计初始化失败，主程序继续运行', exc_info=True)


def record_real_user_activity():
    # 在真实交互回调中调用此函数。
    if statistics is None:
        return
    try:
        statistics.mark_active()
    except Exception:
        logger.warning('统计入队失败，主程序继续运行', exc_info=True)


def close_statistics():
    if statistics is not None:
        statistics.close()
```

实际 UI 框架不同，回调绑定方式也不同；上面的辅助函数需要接到你自己的主程序生命周期，不会自动捕获所有交互。

### 3.2 生命周期和注意事项

| 项目 | 当前示例行为 |
| --- | --- |
| 启动 | 同进程中，每个「状态目录＋应用 ID」首次构造时尝试记录一次 `app_start` |
| 活跃 | 仅在调用 `mark_active()` 时记录；同日五分钟内合并，跨日首次交互立即记录 |
| 安装标识 | 首次生成随机 ID，保存在状态目录的 SQLite 数据库中 |
| 运行会话 | 每个进程生成一次随机 `session_id` |
| 应用版本、平台 | 显式传入，不自动识别应用版本；`sys.platform` 可作为平台值 |
| 网络发送 | 默认后台线程、每批最多 100 条、五秒超时 |
| 退出 | `close()` 不主动等待网络清空；未确认数据留到下次启动重试 |

- 一般一个应用每个进程只创建一个 `Client`。
- 不要每次按钮点击都新建对象，也不要自己再发送重复的启动事件。
- 状态目录应稳定、用户可写并限制访问权限；不要使用临时目录或每次启动生成的新目录。
- 复制状态目录会复制安装标识和待发送事件；不要把已初始化状态库随安装包分发。
- `autostart=False` 只禁用后台发送线程，**不禁用启动事件**。
- `flush_once()` 是同步尝试，主要用于测试；不要放到 UI 主线程。
- `close()` 后不要继续使用对象或读取 `pending`。
- 默认统计时区与 `Asia/Shanghai` 对齐。更换服务器统计时区时应传入匹配的 `statistics_timezone`；使用 `zoneinfo` 时需确保系统有时区数据。
- 支持离线重试但不是零丢失：队列默认 10,000 条，满时丢弃新事件；发生时间超过服务器七天窗口的事件会被拒绝。
- `pending`、`last_error`、`rejected_count`、`overflow_count` 可用于接入调试。持续 403/404 需修复配置，不能只等待自动重试。

其他语言也可以直接使用下一节的 HTTP 协议。当前没有 C#、Electron、Qt 等框架专用 SDK，不要将 Python 示例误当成所有客户端已自动接入。

## 4. 通用 HTTP 协议

### 4.1 请求格式

```http
POST /api/collect
Content-Type: application/json
```

```json
{
  "app_id": "YOUR_DESKTOP_APP_ID",
  "events": [
    {
      "schema_version": 1,
      "event_id": "每个事件生成一个UUID",
      "event_type": "app_start",
      "occurred_at": "2026-10-31T08:00:00Z",
      "anonymous_id": "持久安装或访客标识",
      "session_id": "本次运行或访问会话标识",
      "app_version": "1.0.0",
      "platform": "windows",
      "sdk_version": "my-sdk/1.0"
    }
  ]
}
```

**示例日期必须替换为事件真实发生时间**，不能把固定日期写进客户端。

| 字段 | 必填 | 约束 |
| --- | --- | --- |
| 顶层 `app_id` | 是 | 后台创建的应用 ID，非空，最多 128 字节 |
| 顶层 `events` | 是 | 每批 1–500 条，请求体不超过 1 MiB |
| `schema_version` | 是 | 整数 `1` |
| `event_id` | 是 | 唯一事件 ID；重试保持不变 |
| `event_type` | 是 | Desktop：`app_start`、`active`；Web：`page_view`、`active` |
| `occurred_at` | 是 | 带时区的 RFC3339 字符串，推荐 UTC `Z` |
| `anonymous_id` | 是 | 安装或浏览器标识；正式接入须持久化 |
| `session_id` | 是 | 本次运行/访问会话标识 |
| `app_version` | 否 | 字符串，最多 128 字节 |
| `platform` | 否 | 字符串，最多 64 字节 |
| `sdk_version` | 否 | 字符串，最多 64 字节 |
| `path` | `page_view` 必需 | 以 `/` 开头、不含主机名、不能以 `//` 开头，最多 2048 字节 |

三个事件标识字段须为非空字符串，各 1–128 UTF-8 字节，不含控制字符；推荐使用 UUID。版本、平台字段也不能含控制字符。

当前不支持 `custom` 事件、任意 `properties` 或通用 metadata 存储。不要依赖未定义字段进入统计。

服务器会剔除页面路径中的 query 和 fragment，但**不会自动清理路径段**里的邮箱、用户 ID、令牌等敏感值。需要在业务侧做路径归一化，例如把 `/users/12345` 记录为 `/users/:id`。

### 4.2 身份、事件和时间

```text
anonymous_id：同一安装长期不变
session_id：每次客户端启动换一个；网页由 SDK 按会话规则管理
event_id：每条事件不同；同条事件的重试完全不变
```

- 不使用 IP、MAC 地址或硬件序列号识别用户。
- 桌面客户端每次有效启动发送一次 `app_start`；常驻应用需根据真实活动发送 `active`，才能记录后续日期的日活。
- 服务器接受过去 7×24 小时至未来五分钟内的事件，依据服务器时间校验。
- 统计日按事件发生时间和服务器统计时区计算，不按请求到达时间。
- 网络失败后补发保留发生时间；**不要改成补发时的时间**。
- 七天是接收窗口，不是去重记录的七天 TTL。事件已接收后，超窗重发仍可能被拒绝为 `invalid_time`。

### 4.3 响应与重试

成功提交返回 HTTP 200：

```json
{
  "accepted": 1,
  "duplicates": 1,
  "rejected": [
    {
      "event_id": "原始事件ID",
      "code": "invalid_time",
      "message": "..."
    }
  ]
}
```

- `accepted`：新接收事件数。
- `duplicates`：已经接收过的重复事件数。
- `rejected`：逐条校验失败事件。
- 三者数量合计应等于本批提交数。
- HTTP 200 **不代表所有事件都合法**，全部拒绝也可返回 200。
- 完整确认后可以删除该批本地事件；逐条拒绝的数据不要原样无限重试。
- 网络超时或未取得完整确认时，保留原事件并退避重试。按 `(app_id, event_id)` 去重，避免因超时重发重复计数。

整批错误响应格式：

```json
{"error":{"code":"origin_forbidden","message":"..."}}
```

当前采集按应用约 2 请求/秒、突发 60 请求限流，多个客户端或浏览器共享应用额度。使用批量发送，不要每条事件单独请求；上线前根据目标规模核对容量。

> Python 示例校验完整响应再出队。网页 SDK 的处理更简化：成功响应或除 429 外的 4xx 会移除整批，网络/5xx/429 保留重试，不提供逐条拒绝诊断界面。调试网页接入时应在 Network 中直接查看请求响应。

### 4.4 可运行的桌面协议测试

下面示例适用于 Bash/Git Bash，要求 Python 3.9+ 和支持 `--fail-with-body` 的 curl。**先创建专门的 Desktop 测试应用**，避免把合成事件写入正式产品统计。

```bash
APP_ID='YOUR_DESKTOP_TEST_APP_ID'

payload=$(python -I - "$APP_ID" <<'PY'
import datetime as dt
import json
import sys
import uuid

print(json.dumps({
    'app_id': sys.argv[1],
    'events': [{
        'schema_version': 1,
        'event_id': str(uuid.uuid4()),
        'event_type': 'app_start',
        'occurred_at': dt.datetime.now(dt.timezone.utc).isoformat().replace('+00:00', 'Z'),
        'anonymous_id': str(uuid.uuid4()),
        'session_id': str(uuid.uuid4()),
        'app_version': 'integration-test',
        'platform': 'test',
    }],
}))
PY
)

printf '%s' "$payload" | curl -sS --fail-with-body \
  -H 'Content-Type: application/json' \
  --data-binary @- \
  'https://web.mcylyr.cn/api/collect'
```

首次应得到 `accepted=1`、`duplicates=0`、空拒绝列表。**只重复最后的 curl 发送，不重新生成 `payload`**，应得到 `accepted=0`、`duplicates=1`，后台启动数保持 1。

该测试每次生成新的匿名标识，不能代替正式客户端的持久身份。Desktop 测试请求不带 `Origin`。Web 应用的直接 HTTP 测试则必须使用 `page_view`/`active` 和白名单中的真实 Origin，并为 `page_view` 提供路径。

## 5. 首次接入验收

### 网页

1. 为测试站点创建 Web 应用并配置正确来源。
2. 在站点入口添加一段 SDK；保持页面可见。
3. 打开开发者工具 Network，确认 `sdk.js` 加载成功。
4. 等待约 10–20 秒，检查 `/api/collect` 的请求和响应。
5. 后台选择该应用及当天日期，点击「查询」。
6. 同一浏览器当天访问两个页面：通常日活 1、PV 2；五分钟合并期间不应按交互次数不断增长日活。
7. SPA 路由切换应产生一次对应 PV，不应由自动和手动两套钩子双计。

Web 可能遇到拦截器、多标签并发和本地存储限制，测试时应控制这些条件，不把匿名标识等同于真实人数。

### 桌面客户端

1. 创建 Desktop 测试应用并用稳定状态目录接入。
2. 当天启动五次，保持同一安装标识：预期日活 1、启动 5。
3. 真实交互调用 `mark_active()`，不要仅靠后台空闲定时器计日活。
4. 重复发送同一个事件，启动数不增加。
5. 断网产生事件、恢复网络后确认重试；超过七天的补报会被拒绝。
6. 核对应用版本和平台；默认未传版本时，后台可能显示「未知版本」。
7. 测试后停用测试应用；当前没有后台删除历史数据功能，不要用生产应用反复发送合成数据。

## 6. 指标怎么理解

| 指标 | 当前口径 |
| --- | --- |
| 当日日活 | 同应用、同统计日，产生任意合法事件的匿名标识去重数；启动和 PV 也计活跃 |
| 区间活跃 | 在整个查询区间对匿名标识去重，不是每日 DAU 相加 |
| 启动次数 | 接受且不重复的 Desktop `app_start` 事件条数 |
| 网站会话 | 每应用唯一 `session_id` 的数量，归属到已知最早事件日 |
| 页面浏览 PV | 接受且不重复的 Web `page_view` 事件条数 |
| 新增访客 | 匿名标识的已知最早有效事件日在所选日期内的数量，不是注册人数 |
| 总览活跃 | 各应用区间去重后相加，同一人在多应用中可能重复 |

版本/平台分组中的同一访客也可能重复出现在不同组合中。更早的离线补报可能修正新增访客或会话的日期归属。

**统计按安装/浏览器标识，不做跨设备、跨应用或客户端与网站之间的统一身份合并。** 当前客户端自报事件不能证明真人、授权或准确自然人数，不应用于计费、考勤等可信证明场景。

## 7. 常见错误排查

| 现象 / 错误 | 排查方法 |
| --- | --- |
| 后台登录提示来源不允许 | 浏览器 Origin 必须与服务端 `PUBLIC_ORIGIN` 完全一致；当前管理入口是 `https://web.mcylyr.cn`，HTTP/localhost/其他端口不等价 |
| 网页上报 `403 origin_forbidden` | 检查 Web 应用的 `allowed_origins` 是否包含实际网站 Origin，不能只添加统计后台域名 |
| Desktop 上报 `403 origin_forbidden` | 不附加浏览器 Origin；如框架自动带 Origin，需在应用中配置该精确来源 |
| `403 app_disabled` | 在后台启用对应应用 |
| `404 app_not_found` | 检查应用 ID；Web 与 Desktop 不要混用 ID |
| 请求 200 但没有新增数据 | 查看 `rejected` 和 `duplicates`；不能只依据 HTTP 状态判断全部接收 |
| `invalid_event_type` | 检查应用类型：网站不接收 `app_start`，Desktop 不接收 `page_view` |
| `invalid_time` | 检查服务器/客户端时钟及 UTC 时区；过去七天、未来五分钟之外会拒绝 |
| `invalid_path` | `page_view` 使用纯路径如 `/settings`，不是完整 URL，长度不超过 2048 字节 |
| `400 invalid_json / invalid_batch` | 顶层字段、JSON 类型、每批数量和请求体大小须符合协议 |
| `429 rate_limited` | 批量发送、减少请求频率并退避；同应用额度由所有接入端共享 |
| SDK 未加载或无请求 | 检查 CSP、拦截器、脚本执行顺序、停用标记、页面是否可见，以及正确域名 |
| SPA PV 重复 | 删除叠加在自动路由监听上的手动 `trackPage()` 调用 |
| 页面统计全是 `/` | Hash 网站需显式开启 `data-hash-mode="route"` |
| 常驻客户端后续日期无日活 | 只发送启动不够，需要在真实使用期间调用活跃上报 |
| 登录后没有 Cookie 或会话失效 | 当前安全 Cookie 要求 HTTPS；不要用回环 HTTP 或禁用证书校验测试正式入口 |
| 域名跳到其他登录系统，API 404 | 检查本域名 A/AAAA 是否指向同一统计服务，尤其错误的 IPv6 分流 |
| 图表仍为空 | 检查日期范围、统计时区、所选应用、发送周期，并点击后台「查询」 |

**`PUBLIC_ORIGIN` 与 `allowed_origins` 是两件事：** 前者保护管理后台写请求，后者控制各应用的采集来源。修正其中一个不会自动修正另一个。

## 8. 安全和维护边界

- 使用 HTTPS，不关闭 TLS 证书校验。
- 应用 ID 是公开标识，Origin 校验与限流不是客户端身份认证。
- 不收集密码、令牌、用户输入或完整敏感 URL；页面路径和版本字段同样需要审查。
- 更改统计时区不会自动重算历史日期，请不要在已有正式数据后随意修改。
- 当前没有自动保留期、个人数据删除、聚合重建或自定义事件分析功能。
- 队列有上限且浏览器可能拦截，上报不能代替业务数据库。
- 备份和磁盘容量监控属于服务运维；SDK 接入不代表自动配置了备份任务。

进一步参考：

- [采集与管理 API 协议](protocol.md)
- [网页 SDK 详细说明](web-sdk.md)
- [Python 示例说明](../examples/python/README.md)
- [统计指标口径](metrics.md)
- [数据库备份、恢复与升级](operations.md)
