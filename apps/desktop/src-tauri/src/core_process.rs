#[cfg(target_os = "windows")]
use std::ffi::c_void;
use std::sync::{Arc, Mutex, MutexGuard};

use serde::{Deserialize, Serialize};

use crate::control_client;
use tauri::{AppHandle, Manager};
use tauri_plugin_shell::process::{CommandChild, CommandEvent};
use tauri_plugin_shell::ShellExt;

const SIDECAR_NAME: &str = "aggregation-hub-core";
const BOOTSTRAP_ARGUMENT: &str = "--bootstrap-stdin";
const MANAGEMENT_TOKEN_BYTES: usize = 32;
const MAX_READY_LINE_BYTES: usize = 8 * 1024;
const CORE_VERSION: &str = "0.1.0-rc.5";

#[cfg(target_os = "windows")]
const BCRYPT_USE_SYSTEM_PREFERRED_RNG: u32 = 0x0000_0002;

#[cfg(target_os = "windows")]
#[link(name = "bcrypt")]
extern "system" {
    fn BCryptGenRandom(algorithm: *mut c_void, buffer: *mut u8, buffer_len: u32, flags: u32)
        -> i32;
}

#[derive(Clone, Debug, Default, Serialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum RuntimeState {
    #[default]
    Stopped,
    Starting,
    Running,
    Failed,
}

#[derive(Clone, Debug, Serialize, PartialEq, Eq)]
pub struct RuntimeSnapshot {
    pub state: RuntimeState,
    pub data_plane_url: Option<String>,
    pub started_at: Option<String>,
    pub version: String,
    pub last_error: Option<String>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct ProviderSummary {
    pub id: String,
    pub slug: String,
    pub name: String,
    pub adapter_type: String,
    pub base_url: String,
    pub lifecycle_status: String,
    pub enabled: bool,
    pub version: i64,
}

#[derive(Clone, Debug, Serialize)]
pub struct DashboardSnapshot {
    pub runtime: RuntimeSnapshot,
    pub providers: Vec<ProviderSummary>,
}

#[derive(Clone, Debug, Serialize)]
pub struct OneTimeLocalKey {
    pub id: String,
    pub name: String,
    pub prefix: String,
    pub suffix: String,
    pub key: String,
    pub display_once: bool,
}

#[derive(Deserialize)]
struct ProviderListResponse {
    data: Vec<ProviderSummary>,
}

#[derive(Serialize)]
struct CreateLocalKeyRequest<'a> {
    name: &'a str,
}

#[derive(Deserialize)]
struct CreateLocalKeyResponse {
    id: String,
    name: String,
    prefix: String,
    suffix: String,
    key: String,
    display_once: bool,
}

#[derive(Clone, Debug, Deserialize, PartialEq, Eq)]
#[serde(deny_unknown_fields)]
pub(crate) struct ReadyEvent {
    pub(crate) event: String,
    pub(crate) control_url: String,
    pub(crate) data_plane_url: String,
    pub(crate) pid: u32,
}

#[derive(Default)]
pub(crate) struct RuntimeLifecycle {
    state: RuntimeState,
    ready: Option<ReadyEvent>,
    last_error: Option<String>,
}

impl RuntimeLifecycle {
    pub(crate) fn begin_start(&mut self) -> Result<(), String> {
        if matches!(self.state, RuntimeState::Starting | RuntimeState::Running) {
            return Err("Core 已启动或正在启动".to_owned());
        }
        self.state = RuntimeState::Starting;
        self.ready = None;
        self.last_error = None;
        Ok(())
    }

    pub(crate) fn mark_running(&mut self, ready: ReadyEvent) -> Result<(), String> {
        if self.state != RuntimeState::Starting || !ready.is_valid() {
            return Err("Core ready 事件无效".to_owned());
        }
        self.state = RuntimeState::Running;
        self.ready = Some(ready);
        self.last_error = None;
        Ok(())
    }

    fn mark_failed(&mut self, message: &'static str) {
        self.state = RuntimeState::Failed;
        self.ready = None;
        self.last_error = Some(message.to_owned());
    }

