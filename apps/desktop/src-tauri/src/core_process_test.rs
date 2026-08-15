use crate::core_process::{ReadyEvent, RuntimeLifecycle, RuntimeState};

fn ready_event() -> ReadyEvent {
    ReadyEvent {
        event: "ready".to_owned(),
        control_url: "http://127.0.0.1:49152".to_owned(),
        data_plane_url: "http://127.0.0.1:18443".to_owned(),
        pid: 42,
    }
}

#[test]
fn lifecycle_transitions_stopped_starting_running_stopped() {
    let mut lifecycle = RuntimeLifecycle::default();
    assert_eq!(lifecycle.snapshot().state, RuntimeState::Stopped);

    lifecycle.begin_start().expect("stopped 应允许启动");
    assert_eq!(lifecycle.snapshot().state, RuntimeState::Starting);

    lifecycle
        .mark_running(ready_event())
        .expect("starting 应允许进入 running");
    let running = lifecycle.snapshot();
    assert_eq!(running.state, RuntimeState::Running);
    assert_eq!(
        running.data_plane_url.as_deref(),
        Some("http://127.0.0.1:18443")
    );

    lifecycle.mark_stopped();
    let stopped = lifecycle.snapshot();
    assert_eq!(stopped.state, RuntimeState::Stopped);
    assert_eq!(stopped.data_plane_url, None);
    assert_eq!(stopped.last_error, None);
}

#[test]
fn lifecycle_rejects_duplicate_start() {
    let mut lifecycle = RuntimeLifecycle::default();
    lifecycle.begin_start().expect("首次启动应成功");

    let error = lifecycle
        .begin_start()
        .expect_err("starting 状态必须拒绝重复启动");

    assert_eq!(error, "Core 已启动或正在启动");
    assert_eq!(lifecycle.snapshot().state, RuntimeState::Starting);
}

#[test]
fn runtime_snapshot_never_serializes_management_token_or_control_url() {
    let mut lifecycle = RuntimeLifecycle::default();
    lifecycle.begin_start().expect("首次启动应成功");
    lifecycle
        .mark_running(ready_event())
        .expect("ready 事件应被接受");

    let raw = serde_json::to_string(&lifecycle.snapshot()).expect("snapshot 应可序列化");
    assert!(!raw.contains("management_token"));
    assert!(!raw.contains("token"));
    assert!(!raw.contains("control_url"));
    assert!(raw.contains("data_plane_url"));
}

#[test]
fn lifecycle_rejects_non_loopback_ready_urls() {
    let mut lifecycle = RuntimeLifecycle::default();
    lifecycle.begin_start().expect("首次启动应成功");

    let error = lifecycle
        .mark_running(ReadyEvent {
            event: "ready".to_owned(),
            control_url: "http://0.0.0.0:49152".to_owned(),
            data_plane_url: "http://127.0.0.1:18443".to_owned(),
            pid: 42,
        })
        .expect_err("非回环 Control URL 必须被拒绝");

    assert_eq!(error, "Core ready 事件无效");
    assert_eq!(lifecycle.snapshot().state, RuntimeState::Starting);
}
