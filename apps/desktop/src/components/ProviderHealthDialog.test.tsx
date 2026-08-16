import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ProviderHealthDialog } from "./ProviderHealthDialog";

const provider = {
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
};

describe("ProviderHealthDialog", () => {
  afterEach(cleanup);
  it("renders loading, empty and safe error states", () => {
    const { rerender } = render(<ProviderHealthDialog provider={provider} page={null} loading error={null} onClose={() => undefined} />);
    expect(screen.getByRole("status").textContent).toContain("正在读取测试记录");

    rerender(<ProviderHealthDialog provider={provider} page={{ data: [] }} loading={false} error={null} onClose={() => undefined} />);
    expect(screen.getByText("还没有测试记录。")).toBeTruthy();

    rerender(<ProviderHealthDialog provider={provider} page={null} loading={false} error="无法读取测试记录" onClose={() => undefined} />);
    expect(screen.getByRole("alert").textContent).toBe("无法读取测试记录");
  });

  it("renders only safe record fields", () => {
    render(<ProviderHealthDialog provider={provider} page={{ data: [{ id: "health-1", check_type: "models", status: "failed", latency_ms: 18, error_code: "upstream_auth_failed", checked_at: "2026-08-16T08:00:00Z" }] }} loading={false} error={null} onClose={() => undefined} />);

    expect(screen.getByText("失败")).toBeTruthy();
    expect(screen.getByText("模型列表")).toBeTruthy();
    expect(screen.getByText("18 ms")).toBeTruthy();
    expect(screen.getByText("upstream_auth_failed")).toBeTruthy();
  });

  it("moves focus into the dialog and closes with Escape", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    render(<ProviderHealthDialog provider={provider} page={{ data: [] }} loading={false} error={null} onClose={onClose} />);

    expect(document.activeElement).toBe(screen.getByRole("button", { name: "关闭测试记录" }));
    await user.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalledTimes(1);
  });

});
