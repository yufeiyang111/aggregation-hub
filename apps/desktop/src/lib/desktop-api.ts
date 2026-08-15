import { invoke } from "@tauri-apps/api/core";

export type RuntimeState = "stopped" | "starting" | "running" | "failed";

export interface RuntimeSnapshot {
  state: RuntimeState;
  data_plane_url: string | null;
  started_at: string | null;
  version: string;
  last_error: string | null;
}

export type AuthHeaderMode = "authorization_bearer" | "x_api_key";

export type AdapterConfig =
  | {
      wire_api: "chat_completions" | "responses";
      auth_header_mode: AuthHeaderMode;
      messages_path?: never;
      anthropic_version?: never;
    }
  | {
      messages_path: "/v1/messages";
      anthropic_version: "2023-06-01";
      auth_header_mode: AuthHeaderMode;
      wire_api?: never;
    };

export interface CredentialState {
  configured: boolean;
  masked_hint?: string;
}

export interface ProviderSummary {
  id: string;
  slug: string;
  name: string;
  adapter_type: string;
  auth_type: "api_key" | "bearer_token" | "none";
  base_url: string;
  lifecycle_status: string;
  enabled: boolean;
  timeout_ms: number;
  adapter_config: AdapterConfig;
  version: number;
  credential: CredentialState;
}

export interface DashboardSnapshot {
  runtime: RuntimeSnapshot;
  providers: ProviderSummary[];
}

export interface CreateProviderInput {
  slug: string;
  name: string;
  adapter_type: "openai-compatible" | "local-openai-compatible" | "anthropic-compatible";
  auth_type: "api_key" | "bearer_token" | "none";
  auth_header_mode: AuthHeaderMode;
  base_url: string;
  credential?: string;
}

export interface UpdateProviderInput {
  name: string;
  base_url: string;
  timeout_ms: number;
  auth_header_mode: AdapterConfig["auth_header_mode"];
  credential?: string;
  version: number;
}

export interface ProviderTestResult {
  success: boolean;
  code: string;
  message: string;
  http_status: number;
  retryable: boolean;
}

export interface SyncModelsResult {
  discovered: number;
}

export interface ModelCapabilities {
  streaming: boolean;
  tools: boolean;
  parallel_tools: boolean;
  reasoning: boolean;
  thinking: boolean;
  vision: boolean;
}

export interface ModelCapabilityOverride {
  supports_streaming?: boolean;
  supports_tools?: boolean;
  supports_parallel_tools?: boolean;
  supports_reasoning?: boolean;
  supports_thinking?: boolean;
  supports_vision?: boolean;
}

export interface UpdateModelCapabilitiesInput {
  version: number;
  capability_override: ModelCapabilityOverride;
}

export interface ModelLimitOverride {
  context_window_tokens?: number;
  max_output_tokens?: number;
}

export interface UpdateModelLimitsInput {
  version: number;
  limit_override: ModelLimitOverride;
}

export interface CreateManualModelInput {
  upstream_model_id: string;
  display_name: string;
  capabilities: ModelCapabilities;
  context_window_tokens?: number;
  max_output_tokens?: number;
}

export interface ModelSummary {
  id: string;
  provider_id: string;
  upstream_model_id: string;
  public_model_id: string;
  display_name: string;
  source: string;
  lifecycle_status: "available" | "degraded" | "missing_upstream" | "disabled";
  enabled: boolean;
  capabilities: ModelCapabilities;
  context_window_tokens: number | null;
  max_output_tokens: number | null;
  capability_source: string;
  capability_override: ModelCapabilityOverride;
  limit_override: ModelLimitOverride;
  version: number;
}

export interface ModelListQuery {
  cursor?: string;
  page_size?: number;
  provider_id?: string;
  lifecycle_status?: ModelSummary["lifecycle_status"];
  enabled?: boolean;
  capability?: "streaming" | "tools" | "parallel_tools" | "reasoning" | "thinking" | "vision";
  search?: string;
}

export interface ModelPage {
  data: ModelSummary[];
  next_cursor: string | null;
}

export interface OneTimeLocalKey {
  id: string;
  name: string;
  prefix: string;
  suffix: string;
  key: string;
  display_once: true;
}

export const desktopApi = {
  dashboard: () => invoke<DashboardSnapshot>("dashboard_status"),
  createProvider: (input: CreateProviderInput) => invoke<ProviderSummary>("create_provider", { input }),
  updateProvider: (id: string, input: UpdateProviderInput, adapterConfig: AdapterConfig) => invoke<ProviderSummary>("update_provider", { id, input, adapterConfig }),
  deleteProvider: (id: string, version: number) => invoke<void>("delete_provider", { id, version }),
  enableProvider: (id: string, version: number) => invoke<ProviderSummary>("enable_provider", { id, version }),
  disableProvider: (id: string, version: number) => invoke<ProviderSummary>("disable_provider", { id, version }),
  testProvider: (id: string) => invoke<ProviderTestResult>("test_provider", { id }),
  syncProviderModels: (id: string) => invoke<SyncModelsResult>("sync_provider_models", { id }),
  listModels: (query: ModelListQuery) => invoke<ModelPage>("list_models", { query }),
  updateModelCapabilities: (id: string, input: UpdateModelCapabilitiesInput) => invoke<ModelSummary>("update_model_capabilities", { id, input }),
  updateModelLimits: (id: string, input: UpdateModelLimitsInput) => invoke<ModelSummary>("update_model_limits", { id, input }),
  createManualModel: (provider_id: string, input: CreateManualModelInput) => invoke<ModelSummary>("create_manual_model", { provider_id, input }),
  deleteManualModel: (id: string, version: number) => invoke<void>("delete_manual_model", { id, version }),
  enableModel: (id: string, version: number) => invoke<ModelSummary>("enable_model", { id, version }),
  disableModel: (id: string, version: number) => invoke<ModelSummary>("disable_model", { id, version }),
  createLocalKey: (name: string) => invoke<OneTimeLocalKey>("create_local_key", { name }),
  start: () => invoke<RuntimeSnapshot>("runtime_start"),
  stop: () => invoke<RuntimeSnapshot>("runtime_stop"),
  restart: () => invoke<RuntimeSnapshot>("runtime_restart"),
};
