#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""补建音频节点：把 111.mp4 原片音轨挂上画布并连到 vid-s01。"""
import json, urllib.request

API = "http://127.0.0.1:8080"
CANVAS = "rmk111canvas0000000000000000000001"

# 登录拿 Cookie
req = urllib.request.Request(API + "/api/auth/login", method="POST",
    data=json.dumps({"username": "12029293", "password": "zxc12300", "acceptedTerms": True}).encode(),
    headers={"Content-Type": "application/json"})
resp = urllib.request.urlopen(req)
cookie = resp.headers.get("Set-Cookie", "").split(";")[0]

def get(path):
    r = urllib.request.Request(API + path, headers={"Cookie": cookie})
    return json.load(urllib.request.urlopen(r))

def patch(path, payload):
    r = urllib.request.Request(API + path, method="PATCH",
        data=json.dumps(payload).encode(),
        headers={"Cookie": cookie, "Content-Type": "application/json"})
    try:
        return json.load(urllib.request.urlopen(r))
    except urllib.error.HTTPError as e:
        print("HTTP {} 错误体: {}".format(e.code, e.read().decode("utf-8", "replace")[:800]))
        raise SystemExit(1)

# 1. 读画布，找 vid-s01 位置
canvas = get("/api/canvas-projects/" + CANVAS)["data"]["project"]
nodes = canvas.get("nodes") or []
vid = next((n for n in nodes if n.get("id") == "vid-s01"), None)
if not vid:
    raise SystemExit("找不到 vid-s01")
pos = vid.get("position") or {}
vx, vy = pos.get("x", 0), pos.get("y", 0)
existing = {n["id"] for n in nodes}
conns = canvas.get("connections") or []
print("vid-s01 位置: ({}, {})；已有节点 {} 个，连线 {} 条".format(vx, vy, len(nodes), len(conns)))

# 2. 组操作：建音频节点 + 连线（幂等：已存在就跳过）
node_id = "aud-yuanpian-111"
edge_id = "edge-aud-vid-s01"
ops = []
if node_id not in existing:
    ops.append({"type": "create_node", "node": {
        "id": node_id, "type": "audio", "title": "111.mp4 原片音轨",
        "position": {"x": vx, "y": vy + 340}, "width": 270, "height": 80,
        "metadata": {"assetId": "vr111audio00000000000000000000001",
                     "storageKey": "resource:6af3b332170b663a74b7ae3712f08864",
                     "mimeType": "audio/mpeg", "durationMs": 600027,
                     "status": "ready", "source": "视频逆向复刻"}}})
if not any(c.get("fromNodeId") == node_id and c.get("toNodeId") == "vid-s01" for c in conns):
    ops.append({"type": "create_connection", "connection": {
        "id": edge_id, "fromNodeId": node_id, "toNodeId": "vid-s01"}})

if not ops:
    print("音频节点与连线均已存在，无需操作")
    raise SystemExit(0)

# 3. 提交
result = patch("/api/canvas-projects/" + CANVAS + "/operations", {
    "mutationId": "aud-fix-" + node_id,
    "baseRevision": canvas.get("revision"),
    "operations": ops})
print("提交成功: code={} revision={}".format(result.get("code"), (result.get("data") or {}).get("project", {}).get("revision")))

# 4. 验证
canvas2 = get("/api/canvas-projects/" + CANVAS)["data"]["project"]
n2 = {n["id"]: n for n in (canvas2.get("nodes") or [])}
c2 = canvas2.get("connections") or []
kinds = {}
for n in n2.values():
    kinds[n.get("type")] = kinds.get(n.get("type"), 0) + 1
aud = n2.get(node_id, {})
print("音频节点: {} | storageKey={}".format("已建" if node_id in n2 else "缺失",
      (aud.get("metadata") or {}).get("storageKey")))
print("连线 aud->vid-s01:", "存在" if any(c.get("fromNodeId") == node_id and c.get("toNodeId") == "vid-s01" for c in c2) else "缺失")
print("全画布: 节点 {} 个 {} | 连线 {} 条".format(len(n2), json.dumps(kinds, ensure_ascii=False), len(c2)))
