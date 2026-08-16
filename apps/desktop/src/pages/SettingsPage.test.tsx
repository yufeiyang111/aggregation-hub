import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
  runtimeSettings: vi.fn(),
  listBackups: vi.fn(),
  updateRuntimeSettings: vi.fn(),
  pruneRequests: vi.fn(),
  createBackup: vi.fn(),
  scheduleRestore: vi.fn(),
}));

vi.mock("../lib/desktop-api", () => ({ desktopApi: api }));

import { SettingsPage } from "./SettingsPage";

const runtime = { state: "running" as const, data_plane_url: "http://127.0.0.1:18443", started_at: "2026-08-16T10:00:00Z", version: "0.1.0-rc.6", last_error: null };
const settings = { listen_port: 18443, request_timeout_ms: 60000, request_retention_days: 30, version: 2 };
const backup = { id: "backup-20260816t100000.000z-abcdef12", created_at: "2026-08-16T10:00:00Z", size_bytes: 2048 };

describe("SettingsPage", () => {
  beforeEach(() => {
    for (const mock of Object.values(api)) mock.mockReset();
    api.runtimeSettings.mockResolvedValue(settings);
    api.listBackups.mockResolvedValue({ data: [backup] });
  });
  afterEach(cleanup);

  it("loads settings, saves a typed update, and communicates restart requirement", async () => {
    api.updateRuntimeSettings.mockResolvedValue({ settings: { ...settings, listen_port: 19443, version: 3 }, restart_required: true });
    const user = userEvent.setup();
    render(<SettingsPage runtime={runtime} loading={false} actionPending={false} onRefreshRuntime={vi.fn()} onRestartRuntime={vi.fn()} />);

    expect(await screen.findByDisplayValue("18443")).toBeTruthy();
    await user.clear(screen.getByLabelText("端口"));
    await user.type(screen.getByLabelText("端口"), "19443");
    await user.click(screen.getByRole("button", { name: "保存设置" }));

    await waitFor(() => expect(api.updateRuntimeSettings).toHaveBeenCalledWith({ listen_port: 19443, request_timeout_ms: 60000, request_retention_days: 30, version: 2 }));
    expect((await screen.findByRole("status")).textContent).toContain("重启网关后生效");
  });

  it("requires a second confirmation before scheduling restore", async () => {
    api.scheduleRestore.mockResolvedValue({ safety_backup: { ...backup, id: "backup-safety" }, restart_required: true });
    const user = userEvent.setup();
    render(<SettingsPage runtime={runtime} loading={false} actionPending={false} onRefreshRuntime={vi.fn()} onRestartRuntime={vi.fn()} />);

    await screen.findByText("备份");
    await user.click(screen.getByRole("button", { name: "恢复" }));
    expect(screen.getByRole("dialog", { name: "确认恢复备份" })).toBeTruthy();
    expect(api.scheduleRestore).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "确认恢复" }));

    await waitFor(() => expect(api.scheduleRestore).toHaveBeenCalledWith(backup.id));
  });

  it("does not expose a raw maintenance error", async () => {
    api.createBackup.mockRejectedValue(new Error("C:\\Users\\private\\backup"));
    const user = userEvent.setup();
    render(<SettingsPage runtime={runtime} loading={false} actionPending={false} onRefreshRuntime={vi.fn()} onRestartRuntime={vi.fn()} />);

    await screen.findByText("备份");
    await user.click(screen.getByRole("button", { name: "创建备份" }));

    expect((await screen.findByRole("alert")).textContent).toContain("创建备份失败");
    expect(screen.getByRole("alert").textContent).not.toContain("C:\\Users");
  });
});