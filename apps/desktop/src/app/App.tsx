import { useCallback, useEffect, useMemo, useState } from "react";
import { desktopApi, type DashboardSnapshot, type OneTimeLocalKey, type RuntimeState } from "../lib/desktop-api";

const runtimeLabels: Record<RuntimeState, string> = {
  stopped: "已停止",
  starting: "正在启动",
  running: "运行中",
  failed: "启动失败",
};

function safeMessage(error: unknown, fallback: string) {
  return error instanceof Error && error.message.trim() !== "" ? error.message : fallback;
}

export function App() {
  const [dashboard, setDashboard] = useState<DashboardSnapshot | null>(null);
  const [loading, setLoading] = useState(true);
  const [actionPending, setActionPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [keyName, setKeyName] = useState("Claude Code");
  const [oneTimeKey, setOneTimeKey] = useState<OneTimeLocalKey | null>(null);
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">("idle");

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const nextDashboard = await desktopApi.dashboard();
      setDashboard(nextDashboard);
      setError(null);
    } catch (reason) {
      setError(safeMessage(reason, "无法读取本地网关状态"));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const runtime = dashboard?.runtime;
  const isRunning = runtime?.state === "running";
  const statusLabel = runtime ? runtimeLabels[runtime.state] : "正在读取";
  const statusMessage = useMemo(() => {
    if (error) return error;
    if (loading && !runtime) return "正在连接本地网关";
    if (runtime?.last_error) return runtime.last_error;
    if (runtime?.state === "running") return "本地网关已就绪";
    if (runtime?.state === "starting") return "Core 正在启动，请稍候刷新";
    return "本地网关尚未运行";
  }, [error, loading, runtime]);

  const handleStartOrRestart = async () => {
    setActionPending(true);
    try {
      if (runtime?.state === "stopped" || runtime?.state === "failed") {
        await desktopApi.start();
      } else {
        await desktopApi.restart();
      }
      await refresh();
    } catch (reason) {
      setError(safeMessage(reason, "无法启动本地网关"));
    } finally {
      setActionPending(false);
    }
  };

  const handleCreateKey = async () => {
    const name = keyName.trim();
    if (name === "") {
      setError("请填写 Local Key 名称");
      return;
    }
    setActionPending(true);
    try {
      const createdKey = await desktopApi.createLocalKey(name);
      setOneTimeKey(createdKey);
      setCopyState("idle");
      setError(null);
    } catch (reason) {
      setError(safeMessage(reason, "创建 Local Key 失败"));
    } finally {
      setActionPending(false);
    }
  };

  const handleCopy = async () => {
    if (!oneTimeKey || !navigator.clipboard) {
      setCopyState("failed");
      return;
    }
    try {
      await navigator.clipboard.writeText(oneTimeKey.key);
      setCopyState("copied");
    } catch {
      setCopyState("failed");
    }
  };

  return (
    <main className="app-shell">
      <header className="app-header">
        <div>
          <p className="eyebrow">LOCAL LLM GATEWAY</p>
          <h1>Aggregation Hub</h1>
        </div>
        <span className={`status-chip status-${runtime?.state ?? "loading"}`} aria-label={`当前状态：${statusLabel}`}>
          {statusLabel}
        </span>
      </header>

      <section className="status-card" aria-labelledby="runtime-heading">
        <div>
          <p className="eyebrow">RUNTIME</p>
          <h2 id="runtime-heading">运行状态</h2>
          <p className="status-message" role="status">{statusMessage}</p>
          <dl className="runtime-details">
            <div>
              <dt>Data Plane</dt>
              <dd>{runtime?.data_plane_url ?? "启动后显示"}</dd>
            </div>
            <div>
              <dt>Core 版本</dt>
              <dd>{runtime?.version ?? "—"}</dd>
            </div>
          </dl>
        </div>
        <div className="action-group">
          <button type="button" className="secondary-button" onClick={() => void refresh()} disabled={loading || actionPending}>
            刷新状态
          </button>
          <button type="button" className="primary-button" onClick={() => void handleStartOrRestart()} disabled={actionPending}>
            {runtime?.state === "stopped" || runtime?.state === "failed" ? "启动网关" : "重启网关"}
          </button>
        </div>
      </section>

      {error ? <p className="error-banner" role="alert">{error}</p> : null}

      <section className="content-grid" aria-label="本地网关管理">
        <section className="panel" aria-labelledby="key-heading">
          <p className="eyebrow">CLIENT ACCESS</p>
          <h2 id="key-heading">Local Key</h2>
          <p className="panel-description">创建后仅在当前窗口显示一次。复制后填写到 Claude Code、Codex 或其他编程 Agent 的 API Key 配置中。</p>
          <label className="field-label" htmlFor="local-key-name">名称</label>
          <input id="local-key-name" className="text-input" value={keyName} maxLength={128} onChange={(event) => setKeyName(event.target.value)} disabled={!isRunning || actionPending} />
          <button type="button" className="primary-button full-width" onClick={() => void handleCreateKey()} disabled={!isRunning || actionPending}>
            生成 Local Key
          </button>
          {!isRunning ? <p className="hint">请先等待本地网关进入“运行中”状态。</p> : null}
          {oneTimeKey ? (
            <div className="one-time-key" aria-live="polite">
              <p className="warning-label">仅显示一次</p>
              <code>{oneTimeKey.key}</code>
              <button type="button" className="secondary-button" onClick={() => void handleCopy()}>
                {copyState === "copied" ? "已复制" : "复制 Local Key"}
              </button>
              {copyState === "failed" ? <p className="hint">复制失败，请手动复制后妥善保存。</p> : null}
            </div>
          ) : null}
        </section>

        <section className="panel" aria-labelledby="provider-heading">
          <p className="eyebrow">PROVIDERS</p>
          <h2 id="provider-heading">已接入套餐</h2>
          {loading && !dashboard ? <p className="panel-description">正在读取 Provider 列表。</p> : null}
          {dashboard && dashboard.providers.length === 0 ? <p className="panel-description">暂未添加 Provider。后续可在此处接入不同厂商的套餐和模型。</p> : null}
          {dashboard?.providers.length ? (
            <ul className="provider-list">
              {dashboard.providers.map((provider) => (
                <li key={provider.id} className="provider-item">
                  <div>
                    <strong>{provider.name}</strong>
                    <span>{provider.slug} · {provider.adapter_type}</span>
                  </div>
                  <span className={provider.enabled ? "provider-state enabled" : "provider-state"}>{provider.enabled ? "已启用" : "未启用"}</span>
                </li>
              ))}
            </ul>
          ) : null}
        </section>
      </section>
    </main>
  );
}