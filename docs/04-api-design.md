# Aggregation Hub API 设计

> 文档编号：API-001  
> 状态：设计评审中

## 1. API 分类

| 类型 | 调用者 | 地址 | 鉴权 |
|---|---|---|---|
| Data Plane | Claude Code、Codex、其他 Agent | 固定回环端口 | Local Access Key |
| Control Plane | Tauri Rust | 随机回环端口 | 每次启动管理令牌 |
| OAuth Callback | 系统浏览器 | 临时回环端口 | state + PKCE + 一次性会话 |

React WebView 不直接调用 Control Plane。

## 2. 通用约定

- 请求 ID：`req_<ulid>`，响应头返回 `x-aggregation-hub-request-id`。
- Data Plane 接受 `Authorization: Bearer ah_local_...`；Anthropic 入口也可接受 `x-api-key`。
- 两个认证头同时存在且值不同，返回 `409 conflicting_credentials`。
- JSON 使用 `application/json`；SSE 使用 `text/event-stream; charset=utf-8`。
- 默认请求体上限 8 MiB；V1 不接受表单、Multipart 和压缩请求体。
- 上游错误必须截断、脱敏并转成安全错误，不能透传 HTML、堆栈或完整 Body。

内部错误格式：

```json
{
  "error": {
    "code": "provider_auth_failed",
    "message": "上游服务拒绝了当前凭据",
    "request_id": "req_01...",
    "retryable": false,
    "details": { "provider": "package-a" }
  }
}
```

Ingress 按入口协议转换错误外形。`details` 只允许白名单字段。

| HTTP | code | 含义 |
|---:|---|---|
| 400 | `invalid_request` | JSON 或字段不合法 |
| 400 | `unsupported_feature` | 模型/Adapter 不支持能力 |
| 401 | `invalid_local_key` | 本地密钥无效 |
| 404 | `model_not_found` | 公开模型不存在或禁用 |
| 409 | `conflicting_credentials` / `stale_resource` | 鉴权冲突或版本冲突 |
| 413 | `request_too_large` | 请求超限 |
| 422 | `protocol_conversion_failed` | 无法保持语义转换 |
| 429 | `upstream_rate_limited` | 上游限流 |
| 502 | `provider_auth_failed` | 上游认证失败，避免误判本地密钥 |
| 502 | `provider_unavailable` | DNS、连接或上游 5xx |
| 502 | `upstream_protocol_error` | 上游响应无法解析 |
| 504 | `upstream_timeout` | 上游超时 |

## 3. Data Plane

### GET /health

无需鉴权，只返回最小状态：

```json
{"status":"ok","version":"0.1.0-rc.2","data_plane":"ready"}
```

不得返回 Provider、路径、数据库和凭据详情。

### GET /v1/models

需要鉴权，返回所有已启用且可公开的模型：

```json
{
  "object": "list",
  "data": [
    {
      "id": "package-a/claude-sonnet-4",
      "object": "model",
      "owned_by": "package-a",
      "created": 0
    }
  ]
}
```

`auth_required`、`disabled`、`deleted` Provider 的模型不返回。

### POST /v1/messages

支持 Anthropic Messages 的编程 Agent 关键子集：model、max_tokens、messages、system、stream、tools、tool_choice、temperature、stop_sequences，以及由模型能力控制的 Thinking 字段。

流式输出必须保持消息开始、内容块开始、增量、内容块结束、消息增量和消息结束的合法顺序。具体字段以实施时当前官方文档和真实 Claude Code 夹具为准。

### POST /v1/responses

支持 OpenAI Responses 的编程 Agent 关键子集：model、input、instructions、stream、tools、tool_choice、reasoning、max_output_tokens 和 Function Call Output。

实现时必须按当前 Codex 配置参考确认 Responses wire API，不能用 Chat Completions 假装完整 Codex 兼容。

### POST /v1/chat/completions

支持 model、messages、stream、tools、tool_choice、temperature、max_tokens 和 stop。Chat Completions 与 Responses 分别进入 NormalizedRequest，不能只做字段改名直接互转。

## 4. Normalized Contract

System 与普通 Messages 分离。ContentPart 显式支持 text、image、reasoning、tool_call、tool_result。

Tool 定义：

```go
type ToolDefinition struct {
    Name        string
    Description string
    InputSchema json.RawMessage
}
```

- InputSchema 必须是 JSON Schema 对象并限制大小和深度；
- Tool Call ID 建立 ingress/internal/upstream 请求级映射；
- 找不到对应 Call ID 返回 `invalid_tool_result`；
- Tool 参数增量按字符串拼接，完成后再验证 JSON；
- 日志不得保存参数正文。

内部流式事件：

