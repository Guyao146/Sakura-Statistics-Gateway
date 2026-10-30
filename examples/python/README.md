# Python 通用 HTTP 客户端示例

Python 3.9+，仅标准库，无 pip 依赖。示例适配管理台创建的 **desktop** 应用（启用状态），发送 `app_start` 与 `active`。其他语言和桌面框架尚未接入；网页已有独立的 [/sdk.js 接入文档](../../docs/web-sdk.md)。`web` 应用需浏览器 Origin 白名单和 page_view 生命周期，不能把本示例当作浏览器 SDK。

```sh
python examples/python/demo.py --endpoint http://127.0.0.1:8080 --app-id YOUR_DESKTOP_APP_ID
python -m unittest discover -s examples/python -v
```

生产使用 HTTPS 地址，确保系统 Python 信任服务端证书。本机 Caddy CA 也需正确安装信任，不要禁用 TLS 校验。示例不会发送管理员 Cookie 或密码。

将 `sakura_sdk.py` 放入应用可导入的目录，在应用生命周期创建一个客户端：

```python
from pathlib import Path
from sakura_sdk import Client

telemetry = Client(
    "https://stats.example.com",
    "YOUR_DESKTOP_APP_ID",
    Path.home() / ".my-app" / "telemetry",
    app_version="1.0.0",
    platform="windows",
)

# 在实际点击、输入或其他真实使用行为处理函数中调用。
def on_real_user_action():
    telemetry.mark_active()

# 程序退出时调用，不在退出路径发起同步网络请求。
telemetry.close()
```

每个应用/进程使用一个客户端。状态目录应位于当前用户可写的应用数据目录，跨重启保持稳定并限制权限；不要放进软件安装目录、发布包、公共共享目录或临时目录。SQLite 存储匿名安装 UUID 和未发送事件，不使用硬件标识。卸载后保留还是删除此目录应由产品明确决定；复制此目录会复制匿名身份和未发送事件。

- `anonymous_id` 首次创建后存入 SQLite，后续复用；`session_id` 每次进程启动生成一次，进程内不变。
- 每个状态目录/应用在进程内首次构造时尝试入队一次 `app_start`。启动并不自动创建 `active`。队列满时本次启动可能丢弃，不会谎称可靠送达。
- `mark_active()` 必须由真实使用触发。同统计日 5 分钟内合并，跨统计日首次交互立即入队，不因前一天的合并窗口遗漏。计时使用单调时钟，日界使用统计时区。
- 默认统计时区为固定 UTC+8，与服务端默认 Asia/Shanghai 一致。更换服务端时区时应传入匹配的 `datetime.tzinfo`；可用标准库 `zoneinfo.ZoneInfo`，但系统需有该时区数据。改变服务端时区不会重算已有事件。
- 默认队列上限 10,000 条；满时拒绝新事件，保留原有队列，并增加 `overflow_count`。此上限约束行数，不是 SQLite 文件字节上限；SQLite 页面、WAL、业务字符串长度仍占空间，路径不可写/磁盘已满时本地入队可抛异常，宿主应按自身错误处理策略处理。
- 后台 daemon 线程批量发送，每批默认 100 条。网络失败、HTTP 错误或不完整确认保留原始事件 ID/时间/内容，指数退避加 jitter，最大约 60 秒。新事件不会绕过失败退避。
- HTTP 200 完整确认中的 accepted/duplicates 删除本地事件；明确 rejected 也出队，增加 `rejected_count`，通过 `last_error` 暴露错误。被服务端拒绝的数据不保证可恢复，不要忽略长期错误。
- 默认 HTTP 超时 5 秒。`close()` 默认最多等待线程 0.2 秒，不主动 flush；仍在网络请求中的线程为 daemon，不阻止解释器退出，未确认事件保留到下次启动。响应丢失后重发会由服务端去重。服务端只接收最近 7 天事件，长期离线后可能被拒绝。

事件产生时会有一次本地 SQLite 写入，异步只针对网络发送，不代表完全没有本地 I/O。`flush_once()` 是同步发送接口，主要供测试，不应放在 UI 线程或程序退出路径。后台 `last_error`、`overflow_count` 和 `rejected_count` 是内存诊断信息，进程重启后清零。

测试覆盖安装 ID 持久化与分离、网络状态不确定后固定事件重试、队列容量、跨日活跃合并、不完整确认保留、明确拒绝和慢网络下快速退出。示例没有自动活跃定时器、真实用户鉴别、跨端合并、用户同意 UI 或远程配置功能。集成方应根据产品需要处理采集开关和用户告知。
