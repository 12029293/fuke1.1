#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""影策「视频逆向复刻」一键启动脚本。

流程：登录 -> 上传视频 -> 登记素材 -> 新建画布并插入视频节点 -> 启动 Agent 逆向复刻
      -> 打开画布页面。Agent 默认按原片 1:1 对齐复刻（镜头数、每镜时长全部照
      canvas_inspect_video 回执的 shots 实测值，不裁剪），在画布右侧对话里即可看到
      进度；人物图片节点与逐镜视频生成节点随后自动拆出，
      全部生成动作由用户在画布上手动确认，脚本与 Agent 都不会擅自生成。

仅依赖 Python 3.8+ 标准库。用法示例：

    python video_remake.py --video "C:/Users/你/Desktop/111.mp4" --username 12029293 --password xxx
    python video_remake.py --video a.mp4 --session-cookie "<cookie 值>"
    python video_remake.py --video a.mp4 --dest-canvas <已有画布ID>   # 追加到已有画布
"""

import argparse
import datetime
import hashlib
import json
import mimetypes
import os
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
import webbrowser

DEFAULT_API = "http://127.0.0.1:8080"
DEFAULT_WEB = "http://127.0.0.1:3000"
COOKIE_NAME = "open_ai_canvas_session"
USER_AGENT = "yingce-video-remake/1.0"

# 与后端 CHANNEL_000001 的默认 Agent 模型保持一致；新机部署后按实际渠道修改。
DEFAULT_CHANNEL_ID = "CHANNEL_000001"
DEFAULT_MODEL_KEY = "deepseek-v4.1-flash"


def fail(message):
    print("[错误] " + message, file=sys.stderr)
    sys.exit(1)


def log(message):
    print("[INFO] " + message)


def http_request(url, *, method="GET", headers=None, data=None, timeout=60):
    request = urllib.request.Request(url, data=data, method=method)
    request.add_header("User-Agent", USER_AGENT)
    for key, value in (headers or {}).items():
        request.add_header(key, value)
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            return response.status, response.read(), dict(response.headers.items())
    except urllib.error.HTTPError as error:
        body = error.read()
        headers = dict(error.headers.items()) if error.headers else {}
        return error.code, body, headers
    except urllib.error.URLError as error:
        fail("无法连接影策后端 {}：{}\n请确认服务已启动（启动影策.bat）".format(url, error.reason))


def parse_api_response(status, body, action):
    text = body.decode("utf-8", errors="replace")
    try:
        payload = json.loads(text)
    except json.JSONDecodeError:
        fail("{}失败：HTTP {}，无法解析响应：{}".format(action, status, text[:300]))
    if payload.get("code") != 0:
        fail("{}失败：HTTP {}，code={}，msg={}".format(action, status, payload.get("code"), payload.get("msg", "<空>")))
    return payload.get("data") or {}


def multipart_body(fields, file_field, file_name, file_bytes, mime_type):
    boundary = "----YingceBoundary" + uuid.uuid4().hex
    parts = []
    for name, value in fields:
        parts.append(("--{0}\r\nContent-Disposition: form-data; name=\"{1}\"\r\n\r\n{2}\r\n".format(boundary, name, value)).encode("utf-8"))
    parts.append(("--{0}\r\nContent-Disposition: form-data; name=\"{1}\"; filename=\"{2}\"\r\nContent-Type: {3}\r\n\r\n".format(boundary, file_field, file_name, mime_type)).encode("utf-8"))
    parts.append(file_bytes)
    parts.append(("\r\n--{0}--\r\n".format(boundary)).encode("utf-8"))
    return b"".join(parts), "multipart/form-data; boundary={0}".format(boundary)


def probe_video_dimensions(video_path):
    """ffprobe 探测宽高与时长；探测不到返回 0（影策允许 0 表示未知）。"""
    width = height = 0
    duration_ms = 0
    # 与启动脚本同一约定：ffmpeg/ffprobe 放在「安装根目录/bin」下。
    # 脚本位于 <根>/open-ai-canvas/backend/builtin/video-remake/scripts/，
    # 向上 5 层即安装根目录；探测不到时自动回落到系统 PATH。安装在哪都能用。
    script_root = os.path.dirname(os.path.abspath(__file__))
    for _ in range(5):
        script_root = os.path.dirname(script_root)
    local_bin = os.path.join(script_root, "bin")
    candidates = ["ffprobe"]
    if os.path.isfile(os.path.join(local_bin, "ffprobe.exe")):
        candidates.insert(0, os.path.join(local_bin, "ffprobe.exe"))
    ffprobe = None
    for candidate in candidates:
        try:
            subprocess.run([candidate, "-version"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True)
            ffprobe = candidate
            break
        except (OSError, subprocess.CalledProcessError):
            continue
    if not ffprobe:
        return width, height, duration_ms
    try:
        result = subprocess.run(
            [ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height:format=duration", "-of", "json", video_path],
            stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, check=True, timeout=30,
        )
        info = json.loads(result.stdout.decode("utf-8", errors="replace"))
        streams = info.get("streams") or [{}]
        width = int(streams[0].get("width") or 0)
        height = int(streams[0].get("height") or 0)
        duration_ms = int(float(info.get("format", {}).get("duration") or 0) * 1000)
    except (OSError, subprocess.SubprocessError, ValueError, KeyError, json.JSONDecodeError):
        pass
    return width, height, duration_ms


def iso_now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat(timespec="milliseconds").replace("+00:00", "Z")


def login_for_cookie(api_base, username, password):
    body = json.dumps({"username": username, "password": password, "acceptedTerms": True}).encode("utf-8")
    status, resp, headers = http_request(api_base + "/api/auth/login", method="POST",
                                         headers={"Content-Type": "application/json"}, data=body)
    data = parse_api_response(status, resp, "登录")
    if not data.get("user"):
        fail("登录失败：响应中没有用户信息")
    set_cookie = headers.get("Set-Cookie", "")
    for part in set_cookie.split(";"):
        part = part.strip()
        if part.startswith(COOKIE_NAME + "="):
            value = part[len(COOKIE_NAME) + 1:]
            if value:
                return value
    fail("登录成功但未返回会话 Cookie")


def sha256_file(path):
    digest = hashlib.sha256()
    with open(path, "rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def upload_video(api_base, cookie, video_path):
    file_size = os.path.getsize(video_path)
    log("上传视频：{}（{:.2f} MB）".format(video_path, file_size / (1024 * 1024)))
    guessed = mimetypes.guess_type(video_path)[0]
    mime_type = guessed if guessed and guessed.startswith("video/") else "video/mp4"
    width, height, duration_ms = probe_video_dimensions(video_path)
    if width and height:
        log("探测到尺寸 {}x{}，时长 {} ms".format(width, height, duration_ms))
    upload_key = "v2c-sha256-" + sha256_file(video_path)
    with open(video_path, "rb") as handle:
        file_bytes = handle.read()
    body, content_type = multipart_body(
        [("kind", "video"), ("width", str(width)), ("height", str(height)), ("durationMs", str(duration_ms))],
        "file", os.path.basename(video_path), file_bytes, mime_type)
    status, resp, _ = http_request(
        api_base + "/api/resources", method="POST",
        headers={"Content-Type": content_type, "Cookie": COOKIE_NAME + "=" + cookie, "X-Idempotency-Key": upload_key},
        data=body, timeout=max(120, file_size // (1024 * 1024) * 30 + 60))
    data = parse_api_response(status, resp, "上传视频")
    resource = data.get("resource")
    if not resource or not resource.get("id"):
        fail("上传响应中没有 resource.id")
    log("上传成功：resourceId={}".format(resource["id"]))
    return resource


def register_asset(api_base, cookie, asset_id, title, resource):
    storage_key = "resource:" + resource["id"]
    asset_document = {
        "id": asset_id, "kind": "video", "title": title, "coverUrl": "", "tags": [],
        "status": "confirmed", "category": "material", "source": "视频逆向复刻",
        "createdAt": iso_now(), "updatedAt": iso_now(),
        "data": {
            "url": "/api/resources/{}/file".format(resource["id"]),
            "storageKey": storage_key,
            "width": int(resource.get("width") or 0),
            "height": int(resource.get("height") or 0),
            "durationMs": int(resource.get("durationMs") or 0),
            "bytes": int(resource.get("size") or 0),
            "mimeType": resource.get("mimeType") or "video/mp4",
        },
    }
    payload = json.dumps({"asset": asset_document}).encode("utf-8")
    status, resp, _ = http_request(api_base + "/api/assets/" + urllib.parse.quote(asset_id), method="PUT",
                                   headers={"Content-Type": "application/json", "Cookie": COOKIE_NAME + "=" + cookie},
                                   data=payload)
    parse_api_response(status, resp, "登记素材")
    log("素材登记成功：assetId={}".format(asset_id))
    return storage_key


def create_project(api_base, cookie, title):
    """建短剧项目；同名项目已存在时直接复用（幂等）。"""
    status, resp, _ = http_request(api_base + "/api/projects", method="POST",
                                   headers={"Content-Type": "application/json", "Cookie": COOKIE_NAME + "=" + cookie},
                                   data=json.dumps({"name": title, "type": "short-drama"}).encode("utf-8"))
    data = parse_api_response(status, resp, "创建项目")
    project_id = data["project"]["id"]
    log("项目就绪：projectId={}".format(project_id))
    return project_id


def create_canvas_with_video_node(api_base, cookie, canvas_id, project_id, title, asset_id, storage_key, width, height, duration_ms):
    """建画布 + 视频节点。几何必须是嵌套 position:{x,y}，扁平 x/y 会让前端崩溃。"""
    node_w = 270
    node_h = int(node_w * height / width) if width and height else 477
    operations = [
        {"type": "create_canvas", "id": canvas_id, "patch": {"title": title, "projectId": project_id}},
        {"type": "create_node", "node": {
            "id": "vid-" + asset_id[:12], "type": "video", "title": title + " 原片",
            "position": {"x": 0, "y": 0}, "width": node_w, "height": node_h,
            "metadata": {"assetId": asset_id, "storageKey": storage_key, "mimeType": "video/mp4",
                         "durationMs": duration_ms, "status": "ready"}},
        },
    ]
    payload = {"mutationId": "vr-" + uuid.uuid4().hex[:16], "baseRevision": 0, "operations": operations}
    status, resp, _ = http_request(api_base + "/api/canvas-projects/" + urllib.parse.quote(canvas_id) + "/operations",
                                   method="PATCH",
                                   headers={"Content-Type": "application/json", "Cookie": COOKIE_NAME + "=" + cookie},
                                   data=json.dumps(payload).encode("utf-8"))
    data = parse_api_response(status, resp, "创建画布")
    log("画布就绪：canvasId={} revision={}".format(canvas_id, data.get("project", {}).get("revision")))


def start_remake_run(api_base, cookie, canvas_id, channel_id, model_key):
    """启动 video-remake Agent 运行。user_question 会在画布对话里出现，由用户点选。"""
    prompt = ("请复刻这条视频：对画布上的视频节点走 video-remake 技能全流程——"
              "先用 canvas_inspect_video 抽帧看片理解内容，默认按原片 1:1 对齐（镜头数与每镜时长"
              "全部照回执 shots 的实测值，不裁剪、不压缩成剪辑版）；若回执带 suggestedWindows，"
              "按其中的 startSecond/endSecond 逐轮聚焦看片直到所需区间都有画面。然后产出剧本、分镜表、视觉设定、"
              "图片提示词、视频提示词五个 Markdown 节点；再把每个角色的人物板提示词拆成独立的"
              " image 节点、按分镜表逐镜拆出独立的 video 节点并连线到分镜对应位置。"
              "全程不要调用 generate_media、不要提交任何生成任务——所有生成由用户在画布上手动确认。")
    payload = {
        "canvasId": canvas_id, "prompt": prompt, "permissionMode": "auto",
        "contextScope": ["canvas"], "channelId": channel_id, "channelModelKey": model_key,
        "budget": {"maxCredits": 200, "maxGenerationTasks": 0},
        "idempotencyKey": "vr-run-" + uuid.uuid4().hex[:16],
    }
    status, resp, _ = http_request(api_base + "/api/agent/runs", method="POST",
                                   headers={"Content-Type": "application/json", "Cookie": COOKIE_NAME + "=" + cookie},
                                   data=json.dumps(payload).encode("utf-8"), timeout=60)
    data = parse_api_response(status, resp, "启动 Agent")
    run = data.get("run") or {}
    log("Agent 已启动：runId={} status={}".format(run.get("id"), run.get("status")))
    return run.get("id")


def main():
    parser = argparse.ArgumentParser(
        description="影策：本地视频 -> 画布 -> Agent 逆向复刻（剧本/分镜/人物节点/逐镜视频节点）",
        formatter_class=argparse.ArgumentDefaultsHelpFormatter)
    parser.add_argument("--video", required=True, nargs="+", help="本地视频文件路径，可一次传多个")
    parser.add_argument("--api-url", default=DEFAULT_API, help="影策后端地址")
    parser.add_argument("--canvas-url", default=DEFAULT_WEB, help="影策前端地址")
    parser.add_argument("--username", default="", help="影策账号（与 --session-cookie 二选一）")
    parser.add_argument("--password", default="", help="影策密码")
    parser.add_argument("--session-cookie", default=os.environ.get("YINGCE_SESSION_COOKIE", ""),
                        help="已登录的 open_ai_canvas_session Cookie 值")
    parser.add_argument("--dest-canvas", default="", help="追加到已有画布 ID（默认自动新建画布）")
    parser.add_argument("--channel-id", default=DEFAULT_CHANNEL_ID, help="Agent 使用的模型渠道 ID")
    parser.add_argument("--model-key", default=DEFAULT_MODEL_KEY, help="Agent 使用的模型 Key")
    parser.add_argument("--no-open", action="store_true", help="不自动打开浏览器")
    parser.add_argument("--no-agent", action="store_true", help="只导入视频不启动 Agent（之后在画布对话里说\"复刻这条视频\"即可）")
    args = parser.parse_args()

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

    cookie = args.session_cookie.strip()
    if not cookie and args.username and args.password:
        log("使用账号密码登录……")
        cookie = login_for_cookie(args.api_url.rstrip("/"), args.username.strip(), args.password)
    if not cookie:
        fail("缺少鉴权：请提供 --session-cookie 或 --username/--password")
    api_base = args.api_url.rstrip("/")
    web_base = args.canvas_url.rstrip("/")

    # 多视频时同一画布一次装下；首个视频决定画布标题。
    canvas_id = args.dest_canvas.strip() or ("vr-" + uuid.uuid4().hex[:24])
    first_title = os.path.splitext(os.path.basename(video_paths[0]))[0]
    project_id = None
    imported = []
    for index, video_path in enumerate(video_paths, 1):
        title = os.path.splitext(os.path.basename(video_path))[0]
        log("—— 第 {}/{} 个：{} ——".format(index, len(video_paths), os.path.basename(video_path)))
        resource = upload_video(api_base, cookie, video_path)
        asset_id = "vr-" + uuid.uuid4().hex[:20]
        storage_key = register_asset(api_base, cookie, asset_id, title, resource)
        if not args.dest_canvas.strip():
            project_id = create_project(api_base, cookie, first_title + " 复刻")
            create_canvas_with_video_node(
                api_base, cookie, canvas_id, project_id, first_title, asset_id, storage_key,
                int(resource.get("width") or 0), int(resource.get("height") or 0),
                int(resource.get("durationMs") or 0))
        else:
            # 追加模式：只建节点（画布已存在，baseRevision 需要现查，这里简化为提示用户在画布内用 handoff/Agent 追加）。
            log("已登记素材 {}，追加到已有画布请在画布对话里说\"复刻这条视频\"并把节点拖进画布".format(asset_id))
        imported.append((asset_id, storage_key, title))

    canvas_url = "{}/canvas/{}".format(web_base, canvas_id)
    run_id = None
    if not args.no_agent and not args.dest_canvas.strip():
        time.sleep(1)
        run_id = start_remake_run(api_base, cookie, canvas_id, args.channel_id, args.model_key)

    print("")
    print("=" * 60)
    print("导入完成（{} 个视频）".format(len(imported)))
    for asset_id, storage_key, title in imported:
        print("  素材：{} | {}".format(title, asset_id))
    print("  画布链接 : " + canvas_url)
    if run_id:
        print("  Agent运行: " + run_id)
    print("  下一步   : 在画布右侧对话查看 Agent 复刻进度（默认 1:1 对齐原片镜头结构），")
    print("             随后人物图片节点与逐镜视频节点会自动拆出；生成一律由你手动确认。")
    print("=" * 60)

    if not args.no_open:
        log("打开画布……")
        webbrowser.open(canvas_url)


if __name__ == "__main__":
    main()
