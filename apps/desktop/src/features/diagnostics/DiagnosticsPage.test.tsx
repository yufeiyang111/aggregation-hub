import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
  diagnosticsSummary: vi.fn(),
  exportDiagnostics: vi.fn(),
  openDiagnosticsDirectory: vi.fn(),
}));

vi.mock("../../lib/desktop-api", () => ({ desktopApi: api }));

import { DiagnosticsPage } from "../../pages/DiagnosticsPage";

const availableSummary = {
  format_version: "diagnostics/v1",
  recent_error_count: 2,
  export_available: true,
};

describe("DiagnosticsPage", () => {
  beforeEach(() => {
    api.diagnosticsSummary.mockReset();
    api.exportDiagnostics.mockReset();
    api.openDiagnosticsDirectory.mockReset();
    api.diagnosticsSummary.mockResolvedValue(availableSummary);
  });

  afterEach(cleanup);

  it("loads a safe diagnostics summary and exports on explicit action", async () => {
    api.exportDiagnostics.mockResolvedValue({
      file_name: "aggregation-hub-diagnostics.zip",
      size_bytes: 512,
      generated_at: "2026-08-16T18:00:00Z",
      format_version: "diagnostics/v1",
    });

    const user = userEvent.setup();
    render(<DiagnosticsPage />);

    expect(await screen.findByText("最近安全错误 2 条")).toBeTruthy();

    await user.click(screen.getByRole("button", { name: "导出诊断包" }));

    expect(await screen.findByText("已导出 aggregation-hub-diagnostics.zip")).toBeTruthy();
  });

  it("opens only the fixed diagnostics directory after explicit action", async () => {
    api.openDiagnosticsDirectory.mockResolvedValue(undefined);

    const user = userEvent.setup();
    render(<DiagnosticsPage />);

    await screen.findByText("最近安全错误 2 条");
    await user.click(screen.getByRole("button", { name: "打开诊断文件夹" }));

    await waitFor(() => expect(api.openDiagnosticsDirectory).toHaveBeenCalledOnce());
    expect(screen.getByRole("status").textContent).toContain("已打开诊断文件夹");
  });

  it("shows a safe export error", async () => {
    api.exportDiagnostics.mockRejectedValue(new Error("导出诊断包失败"));

    const user = userEvent.setup();
    render(<DiagnosticsPage />);

    await screen.findByText("最近安全错误 2 条");
    await user.click(screen.getByRole("button", { name: "导出诊断包" }));

    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("导出诊断包失败"));
  });

  it("shows a safe directory error without exposing a path", async () => {
    api.openDiagnosticsDirectory.mockRejectedValue(new Error("C:\\Users\\private\\diagnostics-path"));

    const user = userEvent.setup();
    render(<DiagnosticsPage />);

    await screen.findByText("最近安全错误 2 条");
    await user.click(screen.getByRole("button", { name: "打开诊断文件夹" }));

    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("打开诊断文件夹失败"));
  });
});
