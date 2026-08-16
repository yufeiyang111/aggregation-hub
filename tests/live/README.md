# 真实联调脚本

本目录的脚本用于产生 **本机未跟踪的** 真实兼容性证据。它们不会被 `pnpm check` 自动执行，也不能用来替代单元测试。

## Codex Responses smoke

`codex-smoke.ps1` 验证 Codex 能否通过临时 `CODEX_HOME` 使用 Aggregation Hub 的 OpenAI Responses Provider 配置。脚本只接受回环地址，临时配置只保留环境变量名；不会读取 `.env`、Windows Credential Manager、cc-switch 或任何现有 Codex 配置。

### 仅预检（默认）

```powershell
powershell -NoProfile -File tests/live/codex-smoke.ps1
```

预检会确认：

- 可执行的 `codex` 命令及其版本；
- `base_url` 是 `127.0.0.1`、`localhost` 或 `::1` 的 HTTP `/v1` 地址；
- Public Model ID 和环境变量名符合安全格式；
- 能在系统临时目录创建隔离的 `CODEX_HOME/config.toml`。

预检不会连接网关，不要求也不显示 Local Access Key。

### 显式真实运行

先在**当前 PowerShell 会话**中手动设置已由桌面端新建的 Local Access Key，再显式传入 `-RunLive`：

```powershell
$env:AGGREGATION_HUB_LOCAL_KEY = '在此手动粘贴一次性本地访问密钥'
powershell -NoProfile -File tests/live/codex-smoke.ps1 `
  -RunLive `
  -LocalBaseUrl 'http://127.0.0.1:18443/v1' `
  -Model 'provider-slug/upstream-model-id'
```

脚本会创建临时工作目录，要求 Codex 通过可用 Tool 读取固定测试文件，并检查：Codex 退出码、Tool/Function 事件、最终成功标记和运行输出中没有当前 Local Access Key。临时目录退出后会由脚本进行路径校验后清理；不会修改系统环境变量或用户的 Codex 配置。

> 不要把真实密钥写入命令历史、`.env`、仓库文件、Issue、截图或 CI 日志。完成后请关闭该 PowerShell 窗口，或执行 `Remove-Item Env:AGGREGATION_HUB_LOCAL_KEY` 清理当前会话变量。

## 证据边界

- 未执行 `-RunLive`：仅能声明预检和静态测试，不能声明 L3/L4。
- 执行成功的 Function 场景：可记录为受控的本地 L4 Function 证据，但仍需单独验证取消、上游关闭和无重试。
- 当前尚无可查询的请求追踪接口，因此“取消后 Core 状态为 `cancelled`、上游 Body 已关闭、无重试/重复终态”的端到端断言必须在可观测性阶段完成后再运行；不得仅凭杀掉 Codex 进程声称取消验证通过。
