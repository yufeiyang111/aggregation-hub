import { useMemo } from "react";
import { buildCodexConfig, type CodexConfigTemplate } from "./codexConfig";
import type { LocalResponsesTestResult } from "../../lib/desktop-api";

type CopyState = "idle" | "copied" | "failed";

export function CodexSetupPage({ dataPlaneURL, publicModelID, copyState, onPublicModelIDChange, onCopyConfig, onCopyPowerShell, canTest, testPending, testResult, onTest }: {
  dataPlaneURL: string | undefined;
  publicModelID: string;
  copyState: CopyState;
  onPublicModelIDChange: (value: string) => void;
  onCopyConfig: (value: string) => void;
  onCopyPowerShell: (value: string) => void;
  canTest: boolean;
  testPending: boolean;
  testResult: LocalResponsesTestResult | null;
  onTest: (kind: "text" | "function") => void;
}) {
  const template = useMemo<CodexConfigTemplate | null>(() => {
    if (!dataPlaneURL) return null;
    try {
      return buildCodexConfig({ dataPlaneURL, publicModelID });
    } catch {
      return null;
    }
  }, [dataPlaneURL, publicModelID]);
  const running = Boolean(dataPlaneURL);

  return (
    <section className="codex-setup" aria-labelledby="codex-setup-title">
      <div className="key-panel-heading">
        <div>
          <h2 id="codex-setup-title">Codex</h2>
          <p>复制模板到用户级 <code>~/.codex/config.toml</code>；不会自动写入配置文件或系统环境变量。</p>
        </div>
        <span className={running ? "service-state is-enabled" : "service-state"}>{running ? "可生成配置" : "需先启动网关"}</span>
      </div>
      <label className="field-label" htmlFor="codex-model-id">Public Model ID</label>
      <input id="codex-model-id" className="text-input" value={publicModelID} onChange={(event) => onPublicModelIDChange(event.target.value)} placeholder="provider-slug/upstream-model-id" maxLength={304} disabled={!running} />
      {!running ? <p className="config-hint" role="status">启动网关后将显示可复制的本地 Responses 配置。</p> : null}
      {running && !template ? <p className="config-hint" role="status">请填写一个已启用的 Public Model ID。</p> : null}
      {template ? (
        <>
          <div className="codex-config-grid">
          <section className="config-card" aria-labelledby="codex-config-title">
            <div className="config-card-heading"><h3 id="codex-config-title">config.toml</h3><button type="button" className="button button-secondary" onClick={() => onCopyConfig(template.configToml)}>{copyState === "copied" ? "已复制" : copyState === "failed" ? "复制失败" : "复制配置"}</button></div>
            <pre><code>{template.configToml}</code></pre>
          </section>
          <section className="config-card" aria-labelledby="codex-env-title">
            <div className="config-card-heading"><h3 id="codex-env-title">当前 PowerShell 会话</h3><button type="button" className="button button-secondary" onClick={() => onCopyPowerShell(template.powerShellSession)}>复制命令</button></div>
            <pre><code>{template.powerShellSession}</code></pre>
            <p className="config-hint">将占位符替换为刚创建并一次性保存的 Local Key；关闭终端后该变量自动失效。</p>
          </section>
        </div>
        <section className="codex-test-panel" aria-labelledby="codex-test-title">
          <div><h3 id="codex-test-title">本地诊断</h3><p>只会使用本页刚创建的一次性 Key，直接请求回环网关；不会保存 Key、Prompt、Function 参数或响应正文。</p></div>
          <div className="button-group"><button type="button" className="button button-secondary" disabled={!canTest || testPending} onClick={() => onTest("text")}>{testPending ? "正在测试" : "测试文本"}</button><button type="button" className="button button-secondary" disabled={!canTest || testPending} onClick={() => onTest("function")}>{testPending ? "正在测试" : "测试 Function"}</button></div>
          {!canTest ? <p className="config-hint">请先在本页创建并一次性保存新的 Local Key，才能运行诊断。</p> : null}
          {testResult ? <p className={testResult.success ? "test-result is-success" : "test-result"} role="status">{testResult.message}（HTTP {testResult.http_status}）</p> : null}
          </section>
        </>
      ) : null}
    </section>
  );
}
