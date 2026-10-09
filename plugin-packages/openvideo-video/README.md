# OpenVideo 视频

OpenVideo（`www.openvideo.top`）的 OpenAI 兼容异步视频协议插件。插件只描述
`/api/v1` 线协议，不修改 BeefTV 的通用生成链路，也不保存 API Key。

## 配置

- 默认 Base URL：`https://www.openvideo.top/api/v1`
- 鉴权：`Authorization: Bearer <API Key>`
- 模型：从服务商的 `GET /models` 返回结果中复制 `data[].id`
- 参考素材：插件声明 `requiresPublicMediaUrls`，图片、音频和视频先经过 BeefTV
  已有图床和缓存流程，再把 HTTPS URL 放入生成请求

## 请求流程

1. `GET /models` 获取模型列表（免费模型列表检查，不创建任务）。
2. `POST /videos` 创建异步任务。
3. `GET /videos/{id}` 轮询 `pending`、`processing`、`succeeded` 或 `failed`。

服务商还提供 `POST /assets/uploads` 方便把本地文件上传为公网 URL；该接口是可选的。
BeefTV 插件不会绕过现有媒体图床去执行第二套上传流程。

## 参数

| BeefTV 字段 | OpenVideo 字段 | 说明 |
| --- | --- | --- |
| `model` | `model` | 从 `/models` 复制的模型 ID |
| `prompt` | `prompt` | 视频提示词 |
| 固定值 | `object` | 固定发送 `video` |
| `images` | `images` | 参考图片 HTTPS URL 数组 |
| `videos` | `videos` | 参考视频 HTTPS URL 数组 |
| `audios` | `audios` | 参考音频 HTTPS URL 数组 |
| `duration` | `duration` | 视频时长（秒） |
| `aspectRatio` | `ratio` | 例如 `16:9`、`9:16` |
| `resolution` | `resolution` | 例如 `720p`、`1080p` |
| `providerOptions.openvideo.style_id` | `style_id` | 可选风格 ID |
| `providerOptions.openvideo.team_id` | `team_id` | 可选团队 ID |

## 响应

创建响应使用 `id` 作为任务 ID。轮询响应优先读取 `state`，也兼容数值
`status`（`0` 待处理、`1` 处理中、`2` 成功、`3` 失败）。成功结果读取
`result_url` 或 `result_urls`，并由 BeefTV 持久化临时 URL。

完整接口字段见 [docs/interface.md](docs/interface.md)。
