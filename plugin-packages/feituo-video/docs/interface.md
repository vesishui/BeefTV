# 飞拓视频接口字段

## 协议身份

- 插件 ID：`feituo-video`。
- Provider ID：`feituo-video`。
- 能力：`video`。
- 默认 Base URL：`https://feituokuajing.com`。
- 鉴权驱动：`bearer`。
- 创建：`POST /api/open/v1/video/generate`。
- 查询：`GET /api/open/v1/video/status?jobId={taskId}`。
- 生命周期：异步任务 + 轮询。

## 配置字段

| 字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| `apiKey` | secret | 是 | API Key |

## 统一字段映射

| 统一字段 | 类型 | 必填 | 上游映射 | 说明 |
| --- | --- | --- | --- | --- |
| `model` | string | 是 | `model` | 飞拓模型 ID。 |
| `prompt` | string | 是 | `prompt` | 视频提示词。 |
| `ratio` | string | 否 | `ratio` | 画幅比例。 |
| `seconds` | integer | 否 | `duration` | 时长秒数。 |
| `resolution` | string | 否 | `resolution` | 分辨率，默认 720p。 |
| `images` | media[] | 否 | `imageUrls` | 参考图公网地址数组。 |
| `videos` | media[] | 否 | `videoUrls` | 参考视频公网地址数组。 |
| `audios` | media[] | 否 | `audioUrls` | 参考音频公网地址数组。 |

## 上游请求模板

| 上游位置 | 值或转换表达式 |
| --- | --- |
| `create.method` | `"POST"` |
| `create.path` | `"/api/open/v1/video/generate"` |
| `create.contentType` | `"application/json"` |
| `create.body.model` | `request.model` |
| `create.body.prompt` | `request.prompt` |
| `create.body.ratio` | `coalesce(request.aspectRatio, "9:16")` |
| `create.body.duration` | `coalesce(request.seconds, 10)` |
| `create.body.resolution` | `coalesce(request.resolution, "720p")` |
| `create.body.imageUrls` | `map(sortByOrder(request.images), media.value)` |
| `create.body.videoUrls` | `map(sortByOrder(request.videos), media.value)` |
| `create.body.audioUrls` | `map(sortByOrder(request.audios), media.value)` |
| `poll.method` | `"GET"` |
| `poll.path` | `"/api/open/v1/video/status"` |
| `poll.query.jobId` | `taskId` |

## 响应映射

| 映射位置 | 上游路径 |
| --- | --- |
| `response.taskId` | `jobId` / `data.jobId` / `id` |
| `response.status` | `status`（success / submitted / failed 归一为统一生命周期） |
| `response.videos` | `videoUrl` / `video_url` / `data.videoUrl` / `result_url` |
| `response.message` | `errorMessage` / `error.message` / `message` |

## 兼容边界

飞拓要求参考素材是公网可访问的 HTTPS 地址，`requiresPublicMediaUrls` 为 `true`，由素材图床在上游请求之前完成上传替换。比例白名单为 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9，时长按 5 / 10 / 15 秒归并。`submitted` 表示仍在排队或生成中，不算失败。

<!-- BEEFTV_PLUGIN_MANIFEST_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内实际 `manifest.json` 逐字段一致，覆盖插件身份、权限、配置、鉴权、参数、校验、创建、Agent、查询、取消、结果下载、响应和 Agent 响应映射。`documentation` 字段的值就是当前完整文档；为避免文档在自身内部无限递归，JSON 中仅用等义占位文本表示正文。

```json
{
  "apiVersion": "beeftv.plugin/v2",
  "id": "feituo-video",
  "name": "飞拓视频",
  "version": "1.0.0",
  "author": "BeefTV Contributors",
  "description": "飞拓 AI（feituokuajing.com）视频生成协议：自定义提交端点 + jobId 轮询，素材需公网 URL。",
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
        "id": "feituo-video",
        "label": "飞拓视频",
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
        "baseUrl": "https://feituokuajing.com",
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
            "description": "飞拓模型 ID，例如 seedance2.0。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt",
            "description": "视频提示词。"
          },
          {
            "name": "ratio",
            "type": "string",
            "required": false,
            "mapping": "aspect_ratio",
            "description": "画幅比例，支持 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9。"
          },
          {
            "name": "seconds",
            "type": "integer",
            "required": false,
            "mapping": "duration",
            "description": "时长，按 5 / 10 / 15 秒归并。"
          },
          {
            "name": "images",
            "type": "media[]",
            "required": false,
            "mapping": "imageUrls",
            "description": "参考图公网地址数组，按顺序对应提示词里的 @image1、@image2。"
          },
          {
            "name": "videos",
            "type": "media[]",
            "required": false,
            "mapping": "videoUrls",
            "description": "参考视频公网地址数组，仅部分模型支持。"
          },
          {
            "name": "audios",
            "type": "media[]",
            "required": false,
            "mapping": "audioUrls",
            "description": "参考音频公网地址数组，仅部分模型支持。"
          }
        ],
        "create": {
          "method": "POST",
          "path": "/api/open/v1/video/generate",
          "contentType": "application/json",
          "body": {
            "model": {
              "$ref": "request.model"
            },
            "prompt": {
              "$ref": "request.prompt"
            },
            "ratio": {
              "$coalesce": [
                {
                  "$ref": "request.aspectRatio"
                },
                "9:16"
              ]
            },
            "duration": {
              "$coalesce": [
                {
                  "$ref": "request.seconds"
                },
                10
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
            "imageUrls": {
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
            },
            "videoUrls": {
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
            },
            "audioUrls": {
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
        "poll": {
          "method": "GET",
          "path": "/api/open/v1/video/status",
          "query": {
            "jobId": {
              "$ref": "taskId"
            }
          }
        },
        "response": {
          "taskId": {
            "$coalesce": [
              {
                "$ref": "response.jobId"
              },
              {
                "$ref": "response.data.jobId"
              },
              {
                "$ref": "response.id"
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
                        "success",
                        "succeeded",
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
                "$ref": "response.errorMessage"
              },
              {
                "$ref": "response.error.message"
              },
              {
                "$ref": "response.message"
              }
            ]
          },
          "videos": {
            "$coalesce": [
              {
                "$ref": "response.videoUrl"
              },
              {
                "$ref": "response.video_url"
              },
              {
                "$ref": "response.data.videoUrl"
              },
              {
                "$ref": "response.result_url"
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
            "errorMessage"
          ],
          "resultEphemeral": true
        }
      }
    ]
  }
}
```
<!-- BEEFTV_PLUGIN_MANIFEST_END -->
