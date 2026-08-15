use tauri::{AppHandle, State};

use crate::core_process::{
    CoreProcessManager, CreateProviderInput, DashboardSnapshot, ModelListQuery, ModelPage,
    ModelSummary, OneTimeLocalKey, ProviderSummary, ProviderTestResult, RuntimeSnapshot,
    SyncModelsResult,
};

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
