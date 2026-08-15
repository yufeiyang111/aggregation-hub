# Aggregation Hub Provider Adapter 设计

> 文档编号：ADAPTER-001  
> 状态：设计评审中

## 1. 目标

Adapter 是主要扩展点。新增上游服务时新增或配置 Adapter，不能修改入口协议、Router 或日志模块。

V1 内置：`openai-compatible`、`anthropic-compatible`、`local-openai-compatible`，以及 OAuth Adapter 基础接口和至少一个真实验证实现。

## 2. 职责边界

Adapter 负责：验证非秘密配置、声明认证和能力、构建上游 URL、转换请求、解析非流式与流式响应、解析 Usage、映射安全错误、发现模型和执行能力测试。

Adapter 不负责：选择 Provider、校验 Local Key、访问 UI、直接写数据库、记录正文、绕过 Transport 安全策略、决定价格或扣费。

## 3. 接口草案

```go
type Adapter interface {
    Type() string
    Metadata() AdapterMetadata
    ConfigSchema() json.RawMessage
    ValidateConfig(config json.RawMessage) error

    DiscoverModels(ctx context.Context, client UpstreamClient, provider ProviderRuntime, credential Credential) ([]DiscoveredModel, error)
    BuildRequest(ctx context.Context, route RoutePlan, request NormalizedRequest, credential Credential) (*http.Request, error)
    ParseResponse(ctx context.Context, route RoutePlan, response *http.Response) (NormalizedResponse, error)
    ParseStream(ctx context.Context, route RoutePlan, response *http.Response, emit StreamEmitter) error
    Test(ctx context.Context, client UpstreamClient, provider ProviderRuntime, credential Credential, kind CapabilityTestKind) CapabilityTestResult
}
```

跨边界对象必须强类型或受 JSON Schema 约束，不能返回任意 `any`。

## 4. Metadata 与运行时

Metadata 声明支持的认证类型、入口协议、受保护 Header、模型发现、Stream、Tools、Reasoning、Thinking。最终能力是 Adapter 与具体模型能力的交集。

ProviderRuntime 只包含 ID、slug、结构化 Base URL、认证类型、非秘密配置、已解析 Header 和超时策略。

## 5. 凭据

Core 根据 RoutePlan 的 credential_ref 调用 CredentialStore，生成短生命周期 Credential，并在模型发现、连通性测试、请求构造时显式传入 Adapter。Adapter 不直接依赖 Windows Credential Manager。

OAuth 使用 TokenProvider：AccessToken、Refresh、Revoke。同一账户并发刷新必须 singleflight，不能同时发多个 Refresh 请求。

Go 无法保证 Token 从进程内存绝对擦除，因此文档不得声称秘密从不进入内存。

## 6. URL 与网络

- Base URL 保存时解析和规范化；
- 使用结构化 URL Resolve，不直接拼字符串；
- 只允许 HTTP/HTTPS；
- 禁止 URL 用户名、密码和 fragment；
- 重定向后重新校验目标；
- 跨主机重定向不携带认证头；
- Public Provider 默认拒绝私有/回环目标；
- Local Provider 可显式访问回环或确认过的私有地址；
- 永久阻止云元数据目标。

## 7. 请求转换规则

- 不支持语义返回 `unsupported_feature`；
- 不静默删除 System、Tool、Reasoning、Thinking 或 Stop；
- 不把 Tool Result 随意降级成普通用户文本；
- 上游模型使用 RoutePlan.UpstreamModelID，不发送公开命名空间；
- Provider 特有字段来自受验证配置，不允许客户端任意注入上游 JSON。

Tool Call ID 建立 `internal <-> ingress <-> upstream` 请求级映射，只存在内存。System 在 NormalizedRequest 中单独保存，再由 Adapter 映射到 system、instructions 或官方等价表达。

## 8. 流式转换

StreamEmitter 提供背压，慢客户端时 Adapter 等待或因 Context 取消退出，不能无限缓存。

SSE 解析必须：

- 支持跨 TCP chunk、多行 data；
- 限制单事件和未完成缓冲大小；
- 正确处理空行、注释和终止标记；
- 上游断流且无终态时返回 `upstream_stream_truncated`；
- 解析失败日志只记录事件类型和截断摘要；
- 终态后收到数据视为协议异常。

Tool 参数可以任意边界拆分，Adapter 发送字符串增量，完成后才验证完整 JSON。

## 9. 错误

Adapter 返回 GatewayError：code、SafeMessage、HTTPStatus、Retryable、ProviderID、UpstreamCode、Cause。

- Cause 仅内部使用并脱敏；
- 不返回上游 HTML、堆栈和完整 Body；
- 401/403 更新 Provider 健康状态；
- 429 标记可重试，但 V1 不自动换渠道；
- 流式开始后通过入口 error 事件或断流结束，不能改写成普通 JSON。

## 10. 模型发现

发现结果包含 upstream ID、显示名、能力和来源。

- 新模型默认禁用；
- 已存在模型更新上游声明，不覆盖用户覆盖；
- 用户能力覆盖仅允许六个 `supports_*` 布尔字段，采用字段级合并；空覆盖代表恢复上游声明；
- Control Plane 和 WebView 只使用强类型、allowlist 的 `capability_override`，不传递数据库中的原始 JSON；
- 消失模型标记 missing_upstream；
- 同步失败不删除旧目录；
- 不支持模型列表时引导手工添加。

Adapter ConfigSchema 必须标明类型、默认值、范围、是否高级、是否秘密和是否需要重启。秘密字段只作为创建/替换输入，不写 adapter_config_json。

## 11. 内置 Adapter

### openai-compatible

显式选择上游 wire API：`responses` 或 `chat_completions`。不得通过失败请求自动猜测协议，避免额外费用和副作用。

### anthropic-compatible

配置 Messages 路径、Anthropic Version、认证头模式、Beta Header allowlist 和 Thinking 声明。

### local-openai-compatible

复用 OpenAI 协议实现，但使用 Local 网络策略并在 UI 标识本地服务。

## 12. OAuth 准入

稳定 OAuth Adapter 必须：使用官方授权、无 Cookie/账号密码、完成 PKCE/state/超时/撤销、真实验证刷新、真实验证流式 Tool Calling、记录账户限制并通过服务条款风险审查。未满足时标记 experimental，默认关闭。

## 13. 共享契约测试

每个 Adapter 必须测试：配置正负例、URL、认证且无日志泄露、非流式、SSE 分块、Tool Call/Result、Reasoning/Thinking、Usage 缺失、401/403/429/5xx、超时、断流、取消、超大错误体和模型同步不删除数据。

Fake Server 只证明转换逻辑；稳定支持还需要真实上游证据。
