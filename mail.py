"""微软邮箱取件（Microsoft Graph）独立模块。

只包含业务逻辑：解析账号、用 refresh_token 换 access_token、拉取邮件列表与详情。
不依赖本项目代码，也不依赖第三方库（仅标准库），可直接复制到其他项目使用。

    from mail import MicrosoftMailbox, parse_account

    acc = parse_account("user@hotmail.com----password----client-id----refresh-token")
    box = MicrosoftMailbox(**acc)
    data = box.list_messages(folder="inbox", page=1, page_size=20)
    for m in data["items"]:
        print(m["subject"], m["sender"]["emailAddress"]["address"])
    detail = box.get_message(data["items"][0]["id"])

注意：
- refresh_token 会由微软轮换，模块会把新值写回 box.refresh_token，请及时持久化。
- 邮件正文按 HTML 原样返回（contentType 为 html / text），渲染前请自行消毒。
"""

from __future__ import annotations

import json
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from typing import Any, Dict, List, Optional, Tuple

DEFAULT_TOKEN_URL = "https://login.microsoftonline.com/consumers/oauth2/v2.0/token"
DEFAULT_CLIENT_ID = "dbc8e03a-b00c-46bd-ae65-b683e7707cb0"
DEFAULT_SCOPE = "https://graph.microsoft.com/.default"
DEFAULT_GRAPH_BASE = "https://graph.microsoft.com/v1.0"

DEFAULT_TIMEOUT = 30.0
TOKEN_CACHE_SKEW = 60.0
MAX_PAGE_SIZE = 100

LIST_SELECT = "id,subject,sender,from,toRecipients,receivedDateTime,bodyPreview"


class MailError(RuntimeError):
    """取件过程中的可读错误（令牌失效、接口报错等）。"""


def normalize_folder(folder: Optional[str]) -> str:
    """统一文件夹名：junk / spam 归为 spam，其余归为 inbox。"""
    value = (folder or "").strip().lower()
    if value in ("spam", "junk", "junkemail"):
        return "spam"
    return "inbox"


def graph_folder_id(folder_key: str) -> str:
    return "junkemail" if folder_key == "spam" else "inbox"


def parse_account(raw: str) -> Dict[str, str]:
    """解析「账号----密码----clientid----refreshtoken」。

    兼容 2 / 3 段格式：账号----密码、账号----密码----refreshtoken。
    """
    parts = [p.strip() for p in (raw or "").replace("\r\n", "\n").split("----")]
    if not parts or not parts[0]:
        raise MailError("账号数据为空")
    email = parts[0]
    password = parts[1] if len(parts) > 1 else ""
    if len(parts) >= 4:
        client_id = parts[2]
        refresh_token = parts[3]
    elif len(parts) == 3:
        client_id = ""
        refresh_token = parts[2]
    else:
        client_id = ""
        refresh_token = ""
    if not refresh_token:
        raise MailError("缺少 refresh_token，无法通过 Graph 取件")
    return {
        "email": email,
        "password": password,
        "client_id": client_id,
        "refresh_token": refresh_token,
    }


def _post_form(url: str, form: Dict[str, str], timeout: float) -> Dict[str, Any]:
    body = urllib.parse.urlencode(form).encode("utf-8")
    request = urllib.request.Request(
        url,
        data=body,
        method="POST",
        headers={
            "Content-Type": "application/x-www-form-urlencoded",
            "Accept": "application/json",
        },
    )
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            raw = response.read(1 << 20)
    except urllib.error.HTTPError as exc:
        raw = exc.read() or b""
        text = raw.decode("utf-8", "replace").strip()
        try:
            payload = json.loads(text)
        except ValueError:
            payload = {}
        message = payload.get("error_description") or text or "Refresh token已过期或无效"
        raise MailError(message) from exc
    except urllib.error.URLError as exc:
        raise MailError("换令牌失败: %s" % exc.reason) from exc
    try:
        return json.loads(raw.decode("utf-8"))
    except ValueError as exc:
        raise MailError("解析令牌响应失败") from exc


