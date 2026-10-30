# 网页 SDK

构建后的服务提供 `/sdk.js`，源文件为 `web/public/sdk.js`。在管理台创建 **web** 应用，将网站的精确 Origin（含协议、主机、非默认端口，无尾部斜线）加入 `allowed_origins`，如 `https://www.example.com`。统计服务自身域名不等于访问网站的域名，白名单要填写实际载入脚本的页面 Origin。

```html
<script defer
  src="https://stats.example.com/sdk.js"
  data-app-id="YOUR_WEB_APP_ID"
  data-endpoint="https://stats.example.com/api/collect"></script>
```

脚本默认相对于 SDK 来源解析 `/api/collect`。显式设置 endpoint 更清楚。每个页面使用一个实例；使用 HTTPS，按网站 CSP 放行统计服务的 script-src/connect-src。采集不使用管理员 Cookie。

SDK 记录可见页面首次 page_view，并监听 `history.pushState`、`replaceState`、`popstate`，相同自动路径不会重复上报；`SakuraStats.trackPage('/settings')` 可显式记录路径。默认丢弃 query/hash；显式设置 `data-hash-mode="route"` 时，将 `#/...` 路由编码为普通路径，例如 `/home#/settings?private=1` 记录为 `/home/~hash/settings`，`/#/settings` 记录为 `/~hash/settings`，因此后台可以区分 hash 子路由。hash 内查询参数与普通 `#anchor` 不进入路径。避免在任何路径中放个人信息。

匿名 ID 保存在当前站点 localStorage，多个标签页可共享 session；30 分钟无事件后新建 session。服务端按 session 的最早事件日统计，跨午夜不自动拆分。存储被禁用时退化为内存，刷新后不保证身份或队列延续；跨站点存储隔离，不做跨端或跨站合并。localStorage 不提供跨标签原子事务，多个标签恰好同时首次建会话或更新活跃窗口时仍可能分裂会话或重复发送；服务端仅按 event_id 去重。

活动事件要求页面可见且最近 60 秒有交互；首次加载视作最近交互，之后监听 pointerdown/keydown/touchstart/scroll。同 session 的 active 通常最多每 5 分钟记录一次；进入新的统计日后首次有效活动可立即记录。`data-timezone` 默认 `Asia/Shanghai`，应与服务端 `STATISTICS_TIMEZONE` 一致；管理台生成的接入代码会带上该时区。无效时区回退到默认值。轮询只会在页面可见及活动条件满足时工作。这些事件不构成真实用户证明。

本地队列最多 200 条，满时丢弃最老事件；每批 30 条。网络、5xx、429 会退避重试，事件 ID 保持不变；其他 4xx 和成功响应会移除批次，SDK 不暴露逐条 rejection 的诊断界面。HTTP 成功即按整批已确认处理，不逐字段校验返回数量。超过 7 天的本地事件清除。pagehide 尝试 sendBeacon，因拿不到确认会保留原 ID，下次重复上报交给服务端去重。浏览器关闭、存储清理和网络错误都可能造成丢失，不保证完整送达。

## 停用与重新启用

```js
window.SakuraStats.disable();  // 停止新采集，清空本地待发事件及身份/session，保存停用标记
window.SakuraStats.enable();   // 清除停用标记，以新的匿名身份恢复采集
window.SakuraStats.trackPage('/settings');
window.SakuraStats.flush();    // 返回 Promise；仍受退避与停用状态控制
```

停用标记按应用保存在当前站点 localStorage，并通过 storage 事件同步其他标签页；存储不可用时无法跨刷新持久化。已发出并被服务端接受的请求无法撤回，disable 不会删除服务器历史数据。第一版没有服务端个人数据删除或保留期清理接口。

若产品要求先同意后采集，应在获得同意后才插入 SDK script；脚本载入默认即会采集首次页面访问，加载后再 disable 不等同于事先阻止采集。可由产品设置 UI 调用 disable/enable，SDK 本身不提供同意管理 UI。

采集按应用约 2 请求/秒、突发 60，众多浏览器或桌面安装共享该额度。示例适合第一版规模；并发接入前应评估容量，限流后客户端本地重试也不能保证不丢数据。所有客户端事件均为自报，Origin 校验不会证明事件真实性。
