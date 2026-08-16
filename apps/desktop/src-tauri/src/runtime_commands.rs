use serde::Deserialize;
use tauri::{AppHandle, State};

use crate::core_process::{
    CoreProcessManager, CreateManualModelInput, CreateProviderInput, DashboardSnapshot,
    ModelListQuery, ModelPage, ModelSummary, OneTimeLocalKey, ProviderSummary, ProviderTestResult,
    RuntimeSnapshot, SyncModelsResult, UpdateModelLimitsInput, UpdateProviderInput,
};
use crate::data_plane_client::{self, LocalResponsesTestResult};

#[tauri::command]
pub async fn runtime_status(
    state: State<'_, CoreProcessManager>,
) -> Result<RuntimeSnapshot, String> {
    state.status()
}

#[tauri::command]
pub async fn dashboard_status(
    state: State<'_, CoreProcessManager>,
) -> Result<DashboardSnapshot, String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.dashboard())
        .await
        .map_err(|_| "读取 Core 概览失败".to_owned())?
}

#[tauri::command]
pub async fn create_local_key(
    name: String,
    state: State<'_, CoreProcessManager>,
) -> Result<OneTimeLocalKey, String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.create_local_key(name))
        .await
        .map_err(|_| "创建 Local Key 失败".to_owned())?
}

#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub struct LocalResponsesTestInput {
    pub local_key: String,
    pub model: String,
    pub kind: String,
}

#[tauri::command]
pub async fn test_local_responses(
    input: LocalResponsesTestInput,
    state: State<'_, CoreProcessManager>,
) -> Result<LocalResponsesTestResult, String> {
    let runtime = state.status()?;
    let data_plane_url = runtime
        .data_plane_url
        .ok_or_else(|| "本地网关尚未启动".to_owned())?;
    tauri::async_runtime::spawn_blocking(move || {
        let mut local_key = input.local_key.into_bytes();
        data_plane_client::test_responses(
            &data_plane_url,
            &mut local_key,
            &input.model,
            &input.kind,
        )
    })
    .await
    .map_err(|_| "本地 Responses 测试失败".to_owned())?
}

#[tauri::command]
pub async fn create_provider(
    input: CreateProviderInput,
    state: State<'_, CoreProcessManager>,
) -> Result<ProviderSummary, String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.create_provider(input))
        .await
        .map_err(|_| "创建服务失败".to_owned())?
}

#[tauri::command]
pub async fn update_provider(
    id: String,
    input: UpdateProviderInput,
    adapter_config: super::core_process::AdapterConfig,
    state: State<'_, CoreProcessManager>,
) -> Result<ProviderSummary, String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.update_provider(id, input, adapter_config))
        .await
        .map_err(|_| "更新服务失败".to_owned())?
}

#[tauri::command]
pub async fn delete_provider(
    id: String,
    version: i64,
    state: State<'_, CoreProcessManager>,
) -> Result<(), String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.delete_provider(id, version))
        .await
        .map_err(|_| "删除服务失败".to_owned())?
}

#[tauri::command]
pub async fn enable_provider(
    id: String,
    version: i64,
    state: State<'_, CoreProcessManager>,
) -> Result<ProviderSummary, String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.set_provider_enabled(id, version, true))
        .await
        .map_err(|_| "启用服务失败".to_owned())?
}

#[tauri::command]
pub async fn disable_provider(
    id: String,
    version: i64,
    state: State<'_, CoreProcessManager>,
) -> Result<ProviderSummary, String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.set_provider_enabled(id, version, false))
        .await
        .map_err(|_| "停用服务失败".to_owned())?
}

#[tauri::command]
pub async fn test_provider(
    id: String,
    state: State<'_, CoreProcessManager>,
) -> Result<ProviderTestResult, String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.test_provider(id))
        .await
        .map_err(|_| "测试服务失败".to_owned())?
}

#[tauri::command]
pub async fn sync_provider_models(
    id: String,
    state: State<'_, CoreProcessManager>,
) -> Result<SyncModelsResult, String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.sync_provider_models(id))
        .await
        .map_err(|_| "同步模型失败".to_owned())?
}

#[tauri::command]
pub async fn list_models(
    query: ModelListQuery,
    state: State<'_, CoreProcessManager>,
) -> Result<ModelPage, String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.list_models(query))
        .await
        .map_err(|_| "读取模型目录失败".to_owned())?
}

#[tauri::command]
pub async fn update_model_capabilities(
    id: String,
    input: super::core_process::UpdateModelCapabilitiesInput,
    state: State<'_, CoreProcessManager>,
) -> Result<ModelSummary, String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.update_model_capabilities(id, input))
        .await
        .map_err(|_| "更新模型能力失败".to_owned())?
}

#[tauri::command]
pub async fn update_model_limits(
    id: String,
    input: UpdateModelLimitsInput,
    state: State<'_, CoreProcessManager>,
) -> Result<ModelSummary, String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.update_model_limits(id, input))
        .await
        .map_err(|_| "更新模型参数失败".to_owned())?
}

#[tauri::command]
pub async fn create_manual_model(
    provider_id: String,
    input: CreateManualModelInput,
    state: State<'_, CoreProcessManager>,
) -> Result<ModelSummary, String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.create_manual_model(provider_id, input))
        .await
        .map_err(|_| "创建手工模型失败".to_owned())?
}

#[tauri::command]
pub async fn delete_manual_model(
    id: String,
    version: i64,
    state: State<'_, CoreProcessManager>,
) -> Result<(), String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.delete_manual_model(id, version))
        .await
        .map_err(|_| "删除手工模型失败".to_owned())?
}

#[tauri::command]
pub async fn enable_model(
    id: String,
    version: i64,
    state: State<'_, CoreProcessManager>,
) -> Result<ModelSummary, String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.set_model_enabled(id, version, true))
        .await
        .map_err(|_| "启用模型失败".to_owned())?
}

#[tauri::command]
pub async fn disable_model(
    id: String,
    version: i64,
    state: State<'_, CoreProcessManager>,
) -> Result<ModelSummary, String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.set_model_enabled(id, version, false))
        .await
        .map_err(|_| "禁用模型失败".to_owned())?
}

#[tauri::command]
pub async fn runtime_start(
    app: AppHandle,
    state: State<'_, CoreProcessManager>,
) -> Result<RuntimeSnapshot, String> {
    state.start(&app)
}

#[tauri::command]
pub async fn runtime_stop(state: State<'_, CoreProcessManager>) -> Result<RuntimeSnapshot, String> {
    state.stop()
}

#[tauri::command]
pub async fn runtime_restart(
    app: AppHandle,
    state: State<'_, CoreProcessManager>,
) -> Result<RuntimeSnapshot, String> {
    state.restart(&app)
}
