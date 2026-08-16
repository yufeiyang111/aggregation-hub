# Aggregation Hub 产品需求文档

> 文档编号：PRD-001  
> 状态：设计评审中  
> 基线日期：2026-08-02

## 1. 背景

个人开发者经常同时拥有官方 API、第三方兼容套餐、本地模型服务，以及 Claude Code 或 Codex 等官方编程套餐。不同服务需要维护不同 Base URL、API Key、模型名和客户端配置，切换成本高，也难以统一观察可用性与用量。

Aggregation Hub 提供一个仅在本机运行的桌面中转站。编程 Agent 只配置一次本地 Base URL 和本地访问密钥，再通过带 Provider 命名空间的模型 ID 显式选择套餐。

## 2. 产品目标

- **G-001 统一入口**：为 Claude Code、Codex 和通用 Agent 提供固定本地 API 和模型目录。
- **G-002 多套餐并存**：所有已启用 Provider 同时可用，不维护全局“当前 Provider”。
- **G-003 确定性路由**：公开模型 ID 唯一映射到一个 Provider 和上游模型。
- **G-004 编程 Agent 优先**：优先保证 SSE、Tool Calling、Reasoning/Thinking、取消和错误映射。
- **G-005 Local-first**：配置、凭据引用、请求元数据和用量默认只在本机。
- **G-006 可开源维护**：新增 Provider 通过 Adapter 完成，不把供应商逻辑散落在系统中。

## 3. 非目标

V1 不实现：公共中转站、多租户、注册登录、支付充值、聊天客户端、智能模型选择、跨 Provider 自动故障转移、Cookie/Session 抓取、手机端、公网管理、图片/音频/视频/Batch API、自动修改第三方配置和云同步。

## 4. 用户角色

V1 只有“本机所有者”一个逻辑角色，可以管理 Provider、模型、凭据、网关、用量、诊断和 OAuth 账户。

“运行在当前用户身份下的任意进程”不自动等同于可信管理者。Data Plane 必须要求本地访问密钥，Control Plane 必须与渲染进程隔离。

## 5. 术语

| 术语 | 定义 |
|---|---|
| Provider | 上游连接配置，包括 Adapter、Base URL、凭据和模型 |
| Upstream Model | 上游实际接受的模型标识 |
| Public Model ID | 暴露给客户端的 `provider-slug/upstream-model-id` |
| Adapter | 负责认证、请求/响应转换和能力声明的模块 |
| Data Plane | 面向 Claude Code、Codex 等客户端的兼容 API |
| Control Plane | 面向桌面控制面的管理接口 |
| Local Access Key | 客户端访问本地 Data Plane 的高熵 Bearer Token |
| CredentialStore | 保存和读取上游秘密的抽象层 |
| Normalized Request/Event | 协议无关的内部请求与流式事件 |

## 6. 功能需求

### 6.1 桌面与生命周期

- **FR-DESK-001**：支持启动、停止和重启网关核心。
- **FR-DESK-002**：展示 `starting`、`running`、`degraded`、`stopped`、`failed` 状态。
- **FR-DESK-003**：关闭主窗口默认最小化到托盘，显式退出才终止网关。
- **FR-DESK-004**：支持可选开机启动，默认关闭。
- **FR-DESK-005**：端口冲突不得静默换端口，必须提示用户处理。
- **FR-DESK-006**：核心异常退出最多自动重启三次，之后进入 `failed`。

### 6.2 Provider 管理

- **FR-PROV-001**：创建 OpenAI Compatible Provider。
- **FR-PROV-002**：创建 Anthropic Compatible Provider。
- **FR-PROV-003**：保存名称、唯一 slug、Base URL、认证方式、自定义请求头和超时。
- **FR-PROV-004**：支持启用、禁用、编辑和删除 Provider。
- **FR-PROV-005**：删除前显示受影响模型；默认软删除并保留历史请求。
- **FR-PROV-006**：连接测试区分 DNS、TLS、超时、认证、限流和协议错误。
- **FR-PROV-007**：支持同步或手工添加模型。
- **FR-PROV-008**：自定义 Header 不得覆盖 Host、Content-Length 和 Adapter 认证头。

### 6.3 OAuth 套餐

- **FR-OAUTH-001**：只使用官方授权流程或官方凭据帮助机制。
- **FR-OAUTH-002**：不得要求 Cookie、Session、账号密码或绕过验证的 Token。
- **FR-OAUTH-003**：展示授权、过期、刷新和撤销状态。
- **FR-OAUTH-004**：刷新失败时进入 `auth_required`，不得泄露 Token。
- **FR-OAUTH-005**：V1 至少真实验证 Claude 或 Codex 中一个官方 OAuth 套餐；另一个若受限制，必须形成可复核报告。

### 6.4 模型目录与路由

- **FR-MODEL-001**：公开模型 ID 为 `provider-slug/upstream-model-id`。
- **FR-MODEL-002**：slug 只允许小写字母、数字和连字符，创建后不可直接修改。
- **FR-MODEL-003**：公开模型 ID 全局唯一。
- **FR-MODEL-004**：模型声明流式、工具、Reasoning、Thinking、视觉和上下文能力。
- **FR-MODEL-005**：请求确定性路由到绑定 Provider。
- **FR-MODEL-006**：不支持能力在调用上游前返回结构化错误，不能静默丢弃。
- **FR-MODEL-007**：`GET /v1/models` 返回已启用且可用的模型。

