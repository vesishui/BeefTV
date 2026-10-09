# SparkCine H3 视频

SparkCine（hi.hi.run）的 H3 兼容视频协议插件。

## 协议身份

- 插件 ID：`sparkcine-h3-video`
- Provider ID：`sparkcine-h3-video`
- 能力：`video`
- 默认 Base URL：`https://hi.hi.run`
- 鉴权：Bearer

## 接口

| 阶段 | 方法与路径 |
| --- | --- |
| 创建任务 | `POST /api/h3/v2/video_generation` |
| 查询任务 | `GET /api/h3/v2/query/video_generation/{task_id}` |

创建返回 `task_id`；查询完成后结果在 `task.content.url`，该链接 30 分钟内有效，过期可重新查询任务获取新链接。

## 请求形状

SparkCine 用 `content[]` 数组承载多模态输入，而不是扁平的图片字段：

```json
{
  "model": "MiniMax-H3",
  "content": [
    { "type": "text", "text": "提示词" },
    { "type": "image_url", "image_url": { "url": "https://..." }, "role": "first_frame" },
    { "type": "audio_url", "audio_url": { "url": "https://..." } }
  ],
  "resolution": "768P",
  "duration": 6,
  "ratio": "16:9"
}
```

## 参数

| 统一字段 | 上游字段 | 说明 |
| --- | --- | --- |
| `prompt` | `content[0].text` | 视频提示词。 |
| `images` | `content[].image_url` | 参考图默认 `reference_image`；仅当显式声明首尾帧时使用 `first_frame` / `last_frame`（只适用于 h3-video-frames）。 |
| `audios` | `content[].audio_url` | 参考音频，提示词里可用 `@音频1` 引用。 |
| `ratio` | `ratio` | 只支持 16:9 与 9:16。 |
| `seconds` | `duration` | 4-15 秒。 |
| `resolution` | `resolution` | 白名单 480P / 768P / 960P / 1080P。 |

`model` 字段仅作渠道标记，H3 兼容接口固定提交 `MiniMax-H3`。

## 素材要求

SparkCine 按公网 HTTPS 地址抓取素材，data URL 会校验失败，因此本插件声明 `requiresPublicMediaUrls`，由素材图床在上传后把地址写进 `content[]`。

## 限制

- H3 兼容接口暂不支持视频参考。
- 每 key 每分钟查询与提交各 300 次。
- 任务接受时冻结积分，失败不扣费。
