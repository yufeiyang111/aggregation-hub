import { useDiagnostics } from "../features/diagnostics/useDiagnostics";

export function DiagnosticsPage() {
  const {
    summary,
    loading,
    exporting,
    openingDirectory,
    exported,
    directoryOpened,
    error,
    refresh,
    exportArchive,
    openDirectory,
  } = useDiagnostics();

  if (loading) {
    return (
      <section className="empty-state" aria-busy="true">
        <h2>诊断</h2>
        <p>正在读取诊断摘要…</p>
      </section>
    );
  }

  return (
    <section className="page-section" aria-labelledby="diagnostics-title">
      <div className="page-heading">
        <div>
          <h2 id="diagnostics-title">诊断</h2>
          <p>导出的内容已脱敏，不包含请求正文、凭据或完整路径。</p>
        </div>
        <button type="button" className="button button-secondary" onClick={() => void refresh()}>
          刷新
        </button>
      </div>

      {error ? (
        <p className="error-banner" role="alert">
          {error}
        </p>
      ) : null}

      <div className="settings-grid">
        <div className="settings-card">
          <strong>安全错误</strong>
          <span>{summary ? `最近安全错误 ${summary.recent_error_count} 条` : "摘要不可用"}</span>
        </div>
        <div className="settings-card">
          <strong>诊断格式</strong>
          <span>{summary?.format_version ?? "—"}</span>
        </div>
      </div>

      <div className="settings-actions">
        <button
          type="button"
          className="button button-primary"
          disabled={!summary?.export_available || exporting}
          onClick={() => void exportArchive()}
        >
          {exporting ? "正在导出…" : "导出诊断包"}
        </button>
        <button
          type="button"
          className="button button-secondary"
          disabled={openingDirectory}
          onClick={() => void openDirectory()}
        >
          {openingDirectory ? "正在打开…" : "打开诊断文件夹"}
        </button>
      </div>

      {exported ? <p role="status">已导出 {exported.file_name}</p> : null}
      {directoryOpened ? <p role="status">已打开诊断文件夹</p> : null}
    </section>
  );
}
