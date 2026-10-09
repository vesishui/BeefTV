# OpenVideo 视频接口

## 协议身份

- 插件 ID：`openvideo-video`
- Provider ID：`openvideo-video`
- 能力：`video`
- 默认 Base URL：`https://www.openvideo.top/api/v1`
- 鉴权：Bearer API Key
- 模型列表：`GET /models`
- 创建：`POST /videos`
- 查询：`GET /videos/{id}`
- 生命周期：异步任务 + 轮询

## 配置字段

| 字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| `apiKey` | secret | 是 | OpenVideo API Key |

## 统一字段映射

| 统一字段 | 类型 | 必填 | OpenVideo 字段 | 说明 |
| --- | --- | --- | --- | --- |
| `model` | string | 是 | `model` | 从 `GET /models` 返回的 `data[].id` |
| `prompt` | string | 是 | `prompt` | 视频提示词 |
| `images` | media[] | 否 | `images` | 公网 HTTPS 图片数组 |
| `videos` | media[] | 否 | `videos` | 公网 HTTPS 视频数组 |
| `audios` | media[] | 否 | `audios` | 公网 HTTPS 音频数组 |
| `duration` | integer | 否 | `duration` | 时长秒数 |
| `aspectRatio` | string | 否 | `ratio` | 画幅比例 |
| `resolution` | string | 否 | `resolution` | 分辨率档位 |
| `providerOptions.openvideo.style_id` | string | 否 | `style_id` | 服务商风格 ID |
| `providerOptions.openvideo.team_id` | string | 否 | `team_id` | 服务商团队 ID |

## 请求模板

| 上游位置 | 值 |
| --- | --- |
| `create.method` | `POST` |
| `create.path` | `/videos` |
| `create.contentType` | `application/json` |
| `create.body.object` | 固定值 `video` |
| `create.body.model` | `request.model` |
| `create.body.prompt` | `request.prompt` |
| `create.body.images` | `request.images[].value` |
| `create.body.videos` | `request.videos[].value` |
| `create.body.audios` | `request.audios[].value` |
| `create.body.duration` | `request.duration` |
| `create.body.ratio` | `request.aspectRatio` |
| `create.body.resolution` | `request.resolution` |
| `create.body.style_id` | `request.providerOptions.openvideo.style_id` |
| `create.body.team_id` | `request.providerOptions.openvideo.team_id` |
| `poll.method` | `GET` |
| `poll.path` | `/videos/{{taskId}}` |

## 响应映射

| 映射位置 | 上游路径 |
| --- | --- |
| `response.taskId` | `id` / `data.id` |
| `response.status` | `state` / 数值 `status` |
| `response.videos` | `result_url` / `result_urls` |
| `response.message` | `error.message` / `error` |

## 素材上传边界

服务商的 `POST /assets/uploads` 是一个独立的 multipart 便利接口。它不属于创建请求
的单次声明式模板，不能在 manifest 中表达“上传后再创建”的串行编排；本插件使用
BeefTV 的 `requiresPublicMediaUrls` 媒体策略，让现有图床缓存负责把本地素材转换为
公网 HTTPS URL。用户也可以在服务商侧预先上传素材，然后直接填入 URL。

## 未确认的接口假设

- 默认域名使用 `https://www.openvideo.top`；如果实际服务域名不同，在渠道设置中覆盖
  Base URL，路径仍保持 `/api/v1` 下的相对路径。
- `/models` 响应支持标准 OpenAI 的 `data[].id`，也支持 OpenVideo 的
  `data.video`、`data.image`、`data.audio`、`data.chat` 分组；模型列表读取由 BeefTV
  通用渠道模型探测完成。
- 创建和轮询响应按文档使用扁平字段；插件额外兼容常见的 `data.id`、`data.result_url`
  和 `data.result_urls` 包装。
- 未发送 `team_id` 时由服务商按其个人余额规则处理；插件不会自动填入团队 ID。

<!-- BEEFTV_PLUGIN_MANIFEST_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内实际 `manifest.json` 逐字段一致，覆盖插件身份、权限、配置、鉴权、参数、校验、创建、Agent、查询、取消、结果下载、响应和 Agent 响应映射。`documentation` 字段的值就是当前完整文档；为避免文档在自身内部无限递归，JSON 中仅用等义占位文本表示正文。

