# 山海画布接口字段

## 协议身份

- 插件 ID：`shanhai-canvas`。
- Provider ID：`shanhai-video`、`shanhai-image`、`shanhai-text`。
- 能力：`video`、`image`、`text`。
- 默认 Base URL：`https://shanhai.vnshu.cn`。
- 鉴权驱动：`bearer`。
- 创建：`POST /api/v1/generations`。
- 查询：`GET /api/v1/tasks/{taskId}`。
- 生命周期：异步任务 + 轮询；文本模型通常同步返回。

## 配置字段

| 字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| `apiKey` | secret | 是 | 画布账户在「API 接入」创建的 Key（`oc_live_...`） |

## 统一字段映射

| 统一字段 | 类型 | 必填 | 上游映射 | 说明 |
| --- | --- | --- | --- | --- |
| `model` | string | 是 | `model` | `GET /models` 的 `data[].id`。 |
| `prompt` | string | 是 | `prompt` | 提示词。 |
| `ratio` | string | 否 | `options.aspect_ratio` | 画幅比例。 |
| `seconds` | integer | 否 | `options.duration` | 视频时长，写为字符串。 |
| `resolution` | string | 否 | `options.resolution` | 分辨率档位。 |
| `images` | media[] | 否 | `inputs[type=image]` | 参考图公网直链。 |
| `videos` | media[] | 否 | `inputs[type=video]` | 参考视频公网直链。 |
| `audios` | media[] | 否 | `inputs[type=audio]` | 参考音频公网直链。 |
| `providerOptions` | object | 否 | `providerOptions.shanhai-canvas.body` | 扩展字段合并进请求体。 |

## 上游请求模板

| 上游位置 | 值或转换表达式 |
| --- | --- |
| `create.method` | `"POST"` |
| `create.path` | `"/api/v1/generations"` |
| `create.contentType` | `"application/json"` |
| `create.body.model` | `request.model` |
| `create.body.prompt` | `request.prompt` |
| `create.body.media_type` | 按 provider 固定为 `"text"` / `"image"` / `"video"` |
| `create.body.inputs` | `request.images`、`request.videos`、`request.audios` 按序合并为 `{type,url}`，为空时整体省略 |
| `create.body.options.aspect_ratio` | `request.aspectRatio`，为空时省略 |
| `create.body.options.resolution` | `request.resolution`，为空时省略 |
| `create.body.options.duration` | `request.seconds` 转字符串，为空时省略（仅视频） |

## 响应映射

| 输出 | 取值 |
| --- | --- |
| `taskId` | `id` / `task_id` / `data.id` |
| `status` | `succeeded`、`success`、`completed`、`done` 归一为成功；`failed`、`failure`、`error`、`cancelled` 归一为失败；`queued`、`running` 及其余为进行中 |
| `message` | `error.message` / `error` / `message` |
| `videos` | `output.url` / `video_url` / `url` / `data.url`（视频 provider） |
| `images` | `output.url` / `image_url` / `url` / `data.url`（图片 provider） |
| `text` | `output.text` / `text`（文本 provider） |
| `usage` | `usage` |
| `errorPaths` | `error.code`、`code` |
| `messagePaths` | `error.message`、`message`、`error` |
| `resultEphemeral` | 图片、视频 provider 为 `true`（`output.url` 需带同一枚 Bearer Key 下载） |

## 素材要求

参考素材必须是公网可访问的 HTTPS 直链。图片和视频 provider 声明 `requiresPublicMediaUrls`，由 BeefTV 图床在上游请求之前完成上传替换。

音频参考需要先调用 `POST /api/v1/uploads/audio`（`multipart/form-data`，字段名固定为 `file`，支持 MP3 / WAV，最大 25MB）取得 HTTPS 地址；本插件不代为上传音频。音频参考还必须同时提供至少一张参考图或一段参考视频。

## 错误语义

| 状态码 | 含义 |
| --- | --- |
| 401 | Key 缺失、错误或已撤销。 |
| 403 | Key 没有对应权限。 |
| 422 | 参数、模型或输入地址不合法。 |
| 429 | 超过该 Key 的每分钟限流。 |

<!-- BEEFTV_PLUGIN_MANIFEST_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内实际 `manifest.json` 逐字段一致，覆盖插件身份、权限、配置、鉴权、参数、校验、创建、Agent、查询、取消、结果下载、响应和 Agent 响应映射。`documentation` 字段的值就是当前完整文档；为避免文档在自身内部无限递归，JSON 中仅用等义占位文本表示正文。

