import { useCallback, useEffect, useRef, useState } from "react";
import { desktopApi, type BackupRecord, type RetentionResult, type RuntimeSettings } from "../../lib/desktop-api";

type MaintenanceState = {
  settings: RuntimeSettings | null;
  backups: BackupRecord[];
  loading: boolean;
  saving: boolean;
  creatingBackup: boolean;
  pruning: boolean;
  restoringID: string | null;
  error: string | null;
  notice: string | null;
};

const initialState: MaintenanceState = {
  settings: null,
  backups: [],
  loading: true,
  saving: false,
  creatingBackup: false,
  pruning: false,
  restoringID: null,
  error: null,
  notice: null,
};

function safeError(_error: unknown, fallback: string) {
  return fallback;
}

export function useMaintenance() {
  const [state, setState] = useState<MaintenanceState>(initialState);
  const requestSequence = useRef(0);

  const refresh = useCallback(async () => {
    const sequence = ++requestSequence.current;
    setState((current) => ({ ...current, loading: true, error: null }));
    try {
      const [settings, page] = await Promise.all([desktopApi.runtimeSettings(), desktopApi.listBackups()]);
      if (sequence !== requestSequence.current) return;
      setState((current) => ({ ...current, settings, backups: page.data }));
    } catch (error) {
      if (sequence !== requestSequence.current) return;
      setState((current) => ({ ...current, error: safeError(error, "读取设置和备份失败") }));
    } finally {
      if (sequence === requestSequence.current) setState((current) => ({ ...current, loading: false }));
    }
  }, []);

  useEffect(() => {
    void refresh();
    return () => { requestSequence.current += 1; };
  }, [refresh]);

  const saveSettings = useCallback(async (input: RuntimeSettings) => {
    setState((current) => ({ ...current, saving: true, error: null, notice: null }));
    try {
      const result = await desktopApi.updateRuntimeSettings(input);
      setState((current) => ({
        ...current,
        settings: result.settings,
        notice: result.restart_required ? "设置已保存；端口和超时将在重启网关后生效。" : "设置已保存。",
      }));
    } catch (error) {
      setState((current) => ({ ...current, error: safeError(error, "保存设置失败") }));
    } finally {
      setState((current) => ({ ...current, saving: false }));
    }
  }, []);

  const createBackup = useCallback(async () => {
    setState((current) => ({ ...current, creatingBackup: true, error: null, notice: null }));
    try {
      const created = await desktopApi.createBackup();
      const page = await desktopApi.listBackups();
      setState((current) => ({ ...current, backups: page.data, notice: `已创建备份 ${created.id}` }));
    } catch (error) {
      setState((current) => ({ ...current, error: safeError(error, "创建备份失败") }));
    } finally {
      setState((current) => ({ ...current, creatingBackup: false }));
    }
  }, []);

  const pruneRequests = useCallback(async (): Promise<RetentionResult | null> => {
    setState((current) => ({ ...current, pruning: true, error: null, notice: null }));
    try {
      const result = await desktopApi.pruneRequests();
      setState((current) => ({ ...current, notice: result.deleted_requests > 0 ? `已清理 ${result.deleted_requests} 条过期请求记录。` : "没有需要清理的过期请求记录。" }));
      return result;
    } catch (error) {
      setState((current) => ({ ...current, error: safeError(error, "清理请求记录失败") }));
      return null;
    } finally {
      setState((current) => ({ ...current, pruning: false }));
    }
  }, []);

  const restore = useCallback(async (backupID: string) => {
    setState((current) => ({ ...current, restoringID: backupID, error: null, notice: null }));
    try {
      const result = await desktopApi.scheduleRestore(backupID);
      setState((current) => ({ ...current, notice: `恢复已计划；已先创建安全备份 ${result.safety_backup.id}，重启网关后应用。` }));
    } catch (error) {
      setState((current) => ({ ...current, error: safeError(error, "计划恢复失败") }));
    } finally {
      setState((current) => ({ ...current, restoringID: null }));
    }
  }, []);

  return { ...state, refresh, saveSettings, createBackup, pruneRequests, restore };
}