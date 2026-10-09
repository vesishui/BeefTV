# 山海画布

山海画布（shanhai.vnshu.cn）的生成协议插件。山海不是 [OI] 兼容形状：它用 `POST /generations` 创建异步任务，用 `GET /tasks/{id}` 轮询，`media_type` 区分文本、图片和视频。

## 协议身份

- 插件 ID：`shanhai-canvas`
- Provider ID：`shanhai-video`、`shanhai-image`、`shanhai-text`
- 能力：`video`、`image`、`text`
- 默认 Base URL：`https://shanhai.vnshu.cn`
- 鉴权：Bearer（`Authorization: Bearer oc_live_...`）

## 接口

| 阶段 | 方法与路径 |
| --- | --- |
| 创建任务 | `POST /api/v1/generations` |
| 查询任务 | `GET /api/v1/tasks/{taskId}` |
| 下载成片 | `output.url` 原样返回的地址（`/api/v1/media/runs/{id}`） |

创建返回任务 `id`；查询在 `status` 为 `succeeded` 后从 `output.url` 取结果，`queued` / `running` 表示仍在排队或生成中。`output.url` 是受鉴权地址，下载时必须带同一枚 Bearer Key，因此结果标记为 `resultEphemeral`。文本模型通常在创建时直接返回 `succeeded` 和 `output.text`。

## 请求体

```json
{
  "model": "oc-model-qbdmeb",
  "prompt": "A cinematic mountain sunrise",
  "media_type": "video",
  "inputs": [{ "type": "image", "url": "https://cdn.example.com/reference-1.jpg" }],
  "options": { "aspect_ratio": "16:9", "resolution": "720p", "duration": "15" }
}
```

`options.duration` 是字符串。`inputs` 里每一项必须是公开可访问的 HTTPS 直链，数量不能超过 `GET /models` 返回的 `capabilities.max_reference_images`。

## 参数

| 统一字段 | 上游字段 | 说明 |
| --- | --- | --- |
| `model` | `model` | `GET /models` 返回的 `data[].id`，不要用显示名称。 |
| `prompt` | `prompt` | 提示词，最长 20,000 字符。 |
| `ratio` | `options.aspect_ratio` | 画幅比例，取值以模型 `capabilities.aspect_ratios` 为准。 |
| `seconds` | `options.duration` | 视频时长秒数，按模型 `capabilities.durations` 取值。 |
| `resolution` | `options.resolution` | 分辨率档位，视频如 720p，图片如 1K / 2K / 4K。 |
| `images` | `inputs[type=image]` | 参考图公网 HTTPS 直链数组。 |
| `videos` | `inputs[type=video]` | 参考视频公网 HTTPS 直链数组。 |
| `audios` | `inputs[type=audio]` | 参考音频公网 HTTPS 直链数组。 |

山海的扩展字段可通过 `providerOptions.shanhai-canvas.body` 传入。

## 素材要求

山海要求参考素材是公网可访问的 HTTPS 直链，因此图片、视频两个 provider 声明 `requiresPublicMediaUrls`，由素材图床在上游请求之前完成上传替换；不接受本地路径、登录后页面、HTML 预览页或仅浏览器能访问的地址。素材必须在任务处理期间保持有效。

音频参考需要先用 `POST /uploads/audio`（`multipart/form-data`，文件字段名固定为 `file`，支持 MP3 / WAV，最大 25MB）取得 HTTPS 地址；本插件不代为上传音频，请先自行上传再把地址填入 `audios`。音频参考还必须同时提供至少一张参考图或一段参考视频。

## 错误语义

| 状态码 | 含义 |
| --- | --- |
| 401 | Key 缺失、错误或已撤销。 |
| 403 | Key 没有对应权限。 |
| 422 | 参数、模型或输入地址不合法。 |
| 429 | 超过该 Key 的每分钟限流。 |
