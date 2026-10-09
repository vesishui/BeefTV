# 派普视频接口字段

## 协议身份

- 插件 ID：`paipu-video`。
- Provider ID：`paipu-video`。
- 能力：`video`。
- 默认 Base URL：`https://api.paipu.net`。
- 鉴权驱动：`bearer`。
- 创建：`POST /v1/videos`。
- 查询：`GET /v1/videos/{task_id}`。
- 结果下载：`GET /v1/videos/{task_id}/content`。
- 生命周期：异步任务 + 轮询 + 结果端点下载。

## 配置字段

| 字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| `apiKey` | secret | 是 | API Key |

## 统一字段映射

| 统一字段 | 类型 | 必填 | 上游映射 | 说明 |
| --- | --- | --- | --- | --- |
| `model` | string | 是 | `model` | 派普公开模型 ID。 |
| `prompt` | string | 是 | `prompt` | 视频提示词。 |
| `ratio` | string | 否 | `aspect_ratio` | 画幅比例，默认 16:9。 |
| `seconds` | integer | 否 | `duration` | 时长秒数，默认 4。 |
| `resolution` | string | 否 | `resolution` | 分辨率，默认 720p。 |
| `images` | media[] | 否 | `images` | 参考图公网地址数组。 |
| `videos` | media[] | 否 | `videos` | 参考视频公网地址数组。 |
| `audios` | media[] | 否 | `audios` | 参考音频公网地址数组。 |
| `providerOptions` | object | 否 | `providerOptions.paipu-video.body` | 派普扩展字段合并进请求体。 |

## 上游请求模板

| 上游位置 | 值或转换表达式 |
| --- | --- |
| `create.method` | `"POST"` |
| `create.path` | `"/v1/videos"` |
| `create.contentType` | `"application/json"` |
| `create.body.model` | `request.model` |
| `create.body.prompt` | `request.prompt` |
| `create.body.duration` | `request.seconds`，缺省 4 |
| `create.body.aspect_ratio` | `request.aspectRatio`，缺省 `"16:9"` |
| `create.body.resolution` | `request.resolution`，缺省 `"720p"` |
| `create.body.images` | `request.images` 按序取值 |
| `create.body.videos` | `request.videos` 按序取值 |
| `create.body.audios` | `request.audios` 按序取值 |

## 响应映射

| 输出 | 取值 |
| --- | --- |
| `taskId` | `id` / `task_id` / `taskId` / `data.id` |
| `status` | `succeeded`、`success`、`completed`、`done` 归一为成功；`failed`、`failure`、`error`、`cancelled` 归一为失败；其余为进行中 |
| `message` | `error.message` / `error` / `message` / `fail_reason` |
| `videos` | `video_url` / `videoUrl` / `url` / `output.url` / `data.video_url` |
| `errorPaths` | `error.code`、`code` |
| `messagePaths` | `error.message`、`message`、`fail_reason` |
| `resultEphemeral` | `true`（成片从同源 `/content` 端点下载并附带 Bearer Key） |

## 素材要求

参考素材必须是公网可访问的 HTTPS 直链。插件声明 `requiresPublicMediaUrls`，由 BeefTV 图床在上游请求之前完成上传替换；不接受本地路径、登录页或仅浏览器可访问的地址。

## 错误语义

| 状态码 | 含义 |
| --- | --- |
| 400 | 参数缺失或非法（`invalid_params`），例如把 JSON 请求发成 multipart。 |
| 401 | Key 缺失或错误。 |
| 402 | 余额不足。 |
| 403 | Key 无权限。 |

<!-- BEEFTV_PLUGIN_MANIFEST_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内实际 `manifest.json` 逐字段一致，覆盖插件身份、权限、配置、鉴权、参数、校验、创建、Agent、查询、取消、结果下载、响应和 Agent 响应映射。`documentation` 字段的值就是当前完整文档；为避免文档在自身内部无限递归，JSON 中仅用等义占位文本表示正文。

