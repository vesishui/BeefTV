# 魏进视频

这是 BeefTV 的独立魏进 API 视频协议插件。它不修改 BeefTV 的通用生成链路，也不覆盖现有渠道。

## 配置

- 默认 Base URL：`https://www.weijinapi.top`
- 鉴权：`Authorization: Bearer <API Key>`
- 模型：从魏进的 `GET /v1/models` 返回结果中复制模型 ID
- 参考素材：插件声明 `requiresPublicMediaUrls`，图片、音频和视频会先经过 BeefTV 已有图床流程

## 请求流程

1. `POST /v1/videos` 创建异步任务。
2. `GET /v1/videos/{task_id}` 轮询 `queued`、`in_progress`、`completed` 或 `failed`。
3. 如果轮询响应没有直接返回媒体 URL，使用 `GET /v1/videos/{task_id}/content` 下载 MP4。

## 兼容范围

魏进的图片和文本接口是 OpenAI 兼容形状，继续使用 BeefTV 现有的 OpenAI 图片和聊天协议即可。本插件只负责魏进的非标准异步视频接口。

完整字段和响应映射见 [docs/interface.md](docs/interface.md)。
