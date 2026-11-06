---
name: "视频上画布"
description: "把本地视频文件一键导入影策画布并登记为素材。提供 REST 脚本（upload_video.py）：上传视频到资源存储 → 登记素材 → 生成带 asset 参数的画布链接，浏览器打开后自动新建画布并插入视频节点；也说明如何在已打开的画布里用 Agent 把素材节点接入分镜、批量创作表或生成流程。触发词：导入视频、视频上画布、把视频放进画布、上传视频。"
metadata:
  version: "1.0.0"
  author: "影策"
  owner: "yingce-system"
  skillId: "video-to-canvas"
  tag: creative
  sortWeight: 900
  source: 3
  createdAt: 1762160000000
  updatedAt: 1762160000000
  initialLikeCount: 0
  initialAddedCount: 0
  authorAvatarUrl: ""
  extraInfo: ""
  showcaseMedia: []
---

# 视频上画布：本地视频 → 素材库 → 画布视频节点

> 用户给你一个本地视频路径（或让你把视频放进画布）时使用本技能。核心是一个不依赖浏览器的 REST 脚本 `scripts/upload_video.py`，三步完成：上传 → 登记 → 产出可自动插节点的画布链接。

## 何时调用

- 用户说「把这个视频导入/放进/上画布」「视频转到画布节点」
- 用户给出本地视频文件路径并希望它在影策里可见、可编辑、可参与创作
- 用户想批量导入一批视频到素材库

## 方式一：脚本一条龙（推荐，无需用户手动操作）

用 `skill_read_file` 读取本技能包内 `scripts/upload_video.py`（skillId 均为 `video-to-canvas`），把它写到用户可访问的位置（临时目录即可），然后按下面命令执行。脚本只依赖 Python 3.8+ 标准库，不需要安装任何第三方包。

### 第 0 步：拿到会话 Cookie

脚本用登录会话鉴权（Cookie 名 `open_ai_canvas_session`）。两种获取方式：

1. 让用户从浏览器复制：登录影策后按 F12 → 应用(Application) → Cookies → `http://127.0.0.1:3000` → 复制 `open_ai_canvas_session` 的值；
2. 若你持有用户的账号密码，可直接用登录接口拿 Cookie（脚本已内置 `--username/--password` 参数自动登录）。

### 第 1 步：执行上传

```bash
# 方式 A：账号密码自动登录（推荐，只登录一次）
python upload_video.py --video "D:\素材\样片.mp4" \
  --username 12029293 --password <密码>

# 方式 B：已有会话 Cookie
python upload_video.py --video "D:\素材\样片.mp4" \
  --session-cookie "<open_ai_canvas_session 的值>"

# 批量：一次导入多个视频（或整个目录），只登录一次，最后全部插进同一张画布
python upload_video.py --video "D:\素材\a.mp4" "D:\素材\b.mp4"
python upload_video.py --video "D:\素材\目录"

# 常用可选参数
#   --title "我的样片"        素材标题（仅单视频时有效，默认用文件名）
#   --canvas-url http://127.0.0.1:3000
#   --api-url  http://127.0.0.1:8080
#   --no-open                 只登记素材，不自动打开浏览器
#   --dest-canvas <canvasId>  插入到已存在的画布（默认自动新建画布）
```

脚本成功后输出：

- `assetId`：素材库中的视频素材 ID（批量时逐行列出）
- `storageKey`：`resource:<资源ID>`，画布与 Agent 都用这个键读取视频
- `handoff URL`：形如 `http://127.0.0.1:3000/canvas?mode=handoff&asset=<id1>&asset=<id2>...`

### 第 2 步：让视频出现在画布上

- 脚本默认已自动用系统浏览器打开 handoff URL：前端会**自动新建一个画布**并把视频节点插入画布中央，随后 URL 自动清理参数；
- 若用户指定了 `--dest-canvas`，请把 URL 中的 `/canvas?...` 换成 `/canvas/<dest-canvas>?...` 再打开，视频会插入**那个画布**；
- 打不开浏览器时（远程/无头环境），把 handoff URL 原样发给用户，让他自己在浏览器粘贴打开。

### 幂等与重试

- 上传接口按**文件内容 SHA-256** 幂等：同一文件重复导入返回同一条资源记录，不会重复占存储（assetId 每次会新生成，属正常）；
- 登记素材失败可直接重跑：脚本会对资源命中幂等，素材按新 assetId 再登记一次；
- **登录接口有速率限制**（约每分钟数次，触发后 429 提示等待秒数）：批量导入务必用 `--video` 一次传多个文件（脚本只登录一次），不要循环调用脚本；触发 429 时等提示的秒数再试，或改用 `--session-cookie` 免登录。

