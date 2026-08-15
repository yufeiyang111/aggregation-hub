# Aggregation Hub 设计文档

> 状态：设计已批准；Phase 0 自动化门禁已在本机通过，Phase 1.1 存储基础、Phase 1.2 Local Key 鉴权、Phase 1.3 CredentialStore、Phase 1.4 Provider/模型基础、Phase 1.5 确定性路由与 Phase 1.6 安全 Transport 已完成（L1）  
> 基线日期：2026-08-02  
> 产品形态：Windows 优先、可跨平台扩展的本地桌面 LLM 聚合网关

## 1. 文档目的

本目录是 Aggregation Hub 的产品、架构和工程事实来源。项目进入编码前必须完成设计评审；实现中若需求、接口、数据模型或安全边界变化，必须同步更新对应文档或 ADR。

Aggregation Hub 将用户自己的多个 API 套餐、兼容渠道、本地模型服务和受支持的官方 OAuth 套餐接入一个本地网关，向 Claude Code、Codex 及其他编程 Agent 暴露统一 Base URL、本地访问密钥和模型目录。

## 2. 文档导航

| 文档 | 内容 | 主要读者 |
|---|---|---|
| [00-overall-design.md](./00-overall-design.md) | 总体设计、关键决策和专项入口 | 全体参与者 |
| [01-product-requirements.md](./01-product-requirements.md) | 产品目标、范围、需求与验收 | 产品、开发、测试 |
| [02-functional-design.md](./02-functional-design.md) | 功能模块、流程和状态规则 | 产品、前后端开发 |
| [03-system-architecture.md](./03-system-architecture.md) | 进程、模块、数据流和技术架构 | 架构、后端、桌面端 |
| [04-api-design.md](./04-api-design.md) | 数据面与控制面 API 契约 | 后端、前端、测试 |
| [05-database-design.md](./05-database-design.md) | SQLite 表、索引、迁移和数据生命周期 | 后端、测试 |
| [06-provider-adapter-design.md](./06-provider-adapter-design.md) | Provider Adapter 扩展协议 | 后端、Adapter 开发者 |
| [07-frontend-design.md](./07-frontend-design.md) | 页面、导航、交互和可访问性 | 前端、设计、测试 |
| [08-security-design.md](./08-security-design.md) | 威胁模型、凭据、网络和日志安全 | 全体开发者 |
| [09-testing-strategy.md](./09-testing-strategy.md) | 测试分层、兼容矩阵和发布门禁 | 开发、测试、维护者 |
| [10-requirements-traceability.md](./10-requirements-traceability.md) | 需求到模块、接口、表和测试的追踪 | 开发、评审者 |
| [11-roadmap.md](./11-roadmap.md) | 高层分期和阶段退出条件 | 产品、维护者 |
| [12-open-source-and-release.md](./12-open-source-and-release.md) | 开源、许可证、发布和供应链规则 | 维护者、贡献者 |
| [13-implementation-plan.md](./13-implementation-plan.md) | V1 总实施计划、阶段依赖、版本和验收映射 | 全体开发者、AI Agent |
| [14-windows-dev-environment.md](./14-windows-dev-environment.md) | Windows 本地 D 盘工具链、环境变量与缓存约定 | Windows 开发者、维护者 |
| [implementation/](./implementation/) | Phase 0~7 逐任务、逐文件、逐测试计划 | 实施者、评审者 |
| [references.md](./references.md) | 官方参考与版本核对规则 | 全体开发者 |
| [adr/](./adr/) | 架构决策记录 | 全体开发者 |
| [ai/AI_CONTEXT.md](./ai/AI_CONTEXT.md) | AI 开发稳定上下文和不可破坏约束 | AI Agent、维护者 |
| [ai/TASK_TEMPLATE.md](./ai/TASK_TEMPLATE.md) | AI 开发任务模板 | AI Agent、任务发起人 |
| [ai/REVIEW_CHECKLIST.md](./ai/REVIEW_CHECKLIST.md) | AI 代码与文档审查清单 | AI Agent、评审者 |

## 3. 推荐阅读顺序

1. 本文档。
2. `00-overall-design.md`。
3. `01-product-requirements.md`。
4. `03-system-architecture.md`。
5. `08-security-design.md`。
6. `ai/AI_CONTEXT.md`。
7. `13-implementation-plan.md`。
8. 当前 Phase 的 `implementation/*.md`。
9. 当前任务的专项设计文档。
10. 相关 ADR。
## 4. 事实来源优先级

发生冲突时按以下顺序处理：

1. 用户最新明确批准的需求。
2. 已接受 ADR。
3. 产品需求与安全设计。
4. API、数据库、Adapter 和前端专项设计。
5. 测试策略与追踪矩阵。
6. 当前实现和自动化测试。

文档和实现冲突时不得静默选择其一，必须确认是实现偏离设计还是设计已经过期，并在同一变更中修正。

## 5. 当前设计摘要

- 独立实现 Local-first 核心，不复制 New API 的 AGPL 源码。
- 桌面控制面采用 Tauri 2 + React + TypeScript。
- 网关核心采用独立 Go 进程，为未来 headless 模式保留边界。
- 本地配置和统计使用 SQLite。
- 数据面仅监听 `127.0.0.1`，V1 不提供公网和局域网监听。
- 提供 Anthropic Messages、OpenAI Responses 和 OpenAI Chat Completions 兼容入口。
- 模型使用 `provider-slug/upstream-model-id`，V1 确定性路由。
- Windows 默认使用系统凭据库，不要求主密码。
- 默认不保存 Prompt、回复正文、Authorization Header 和 Tool 参数。
- OAuth 只允许官方授权流程，不导入 Cookie、网页 Session 或账号密码。

## 6. 当前执行边界

设计已批准，逐任务实施计划已经建立。编码必须从 [Phase 0](./implementation/00-foundation.md) 开始，并遵守以下边界：

- 当前已建立 Phase 0 工程骨架，但尚未实现 Provider、凭据、路由和客户端兼容等业务能力，不能把工程脚手架或文档完成等同于产品已实现；
- 依赖版本以总实施计划为初始基线，安装后必须提交对应锁文件；
- Claude Code、Codex、真实 Provider 和 OAuth 兼容只能按 L3/L4/L5 真实证据声明；
- 每个 Task 先写失败测试，再实现并执行计划中的验证命令；
- 未获用户明确授权时不安装系统工具、不 Commit、不 Push。
