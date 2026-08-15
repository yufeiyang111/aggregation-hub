# Aggregation Hub 开源与发布设计

> 文档编号：OSS-001  
> 状态：设计评审中

## 1. 目标

允许个人和组织自用、修改和分发；清楚区分稳定与实验功能；保持 Adapter 可扩展但不动态执行未知代码；发布产物可验证并包含依赖清单。

## 2. 许可证建议

建议项目代码采用 Apache License 2.0：允许商业和非商业使用，包含明确专利授权，适合工具链与企业内部采用，并符合独立实现路线。创建 `LICENSE` 前由用户确认。

任何复制第三方代码前必须核对许可证；“开源”不等于可以改名后使用任意许可证。

## 3. New API 参考边界

允许学习公开 API、模块职责、Adapter 和模型映射等通用概念；禁止在未评估前复制 Go/React/Electron 源码、数据库迁移和业务实现，或把 AGPL 组件合并后宣称整体 Apache-2.0。

贡献者提交来源不明的大段代码时必须说明来源，否则拒绝合并。

## 4. 仓库治理

设计批准并初始化仓库时创建根目录 `LICENSE`、`README.md`、`AGENTS.md`、`CONTRIBUTING.md`、`CODE_OF_CONDUCT.md`、`SECURITY.md`、`CHANGELOG.md`、`.gitignore` 和 CI/发布工作流。根 `AGENTS.md` 引用 `docs/ai/AI_CONTEXT.md`。

详细设计继续位于 `docs/`。

## 5. 贡献规则

- 一个 PR 聚焦一个功能或缺陷；
- 引用需求 ID、Issue 或 ADR；
- API、数据库、安全和用户行为变化同步文档；
- 新 Adapter 通过共享契约；
- 新 OAuth Adapter 通过安全准入和真实证据；
- 不接受真实凭据、正文和个人信息夹具；
- 建议 DCO Signed-off-by，不要求复杂 CLA；
- 合并前检查依赖许可证和生成文件漂移。

## 6. 版本与稳定性

采用语义化版本。即使在 0.x，也不能随意破坏 SQLite、Provider slug 和 Public Model ID。

稳定等级：experimental 默认关闭；preview 契约完整但真实证据有限；stable 通过共享契约和声明的 L4/L5。

## 7. 发布产物

Windows 发布至少包含安装包、版本、SHA-256、SBOM、第三方许可证、迁移说明、兼容矩阵、已知限制、安全说明和对应源码标签。

安装包不得包含真实 Key、测试账号、开发 `.env`、调试日志、来源不明二进制和未处理的许可证冲突。

## 7.1 当前 Windows 预发布构建基线

当前仓库已提供 Windows x64 的 NSIS 一键安装包构建基线：`pnpm release:windows` 会运行完整 Gate、构建 Tauri Desktop 与 Core Sidecar、整理 `Setup.exe`、生成 SHA-256 和无秘密的工件清单。Tauri 配置为当前用户安装、在线 WebView2 Bootstrapper、拒绝降级安装，并创建开始菜单和桌面快捷方式。

`.github/workflows/windows-release-build.yml` 只能由 `workflow_dispatch` 手动触发；触发时必须输入与 `tauri.conf.json` 完全一致的版本号，且只上传 GitHub Actions Artifact；不创建 GitHub Release、不上传外部存储、不使用发布或签名凭据。当前产物必须标识为 unsigned，未完成干净 Windows VM 安装、升级、卸载与真实客户端兼容验证前，不得声称可正式发布。

发布工件静态扫描仅检测已知 Local Key、常见 API Key、GitHub Token 和 SQLite 数据库头标记；它不能替代 Phase 7 的 Sentinel 全链路秘密扫描。
## 8. 发布流程

冻结候选 -> 完整 CI -> 构建签名 -> 干净 VM 安装 -> 数据库升级/恢复 -> Claude Code/Codex L4 -> OAuth L5 或限制报告 -> 秘密扫描 -> 生成 SBOM/校验和 -> 发布标签与说明。

## 9. 自动更新

如果提供自动更新，清单必须签名，只从官方 HTTPS 源获取，下载后校验，失败保留旧版本，迁移前备份，拒绝不安全降级。没有可靠签名设施时，手动更新优于不安全自动更新。

## 10. 安全报告

根 `SECURITY.md` 提供私密渠道、支持版本、响应时间、轮换建议和协调披露流程。安全 Issue 不要求报告者公开 Token 或含秘密日志。