```json
{
  "apiVersion": "beeftv.plugin/v2",
  "id": "openvideo-video",
  "name": "OpenVideo 视频",
  "version": "1.0.0",
  "author": "BeefTV Contributors",
  "description": "OpenVideo /api/v1 异步视频协议：模型列表、视频创建与任务轮询。",
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
        "id": "openvideo-video",
        "label": "OpenVideo 视频",
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
        "baseUrl": "https://www.openvideo.top/api/v1",
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
            "description": "从 GET /models 返回结果中复制的模型 ID。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt",
            "description": "视频提示词。"
          },
          {
            "name": "images",
            "type": "media[]",
            "required": false,
            "mapping": "images",
            "description": "参考图片公网 HTTPS 地址数组。"
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
            "name": "duration",
            "type": "integer",
            "required": false,
            "mapping": "duration",
            "description": "视频时长（秒）。"
          },
          {
            "name": "aspectRatio",
            "type": "string",
            "required": false,
            "mapping": "ratio",
            "description": "画幅比例，例如 16:9。"
          },
          {
            "name": "resolution",
            "type": "string",
            "required": false,
            "mapping": "resolution",
            "description": "分辨率档位，例如 720p。"
          },
          {
            "name": "providerOptions",
            "type": "object",
            "required": false,
            "mapping": "provider-specific fields",
            "description": "通过 providerOptions.openvideo 传入 style_id、team_id 或新增字段。"
          }
        ],
        "create": {
          "method": "POST",
          "path": "/videos",
          "contentType": "application/json",
          "body": {
            "$merge": [
              {
                "object": "video",
                "model": {
                  "$ref": "request.model"
                },
                "prompt": {
                  "$ref": "request.prompt"
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
                },
                "duration": {
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
                "ratio": {
                  "$omitEmpty": {
                    "$ref": "request.aspectRatio"
                  }
                },
                "resolution": {
                  "$omitEmpty": {
                    "$ref": "request.resolution"
                  }
                },
                "style_id": {
                  "$omitEmpty": {
                    "$ref": "request.providerOptions.openvideo.style_id"
                  }
                },
                "team_id": {
                  "$omitEmpty": {
                    "$ref": "request.providerOptions.openvideo.team_id"
                  }
                }
              },
              {
                "$coalesce": [
                  {
                    "$ref": "request.providerOptions.openvideo.body"
                  },
                  {}
                ]
              }
            ]
          }
        },
        "poll": {
          "method": "GET",
          "path": "/videos/{{taskId}}"
        },
        "response": {
          "taskId": {
            "$coalesce": [
              {
                "$ref": "response.id"
              },
              {
                "$ref": "response.data.id"
              },
              {
                "$ref": "response.task_id"
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
                                "$ref": "response.state"
                              },
                              {
                                "$toString": {
                                  "$ref": "response.status"
                                }
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
                        "done",
                        "2"
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
                                "$ref": "response.state"
                              },
                              {
                                "$toString": {
                                  "$ref": "response.status"
                                }
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
                        "3"
                      ]
                    ]
                  },
                  "then": "failed"
                },
                {
                  "when": {
                    "$in": [
                      {
                        "$lower": {
                          "$trim": {
                            "$coalesce": [
                              {
                                "$ref": "response.state"
                              },
                              {
                                "$toString": {
                                  "$ref": "response.status"
                                }
                              },
                              ""
                            ]
                          }
                        }
                      },
                      [
                        "processing",
                        "in_progress",
                        "running",
                        "1"
                      ]
                    ]
                  },
                  "then": "processing"
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
                "$ref": "response.data.error"
              }
            ]
          },
          "videos": {
            "$coalesce": [
              {
                "$ref": "response.result_urls"
              },
              {
                "$ref": "response.result_url"
              },
              {
                "$ref": "response.data.result_urls"
              },
              {
                "$ref": "response.data.result_url"
              }
            ]
          },
          "errorPaths": [
            "error.code",
            "code"
          ],
          "messagePaths": [
            "error.message",
            "error",
            "message"
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
