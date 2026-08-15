# Windows D 盘开发环境

> 状态：本机开发环境约定  
> 更新日期：2026-08-03  
> 适用范围：本仓库在 Windows 上的本地开发；不作为开源贡献者、CI 或发行版用户的强制前置条件。

## 1. 目标与边界

为避免 C 盘被 SDK、依赖缓存和构建中间产物占满，本机开发环境统一使用 `D:\own-tool\Aggregation Hub\.toolchains\`。该目录已被 `.gitignore` 忽略，绝不包含源码、真实 Provider 凭据、Local Access Key 或数据库数据。

本约定将 **工具安装、下载归档、依赖缓存与大型构建缓存** 放到 D 盘；Windows、Visual Studio Installer 和少数系统组件仍可能在 C 盘写入少量注册表、日志或系统元数据，这不是项目可安全重定向的范围。

本机已存在的 Windows SDK 位于系统目录（C:\\Program Files (x86)\\Windows Kits\\10），Build Tools 的 VsDevCmd.bat 会复用其 c.exe 与 midl.exe；此次项目安装没有另行下载或复制一套 SDK 到 C 盘。

不持久化修改用户级 `TEMP`/`TMP`，避免影响其他 Windows 应用；激活本项目工具链时，当前 PowerShell 进程的临时目录才会临时切换到 D 盘。

## 2. 目录和持久化环境变量

| 分类 | D 盘目录 | 用户级环境变量 |
|---|---|---|
| Go SDK 与模块/构建缓存 | `.toolchains\go`、`go-work`、`go-pkg-mod`、`go-build-cache`、`go-bin` | `GOROOT`、`GOPATH`、`GOMODCACHE`、`GOCACHE`、`GOBIN` |
| Rust SDK、Registry 和构建产物 | `rustup`、`cargo`、`cargo-target` | `RUSTUP_HOME`、`CARGO_HOME`、`CARGO_TARGET_DIR` |
| pnpm/Corepack/npm/Yarn 缓存 | `corepack`、`pnpm-home`、`pnpm-store`、`pnpm-cache`、`npm-cache`、`yarn-cache`、`yarn-global` | `COREPACK_HOME`、`PNPM_HOME`、`pnpm_config_store_dir`、`pnpm_config_cache_dir`、`npm_config_cache`、`YARN_CACHE_FOLDER`、`YARN_GLOBAL_FOLDER` |
| Node 编译缓存 | `node-compile-cache` | `NODE_COMPILE_CACHE` |
| 当前项目临时文件 | `temp` | 仅由当前终端的 `TEMP`、`TMP` 使用 |
| Visual Studio Build Tools | `vs-installer`、`vs-cache`、`vs-shared`、`vs-buildtools` | 安装器参数固定到这些目录 |
| 下载归档和安装日志 | `downloads`、`logs` | 不配置为系统全局目录 |

用户级 `Path` 会追加 Go、Cargo、pnpm 以及已检测到的 D 盘 Node.js 目录。因此新打开的终端可以重用已安装工具，无需重复下载。

## 3. 使用方式

首次或修改配置后，运行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\configure-d-toolchain.ps1
```

关闭并重新打开 PowerShell、Windows Terminal 或 Codex 终端，让用户级 `Path` 与环境变量完整生效。已经打开的终端可执行：

```powershell
. .\scripts\enter-d-toolchain.ps1
```

该命令只影响当前 PowerShell；它还会设置当前进程的 `TEMP`、`TMP` 与 `NODE_COMPILE_CACHE` 到 D 盘。之后可执行常用验证，例如：

```powershell
pnpm store path
npm config get cache
go env GOMODCACHE GOCACHE GOBIN
cargo --version
```

## 4. 安装和可重复使用

安装脚本会使用官方 HTTPS 下载源，并尽量复用 D 盘已完成的下载文件。Go ZIP 会校验官方 SHA-256；脚本不会自动删除历史下载、缓存或源码。

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\install-d-toolchain.ps1
```

该脚本会配置 D 盘环境、安装/复用 Go 与 Rust，并将 Visual Studio Build Tools 的安装、共享组件和缓存指向 D 盘。脚本会检测 D 盘已有的 `cl.exe`、`VsDevCmd.bat` 和 Rust 工具，存在且可用时直接复用，不重新下载安装。

pnpm 由 Node.js 自带的 Corepack 管理，shim 安装在 `.toolchains\pnpm-home`，Corepack 下载缓存位于 `.toolchains\corepack`。项目锁文件仍是可复现依赖的唯一事实来源，安装时应优先使用：

```powershell
pnpm install --frozen-lockfile
```

## 5. Go race 检查

本机已验证的 Windows race 入口为：

```powershell
pnpm core:test:race
```

该脚本显式使用 `-ldflags=-linkmode=external`。2026-08-14 在本机 Go 1.26.5 上，默认内部链接模式无法链接 race runtime 的 Windows 导入符号；外部链接模式可保持 race 检测启用而不是跳过该门禁。运行此脚本仍需要 `go env CC`/`CXX` 可解析到兼容的 C/C++ 工具链；普通 `pnpm core:test` 不受此额外要求影响。
## 5. 清理和安全

- 不运行自动清理脚本，不删除 `downloads`、缓存或构建目录；需要释放空间时先确认目标目录和可重建性。
- `.toolchains` 仅含开发工具、依赖缓存和构建产物，仍不得放入真实 API Key、Token、Cookie、`.env` 或业务数据库。
- 不将本机绝对路径写入可移植的 `.npmrc`、应用运行时配置或发行包；开源贡献者可使用自己的工具链路径。
- CI 不依赖此目录，仍按锁文件和 `rust-toolchain.toml` 独立安装与验证。