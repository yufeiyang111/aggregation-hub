export const codexProviderID = "aggregation_hub";
export const codexLocalKeyEnvironment = "AGGREGATION_HUB_LOCAL_KEY";

export interface CodexConfigInput {
  dataPlaneURL: string;
  publicModelID: string;
}

export interface CodexConfigTemplate {
  configToml: string;
  powerShellSession: string;
}

export function buildCodexConfig(input: CodexConfigInput): CodexConfigTemplate {
  const baseURL = codexBaseURL(input.dataPlaneURL);
  const model = normalizeModelID(input.publicModelID);
  const quotedBaseURL = tomlString(baseURL);
  const quotedModel = tomlString(model);

  return {
    configToml: [
      `model_provider = "${codexProviderID}"`,
      `model = ${quotedModel}`,
      "",
      `[model_providers.${codexProviderID}]`,
      'name = "Aggregation Hub"',
      `base_url = ${quotedBaseURL}`,
      `env_key = "${codexLocalKeyEnvironment}"`,
      'wire_api = "responses"',
      "requires_openai_auth = false",
      "request_max_retries = 0",
      "stream_max_retries = 0",
    ].join("\n"),
    powerShellSession: `$env:${codexLocalKeyEnvironment} = "PASTE_LOCAL_KEY_HERE"\ncodex`,
  };
}

export function codexBaseURL(dataPlaneURL: string): string {
  const normalized = dataPlaneURL.trim().replace(/\/+$/, "");
  if (normalized === "") throw new Error("本地网关地址不能为空");
  let parsed: URL;
  try {
    parsed = new URL(normalized);
  } catch {
    throw new Error("本地网关地址无效");
  }
  if ((parsed.protocol !== "http:" && parsed.protocol !== "https:") || parsed.username !== "" || parsed.password !== "" || parsed.search !== "" || parsed.hash !== "") {
    throw new Error("本地网关地址无效");
  }
  if (parsed.hostname !== "127.0.0.1" && parsed.hostname !== "localhost" && parsed.hostname !== "::1") {
    throw new Error("Codex 配置只允许本地网关地址");
  }
  return `${normalized}/v1`;
}

function normalizeModelID(value: string): string {
  const model = value.trim();
  if (model === "" || model.length > 304 || /\s/.test(model)) throw new Error("请填写已启用的 Public Model ID");
  return model;
}

function tomlString(value: string): string {
  return `"${value.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
}
