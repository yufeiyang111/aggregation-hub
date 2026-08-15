# Aggregation Hub 高层路线图

> 文档编号：ROADMAP-001  
> 状态：设计评审中  
> 本文件描述交付顺序，不替代逐任务实施计划。

## 原则

每阶段形成可运行纵向切片；先 API Key 后 OAuth；Fake 与真实证据分开；不为赶进度关闭安全、类型、迁移或测试门禁。

## Phase 0：工程基线

设计批准、实施计划、仓库治理、Go/Rust/React 基础构建、CI、统一命令和契约生成。退出条件：干净 Windows 可构建，但不声称代理可用。

## Phase 1：Core 与 Desktop 生命周期

Core `/health`、SQLite 初始迁移、Local Key、Tauri sidecar 启停、ready 事件、异常重试、托盘、最小 Runtime/Settings 和首次向导。退出条件：可靠控制 Core，端口和数据库异常可解释。

## Phase 2：OpenAI Compatible 纵向切片

Provider CRUD、Windows CredentialStore、模型、OpenAI Adapter、`/v1/chat/completions`、模型目录、Provider UI、Fake 契约和真实 Provider L3。退出条件：通用 OpenAI 客户端真实调用。

## Phase 3：Anthropic 与 Claude Code

Anthropic Adapter、`/v1/messages`、System、Tool、Thinking、Anthropic SSE、Claude Code 配置生成、取消传播和 Claude Code L4。退出条件：Claude Code 用固定 Base URL 和 Public Model ID 完成真实任务。

## Phase 4：Responses 与 Codex

`/v1/responses`、Responses 事件、Function Call/Output、Reasoning、Codex 配置生成和 Codex L4。退出条件：Codex 真实任务不依赖 Chat Completions 伪兼容。

## Phase 5：可观测性与产品完整度

请求元数据、Token/价格、Dashboard、请求/用量页面、健康、保留、诊断、备份恢复、设置和可访问性。退出条件：日常使用不依赖终端日志，诊断无秘密。

## Phase 6：OAuth

OAuth 基础设施、系统浏览器、PKCE/state、TokenProvider、至少一个实验 Adapter、L5 证据和限制说明。未达到准入条件的 Adapter 默认关闭。

## Phase 7：Windows V1

安装包、签名、校验和、SBOM、干净 VM、迁移恢复、兼容矩阵、安全报告渠道、贡献指南和发布说明。退出条件：满足 PRD V1 验收和发布门禁。

## V1 后候选

同模型多 Provider、手动/自动故障转移、局域网、headless、macOS/Linux、自定义 CA、便携明文凭据、社区 Adapter SDK、正文调试采样、额度告警、配置导入导出、自动配置客户端和智能路由。所有候选需独立 ADR，不是当前承诺。