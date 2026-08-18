#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# name: 微乐金币活动
# cron: 20 8 * * *

"""微乐小游戏活动金币领取，支持 YYB_SERVER 多账号。

环境变量：
  YYB_SERVER             必填，每行“YYB地址@账号ID或OpenID”
  WEILE_DRY_RUN           可选，设为 1 时只登录并查询，不领取
  WEILE_SHARE_CLAIM_MAX   可选，每个账号本次最多领取几次分享福利，默认 6，最大 6
  WEILE_JPQ_CLAIM_MAX     可选，每个账号本次最多领取几次分享记牌器，默认 2，最大 2
  WEILE_ENABLE_SUBSCRIBE  可选，是否领取订阅更新奖励，默认 1

依赖：requests
"""

from __future__ import annotations

import os
import random
import re
import time
import uuid
from dataclasses import dataclass
from typing import Any

import requests


APP_ID = "wxbe254e0a4b639be6"
PRODUCT_ID = 188
CHANNEL_ID = 818
CLIENT_VERSION = "1.9.179"
API_VERSION = os.getenv("WEILE_API_VERSION", "1.9.179.167.891").strip()
REGION = os.getenv("WEILE_REGION", "370101").strip()
VC_PLATFORM = "rx_qipai_jiaxiang"
TIMEOUT = 30
PASSPORT_URL = "https://anhvcpo.jiaxiangxm.com/v1/passport/account/login_by_credential"
BUSINESS_LOGIN_URL = "https://pine1rqbn.jiaxiangxm.com/ruixue/loginbyother?format=json"
ACTIVITY_BASE = "https://bdjnrkq.jiaxiangxm.com"
USER_AGENT = (
    "Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 "
    "(KHTML, like Gecko) Mobile MicroMessenger/8.0.75 MiniGame"
)


class ScriptError(RuntimeError):
    pass


@dataclass
class YybAccount:
    index: int
    server: str
    ref: str
    remark: str = ""

    @property
    def label(self) -> str:
        account = f"账号 {self.ref}" if self.ref.isdigit() else f"账号 {self.index}"
        return f"{self.remark}（{account}）" if self.remark else account


def env_flag(name: str, default: bool) -> bool:
    value = os.getenv(name)
    if value is None:
        return default
    return value.strip().lower() not in {"", "0", "false", "no", "off"}


def env_int(name: str, default: int, minimum: int, maximum: int) -> int:
    try:
        value = int(os.getenv(name, str(default)).strip())
    except ValueError:
        value = default
    return max(minimum, min(maximum, value))


def safe_text(value: Any) -> str:
    text = str(value or "").replace("\r", " ").replace("\n", " ").strip()
    text = re.sub(r"(?i)(token|code|openid|authorization)[=: ]+[^ ,}]+", r"\1=***", text)
    return text[:240]


def parse_accounts() -> list[YybAccount]:
    accounts: list[YybAccount] = []
    for line in os.getenv("YYB_SERVER", "").splitlines():
        line = line.strip()
        if not line or "@" not in line or line == "[object Object]":
            continue
        server, ref = (part.strip() for part in line.split("@", 1))
        if not server or not ref:
            continue
        if not server.startswith(("http://", "https://")):
            server = "http://" + server
        accounts.append(YybAccount(len(accounts) + 1, server.rstrip("/"), ref))
    if not accounts:
        raise ScriptError("未配置 YYB_SERVER，格式：yyb-go:8000@账号ID或OpenID")
    return accounts


def load_remarks(accounts: list[YybAccount]) -> None:
    grouped: dict[str, list[YybAccount]] = {}
    for account in accounts:
        grouped.setdefault(account.server, []).append(account)

    for server, rows in grouped.items():
        try:
            response = requests.get(server + "/accounts", timeout=10)
            payload: Any = response.json()
            items = payload.get("data") if isinstance(payload, dict) else payload
            if not response.ok or not isinstance(items, list):
                continue
        except (requests.RequestException, ValueError):
            continue

        for account in rows:
            for item in items:
                if not isinstance(item, dict):
                    continue
                if str(item.get("id")) == account.ref or str(item.get("openid")) == account.ref:
                    account.remark = str(
                        item.get("remark") or item.get("nickname") or item.get("alias") or ""
                    ).strip()
                    break


def response_json(response: requests.Response, action: str) -> dict[str, Any]:
    try:
        payload = response.json()
    except ValueError as exc:
        raise ScriptError(f"{action}返回非 JSON，HTTP {response.status_code}") from exc
    if not response.ok:
        message = payload.get("msg") or payload.get("message") or ""
        raise ScriptError(f"{action}失败，HTTP {response.status_code} {safe_text(message)}")
    if not isinstance(payload, dict):
        raise ScriptError(f"{action}返回格式异常")
    return payload


