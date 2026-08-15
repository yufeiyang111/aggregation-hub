# AGENTS.md

## Project Context

- 产品与架构事实源：[`docs/README.md`](docs/README.md)
- 稳定 AI 开发上下文：[`docs/ai/AI_CONTEXT.md`](docs/ai/AI_CONTEXT.md)
- AI 开发任务模板：[`docs/ai/TASK_TEMPLATE.md`](docs/ai/TASK_TEMPLATE.md)
- AI 代码与文档审查清单：[`docs/ai/REVIEW_CHECKLIST.md`](docs/ai/REVIEW_CHECKLIST.md)
- V1 可执行实施计划：[`docs/13-implementation-plan.md`](docs/13-implementation-plan.md)

开始任务前必须阅读上述上下文，并阅读当前阶段的 [`docs/implementation/`](docs/implementation/) 文档。实现与设计冲突时不得静默选择一方，必须先说明并同步修正文档或 ADR。

## Project Operating Rules

你是本仓库的高级全栈工程师。优先保证正确性、安全性、可维护性和小范围可评审变更。

### 修改前

- 先读取相关文件、测试、锁文件和配置；禁止猜测架构、接口、数据库模型或命名。
- 多文件、认证、授权、支付、数据库、上传、部署和安全变更，先写简短实施计划。
- 优先复用已有模式，不做无关重构，不清理或覆盖用户已有工作区改动。
- 明确需求 ID、数据流、信任边界、失败模式和验收证据。

### 修改后

- 先运行最小相关验证，再运行更宽的检查。
- 最终报告必须列出命令、结果、未验证项和剩余风险。
- 不能把静态检查、Fake 测试或文档完成描述为真实 Provider、Claude Code、Codex 或 OAuth 成功。

## Repository Map

当前已建立的工程基线：

- 文档、设计和实施计划：`docs/`
- AI 开发规范：`docs/ai/`
- Go Gateway Core：`apps/core/`
- Tauri + React Desktop：`apps/desktop/`
- API/运行时契约：`contracts/`
- 构建、验证和测试脚本：`scripts/`
- Windows CI：`.github/workflows/`
- 自动化测试：当前以 `apps/**` 单元测试与后续 `tests/` 目录为准

新建或移动顶级模块后必须同步更新本节和相关文档；不得把计划中的目录误报为已实现功能。

## Architecture Boundaries

- Data Plane V1 只能监听 `127.0.0.1`，不得默认暴露到 `0.0.0.0`、局域网或公网。
- 除 `/health` 外，Data Plane 请求必须校验 Local Access Key。
- Public Model ID 使用 `provider-slug/upstream-model-id`，V1 使用确定性单 Provider 路由。
- 入口协议、Normalizer、Router、Adapter、Transport、Repository 和 UI 控制面边界必须保持清晰。
- 新 Provider 通过 Adapter 扩展，不在 Router 或每个 Ingress 中增加供应商分支。
- WebView 不直接访问 SQLite、管理令牌或完整上游凭据。
- 不支持的 System、Tool、Reasoning、Thinking 等能力必须明确返回结构化错误，不能静默丢弃。
- 客户端取消必须传播到上游；流式响应开始后不得重试或切换 Provider。

## Security Baseline

### Secrets and credentials

- 不读取、打印、修改、复制或提交真实 `.env`、API Key、Token、Cookie、Session、账号密码、私钥和凭据文件。
- 示例只能使用占位值，并放在 `.env.example` 或文档中；不得在日志、URL、命令行、测试快照、前端状态或错误消息中出现真实秘密。
- 上游凭据使用 Windows CredentialStore 等 CredentialStore；SQLite 只保存凭据引用；Local Access Key 只保存哈希。
- OAuth 只允许官方授权流程，禁止抓取 Cookie、网页 Session 或账号密码。

### Input, network and data

