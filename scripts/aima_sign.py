#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# name: 爱玛电动车签到
# cron: 18 8 * * *

"""爱玛会员小程序签到，支持 YYB_SERVER 多账号和账号备注。"""

from __future__ import annotations

import hashlib
import os
import re
import time
import uuid
from dataclasses import dataclass
from email.utils import parsedate_to_datetime
from typing import Any

import requests


APP_ID = "wx2dcfb409fd5ddfb4"
API_BASE = "https://scrm.aimatech.com/aima/wxclient"
SIGN_SALT = "AimaScrm321_^"
TIMEOUT = 30
USER_AGENT = (
    "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) "
    "AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 "
    "MicroMessenger/8.0.75 MiniProgramEnv/iOS"
)


class ScriptError(RuntimeError):
    pass


class AccountSkipped(ScriptError):
    pass


@dataclass
class YybAccount:
    index: int
    server: str
    ref: str
    remark: str = ""

    @property
    def label(self) -> str:
        base = f"账号 {self.ref}" if self.ref.isdigit() else f"账号 {self.index}"
        return f"{self.remark}（{base}）" if self.remark else base


def safe_text(value: Any) -> str:
    text = str(value or "").replace("\r", " ").replace("\n", " ").strip()
    text = re.sub(r"(?i)(token|authorization|openid|code)[=: ]+[^ ,}]+", r"\1=***", text)
    text = re.sub(r"(?<!\d)1\d{9}(\d)(?!\d)", r"1*********\1", text)
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
        message = payload.get("chnDesc") or payload.get("message") or payload.get("msg") or ""
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


class AimaClient:
    def __init__(self, account: YybAccount) -> None:
        self.account = account
        self.session = requests.Session()
        self.session.trust_env = False
        self.session.headers.update(
            {
                "Accept": "*/*",
                "Content-Type": "application/json",
                "Referer": f"https://servicewechat.com/{APP_ID}/0/page-frame.html",
                "User-Agent": USER_AGENT,
            }
        )
        self.timestamp = int(time.time() * 1000)
        self.token = ""

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
        data = payload.get("data") or {}
        account_info = data.get("account") if isinstance(data, dict) else None
        if isinstance(account_info, dict):
            self.account.remark = str(
                account_info.get("remark")
                or account_info.get("nickname")
                or account_info.get("alias")
                or self.account.remark
            ).strip()
        code = str(yyb_result(payload).get("code") or "")
        if not code:
            raise ScriptError("YYB 未返回 wx.login code")
        return code

    def _headers(self) -> dict[str, str]:
        trace_id = str(uuid.uuid4())
        token_part = self.token[50:80] if self.token else ""
        source = (
            f"App-IdscrmTime-Stamp{self.timestamp}TraceLog-Id{trace_id}"
            f"Access-Token{token_part}{SIGN_SALT}"
        )
        return {
            "App-Id": "scrm",
            "Time-Stamp": str(self.timestamp),
            "TraceLog-Id": trace_id,
            "Access-Token": self.token,
            "Sign": hashlib.md5(source.encode()).hexdigest(),
        }

    def request(self, method: str, path: str, *, data: dict[str, Any] | None = None) -> Any:
        try:
            response = self.session.request(
                method,
                API_BASE + "/" + path.lstrip("/"),
                json=data if method.upper() != "GET" else None,
                params=data if method.upper() == "GET" else None,
                headers=self._headers(),
                timeout=TIMEOUT,
            )
        except requests.RequestException as exc:
            raise ScriptError(f"{path} 请求失败：{safe_text(exc)}") from exc

        date_header = response.headers.get("Date")
        if date_header:
            try:
                self.timestamp = int(parsedate_to_datetime(date_header).timestamp() * 1000)
            except (TypeError, ValueError, OverflowError):
                self.timestamp = int(time.time() * 1000)
        else:
            self.timestamp = int(time.time() * 1000)
        refreshed_token = response.headers.get("Set-Access-Token")
        if refreshed_token:
            self.token = refreshed_token

        if path == "user/members:login" and response.status_code == 401:
            try:
                message = response.json().get("chnDesc") or "该微信尚未注册爱玛会员"
            except ValueError:
                message = "该微信尚未注册爱玛会员"
            raise AccountSkipped(safe_text(message))

        payload = response_json(response, path)
        if payload.get("code") != 200:
            message = payload.get("chnDesc") or payload.get("engDesc") or payload.get("detail")
            raise ScriptError(f"{path}失败：{safe_text(message or payload.get('code'))}")
        return payload.get("content")

    def login(self) -> None:
        code = self.get_wx_code()
        self.request("POST", "user/members:login", data={"code": code})
        if not self.token:
            raise ScriptError("爱玛登录未返回 Access-Token，请先在爱玛会员小程序完成注册")

    def profile(self) -> dict[str, Any]:
        content = self.request("GET", "member/IndexInfo")
        if not isinstance(content, dict) or not content.get("id"):
            raise ScriptError("未获取到爱玛会员资料，请先在小程序完成会员授权")
        return content

    def find_sign_activity(self) -> dict[str, Any]:
        content = self.request("POST", "mkt/activities/locations:search", data={"locations": [0, 1, 2]})
        if not isinstance(content, list):
            raise ScriptError("活动列表返回格式异常")
        now = int(time.time() * 1000)
        candidates: dict[str, dict[str, Any]] = {}
        for item in content:
            if not isinstance(item, dict):
                continue
            is_sign = item.get("templateType") == 3 or "签到" in str(item.get("templateName") or "")
            in_time = int(item.get("beginDate") or 0) <= now <= int(item.get("endDate") or 0)
            if is_sign and in_time and item.get("enabled") is not False and item.get("activityId"):
                candidates[str(item["activityId"])] = item
        if not candidates:
            raise ScriptError("未发现当前有效的爱玛签到活动")
        return max(candidates.values(), key=lambda item: int(item.get("beginDate") or 0))

    def sign_detail(self, activity_id: str) -> dict[str, Any]:
        content = self.request("POST", "mkt/activities/sign:search", data={"activityId": activity_id})
        if not isinstance(content, dict):
            raise ScriptError("签到状态返回格式异常")
        return content

    def sign(self, activity_id: str) -> dict[str, Any]:
        content = self.request(
            "POST",
            "mkt/activities/sign:join",
            data={"activityId": activity_id, "activitySceneId": None},
        )
        return content if isinstance(content, dict) else {}