## 方式二：在画布 Agent 里操作（画布已打开时）

用户已经打开某个画布并让 Agent 处理视频时，走 Agent 工具链而不是脚本：

1. **上传**：让用户把视频文件直接拖进画布空白处（前端自动完成上传+登记+建视频节点），或用创作页/素材库的上传入口；
2. **确认状态**：`canvas_get_state` 读画布，找到 `nodeType: "video"` 的节点，检查 metadata 里 `storageKey` 是否以 `resource:` 开头（服务端资源）、`status` 是否成功；
3. **接线**：用 `canvas_apply_ops` 的 `connect_nodes` 把视频节点接到下游（分镜、批量创作表、图片/视频生成节点等）。连线前先 `canvas_list_node_types` 确认目标节点的 `canTarget` 与接受的 `inputKind`；
4. **注意**：Agent 工具**不能**直接写视频 URL 或任意 metadata（安全约束），所以「上传视频文件本体」这一步必须由用户在前端完成或走方式一的脚本——Agent 负责之后的组织与创作。

## 数据合同（排障时对照）

脚本按影策前端同款合同构造请求，字段含义如下，出错时逐项核对：

1. **上传资源** `POST /api/resources`（multipart）：
   - `file`：视频文件本体；`kind` 留空由服务端按 MIME 探测（mp4→video）；
   - 可选 `width/height/durationMs`（脚本会尝试用 ffprobe 探测，缺失时传 0）；
   - 响应 `data.resource`：`{ id, mimeType, size, width, height, durationMs, ... }`。
2. **登记素材** `PUT /api/assets/<assetId>`（JSON，≤4MB）：
   - 必需字段：`kind:"video"`、`title`、`coverUrl`、`tags:[]`、`data:{...}`；
   - `data` 必需：`url`（或 `storageKey`，两者至少一个）、`width`、`height`、`bytes`、`mimeType`；
   - `data.storageKey` 必须是 `resource:<上传返回的资源ID>`，这样画布和 Agent 才能读服务端资源；
   - `width/height` 未知时传 0（视频合同允许 0 表示未知，禁止伪造 1×1）。
3. **handoff URL**：`/canvas?mode=handoff&asset=<assetId>`（可多个 `asset` 参数）；
   - 打开 `/canvas`（画布库页）：自动新建画布并插入素材节点；
   - 打开 `/canvas/<id>`（指定画布）：插入到该画布，已插入过的素材不会重复插入。

## 常见错误

| 现象 | 原因与处理 |
|---|---|
| 401 unauthorized | 会话 Cookie 过期或没带对；重新登录取 Cookie 或改用 `--username/--password` |
| 429 请求次数已达上限 | 登录接口限流；等待提示的秒数再试，批量导入改为一次传多个视频文件 |
| 400 单个上传文件必须小于 NMB | 视频超过服务器配额；换小文件或在管理端调大 ResourceUploadMB |
| 400 素材缺少 X 字段 | 登记请求 JSON 不合合同，按上文「数据合同」第 2 条补齐 |
| 400 视频素材缺少 url 或 storageKey | `data` 里两个字段都为空；确认上传成功返回了 resource.id 并拼出 `resource:<id>` |
| 打开 handoff URL 提示「部分素材不存在」 | assetId 不属于当前登录用户，或登记请求实际失败；先 `GET /api/assets/<assetId>` 验证 |
| 画布上节点是黑的/播放不出 | 视频编码浏览器不支持（如 HEVC）；影策会自动转码 H.264 副本，等 playbackStatus=ready 再播 |

## 分发到其他电脑

本技能是**内置技能**，随影策后端程序（yingce-server.exe）分发，装机即有：

1. 新电脑安装影策后启动服务，用管理员账号登录；
2. 技能页搜索「视频上画布」即可看到并添加（内置技能自动注册，无需手动安装包）；
3. 若要改脚本或说明：编辑源码仓库 `backend/builtin/video-to-canvas/` 后重新编译后端；或把该目录打成 zip 通过「技能安装」入口上传（≤20MB）作为独立副本。

## 边界

- 本技能只负责「视频进入画布」这一段；进入后的剪辑思路、提示词、生成流程交给对应创作技能；
- 脚本不在用户机器上安装依赖、不修改视频内容；上传配额遵循服务端策略；
- 涉及他人版权的视频，先确认用户有使用权再导入。
