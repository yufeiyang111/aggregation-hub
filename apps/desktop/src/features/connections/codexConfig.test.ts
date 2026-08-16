import { describe, expect, it } from "vitest";
import { buildCodexConfig, codexBaseURL } from "./codexConfig";

describe("buildCodexConfig", () => {
  it("生成 Responses Provider、临时 PowerShell 环境变量和 Public Model", () => {
    expect(buildCodexConfig({ dataPlaneURL: "http://127.0.0.1:18443/", publicModelID: "bundle/gpt-test" })).toEqual({
      configToml: [
        'model_provider = "aggregation_hub"',
        'model = "bundle/gpt-test"',
        "",
        "[model_providers.aggregation_hub]",
        'name = "Aggregation Hub"',
        'base_url = "http://127.0.0.1:18443/v1"',
        'env_key = "AGGREGATION_HUB_LOCAL_KEY"',
        'wire_api = "responses"',
        "requires_openai_auth = false",
        "request_max_retries = 0",
        "stream_max_retries = 0",
      ].join("\n"),
      powerShellSession: '$env:AGGREGATION_HUB_LOCAL_KEY = "PASTE_LOCAL_KEY_HERE"\ncodex',
    });
  });

  it("拒绝非本地地址和包含空白字符的模型 ID", () => {
    expect(() => codexBaseURL("https://provider.example")).toThrow("只允许本地网关地址");
    expect(() => buildCodexConfig({ dataPlaneURL: "http://127.0.0.1:18443", publicModelID: "invalid model" })).toThrow("Public Model ID");
  });
});