def is_signed(detail: dict[str, Any]) -> bool:
    return detail.get("signed") == 1 or detail.get("signStatus") == 1


def points(profile: dict[str, Any]) -> dict[str, Any]:
    value = profile.get("vipMemberPointDTO")
    return value if isinstance(value, dict) else {}


def print_profile(profile: dict[str, Any], *, prefix: str = "会员信息") -> None:
    point = points(profile)
    level = profile.get("vipMemberLevelDTO") or {}
    member = profile.get("vipMemberInfoDTO") or {}
    print(
        f"{prefix}：等级 {profile.get('memberLevelName') or '-'}，当前积分 {point.get('pointValue', '-')}，"
        f"累计获得 {point.get('getPoint', '-')}，累计消费 {point.get('consumePoint', '-')}"
    )
    print(
        f"成长值 {level.get('growthValue', '-')}，绑定车辆 {member.get('bindCarCnt', 0)}，"
        f"优惠券 {profile.get('couponCnt', 0)}"
    )


def run_account(account: YybAccount) -> None:
    print(f"\n================ {account.label} ================")
    client = AimaClient(account)
    client.login()
    if account.remark:
        print(f"YYB 账号：{account.label}")
    before = client.profile()
    print_profile(before, prefix="签到前")

    activity = client.find_sign_activity()
    activity_id = str(activity["activityId"])
    print(f"当前活动：{activity.get('name') or activity_id}")
    detail = client.sign_detail(activity_id)
    if is_signed(detail):
        print("今日已签到，本轮无需重复签到")
    else:
        result = client.sign(activity_id)
        reward = result.get("point")
        print(f"签到接口成功：获得 {reward} 积分" if reward is not None else "签到接口成功")
        detail = client.sign_detail(activity_id)
        if not is_signed(detail):
            raise ScriptError("签到接口返回成功，但重新查询仍未确认今日已签到")
        print("签到结果校验：今日已签到")

    after = client.profile()
    print_profile(after, prefix="签到后")
    delta = (points(after).get("pointValue") or 0) - (points(before).get("pointValue") or 0)
    if delta:
        print(f"本次积分变化：+{delta}")


def main() -> int:
    try:
        accounts = parse_accounts()
    except ScriptError as exc:
        print(f"配置错误：{exc}")
        return 1
    load_remarks(accounts)
    print(f"共读取 {len(accounts)} 个 YYB 账号，爱玛 AppID：{APP_ID}")
    success = 0
    skipped = 0
    failed = 0
    for account in accounts:
        try:
            run_account(account)
            success += 1
        except AccountSkipped as exc:
            skipped += 1
            print(f"{account.label}已跳过：{safe_text(exc)}")
        except (ScriptError, requests.RequestException) as exc:
            failed += 1
            print(f"{account.label}执行失败：{safe_text(exc)}")
    print(f"\n执行完成：成功 {success}，跳过 {skipped}，失败 {failed}，总计 {len(accounts)}")
    return 0 if success else 1


if __name__ == "__main__":
    raise SystemExit(main())
