#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod control_client;
mod core_process;
#[cfg(test)]
mod core_process_test;
mod runtime_commands;

use core_process::CoreProcessManager;
use runtime_commands::{
    create_local_key, create_manual_model, create_provider, dashboard_status, delete_manual_model,
    delete_provider, disable_model, disable_provider, enable_model, enable_provider, list_models,
    runtime_restart, runtime_start, runtime_status, runtime_stop, sync_provider_models,
    test_provider, update_model_capabilities, update_model_limits, update_provider,
};
use tauri::Manager;

fn run() {
    tauri::Builder::default()
        .manage(CoreProcessManager::new())
        // Shell 仅由 Rust 生命周期管理器调用；前端不接收任意命令、参数或环境变量。
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_autostart::init(
            tauri_plugin_autostart::MacosLauncher::LaunchAgent,
            None,
        ))
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_opener::init())
        .invoke_handler(tauri::generate_handler![
            runtime_status,
            dashboard_status,
            create_local_key,
            create_provider,
            update_provider,
            delete_provider,
            enable_provider,
            disable_provider,
            test_provider,
            sync_provider_models,
            list_models,
            update_model_capabilities,
            update_model_limits,
            create_manual_model,
            delete_manual_model,
            enable_model,
            disable_model,
            runtime_start,
            runtime_stop,
            runtime_restart
        ])
        .setup(|app| {
            let app_handle = app.handle().clone();
            let manager = app.state::<CoreProcessManager>().inner().clone();
            tauri::async_runtime::spawn(async move {
                // 启动失败会被记录为 Failed，WebView 只能通过 runtime_status 获取脱敏摘要。
                let _ = manager.start(&app_handle);
            });
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("Aggregation Hub 桌面应用启动失败")
        .run(|app, event| {
            if let tauri::RunEvent::Exit = event {
                let _ = app.state::<CoreProcessManager>().stop();
            }
        });
}

fn main() {
    run();
}