def _get_json(
    url: str,
    access_token: str,
    params: Optional[Dict[str, str]],
    timeout: float,
) -> Dict[str, Any]:
    if params:
        url = url + "?" + urllib.parse.urlencode(params)
    request = urllib.request.Request(
        url,
        method="GET",
        headers={
            "Authorization": "Bearer " + access_token,
            "Accept": "application/json",
            "Prefer": 'outlook.body-content-type="html"',
        },
    )
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            raw = response.read(8 << 20)
    except urllib.error.HTTPError as exc:
        raw = exc.read() or b""
        text = raw.decode("utf-8", "replace").strip()
        try:
            payload = json.loads(text)
            text = (payload.get("error") or {}).get("message") or text
        except ValueError:
            pass
        raise MailError(text or "Graph 请求失败（%d）" % exc.code) from exc
    except urllib.error.URLError as exc:
        raise MailError("Graph 请求失败: %s" % exc.reason) from exc
    try:
        return json.loads(raw.decode("utf-8"))
    except ValueError as exc:
        raise MailError("解析 Graph 响应失败") from exc


def _address(raw: Any) -> Dict[str, Any]:
    item = raw if isinstance(raw, dict) else {}
    email = item.get("emailAddress") if isinstance(item.get("emailAddress"), dict) else {}
    address = (email.get("address") or "").strip()
    name = (email.get("name") or "").strip() or address
    return {"emailAddress": {"name": name, "address": address}}


def _addresses(raw: Any) -> List[Dict[str, Any]]:
    if not isinstance(raw, list):
        return []
    return [_address(item) for item in raw]


def _normalize_content_type(value: Any) -> str:
    return "html" if str(value or "").strip().lower() in ("html", "text/html") else "text"


def _map_message(raw: Dict[str, Any], folder_key: str) -> Dict[str, Any]:
    item = raw if isinstance(raw, dict) else {}
    sender_raw = item.get("sender") or item.get("from")
    from_raw = item.get("from") or item.get("sender")

    message: Dict[str, Any] = {
        "id": item.get("id") or "",
        "folder": folder_key,
        "subject": item.get("subject") or "(无主题)",
        "sender": _address(sender_raw),
        "from": _address(from_raw),
        "toRecipients": _addresses(item.get("toRecipients")),
        "receivedDateTime": item.get("receivedDateTime") or "",
        "bodyPreview": (item.get("bodyPreview") or "").strip(),
    }

    body = item.get("body")
    if isinstance(body, dict):
        message["contentType"] = _normalize_content_type(body.get("contentType"))
        message["body"] = {
            "contentType": message["contentType"],
            "content": body.get("content") or "",
        }
    else:
        message["contentType"] = "text"
    return message