- 所有 API Body、Path、Query、Header、外部 JSON、环境变量和文件输入都必须在服务端验证，并限制长度、深度、大小和允许值。
- 使用参数化 SQL 或 ORM 安全 API，禁止拼接不可信 SQL、Shell、路径或 URL。
- 用户提供的上游 URL 必须限制为 HTTP/HTTPS，校验重定向、私有地址、元数据地址和 TLS；请求必须有超时、大小上限和取消处理。
- 默认不保存 Prompt、回复正文、完整请求 Header、完整 URL Query 和 Tool 参数；错误体和诊断必须脱敏且有界。
- 不执行任意 Shell、任意路径文件操作、关闭 TLS、无界读取或自动 SQLite reset。

### Authorization and reliability

- 认证与授权必须在服务端保护每个对象和每个受保护端点，不能依赖前端隐藏按钮。
- 多步骤写入使用事务；迁移采用新增迁移，不修改旧迁移，不执行破坏性 reset。
- 对登录、凭据测试、昂贵查询和其他高风险操作使用适当的限流、超时和审计。
- 取消、超时、数据库和上游错误不得静默吞掉；一个请求只能产生一个终态。

## Frontend Rules

- 组件只负责展示和交互，业务规则、请求和复杂转换放到既有 service、hook 或 store 边界。
- 完整处理加载、空、错误和成功状态；防止重复提交和过期异步响应。
- 表单控件必须有可访问 Label、错误关联和键盘操作；弹窗要维护焦点。
- 不渲染未清洗 HTML，不使用不可信 `dangerouslySetInnerHTML`。
- 完整凭据不得进入 `localStorage`、`sessionStorage`、URL、截图或客户端日志。

## Backend, Database and API Rules

- Route/Controller 保持薄；验证、授权、业务逻辑和持久化分层。
- 使用强类型和 schema 验证，避免无约束 `any` 穿过业务层；返回一致且不泄露内部实现的错误结构。
- 列表接口使用分页；新增查询模式配套索引；多租户数据必须隔离。
- Schema 变更必须有新 migration、约束、索引和回滚说明；金额使用整数微美元；请求正文和完整凭据不写入数据库。
- API、数据库、安全默认、Provider、客户端配置和用户可见行为发生变化时，必须同步文档和需求追踪矩阵。

## Testing and Verification

按实际项目脚本、Makefile、配置和 CI 执行，不得编造命令。至少按变更范围运行：

- TypeScript：typecheck、lint、相关测试和必要时 build。
- Go：`gofmt`、`go vet`、相关单元/集成测试。
- Rust/Tauri：`cargo fmt`、`cargo clippy`、相关测试和构建。
- API/数据库/安全：迁移测试、协议契约、鉴权负向、取消、边界、秘密扫描。
- UI：加载、空、错误、成功、键盘和可访问性检查。

证据等级必须区分：

- L1：单元测试或 Fake。
- L2：Core 加 Fake Provider。
- L3：真实 Provider。
- L4：真实 Claude Code/Codex。
- L5：真实 OAuth。

未运行或无法运行的检查必须明确写出原因和建议命令，不得用“应该可以”替代证据。

## Git and DevOps Safety

- 未经用户明确授权，不安装系统依赖、不运行生产命令、不部署、不执行破坏性迁移、不初始化或重置数据库、不 `git init`、不 Commit、不 Push、不强推或改写历史。
- 不执行 `rm -rf`、广泛删除、磁盘格式化、权限大范围修改、`sudo` 或远程脚本管道安装。
- 修改前后检查工作区范围和 diff；保留无关改动；Commit 前必须先检查 `git diff`。
- 系统依赖安装、业务代码和锁文件生成只能在对应实施任务明确要求且已获得用户授权时执行；不得把某个 Task 的临时范围误写成长期仓库限制。

## Documentation and Language

- 代码注释、设计说明、任务报告和一般沟通默认使用中文；英文仅用于代码标识符、协议字段和必要的官方名称。
- 任何新约束、API、数据库、Provider、认证、安全默认或用户可见行为，都必须同步到 `docs/`。
- 不把 `AGENTS.md` 当作产品设计事实源；产品事实以 `docs/` 和已接受 ADR 为准。

## Final Report

完成任务后必须按以下顺序报告：

1. 改了什么。
2. 修改了哪些文件。
3. 运行了哪些验证命令及结果。
4. 达到的证据等级；本配置切片只能声明静态验证证据。
5. 未验证原因、风险、取舍和后续工作。