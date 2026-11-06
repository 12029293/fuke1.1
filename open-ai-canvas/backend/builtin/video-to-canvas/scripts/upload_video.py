#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""影策「视频上画布」一条龙脚本。

流程：登录(可选) -> 上传视频到资源存储 -> 登记为视频素材 -> 生成画布 handoff URL
      -> 可选自动用默认浏览器打开（前端自动新建/进入画布并插入视频节点）。

仅依赖 Python 3.8+ 标准库。用法示例：

    python upload_video.py --video "D:/素材/样片.mp4" --username 12029293 --password xxx
    python upload_video.py --video "D:/素材/样片.mp4" --session-cookie "<cookie 值>"
    python upload_video.py --video a.mp4 --no-open          # 只登记不打开浏览器
    python upload_video.py --video a.mp4 --dest-canvas <id> # 插入指定画布
"""

import argparse
import datetime
import hashlib
import json
import mimetypes
import os
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request
import uuid
import webbrowser

DEFAULT_API = "http://127.0.0.1:8080"
DEFAULT_WEB = "http://127.0.0.1:3000"
COOKIE_NAME = "open_ai_canvas_session"
USER_AGENT = "yingce-video-to-canvas/1.0"


def fail(message: str) -> "NoReturn":  # type: ignore[name-defined]
    print("[错误] " + message, file=sys.stderr)
    sys.exit(1)


def log(message: str) -> None:
    print("[INFO] " + message)


def http_request(
    url: str,
    *,
    method: str = "GET",
    headers=None,
    data: bytes = None,
    timeout: int = 60,
):
    request = urllib.request.Request(url, data=data, method=method)
    request.add_header("User-Agent", USER_AGENT)
    for key, value in (headers or {}).items():
        request.add_header(key, value)
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            body = response.read()
            status = response.status
            response_headers = dict(response.headers.items())
    except urllib.error.HTTPError as error:
        body = error.read()
        status = error.code
        response_headers = dict(error.headers.items()) if error.headers else {}
    except urllib.error.URLError as error:
        fail("无法连接影策后端 {}：{}\n请确认服务已启动（启动影策.bat）".format(url, error.reason))
    return status, body, response_headers


def parse_api_response(status: int, body: bytes, action: str):
    text = body.decode("utf-8", errors="replace")
    try:
        payload = json.loads(text)
    except json.JSONDecodeError:
        fail("{}失败：HTTP {}，无法解析响应：{}".format(action, status, text[:300]))
    code = payload.get("code")
    if code != 0:
        fail("{}失败：HTTP {}，code={}，msg={}".format(action, status, code, payload.get("msg", "<空>")))
    return payload.get("data") or {}


def multipart_body(fields, file_field, file_name, file_bytes, mime_type):
    boundary = "----YingceBoundary" + uuid.uuid4().hex
    parts = []
    for name, value in fields:
        parts.append(
            ("--{0}\r\nContent-Disposition: form-data; name=\"{1}\"\r\n\r\n{2}\r\n".format(boundary, name, value)).encode("utf-8")
        )
    parts.append(
        ("--{0}\r\nContent-Disposition: form-data; name=\"{1}\"; filename=\"{2}\"\r\n"
         "Content-Type: {3}\r\n\r\n".format(boundary, file_field, file_name, mime_type)).encode("utf-8")
    )
    parts.append(file_bytes)
    parts.append(("\r\n--{0}--\r\n".format(boundary)).encode("utf-8"))
    body = b"".join(parts)
    content_type = "multipart/form-data; boundary={0}".format(boundary)
    return body, content_type


def probe_video_dimensions(video_path: str):
    """尽力用 ffprobe 探测宽高/时长，探测不到就返回 0（影策允许 0 表示未知）。"""
    width = height = 0
    duration_ms = 0
    ffprobe = None
    for candidate in ("ffprobe", r"C:\ffmpeg\bin\ffprobe.exe"):
        try:
            subprocess.run(
                [candidate, "-version"],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                check=True,
            )
            ffprobe = candidate
            break
        except (OSError, subprocess.CalledProcessError):
            continue
    if not ffprobe:
        return width, height, duration_ms
    try:
        result = subprocess.run(
            [
                ffprobe,
                "-v", "error",
                "-select_streams", "v:0",
                "-show_entries", "stream=width,height:format=duration",
                "-of", "json",
                video_path,
            ],
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            check=True,
            timeout=30,
        )
        info = json.loads(result.stdout.decode("utf-8", errors="replace"))
        streams = info.get("streams") or [{}]
        width = int(streams[0].get("width") or 0)
        height = int(streams[0].get("height") or 0)
        duration = float(info.get("format", {}).get("duration") or 0)
        duration_ms = int(duration * 1000)
    except (OSError, subprocess.SubprocessError, ValueError, KeyError, json.JSONDecodeError):
        pass
    return width, height, duration_ms


def iso_now() -> str:
    return datetime.datetime.now(datetime.timezone.utc).isoformat(timespec="milliseconds").replace("+00:00", "Z")


def login_for_cookie(api_base: str, username: str, password: str) -> str:
    """登录获取会话 Cookie 值。"""
    body = json.dumps({"username": username, "password": password, "acceptedTerms": True}).encode("utf-8")
    status, resp, headers = http_request(
        api_base + "/api/auth/login",
        method="POST",
        headers={"Content-Type": "application/json"},
        data=body,
    )
    data = parse_api_response(status, resp, "登录")
    if not data.get("user"):
        fail("登录失败：响应中没有用户信息")
    # Set-Cookie: open_ai_canvas_session=...; Path=/; HttpOnly
    set_cookie = headers.get("Set-Cookie", "")
    for part in set_cookie.split(";"):
        part = part.strip()
        if part.startswith(COOKIE_NAME + "="):
            value = part[len(COOKIE_NAME) + 1:]
            if value:
                return value
    fail("登录成功但未返回会话 Cookie，请改用 --session-cookie 手动提供")


def sha256_file(path: str) -> str:
    digest = hashlib.sha256()
    with open(path, "rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def upload_video(api_base: str, cookie: str, video_path: str):
    """上传视频文件到资源存储，返回 resource 字典。

    幂等键 = 文件内容 SHA-256：同一文件重复导入会命中同一条资源记录，
    不会重复占用存储配额。
    """
    file_size = os.path.getsize(video_path)
    log("上传视频：{}（{:.2f} MB）".format(video_path, file_size / (1024 * 1024)))
    guessed = mimetypes.guess_type(video_path)[0]
    mime_type = guessed if guessed and guessed.startswith("video/") else "video/mp4"
    width, height, duration_ms = probe_video_dimensions(video_path)
    if width and height:
        log("探测到尺寸 {}x{}，时长 {} ms".format(width, height, duration_ms))
    else:
        log("未探测到视频尺寸（不影响导入，画布按默认尺寸展示）")
    upload_key = "v2c-sha256-" + sha256_file(video_path)
    with open(video_path, "rb") as handle:
        file_bytes = handle.read()
    fields = [
        ("kind", "video"),
        ("width", str(width)),
        ("height", str(height)),
        ("durationMs", str(duration_ms)),
    ]
    body, content_type = multipart_body(
        fields, "file", os.path.basename(video_path), file_bytes, mime_type
    )
    status, resp, _ = http_request(
        api_base + "/api/resources",
        method="POST",
        headers={
            "Content-Type": content_type,
            "Cookie": COOKIE_NAME + "=" + cookie,
            "X-Idempotency-Key": upload_key,
        },
        data=body,
        timeout=max(120, file_size // (1024 * 1024) * 30 + 60),
    )
    data = parse_api_response(status, resp, "上传视频")
    resource = data.get("resource")
    if not resource or not resource.get("id"):
        fail("上传响应中没有 resource.id：" + json.dumps(data, ensure_ascii=False)[:300])
    log("上传成功：resourceId={} mimeType={} size={}".format(
        resource.get("id"), resource.get("mimeType"), resource.get("size")))
    return resource


def register_asset(api_base: str, cookie: str, asset_id: str, title: str, resource: dict):
    """登记视频素材（与影策前端素材合同一致）。重复执行是 upsert 语义。"""
    storage_key = "resource:" + resource["id"]
    width = int(resource.get("width") or 0)
    height = int(resource.get("height") or 0)
    bytes_size = int(resource.get("size") or 0)
    mime_type = resource.get("mimeType") or "video/mp4"
    duration_ms = int(resource.get("durationMs") or 0)
    now = iso_now()
    # coverUrl 允许为空串（合同只要求字段存在；画布用 storageKey 渲染）。
    asset_document = {
        "id": asset_id,
        "kind": "video",
        "title": title,
        "coverUrl": "",
        "tags": [],
        "status": "confirmed",
        "category": "material",
        "source": "视频导入",
        "createdAt": now,
        "updatedAt": now,
        "data": {
            "url": "/api/resources/{}/file".format(resource["id"]),
            "storageKey": storage_key,
            "width": width,
            "height": height,
            "durationMs": duration_ms,
            "bytes": bytes_size,
            "mimeType": mime_type,
        },
    }
    payload = json.dumps({"asset": asset_document}).encode("utf-8")
    status, resp, _ = http_request(
        api_base + "/api/assets/" + urllib.parse.quote(asset_id),
        method="PUT",
        headers={
            "Content-Type": "application/json",
            "Cookie": COOKIE_NAME + "=" + cookie,
        },
        data=payload,
    )
    parse_api_response(status, resp, "登记素材")
    log("素材登记成功：assetId={} storageKey={}".format(asset_id, storage_key))
    return storage_key


def main() -> None:
    parser = argparse.ArgumentParser(
        description="影策：本地视频 -> 素材库 -> 画布视频节点（一条龙）",
        formatter_class=argparse.ArgumentDefaultsHelpFormatter,
    )
    parser.add_argument("--video", required=True, nargs="+",
                        help="本地视频文件路径，可一次传多个（mp4/webm/mov 等）；传目录则导入目录下全部视频")
    parser.add_argument("--api-url", default=DEFAULT_API, help="影策后端地址")
    parser.add_argument("--canvas-url", default=DEFAULT_WEB, help="影策前端地址")
    parser.add_argument("--title", default="", help="素材标题（仅单视频时有效，默认用文件名去扩展名）")
    parser.add_argument("--username", default="", help="影策账号（与 --session-cookie 二选一）")
    parser.add_argument("--password", default="", help="影策密码")
    parser.add_argument("--session-cookie", default=os.environ.get("YINGCE_SESSION_COOKIE", ""),
                        help="已登录的 open_ai_canvas_session Cookie 值（也可用环境变量 YINGCE_SESSION_COOKIE）")
    parser.add_argument("--dest-canvas", default="", help="插入到已存在的画布 ID（默认自动新建画布）")
    parser.add_argument("--no-open", action="store_true", help="只上传登记，不自动打开浏览器")
    args = parser.parse_args()

    # ---- 收集视频文件（支持目录展开）----
    video_exts = {".mp4", ".webm", ".mov", ".m4v", ".mkv", ".avi"}
    video_paths = []
    for raw in args.video:
        path = os.path.abspath(os.path.expanduser(raw))
        if os.path.isdir(path):
            for name in sorted(os.listdir(path)):
                candidate = os.path.join(path, name)
                if os.path.isfile(candidate) and os.path.splitext(name)[1].lower() in video_exts:
                    video_paths.append(candidate)
        elif os.path.isfile(path):
            video_paths.append(path)
        else:
            fail("视频文件不存在：" + path)
    if not video_paths:
        fail("没有找到可导入的视频文件")
    api_base = args.api_url.rstrip("/")
    web_base = args.canvas_url.rstrip("/")

    # ---- 1. 会话（只登录一次；登录接口有速率限制，多视频务必复用同一会话）----
    cookie = args.session_cookie.strip()
    if not cookie and args.username and args.password:
        log("使用账号密码登录……")
        cookie = login_for_cookie(api_base, args.username.strip(), args.password)
    if not cookie:
        fail(
            "缺少鉴权：请提供 --session-cookie（浏览器 F12 -> 应用 -> Cookies -> "
            "open_ai_canvas_session）或 --username/--password"
        )

    # ---- 2..3. 逐个上传 + 登记 ----
    imported = []  # (asset_id, storage_key, title)
    for index, video_path in enumerate(video_paths, 1):
        if len(video_paths) > 1:
            log("—— 第 {}/{} 个：{} ——".format(index, len(video_paths), os.path.basename(video_path)))
        resource = upload_video(api_base, cookie, video_path)
        title = args.title.strip() or os.path.splitext(os.path.basename(video_path))[0]
        asset_id = "v2c-" + uuid.uuid4().hex[:20]
        storage_key = register_asset(api_base, cookie, asset_id, title, resource)
        imported.append((asset_id, storage_key, title))

    # ---- 4. 画布链接（把全部素材一次性带进同一张画布）----
    params = [("mode", "handoff")]
    for asset_id, _storage, _title in imported:
        params.append(("asset", asset_id))
    query = urllib.parse.urlencode(params)
    if args.dest_canvas.strip():
        open_url = "{}/canvas/{}?{}".format(web_base, args.dest_canvas.strip(), query)
    else:
        open_url = "{}/canvas?{}".format(web_base, query)

    print("")
    print("=" * 60)
    print("导入完成（{} 个视频）".format(len(imported)))
    for asset_id, storage_key, title in imported:
        print("  素材：{} | {} | {}".format(title, asset_id, storage_key))
    print("  画布链接   : " + open_url)
    print("=" * 60)

    if not args.no_open:
        log("正在打开浏览器（前端会自动新建/进入画布并插入视频节点）……")
        opened = webbrowser.open(open_url)
        if not opened:
            log("无法自动打开浏览器，请手动把上面的画布链接粘贴到浏览器。")
    else:
        log("按 --no-open 跳过自动打开；需要上画布时打开上面的链接即可。")


if __name__ == "__main__":
    main()