class MicrosoftMailbox:
    """一个微软邮箱账号的取件客户端（Graph API）。

    password 仅用于兼容「账号----密码----clientid----refreshtoken」的入参，
    Graph 取件实际只使用 client_id + refresh_token。
    """

    def __init__(
        self,
        email: str,
        password: str = "",
        client_id: str = "",
        refresh_token: str = "",
        *,
        token_url: str = DEFAULT_TOKEN_URL,
        graph_base: str = DEFAULT_GRAPH_BASE,
        scope: str = DEFAULT_SCOPE,
        timeout: float = DEFAULT_TIMEOUT,
    ) -> None:
        email = (email or "").strip()
        if not email:
            raise MailError("缺少邮箱账号")
        if not refresh_token:
            raise MailError("缺少 refresh_token，无法通过 Graph 取件")
        self.email = email
        self.password = password or ""
        self.client_id = (client_id or "").strip() or DEFAULT_CLIENT_ID
        self.refresh_token = refresh_token.strip()
        self.token_url = token_url
        self.graph_base = graph_base.rstrip("/")
        self.scope = scope
        self.timeout = timeout

        self._access_token = ""
        self._expires_at = 0.0
        self._lock = threading.Lock()

    def _refresh_access_token(self) -> Tuple[str, str, int]:
        form = {
            "client_id": self.client_id,
            "grant_type": "refresh_token",
            "refresh_token": self.refresh_token,
        }
        if self.scope:
            form["scope"] = self.scope
        payload = _post_form(self.token_url, form, self.timeout)

        access_token = payload.get("access_token") or ""
        if not access_token:
            raise MailError(payload.get("error_description") or "获取 access_token 失败")
        next_refresh = payload.get("refresh_token") or self.refresh_token
        try:
            expires_in = int(payload.get("expires_in") or 0)
        except (TypeError, ValueError):
            expires_in = 0
        if expires_in < 60:
            expires_in = 3600
        return access_token, next_refresh, expires_in

    def _token(self) -> str:
        with self._lock:
            if self._access_token and time.time() + TOKEN_CACHE_SKEW < self._expires_at:
                return self._access_token
            access_token, refresh_token, expires_in = self._refresh_access_token()
            self._access_token = access_token
            self.refresh_token = refresh_token
            self._expires_at = time.time() + expires_in
            return self._access_token

    def list_messages(
        self,
        folder: str = "inbox",
        page: int = 1,
        page_size: int = 20,
        include_body: bool = False,
    ) -> Dict[str, Any]:
        """拉取一页邮件列表（按收件时间倒序）。include_body=True 时带正文。"""
        folder_key = normalize_folder(folder)
        folder_id = graph_folder_id(folder_key)
        safe_page = max(1, int(page or 1))
        safe_size = min(MAX_PAGE_SIZE, max(1, int(page_size or 20)))
        skip = (safe_page - 1) * safe_size
        select = LIST_SELECT + (",body" if include_body else "")

        token = self._token()
        total_payload = _get_json(
            self.graph_base + "/me/mailFolders/" + urllib.parse.quote(folder_id, safe=""),
            token,
            {"$select": "totalItemCount"},
            self.timeout,
        )
        payload = _get_json(
            self.graph_base + "/me/mailFolders/" + urllib.parse.quote(folder_id, safe="") + "/messages",
            token,
            {
                "$top": str(safe_size),
                "$skip": str(skip),
                "$orderby": "receivedDateTime desc",
                "$select": select,
            },
            self.timeout,
        )

        total = total_payload.get("totalItemCount")
        total = int(total) if isinstance(total, (int, float)) else 0

        items: List[Dict[str, Any]] = []
        for raw in payload.get("value") or []:
            message = _map_message(raw, folder_key)
            if not include_body:
                message.pop("body", None)
            items.append(message)

        return {
            "email": self.email,
            "folder": folder_key,
            "total": max(0, total),
            "page": safe_page,
            "page_size": safe_size,
            "items": items,
            "refresh_token": self.refresh_token,
        }

    def get_message(self, message_id: str, folder: str = "inbox") -> Dict[str, Any]:
        """按邮件 ID 拉取详情（含正文）。"""
        target = (message_id or "").strip()
        if not target:
            raise MailError("邮件 ID 不能为空")
        folder_key = normalize_folder(folder)
        payload = _get_json(
            self.graph_base + "/me/messages/" + urllib.parse.quote(target, safe=""),
            self._token(),
            {"$select": LIST_SELECT + ",body"},
            self.timeout,
        )
        return {
            "email": self.email,
            "folder": folder_key,
            "item": _map_message(payload, folder_key),
            "refresh_token": self.refresh_token,
        }

    def fetch_latest(
        self,
        limit: int = 10,
        folder: str = "inbox",
        include_body: bool = True,
    ) -> List[Dict[str, Any]]:
        """便捷方法：取最新 N 封，默认带正文。"""
        size = min(MAX_PAGE_SIZE, max(1, int(limit or 10)))
        return self.list_messages(folder=folder, page=1, page_size=size, include_body=include_body)["items"]