def yyb_result(payload: dict[str, Any]) -> dict[str, Any]:
    data = payload.get("data") or {}
    nested = data.get("data") if isinstance(data, dict) else {}
    result = data.get("result") if isinstance(data, dict) else None
    if not isinstance(result, dict) and isinstance(nested, dict):
        result = nested.get("result")
    return result if isinstance(result, dict) else (data if isinstance(data, dict) else {})


def reward_text(rewards: Any) -> str:
    if not isinstance(rewards, list):
        return "未返回奖励明细"
    names = {888: "微乐币", 411: "记牌器"}
    parts: list[str] = []
    for item in rewards:
        if not isinstance(item, (list, tuple)) or len(item) < 2:
            continue
        try:
            reward_id = int(item[0])
        except (TypeError, ValueError):
            reward_id = -1
        parts.append(f"{names.get(reward_id, f'道具{item[0]}')} {item[1]}")
    return "、".join(parts) or "未返回奖励明细"


class WeileClient:
    def __init__(self, account: YybAccount) -> None:
        self.account = account
        self.session = requests.Session()
        self.session.trust_env = False
        self.session.headers.update(
            {
                "Accept": "*/*",
                "Referer": f"https://servicewechat.com/{APP_ID}/564/page-frame.html",
                "User-Agent": USER_AGENT,
            }
        )
        self.userid = ""
        self.token = ""
        self.nickname = ""

    def get_wx_code(self) -> str:
        try:
            response = self.session.post(
                self.account.server + "/wxapp/getCode",
                json={"ref": self.account.ref, "app_id": APP_ID},
                timeout=TIMEOUT,
            )
        except requests.RequestException as exc:
            raise ScriptError(f"YYB 获取 code 失败：{safe_text(exc)}") from exc
        payload = response_json(response, "YYB 获取 code")
        if payload.get("code") != 0:
            raise ScriptError(f"YYB 获取 code 失败：{safe_text(payload.get('msg'))}")
        code = str(yyb_result(payload).get("code") or "")
        if not code:
            raise ScriptError("YYB 未返回 wx.login code")
        return code

    def passport_login(self, code: str) -> dict[str, Any]:
        device = str(uuid.uuid4())
        headers = {
            "ruixue-version": "4.0.2",
            "ruixue-platformid": "4",
            "ruixue-productid": str(PRODUCT_ID),
            "ruixue-devicecode": device,
            "ruixue-traceid": str(uuid.uuid4()),
            "ruixue-channelid": str(CHANNEL_ID),
            "ruixue-tzoffset": "8",
            "ruixue-appinfo": f"version={CLIENT_VERSION}",
            "ruixue-language": "zh-CN",
            "ruixue-cpid": "1000101",
        }
        body = {
            "ts": int(time.time() * 1000),
            "method": "minigame",
            "distinct_id": device,
            "user_source": {},
            "custom_ext": {},
            "ext": {"version": "base", "code": code},
            "open_source": "1001",
            "device": {
                "device_info": {
                    "memorySize": 4096,
                    "system": "Android 14",
                    "model": "Android",
                    "benchmarkLevel": 20,
                    "brand": "Android",
                    "platform": "android",
                },
                "ad_rawargs": {},
            },
            "activate": {"result": {"id": 0, "subchannelid": ""}},
        }
        try:
            response = self.session.post(
                PASSPORT_URL, json=body, headers=headers, timeout=TIMEOUT
            )
        except requests.RequestException as exc:
            raise ScriptError(f"瑞雪登录失败：{safe_text(exc)}") from exc
        payload = response_json(response, "瑞雪登录")
        data = payload.get("data") or {}
        if payload.get("code") != 0 or not isinstance(data, dict) or not data.get("token"):
            raise ScriptError(payload.get("message") or payload.get("msg") or "瑞雪登录未返回凭据")
        self.nickname = str(data.get("nickname") or "")
        return data

    def business_login(self, login_data: dict[str, Any]) -> None:
        external_openid = str(login_data.get("tid") or login_data.get("openid") or "")
        body = {
            "logindomain": "pine1rqbn.jiaxiangxm.com",
            "appid": PRODUCT_ID,
            "channelid": CHANNEL_ID,
            "clienttype": 32,
            "devicecode": str(login_data.get("openid") or uuid.uuid4()),
            "uri": (
                "https://nephtsf.jiaxiangxm.com/index/smallLoginBase/"
                f"{PRODUCT_ID}/{CHANNEL_ID}/{CLIENT_VERSION}/{REGION}"
            ),
            "value": (
                f"city={REGION}&min_from=&openid={external_openid}&type=ruixue&"
                f"udid={'1' * 40}&userid=0&v=1&wechat_id={APP_ID}"
            ),
            "apptype": "minigame",
            "logindata": login_data,
            "vc_platform": VC_PLATFORM,
            "signtstamp": int(time.time() * 1000),
        }
        try:
            response = self.session.post(BUSINESS_LOGIN_URL, json=body, timeout=TIMEOUT)
        except requests.RequestException as exc:
            raise ScriptError(f"微乐业务登录失败：{safe_text(exc)}") from exc
        if not response.ok:
            raise ScriptError(f"微乐业务登录失败，HTTP {response.status_code}")
        userid_match = re.search(r"\bid=(\d+)", response.text)
        token_match = re.search(r'\bcode="([^"]+)"', response.text)
        status_match = re.search(r"\bstatus=(\d+)", response.text)
        if status_match and status_match.group(1) != "0":
            message_match = re.search(r'msg="([^"]*)"', response.text)
            raise ScriptError(
                f"微乐业务登录失败：{safe_text(message_match.group(1) if message_match else response.text)}"
            )
        if not userid_match or not token_match:
            raise ScriptError("微乐业务登录未返回 userid/token")
        self.userid = userid_match.group(1)
        self.token = token_match.group(1)

    def authenticate(self) -> None:
        login_data = self.passport_login(self.get_wx_code())
        self.business_login(login_data)

    def activity(self, path: str, extra: dict[str, Any] | None = None) -> dict[str, Any]:
        if not self.userid or not self.token:
            raise ScriptError("微乐业务账号尚未登录")
        form: dict[str, Any] = {
            "hallID": "0",
            "signtstamp": str(int(time.time() * 1000)),
            "token": self.token,
            "userid": self.userid,
            "vc_platform": VC_PLATFORM,
        }
        if extra:
            form.update(extra)
        url = (
            f"{ACTIVITY_BASE}{path}/{PRODUCT_ID}/{CHANNEL_ID}/"
            f"{API_VERSION}/{REGION}?format=json"
        )
        try:
            response = self.session.post(url, data=form, timeout=TIMEOUT)
        except requests.RequestException as exc:
            raise ScriptError(f"{path} 请求失败：{safe_text(exc)}") from exc
        return response_json(response, path)

    def query_summary(self) -> None:
        payload = self.activity("/thirdparty/v4/minigame/integral/daily/summary")
        if payload.get("status") != 0:
            print(f"每日任务查询失败：{safe_text(payload.get('msg'))}")
            return
        data = payload.get("data") or {}
        print(
            f"每日任务：共 {data.get('daily_task_total', '-')} 项，"
            f"未完成 {data.get('unfinished_count', '-')} 项"
        )

    def share_info(self) -> dict[str, Any]:
        payload = self.activity("/shareaward/fl/info", {"tag": "big-lucky", "ver": "2"})
        if payload.get("status") != 0:
            raise ScriptError(f"分享福利查询失败：{safe_text(payload.get('msg'))}")
        data = payload.get("data") or {}
        return data if isinstance(data, dict) else {}

    def claim_share(self, maximum: int, dry_run: bool) -> int:
        info = self.share_info()
        received = int(info.get("receive_times") or 0)
        limit = int(info.get("limit") or 0)
        amount = int(info.get("wl_coin") or 0)
        print(f"分享福利：今日 {received}/{limit} 次，每次 {amount} 微乐币")
        remaining = max(0, limit - received)
        claim_count = min(maximum, remaining)
        if dry_run or claim_count == 0:
            return 0

        success = 0
        for index in range(claim_count):
            payload = self.activity(
                "/shareaward/fl/update",
                {"award_type": "0", "tag": "big-lucky", "ver": "2"},
            )
            if payload.get("status") != 0:
                print(f"分享福利领取停止：{safe_text(payload.get('msg'))}")
                break
            data = payload.get("data") or {}
            print(f"分享福利领取成功：{reward_text(data.get('rewards'))}")
            success += 1
            if index + 1 < claim_count:
                delay = random.uniform(1.0, 2.0)
                print(f"等待 {delay:.1f} 秒后继续领取...")
                time.sleep(delay)

        final_info = self.share_info()
        final_received = int(final_info.get("receive_times") or 0)
        final_limit = int(final_info.get("limit") or limit)
        if final_limit > 0 and final_received >= final_limit:
            print(f"分享福利已完成：今日 {final_received}/{final_limit} 次")
        else:
            print(f"分享福利当前进度：今日 {final_received}/{final_limit} 次")
        return success

    def free_jpq_info(self) -> dict[str, Any]:
        payload = self.activity("/shareaward/free-jpq/info")
        if payload.get("status") != 0:
            raise ScriptError(f"免费记牌器查询失败：{safe_text(payload.get('msg'))}")
        data = payload.get("data") or {}
        return data if isinstance(data, dict) else {}

    def claim_share_jpq(self, maximum: int, dry_run: bool) -> int:
        info = self.free_jpq_info()
        share = info.get("share") or {}
        if not isinstance(share, dict):
            print("分享领记牌器：接口未返回分享任务")
            return 0

        used = int(share.get("used") or 0)
        limit = int(share.get("limit") or 0)
        func = str(share.get("func") or "jpq_free_share1")
        print(
            f"分享领记牌器：今日 {used}/{limit} 次，"
            f"每次 {reward_text(info.get('rewards'))}"
        )
        claim_count = min(maximum, max(0, limit - used))
        if dry_run or claim_count == 0:
            return 0

        success = 0
        for index in range(claim_count):
            payload = self.activity(
                "/shareaward/get",
                {
                    "func": func,
                    "gameid": "0",
                    "share_type": "share",
                    "use_limit": "1",
                    "ver": "4",
                },
            )
            if payload.get("status") != 0:
                print(f"分享领记牌器停止：{safe_text(payload.get('msg'))}")
                break
            data = payload.get("data") or {}
            award = data.get("award") if isinstance(data, dict) else None
            rewards = list(award.items()) if isinstance(award, dict) else []
            print(f"分享领记牌器成功：{reward_text(rewards)}")
            success += 1
            if index + 1 < claim_count:
                delay = random.uniform(1.0, 2.0)
                print(f"等待 {delay:.1f} 秒后继续领取记牌器...")
                time.sleep(delay)

        final_info = self.free_jpq_info()
        final_share = final_info.get("share") or {}
        final_used = int(final_share.get("used") or 0) if isinstance(final_share, dict) else 0
        final_limit = (
            int(final_share.get("limit") or limit) if isinstance(final_share, dict) else limit
        )
        if final_limit > 0 and final_used >= final_limit:
            print(f"分享领记牌器已完成：今日 {final_used}/{final_limit} 次")
        else:
            print(f"分享领记牌器当前进度：今日 {final_used}/{final_limit} 次")
        return success

    def subscription_templates(self) -> int:
        payload = self.activity("/commonset/subscribe/get_templates")
        if payload.get("status") != 0:
            return 0
        data = payload.get("data") or {}
        return len(data) if isinstance(data, dict) else 0

    def claim_subscription(self, dry_run: bool) -> bool:
        template_count = self.subscription_templates()
        print(f"订阅奖励：发现 {template_count} 个消息模板")
        if dry_run:
            return False
        payload = self.activity("/commonset/v3/subscribe/reward", {"tag": "gxtz"})
        if payload.get("status") != 0:
            print(f"订阅奖励未领取：{safe_text(payload.get('msg'))}")
            return False
        data = payload.get("data") or {}
        print(f"订阅奖励领取成功：{reward_text(data.get('rewards'))}")
        return True


