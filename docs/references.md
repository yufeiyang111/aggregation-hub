# Aggregation Hub 参考资料

> 基线日期：2026-08-02  
> 实现时必须重新核对当前官方文档，本页不是永久冻结的第三方 API 规范。

## New API

- 官方仓库：https://github.com/QuantumNous/new-api
- 参考：Channel、Adapter、Relay、模型映射、协议入口、用量和 Electron 包装思路。
- 许可证：AGPL-3.0。
- 边界：只参考公开概念和行为，不复制源码到计划采用宽松许可证的实现中。

## Claude Code 与 Anthropic

- **2026-08-15 已核对（Anthropic 官方）**：Messages API：https://platform.claude.com/docs/en/api/messages
- **2026-08-15 已核对（Anthropic 官方）**：Messages Streaming：https://platform.claude.com/docs/en/build-with-claude/streaming
- LLM Gateway：https://platform.claude.com/docs/en/claude-code/llm-gateway
- Claude Code Settings：https://platform.claude.com/docs/en/claude-code/settings
- Tool Use：https://platform.claude.com/docs/en/agents-and-tools/tool-use/overview

实施时核对 Base URL/认证变量、Claude Code 必需端点、SSE 顺序、Tool 内容块、Thinking/Beta Header，以及官方 OAuth 是否允许第三方网关复用。

## Codex 与 OpenAI

- Codex Config Reference：https://developers.openai.com/codex/config-reference
- Responses API：https://developers.openai.com/api/reference/responses
- Function Calling：https://developers.openai.com/api/docs/guides/function-calling
- **2026-08-16 已核对（OpenAI 官方）**：Streaming Responses：https://developers.openai.com/api/docs/guides/streaming-responses

实施时核对 model_providers 的 base_url、认证、wire_api，Codex Responses 要求、Function Call/Output、Reasoning、ChatGPT 登录与 API Key 边界，以及官方 OAuth/凭据帮助机制。

## Desktop、安全和数据库

- Tauri Sidecar：https://v2.tauri.app/develop/sidecar/
- Tauri System Tray：https://v2.tauri.app/learn/system-tray/
- Tauri Capabilities：https://v2.tauri.app/security/capabilities/
- Microsoft Credential Management：https://learn.microsoft.com/en-us/windows/win32/secauthn/credential-management
- OWASP Secrets Management：https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html
- SQLite Foreign Keys：https://www.sqlite.org/foreignkeys.html
- SQLite WAL：https://www.sqlite.org/wal.html
- SQLite Backup：https://www.sqlite.org/backup.html

## 许可证

- GNU AGPL-3.0：https://www.gnu.org/licenses/agpl-3.0.html
- Apache-2.0：https://www.apache.org/licenses/LICENSE-2.0

本页不是法律意见。复制、链接或分发第三方代码前需按实际方式重新审查。