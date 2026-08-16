import { useCallback, useEffect, useRef, useState } from "react";
import { desktopApi, type DiagnosticsExport, type DiagnosticsSummary } from "../../lib/desktop-api";

interface DiagnosticsState {
  summary: DiagnosticsSummary | null;
  loading: boolean;
  exporting: boolean;
  openingDirectory: boolean;
  exported: DiagnosticsExport | null;
  directoryOpened: boolean;
  error: string | null;
}

const initialState: DiagnosticsState = {
  summary: null,
  loading: true,
  exporting: false,
  openingDirectory: false,
  exported: null,
  directoryOpened: false,
  error: null,
};

function toSafeErrorMessage(fallback: string) {
  return fallback;
}

export function useDiagnostics() {
  const [state, setState] = useState<DiagnosticsState>(initialState);
  const requestSequence = useRef(0);

  const refresh = useCallback(async () => {
    const sequence = ++requestSequence.current;
    setState((current) => ({ ...current, loading: true, error: null }));

    try {
      const summary = await desktopApi.diagnosticsSummary();
      if (sequence !== requestSequence.current) {
        return;
      }

      setState((current) => ({ ...current, summary }));
    } catch {
      if (sequence !== requestSequence.current) {
        return;
      }

      setState((current) => ({ ...current, error: "读取诊断摘要失败" }));
    } finally {
      if (sequence === requestSequence.current) {
        setState((current) => ({ ...current, loading: false }));
      }
    }
  }, []);

  useEffect(() => {
    void refresh();

    return () => {
      requestSequence.current += 1;
    };
  }, [refresh]);

  const exportArchive = useCallback(async () => {
    setState((current) => ({
      ...current,
      exporting: true,
      directoryOpened: false,
      error: null,
    }));

    try {
      const exported = await desktopApi.exportDiagnostics();
      setState((current) => ({ ...current, exported }));
    } catch {
      setState((current) => ({
        ...current,
        error: toSafeErrorMessage("导出诊断包失败"),
      }));
    } finally {
      setState((current) => ({ ...current, exporting: false }));
    }
  }, []);

  const openDirectory = useCallback(async () => {
    setState((current) => ({
      ...current,
      openingDirectory: true,
      directoryOpened: false,
      error: null,
    }));

    try {
      await desktopApi.openDiagnosticsDirectory();
      setState((current) => ({ ...current, directoryOpened: true }));
    } catch {
      setState((current) => ({
        ...current,
        error: toSafeErrorMessage("打开诊断文件夹失败"),
      }));
    } finally {
      setState((current) => ({ ...current, openingDirectory: false }));
    }
  }, []);

  return {
    ...state,
    refresh,
    exportArchive,
    openDirectory,
  };
}
