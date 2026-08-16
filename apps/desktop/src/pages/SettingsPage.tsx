import { type FormEvent, useEffect, useState } from "react";
import type { BackupRecord, RuntimeSettings, RuntimeSnapshot } from "../lib/desktop-api";
import { useMaintenance } from "../features/settings/useMaintenance";

type Props = {
  runtime: RuntimeSnapshot | undefined;
  loading: boolean;
  actionPending: boolean;
  onRefreshRuntime: () => void;
  onRestartRuntime: () => void;
};

type SettingsDraft = {
  listenPort: string;
  requestTimeoutMS: string;
  requestRetentionDays: string;
  version: number;
};

function draftFromSettings(value: RuntimeSettings): SettingsDraft {
  return {
    listenPort: value.listen_port.toString(),
    requestTimeoutMS: value.request_timeout_ms.toString(),
    requestRetentionDays: value.request_retention_days.toString(),
    version: value.version,
  };
}

function runtimeLabel(runtime: RuntimeSnapshot | undefined) {
  if (!runtime) return "正在读取";
  return { stopped: "已停止", starting: "正在启动", running: "运行中", failed: "启动失败" }[runtime.state];
}

function formatBackupTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "时间不可用" : date.toLocaleString("zh-CN", { hour12: false });
}

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / (1024 * 1024)).toFixed(1)} MB`;
}

function BackupRestoreDialog({ backup, pending, onCancel, onConfirm }: { backup: BackupRecord | null; pending: boolean; onCancel: () => void; onConfirm: () => void }) {
  if (!backup) return null;
  return (
    <div className="dialog-backdrop" role="presentation">
      <section className="confirm-dialog" role="dialog" aria-modal="true" aria-labelledby="restore-title">
        <h2 id="restore-title">确认恢复备份</h2>
        <p>会先为当前数据库创建安全备份。恢复会在网关重启前应用，期间本地请求会暂时不可用。</p>
        <code>{backup.id}</code>
        <div className="button-group">
          <button type="button" className="button button-secondary" onClick={onCancel} disabled={pending}>取消</button>
          <button type="button" className="button button-danger" onClick={onConfirm} disabled={pending} autoFocus>{pending ? "正在计划恢复" : "确认恢复"}</button>
        </div>
      </section>
    </div>
  );
}

export function SettingsPage({ runtime, loading, actionPending, onRefreshRuntime, onRestartRuntime }: Props) {
  const { settings, backups, loading: maintenanceLoading, saving, creatingBackup, pruning, restoringID, error, notice, refresh, saveSettings, createBackup, pruneRequests, restore } = useMaintenance();
  const [draft, setDraft] = useState<SettingsDraft | null>(null);
  const [formError, setFormError] = useState<string | null>(null);
  const [restoreTarget, setRestoreTarget] = useState<BackupRecord | null>(null);

  useEffect(() => {
    if (settings) setDraft(draftFromSettings(settings));
  }, [settings]);

  const submitSettings = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!draft) return;
    const listenPort = Number(draft.listenPort);
    const requestTimeoutMS = Number(draft.requestTimeoutMS);
    const requestRetentionDays = Number(draft.requestRetentionDays);
    if (!Number.isInteger(listenPort) || listenPort < 1024 || listenPort > 65535 || !Number.isInteger(requestTimeoutMS) || requestTimeoutMS < 1000 || requestTimeoutMS > 3600000 || !Number.isInteger(requestRetentionDays) || requestRetentionDays < 1 || requestRetentionDays > 3650) {
      setFormError("请填写有效的端口、超时和保留天数。");
      return;
    }
    setFormError(null);
    void saveSettings({ listen_port: listenPort, request_timeout_ms: requestTimeoutMS, request_retention_days: requestRetentionDays, version: draft.version });
  };

  const confirmRestore = () => {
    if (!restoreTarget) return;
    void restore(restoreTarget.id);
    setRestoreTarget(null);
  };

  const shouldStart = runtime?.state === "stopped" || runtime?.state === "failed";
  const pending = saving || creatingBackup || pruning || restoringID !== null;

  if (maintenanceLoading && !settings) {
    return <section className="empty-state" aria-busy="true"><h2>设置</h2><p>正在读取本地设置…</p></section>;
  }

  return (
    <section className="page-section" aria-labelledby="settings-title">
      <div className="page-heading">
        <div><h1 id="settings-title">设置</h1><p>网关只监听本机回环地址；凭据不保存在此页面或 SQLite 中。</p></div>
        <button type="button" className="button button-secondary" onClick={() => void refresh()} disabled={maintenanceLoading || pending}>刷新</button>
      </div>
      {error ? <p className="error-banner" role="alert">{error}</p> : null}
      {notice ? <p className="maintenance-notice" role="status">{notice}</p> : null}

      <section className="settings-list" aria-label="网关运行状态">
        <div className="settings-row"><div><h2>运行状态</h2><p>{runtimeLabel(runtime)} · {runtime?.version ?? "—"}</p></div><div className="button-group"><button type="button" className="button button-secondary" onClick={onRefreshRuntime} disabled={loading || actionPending}>刷新状态</button><button type="button" className="button button-primary" onClick={onRestartRuntime} disabled={!runtime || actionPending}>{shouldStart ? "启动网关" : "重启网关"}</button></div></div>
      </section>

      <form className="settings-form" onSubmit={submitSettings}>
        <div className="settings-card"><h2>连接</h2><p>地址固定为 127.0.0.1，修改端口后需重启网关。</p><label className="form-field"><span>端口</span><input className="text-input" inputMode="numeric" value={draft?.listenPort ?? ""} onChange={(event) => setDraft((current) => current ? { ...current, listenPort: event.target.value } : current)} disabled={!draft || pending} /></label><label className="form-field"><span>请求超时（毫秒）</span><input className="text-input" inputMode="numeric" value={draft?.requestTimeoutMS ?? ""} onChange={(event) => setDraft((current) => current ? { ...current, requestTimeoutMS: event.target.value } : current)} disabled={!draft || pending} /></label></div>
        <div className="settings-card"><h2>保留</h2><p>请求正文不会保存；历史请求元数据到期后可按天数清理，日用量汇总会保留。</p><label className="form-field"><span>请求记录保留天数</span><input className="text-input" inputMode="numeric" value={draft?.requestRetentionDays ?? ""} onChange={(event) => setDraft((current) => current ? { ...current, requestRetentionDays: event.target.value } : current)} disabled={!draft || pending} /></label><button type="button" className="button button-secondary" onClick={() => void pruneRequests()} disabled={pending}>{pruning ? "正在清理…" : "清理过期记录"}</button></div>
        {formError ? <p className="form-error" role="alert">{formError}</p> : null}
        <div className="settings-actions"><button type="submit" className="button button-primary" disabled={!draft || pending}>{saving ? "正在保存…" : "保存设置"}</button></div>
      </form>

      <section className="data-panel maintenance-backups" aria-labelledby="backup-title">
        <div className="page-heading"><div><h2 id="backup-title">备份</h2><p>备份只写入应用固定目录，最多保留最近五份；列表不暴露本机绝对路径。</p></div><button type="button" className="button button-primary" onClick={() => void createBackup()} disabled={pending}>{creatingBackup ? "正在备份…" : "创建备份"}</button></div>
        {backups.length === 0 ? <p className="privacy-note">还没有备份。</p> : <ul className="backup-list">{backups.map((backup) => <li key={backup.id}><div><strong>{formatBackupTime(backup.created_at)}</strong><span>{formatBytes(backup.size_bytes)}</span></div><button type="button" className="button button-secondary" onClick={() => setRestoreTarget(backup)} disabled={pending}>恢复</button></li>)}</ul>}
      </section>
      <BackupRestoreDialog backup={restoreTarget} pending={restoringID !== null} onCancel={() => setRestoreTarget(null)} onConfirm={confirmRestore} />
    </section>
  );
}