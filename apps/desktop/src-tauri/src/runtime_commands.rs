use serde::Deserialize;
use std::{
    fs,
    path::{Path, PathBuf},
};

use tauri::{AppHandle, Manager, State};
use tauri_plugin_opener::OpenerExt;

use crate::core_process::{
    CoreProcessManager, CreateManualModelInput, CreateProviderInput, DashboardSnapshot,
    DiagnosticsExport, DiagnosticsSummary, ModelListQuery, ModelPage, ModelSummary,
    OneTimeLocalKey, ProviderHealthPage, ProviderSummary, ProviderTestResult, RequestListQuery,
    RequestMetadata, RequestPage, RuntimeSnapshot, SyncModelsResult, UpdateModelLimitsInput,
    UpdateProviderInput, UsageQuery, UsageSummary, UsageTimeSeries,
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
pub async fn list_requests(
    query: RequestListQuery,
    state: State<'_, CoreProcessManager>,
) -> Result<RequestPage, String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.list_requests(query))
        .await
        .map_err(|_| "读取请求记录失败".to_owned())?
}
#[tauri::command]
pub async fn get_request(
    id: String,
    state: State<'_, CoreProcessManager>,
) -> Result<RequestMetadata, String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.get_request(id))
        .await
        .map_err(|_| "读取请求详情失败".to_owned())?
}
#[tauri::command]
pub async fn usage_summary(
    query: UsageQuery,
    state: State<'_, CoreProcessManager>,
) -> Result<UsageSummary, String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.usage_summary(query))
        .await
        .map_err(|_| "读取用量汇总失败".to_owned())?
}
#[tauri::command]
pub async fn usage_time_series(
    query: UsageQuery,
    state: State<'_, CoreProcessManager>,
) -> Result<UsageTimeSeries, String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.usage_time_series(query))
        .await
        .map_err(|_| "读取用量趋势失败".to_owned())?
}
#[tauri::command]
pub fn open_diagnostics_directory(app: AppHandle) -> Result<(), String> {
    let local_data_dir = app
        .path()
        .local_data_dir()
        .map_err(|_| "运行数据目录不可用".to_owned())?;
    let diagnostics_dir = diagnostics_directory_path(&local_data_dir);

    fs::create_dir_all(&diagnostics_dir).map_err(|_| "创建诊断目录失败".to_owned())?;
    app.opener()
        .open_path(diagnostics_dir.to_string_lossy(), None::<&str>)
        .map_err(|_| "打开诊断目录失败".to_owned())
}

fn diagnostics_directory_path(local_data_dir: &Path) -> PathBuf {
    local_data_dir.join("AggregationHub").join("diagnostics")
}

#[tauri::command]
pub async fn diagnostics_summary(
    state: State<'_, CoreProcessManager>,
) -> Result<DiagnosticsSummary, String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.diagnostics())
        .await
        .map_err(|_| "读取诊断摘要失败")?
}

#[tauri::command]
pub async fn diagnostics_export(
    state: State<'_, CoreProcessManager>,
) -> Result<DiagnosticsExport, String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.export_diagnostics())
        .await
        .map_err(|_| "导出诊断包失败")?
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
pub async fn list_provider_health(
    id: String,
    state: State<'_, CoreProcessManager>,
) -> Result<ProviderHealthPage, String> {
    let manager = state.inner().clone();
    tauri::async_runtime::spawn_blocking(move || manager.list_provider_health(id))
        .await
        .map_err(|_| "读取服务健康记录失败".to_owned())?
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

#[cfg(test)]
mod tests {
    use std::path::Path;

    use super::diagnostics_directory_path;

    #[test]
    fn diagnostics_directory_is_fixed_below_local_data_directory() {
        let directory = diagnostics_directory_path(Path::new(r"C:\\Users\\tester\\AppData\\Local"));

        assert_eq!(
            directory,
            Path::new(r"C:\\Users\\tester\\AppData\\Local")
                .join("AggregationHub")
                .join("diagnostics")
        );
    }
}
