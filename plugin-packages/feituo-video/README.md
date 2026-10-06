# 飞拓视频

飞拓 AI（feituokuajing.com）的视频生成协议插件。飞拓使用自有端点，不是 OpenAI 兼容形状。

## 协议身份

- 插件 ID：`feituo-video`
- Provider ID：`feituo-video`
- 能力：`video`
- 默认 Base URL：`https://feituokuajing.com`
- 鉴权：Bearer

## 接口

| 阶段 | 方法与路径 |
| --- | --- |
| 创建任务 | `POST /api/open/v1/video/generate` |
| 查询任务 | `GET /api/open/v1/video/status?jobId={taskId}` |

创建返回 `jobId`；查询在 `status` 为 `success` 时从 `videoUrl` 取结果，`submitted` 表示仍在排队或生成中。

## 参数

| 统一字段 | 上游字段 | 说明 |
| --- | --- | --- |
| `model` | `model` | 飞拓模型 ID，例如 `seedance2.0`。 |
| `prompt` | `prompt` | 视频提示词。 |
| `ratio` | `ratio` | 支持 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9。 |
| `seconds` | `duration` | 按 5 / 10 / 15 秒归并。 |
| `resolution` | `resolution` | 默认 720p。 |
| `images` | `imageUrls` | 参考图公网地址数组。 |
| `videos` | `videoUrls` | 参考视频公网地址数组，仅部分模型支持。 |
| `audios` | `audioUrls` | 参考音频公网地址数组，仅部分模型支持。 |

## 素材要求

飞拓要求参考素材是公网可访问的 HTTPS 地址，因此本插件声明 `requiresPublicMediaUrls`，由素材图床在上游请求之前完成上传替换。提示词里可用 `@image1`、`@image2` 引用对应素材。

## 限制

- `seedance2.0` 最多 6 张参考图，`dreamina-seedance-2.0` 与 `xn-seedance-2.0` 最多 9 张。
- 余额不足返回 402，无效密钥返回 403，错误信息原样透传。
