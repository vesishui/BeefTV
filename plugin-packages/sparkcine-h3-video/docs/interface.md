# SparkCine H3 视频接口字段

## 协议身份

- 插件 ID：`sparkcine-h3-video`。
- Provider ID：`sparkcine-h3-video`。
- 能力：`video`。
- 默认 Base URL：`https://hi.hi.run`。
- 鉴权驱动：`bearer`。
- 创建：`POST /api/h3/v2/video_generation`。
- 查询：`GET /api/h3/v2/query/video_generation/{taskId}`。
- 生命周期：异步任务 + 轮询。

## 配置字段

| 字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| `apiKey` | secret | 是 | API Key |

## 统一字段映射

| 统一字段 | 类型 | 必填 | 上游映射 | 说明 |
| --- | --- | --- | --- | --- |
| `prompt` | string | 是 | `content[0].text` | 视频提示词，作为 content 数组第一项。 |
| `images` | media[] | 否 | `content[].image_url` | 参考图；首尾帧模式赋予 first_frame / last_frame。 |
| `audios` | media[] | 否 | `content[].audio_url` | 参考音频。 |
| `ratio` | string | 否 | `ratio` | 只支持 16:9 与 9:16。 |
| `seconds` | integer | 否 | `duration` | 4-15 秒。 |
| `resolution` | string | 否 | `resolution` | 480P / 768P / 960P / 1080P。 |

## 上游请求模板

| 上游位置 | 值或转换表达式 |
| --- | --- |
| `create.method` | `"POST"` |
| `create.path` | `"/api/h3/v2/video_generation"` |
| `create.contentType` | `"application/json"` |
| `create.body.model` | `"MiniMax-H3"` |
| `create.body.content[0]` | `{type: "text", text: request.prompt}` |
| `create.body.content[1..n]` | `map(sortByOrder(request.images), {type:"image_url", image_url:{url: media.value}, role})` |
| `create.body.content[n+1..]` | `map(sortByOrder(request.audios), {type:"audio_url", audio_url:{url: media.value}})` |
| `create.body.resolution` | `coalesce(request.resolution, "768P")` |
| `create.body.duration` | `coalesce(request.seconds, 6)` |
| `create.body.ratio` | `coalesce(request.aspectRatio, "16:9")` |
| `poll.method` | `"GET"` |
| `poll.path` | `"/api/h3/v2/query/video_generation/{{taskId}}"` |

## 响应映射

| 映射位置 | 上游路径 |
| --- | --- |
| `response.taskId` | `task_id` / `id` / `task.id` |
| `response.status` | `status` / `task.status`（success / succeeded / completed 归一为成功） |
| `response.videos` | `content.url` / `task.content.url` / `task.content.video_url` |
| `response.message` | `error.message` / `task.error.message` / `message` |

## 兼容边界

SparkCine 按公网 HTTPS 地址抓取素材，data URL 会校验失败，因此 `requiresPublicMediaUrls` 为 `true`，由素材图床在上传后把地址写进 `content[]`。结果链接 `content.url` 仅 30 分钟内有效，过期需重新查询任务获取新链接。H3 兼容接口暂不支持视频参考。

<!-- BEEFTV_PLUGIN_MANIFEST_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内实际 `manifest.json` 逐字段一致，覆盖插件身份、权限、配置、鉴权、参数、校验、创建、Agent、查询、取消、结果下载、响应和 Agent 响应映射。`documentation` 字段的值就是当前完整文档；为避免文档在自身内部无限递归，JSON 中仅用等义占位文本表示正文。

