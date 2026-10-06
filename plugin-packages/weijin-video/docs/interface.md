# 魏进视频接口字段

## 协议身份

- 插件 ID：`weijin-video`
- Provider ID：`weijin-video`
- 能力：`video`
- 默认 Base URL：`https://www.weijinapi.top`
- 鉴权：Bearer API Key
- 创建：`POST /v1/videos`
- 查询：`GET /v1/videos/{{taskId}}`
- 内容下载：`GET /v1/videos/{{taskId}}/content`
- 生命周期：异步任务 + 轮询 + 独立二进制结果下载

## 配置字段

| 字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| `apiKey` | secret | 是 | 魏进 API Key |

## 统一字段映射

| 统一字段 | 类型 | 必填 | 魏进字段 | 说明 |
| --- | --- | --- | --- | --- |
| `model` | string | 是 | `model` | 从 `GET /v1/models` 复制模型 ID。 |
| `prompt` | string | 是 | `prompt` | 视频提示词。 |
| `duration` | integer | 否 | `seconds` | 视频时长。 |
| `aspectRatio` | string | 否 | `aspect_ratio` | 例如 `16:9`、`9:16`。 |
| `resolution` | string | 否 | `resolution` | 例如 `720p`、`1080p`。 |
| `images` | media[] | 否 | `images` | 公网 HTTPS 图片 URL 数组。 |
| `videos` | media[] | 否 | `videos` | 公网 HTTPS 视频 URL 数组。 |
| `audios` | media[] | 否 | `audios` | 公网 HTTPS 音频 URL 数组。 |

## 请求模板

| 上游位置 | 值 |
| --- | --- |
| `create.method` | `POST` |
| `create.path` | `/v1/videos` |
| `create.contentType` | `application/json` |
| `create.body.model` | `request.model` |
| `create.body.prompt` | `request.prompt` |
| `create.body.seconds` | `request.duration`，小于等于 0 时省略 |
| `create.body.aspect_ratio` | `request.aspectRatio` |
| `create.body.resolution` | `request.resolution` |
| `create.body.images` | `request.images[].value` |
| `create.body.videos` | `request.videos[].value` |
| `create.body.audios` | `request.audios[].value` |
| `poll.path` | `/v1/videos/{{taskId}}` |
| `result.path` | `/v1/videos/{{taskId}}/content` |

`providerOptions.weijin-video.body` 可用于追加魏进新增加的请求字段；标准字段仍由 BeefTV 统一生成。

## 响应映射

创建或轮询响应中的任务 ID按以下顺序读取：

- `id`
- `task_id`
- `data.id`

状态读取：

- `status`
- `state`
- `data.status`

完成结果 URL读取：

- `result_url`
- `video_url`
- `url`
- `content`
- `content.url`
- `content.video_url`
- `data.result_url`
- `data.video_url`
- `data.url`

如果轮询完成但没有直接返回 URL，宿主调用独立 `result` 操作下载 MP4，并持久化为 BeefTV 视频素材。

## 素材处理

魏进的视频请求要求参考素材使用公网 HTTPS URL。本插件声明 `requiresPublicMediaUrls: true`，因此不会把本地路径或 data URL 直接交给魏进；图片、音频、视频先走 BeefTV 已有素材图床和缓存流程。

<!-- BEEFTV_PLUGIN_MANIFEST_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内实际 `manifest.json` 逐字段一致，覆盖插件身份、权限、配置、鉴权、参数、校验、创建、Agent、查询、取消、结果下载、响应和 Agent 响应映射。`documentation` 字段的值就是当前完整文档；为避免文档在自身内部无限递归，JSON 中仅用等义占位文本表示正文。