```text
response_start
content_start
text_delta
reasoning_delta
tool_call_start
tool_call_arguments_delta
content_end
usage_update
response_end
error
```

每个流一个 start，一个 end 或 error；终态后禁止继续发送。

Usage 字段使用指针表示未知：input、output、cached input、cache write、reasoning，并记录来源 `upstream_reported`、`locally_estimated` 或 `unknown`。未知不能填零冒充。

## 5. Control Plane

逻辑前缀：`/internal/v1`。React 通过 Tauri Commands 间接调用。

所有 /internal/v1 接口必须使用每次启动随机生成的 X-Aggregation-Hub-Management-Token 请求头鉴权；令牌只由 Tauri Rust Sidecar 持有，禁止进入 WebView、日志、URL、SQLite 和外部客户端配置。

### Runtime

Rust Lifecycle Manager 暴露 Tauri Commands：

```text
runtime_status
runtime_start
runtime_stop
runtime_restart
dashboard_status
create_local_key
```

`dashboard_status` 只返回 `RuntimeSnapshot` 和 Provider 的安全摘要（ID、slug、名称、Adapter 类型、Base URL、生命周期状态、启用状态和版本号）。它不会返回 Control Plane URL、管理令牌、CredentialStore 引用或上游凭据。

`create_local_key` 只接受长度为 1~128 的 `name`，由 Rust Sidecar 转发给内部 `POST /internal/v1/local-keys`。成功结果只在当前 WebView 内存中交付一次，包含 `display_once: true` 和完整 Local Key；前端不得持久化、记录或在页面刷新后恢复该值。

Core 存活时内部接口仅提供：

```text
GET  /internal/v1/runtime
POST /internal/v1/runtime/shutdown
```

shutdown 超时后 Rust 才终止子进程。

### Provider

```text
GET/POST /internal/v1/providers
GET/PATCH/DELETE /internal/v1/providers/{id}
POST /internal/v1/providers/{id}/enable
POST /internal/v1/providers/{id}/disable
POST /internal/v1/providers/{id}/test
POST /internal/v1/providers/{id}/sync-models
```

创建/替换凭据使用专用字段；响应只返回 `configured` 和 `masked_hint`。PATCH 省略凭据表示保持不变，禁止把掩码误写为新凭据。

#### Task 1.7 当前实现边界

当前 Core 已实现 Provider 的列表、创建、读取、更新、启用、禁用和删除路由。写入请求仅接受显式 JSON DTO，最大 64 KiB，拒绝未知字段；更新、启用、禁用与删除都必须携带 `version`，版本冲突返回 `409 stale_resource`。响应不返回 CredentialStore 引用或完整凭据。

当前 Core 还实现 `POST /internal/v1/local-keys`：请求体为 `name` 和可选 `expires_at`，成功时以 `201` 返回唯一一次包含完整 `key` 的响应及 `display_once: true`。完整值不写入 SQLite、ready 事件或日志。当前桌面端已经通过 `create_local_key` Tauri Command 提供一次性展示与复制；`GET /internal/v1/local-keys`、吊销和完整管理页保留给后续任务。
### Models

```text
GET /internal/v1/models
POST /internal/v1/providers/{providerId}/models
PATCH/DELETE /internal/v1/models/{id}
POST /internal/v1/models/{id}/enable
POST /internal/v1/models/{id}/disable
POST /internal/v1/models/{id}/test
```

列表支持 Provider、状态、能力、搜索和分页；page_size 默认 50、最大 200。

### OAuth、Keys、Requests

```text
POST /internal/v1/oauth/sessions
GET  /internal/v1/oauth/sessions/{id}
POST /internal/v1/oauth/accounts/{id}/refresh
POST /internal/v1/oauth/accounts/{id}/revoke
GET/POST /internal/v1/local-keys
POST /internal/v1/local-keys/{id}/revoke
GET /internal/v1/requests
GET /internal/v1/requests/{id}
GET /internal/v1/usage/summary
GET /internal/v1/usage/timeseries
```

创建 Local Key 是唯一返回完整本地密钥的响应，并带 `display_once: true`。请求详情只返回元数据。

### Settings/Diagnostics

```text
GET/PATCH /internal/v1/settings
GET /internal/v1/diagnostics
POST /internal/v1/diagnostics/export
POST /internal/v1/maintenance/prune
```

修改监听端口返回 `restart_required: true`，不得静默重启中断请求。

## 6. 幂等与并发

- Provider 创建支持 Idempotency-Key。
- PATCH 使用 version 乐观锁，冲突返回 `409 stale_resource`。
- OAuth 会话和密钥创建不由 UI 自动重试。
- 控制面写操作记录不含秘密的审计事件。
- 外部协议变化必须同步 DTO、Normalized Contract、Adapter、测试和追踪矩阵。