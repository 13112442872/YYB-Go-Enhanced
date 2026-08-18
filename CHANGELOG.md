# 更新日志

本项目按实际提交时间记录主要功能变化，便于部署后确认版本内容。

## 2026-08-18

- Docker 多架构构建移除可下载 Action，改为工作流内生成 GHCR 标签，并为代码拉取、QEMU、Buildx、登录与构建推送增加最多 3 次重试，消除 GitHub/Codeload 在 Set up job 阶段下载 Action 失败的路径。
- 新增独立 Magisk Actions：主分支自动编译校验 ARM64 模块，`magisk-v*` 标签自动创建 Release 并上传可安装 ZIP；Docker 与 Magisk 构建互不影响。
- 新增 `scripts/weile_coin.py`：使用 YYB 动态登录微乐小游戏，支持多账号查询每日任务，并领取 HAR 已验证的分享福利金币和订阅更新金币。

## 2026-08-17

- 增加账号独立代理：扫码、本机微信授权、OAuth 回调、凭据刷新、用户资料、`wx.login`、LongLink 和 ShortLink 统一使用账号代理。
- 支持直连、静态代理和动态代理 API，兼容 HTTP CONNECT、SOCKS5、用户名密码认证，以及 `txt`、`json`、`json2` 和常见嵌套响应；代理 API 可自行携带省市参数。
- 账号显式代理失败时不回退直连；控制台可按账号读取、测试和保存代理，添加账号前也可指定代理。
- 将账号代理从工作台窄栏移到独立“代理设置”页面，支持按账号纵向切换、搜索、测试和保存；工作台保留当前代理摘要和账号上下文返回，修复长代理 API 地址穿出调用配置区域及旧静态脚本缓存问题。
- 默认 Compose 从 `yyb-go + nginx` 两容器收敛为单个 `yyb-go` 容器直接映射 8000；应用内登录继续负责控制台认证，外部 HTTPS 反代按需单独配置。
- 记录 refresh token 的首次观察时间；连续使用约 25 天后在账号卡片显示“建议重扫”。保活仍只承诺刷新微信实际允许续期的凭据，不再暗示 refresh token 可无限续期。
- 将已通过官方 Magisk 真机安装、进入控制台和扫码验证的 Android ARM64 常驻模块合入主分支；稳定包为 [Magisk v0.1.4](https://github.com/525815266/YYB-Go-Enhanced/releases/tag/magisk-v0.1.4)。
- Magisk v0.1.4 修复 Windows 打包导致 `config.conf.example` 使用 CRLF、`PORT` 被解析为 `8000\r` 而无法启动的问题；启动时会自动修复已有持久化配置，并扩大安装包换行检查范围。
- Magisk 运行时增加可配置 DNS 解析器，默认避开 Android 静态程序无法访问的 `[::1]:53`，修复微信登录二维码域名解析失败。

## 2026-08-14

- Web 用户与会话默认改用持久化 SQLite，首个注册账号自动成为管理员；支持通过 `YYB_AUTH_DRIVER` 切换 MySQL 或关闭认证，并继续兼容旧的 `YYB_AUTH_MYSQL_DSN`。
- 微信 HTTPDNS 无响应或缺少 LongLink 候选时，回退到官方 `longcloud.weixin.com:443`，避免在普通 DNS 和 443 端口可用时直接返回 502。
- `/wx/encryptkey` 改为必须提供目标业务的真实 `payload`，不再发送已知无效的空 `getUserEncryptKey` 请求；同步更新控制台与 OpenAPI。
- 补充文章会话、文章扩展、点赞和业务 `encryptData` 的调用边界，明确兼容路由不会自动推导业务参数。

## 2026-08-13

- 按管理平台架构统一工作台、添加账号、运行管理、用户管理和个人设置，增加固定侧栏、顶栏、当前用户身份和移动端导航。
- 用户新增与密码重置改为完整表单对话框，移除浏览器 `prompt` 交互并增加表单内错误反馈。
- 运行日志改为右侧抽屉，自动刷新时保留当前阅读位置；修复青龙 `/open/logs` 响应超过 2 MB 后被截断并报 `unexpected end of JSON input` 的问题。
- 将 Nginx Basic Auth 替换为应用内登录页面，增加注册、退出、个人设置、管理员用户管理和角色权限；用户及网页登录会话使用 MySQL，微信协议数据继续使用 SQLite。
- 网页管理路由使用哈希 Session Cookie 和登录限速；`/wx/*`、`/wxapp/*` 保持兼容，不要求网页登录。
- 合入 [PR #9](https://github.com/525815266/YYB-Go-Enhanced/pull/9)，增加呆呆面板（daidai-panel）支持、青龙/呆呆统一面板驱动和 GHCR 多架构镜像构建工作流。
- 修复面板适配引入的青龙兼容问题：青龙任务启停继续以 `isDisabled` 判断，运行状态优先使用青龙 `status`，不会因残留 PID 被误判为运行中。
- 呆呆面板删除任务失败时不再忽略错误；增加青龙状态兼容测试，并通过全量测试与静态检查。

## 2026-08-06

- [7408e0b](https://github.com/525815266/YYB-Go-Enhanced/commit/7408e0b) 将二维码授权加入首页“调用配置”，不选账号也可直接创建授权会话。
- [7ec2917](https://github.com/525815266/YYB-Go-Enhanced/commit/7ec2917) 新增截图所示的 `/wx/*` 兼容接口：`/wx/code`、`/wx/getuserinfo`、`/wx/encryptkey`、`/wx/getphonenumber`、`/wx/cloud`、`/wx/qrcodeauth`、`/wx/mpgeta8key`、`/wx/appmsgext` 和 `/wx/appmsglike`；其中云函数及文章相关接口复用 `operateWxData`，不会伪造微信结果。
- [a89cb04](https://github.com/525815266/YYB-Go-Enhanced/commit/a89cb04) 将公众号网页授权并入首页“调用配置”。选择“公众号网页授权”后，可填写公众号 AppID、回调地址、授权范围和 State，并生成官方 OAuth 授权链接。
- [2e09fc5](https://github.com/525815266/YYB-Go-Enhanced/commit/2e09fc5) 新增 `POST /wx/oauth`，校验公众号 AppID、回调地址和授权参数；不伪造授权 code，用户授权后由回调地址接收 code。

## 2026-08-05

- [c2295cb](https://github.com/525815266/YYB-Go-Enhanced/commit/c2295cb) 增加本机微信快速授权，并保留手机扫码作为回退方式。

## 2026-08-04

- [dc74f47](https://github.com/525815266/YYB-Go-Enhanced/commit/dc74f47) 增加青龙一键同步和账号备注，备注可参与账号任务管理。
- [f3e4599](https://github.com/525815266/YYB-Go-Enhanced/commit/f3e4599) 增加账号级联删除，清理对应的 YYB 数据、青龙环境变量和专属任务。

## 2026-08-03

- [47e8fd8](https://github.com/525815266/YYB-Go-Enhanced/commit/47e8fd8) 收录修复后的青龙脚本。
- [c9fb7e7](https://github.com/525815266/YYB-Go-Enhanced/commit/c9fb7e7) 修复 `wx.login` 返回空 code 时的会话重建逻辑。

## 2026-07-31

- [24a1ae8](https://github.com/525815266/YYB-Go-Enhanced/commit/24a1ae8) 增加账号凭据主动保活和提前续期。
- [b462dd5](https://github.com/525815266/YYB-Go-Enhanced/commit/b462dd5)、[2e0b691](https://github.com/525815266/YYB-Go-Enhanced/commit/2e0b691)、[75f4461](https://github.com/525815266/YYB-Go-Enhanced/commit/75f4461) 增加青龙账号级任务运行、开关和日志查询，并修复任务状态显示。
- [afba7fd](https://github.com/525815266/YYB-Go-Enhanced/commit/afba7fd) 修复运行日志刷新时强制跳回顶部的问题，保留当前阅读位置。

## 2026-07-30

- [a0cb5bb](https://github.com/525815266/YYB-Go-Enhanced/commit/a0cb5bb) 发布增强版基础功能：微信扫码登录、账号与 OpenID 管理、`wx.login` code 获取、SQLite 持久化、Docker 部署和青龙接入。

## 当前边界

公众号功能是网页 OAuth 授权链接生成，不是微信公众号后台登录。公众号后台需要其官方管理员登录；OAuth 授权成功后的 `code` 会回调到公众号后台配置的授权域名。
