import { useMemo } from "react";
import { EmptyState } from "../components/EmptyState";
import { formatRate, formatTokens, recentUsageQuery, useUsage } from "../features/observability/useObservability";

export function DashboardPage() {
  const query = useMemo(recentUsageQuery, []);
  const { data, loading, error, refresh } = useUsage(query);
  return <section className="page-section" aria-labelledby="overview-title"><div className="page-heading"><div><p className="eyebrow">概览</p><h1 id="overview-title">最近七天</h1><p>只显示 Token 与请求统计，不包含费用或请求正文。</p></div><button className="button button-secondary" type="button" onClick={() => void refresh()} disabled={loading}>刷新</button></div>{loading ? <p role="status">正在读取概览…</p> : error ? <section className="inline-error" role="alert"><p>{error}</p><button className="button button-secondary" type="button" onClick={() => void refresh()}>重试</button></section> : !data || data.summary.request_count === 0 ? <EmptyState title="暂无请求数据" description="网关完成请求后，这里会显示 Token、缓存命中率和输出量。" /> : <><div className="metric-grid"><Metric label="请求数" value={formatTokens(data.summary.request_count)} /><Metric label="输出 Token" value={formatTokens(data.summary.output_tokens)} /><Metric label="缓存命中率" value={formatRate(data.summary.cache_hit_rate_basis_points)} /><Metric label="失败请求" value={formatTokens(data.summary.failed_count)} /></div><UsageTable points={data.series.data} /></>}</section>;
}

export function UsageTable({ points }: { points: Array<{ date_utc: string; request_count: number; output_tokens: number; cache_hit_rate_basis_points: number | null }> }) {
  return <section className="data-panel"><h2>按日趋势</h2><div className="table-scroll"><table><thead><tr><th>UTC 日期</th><th>请求</th><th>输出 Token</th><th>缓存命中率</th></tr></thead><tbody>{points.map((point) => <tr key={point.date_utc}><td>{point.date_utc}</td><td>{formatTokens(point.request_count)}</td><td>{formatTokens(point.output_tokens)}</td><td>{formatRate(point.cache_hit_rate_basis_points)}</td></tr>)}</tbody></table></div></section>;
}
function Metric({ label, value }: { label: string; value: string }) { return <article className="metric-card"><span>{label}</span><strong>{value}</strong></article>; }
