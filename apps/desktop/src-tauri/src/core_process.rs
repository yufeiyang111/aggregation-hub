#[cfg(target_os = "windows")]
use std::ffi::c_void;
use std::sync::{Arc, Mutex, MutexGuard};

use serde::{Deserialize, Serialize, Serializer};

use crate::control_client;
use tauri::{AppHandle, Manager};
use tauri_plugin_shell::process::{CommandChild, CommandEvent};
use tauri_plugin_shell::ShellExt;

const SIDECAR_NAME: &str = "aggregation-hub-core";
const BOOTSTRAP_ARGUMENT: &str = "--bootstrap-stdin";
const MANAGEMENT_TOKEN_BYTES: usize = 32;
const MAX_READY_LINE_BYTES: usize = 8 * 1024;
const CORE_VERSION: &str = "0.1.0-rc.6";

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
    pub auth_type: String,
    pub base_url: String,
    pub lifecycle_status: String,
    pub enabled: bool,
    pub timeout_ms: i64,
    pub adapter_config: AdapterConfig,
    pub version: i64,
    pub credential: CredentialState,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct CredentialState {
    pub configured: bool,
    pub masked_hint: Option<String>,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct CreateProviderInput {
    pub slug: String,
    pub name: String,
    pub adapter_type: String,
    pub auth_type: String,
    pub auth_header_mode: String,
    pub base_url: String,
    pub credential: Option<String>,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct UpdateProviderInput {
    pub name: String,
    pub base_url: String,
    pub timeout_ms: i64,
    pub auth_header_mode: String,
    pub credential: Option<String>,
    pub version: i64,
}

#[derive(Serialize)]
struct ProviderUpdateRequest {
    name: String,
    base_url: String,
    timeout_ms: i64,
    adapter_config: AdapterConfig,
    credential: Option<EphemeralSecret>,
    version: i64,
}

#[derive(Serialize)]
struct ProviderCreateRequest {
    slug: String,
    name: String,
    adapter_type: String,
    auth_type: String,
    base_url: String,
    timeout_ms: i64,
    adapter_config: AdapterConfig,
    credential: Option<EphemeralSecret>,
    version: i64,
}

struct EphemeralSecret(Vec<u8>);

impl EphemeralSecret {
    fn from_string(value: String) -> Self {
        Self(value.into_bytes())
    }
}

impl Serialize for EphemeralSecret {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        let value = std::str::from_utf8(&self.0).map_err(serde::ser::Error::custom)?;
        serializer.serialize_str(value)
    }
}

impl Drop for EphemeralSecret {
    fn drop(&mut self) {
        self.0.fill(0);
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct AdapterConfig {
    wire_api: String,
    auth_header_mode: String,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct ProviderTestResult {
    pub success: bool,
    pub code: String,
    pub message: String,
    pub http_status: i64,
    pub retryable: bool,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct SyncModelsResult {
    pub discovered: i64,
}

#[derive(Clone, Debug, Serialize)]
pub struct DashboardSnapshot {
    pub runtime: RuntimeSnapshot,
    pub providers: Vec<ProviderSummary>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct ModelCapabilities {
    pub streaming: bool,
    pub tools: bool,
    pub parallel_tools: bool,
    pub reasoning: bool,
    pub thinking: bool,
    pub vision: bool,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ModelCapabilityOverride {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub supports_streaming: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub supports_tools: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub supports_parallel_tools: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub supports_reasoning: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub supports_thinking: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub supports_vision: Option<bool>,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct UpdateModelCapabilitiesInput {
    pub version: i64,
    pub capability_override: ModelCapabilityOverride,
}

#[derive(Clone, Debug, Default, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ModelLimitOverride {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub context_window_tokens: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub max_output_tokens: Option<i64>,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct UpdateModelLimitsInput {
    pub version: i64,
    pub limit_override: ModelLimitOverride,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct CreateManualModelInput {
    pub upstream_model_id: String,
    pub display_name: String,
    pub capabilities: ModelCapabilities,
    pub context_window_tokens: Option<i64>,
    pub max_output_tokens: Option<i64>,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct ModelSummary {
    pub id: String,
    pub provider_id: String,
    pub upstream_model_id: String,
    pub public_model_id: String,
    pub display_name: String,
    pub source: String,
    pub lifecycle_status: String,
    pub enabled: bool,
    pub capabilities: ModelCapabilities,
    pub context_window_tokens: Option<i64>,
    pub max_output_tokens: Option<i64>,
    pub capability_source: String,
    pub capability_override: ModelCapabilityOverride,
    pub limit_override: ModelLimitOverride,
    pub version: i64,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct ModelPage {
    pub data: Vec<ModelSummary>,
    pub next_cursor: Option<String>,
}

#[derive(Clone, Debug, Deserialize, Default)]
#[serde(deny_unknown_fields)]
pub struct ModelListQuery {
    pub cursor: Option<String>,
    pub page_size: Option<u16>,
    pub provider_id: Option<String>,
    pub lifecycle_status: Option<String>,
    pub enabled: Option<bool>,
    pub capability: Option<String>,
    pub search: Option<String>,
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

#[derive(Serialize)]
struct VersionRequest {
    version: i64,
}

#[derive(Serialize)]
struct ModelCapabilitiesUpdateRequest {
    version: i64,
    capability_override: ModelCapabilityOverride,
}

#[derive(Serialize)]
struct ModelLimitsUpdateRequest {
    version: i64,
    limit_override: ModelLimitOverride,
}

#[derive(Serialize)]
struct ManualModelCreateRequest {
    upstream_model_id: String,
    display_name: String,
    capabilities: ModelCapabilities,
    context_window_tokens: Option<i64>,
    max_output_tokens: Option<i64>,
}

fn validate_limit_override(value: &ModelLimitOverride) -> Result<(), String> {
    for limit in [value.context_window_tokens, value.max_output_tokens] {
        if let Some(limit) = limit {
            if limit <= 0 {
                return Err("模型参数必须是正整数".to_owned());
            }
        }
    }
    Ok(())
}

fn validate_manual_model_input(input: &CreateManualModelInput) -> Result<(), String> {
    if input.upstream_model_id.trim().is_empty()
        || input.upstream_model_id.trim().len() > 255
        || input.display_name.trim().is_empty()
        || input.display_name.trim().len() > 255
    {
        return Err("手工模型标识或名称无效".to_owned());
    }
    validate_limit_override(&ModelLimitOverride {
        context_window_tokens: input.context_window_tokens,
        max_output_tokens: input.max_output_tokens,
    })
}

pub(crate) fn validate_create_provider(input: &CreateProviderInput) -> Result<(), String> {
    if input.slug.is_empty()
        || input.slug.len() > 48
        || input
            .slug
            .bytes()
            .any(|byte| !(byte.is_ascii_lowercase() || byte.is_ascii_digit() || byte == b'-'))
        || input.name.trim().is_empty()
        || input.name.chars().count() > 128
        || input.base_url.trim().is_empty()
        || input.base_url.len() > 2048
        || !matches!(
            input.adapter_type.as_str(),
            "openai-compatible" | "local-openai-compatible"
        )
        || !matches!(
            input.auth_type.as_str(),
            "api_key" | "bearer_token" | "none"
        )
        || !matches!(
            input.auth_header_mode.as_str(),
            "authorization_bearer" | "x_api_key"
        )
    {
        return Err("服务配置无效".to_owned());
    }
    match input.auth_type.as_str() {
        "none" if input.credential.is_some() => Err("无认证服务不应填写密钥".to_owned()),
        "api_key" | "bearer_token"
            if input
                .credential
                .as_deref()
                .map(str::trim)
                .filter(|value| !value.is_empty())
                .is_none() =>
        {
            Err("请填写上游密钥".to_owned())
        }
        _ => Ok(()),
    }
}

pub(crate) fn validate_update_provider(input: &UpdateProviderInput) -> Result<(), String> {
    if input.name.trim().is_empty()
        || input.name.chars().count() > 128
        || input.base_url.trim().is_empty()
        || input.base_url.len() > 2048
        || !(1_000..=3_600_000).contains(&input.timeout_ms)
        || input.version < 1
        || !matches!(
            input.auth_header_mode.as_str(),
            "authorization_bearer" | "x_api_key"
        )
        || input
            .credential
            .as_deref()
            .is_some_and(|value| value.trim().is_empty() || value.len() > 5_120)
    {
        return Err("服务配置无效".to_owned());
    }
    Ok(())
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

pub(crate) fn build_model_list_path(query: &ModelListQuery) -> Result<String, String> {
    if let Some(page_size) = query.page_size {
        if page_size == 0 || page_size > 200 {
            return Err("模型分页参数无效".to_owned());
        }
    }
    if let Some(cursor) = query.cursor.as_deref() {
        if !valid_model_id(cursor) {
            return Err("模型分页游标无效".to_owned());
        }
    }
    if let Some(provider_id) = query.provider_id.as_deref() {
        if !valid_model_id(provider_id) {
            return Err("Provider 标识无效".to_owned());
        }
    }
    if let Some(status) = query.lifecycle_status.as_deref() {
        if !matches!(
            status,
            "available" | "degraded" | "missing_upstream" | "disabled"
        ) {
            return Err("模型状态筛选无效".to_owned());
        }
    }
    if let Some(capability) = query.capability.as_deref() {
        if !matches!(
            capability,
            "streaming" | "tools" | "parallel_tools" | "reasoning" | "thinking" | "vision"
        ) {
            return Err("模型能力筛选无效".to_owned());
        }
    }
    if let Some(search) = query.search.as_deref() {
        if search.is_empty()
            || search.trim() != search
            || search.len() > 128
            || search.chars().any(char::is_control)
        {
            return Err("模型搜索条件无效".to_owned());
        }
    }

    let mut parameters = Vec::new();
    if let Some(cursor) = query.cursor.as_deref() {
        parameters.push(("cursor", cursor.to_owned()));
    }
    if let Some(page_size) = query.page_size {
        parameters.push(("page_size", page_size.to_string()));
    }
    if let Some(provider_id) = query.provider_id.as_deref() {
        parameters.push(("provider_id", provider_id.to_owned()));
    }
    if let Some(status) = query.lifecycle_status.as_deref() {
        parameters.push(("lifecycle_status", status.to_owned()));
    }
    if let Some(enabled) = query.enabled {
        parameters.push(("enabled", enabled.to_string()));
    }
    if let Some(capability) = query.capability.as_deref() {
        parameters.push(("capability", capability.to_owned()));
    }
    if let Some(search) = query.search.as_deref() {
        parameters.push(("search", percent_encode(search)));
    }
    if parameters.is_empty() {
        return Ok("/internal/v1/models".to_owned());
    }
    let query = parameters
        .into_iter()
        .map(|(name, value)| format!("{name}={value}"))
        .collect::<Vec<_>>()
        .join("&");
    Ok(format!("/internal/v1/models?{query}"))
}

fn valid_model_id(value: &str) -> bool {
    !value.is_empty() && value.len() <= 64 && value.bytes().all(|byte| byte.is_ascii_alphanumeric())
}

fn percent_encode(value: &str) -> String {
    const HEX: &[u8; 16] = b"0123456789ABCDEF";
    let mut result = String::with_capacity(value.len());
    for byte in value.bytes() {
        if byte.is_ascii_alphanumeric() || matches!(byte, b'-' | b'_' | b'.' | b'~') {
            result.push(byte as char);
            continue;
        }
        result.push('%');
        result.push(HEX[(byte >> 4) as usize] as char);
        result.push(HEX[(byte & 0x0f) as usize] as char);
    }
    result
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

    pub fn list_models(&self, query: ModelListQuery) -> Result<ModelPage, String> {
        let path = build_model_list_path(&query)?;
        let inner = self.lock()?;
        if inner.lifecycle.snapshot().state != RuntimeState::Running {
            return Err("Core 尚未运行".to_owned());
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
        control_client::get_json(&ready.control_url, token.as_str(), &path)
    }

    pub fn set_model_enabled(
        &self,
        id: String,
        version: i64,
        enabled: bool,
    ) -> Result<ModelSummary, String> {
        if !valid_model_id(&id) || version < 1 {
            return Err("模型标识或版本无效".to_owned());
        }
        let action = if enabled { "enable" } else { "disable" };
        let path = format!("/internal/v1/models/{id}/{action}");
        let inner = self.lock()?;
        if inner.lifecycle.snapshot().state != RuntimeState::Running {
            return Err("Core 尚未运行".to_owned());
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
        control_client::post_json(
            &ready.control_url,
            token.as_str(),
            &path,
            &VersionRequest { version },
        )
    }

    pub fn update_model_capabilities(
        &self,
        id: String,
        input: UpdateModelCapabilitiesInput,
    ) -> Result<ModelSummary, String> {
        if !valid_model_id(&id) || input.version < 1 {
            return Err("模型标识或版本无效".to_owned());
        }
        let inner = self.lock()?;
        if inner.lifecycle.snapshot().state != RuntimeState::Running {
            return Err("Core 尚未运行".to_owned());
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
        let path = format!("/internal/v1/models/{id}");
        control_client::patch_json(
            &ready.control_url,
            token.as_str(),
            &path,
            &ModelCapabilitiesUpdateRequest {
                version: input.version,
                capability_override: input.capability_override,
            },
        )
    }

    pub fn update_model_limits(
        &self,
        id: String,
        input: UpdateModelLimitsInput,
    ) -> Result<ModelSummary, String> {
        if !valid_model_id(&id) || input.version < 1 {
            return Err("模型标识或版本无效".to_owned());
        }
        validate_limit_override(&input.limit_override)?;
        let inner = self.lock()?;
        if inner.lifecycle.snapshot().state != RuntimeState::Running {
            return Err("Core 尚未运行".to_owned());
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
        let path = format!("/internal/v1/models/{id}/limits");
        control_client::patch_json(
            &ready.control_url,
            token.as_str(),
            &path,
            &ModelLimitsUpdateRequest {
                version: input.version,
                limit_override: input.limit_override,
            },
        )
    }

    pub fn create_manual_model(
        &self,
        provider_id: String,
        input: CreateManualModelInput,
    ) -> Result<ModelSummary, String> {
        if !valid_model_id(&provider_id) {
            return Err("服务标识无效".to_owned());
        }
        validate_manual_model_input(&input)?;
        let request = ManualModelCreateRequest {
            upstream_model_id: input.upstream_model_id.trim().to_owned(),
            display_name: input.display_name.trim().to_owned(),
            capabilities: input.capabilities,
            context_window_tokens: input.context_window_tokens,
            max_output_tokens: input.max_output_tokens,
        };
        let inner = self.lock()?;
        if inner.lifecycle.snapshot().state != RuntimeState::Running {
            return Err("Core 尚未运行".to_owned());
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
        let path = format!("/internal/v1/providers/{provider_id}/models");
        control_client::post_json(&ready.control_url, token.as_str(), &path, &request)
    }

    pub fn delete_manual_model(&self, id: String, version: i64) -> Result<(), String> {
        if !valid_model_id(&id) || version < 1 {
            return Err("模型标识或版本无效".to_owned());
        }
        let inner = self.lock()?;
        if inner.lifecycle.snapshot().state != RuntimeState::Running {
            return Err("Core 尚未运行".to_owned());
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
        let path = format!("/internal/v1/models/{id}");
        control_client::delete_json(
            &ready.control_url,
            token.as_str(),
            &path,
            &VersionRequest { version },
        )
    }

    pub fn create_provider(
        &self,
        mut input: CreateProviderInput,
    ) -> Result<ProviderSummary, String> {
        validate_create_provider(&input)?;
        let request = ProviderCreateRequest {
            slug: input.slug.trim().to_owned(),
            name: input.name.trim().to_owned(),
            adapter_type: input.adapter_type,
            auth_type: input.auth_type,
            base_url: input.base_url.trim().to_owned(),
            timeout_ms: 30_000,
            adapter_config: AdapterConfig {
                wire_api: "chat_completions".to_owned(),
                auth_header_mode: input.auth_header_mode,
            },
            credential: input.credential.take().map(EphemeralSecret::from_string),
            version: 0,
        };
        let inner = self.lock()?;
        if inner.lifecycle.snapshot().state != RuntimeState::Running {
            return Err("Core 尚未运行".to_owned());
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
        control_client::post_json(
            &ready.control_url,
            token.as_str(),
            "/internal/v1/providers",
            &request,
        )
    }

    pub fn update_provider(
        &self,
        id: String,
        mut input: UpdateProviderInput,
        adapter_config: AdapterConfig,
    ) -> Result<ProviderSummary, String> {
        if !valid_model_id(&id) {
            return Err("服务标识无效".to_owned());
        }
        validate_update_provider(&input)?;
        if !matches!(
            adapter_config.wire_api.as_str(),
            "chat_completions" | "responses"
        ) {
            return Err("服务配置无效".to_owned());
        }
        let request = ProviderUpdateRequest {
            name: input.name.trim().to_owned(),
            base_url: input.base_url.trim().to_owned(),
            timeout_ms: input.timeout_ms,
            adapter_config: AdapterConfig {
                wire_api: adapter_config.wire_api,
                auth_header_mode: input.auth_header_mode,
            },
            credential: input.credential.take().map(EphemeralSecret::from_string),
            version: input.version,
        };
        let path = format!("/internal/v1/providers/{id}");
        let inner = self.lock()?;
        if inner.lifecycle.snapshot().state != RuntimeState::Running {
            return Err("Core 尚未运行".to_owned());
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
        control_client::patch_json(&ready.control_url, token.as_str(), &path, &request)
    }

    pub fn delete_provider(&self, id: String, version: i64) -> Result<(), String> {
        if !valid_model_id(&id) || version < 1 {
            return Err("服务标识或版本无效".to_owned());
        }
        let path = format!("/internal/v1/providers/{id}");
        let inner = self.lock()?;
        if inner.lifecycle.snapshot().state != RuntimeState::Running {
            return Err("Core 尚未运行".to_owned());
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
        control_client::delete_json(
            &ready.control_url,
            token.as_str(),
            &path,
            &VersionRequest { version },
        )
    }

    pub fn set_provider_enabled(
        &self,
        id: String,
        version: i64,
        enabled: bool,
    ) -> Result<ProviderSummary, String> {
        if !valid_model_id(&id) || version < 1 {
            return Err("服务标识或版本无效".to_owned());
        }
        let action = if enabled { "enable" } else { "disable" };
        let path = format!("/internal/v1/providers/{id}/{action}");
        let inner = self.lock()?;
        if inner.lifecycle.snapshot().state != RuntimeState::Running {
            return Err("Core 尚未运行".to_owned());
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
        control_client::post_json(
            &ready.control_url,
            token.as_str(),
            &path,
            &VersionRequest { version },
        )
    }

    pub fn test_provider(&self, id: String) -> Result<ProviderTestResult, String> {
        if !valid_model_id(&id) {
            return Err("服务标识无效".to_owned());
        }
        let path = format!("/internal/v1/providers/{id}/test");
        let inner = self.lock()?;
        if inner.lifecycle.snapshot().state != RuntimeState::Running {
            return Err("Core 尚未运行".to_owned());
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
        control_client::post_json(&ready.control_url, token.as_str(), &path, &())
    }

    pub fn sync_provider_models(&self, id: String) -> Result<SyncModelsResult, String> {
        if !valid_model_id(&id) {
            return Err("服务标识无效".to_owned());
        }
        let path = format!("/internal/v1/providers/{id}/sync-models");
        let inner = self.lock()?;
        if inner.lifecycle.snapshot().state != RuntimeState::Running {
            return Err("Core 尚未运行".to_owned());
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
        control_client::post_json(&ready.control_url, token.as_str(), &path, &())
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
