# Aggregation Hub 测试策略

> 文档编号：TEST-001  
> 状态：设计评审中

## 1. 原则

兼容性必须由契约和真实运行证据证明；Fake 负责确定性，真实 Provider 和客户端负责外部兼容；安全负向场景与功能同等重要；流式、取消和 Tool Calling 是发布门禁。

## 2. 证据等级

| 等级 | 证据 | 可声明内容 |
|---|---|---|
| L1 | 单元/Fake | 转换逻辑通过 |
| L2 | Core + Fake 上游 | 代理链路通过 |
| L3 | 真实 Provider + 自写客户端 | 上游兼容 |
| L4 | Claude Code/Codex + 真实 Provider | 客户端真实兼容 |
| L5 | OAuth 登录 + 客户端 + Tool Calling | OAuth 套餐真实兼容 |

不能用 L1/L2 声称 L4/L5。

## 3. Go Core

### 单元测试

覆盖模型 ID、能力校验、错误映射、费用、URL/网络分类、Header allowlist、协议规范化、Tool ID、SSE chunk、脱敏、保留期、Key 哈希。纯业务模块目标行覆盖率 85% 以上，但覆盖率不能替代分支断言。

### Repository

使用真实临时 SQLite 文件：空库迁移、历史版本迁移、外键/唯一约束、乐观锁、软删除、价格历史、日汇总、失败不 reset、重启恢复、清理幂等。

### Adapter 契约

Fake Upstream 模拟非流式、SSE 任意分块、Tool 参数跨事件、Usage 末尾/缺失、401/403/429/500、超时、半关闭、断流、超大 Header/Body/Event、跨主机重定向和客户端取消。

## 4. Data Plane

Anthropic Messages：单轮、多轮、System、Tool Call、Tool Result、Thinking、能力拒绝、SSE 和取消。

OpenAI Responses：Input、Instructions、流式文本、Function Call/Output、Reasoning、Usage、取消和不支持字段。

Chat Completions：messages、stream、tools、tool_choice、stop、Usage，以及与 Responses 独立转换。

模型目录：鉴权、禁用 Provider、同名模型命名空间、状态变化和无敏感字段。

## 5. Control Plane

测试 DTO Schema、WebView 无令牌无法直连、Provider 创建补偿事务、PATCH 版本冲突、掩码凭据不会回写、Provider 健康记录的错误码 allowlist/七天保留/受限查询参数、OAuth 会话一次性、Local Key 仅显示一次、敏感动作审计、分页排序 allowlist 和诊断导出 allowlist。

## 6. Desktop 与前端

Rust/Tauri：Core 启停、异常重试、ready 解析、stdin 管理令牌、端口冲突、用户退出、Command 校验、禁止任意路径/Shell、托盘、开机启动和更新签名。

React：首次向导、Provider 向导、显式测试与按需健康记录、模型同步、配置复制、Key 轮换、请求筛选、OAuth 状态、端口重启确认，以及每页加载/空/错误/成功。

可访问性：键盘、焦点、Label、屏幕阅读器名称、对比度、200% 缩放、减少动画和图表表格替代。

## 7. 桌面 E2E

使用发布或接近发布构建和临时数据目录，覆盖安装、首次启动、生成 Key、添加 Provider、测试模型、复制配置、托盘、Core 崩溃恢复、应用重启恢复和卸载数据提示。

Windows 发布前必须在无 Go/Rust/Node 的干净虚拟机安装运行。

## 8. 真实验证

真实 Provider 测试只在维护者受控环境运行，凭据通过环境或系统凭据库注入，不进入公共 CI 日志。结束后扫描日志和诊断包，凭据泄露立即撤销。

V1 发布候选必须完成：Claude Code L4、Codex L4、至少一个 OAuth L5 或正式限制报告。

## 9. 安全与稳定性

安全测试：Local Key、CORS、SSRF、DNS/Redirect、Header 注入、JSON 深度、Tool Schema、SSE 无限流、OAuth CSRF/PKCE/重放、磁盘满、秘密扫描、诊断 ZIP 路径穿越和更新签名。

性能测试：代理附加延迟、100 并发流、慢客户端背压、长 SSE、日志队列拥塞、24 小时稳定、网络抖动、Core 重启和大量记录分页清理。报告说明机器、构建、上游和是否排除网络。

## 10. CI 门禁

PR：Go fmt/vet/lint/test、Rust fmt/clippy/test、TypeScript typecheck/lint/test、迁移测试、Adapter 契约、Schema 漂移、文档链接/占位符、秘密扫描、依赖与许可证。

主分支：完整集成、桌面构建、安装烟雾、SBOM 和校验和。

发布候选：L4/L5、干净 Windows、数据库升级恢复、安全人工复核。

## 11. Bug 回归

先复现并添加失败测试，再修根因；运行最小测试和受影响 broader checks；最终报告明确静态、Fake 和真实运行证据。

## Task 5.4 覆盖补充（2026-08-16）

- 请求与用量查询：分页稳定性、参数上限、UTC 时间窗、未知 Token/缓存命中率、无 N+1 读取和安全 DTO。
- 桌面页面：概览、请求记录、用量的 loading、empty、error、success；请求详情 Escape 关闭且明确提示未保存正文。
- 不测试或声称价格、费用、真实 Provider、真实 Claude Code/Codex 或 OAuth 结果；这些均不在本任务证据范围内。


## Task 5.6 覆盖补充（2026-08-16）

- 真实临时 SQLite：请求保留按 500 条批处理、终态过滤、幂等、审计、取消，以及清理后 `usage_daily` Token 汇总仍存在。
- 备份恢复：WAL checkpoint + SQLite 快照、`integrity_check`、最近五份淘汰、非法备份标识、取消、损坏 pending 快照拒绝、恢复前数据库留存，以及 Core 在监听端口前应用已计划恢复。
- 设置：端口/超时/保留期边界、默认值、版本冲突和 `restart_required`；Control Plane、Rust bridge 与 React 设置页覆盖受控 DTO、二次确认和脱敏失败提示。
- `tests/e2e/backup-restore.ps1` 是上述 L1 测试的可重复入口，不是干净虚拟机安装、真实 Provider、Claude Code 或 Codex E2E 证据。
