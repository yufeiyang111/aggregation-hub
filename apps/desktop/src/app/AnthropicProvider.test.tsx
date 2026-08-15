import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const apiMocks = vi.hoisted(() => ({
  dashboard: vi.fn(),
  createProvider: vi.fn(),
  updateProvider: vi.fn(),
  deleteProvider: vi.fn(),
  enableProvider: vi.fn(),
  disableProvider: vi.fn(),
  testProvider: vi.fn(),
  syncProviderModels: vi.fn(),
  listModels: vi.fn(),
  updateModelCapabilities: vi.fn(),
  enableModel: vi.fn(),
  disableModel: vi.fn(),
  createLocalKey: vi.fn(),
  start: vi.fn(),
  stop: vi.fn(),
  restart: vi.fn(),
}));

vi.mock("../lib/desktop-api", () => ({
  desktopApi: apiMocks,
}));

import { App } from "./App";

const runningDashboard = {
  runtime: {
    state: "running" as const,
    data_plane_url: "http://127.0.0.1:18443",
    started_at: null,
    version: "0.1.0-rc.6",
    last_error: null,
  },
  providers: [
    {
      id: "provider-1",
      slug: "package-a",
      name: "Package A",
      adapter_type: "openai-compatible",
      auth_type: "api_key" as const,
      base_url: "https://example.test",
      lifecycle_status: "enabled",
      enabled: true,
      timeout_ms: 30000,
      adapter_config: { wire_api: "chat_completions" as const, auth_header_mode: "authorization_bearer" as const },
      credential: { configured: true, masked_hint: "已配置" },
      version: 1,
    },
  ],
};
const anthropicDashboard = {
  ...runningDashboard,
  providers: [{
    ...runningDashboard.providers[0],
    name: "Anthropic 上游",
    adapter_type: "anthropic-compatible",
    base_url: "https://api.anthropic.example",
    adapter_config: { messages_path: "/v1/messages" as const, anthropic_version: "2023-06-01" as const, auth_header_mode: "x_api_key" as const },
  }],
};


describe("App", () => {
  afterEach(cleanup);

  beforeEach(() => {
    apiMocks.dashboard.mockReset();
    apiMocks.createProvider.mockReset();
    apiMocks.updateProvider.mockReset();
    apiMocks.dashboard.mockResolvedValue(runningDashboard);
    apiMocks.createProvider.mockResolvedValue(runningDashboard.providers[0]);
    apiMocks.updateProvider.mockResolvedValue(runningDashboard.providers[0]);
  });

  it("creates an Anthropic Messages provider with API key authentication", async () => {
    const user = userEvent.setup();
    render(<App />);

    await screen.findByText("Package A");
    await user.click(screen.getByRole("button", { name: "新增服务" }));
    await user.type(screen.getByLabelText("名称"), "Anthropic 网关");
    await user.type(screen.getByLabelText("Slug"), "anthropic-gateway");
    await user.selectOptions(screen.getByLabelText("服务类型"), "anthropic-compatible");
    await user.type(screen.getByLabelText("上游地址"), "https://api.anthropic.example");
    await user.type(screen.getByLabelText("上游密钥"), "test-anthropic-key");
    await user.click(screen.getByRole("button", { name: "保存服务" }));

    await waitFor(() => expect(apiMocks.createProvider).toHaveBeenCalledWith({
      slug: "anthropic-gateway",
      name: "Anthropic 网关",
      adapter_type: "anthropic-compatible",
      auth_type: "api_key",
      auth_header_mode: "x_api_key",
      base_url: "https://api.anthropic.example",
      credential: "test-anthropic-key",
    }));
  });
  it("keeps Anthropic Messages configuration when editing a provider", async () => {
    const user = userEvent.setup();
    apiMocks.dashboard.mockResolvedValue(anthropicDashboard);
    apiMocks.updateProvider.mockResolvedValue(anthropicDashboard.providers[0]);
    render(<App />);

    await screen.findByText("Anthropic 上游");
    await user.click(screen.getByRole("button", { name: "编辑" }));
    await user.clear(screen.getByLabelText("名称"));
    await user.type(screen.getByLabelText("名称"), "Anthropic 新名称");
    await user.click(screen.getByRole("button", { name: "保存修改" }));

    await waitFor(() => expect(apiMocks.updateProvider).toHaveBeenCalledWith("provider-1", {
      name: "Anthropic 新名称",
      base_url: "https://api.anthropic.example",
      timeout_ms: 30000,
      auth_header_mode: "x_api_key",
      credential: undefined,
      version: 1,
    }, {
      messages_path: "/v1/messages",
      anthropic_version: "2023-06-01",
      auth_header_mode: "x_api_key",
    }));
  });

});