### 6.5 兼容 API

- **FR-API-001**：提供 `POST /v1/messages`。
- **FR-API-002**：提供 `POST /v1/responses`。
- **FR-API-003**：提供 `POST /v1/chat/completions`。
- **FR-API-004**：提供 `GET /v1/models`。
- **FR-API-005**：提供最小化 `GET /health`。
- **FR-API-006**：除 `/health` 外必须校验 Local Access Key。
- **FR-API-007**：支持非流式和 SSE。
- **FR-API-008**：支持 Tool Calling、Tool Result、多轮消息和系统指令。
- **FR-API-009**：客户端断开和取消向上游传播。
- **FR-API-010**：流式输出开始后不得切换 Provider 或重放请求。

### 6.6 本地访问密钥

- **FR-KEY-001**：首次启动生成 `ah_local_` 前缀高熵密钥。
- **FR-KEY-002**：数据库只保存哈希、前后缀、创建和最后使用时间。
- **FR-KEY-003**：完整密钥只展示一次；遗失后轮换。
- **FR-KEY-004**：支持短暂重叠窗口平滑迁移。
- **FR-KEY-005**：支持立即吊销。

### 6.7 日志与用量

- **FR-OBS-001**：记录请求 ID、时间、协议、Provider、模型、状态和错误类别。
- **FR-OBS-002**：记录总耗时、首 Token、输入/输出/缓存/Reasoning Token。
- **FR-OBS-003**：支持筛选和分页。
- **FR-OBS-004**：默认不保存 Prompt、回复、Tool 参数、Header 和上游原始错误体。
- **FR-OBS-005**：按请求与 UTC 日汇总输入、输出、缓存和 Reasoning Token；缓存命中率仅在输入与缓存 Token 均由上游报告时计算，未知值不得显示为 0%。
- **FR-OBS-006**：可导出不含秘密的诊断包。

### 6.8 客户端接入

- **FR-CONN-001**：生成 Claude Code 配置说明。
- **FR-CONN-002**：生成 Codex 自定义 Provider 配置说明。
- **FR-CONN-003**：V1 不自动写第三方配置文件。
- **FR-CONN-004**：区分本地健康测试和完整上游测试。

## 7. 非功能需求

### 安全

- **NFR-SEC-001**：Data Plane 固定监听 `127.0.0.1`。
- **NFR-SEC-002**：WebView 不直接访问 Control Plane。
- **NFR-SEC-003**：日志、错误和导出统一脱敏。
- **NFR-SEC-004**：Base URL 仅 HTTP/HTTPS，默认校验 TLS，V1 不提供关闭选项。
- **NFR-SEC-005**：凭据不得进入命令行、URL、进程标题和崩溃报告。

### 可靠性

- **NFR-REL-001**：客户端取消后目标在 500 毫秒内取消上游 Context。
- **NFR-REL-002**：SQLite 写入使用事务并启用外键。
- **NFR-REL-003**：迁移失败保持旧数据库可恢复，不自动重置。
- **NFR-REL-004**：请求只能有一个终态。

### 性能

- **NFR-PERF-001**：无网络因素时，本地代理新增首 Token 延迟目标小于 30 ms。
- **NFR-PERF-002**：SSE 边读边写，不缓冲完整响应。
- **NFR-PERF-003**：日志通过有界队列，不阻塞流式主路径。
- **NFR-PERF-004**：发布构建目标：桌面空闲内存不超过 180 MB，Core 不超过 80 MB。

### 可维护性与 UX

- **NFR-MAIN-001**：新增 Provider 不修改核心 Router 的供应商分支。
- **NFR-MAIN-002**：Ingress、Normalizer、Router、Adapter 分层。
- **NFR-MAIN-003**：数据库迁移前向追加，禁止自动 reset。
- **NFR-MAIN-004**：公开能力关联测试和追踪矩阵。
- **NFR-UX-001**：页面包含加载、空、错误和成功状态。
- **NFR-UX-002**：关键操作支持键盘和可访问标签。
- **NFR-UX-003**：删除、轮换、撤销和清理二次确认。
- **NFR-UX-004**：错误给出下一步操作。

## 8. V1 验收场景

- **AC-001 Claude Code**：真实完成 `/v1/messages` 流式文本和 Tool Calling。
- **AC-002 Codex**：真实完成 `/v1/responses` 流式任务和 Tool Calling；取消可传播。
- **AC-003 同名模型隔离**：两个 Provider 的同名模型准确路由。
- **AC-004 安全日志**：日志、SQLite 和诊断包无完整秘密和请求正文。
- **AC-005 重启恢复**：配置、模型和用量可恢复；运行中请求标记 `aborted_by_restart`。
- **AC-006 OAuth**：至少一个官方 OAuth Adapter 达到真实 L5 证据。

## 9. 成功指标

- 日常使用不再修改客户端 Base URL；
- 公开模型路由错误为零；
- 流式和 Tool Calling 契约全部通过；
- 秘密扫描零泄露；
- 非正常退出后数据库可恢复；
- 新增标准 Adapter 不修改核心 Router。