    pub(crate) fn mark_stopped(&mut self) {
        self.state = RuntimeState::Stopped;
        self.ready = None;
        self.last_error = None;
    }

    pub(crate) fn snapshot(&self) -> RuntimeSnapshot {
        RuntimeSnapshot {
            state: self.state.clone(),
            data_plane_url: self
                .ready
                .as_ref()
                .map(|ready| ready.data_plane_url.clone()),
            // Phase 0 ready 协议尚未携带 started_at；Phase 1 Control Plane 会补齐。
            started_at: None,
            version: CORE_VERSION.to_owned(),
            last_error: self.last_error.clone(),
        }
    }
}

impl ReadyEvent {
    fn is_valid(&self) -> bool {
        self.event == "ready"
            && self.pid > 0
            && is_loopback_http_url(&self.control_url)
            && is_loopback_http_url(&self.data_plane_url)
    }
}

fn is_loopback_http_url(value: &str) -> bool {
    let Some(port) = value.strip_prefix("http://127.0.0.1:") else {
        return false;
    };
    if port.is_empty() || !port.bytes().all(|byte| byte.is_ascii_digit()) {
        return false;
    }
    matches!(port.parse::<u16>(), Ok(port) if port > 0)
}

struct SecretToken(Vec<u8>);

impl SecretToken {
    fn as_str(&self) -> &str {
        // Token 由固定十六进制编码生成，因此始终是合法 UTF-8。
        std::str::from_utf8(&self.0).expect("内部十六进制令牌必须是 UTF-8")
    }
}

impl Drop for SecretToken {
    fn drop(&mut self) {
        self.0.fill(0);
    }
}

#[derive(Default)]
struct CoreProcessInner {
    lifecycle: RuntimeLifecycle,
    child: Option<CommandChild>,
    management_token: Option<SecretToken>,
    generation: u64,
}

#[derive(Clone, Default)]
pub struct CoreProcessManager {
    inner: Arc<Mutex<CoreProcessInner>>,
}

impl CoreProcessManager {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn status(&self) -> Result<RuntimeSnapshot, String> {
        Ok(self.lock()?.lifecycle.snapshot())
    }

    pub fn dashboard(&self) -> Result<DashboardSnapshot, String> {
        let inner = self.lock()?;
        let runtime = inner.lifecycle.snapshot();
        if runtime.state != RuntimeState::Running {
            return Ok(DashboardSnapshot {
                runtime,
                providers: Vec::new(),
            });
        }
        let ready = inner
            .lifecycle
            .ready
            .as_ref()
            .ok_or_else(|| "Core 运行状态无效".to_owned())?;
        let token = inner
            .management_token
            .as_ref()
            .ok_or_else(|| "Core 管理连接不可用".to_owned())?;
        let page: ProviderListResponse =
            control_client::get_json(&ready.control_url, token.as_str(), "/internal/v1/providers")?;
        Ok(DashboardSnapshot {
            runtime,
            providers: page.data,
        })
    }

    pub fn create_local_key(&self, name: String) -> Result<OneTimeLocalKey, String> {
        let name = name.trim();
        if name.is_empty() || name.chars().count() > 128 {
            return Err("Local Key 名称无效".to_owned());
        }
        let inner = self.lock()?;
        let ready = inner
            .lifecycle
            .ready
            .as_ref()
            .ok_or_else(|| "Core 尚未运行".to_owned())?;
        let token = inner
            .management_token
            .as_ref()
            .ok_or_else(|| "Core 管理连接不可用".to_owned())?;
        let response: CreateLocalKeyResponse = control_client::post_json(
            &ready.control_url,
            token.as_str(),
            "/internal/v1/local-keys",
            &CreateLocalKeyRequest { name },
        )?;
        if !response.display_once || !response.key.starts_with("ah_local_") {
            return Err("Core Local Key 响应无效".to_owned());
        }
        Ok(OneTimeLocalKey {
            id: response.id,
            name: response.name,
            prefix: response.prefix,
            suffix: response.suffix,
            key: response.key,
            display_once: response.display_once,
        })
    }

