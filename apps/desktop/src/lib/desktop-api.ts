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
  createLocalKey: (name: string) => invoke<OneTimeLocalKey>("create_local_key", { name }),
  start: () => invoke<RuntimeSnapshot>("runtime_start"),
  restart: () => invoke<RuntimeSnapshot>("runtime_restart"),
};