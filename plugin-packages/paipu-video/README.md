# 派普视频

派普网关（api.paipu.net）的视频生成协议插件。派普使用 JSON 请求体，不是 [OI] Videos / Sora 的 multipart 形状。

## 协议身份

- 插件 ID：`paipu-video`
- Provider ID：`paipu-video`
- 能力：`video`
- 默认 Base URL：`https://api.paipu.net`
- 鉴权：Bearer

## 接口

| 阶段 | 方法与路径 |
| --- | --- |
| 创建任务 | `POST /v1/videos` |
| 查询任务 | `GET /v1/videos/{task_id}` |
| 下载成片 | `GET /v1/videos/{task_id}/content` |

创建返回任务 `id`；查询在 `status` 为 `succeeded` 后由 BeefTV 请求 `/content` 取回成片。下载地址与渠道同源，会带上同一枚 Bearer Key，因此结果标记为 `resultEphemeral`。

## 参数

| 统一字段 | 上游字段 | 说明 |
| --- | --- | --- |
| `model` | `model` | 派普公开模型 ID，例如 `lec-xg-sd-2-5`。 |
| `prompt` | `prompt` | 非空提示词。 |
| `ratio` | `aspect_ratio` | 支持 16:9 / 9:16，默认 16:9。 |
| `seconds` | `duration` | 整数秒，默认 4，按模型能力区间取值。 |
| `resolution` | `resolution` | 当前固定 720p。 |
| `images` | `images` | 参考图公网地址数组，最多 30 张。 |
| `videos` | `videos` | 参考视频公网地址数组，最多 10 段。 |
| `audios` | `audios` | 参考音频公网地址数组，最多 10 段。 |

派普的 `face` 等扩展字段可通过 `providerOptions.paipu-video.body` 传入，例如 `{"face": true}` 开启内置过人脸。

## 素材要求

派普要求参考素材是公网可访问的 HTTPS 地址，因此本插件声明 `requiresPublicMediaUrls`，由素材图床在上游请求之前完成上传替换。提示词里可用 `@image1`、`@image2` 引用对应素材。

## 限制

- `lec-xg-sd-2-5`：720p，4-30 秒，画幅 16:9 / 9:16，最多 30 张参考图、10 段参考视频、10 段参考音频，按次计费。
- 缺少必填参数返回 400 `invalid_params`，错误信息原样透传。