    pub fn start(&self, app: &AppHandle) -> Result<RuntimeSnapshot, String> {
        let generation = {
            let mut inner = self.lock()?;
            if inner.child.is_some() {
                return Err("Core 进程仍由生命周期管理器持有".to_owned());
            }
            inner.lifecycle.begin_start()?;
            inner.generation = inner.generation.wrapping_add(1);
            inner.generation
        };

        let token = match generate_management_token() {
            Ok(token) => token,
            Err(()) => {
                self.fail_generation(generation, "管理令牌生成失败");
                return Err("管理令牌生成失败".to_owned());
            }
        };

        let data_dir = match app.path().local_data_dir() {
            Ok(path) => path.join("AggregationHub").to_string_lossy().into_owned(),
            Err(_) => {
                self.fail_generation(generation, "运行数据目录不可用");
                return Err("运行数据目录不可用".to_owned());
            }
        };
        let command = match app.shell().sidecar(SIDECAR_NAME) {
            Ok(command) => command.args([BOOTSTRAP_ARGUMENT]),
            Err(_) => {
                self.fail_generation(generation, "Core Sidecar 配置无效");
                return Err("Core Sidecar 配置无效".to_owned());
            }
        };

        let (mut receiver, mut child) = match command.spawn() {
            Ok(process) => process,
            Err(_) => {
                self.fail_generation(generation, "Core Sidecar 启动失败");
                return Err("Core Sidecar 启动失败".to_owned());
            }
        };

        let mut bootstrap_line = match serialize_bootstrap_line(&token, &data_dir) {
            Ok(line) => line,
            Err(()) => {
                let _ = child.kill();
                self.fail_generation(generation, "Core bootstrap 编码失败");
                return Err("Core bootstrap 编码失败".to_owned());
            }
        };
        let write_result = child.write(&bootstrap_line);
        bootstrap_line.fill(0);
        if write_result.is_err() {
            let _ = child.kill();
            self.fail_generation(generation, "Core bootstrap 写入失败");
            return Err("Core bootstrap 写入失败".to_owned());
        }

        {
            let mut inner = self.lock()?;
            if inner.generation != generation || inner.lifecycle.state != RuntimeState::Starting {
                let _ = child.kill();
                return Err("Core 启动已取消".to_owned());
            }
            inner.child = Some(child);
            inner.management_token = Some(token);
        }

        let manager = self.clone();
        tauri::async_runtime::spawn(async move {
            let mut ready_line = Vec::new();
            let mut ready_received = false;
            let mut terminated = false;

            while let Some(event) = receiver.recv().await {
                match event {
                    CommandEvent::Stdout(bytes) if !ready_received => {
                        ready_line.extend_from_slice(&bytes);
                        if ready_line.len() > MAX_READY_LINE_BYTES {
                            manager.fail_generation(generation, "Core ready 事件过大");
                            return;
                        }

                        if let Some(newline) = ready_line.iter().position(|byte| *byte == b'\n') {
                            if newline + 1 != ready_line.len() {
                                manager.fail_generation(generation, "Core stdout 协议无效");
                                return;
                            }

                            let mut line = &ready_line[..newline];
                            if line.ends_with(b"\r") {
                                line = &line[..line.len() - 1];
                            }
                            let ready = match serde_json::from_slice::<ReadyEvent>(line) {
                                Ok(ready) => ready,
                                Err(_) => {
                                    manager.fail_generation(generation, "Core ready 事件无效");
                                    return;
                                }
                            };
                            if manager.mark_running(generation, ready).is_err() {
                                manager.fail_generation(generation, "Core ready 事件无效");
                                return;
                            }
                            ready_received = true;
                            ready_line.fill(0);
                            ready_line.clear();
                        }
                    }
                    CommandEvent::Stdout(_) if ready_received => {
                        manager.fail_generation(generation, "Core stdout 协议无效");
                        return;
                    }
                    CommandEvent::Stderr(_) => {
                        // 不把 Sidecar stderr 原文复制到前端状态或桌面日志，避免秘密扩散。
                    }
                    CommandEvent::Error(_) => {
                        manager.fail_generation(generation, "Core 进程通信失败");
                        return;
                    }
                    CommandEvent::Terminated(_) => {
                        manager.mark_terminated(generation);
                        terminated = true;
                        break;
                    }
                    _ => {}
                }
            }

            if !terminated {
                manager.fail_generation(generation, "Core 进程事件流意外结束");
            }
        });

        self.status()
    }

