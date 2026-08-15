use tauri::{AppHandle, State};

use crate::core_process::{
    CoreProcessManager, DashboardSnapshot, OneTimeLocalKey, RuntimeSnapshot,
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
