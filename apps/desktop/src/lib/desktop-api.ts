import { invoke } from "@tauri-apps/api/core";

export type RuntimeState = "stopped" | "starting" | "running" | "failed";

export interface RuntimeSnapshot {
  state: RuntimeState;
  data_plane_url: string | null;
  started_at: string | null;
  version: string;
  last_error: string | null;
}

export interface ProviderSummary {
  id: string;
  slug: string;
  name: string;
  adapter_type: string;
  base_url: string;
  lifecycle_status: string;
  enabled: boolean;
  version: number;
}

export interface DashboardSnapshot {
  runtime: RuntimeSnapshot;
  providers: ProviderSummary[];
}

export interface CreateProviderInput {
  slug: string;
  name: string;
  adapter_type: "openai-compatible" | "local-openai-compatible";
  auth_type: "api_key" | "bearer_token" | "none";
  auth_header_mode: "authorization_bearer" | "x_api_key";
  base_url: string;
  credential?: string;
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
  deleteProvider: (id: string, version: number) => invoke<void>("delete_provider", { id, version }),
  enableProvider: (id: string, version: number) => invoke<ProviderSummary>("enable_provider", { id, version }),
  disableProvider: (id: string, version: number) => invoke<ProviderSummary>("disable_provider", { id, version }),
  testProvider: (id: string) => invoke<ProviderTestResult>("test_provider", { id }),
  syncProviderModels: (id: string) => invoke<SyncModelsResult>("sync_provider_models", { id }),
  listModels: (query: ModelListQuery) => invoke<ModelPage>("list_models", { query }),
  enableModel: (id: string, version: number) => invoke<ModelSummary>("enable_model", { id, version }),
  disableModel: (id: string, version: number) => invoke<ModelSummary>("disable_model", { id, version }),
  createLocalKey: (name: string) => invoke<OneTimeLocalKey>("create_local_key", { name }),
  start: () => invoke<RuntimeSnapshot>("runtime_start"),
  stop: () => invoke<RuntimeSnapshot>("runtime_stop"),
  restart: () => invoke<RuntimeSnapshot>("runtime_restart"),
};
