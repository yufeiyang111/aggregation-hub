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

const modelPage = {
  data: [
    {
      id: "model-1",
      provider_id: "provider-1",
      upstream_model_id: "gpt-test",
      public_model_id: "package-a/gpt-test",
      display_name: "GPT Test",
      source: "upstream",
      lifecycle_status: "available" as const,
      enabled: false,
      capabilities: { streaming: true, tools: true, parallel_tools: false, reasoning: false, thinking: false, vision: false },
      context_window_tokens: null,
      max_output_tokens: null,
      capability_source: "upstream",
      version: 3,
    },
  ],
  next_cursor: null,
};

describe("App", () => {
  afterEach(cleanup);

  beforeEach(() => {
    apiMocks.dashboard.mockReset();
    apiMocks.createProvider.mockReset();
    apiMocks.updateProvider.mockReset();
    apiMocks.deleteProvider.mockReset();
    apiMocks.enableProvider.mockReset();
    apiMocks.disableProvider.mockReset();
    apiMocks.testProvider.mockReset();
    apiMocks.syncProviderModels.mockReset();
    apiMocks.listModels.mockReset();
    apiMocks.enableModel.mockReset();
    apiMocks.disableModel.mockReset();
    apiMocks.createLocalKey.mockReset();
    apiMocks.start.mockReset();
    apiMocks.stop.mockReset();
    apiMocks.restart.mockReset();
    apiMocks.dashboard.mockResolvedValue(runningDashboard);
    apiMocks.createProvider.mockResolvedValue(runningDashboard.providers[0]);
    apiMocks.updateProvider.mockResolvedValue(runningDashboard.providers[0]);
    apiMocks.deleteProvider.mockResolvedValue(undefined);
    apiMocks.enableProvider.mockResolvedValue({ ...runningDashboard.providers[0], enabled: true, version: 2 });
    apiMocks.disableProvider.mockResolvedValue({ ...runningDashboard.providers[0], enabled: false, version: 2 });
    apiMocks.testProvider.mockResolvedValue({ success: true, code: "ok", message: "通过", http_status: 200, retryable: false });
    apiMocks.syncProviderModels.mockResolvedValue({ discovered: 1 });
    apiMocks.listModels.mockResolvedValue(modelPage);
    apiMocks.enableModel.mockResolvedValue({ ...modelPage.data[0], enabled: true, version: 4 });
    apiMocks.disableModel.mockResolvedValue({ ...modelPage.data[0], enabled: false, version: 4 });
  });

  it("shows provider summaries on the default service page", async () => {
    render(<App />);

    expect(await screen.findByRole("heading", { name: "服务" })).toBeTruthy();
    expect(screen.getByText("Package A")).toBeTruthy();
    expect(screen.getByText("https://example.test")).toBeTruthy();
    expect(apiMocks.dashboard).toHaveBeenCalledTimes(1);
  });

  it("keeps runtime details in the compact status menu", async () => {
    const user = userEvent.setup();
    render(<App />);

    await screen.findByText("Package A");
    await user.click(screen.getByRole("button", { name: "运行中" }));

    expect(screen.getByRole("dialog", { name: "网关状态和操作" })).toBeTruthy();
    expect(screen.getByText("http://127.0.0.1:18443")).toBeTruthy();
  });

  it("stops a running gateway from the compact status menu", async () => {
    apiMocks.stop.mockResolvedValue({ ...runningDashboard.runtime, state: "stopped", data_plane_url: null });
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Package A");
    await user.click(screen.getByRole("button", { name: "运行中" }));
    await user.click(screen.getByRole("button", { name: "停止网关" }));
    await waitFor(() => expect(apiMocks.stop).toHaveBeenCalledTimes(1));
  });

  it("does not allow Local Key creation while the gateway is stopped", async () => {
    apiMocks.dashboard.mockResolvedValue({
      ...runningDashboard,
      runtime: {
        ...runningDashboard.runtime,
        state: "stopped" as const,
        data_plane_url: null,
      },
    });
    const user = userEvent.setup();
    render(<App />);

    await screen.findByRole("heading", { name: "服务" });
    await user.click(screen.getByRole("button", { name: "客户端配置" }));
    const createButton = screen.getByRole("button", { name: "创建 Local Key" }) as HTMLButtonElement;
    expect(createButton.disabled).toBe(true);
    await user.click(createButton);
    expect(apiMocks.createLocalKey).not.toHaveBeenCalled();
  });

  it("shows a Local Key only after the desktop bridge creates it", async () => {
    apiMocks.createLocalKey.mockResolvedValue({
      id: "key-1",
      name: "Claude Code",
      prefix: "ah_local_test",
      suffix: "_only",
      key: "ah_local_test_only",
      display_once: true,
    });
    const user = userEvent.setup();
    render(<App />);

    await screen.findByText("Package A");
    await user.click(screen.getByRole("button", { name: "客户端配置" }));
    await user.click(screen.getByRole("button", { name: "创建 Local Key" }));

    expect(await screen.findByText("ah_local_test_only")).toBeTruthy();
    await waitFor(() => expect(apiMocks.createLocalKey).toHaveBeenCalledWith("Claude Code"));
  });

  it("loads models through the desktop bridge and confirms enablement", async () => {
    const user = userEvent.setup();
    render(<App />);

    await screen.findByText("Package A");
    await user.click(screen.getByRole("button", { name: "模型" }));

    expect(await screen.findByText("GPT Test")).toBeTruthy();
    await waitFor(() => expect(apiMocks.listModels).toHaveBeenCalledWith({ page_size: 50 }));
    await user.click(screen.getByRole("button", { name: "启用" }));
    expect(screen.getByRole("dialog", { name: "确认启用模型" })).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "确认启用" }));
    await waitFor(() => expect(apiMocks.enableModel).toHaveBeenCalledWith("model-1", 3));
  });
  it("creates a provider only through the typed desktop command", async () => {
    const user = userEvent.setup();
    render(<App />);

    await screen.findByText("Package A");
    await user.click(screen.getByRole("button", { name: "新增服务" }));
    await user.type(screen.getByLabelText("名称"), "我的 OpenAI");
    await user.type(screen.getByLabelText("Slug"), "my-openai");
    await user.type(screen.getByLabelText("上游地址"), "https://api.example.test");
    await user.type(screen.getByLabelText("上游密钥"), "secret-for-test-only");
    await user.click(screen.getByRole("button", { name: "保存服务" }));

    await waitFor(() => expect(apiMocks.createProvider).toHaveBeenCalledWith({
      slug: "my-openai",
      name: "我的 OpenAI",
      adapter_type: "openai-compatible",
      auth_type: "api_key",
      auth_header_mode: "authorization_bearer",
      base_url: "https://api.example.test",
      credential: "secret-for-test-only",
    }));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "新增服务" })).toBeNull());
  });

  it("requires confirmation before deleting a provider through the typed desktop command", async () => {
    const user = userEvent.setup();
    render(<App />);

    await screen.findByText("Package A");
    await user.click(screen.getByRole("button", { name: "删除" }));
    expect(screen.getByRole("dialog", { name: "删除服务" })).toBeTruthy();
    expect(apiMocks.deleteProvider).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "确认删除" }));
    await waitFor(() => expect(apiMocks.deleteProvider).toHaveBeenCalledWith("provider-1", 1));
  });

  it("updates provider settings without resubmitting the existing credential", async () => {
    const user = userEvent.setup();
    render(<App />);

    await screen.findByText("Package A");
    await user.click(screen.getByRole("button", { name: "编辑" }));
    expect(screen.getByRole("dialog", { name: "编辑服务" })).toBeTruthy();
    const name = screen.getByLabelText("名称");
    await user.clear(name);
    await user.type(name, "Package B");
    await user.click(screen.getByRole("button", { name: "保存修改" }));
    await waitFor(() => expect(apiMocks.updateProvider).toHaveBeenCalledWith("provider-1", {
      name: "Package B",
      base_url: "https://example.test",
      timeout_ms: 30000,
      auth_header_mode: "authorization_bearer",
      credential: undefined,
      version: 1,
    }, { wire_api: "chat_completions", auth_header_mode: "authorization_bearer" }));
  });

});