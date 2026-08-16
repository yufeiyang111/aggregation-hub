import { useEffect, useRef } from "react";

import type { ProviderHealthPage, ProviderSummary } from "../lib/desktop-api";

function formatHealthTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "时间未知" : new Intl.DateTimeFormat("zh-CN", { dateStyle: "short", timeStyle: "short" }).format(date);
}

function healthStatusLabel(status: ProviderHealthPage["data"][number]["status"]) {
  if (status === "succeeded") return "通过";
  if (status === "skipped") return "未支持";
  return "失败";
}

function healthCheckTypeLabel(checkType: ProviderHealthPage["data"][number]["check_type"]) {
  if (checkType === "models") return "模型列表";
  if (checkType === "connection") return "连接";
  return "完成请求";
}

export function ProviderHealthDialog({ provider, page, loading, error, onClose }: { provider: ProviderSummary | null; page: ProviderHealthPage | null; loading: boolean; error: string | null; onClose: () => void }) {
  const closeButtonRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!provider) return undefined;
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    closeButtonRef.current?.focus();
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("keydown", handleKeyDown);
      previous?.focus();
    };
  }, [onClose, provider]);

  if (!provider) return null;
  return (
    <div className="dialog-backdrop" role="presentation">
      <section className="confirm-dialog provider-health-dialog" role="dialog" aria-modal="true" aria-labelledby="provider-health-title">
        <div className="dialog-heading">
          <div><h2 id="provider-health-title">测试记录</h2><p>{provider.name} 最近七天的显式测试结果，不包含请求正文或凭据。</p></div>
          <button ref={closeButtonRef} type="button" className="dialog-close" aria-label="关闭测试记录" onClick={onClose}>×</button>
        </div>
        {loading ? <p className="inline-status" role="status">正在读取测试记录…</p> : null}
        {!loading && error ? <p className="form-error" role="alert">{error}</p> : null}
        {!loading && !error && page?.data.length === 0 ? <p className="inline-status">还没有测试记录。</p> : null}
        {!loading && !error && page && page.data.length > 0 ? <ul className="health-record-list" aria-label="服务测试记录">{page.data.map((item) => <li key={item.id}><div><strong>{healthStatusLabel(item.status)}</strong><span>{healthCheckTypeLabel(item.check_type)}</span></div><div><span>{item.latency_ms === null ? "—" : `${item.latency_ms} ms`}</span><time dateTime={item.checked_at}>{formatHealthTime(item.checked_at)}</time>{item.error_code && item.error_code !== "ok" ? <small>{item.error_code}</small> : null}</div></li>)}</ul> : null}
        <div className="button-group"><button type="button" className="button button-secondary" onClick={onClose}>关闭</button></div>
      </section>
    </div>
  );
}
