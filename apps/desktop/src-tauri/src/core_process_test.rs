use crate::core_process::{
    build_model_list_path, validate_create_provider, validate_update_provider, CreateProviderInput,
    ModelCapabilityOverride, ModelListQuery, ReadyEvent, RuntimeLifecycle, RuntimeState,
    UpdateModelCapabilitiesInput, UpdateProviderInput,
};

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

#[test]
fn model_query_builder_uses_only_typed_safe_parameters() {
    let path = build_model_list_path(&ModelListQuery {
        page_size: Some(50),
        enabled: Some(false),
        capability: Some("tools".to_owned()),
        search: Some("GPT 4/工具".to_owned()),
        ..ModelListQuery::default()
    })
    .expect("模型查询应可构建");
    assert_eq!(
        path,
        "/internal/v1/models?page_size=50&enabled=false&capability=tools&search=GPT%204%2F%E5%B7%A5%E5%85%B7"
    );
    assert!(build_model_list_path(&ModelListQuery {
        capability: Some("raw_sql".to_owned()),
        ..ModelListQuery::default()
    })
    .is_err());
    assert!(build_model_list_path(&ModelListQuery {
        search: Some(" bad".to_owned()),
        ..ModelListQuery::default()
    })
    .is_err());
}

#[test]
fn provider_update_input_allows_credential_omission_and_rejects_invalid_versions() {
    let valid = UpdateProviderInput {
        name: "Provider A".to_owned(),
        base_url: "https://example.test".to_owned(),
        timeout_ms: 30_000,
        auth_header_mode: "authorization_bearer".to_owned(),
        credential: None,
        version: 2,
    };
    assert!(validate_update_provider(&valid).is_ok());
    assert!(validate_update_provider(&UpdateProviderInput {
        version: 0,
        ..valid.clone()
    })
    .is_err());
    assert!(validate_update_provider(&UpdateProviderInput {
        credential: Some("  ".to_owned()),
        ..valid
    })
    .is_err());
}

#[test]
fn model_capability_input_is_typed_and_rejects_unknown_fields() {
    let input: UpdateModelCapabilitiesInput =
        serde_json::from_str(r#"{"version":3,"capability_override":{"supports_tools":false}}"#)
            .expect("已知模型能力覆盖应可解析");
    assert_eq!(input.version, 3);
    assert_eq!(input.capability_override.supports_tools, Some(false));
    assert!(serde_json::from_str::<UpdateModelCapabilitiesInput>(
        r#"{"version":3,"capability_override":{"unknown":true}}"#,
    )
    .is_err());
    assert!(serde_json::from_str::<ModelCapabilityOverride>(
        r#"{"supports_tools":true,"extra":false}"#,
    )
    .is_err());
    let reset =
        serde_json::to_string(&ModelCapabilityOverride::default()).expect("空覆盖对象应可序列化");
    assert_eq!(reset, "{}");
}

#[test]
fn provider_create_input_rejects_invalid_slug_and_missing_credential() {
    let valid = CreateProviderInput {
        slug: "provider-a".to_owned(),
        name: "Provider A".to_owned(),
        adapter_type: "openai-compatible".to_owned(),
        auth_type: "api_key".to_owned(),
        auth_header_mode: "authorization_bearer".to_owned(),
        base_url: "https://example.test".to_owned(),
        credential: Some("test-only-value".to_owned()),
    };
    assert!(validate_create_provider(&valid).is_ok());

    let invalid_slug = CreateProviderInput {
        slug: "Invalid".to_owned(),
        ..valid.clone()
    };
    assert!(validate_create_provider(&invalid_slug).is_err());
    let missing_credential = CreateProviderInput {
        credential: None,
        ..valid
    };
    assert!(validate_create_provider(&missing_credential).is_err());
}
