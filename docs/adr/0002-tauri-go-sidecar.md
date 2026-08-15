# ADR-0002：Tauri 控制面与 Go 网关 Sidecar

- 状态：提议，随本批设计评审
- 日期：2026-08-02

## 决策

采用两个可独立测试的进程：Tauri 2 + React + TypeScript 负责桌面控制面；Go 负责网关 Core。React 通过 Tauri Command 调用 Rust，Rust 管理 Core 生命周期并调用内部 Control Plane；客户端直接访问 Core Data Plane。

## 理由

Tauri 提供托盘和权限边界；Go 适合 HTTP/SSE、Context 取消和单文件交付；Core 不依赖 WebView，可独立做契约、负载和未来 headless 测试。

## 代价

构建链包含 Rust、Node、Go；需要管理子进程、端口、管理令牌和跨语言 DTO；安装包含两个二进制。

拒绝方案：Electron + Go 资源较重；Wails v2 的托盘与能力边界不够明确；纯 Rust Core 的 Adapter 初期成本更高。