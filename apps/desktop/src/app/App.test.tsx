import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const apiMocks = vi.hoisted(() => ({
  dashboard: vi.fn(),
  createLocalKey: vi.fn(),
  start: vi.fn(),
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
    version: "0.1.0-rc.3",
    last_error: null,
  },
  providers: [
    {
      id: "provider-1",
      slug: "package-a",
      name: "Package A",
      adapter_type: "openai-compatible",
      base_url: "https://example.test",
      lifecycle_status: "enabled",
      enabled: true,
      version: 1,
    },
  ],
};

describe("App", () => {
  afterEach(cleanup);
  beforeEach(() => {
    apiMocks.dashboard.mockReset();
    apiMocks.createLocalKey.mockReset();
    apiMocks.start.mockReset();
    apiMocks.restart.mockReset();
    apiMocks.dashboard.mockResolvedValue(runningDashboard);
  });

  it("shows runtime and provider summaries through the desktop bridge", async () => {
    render(<App />);

    expect(await screen.findByText("http://127.0.0.1:18443")).toBeTruthy();
    expect(screen.getByText("Package A")).toBeTruthy();
    expect(apiMocks.dashboard).toHaveBeenCalledTimes(1);
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

    await waitFor(() => expect(apiMocks.dashboard).toHaveBeenCalledTimes(1));
    const createButton = screen.getByRole("button", { name: /Local Key/ }) as HTMLButtonElement;
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

    await screen.findByText("http://127.0.0.1:18443");
    await user.click(screen.getByRole("button", { name: /Local Key/ }));

    expect(await screen.findByText("ah_local_test_only")).toBeTruthy();
    await waitFor(() => expect(apiMocks.createLocalKey).toHaveBeenCalledWith("Claude Code"));
  });
});