import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({ listRequests: vi.fn(), getRequest: vi.fn(), usageSummary: vi.fn(), usageTimeSeries: vi.fn() }));
vi.mock("../../lib/desktop-api", () => ({ desktopApi: api }));
import { DashboardPage } from "../../pages/DashboardPage";
import { RequestListPage } from "../../pages/RequestListPage";
import { UsagePage } from "../../pages/UsagePage";

const usage = { request_count: 2, succeeded_count: 2, failed_count: 0, cancelled_count: 0, input_tokens: 10, output_tokens: 4, cached_input_tokens: 2, cache_write_tokens: 0, reasoning_tokens: 0, input_token_reported_count: 2, output_token_reported_count: 2, cached_input_token_reported_count: 2, reasoning_token_reported_count: 2, cache_eligible_input_tokens: 10, cache_eligible_cached_input_tokens: 2, cache_hit_rate_basis_points: 2000 };
beforeEach(() => { Object.values(api).forEach((mock) => mock.mockReset()); api.usageSummary.mockResolvedValue(usage); api.usageTimeSeries.mockResolvedValue({ data: [{ date_utc: "2026-08-16", ...usage }] }); api.listRequests.mockResolvedValue({ data: [{ id:"r1", created_at:"2026-08-16T00:00:00Z", completed_at:null, source_protocol:"openai_responses", provider_slug:"demo", public_model_id:"demo/model", streaming:false, status:"succeeded", http_status:200, error_code:null, retryable:false, input_tokens:1, output_tokens:2, cached_input_tokens:null, cache_write_tokens:null, reasoning_tokens:null, duration_ms:10 }], next_cursor:null }); });
afterEach(cleanup);

describe("observability pages", () => {
  it("renders dashboard and usage summaries", async () => {
    render(<DashboardPage />);
    expect(await screen.findByText("最近七天")).toBeTruthy();
    expect(screen.getAllByText("输出 Token").length).toBeGreaterThan(0);
    cleanup();
    render(<UsagePage />);
    expect(await screen.findByText("Token 用量")).toBeTruthy();
    expect(screen.getAllByText("20%").length).toBeGreaterThan(0);
  });

  it("opens request details and keeps body data out of the drawer", async () => {
    api.getRequest.mockResolvedValue({ id:"r1", created_at:"2026-08-16T00:00:00Z", completed_at:null, source_protocol:"openai_responses", provider_slug:"demo", public_model_id:"demo/model", streaming:false, status:"succeeded", http_status:200, error_code:null, retryable:false, input_tokens:1, output_tokens:2, cached_input_tokens:null, cache_write_tokens:null, reasoning_tokens:null, duration_ms:10 });
    const user = userEvent.setup();
    render(<RequestListPage />);
    await user.click(await screen.findByRole("button", { name: /2026/ }));
    expect(await screen.findByText("默认不保存 Prompt、回复正文、请求 Header 或 Tool 参数。")).toBeTruthy();
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("renders a safe retry error state", async () => {
    api.usageSummary.mockRejectedValue(new Error("secret"));
    render(<UsagePage />);
    expect((await screen.findByRole("alert")).textContent).toContain("读取用量数据失败");
  });
});