```json
{
  "apiVersion": "beeftv.plugin/v2",
  "id": "paipu-video",
  "name": "派普视频",
  "version": "1.0.0",
  "author": "BeefTV Contributors",
  "description": "派普网关（api.paipu.net）视频生成协议：JSON 提交 POST /v1/videos，任务查询 GET /v1/videos/{task_id}，成片下载 GET /v1/videos/{task_id}/content；参考素材需公网 HTTPS 地址。",
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
        "id": "paipu-video",
        "label": "派普视频",
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
        "baseUrl": "https://api.paipu.net",
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
            "description": "派普公开模型 ID，例如 lec-xg-sd-2-5，请从模型文档复制。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt",
            "description": "非空的视频生成提示词。"
          },
          {
            "name": "aspectRatio",
            "type": "string",
            "required": false,
            "mapping": "aspect_ratio",
            "description": "输出画幅，支持 16:9 / 9:16，默认 16:9。"
          },
          {
            "name": "seconds",
            "type": "integer",
            "required": false,
            "mapping": "duration",
            "description": "视频时长（秒），整数，按模型能力区间取值，默认 4。"
          },
          {
            "name": "resolution",
            "type": "string",
            "required": false,
            "mapping": "resolution",
            "description": "分辨率档位，派普视频当前固定为 720p。"
          },
          {
            "name": "images",
            "type": "media[]",
            "required": false,
            "mapping": "images",
            "description": "参考图公网 HTTPS 地址数组，最多 30 张，由 BeefTV 图床先完成上传。"
          },
          {
            "name": "videos",
            "type": "media[]",
            "required": false,
            "mapping": "videos",
            "description": "参考视频公网 HTTPS 地址数组，最多 10 段。"
          },
          {
            "name": "audios",
            "type": "media[]",
            "required": false,
            "mapping": "audios",
            "description": "参考音频公网 HTTPS 地址数组，最多 10 段。"
          },
          {
            "name": "providerOptions",
            "type": "object",
            "required": false,
            "mapping": "provider-specific fields",
            "description": "派普扩展字段，可通过 providerOptions.paipu-video.body 传入，例如 {\"face\": true} 开启过人脸。"
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
                "duration": {
                  "$coalesce": [
                    {
                      "$ref": "request.seconds"
                    },
                    4
                  ]
                },
                "aspect_ratio": {
                  "$coalesce": [
                    {
                      "$ref": "request.aspectRatio"
                    },
                    "16:9"
                  ]
                },
                "resolution": {
                  "$coalesce": [
                    {
                      "$ref": "request.resolution"
                    },
                    "720p"
                  ]
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
                    "$ref": "request.providerOptions.paipu-video.body"
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
                "$ref": "response.taskId"
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
                              ""
                            ]
                          }
                        }
                      },
                      [
                        "succeeded",
                        "success",
                        "completed",
                        "done"
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
                              ""
                            ]
                          }
                        }
                      },
                      [
                        "failed",
                        "failure",
                        "error",
                        "cancelled",
                        "canceled"
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
                "$ref": "response.error"
              },
              {
                "$ref": "response.message"
              },
              {
                "$ref": "response.fail_reason"
              }
            ]
          },
          "videos": {
            "$coalesce": [
              {
                "$ref": "response.video_url"
              },
              {
                "$ref": "response.videoUrl"
              },
              {
                "$ref": "response.url"
              },
              {
                "$ref": "response.output.url"
              },
              {
                "$ref": "response.data.video_url"
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
            "fail_reason"
          ],
          "resultEphemeral": true
        }
      }
    ]
  },
  "documentation": "<当前插件的完整 documentation，由 README.md 与 docs/interface.md 拼接而成；为避免 JSON 递归，此处不重复展开正文。>"
}
```
<!-- BEEFTV_PLUGIN_MANIFEST_END -->
