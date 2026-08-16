import { useEffect, useRef } from "react";
import { formatTokens, useRequestDetail } from "../features/observability/useObservability";

export function RequestDetailDrawer({ requestID, onClose }: { requestID: string | null; onClose: () => void }) {
  const closeButton = useRef<HTMLButtonElement>(null);
  const { data, loading, error } = useRequestDetail(requestID);
  useEffect(() => { if (requestID) closeButton.current?.focus(); }, [requestID]);
  useEffect(() => { const close = (event: KeyboardEvent) => { if (event.key === "Escape") onClose(); }; window.addEventListener("keydown", close); return () => window.removeEventListener("keydown", close); }, [onClose]);
  if (!requestID) return null;
  return <aside className="request-drawer" role="dialog" aria-modal="true" aria-labelledby="request-detail-title"><div className="drawer-heading"><div><p className="eyebrow">请求详情</p><h2 id="request-detail-title">{requestID}</h2></div><button ref={closeButton} className="button button-secondary" type="button" onClick={onClose}>关闭</button></div>{loading ? <p role="status">正在读取请求详情…</p> : error ? <p role="alert">{error}</p> : data ? <dl className="detail-list"><div><dt>状态</dt><dd>{data.status}</dd></div><div><dt>服务 / 模型</dt><dd>{data.provider_slug} / {data.public_model_id}</dd></div><div><dt>协议</dt><dd>{data.source_protocol}</dd></div><div><dt>输入 / 输出</dt><dd>{formatTokens(data.input_tokens)} / {formatTokens(data.output_tokens)}</dd></div><div><dt>耗时</dt><dd>{data.duration_ms == null ? "—" : `${data.duration_ms} ms`}</dd></div><div><dt>错误类别</dt><dd>{data.error_code ?? "—"}</dd></div></dl> : null}<p className="privacy-note">默认不保存 Prompt、回复正文、请求 Header 或 Tool 参数。</p></aside>;
}
