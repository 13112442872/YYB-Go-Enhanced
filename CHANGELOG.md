# 更新日志

本项目按实际提交时间记录主要功能变化，便于部署后确认版本内容。

## 2026-08-06

- 新增截图所示的 `/wx/*` 兼容接口：`/wx/code`、`/wx/getuserinfo`、`/wx/encryptkey`、`/wx/getphonenumber`、`/wx/cloud`、`/wx/qrcodeauth`、`/wx/mpgeta8key`、`/wx/appmsgext` 和 `/wx/appmsglike`；其中云函数及文章相关接口复用 `operateWxData`，不会伪造微信结果。
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
