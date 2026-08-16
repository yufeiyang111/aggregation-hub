import { useCallback, useEffect, useRef, useState } from "react";
import { desktopApi, type RequestListQuery, type RequestMetadata, type RequestPage, type UsageQuery, type UsageSummary, type UsageTimeSeries } from "../../lib/desktop-api";

interface LoadState<T> { data: T | null; loading: boolean; error: string | null; }
const loadingState = <T,>(): LoadState<T> => ({ data: null, loading: true, error: null });

export function useRequestList(query: RequestListQuery) {
  const [state, setState] = useState<LoadState<RequestPage>>(loadingState);
  const sequence = useRef(0);
  const refresh = useCallback(async () => {
    const current = ++sequence.current;
    setState((previous) => ({ ...previous, loading: true, error: null }));
    try { const data = await desktopApi.listRequests(query); if (current === sequence.current) setState({ data, loading: false, error: null }); }
    catch { if (current === sequence.current) setState((previous) => ({ ...previous, loading: false, error: "读取请求记录失败" })); }
  }, [query]);
  useEffect(() => { void refresh(); return () => { sequence.current += 1; }; }, [refresh]);
  return { ...state, refresh };
}

export function useRequestDetail(id: string | null) {
  const [state, setState] = useState<LoadState<RequestMetadata>>({ data: null, loading: false, error: null });
  useEffect(() => {
    if (!id) { setState({ data: null, loading: false, error: null }); return; }
    let active = true; setState({ data: null, loading: true, error: null });
    void desktopApi.getRequest(id).then((data) => { if (active) setState({ data, loading: false, error: null }); }).catch(() => { if (active) setState({ data: null, loading: false, error: "读取请求详情失败" }); });
    return () => { active = false; };
  }, [id]);
  return state;
}

export function useUsage(query: UsageQuery) {
  const [state, setState] = useState<LoadState<{ summary: UsageSummary; series: UsageTimeSeries }>>(loadingState);
  const sequence = useRef(0);
  const refresh = useCallback(async () => {
    const current = ++sequence.current; setState((previous) => ({ ...previous, loading: true, error: null }));
    try { const [summary, series] = await Promise.all([desktopApi.usageSummary(query), desktopApi.usageTimeSeries(query)]); if (current === sequence.current) setState({ data: { summary, series }, loading: false, error: null }); }
    catch { if (current === sequence.current) setState((previous) => ({ ...previous, loading: false, error: "读取用量数据失败" })); }
  }, [query]);
  useEffect(() => { void refresh(); return () => { sequence.current += 1; }; }, [refresh]);
  return { ...state, refresh };
}

export function formatTokens(value: number | null | undefined): string { return value == null ? "—" : new Intl.NumberFormat("zh-CN", { notation: value >= 10_000 ? "compact" : "standard", maximumFractionDigits: 1 }).format(value); }
export function formatRate(value: number | null): string { return value == null ? "—" : `${(value / 100).toFixed(value % 100 === 0 ? 0 : 2)}%`; }
export function recentUsageQuery(): UsageQuery { const now = new Date(); const to = now.toISOString(); const from = new Date(now.getTime() - 6 * 86_400_000).toISOString(); return { from_utc: from, to_utc: to }; }
