# 首版验证记录

验证日期：2026-10-30。业务测试使用隔离数据库和明确标记的合成数据，不包含生产用户或生产统计。

## 已完成

- 代码已推送至 [GitHub 公共仓库](https://github.com/Guyao146/Sakura-Statistics-Gateway)。
- [首次镜像发布](https://github.com/Guyao146/Sakura-Statistics-Gateway/actions/runs/41041901315)全部成功：`ghcr.io/guyao146/sakura-statistics-gateway` 已发布 `linux/amd64`、`linux/arm64` 镜像；GHCR 匿名 token 和 manifest 已验证可用，使用公开镜像无需服务器登录。
- [固定首版发布](https://github.com/Guyao146/Sakura-Statistics-Gateway/actions/runs/41043699773)和[首版 Compose 验收](https://github.com/Guyao146/Sakura-Statistics-Gateway/actions/runs/41044000434)均通过。`v0.1.0` 的公开标签与部署包 digest 一致：`sha256:a74a5559c3a050539e18f3af6fd1f98f7fe7f2821287c6b8851f22c50a81c956`；Release 已提供部署包和 SHA256 校验文件。
- CI：Go 测试、go vet、构建、真实 PostgreSQL 16 集成测试及 Linux race 检测全部通过。
- 前端：21 项网页 SDK 测试和 Vue/TypeScript 生产构建通过，覆盖存储不可用、网络重试、事件确认、路由、停用后身份轮换及统计时区午夜活动。
- Python：客户端 7 项测试、备份/恢复工具 5 项模拟调用测试通过；模拟测试覆盖拒绝覆盖备份、失败文件清理、拒绝非空库恢复等行为。
- [实际 Compose 集成验收](https://github.com/Guyao146/Sakura-Statistics-Gateway/actions/runs/41042208666)成功：在 Linux amd64 隔离环境拉取最初已发布镜像的固定 digest，完成 Compose 启动、PostgreSQL 16 与应用 readiness、HTML 和 `/sdk.js` 加载、Cookie 登录、desktop 应用创建、启动事件上报、重复上报去重及 stats 校验。统计结果为活跃 1、启动 1、新增 1、网站会话 0。
- 本地补充：Go 1.27.3 的 gofmt、单元测试、go vet、Windows 构建，以及 PostgreSQL 17.7 的三项真实集成测试通过；`CGO_ENABLED=0` 的 Linux amd64/arm64 交叉编译成功。
- 数据库业务覆盖迁移重复执行、管理员初始化、应用管理、部分接收、同批重复和网络重试、补报修正、跨日活跃去重、版本/路径明细、桌面启动与网站会话区分、各应用活跃相加。
- 真实浏览器管理台联调通过 Cookie 登录、Python 接入数据总览、Web/Desktop 应用创建、网站来源配置、详情空态和应用启停。Python SDK → HTTP 服务 → PostgreSQL → 管理报表的启动/活动统计及队列确认通过。
- Compose/工作流的 YAML 和关键配置检查通过；基础部署只引用镜像，数据库端口不公开，应用端口仅绑定回环地址。

## 固定版本部署

首版使用 `v0.1.0` 标签，部署入口为 [Release 页面](https://github.com/Guyao146/Sakura-Statistics-Gateway/releases/tag/v0.1.0)与[部署包下载](https://github.com/Guyao146/Sakura-Statistics-Gateway/releases/download/v0.1.0/sakura-deployment.tar.gz)。部署包固定对应发布镜像的 digest，服务器无需源码构建。

上面的流水线链接记录具体测试运行及被测镜像；后续版本不能仅沿用旧版本的验收结论。

## 尚未完成

- Caddy 在真实域名上的证书签发和 HTTPS 验收。
- ARM 容器实际运行；已完成 arm64 镜像发布和交叉编译，不等于运行验证。
- 可见网页的 SDK → 服务端 → 数据库 → 图表全链路。脚本已从真实服务加载，但测试浏览器页面始终为 `hidden`，按设计未自动采集。
- 真实容器数据库的备份及独立空库恢复演练；工具模拟测试不能替代实际恢复验证。

部署到真实环境后仍需检查域名 TLS、readiness 和管理台，并在独立空库演练恢复；使用 ARM 服务器时补做实际运行验收。