def main() -> None:
    accounts = parse_accounts()
    load_remarks(accounts)
    dry_run = env_flag("WEILE_DRY_RUN", False)
    share_max = env_int("WEILE_SHARE_CLAIM_MAX", 6, 0, 6)
    jpq_max = env_int("WEILE_JPQ_CLAIM_MAX", 2, 0, 2)
    enable_subscription = env_flag("WEILE_ENABLE_SUBSCRIBE", True)

    print("=" * 50)
    print(f"微乐金币活动启动，共 {len(accounts)} 个 YYB 账号")
    if dry_run:
        print("当前为只查询模式，不会领取奖励")
    print("=" * 50)

    completed = 0
    for position, account in enumerate(accounts, 1):
        print(f"\n[{position}/{len(accounts)}] {account.label}")
        try:
            client = WeileClient(account)
            client.authenticate()
            print(f"登录成功：{client.nickname or '未设置昵称'}")
            client.query_summary()
            client.claim_share(share_max, dry_run)
            client.claim_share_jpq(jpq_max, dry_run)
            if enable_subscription:
                client.claim_subscription(dry_run)
            completed += 1
        except Exception as exc:
            print(f"执行失败：{safe_text(exc)}")
        if position < len(accounts):
            time.sleep(random.uniform(1.5, 3.0))

    print(f"\n执行结束：完成 {completed}/{len(accounts)} 个账号")


if __name__ == "__main__":
    try:
        main()
    except ScriptError as exc:
        print(f"配置错误：{safe_text(exc)}")
        raise SystemExit(1)
