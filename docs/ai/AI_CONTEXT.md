# Aggregation Hub AI 开发上下文

> AI Agent 修改项目之前必须阅读。详细契约以 `docs/` 和已接受 ADR 为准。

## 1. 项目使命

构建 Windows 优先、可跨平台的本地桌面 LLM 聚合网关。用户接入自己的 API 套餐和受支持 OAuth 套餐，Claude Code、Codex 等只配置一个本地 Base URL 和 Local Access Key，通过命名空间模型 ID 显式选择上游。

## 2. 不可破坏约束

1. V1 本地单用户，不是公共中转站。
2. Data Plane 固定 `127.0.0.1`。
3. 除 `/health` 外必须鉴权。
4. Public Model ID 为 `provider-slug/upstream-model-id`。
5. V1 确定性路由，不自动跨 Provider 切换。
6. 不支持能力明确报错，不静默删除字段。
7. 默认不保存 Prompt、回复和 Tool 参数。
8. OAuth 不读取 Cookie、Session 或账号密码。
9. WebView 不接触管理令牌、数据库和完整凭据。
10. 新 Provider 通过 Adapter，不在 Router 增加供应商分支。
11. 客户端取消必须传播到上游。
12. Fake 测试不能描述为客户端或 OAuth 真实成功。

## 3. 架构

```text
Tauri 2 + React/TypeScript
          │ Tauri Command
Tauri Rust Lifecycle/Control
          │ 内部 Control Plane
Go aggregation-hub-core
  ├── Data Plane / Ingress / Normalizer
  ├── Model Registry / Router / Adapter
  ├── Transport / CredentialStore
  ├── SQLite Repository
  └── Observability
```

阅读 `03-system-architecture.md`、`04-api-design.md`、`05-database-design.md`、`06-provider-adapter-design.md`。

## 4. 开始任务前

1. 阅读 `docs/README.md`、本文件、关联需求和 ADR。
2. 检查当前代码、测试、锁文件、CI 和未提交改动。
3. 确认需求 ID、数据流、信任边界和失败模式。
4. 多文件、数据库、认证、OAuth 和安全变更先写简短计划。
5. Bug 尽可能先复现。
6. 不读取、打印或修改真实 `.env` 和凭据文件。

## 5. 实现规则

- 做最小可评审纵向切片；
- 保持 Ingress、Normalizer、Router、Adapter、Transport、Repository 边界；
- 使用强类型，避免 `any` 和无约束 JSON；
- API、数据库和外部输入全部验证；
- SQL 参数化；
- 注释使用中文，解释原因和边界；
- 新依赖需必要、维护良好并检查许可证；
- 不修改无关文件，不清理用户工作区；
- 不通过关闭 lint、TLS、鉴权或测试让功能通过。

## 6. 修改位置约束

| 需求 | 应修改 | 不应修改 |
|---|---|---|
| 入口字段 | Ingress + Normalized Contract + 测试 | Provider Router |
| 新 Provider | Adapter + Schema + 契约测试 | 全部 Ingress |
| 新路由 | Router + ADR + 测试 | Adapter |
| DB 字段 | migration + Repository + 文档 | UI 直接 SQL |
| 页面 | feature/page + control DTO | SQLite |
| 凭据 | CredentialStore + Service + 安全测试 | 日志、前端 Store |
| OAuth | OAuth Service/Adapter | Cookie 抓取、WebView Token |

## 7. 错误和秘密

- 返回安全结构化错误，内部 cause 经过脱敏；
- 不透传上游完整错误体；
- 不吞取消、超时和 DB 错误；
- 流式开始后不能改成普通 JSON 错误；
- 一个请求只有一个终态；
- Usage 未知保持未知；
- 禁止 Token 进入日志、URL、命令行、测试快照和提交；
- 禁止任意 Shell/路径 Tauri Command、关闭 TLS、无界读取和自动 reset SQLite。

## 8. 验证

完成前按实际项目命令运行：Go format/vet/test、Rust fmt/clippy/test、TypeScript typecheck/lint/test、迁移测试、协议契约、安全负向和秘密扫描。UI 检查加载、空、错误、成功和键盘状态。

证据等级：L1 单元/Fake、L2 Core+Fake、L3 真实 Provider、L4 真实 Claude Code/Codex、L5 真实 OAuth。最终报告必须说明达到哪一级。

## 9. 文档与完成定义

API、数据库、安全默认、Adapter、客户端配置、支持矩阵和用户行为变化必须同步文档与追踪矩阵。如果实现发现设计不可行，先提出 ADR 或设计修订，不能偷偷换架构。

任务完成要求：需求实现、正负测试、质量门禁、安全未破坏、文档同步、真实证据或明确未验证、无关文件未改动。