```json
{
  "apiVersion": "beeftv.plugin/v2",
  "id": "shanhai-canvas",
  "name": "山海画布",
  "version": "1.0.0",
  "author": "BeefTV Contributors",
  "description": "山海画布（shanhai.vnshu.cn）协议：POST /api/v1/generations 创建异步任务，GET /api/v1/tasks/{id} 轮询，成片走 output.url（需同一枚 Bearer Key）。覆盖文本、图片、视频三类模型。",
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
        "id": "shanhai-video",
        "label": "山海画布视频",
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
        "baseUrl": "https://shanhai.vnshu.cn",
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
            "description": "山海模型调用 ID，取 GET /models 返回的 data[].id，例如 oc-model-qbdmeb。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt",
            "description": "提示词，最长 20,000 字符。"
          },
          {
            "name": "ratio",
            "type": "string",
            "required": false,
            "mapping": "aspect_ratio",
            "description": "画幅比例，取值以 GET /models 的 capabilities.aspect_ratios 为准。"
          },
          {
            "name": "seconds",
            "type": "integer",
            "required": false,
            "mapping": "duration",
            "description": "视频时长（秒），映射到 options.duration，取值以模型 capabilities.durations 为准。"
          },
          {
            "name": "resolution",
            "type": "string",
            "required": false,
            "mapping": "resolution",
            "description": "分辨率档位，例如 720p，取值以模型 capabilities.resolutions 为准。"
          },
          {
            "name": "images",
            "type": "media[]",
            "required": false,
            "mapping": "images",
            "description": "参考图公网 HTTPS 直链数组，数量不超过 capabilities.max_reference_images。"
          },
          {
            "name": "videos",
            "type": "media[]",
            "required": false,
            "mapping": "videos",
            "description": "参考视频公网 HTTPS 直链数组。"
          },
          {
            "name": "audios",
            "type": "media[]",
            "required": false,
            "mapping": "audios",
            "description": "参考音频公网 HTTPS 直链数组；音频参考必须同时提供至少一张参考图或一段参考视频。"
          },
          {
            "name": "providerOptions",
            "type": "object",
            "required": false,
            "mapping": "provider-specific fields",
            "description": "山海尚未固定的扩展字段，可通过 providerOptions.shanhai-canvas.body 传入。"
          }
        ],
        "create": {
          "method": "POST",
          "path": "/api/v1/generations",
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
                "media_type": "video",
                "inputs": {
                  "$omitEmpty": {
                    "$concatArrays": [
                      {
                        "$map": {
                          "from": {
                            "$sortByOrder": {
                              "$ref": "request.images"
                            }
                          },
                          "as": "media",
                          "in": {
                            "type": "image",
                            "url": {
                              "$ref": "media.value"
                            }
                          }
                        }
                      },
                      {
                        "$map": {
                          "from": {
                            "$sortByOrder": {
                              "$ref": "request.videos"
                            }
                          },
                          "as": "media",
                          "in": {
                            "type": "video",
                            "url": {
                              "$ref": "media.value"
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
                            "type": "audio",
                            "url": {
                              "$ref": "media.value"
                            }
                          }
                        }
                      }
                    ]
                  }
                },
                "options": {
                  "$omitEmpty": {
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
                    "duration": {
                      "$omitEmpty": {
                        "$toString": {
                          "$ref": "request.seconds"
                        }
                      }
                    }
                  }
                }
              },
              {
                "$coalesce": [
                  {
                    "$ref": "request.providerOptions.shanhai-canvas.body"
                  },
                  {}
                ]
              }
            ]
          }
        },
        "poll": {
          "method": "GET",
          "path": "/api/v1/tasks/{{taskId}}"
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
              }
            ]
          },
          "videos": {
            "$coalesce": [
              {
                "$ref": "response.output.url"
              },
              {
                "$ref": "response.video_url"
              },
              {
                "$ref": "response.url"
              },
              {
                "$ref": "response.data.url"
              }
            ]
          },
          "resultEphemeral": true,
          "usage": {
            "$ref": "response.usage"
          },
          "errorPaths": [
            "error.code",
            "code"
          ],
          "messagePaths": [
            "error.message",
            "message",
            "error"
          ]
        }
      },
      {
        "id": "shanhai-image",
        "label": "山海画布图片",
        "capabilities": [
          "image"
        ],
        "scopes": [
          "admin.system-channel",
          "user.custom-channel",
          "canvas",
          "creation",
          "agent"
        ],
        "baseUrl": "https://shanhai.vnshu.cn",
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
            "description": "山海模型调用 ID，例如 shanhai-nano-banana-pro。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt",
            "description": "提示词，最长 20,000 字符。"
          },
          {
            "name": "ratio",
            "type": "string",
            "required": false,
            "mapping": "aspect_ratio",
            "description": "画幅比例，取值以 GET /models 的 capabilities.aspect_ratios 为准。"
          },
          {
            "name": "resolution",
            "type": "string",
            "required": false,
            "mapping": "resolution",
            "description": "图片分辨率档位，例如 1K / 2K / 4K。"
          },
          {
            "name": "images",
            "type": "media[]",
            "required": false,
            "mapping": "images",
            "description": "参考图公网 HTTPS 直链数组。"
          },
          {
            "name": "videos",
            "type": "media[]",
            "required": false,
            "mapping": "videos",
            "description": "参考视频公网 HTTPS 直链数组，仅部分模型支持。"
          },
          {
            "name": "audios",
            "type": "media[]",
            "required": false,
            "mapping": "audios",
            "description": "参考音频公网 HTTPS 直链数组，仅部分模型支持。"
          },
          {
            "name": "providerOptions",
            "type": "object",
            "required": false,
            "mapping": "provider-specific fields",
            "description": "山海尚未固定的扩展字段，可通过 providerOptions.shanhai-canvas.body 传入。"
          }
        ],
        "create": {
          "method": "POST",
          "path": "/api/v1/generations",
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
                "media_type": "image",
                "inputs": {
                  "$omitEmpty": {
                    "$concatArrays": [
                      {
                        "$map": {
                          "from": {
                            "$sortByOrder": {
                              "$ref": "request.images"
                            }
                          },
                          "as": "media",
                          "in": {
                            "type": "image",
                            "url": {
                              "$ref": "media.value"
                            }
                          }
                        }
                      },
                      {
                        "$map": {
                          "from": {
                            "$sortByOrder": {
                              "$ref": "request.videos"
                            }
                          },
                          "as": "media",
                          "in": {
                            "type": "video",
                            "url": {
                              "$ref": "media.value"
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
                            "type": "audio",
                            "url": {
                              "$ref": "media.value"
                            }
                          }
                        }
                      }
                    ]
                  }
                },
                "options": {
                  "$omitEmpty": {
                    "aspect_ratio": {
                      "$omitEmpty": {
                        "$ref": "request.aspectRatio"
                      }
                    },
                    "resolution": {
                      "$omitEmpty": {
                        "$ref": "request.resolution"
                      }
                    }
                  }
                }
              },
              {
                "$coalesce": [
                  {
                    "$ref": "request.providerOptions.shanhai-canvas.body"
                  },
                  {}
                ]
              }
            ]
          }
        },
        "poll": {
          "method": "GET",
          "path": "/api/v1/tasks/{{taskId}}"
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
              }
            ]
          },
          "images": {
            "$coalesce": [
              {
                "$ref": "response.output.url"
              },
              {
                "$ref": "response.image_url"
              },
              {
                "$ref": "response.url"
              },
              {
                "$ref": "response.data.url"
              }
            ]
          },
          "resultEphemeral": true,
          "usage": {
            "$ref": "response.usage"
          },
          "errorPaths": [
            "error.code",
            "code"
          ],
          "messagePaths": [
            "error.message",
            "message",
            "error"
          ]
        }
      },
      {
        "id": "shanhai-text",
        "label": "山海画布文本",
        "capabilities": [
          "text"
        ],
        "scopes": [
          "admin.system-channel",
          "user.custom-channel",
          "canvas",
          "creation",
          "agent"
        ],
        "baseUrl": "https://shanhai.vnshu.cn",
        "requiresPublicMediaUrls": false,
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
            "description": "山海文本模型调用 ID，例如 shanhai-deepseek-v4-pro。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt",
            "description": "提示词，最长 20,000 字符。"
          },
          {
            "name": "providerOptions",
            "type": "object",
            "required": false,
            "mapping": "provider-specific fields",
            "description": "山海尚未固定的扩展字段，可通过 providerOptions.shanhai-canvas.body 传入。"
          }
        ],
        "create": {
          "method": "POST",
          "path": "/api/v1/generations",
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
                "media_type": "text"
              },
              {
                "$coalesce": [
                  {
                    "$ref": "request.providerOptions.shanhai-canvas.body"
                  },
                  {}
                ]
              }
            ]
          }
        },
        "poll": {
          "method": "GET",
          "path": "/api/v1/tasks/{{taskId}}"
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
              }
            ]
          },
          "text": {
            "$coalesce": [
              {
                "$ref": "response.output.text"
              },
              {
                "$ref": "response.text"
              }
            ]
          },
          "usage": {
            "$ref": "response.usage"
          },
          "errorPaths": [
            "error.code",
            "code"
          ],
          "messagePaths": [
            "error.message",
            "message",
            "error"
          ]
        }
      }
    ]
  },
  "documentation": "<当前插件的完整 documentation，由 README.md 与 docs/interface.md 拼接而成；为避免 JSON 递归，此处不重复展开正文。>"
}
```
<!-- BEEFTV_PLUGIN_MANIFEST_END -->
