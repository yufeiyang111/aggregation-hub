import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CodexSetupPage } from "./CodexSetupPage";

describe("CodexSetupPage", () => {
  afterEach(cleanup);
  it("展示可复制的用户级配置模板", async () => {
    const user = userEvent.setup();
    const copyConfig = vi.fn();
    const testResponses = vi.fn();
    render(<CodexSetupPage dataPlaneURL="http://127.0.0.1:18443" publicModelID="bundle/gpt-test" copyState="idle" onPublicModelIDChange={vi.fn()} onCopyConfig={copyConfig} onCopyPowerShell={vi.fn()} canTest testPending={false} testResult={null} onTest={testResponses} />);
    expect(screen.getByText((_, element) => element?.tagName === "CODE" && element.textContent?.includes("[model_providers.aggregation_hub]") === true)).toBeTruthy();
    expect(screen.getByText((_, element) => element?.tagName === "CODE" && element.textContent?.includes('base_url = "http://127.0.0.1:18443/v1"') === true)).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "复制配置" }));
    expect(copyConfig).toHaveBeenCalledWith(expect.stringContaining('wire_api = "responses"'));
    await user.click(screen.getByRole("button", { name: "测试文本" }));
    expect(testResponses).toHaveBeenCalledWith("text");
  });

  it("没有新建 Key 时禁用本地诊断", () => {
    render(<CodexSetupPage dataPlaneURL="http://127.0.0.1:18443" publicModelID="bundle/gpt-test" copyState="idle" onPublicModelIDChange={vi.fn()} onCopyConfig={vi.fn()} onCopyPowerShell={vi.fn()} canTest={false} testPending={false} testResult={null} onTest={vi.fn()} />);
    expect((screen.getByRole("button", { name: "测试文本" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("网关未启动时禁用生成与复制", () => {
    render(<CodexSetupPage dataPlaneURL={undefined} publicModelID="bundle/gpt-test" copyState="idle" onPublicModelIDChange={vi.fn()} onCopyConfig={vi.fn()} onCopyPowerShell={vi.fn()} canTest={false} testPending={false} testResult={null} onTest={vi.fn()} />);
    expect((screen.getByLabelText("Public Model ID") as HTMLInputElement).disabled).toBe(true);
    expect(screen.getByText("启动网关后将显示可复制的本地 Responses 配置。")).toBeTruthy();
  });
});