```json
{
  "apiVersion": "beeftv.plugin/v2",
  "id": "sparkcine-h3-video",
  "name": "SparkCine H3 视频",
  "version": "1.0.0",
  "author": "BeefTV Contributors",
  "description": "SparkCine（hi.hi.run）H3 兼容视频协议：content[] 提交 + 任务 ID 轮询，素材需公网 URL。",
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
        "id": "sparkcine-h3-video",
        "label": "SparkCine H3 视频",
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
        "baseUrl": "https://hi.hi.run",
        "requiresPublicMediaUrls": true,
        "auth": {
          "type": "bearer",
          "field": "apiKey"
        },
        "parameters": [
          {
            "name": "model",
            "type": "string",
            "required": false,
            "mapping": "model",
            "description": "渠道模型 ID 仅作标记，H3 兼容接口固定提交 MiniMax-H3。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "content[0].text",
            "description": "视频提示词，作为 content 数组的第一项。"
          },
          {
            "name": "images",
            "type": "media[]",
            "required": false,
            "mapping": "content[].image_url",
            "description": "参考图公网地址。首尾帧模式按顺序对应 first_frame 与 last_frame。"
          },
          {
            "name": "audios",
            "type": "media[]",
            "required": false,
            "mapping": "content[].audio_url",
            "description": "参考音频公网地址，提示词里可用 @音频1 引用。"
          },
          {
            "name": "ratio",
            "type": "string",
            "required": false,
            "mapping": "ratio",
            "description": "画幅比例，只支持 16:9 与 9:16。"
          },
          {
            "name": "seconds",
            "type": "integer",
            "required": false,
            "mapping": "duration",
            "description": "时长 4-15 秒。"
          },
          {
            "name": "resolution",
            "type": "string",
            "required": false,
            "mapping": "resolution",
            "description": "分辨率，白名单 480P / 768P / 960P / 1080P。"
          }
        ],
        "create": {
          "method": "POST",
          "path": "/api/h3/v2/video_generation",
          "contentType": "application/json",
          "body": {
            "model": "MiniMax-H3",
            "content": {
              "$concatArrays": [
                [
                  {
                    "type": "text",
                    "text": {
                      "$ref": "request.prompt"
                    }
                  }
                ],
                {
                  "$map": {
                    "from": {
                      "$sortByOrder": {
                        "$ref": "request.images"
                      }
                    },
                    "as": "media",
                    "in": {
                      "type": "image_url",
                      "image_url": {
                        "url": {
                          "$ref": "media.value"
                        }
                      },
                      "role": {
                        "$if": {
                          "condition": {
                            "$eq": [
                              {
                                "$ref": "media.role"
                              },
                              "last_frame"
                            ]
                          },
                          "then": "last_frame",
                          "else": {
                            "$if": {
                              "condition": {
                                "$eq": [
                                  {
                                    "$ref": "media.order"
                                  },
                                  0
                                ]
                              },
                              "then": "first_frame",
                              "else": null
                            }
                          }
                        }
                      }
                    }
                  }
                },
                {
                  "$map": {
                    "from": {
                      "$sortByOrder": {
                        "$ref": "request.audios"
                      }
                    },
                    "as": "media",
                    "in": {
                      "type": "audio_url",
                      "audio_url": {
                        "url": {
                          "$ref": "media.value"
                        }
                      }
                    }
                  }
                }
              ]
            },
            "resolution": {
              "$coalesce": [
                {
                  "$ref": "request.resolution"
                },
                "768P"
              ]
            },
            "duration": {
              "$coalesce": [
                {
                  "$ref": "request.seconds"
                },
                6
              ]
            },
            "ratio": {
              "$coalesce": [
                {
                  "$ref": "request.aspectRatio"
                },
                "16:9"
              ]
            }
          }
        },
        "poll": {
          "method": "GET",
          "path": "/api/h3/v2/query/video_generation/{{taskId}}"
        },
        "response": {
          "taskId": {
            "$coalesce": [
              {
                "$ref": "response.task_id"
              },
              {
                "$ref": "response.id"
              },
              {
                "$ref": "response.task.id"
              },
              {
                "$ref": "taskId"
              }
            ]
          },
          "status": {
            "$switch": {
              "cases": [
                {
                  "when": {
                    "$in": [
                      {
                        "$lower": {
                          "$trim": {
                            "$coalesce": [
                              {
                                "$ref": "response.status"
                              },
                              {
                                "$ref": "response.task.status"
                              },
                              ""
                            ]
                          }
                        }
                      },
                      [
                        "success",
                        "succeeded",
                        "completed"
                      ]
                    ]
                  },
                  "then": "succeeded"
                },
                {
                  "when": {
                    "$in": [
                      {
                        "$lower": {
                          "$trim": {
                            "$coalesce": [
                              {
                                "$ref": "response.status"
                              },
                              {
                                "$ref": "response.task.status"
                              },
                              ""
                            ]
                          }
                        }
                      },
                      [
                        "failure",
                        "failed",
                        "error"
                      ]
                    ]
                  },
                  "then": "failed"
                }
              ],
              "default": "pending"
            }
          },
          "message": {
            "$coalesce": [
              {
                "$ref": "response.error.message"
              },
              {
                "$ref": "response.task.error.message"
              },
              {
                "$ref": "response.message"
              }
            ]
          },
          "videos": {
            "$coalesce": [
              {
                "$ref": "response.content.url"
              },
              {
                "$ref": "response.task.content.url"
              },
              {
                "$ref": "response.task.content.video_url"
              }
            ]
          },
          "errorPaths": [
            "error.code",
            "code"
          ],
          "messagePaths": [
            "error.message",
            "message"
          ],
          "resultEphemeral": true
        }
      }
    ]
  }
}
```
<!-- BEEFTV_PLUGIN_MANIFEST_END -->