```json
{
  "apiVersion": "beeftv.plugin/v2",
  "id": "weijin-video",
  "name": "魏进视频",
  "version": "1.0.0",
  "author": "BeefTV Contributors",
  "description": "魏进 API 视频生成协议：/v1/videos 异步创建、任务轮询和 MP4 内容下载。",
  "documentation": "<当前插件的完整 documentation，由 README.md 与 docs/interface.md 拼接而成；为避免 JSON 递归，此处不重复展开正文。>",
  "permissions": [
    "generation.run",
    "media.read"
  ],
  "configuration": {
    "fields": [
      {
        "name": "apiKey",
        "type": "secret",
        "label": "API Key",
        "required": true
      }
    ]
  },
  "contributes": {
    "providers": [
      {
        "id": "weijin-video",
        "label": "魏进视频",
        "capabilities": [
          "video"
        ],
        "scopes": [
          "admin.system-channel",
          "user.custom-channel",
          "canvas",
          "creation",
          "agent"
        ],
        "baseUrl": "https://www.weijinapi.top",
        "requiresPublicMediaUrls": true,
        "auth": {
          "type": "bearer",
          "field": "apiKey"
        },
        "parameters": [
          {
            "name": "model",
            "type": "string",
            "required": true,
            "mapping": "model",
            "description": "魏进模型 ID，建议从 GET /v1/models 返回结果中复制。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt",
            "description": "视频提示词。"
          },
          {
            "name": "duration",
            "type": "integer",
            "required": false,
            "mapping": "seconds",
            "description": "视频时长，映射到魏进的 seconds。"
          },
          {
            "name": "aspectRatio",
            "type": "string",
            "required": false,
            "mapping": "aspect_ratio",
            "description": "画幅比例，例如 16:9 或 9:16。"
          },
          {
            "name": "resolution",
            "type": "string",
            "required": false,
            "mapping": "resolution",
            "description": "分辨率档位，例如 720p 或 1080p。"
          },
          {
            "name": "images",
            "type": "media[]",
            "required": false,
            "mapping": "images",
            "description": "参考图片公网 HTTPS 地址数组，由 BeefTV 图床先完成上传。"
          },
          {
            "name": "videos",
            "type": "media[]",
            "required": false,
            "mapping": "videos",
            "description": "参考视频公网 HTTPS 地址数组。"
          },
          {
            "name": "audios",
            "type": "media[]",
            "required": false,
            "mapping": "audios",
            "description": "参考音频公网 HTTPS 地址数组。"
          },
          {
            "name": "providerOptions",
            "type": "object",
            "required": false,
            "mapping": "provider-specific fields",
            "description": "魏进尚未固定的扩展字段，可通过 providerOptions.weijin-video.body 传入。"
          }
        ],
        "create": {
          "method": "POST",
          "path": "/v1/videos",
          "contentType": "application/json",
          "body": {
            "$merge": [
              {
                "model": {
                  "$ref": "request.model"
                },
                "prompt": {
                  "$ref": "request.prompt"
                },
                "seconds": {
                  "$omitEmpty": {
                    "$if": {
                      "condition": {
                        "$gt": [
                          {
                            "$ref": "request.duration"
                          },
                          0
                        ]
                      },
                      "then": {
                        "$ref": "request.duration"
                      },
                      "else": null
                    }
                  }
                },
                "aspect_ratio": {
                  "$omitEmpty": {
                    "$ref": "request.aspectRatio"
                  }
                },
                "resolution": {
                  "$omitEmpty": {
                    "$ref": "request.resolution"
                  }
                },
                "images": {
                  "$omitEmpty": {
                    "$map": {
                      "from": {
                        "$sortByOrder": {
                          "$ref": "request.images"
                        }
                      },
                      "as": "media",
                      "in": {
                        "$ref": "media.value"
                      }
                    }
                  }
                },
                "videos": {
                  "$omitEmpty": {
                    "$map": {
                      "from": {
                        "$sortByOrder": {
                          "$ref": "request.videos"
                        }
                      },
                      "as": "media",
                      "in": {
                        "$ref": "media.value"
                      }
                    }
                  }
                },
                "audios": {
                  "$omitEmpty": {
                    "$map": {
                      "from": {
                        "$sortByOrder": {
                          "$ref": "request.audios"
                        }
                      },
                      "as": "media",
                      "in": {
                        "$ref": "media.value"
                      }
                    }
                  }
                }
              },
              {
                "$coalesce": [
                  {
                    "$ref": "request.providerOptions.weijin-video.body"
                  },
                  {}
                ]
              }
            ]
          }
        },
        "poll": {
          "method": "GET",
          "path": "/v1/videos/{{taskId}}"
        },
        "result": {
          "method": "GET",
          "path": "/v1/videos/{{taskId}}/content",
          "headers": {
            "Accept": "video/mp4"
          }
        },
        "response": {
          "taskId": {
            "$coalesce": [
              {
                "$ref": "response.id"
              },
              {
                "$ref": "response.task_id"
              },
              {
                "$ref": "response.data.id"
              },
              {
                "$ref": "taskId"
              }
            ]
          },
          "status": {
            "$coalesce": [
              {
                "$ref": "response.status"
              },
              {
                "$ref": "response.state"
              },
              {
                "$ref": "response.data.status"
              },
              "pending"
            ]
          },
          "message": {
            "$coalesce": [
              {
                "$ref": "response.error.message"
              },
              {
                "$ref": "response.error"
              },
              {
                "$ref": "response.message"
              },
              {
                "$ref": "response.data.error"
              }
            ]
          },
          "videos": {
            "$coalesce": [
              {
                "$ref": "response.result_url"
              },
              {
                "$ref": "response.video_url"
              },
              {
                "$ref": "response.url"
              },
              {
                "$ref": "response.content"
              },
              {
                "$ref": "response.content.url"
              },
              {
                "$ref": "response.content.video_url"
              },
              {
                "$ref": "response.data.result_url"
              },
              {
                "$ref": "response.data.video_url"
              },
              {
                "$ref": "response.data.url"
              }
            ]
          },
          "errorPaths": [
            "error.code",
            "code"
          ],
          "messagePaths": [
            "error.message",
            "message",
            "error"
          ],
          "resultEphemeral": true
        }
      }
    ]
  }
}
```
<!-- BEEFTV_PLUGIN_MANIFEST_END -->
