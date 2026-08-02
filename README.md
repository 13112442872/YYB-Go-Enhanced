# YYB Go Enhanced

应用宝协议服务增强版，提供微信扫码登录、账号与 OpenID 管理、`wx.login` code 获取、凭据按需续期、Web 控制台，以及 Docker 和青龙接入。

## 功能

- 微信扫码添加账号，扫码成功后显示账号 ID、OpenID 和存活状态
- Web 控制台管理账号并复制 OpenID
- 提供 `getCode`、`getPhoneNumber` 和 `operateWxData` 接口
- 应用宝短期凭据接近失效时由后台任务主动续期，业务调用失败时也会按需续期
- SQLite 持久化账号与协议会话
- Nginx Basic Auth 保护公开的 Web 入口
- 支持与青龙容器共享 Docker 网络
- 账号运行管理：每个微信账号独立创建、启停和运行青龙脚本，并查看日志
- 账号独立推送：支持 Server酱、PushPlus 和企业微信机器人，密钥只保存在青龙环境变量

## Docker Compose 部署

运行环境需要 Docker、Docker Compose，以及名为 `qinglong_default` 的 Docker 网络。如果没有青龙，也可以先创建同名网络：

```bash
docker network create qinglong_default
```

创建本地配置：

```bash
cp .env.example .env
```

编辑 `.env`，至少修改 `YYB_WEB_PASSWORD`。随后构建并启动：

```bash
docker compose up -d --build
```

默认访问地址为 `http://服务器IP:8000`。登录用户名和密码由 `.env` 中的 `YYB_WEB_USER`、`YYB_WEB_PASSWORD` 决定。

如果只允许某个局域网地址监听，可设置：

```dotenv
YYB_BIND_ADDRESS=192.168.1.10
```

## 自动保活

服务默认每 30 分钟检查一次账号，并在 access token 剩余不足 45 分钟时，通过 refresh token 更新 access token、refresh token 和 login buffer。该过程不会生成未消费的 `wx.login` code。

可以在 `.env` 中调整：

```dotenv
YYB_KEEPALIVE_INTERVAL=30m
YYB_KEEPALIVE_AHEAD=45m
```

将 `YYB_KEEPALIVE_INTERVAL` 设为 `0` 可关闭后台保活。提前续期遇到临时网络失败时会保留当前账号状态并在后续周期重试；凭据真正过期或 refresh token 被服务端撤销后仍然需要重新扫码。

## 青龙接入

当青龙和本服务都连接到 `qinglong_default` 网络后，青龙环境变量可以填写：

```text
YYB_SERVER=yyb-go:8000@1
```

`@` 后可以使用控制台显示的账号 ID 或 OpenID。账号 ID 是本地数据库编号，删除并重新添加账号后可能变化；OpenID 更适合长期配置。

已确认报错的青龙脚本修复版收录在 [`scripts/`](scripts/README.md)。

## API 示例

获取 `wx.login` code：

```bash
curl -X POST http://yyb-go:8000/wxapp/getCode \
  -H 'Content-Type: application/json' \
  -d '{"ref":"1","app_id":"wx0000000000000000"}'
```

主动刷新单个账号状态：

```bash
curl -X POST http://yyb-go:8000/accounts/refresh \
  -H 'Content-Type: application/json' \
  -d '{"ref":"1"}'
```

Web 控制台内还提供完整的 OpenAPI 文档入口。

## 账号运行管理

在 `.env` 中配置青龙 OpenAPI 后，打开 `/runs`：

```dotenv
QL_URL=http://qinglong:5700
QL_CLIENT_ID=你的青龙应用 Client ID
QL_CLIENT_SECRET=你的青龙应用 Client Secret
YYB_QINGLONG_SERVER=yyb-go:8000
YYB_QINGLONG_REPO=SuperNaiBA_YYB-GO-Script
```

管理页只发现订阅中直接使用 `YYB_SERVER` 的 `.js` 和 `.py` 任务。每个“账号 + 脚本”会创建一个独立青龙任务，新任务默认关闭；手动点击“运行一次”才会立即执行。账号变量通过青龙 `task_before` 注入，运行日志按“账号 + 脚本”写入独立目录，管理页只读取当前账号的目录。账号推送 Token 不写入任务命令和 YYB 数据库，接口也不会返回明文。

如果原订阅生成的全局任务仍在运行，管理页会显示重复运行提示。迁移到账号任务后，请在青龙中停用对应的旧全局任务。

## 数据与安全

- 不要提交 `.env`、`data/`、SQLite 数据库、登录凭据或真实 OpenID。
- 建议仅在可信局域网内运行，不要把内部的 `yyb-go:8000` 接口直接暴露到公网。
- `wx.login` code 是短期且一次性的；refresh token 也可能被服务端撤销，失效后需要重新扫码。
- 本项目仅供学习和个人研究使用，请遵守相关平台条款及所在地法律法规。

## 来源说明

本项目基于 [SuperNaiBA/YYB_GO](https://github.com/SuperNaiBA/YYB_GO) 整理和增强，主要补充了账号信息展示、OpenID 可见性、Web 控制台资源修复、Docker 部署与访问保护。请同时遵守上游项目的授权条件；如需分发或商业使用，请先取得相应权利人的许可。