    pub fn stop(&self) -> Result<RuntimeSnapshot, String> {
        let mut inner = self.lock()?;
        inner.generation = inner.generation.wrapping_add(1);

        // CommandChild::kill 会消费 child；先从状态中取出，避免保留失效进程句柄。
        if let Some(child) = inner.child.take() {
            if child.kill().is_err() {
                inner.management_token = None;
                inner.lifecycle.mark_failed("Core 进程停止失败");
                return Err("Core 进程停止失败".to_owned());
            }
        }

        inner.management_token = None;
        inner.lifecycle.mark_stopped();
        Ok(inner.lifecycle.snapshot())
    }

    pub fn restart(&self, app: &AppHandle) -> Result<RuntimeSnapshot, String> {
        self.stop()?;
        self.start(app)
    }

    fn mark_running(&self, generation: u64, ready: ReadyEvent) -> Result<(), String> {
        let mut inner = self.lock()?;
        if inner.generation != generation || inner.child.is_none() {
            return Err("Core 启动已取消".to_owned());
        }
        inner.lifecycle.mark_running(ready)
    }

    fn mark_terminated(&self, generation: u64) {
        let Ok(mut inner) = self.lock() else {
            return;
        };
        if inner.generation != generation {
            return;
        }
        inner.child = None;
        inner.management_token = None;
        inner.lifecycle.mark_failed("Core 进程意外退出");
    }

    fn fail_generation(&self, generation: u64, message: &'static str) {
        let Ok(mut inner) = self.lock() else {
            return;
        };
        if inner.generation != generation {
            return;
        }
        // 失败分支同样消费 child，确保生命周期状态不再持有已终止或未知状态的句柄。
        if let Some(child) = inner.child.take() {
            let _ = child.kill();
        }
        inner.management_token = None;
        inner.lifecycle.mark_failed(message);
    }

    fn lock(&self) -> Result<MutexGuard<'_, CoreProcessInner>, String> {
        self.inner
            .lock()
            .map_err(|_| "Core 生命周期状态不可用".to_owned())
    }
}

#[derive(Serialize)]
struct BootstrapLine<'a> {
    management_token: &'a str,
    data_dir: &'a str,
}

fn serialize_bootstrap_line(token: &SecretToken, data_dir: &str) -> Result<Vec<u8>, ()> {
    let mut line = serde_json::to_vec(&BootstrapLine {
        management_token: token.as_str(),
        data_dir,
    })
    .map_err(|_| ())?;
    line.push(b'\n');
    Ok(line)
}

fn generate_management_token() -> Result<SecretToken, ()> {
    let mut random = [0_u8; MANAGEMENT_TOKEN_BYTES];
    fill_os_random(&mut random)?;

    const HEX: &[u8; 16] = b"0123456789abcdef";
    let mut encoded = Vec::with_capacity(MANAGEMENT_TOKEN_BYTES * 2);
    for byte in random {
        encoded.push(HEX[(byte >> 4) as usize]);
        encoded.push(HEX[(byte & 0x0f) as usize]);
    }
    random.fill(0);
    Ok(SecretToken(encoded))
}

#[cfg(target_os = "windows")]
fn fill_os_random(buffer: &mut [u8]) -> Result<(), ()> {
    let length = u32::try_from(buffer.len()).map_err(|_| ())?;
    // 使用 Windows CNG 系统首选 CSPRNG，不依赖可预测时间、PID 或应用状态。
    let status = unsafe {
        BCryptGenRandom(
            std::ptr::null_mut(),
            buffer.as_mut_ptr(),
            length,
            BCRYPT_USE_SYSTEM_PREFERRED_RNG,
        )
    };
    if status == 0 {
        Ok(())
    } else {
        buffer.fill(0);
        Err(())
    }
}

#[cfg(not(target_os = "windows"))]
fn fill_os_random(_buffer: &mut [u8]) -> Result<(), ()> {
    // V1 构建目标固定为 Windows；其他平台需在后续任务选择对应系统 CSPRNG。
    Err(())
}
