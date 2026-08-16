import { type ChangeEvent, type FormEvent, type MouseEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { CodexSetupPage } from "../features/connections/CodexSetupPage";
import { DiagnosticsPage } from "../pages/DiagnosticsPage";
import { desktopApi, type CreateManualModelInput, type CreateProviderInput, type DashboardSnapshot, type ModelCapabilityOverride, type ModelLimitOverride, type ModelListQuery, type ModelPage as ModelPageData, type ModelSummary, type OneTimeLocalKey, type ProviderSummary, type RuntimeSnapshot, type RuntimeState, type UpdateProviderInput } from "../lib/desktop-api";
import { EmptyState } from "../components/EmptyState";
import { StatusDot } from "../components/StatusDot";

type LocalResponsesTestResult = import("../lib/desktop-api").LocalResponsesTestResult;

type PageID = "services" | "models" | "clients" | "logs" | "settings";
type CopyState = "idle" | "copied" | "failed";

const navigationItems: ReadonlyArray<{ id: PageID; label: string }> = [
  { id: "services", label: "服务" },
  { id: "models", label: "模型" },
  { id: "clients", label: "客户端配置" },
  { id: "logs", label: "日志" },
  { id: "settings", label: "设置" },
];

const runtimeLabels: Record<RuntimeState, string> = {
  stopped: "已停止",
  starting: "正在启动",
  running: "运行中",
  failed: "启动失败",
};

const modelStatusLabels: Record<ModelSummary["lifecycle_status"], string> = {
  available: "可用",
  degraded: "降级",
  missing_upstream: "上游缺失",
  disabled: "已禁用",
};

const modelCapabilityLabels = {
  streaming: "流式",
  tools: "工具",
  parallel_tools: "并行工具",
  reasoning: "推理",
  thinking: "思考",
  vision: "视觉",
} as const;

type ModelCapability = keyof typeof modelCapabilityLabels;
type ModelEnabledFilter = "all" | "enabled" | "disabled";

type ModelCapabilityValues = Record<ModelCapability, boolean>;

const modelCapabilityOverrideKeys: Record<ModelCapability, keyof ModelCapabilityOverride> = {
  streaming: "supports_streaming",
  tools: "supports_tools",
  parallel_tools: "supports_parallel_tools",
  reasoning: "supports_reasoning",
  thinking: "supports_thinking",
  vision: "supports_vision",
};

function capabilityValuesFromModel(model: ModelSummary | null): ModelCapabilityValues {
  return {
    streaming: model?.capabilities.streaming ?? false,
    tools: model?.capabilities.tools ?? false,
    parallel_tools: model?.capabilities.parallel_tools ?? false,
    reasoning: model?.capabilities.reasoning ?? false,
    thinking: model?.capabilities.thinking ?? false,
    vision: model?.capabilities.vision ?? false,
  };
}

function capabilityOverrideFromValues(values: ModelCapabilityValues): ModelCapabilityOverride {
  return (Object.keys(modelCapabilityOverrideKeys) as ModelCapability[]).reduce<ModelCapabilityOverride>((result, capability) => {
    result[modelCapabilityOverrideKeys[capability]] = values[capability];
    return result;
  }, {});
}
type PendingModelChange = { model: ModelSummary; enabled: boolean };
type PendingModelDelete = { model: ModelSummary };
type PendingProviderChange = { provider: ProviderSummary; enabled: boolean };
type PendingProviderDelete = { provider: ProviderSummary };
type ProviderFeedback = { providerID: string; message: string; success: boolean } | null;

function safeMessage(error: unknown, fallback: string) {
  return error instanceof Error && error.message.trim() !== "" ? error.message : fallback;
}

function initialOf(value: string) {
  return value.trim().slice(0, 1).toUpperCase() || "服";
}


function ProviderRow({ provider, runtimeRunning, actionPending, feedback, onTest, onSyncModels, onRequestChange, onRequestEdit, onRequestDelete }: {
  provider: ProviderSummary;
  runtimeRunning: boolean;
  actionPending: boolean;
  feedback: ProviderFeedback;
  onTest: (provider: ProviderSummary) => void;
  onSyncModels: (provider: ProviderSummary) => void;
  onRequestChange: (provider: ProviderSummary, enabled: boolean) => void;
  onRequestEdit: (provider: ProviderSummary) => void;
  onRequestDelete: (provider: ProviderSummary) => void;
}) {
  const handleTest = useCallback(() => onTest(provider), [onTest, provider]);
  const handleSyncModels = useCallback(() => onSyncModels(provider), [onSyncModels, provider]);
  const handleToggle = useCallback(() => onRequestChange(provider, !provider.enabled), [onRequestChange, provider]);
  const handleEdit = useCallback(() => onRequestEdit(provider), [onRequestEdit, provider]);
  const handleDelete = useCallback(() => onRequestDelete(provider), [onRequestDelete, provider]);
  const disabled = !runtimeRunning || actionPending;

  return (
    <li className="service-row service-row-managed">
      <span className="drag-handle" aria-hidden="true">⠿</span>
      <span className="service-avatar" aria-hidden="true">{initialOf(provider.name)}</span>
      <div className="service-main">
        <strong>{provider.name}</strong>
        <span>{provider.base_url}</span>
        {feedback?.providerID === provider.id ? <small className={feedback.success ? "provider-feedback is-success" : "provider-feedback"}>{feedback.message}</small> : null}
      </div>
      <div className="service-meta service-actions">
        <span>{provider.adapter_type}</span>
        <span className={provider.enabled ? "service-state is-enabled" : "service-state"}>{provider.enabled ? "已启用" : "未启用"}</span>
        <div className="service-button-group">
          <button type="button" className="button button-secondary" onClick={handleTest} disabled={disabled}>测试</button>
          <button type="button" className="button button-secondary" onClick={handleSyncModels} disabled={disabled}>同步模型</button>
          <button type="button" className={provider.enabled ? "button button-secondary" : "button button-primary"} onClick={handleToggle} disabled={disabled}>{actionPending ? "正在更新" : provider.enabled ? "停用" : "启用"}</button>
          <button type="button" className="button button-secondary" onClick={handleEdit} disabled={disabled}>编辑</button>
          <button type="button" className="button button-secondary button-danger" onClick={handleDelete} disabled={disabled}>删除</button>
        </div>
      </div>
    </li>
  );
}

function ServicePage({ dashboard, loading, actionPendingID, feedback, onCreate, onTest, onSyncModels, onRequestChange, onRequestEdit, onRequestDelete }: {
  dashboard: DashboardSnapshot | null;
  loading: boolean;
  actionPendingID: string | null;
  feedback: ProviderFeedback;
  onCreate: () => void;
  onTest: (provider: ProviderSummary) => void;
  onSyncModels: (provider: ProviderSummary) => void;
  onRequestChange: (provider: ProviderSummary, enabled: boolean) => void;
  onRequestEdit: (provider: ProviderSummary) => void;
  onRequestDelete: (provider: ProviderSummary) => void;
}) {
  const providers = dashboard?.providers ?? [];
  const runtimeRunning = dashboard?.runtime.state === "running";

  return (
    <section className="page-section" aria-labelledby="services-title">
      <div className="page-heading">
        <div>
          <h1 id="services-title">服务</h1>
          <p>配置上游地址和凭据，再同步可用模型。</p>
        </div>
        <div className="page-heading-actions">
          <span className="page-count">{providers.length} 个服务</span>
          <button type="button" className="button button-primary" onClick={onCreate} disabled={!runtimeRunning}>新增服务</button>
        </div>
      </div>

      {loading ? <p className="inline-status" role="status">正在读取服务列表…</p> : null}
      {!loading && providers.length === 0 ? <EmptyState title="还没有服务" description="新增一个 OpenAI 兼容服务后，即可测试连接并同步模型。" /> : null}
      {providers.length > 0 ? (
        <ul className="service-list" aria-label="已保存服务">
          {providers.map((provider) => <ProviderRow key={provider.id} provider={provider} runtimeRunning={runtimeRunning} actionPending={actionPendingID === provider.id} feedback={feedback} onTest={onTest} onSyncModels={onSyncModels} onRequestChange={onRequestChange} onRequestEdit={onRequestEdit} onRequestDelete={onRequestDelete} />)}
        </ul>
      ) : null}
    </section>
  );
}

function CreateProviderDialog({ open, pending, onClose, onCreate }: { open: boolean; pending: boolean; onClose: () => void; onCreate: (input: CreateProviderInput) => Promise<boolean> }) {
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [adapterType, setAdapterType] = useState<CreateProviderInput["adapter_type"]>("openai-compatible");
  const [authType, setAuthType] = useState<CreateProviderInput["auth_type"]>("api_key");
  const [authHeaderMode, setAuthHeaderMode] = useState<CreateProviderInput["auth_header_mode"]>("authorization_bearer");
  const [baseURL, setBaseURL] = useState("");
  const [credential, setCredential] = useState("");
  const [formError, setFormError] = useState<string | null>(null);

  const handleAdapterChange = useCallback((event: ChangeEvent<HTMLSelectElement>) => {
    const next = event.target.value as CreateProviderInput["adapter_type"];
    setAdapterType(next);
    setCredential("");
    if (next === "local-openai-compatible") {
      setAuthType("none");
      return;
    }
    if (next === "anthropic-compatible") {
      setAuthType("api_key");
      setAuthHeaderMode("x_api_key");
      return;
    }
    setAuthType("api_key");
    setAuthHeaderMode("authorization_bearer");
  }, []);
  const handleAuthChange = useCallback((event: ChangeEvent<HTMLSelectElement>) => {
    const next = event.target.value as CreateProviderInput["auth_type"];
    setAuthType(next);
    if (next === "none") setCredential("");
  }, []);
  const handleSubmit = useCallback(async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const normalizedSlug = slug.trim();
    const normalizedName = name.trim();
    const normalizedURL = baseURL.trim();
    if (!/^[a-z0-9-]{1,48}$/.test(normalizedSlug) || normalizedName === "" || normalizedURL === "" || (authType !== "none" && credential.trim() === "")) {
      setFormError("请检查名称、slug、地址和认证信息。");
      return;
    }
    setFormError(null);
    const created = await onCreate({ slug: normalizedSlug, name: normalizedName, adapter_type: adapterType, auth_type: authType, auth_header_mode: authHeaderMode, base_url: normalizedURL, credential: authType === "none" ? undefined : credential });
    setCredential("");
    if (created) {
      setName("");
      setSlug("");
      setBaseURL("");
      onClose();
    }
  }, [adapterType, authHeaderMode, authType, baseURL, credential, name, onClose, onCreate, slug]);

  if (!open) return null;
  return (
    <div className="dialog-backdrop" role="presentation">
      <section className="confirm-dialog provider-dialog" role="dialog" aria-modal="true" aria-labelledby="provider-create-title">
        <div className="dialog-heading">
          <div><h2 id="provider-create-title">新增服务</h2><p>凭据只会通过本地桌面桥接提交，不会写入浏览器存储或服务列表。</p></div>
          <button type="button" className="dialog-close" aria-label="关闭新增服务" onClick={onClose} disabled={pending}>×</button>
        </div>
        <form className="provider-form" onSubmit={handleSubmit}>
          <label><span>名称</span><input className="text-input" value={name} onChange={(event) => setName(event.target.value)} maxLength={128} autoFocus /></label>
          <label><span>Slug</span><input className="text-input" value={slug} onChange={(event) => setSlug(event.target.value)} maxLength={48} placeholder="my-openai" /></label>
          <label><span>服务类型</span><select className="text-input" value={adapterType} onChange={handleAdapterChange}><option value="openai-compatible">OpenAI 兼容</option><option value="anthropic-compatible">Anthropic 兼容（Messages）</option><option value="local-openai-compatible">本地 OpenAI 兼容</option></select></label>
          <label><span>上游地址</span><input className="text-input" value={baseURL} onChange={(event) => setBaseURL(event.target.value)} maxLength={2048} placeholder="https://api.example.com" inputMode="url" /></label>
          <label><span>认证方式</span><select className="text-input" value={authType} onChange={handleAuthChange} disabled={adapterType === "local-openai-compatible"}><option value="api_key">API Key</option><option value="bearer_token">Bearer Token</option>{adapterType !== "anthropic-compatible" ? <option value="none">不使用认证</option> : null}</select></label>
          {authType !== "none" ? <><label><span>认证请求头</span><select className="text-input" value={authHeaderMode} onChange={(event) => setAuthHeaderMode(event.target.value as CreateProviderInput["auth_header_mode"])}><option value="authorization_bearer">Authorization: Bearer</option><option value="x_api_key">X-API-Key</option></select></label><label className="provider-form-wide"><span>上游密钥</span><input className="text-input" value={credential} onChange={(event) => setCredential(event.target.value)} type="password" autoComplete="off" maxLength={5120} /></label></> : null}
          {formError ? <p className="form-error" role="alert">{formError}</p> : null}
          <div className="button-group provider-form-wide"><button type="button" className="button button-secondary" onClick={onClose} disabled={pending}>取消</button><button type="submit" className="button button-primary" disabled={pending}>{pending ? "正在保存" : "保存服务"}</button></div>
        </form>
      </section>
    </div>
  );
}

function EditProviderDialog({ provider, pending, onClose, onUpdate }: { provider: ProviderSummary | null; pending: boolean; onClose: () => void; onUpdate: (provider: ProviderSummary, input: UpdateProviderInput) => Promise<boolean> }) {
  const [name, setName] = useState("");
  const [baseURL, setBaseURL] = useState("");
  const [timeoutMS, setTimeoutMS] = useState("30000");
  const [authHeaderMode, setAuthHeaderMode] = useState<UpdateProviderInput["auth_header_mode"]>("authorization_bearer");
  const [credential, setCredential] = useState("");
  const [formError, setFormError] = useState<string | null>(null);

  useEffect(() => {
    if (!provider) return;
    setName(provider.name);
    setBaseURL(provider.base_url);
    setTimeoutMS(String(provider.timeout_ms));
    setAuthHeaderMode(provider.adapter_config.auth_header_mode);
    setCredential("");
    setFormError(null);
  }, [provider]);

  const handleSubmit = useCallback(async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!provider) return;
    const normalizedName = name.trim();
    const normalizedURL = baseURL.trim();
    const parsedTimeout = Number(timeoutMS);
    if (normalizedName === "" || normalizedURL === "" || !Number.isInteger(parsedTimeout) || parsedTimeout < 1000 || parsedTimeout > 3600000) {
      setFormError("请检查名称、上游地址和超时时间。");
      return;
    }
    setFormError(null);
    const updated = await onUpdate(provider, { name: normalizedName, base_url: normalizedURL, timeout_ms: parsedTimeout, auth_header_mode: authHeaderMode, credential: credential.trim() === "" ? undefined : credential, version: provider.version });
    setCredential("");
    if (updated) onClose();
  }, [authHeaderMode, baseURL, credential, name, onClose, onUpdate, provider, timeoutMS]);

  if (!provider) return null;
  return <div className="dialog-backdrop" role="presentation"><section className="confirm-dialog provider-dialog" role="dialog" aria-modal="true" aria-labelledby="provider-edit-title"><div className="dialog-heading"><div><h2 id="provider-edit-title">编辑服务</h2><p>保留密钥输入为空即可继续使用当前凭据；填写新值才会替换。</p></div><button type="button" className="dialog-close" aria-label="关闭编辑服务" onClick={onClose} disabled={pending}>×</button></div><form className="provider-form" onSubmit={handleSubmit}><label><span>名称</span><input className="text-input" value={name} onChange={(event) => setName(event.target.value)} maxLength={128} autoFocus /></label><label><span>上游地址</span><input className="text-input" value={baseURL} onChange={(event) => setBaseURL(event.target.value)} maxLength={2048} inputMode="url" /></label><label><span>超时（毫秒）</span><input className="text-input" value={timeoutMS} onChange={(event) => setTimeoutMS(event.target.value)} inputMode="numeric" /></label><label><span>认证请求头</span><select className="text-input" value={authHeaderMode} onChange={(event) => setAuthHeaderMode(event.target.value as UpdateProviderInput["auth_header_mode"])}><option value="authorization_bearer">Authorization: Bearer</option><option value="x_api_key">X-API-Key</option></select></label>{provider.auth_type !== "none" ? <label className="provider-form-wide"><span>替换上游密钥（可选）</span><input className="text-input" value={credential} onChange={(event) => setCredential(event.target.value)} type="password" autoComplete="off" maxLength={5120} placeholder={provider.credential.configured ? "当前凭据已配置" : "填写新的上游密钥"} /></label> : null}{formError ? <p className="form-error" role="alert">{formError}</p> : null}<div className="button-group provider-form-wide"><button type="button" className="button button-secondary" onClick={onClose} disabled={pending}>取消</button><button type="submit" className="button button-primary" disabled={pending}>{pending ? "正在保存" : "保存修改"}</button></div></form></section></div>;
}

function ProviderChangeDialog({ change, pending, onConfirm, onCancel }: { change: PendingProviderChange | null; pending: boolean; onConfirm: () => void; onCancel: () => void }) {
  if (!change) return null;
  const action = change.enabled ? "启用" : "停用";
  return <div className="dialog-backdrop" role="presentation"><section className="confirm-dialog" role="dialog" aria-modal="true" aria-labelledby="provider-change-title"><h2 id="provider-change-title">确认{action}服务</h2><p>{change.enabled ? "启用后，已启用的可用模型将出现在本地网关模型列表中。" : "停用后，新的本地请求不会再路由到这个服务。"}</p><code>{change.provider.name}</code><div className="button-group"><button type="button" className="button button-secondary" onClick={onCancel} disabled={pending}>取消</button><button type="button" className="button button-primary" onClick={onConfirm} disabled={pending} autoFocus>{pending ? "正在更新" : "确认" + action}</button></div></section></div>;
}

function ClientConfigPage({
  runtime,
  actionPending,
  keyName,
  oneTimeKey,
  copyState,
  addressCopyState,
  onKeyNameChange,
  onCreateKey,
  onCopyKey,
  onCopyAddress,
  codexModelID,
  codexCopyState,
  onCodexModelIDChange,
  onCopyCodexConfig,
  canTest,
  testPending,
  testResult,
  onTest,
}: {
  runtime: RuntimeSnapshot | undefined;
  actionPending: boolean;
  keyName: string;
  oneTimeKey: OneTimeLocalKey | null;
  copyState: CopyState;
  addressCopyState: CopyState;
  onKeyNameChange: (value: string) => void;
  onCreateKey: () => void;
  onCopyKey: () => void;
  onCopyAddress: () => void;
  codexModelID: string;
  codexCopyState: CopyState;
  onCodexModelIDChange: (value: string) => void;
  onCopyCodexConfig: (value: string) => void;
  canTest: boolean;
  testPending: boolean;
  testResult: LocalResponsesTestResult | null;
  onTest: (kind: "text" | "function") => void;
}) {
  const isRunning = runtime?.state === "running";

  return (
    <section className="page-section" aria-labelledby="clients-title">
      <div className="page-heading">
        <div>
          <h1 id="clients-title">客户端配置</h1>
          <p>创建本地访问密钥，并将本地地址填入支持的编程工具。</p>
        </div>
      </div>

      <div className="settings-list">
        <section className="settings-row" aria-labelledby="address-title">
          <div>
            <h2 id="address-title">本地地址</h2>
            <p>{runtime?.data_plane_url ?? "启动网关后显示"}</p>
          </div>
          <button type="button" className="button button-secondary" onClick={onCopyAddress} disabled={!runtime?.data_plane_url}>
            {addressCopyState === "copied" ? "已复制" : addressCopyState === "failed" ? "复制失败" : "复制地址"}
          </button>
        </section>

        <section className="key-panel" aria-labelledby="key-title">
          <div className="key-panel-heading">
            <div>
              <h2 id="key-title">本地访问密钥</h2>
              <p>密钥只会完整显示一次，请立即复制并妥善保存。</p>
            </div>
            <span className={isRunning ? "service-state is-enabled" : "service-state"}>{isRunning ? "网关可用" : "需先启动网关"}</span>
          </div>
          <label className="field-label" htmlFor="key-name">名称</label>
          <div className="key-form">
            <input id="key-name" className="text-input" value={keyName} onChange={(event) => onKeyNameChange(event.target.value)} maxLength={80} />
            <button type="button" className="button button-primary" onClick={onCreateKey} disabled={!isRunning || actionPending}>
              创建 Local Key
            </button>
          </div>
          {oneTimeKey ? (
            <div className="one-time-key" role="status">
              <span>请立即保存，关闭后不会再次显示。</span>
              <code>{oneTimeKey.key}</code>
              <button type="button" className="button button-secondary key-copy-button" onClick={onCopyKey}>
                {copyState === "copied" ? "已复制" : copyState === "failed" ? "复制失败" : "复制密钥"}
              </button>
            </div>
          ) : null}
        </section>
        <CodexSetupPage dataPlaneURL={runtime?.data_plane_url ?? undefined} publicModelID={codexModelID} copyState={codexCopyState} onPublicModelIDChange={onCodexModelIDChange} onCopyConfig={onCopyCodexConfig} onCopyPowerShell={onCopyCodexConfig} canTest={canTest} testPending={testPending} testResult={testResult} onTest={onTest} />
      </div>
    </section>
  );
}

function ModelRow({ model, actionPending, onRequestChange, onRequestEdit, onRequestEditLimits, onRequestDelete }: { model: ModelSummary; actionPending: boolean; onRequestChange: (model: ModelSummary, enabled: boolean) => void; onRequestEdit: (model: ModelSummary) => void; onRequestEditLimits: (model: ModelSummary) => void; onRequestDelete: (model: ModelSummary) => void }) {
  const capabilityLabels = (Object.keys(modelCapabilityLabels) as ModelCapability[])
    .filter((capability) => model.capabilities[capability])
    .map((capability) => modelCapabilityLabels[capability]);

  const handleToggle = useCallback(() => onRequestChange(model, !model.enabled), [model, onRequestChange]);
  const handleEdit = useCallback(() => onRequestEdit(model), [model, onRequestEdit]);
  const handleEditLimits = useCallback(() => onRequestEditLimits(model), [model, onRequestEditLimits]);
  const handleDelete = useCallback(() => onRequestDelete(model), [model, onRequestDelete]);

  return (
    <li className="model-row">
      <span className="model-avatar" aria-hidden="true">M</span>
      <div className="model-main">
        <strong>{model.display_name}</strong>
        <span>{model.public_model_id}</span>
        <div className="model-tags" aria-label="模型能力">
          <span className={model.enabled ? "service-state is-enabled" : "service-state"}>{model.enabled ? "已启用" : "未启用"}</span>
          <span className="service-state">{modelStatusLabels[model.lifecycle_status]}</span>
          {model.source === "manual" ? <span className="model-capability">手工</span> : null}
          {capabilityLabels.map((label) => <span key={label} className="model-capability">{label}</span>)}
        </div>
      </div>
      <div className="model-meta">
        <span>{model.upstream_model_id}</span>
        <div className="model-actions">
          <button type="button" className="button button-secondary" onClick={handleEdit} disabled={actionPending}>能力</button>
          <button type="button" className="button button-secondary" onClick={handleEditLimits} disabled={actionPending}>参数</button>
          {model.source === "manual" ? <button type="button" className="button button-danger" onClick={handleDelete} disabled={actionPending}>删除</button> : null}
          <button type="button" className={model.enabled ? "button button-secondary" : "button button-primary"} onClick={handleToggle} disabled={actionPending || (model.lifecycle_status !== "available" && model.lifecycle_status !== "degraded" && !model.enabled)}>
            {actionPending ? "正在更新" : model.enabled ? "停用" : "启用"}
          </button>
        </div>
      </div>
    </li>
  );
}

function ModelPage({
  runtime,
  page,
  loading,
  actionPendingID,
  search,
  enabledFilter,
  capabilityFilter,
  onSearchChange,
  onEnabledFilterChange,
  onCapabilityFilterChange,
  onApplyFilters,
  onNextPage,
  onRefresh,
  onRequestChange,
  onRequestEdit,
  onRequestEditLimits,
  onRequestDelete,
  onCreateManual,
  providers,
}: {
  runtime: RuntimeSnapshot | undefined;
  page: ModelPageData | null;
  loading: boolean;
  actionPendingID: string | null;
  search: string;
  enabledFilter: ModelEnabledFilter;
  capabilityFilter: "" | ModelCapability;
  onSearchChange: (value: string) => void;
  onEnabledFilterChange: (value: ModelEnabledFilter) => void;
  onCapabilityFilterChange: (value: "" | ModelCapability) => void;
  onApplyFilters: () => void;
  onNextPage: () => void;
  onRefresh: () => void;
  onRequestChange: (model: ModelSummary, enabled: boolean) => void;
  onRequestEdit: (model: ModelSummary) => void;
  onRequestEditLimits: (model: ModelSummary) => void;
  onRequestDelete: (model: ModelSummary) => void;
  onCreateManual: () => void;
  providers: ProviderSummary[];
}) {
  const handleSubmit = useCallback((event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    onApplyFilters();
  }, [onApplyFilters]);
  const handleSearchChange = useCallback((event: ChangeEvent<HTMLInputElement>) => onSearchChange(event.target.value), [onSearchChange]);
  const handleEnabledChange = useCallback((event: ChangeEvent<HTMLSelectElement>) => onEnabledFilterChange(event.target.value as ModelEnabledFilter), [onEnabledFilterChange]);
  const handleCapabilityChange = useCallback((event: ChangeEvent<HTMLSelectElement>) => onCapabilityFilterChange(event.target.value as "" | ModelCapability), [onCapabilityFilterChange]);

  if (runtime?.state !== "running") {
    return <EmptyState title="网关尚未运行" description="启动本地网关后，才能读取已同步的模型目录。" />;
  }

  return (
    <section className="page-section" aria-labelledby="models-title">
      <div className="page-heading">
        <div>
          <h1 id="models-title">模型</h1>
          <p>从已同步的服务中选择可以暴露给本地网关的模型。</p>
        </div>
        <div className="button-group">
          <button type="button" className="button button-secondary" onClick={onRefresh} disabled={loading}>刷新</button>
          <button type="button" className="button button-primary" onClick={onCreateManual} disabled={loading || providers.length === 0}>手工添加</button>
        </div>
      </div>
      <form className="model-filter" aria-label="模型筛选" onSubmit={handleSubmit}>
        <label>
          <span>搜索</span>
          <input className="text-input" value={search} onChange={handleSearchChange} maxLength={128} placeholder="模型名称或公开模型 ID" />
        </label>
        <label>
          <span>状态</span>
          <select className="text-input" value={enabledFilter} onChange={handleEnabledChange}>
            <option value="all">全部</option>
            <option value="enabled">已启用</option>
            <option value="disabled">未启用</option>
          </select>
        </label>
        <label>
          <span>能力</span>
          <select className="text-input" value={capabilityFilter} onChange={handleCapabilityChange}>
            <option value="">全部</option>
            {(Object.keys(modelCapabilityLabels) as ModelCapability[]).map((capability) => <option key={capability} value={capability}>{modelCapabilityLabels[capability]}</option>)}
          </select>
        </label>
        <button type="submit" className="button button-primary" disabled={loading}>筛选</button>
      </form>
      {loading ? <p className="inline-status" role="status">正在读取模型目录…</p> : null}
      {!loading && page?.data.length === 0 ? <EmptyState title="没有找到模型" description="请先同步上游模型，或调整当前筛选条件。" /> : null}
      {page && page.data.length > 0 ? (
        <>
          <ul className="model-list" aria-label="已同步模型">
            {page.data.map((model) => <ModelRow key={model.id} model={model} actionPending={actionPendingID === model.id} onRequestChange={onRequestChange} onRequestEdit={onRequestEdit} onRequestEditLimits={onRequestEditLimits} onRequestDelete={onRequestDelete} />)}
          </ul>
          {page.next_cursor ? <div className="model-pagination"><button type="button" className="button button-secondary" onClick={onNextPage} disabled={loading || actionPendingID !== null}>下一页</button></div> : null}
        </>
      ) : null}
    </section>
  );
}

function ProviderDeleteDialog({ provider, pending, onConfirm, onCancel }: { provider: ProviderSummary | null; pending: boolean; onConfirm: () => void; onCancel: () => void }) {
  if (!provider) return null;
  return <div className="dialog-backdrop" role="presentation"><section className="confirm-dialog" role="dialog" aria-modal="true" aria-labelledby="provider-delete-title"><h2 id="provider-delete-title">删除服务</h2><p>会移除服务配置及其本地模型目录。此操作不能撤销；上游账户与密钥不会被修改。</p><code>{provider.name}</code><div className="button-group"><button type="button" className="button button-secondary" onClick={onCancel} disabled={pending}>取消</button><button type="button" className="button button-danger" onClick={onConfirm} disabled={pending} autoFocus>{pending ? "正在删除" : "确认删除"}</button></div></section></div>;
}

function EditModelCapabilitiesDialog({ model, pending, onClose, onUpdate }: { model: ModelSummary | null; pending: boolean; onClose: () => void; onUpdate: (model: ModelSummary, capabilityOverride: ModelCapabilityOverride) => void }) {
  const [values, setValues] = useState<ModelCapabilityValues>(() => capabilityValuesFromModel(model));

  useEffect(() => {
    setValues(capabilityValuesFromModel(model));
  }, [model]);

  const handleToggle = useCallback((event: ChangeEvent<HTMLInputElement>) => {
    const capability = event.currentTarget.name as ModelCapability;
    const checked = event.currentTarget.checked;
    setValues((current) => ({ ...current, [capability]: checked }));
  }, []);
  const handleSubmit = useCallback((event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (model) onUpdate(model, capabilityOverrideFromValues(values));
  }, [model, onUpdate, values]);
  const handleReset = useCallback(() => {
    if (model) onUpdate(model, {});
  }, [model, onUpdate]);

  if (!model) return null;
  return (
    <div className="dialog-backdrop" role="presentation">
      <section className="confirm-dialog model-capability-dialog" role="dialog" aria-modal="true" aria-labelledby="model-capability-title">
        <h2 id="model-capability-title">模型能力</h2>
        <p>覆盖只影响本地网关的预检与路由，不能让上游实际支持未实现的能力。</p>
        <code>{model.public_model_id}</code>
        <form onSubmit={handleSubmit}>
          <fieldset className="capability-settings" disabled={pending}>
            <legend>本地声明</legend>
            {(Object.keys(modelCapabilityLabels) as ModelCapability[]).map((capability) => (
              <label key={capability} className="capability-setting">
                <input name={capability} type="checkbox" checked={values[capability]} onChange={handleToggle} />
                <span>{modelCapabilityLabels[capability]}</span>
              </label>
            ))}
          </fieldset>
          <p className="dialog-note">当前来源：{model.capability_override && Object.keys(model.capability_override).length > 0 ? "已覆盖" : model.capability_source}</p>
          <div className="button-group">
            <button type="button" className="button button-secondary" onClick={onClose} disabled={pending}>取消</button>
            <button type="button" className="button button-secondary" onClick={handleReset} disabled={pending}>恢复上游声明</button>
            <button type="submit" className="button button-primary" disabled={pending} autoFocus>{pending ? "正在保存" : "保存能力设置"}</button>
          </div>
        </form>
      </section>
    </div>
  );
}

function parseOptionalPositiveLimit(value: string): number | undefined | null {
  const trimmed = value.trim();
  if (trimmed === "") return undefined;
  const parsed = Number(trimmed);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : null;
}

function EditModelLimitsDialog({ model, pending, onClose, onUpdate }: { model: ModelSummary | null; pending: boolean; onClose: () => void; onUpdate: (model: ModelSummary, limitOverride: ModelLimitOverride) => void }) {
  const [contextWindow, setContextWindow] = useState("");
  const [maxOutput, setMaxOutput] = useState("");
  const [formError, setFormError] = useState<string | null>(null);

  useEffect(() => {
    setContextWindow(model?.limit_override.context_window_tokens?.toString() ?? "");
    setMaxOutput(model?.limit_override.max_output_tokens?.toString() ?? "");
    setFormError(null);
  }, [model]);

  const handleSubmit = useCallback((event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!model) return;
    const contextValue = parseOptionalPositiveLimit(contextWindow);
    const outputValue = parseOptionalPositiveLimit(maxOutput);
    if (contextValue === null || outputValue === null) {
      setFormError("参数必须是正整数；留空即可恢复上游声明。");
      return;
    }
    onUpdate(model, {
      ...(contextValue === undefined ? {} : { context_window_tokens: contextValue }),
      ...(outputValue === undefined ? {} : { max_output_tokens: outputValue }),
    });
  }, [contextWindow, maxOutput, model, onUpdate]);

  if (!model) return null;
  return (
    <div className="dialog-backdrop" role="presentation">
      <section className="confirm-dialog" role="dialog" aria-modal="true" aria-labelledby="model-limits-title">
        <h2 id="model-limits-title">模型参数</h2>
        <p>仅覆盖本地目录中展示的上下文和最大输出限制；留空会恢复上游声明。</p>
        <code>{model.public_model_id}</code>
        <form onSubmit={handleSubmit}>
          <label className="form-field"><span>上下文长度</span><input className="text-input" inputMode="numeric" value={contextWindow} onChange={(event) => setContextWindow(event.target.value)} placeholder={model.context_window_tokens?.toString() ?? "未声明"} disabled={pending} /></label>
          <label className="form-field"><span>最大输出</span><input className="text-input" inputMode="numeric" value={maxOutput} onChange={(event) => setMaxOutput(event.target.value)} placeholder={model.max_output_tokens?.toString() ?? "未声明"} disabled={pending} /></label>
          {formError ? <p className="form-error" role="alert">{formError}</p> : null}
          <div className="button-group">
            <button type="button" className="button button-secondary" onClick={onClose} disabled={pending}>取消</button>
            <button type="submit" className="button button-primary" disabled={pending} autoFocus>{pending ? "正在保存" : "保存参数"}</button>
          </div>
        </form>
      </section>
    </div>
  );
}

function ManualModelDialog({ providers, pending, onClose, onCreate }: { providers: ProviderSummary[]; pending: boolean; onClose: () => void; onCreate: (providerID: string, input: CreateManualModelInput) => void }) {
  const [providerID, setProviderID] = useState(providers[0]?.id ?? "");
  const [upstreamModelID, setUpstreamModelID] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [values, setValues] = useState<ModelCapabilityValues>({ streaming: true, tools: false, parallel_tools: false, reasoning: false, thinking: false, vision: false });
  const [contextWindow, setContextWindow] = useState("");
  const [maxOutput, setMaxOutput] = useState("");
  const [formError, setFormError] = useState<string | null>(null);

  useEffect(() => {
    if (!providers.some((provider) => provider.id === providerID)) setProviderID(providers[0]?.id ?? "");
  }, [providerID, providers]);

  const handleToggle = useCallback((event: ChangeEvent<HTMLInputElement>) => {
    const capability = event.currentTarget.name as ModelCapability;
    setValues((current) => ({ ...current, [capability]: event.currentTarget.checked }));
  }, []);
  const handleSubmit = useCallback((event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const contextValue = parseOptionalPositiveLimit(contextWindow);
    const outputValue = parseOptionalPositiveLimit(maxOutput);
    if (!providerID || upstreamModelID.trim() === "" || displayName.trim() === "") {
      setFormError("请选择服务，并填写模型标识和显示名称。");
      return;
    }
    if (contextValue === null || outputValue === null) {
      setFormError("参数必须是正整数，或保持留空。");
      return;
    }
    onCreate(providerID, {
      upstream_model_id: upstreamModelID.trim(),
      display_name: displayName.trim(),
      capabilities: values,
      ...(contextValue === undefined ? {} : { context_window_tokens: contextValue }),
      ...(outputValue === undefined ? {} : { max_output_tokens: outputValue }),
    });
  }, [contextWindow, displayName, maxOutput, onCreate, providerID, upstreamModelID, values]);

  return (
    <div className="dialog-backdrop" role="presentation">
      <section className="confirm-dialog model-capability-dialog" role="dialog" aria-modal="true" aria-labelledby="manual-model-title">
        <h2 id="manual-model-title">手工添加模型</h2>
        <p>适合上游没有模型发现接口，或需要补充目录中未返回的模型。</p>
        <form onSubmit={handleSubmit}>
          <label className="form-field"><span>服务</span><select className="text-input" value={providerID} onChange={(event) => setProviderID(event.target.value)} disabled={pending}>{providers.map((provider) => <option key={provider.id} value={provider.id}>{provider.name}</option>)}</select></label>
          <label className="form-field"><span>上游模型标识</span><input className="text-input" value={upstreamModelID} onChange={(event) => setUpstreamModelID(event.target.value)} maxLength={255} placeholder="例如 gpt-4.1" disabled={pending} /></label>
          <label className="form-field"><span>显示名称</span><input className="text-input" value={displayName} onChange={(event) => setDisplayName(event.target.value)} maxLength={255} placeholder="例如 GPT-4.1" disabled={pending} /></label>
          <fieldset className="capability-settings" disabled={pending}><legend>能力</legend>{(Object.keys(modelCapabilityLabels) as ModelCapability[]).map((capability) => <label key={capability} className="capability-setting"><input name={capability} type="checkbox" checked={values[capability]} onChange={handleToggle} /><span>{modelCapabilityLabels[capability]}</span></label>)}</fieldset>
          <label className="form-field"><span>上下文长度（可选）</span><input className="text-input" inputMode="numeric" value={contextWindow} onChange={(event) => setContextWindow(event.target.value)} placeholder="例如 128000" disabled={pending} /></label>
          <label className="form-field"><span>最大输出（可选）</span><input className="text-input" inputMode="numeric" value={maxOutput} onChange={(event) => setMaxOutput(event.target.value)} placeholder="例如 8192" disabled={pending} /></label>
          {formError ? <p className="form-error" role="alert">{formError}</p> : null}
          <div className="button-group"><button type="button" className="button button-secondary" onClick={onClose} disabled={pending}>取消</button><button type="submit" className="button button-primary" disabled={pending} autoFocus>{pending ? "正在创建" : "创建模型"}</button></div>
        </form>
      </section>
    </div>
  );
}

function ModelDeleteDialog({ change, pending, onConfirm, onCancel }: { change: PendingModelDelete | null; pending: boolean; onConfirm: () => void; onCancel: () => void }) {
  if (!change) return null;
  return <div className="dialog-backdrop" role="presentation"><section className="confirm-dialog" role="dialog" aria-modal="true" aria-labelledby="model-delete-title"><h2 id="model-delete-title">删除手工模型</h2><p>仅从本地模型目录移除，不会修改上游服务或账号。</p><code>{change.model.public_model_id}</code><div className="button-group"><button type="button" className="button button-secondary" onClick={onCancel} disabled={pending}>取消</button><button type="button" className="button button-danger" onClick={onConfirm} disabled={pending} autoFocus>{pending ? "正在删除" : "确认删除"}</button></div></section></div>;
}

function ModelChangeDialog({ change, pending, onConfirm, onCancel }: { change: PendingModelChange | null; pending: boolean; onConfirm: () => void; onCancel: () => void }) {
  if (!change) return null;
  const action = change.enabled ? "启用" : "停用";
  return (
    <div className="dialog-backdrop" role="presentation">
      <section className="confirm-dialog" role="dialog" aria-modal="true" aria-labelledby="model-change-title">
        <h2 id="model-change-title">确认{action}模型</h2>
        <p>{change.enabled ? "启用后，该模型会在对应 Provider 也启用时出现在本地网关的模型列表中。" : "停用后，新的本地请求将不能再路由到该模型。"}</p>
        <code>{change.model.public_model_id}</code>
        <div className="button-group">
          <button type="button" className="button button-secondary" onClick={onCancel} disabled={pending}>取消</button>
          <button type="button" className="button button-primary" onClick={onConfirm} disabled={pending} autoFocus>{pending ? "正在更新" : "确认" + action}</button>
        </div>
      </section>
    </div>
  );
}

function SettingsPage({
  runtime,
  loading,
  actionPending,
  onRefresh,
  onStartOrRestart,
}: {
  runtime: RuntimeSnapshot | undefined;
  loading: boolean;
  actionPending: boolean;
  onRefresh: () => void;
  onStartOrRestart: () => void;
}) {
  const shouldStart = runtime?.state === "stopped" || runtime?.state === "failed";

  return (
    <section className="page-section" aria-labelledby="settings-title">
      <div className="page-heading">
        <div>
          <h1 id="settings-title">设置</h1>
          <p>查看当前桌面端和本地网关的基础信息。</p>
        </div>
      </div>
      <section className="settings-list" aria-label="网关设置">
        <div className="settings-row">
          <div>
            <h2>当前版本</h2>
            <p>{runtime?.version ?? "—"}</p>
          </div>
        </div>
        <div className="settings-row">
          <div>
            <h2>运行状态</h2>
            <p>{runtime ? runtimeLabels[runtime.state] : "正在读取"}</p>
          </div>
          <div className="button-group">
            <button type="button" className="button button-secondary" onClick={onRefresh} disabled={loading || actionPending}>刷新</button>
            <button type="button" className="button button-primary" onClick={onStartOrRestart} disabled={!runtime || actionPending}>
              {shouldStart ? "启动网关" : "重启网关"}
            </button>
          </div>
        </div>
      </section>
    </section>
  );
}

export function App() {
  const [dashboard, setDashboard] = useState<DashboardSnapshot | null>(null);
  const [loading, setLoading] = useState(true);
  const [actionPending, setActionPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [activePage, setActivePage] = useState<PageID>("services");
  const [statusMenuOpen, setStatusMenuOpen] = useState(false);
  const [keyName, setKeyName] = useState("Claude Code");
  const [oneTimeKey, setOneTimeKey] = useState<OneTimeLocalKey | null>(null);
  const [copyState, setCopyState] = useState<CopyState>("idle");
  const [addressCopyState, setAddressCopyState] = useState<CopyState>("idle");
  const [codexModelID, setCodexModelID] = useState("");
  const [codexCopyState, setCodexCopyState] = useState<CopyState>("idle");
  const [codexTestPending, setCodexTestPending] = useState(false);
  const [codexTestResult, setCodexTestResult] = useState<LocalResponsesTestResult | null>(null);
  const [modelsPage, setModelsPage] = useState<ModelPageData | null>(null);
  const [modelsFetched, setModelsFetched] = useState(false);
  const [modelLoading, setModelLoading] = useState(false);
  const [modelActionPendingID, setModelActionPendingID] = useState<string | null>(null);
  const [pendingModelChange, setPendingModelChange] = useState<PendingModelChange | null>(null);
  const [pendingModelDelete, setPendingModelDelete] = useState<PendingModelDelete | null>(null);
  const [editingModel, setEditingModel] = useState<ModelSummary | null>(null);
  const [editingModelLimits, setEditingModelLimits] = useState<ModelSummary | null>(null);
  const [manualModelCreateOpen, setManualModelCreateOpen] = useState(false);
  const [modelSearch, setModelSearch] = useState("");
  const [modelEnabledFilter, setModelEnabledFilter] = useState<ModelEnabledFilter>("all");
  const [modelCapabilityFilter, setModelCapabilityFilter] = useState<"" | ModelCapability>("");
  const [modelQuery, setModelQuery] = useState<ModelListQuery>({ page_size: 50 });
  const [providerCreateOpen, setProviderCreateOpen] = useState(false);
  const [editingProvider, setEditingProvider] = useState<ProviderSummary | null>(null);
  const [providerActionPendingID, setProviderActionPendingID] = useState<string | null>(null);
  const [pendingProviderChange, setPendingProviderChange] = useState<PendingProviderChange | null>(null);
  const [pendingProviderDelete, setPendingProviderDelete] = useState<PendingProviderDelete | null>(null);
  const [providerFeedback, setProviderFeedback] = useState<ProviderFeedback>(null);
  const statusMenuRef = useRef<HTMLDivElement>(null);
  const modelRequestRef = useRef(0);

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

  const loadModels = useCallback(async (query: ModelListQuery) => {
    const requestID = modelRequestRef.current + 1;
    modelRequestRef.current = requestID;
    setModelLoading(true);
    try {
      const nextPage = await desktopApi.listModels(query);
      if (modelRequestRef.current !== requestID) return;
      setModelsPage(nextPage);
      setModelsFetched(true);
      setError(null);
    } catch (reason) {
      if (modelRequestRef.current !== requestID) return;
      setModelsPage(null);
      setModelsFetched(true);
      setError(safeMessage(reason, "无法读取模型目录"));
    } finally {
      if (modelRequestRef.current === requestID) setModelLoading(false);
    }
  }, []);

  useEffect(() => {
    if (activePage !== "models" || dashboard?.runtime.state !== "running" || modelsFetched) return;
    void loadModels(modelQuery);
  }, [activePage, dashboard?.runtime.state, loadModels, modelQuery, modelsFetched]);

  useEffect(() => {
    if (!statusMenuOpen) return undefined;

    const handlePointerDown = (event: PointerEvent) => {
      if (statusMenuRef.current?.contains(event.target as Node)) return;
      setStatusMenuOpen(false);
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setStatusMenuOpen(false);
    };

    document.addEventListener("pointerdown", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [statusMenuOpen]);

  const runtime = dashboard?.runtime;
  const statusLabel = runtime ? runtimeLabels[runtime.state] : "读取中";
  const statusDescription = useMemo(() => {
    if (error) return error;
    if (runtime?.last_error) return runtime.last_error;
    if (runtime?.state === "running") return "本地网关已就绪";
    if (runtime?.state === "starting") return "本地网关正在启动";
    return "本地网关尚未运行";
  }, [error, runtime]);

  const handleNavigation = useCallback((event: MouseEvent<HTMLButtonElement>) => {
    const page = event.currentTarget.dataset.page as PageID | undefined;
    if (!page) return;
    setActivePage(page);
    setStatusMenuOpen(false);
  }, []);

  const handleStatusMenuToggle = useCallback(() => {
    setStatusMenuOpen((open) => !open);
  }, []);

  const handleBrandClick = useCallback((event: MouseEvent<HTMLAnchorElement>) => {
    event.preventDefault();
    setActivePage("services");
  }, []);

  const handleRefresh = useCallback(() => {
    void refresh();
  }, [refresh]);

  const handleStartOrRestart = useCallback(async () => {
    setActionPending(true);
    try {
      if (runtime?.state === "stopped" || runtime?.state === "failed") {
        await desktopApi.start();
      } else {
        await desktopApi.restart();
      }
      await refresh();
      setModelsFetched(false);
    } catch (reason) {
      setError(safeMessage(reason, "无法启动本地网关"));
    } finally {
      setActionPending(false);
    }
  }, [refresh, runtime?.state]);

  const handleStop = useCallback(async () => {
    setActionPending(true);
    try {
      await desktopApi.stop();
      await refresh();
      setStatusMenuOpen(false);
    } catch (reason) {
      setError(safeMessage(reason, "无法停止本地网关"));
    } finally {
      setActionPending(false);
    }
  }, [refresh]);

  const handleCreateKey = useCallback(async () => {
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
  }, [keyName]);

  const handleCreateProvider = useCallback(async (input: CreateProviderInput) => {
    setActionPending(true);
    try {
      await desktopApi.createProvider(input);
      await refresh();
      setModelsFetched(false);
      setError(null);
      return true;
    } catch (reason) {
      setError(safeMessage(reason, "保存服务失败"));
      return false;
    } finally {
      setActionPending(false);
    }
  }, [refresh]);

  const handleUpdateProvider = useCallback(async (provider: ProviderSummary, input: UpdateProviderInput) => {
    setProviderActionPendingID(provider.id);
    try {
      await desktopApi.updateProvider(provider.id, input, provider.adapter_config);
      setModelsFetched(false);
      await refresh();
      setError(null);
      return true;
    } catch (reason) {
      setError(safeMessage(reason, "更新服务失败"));
      return false;
    } finally {
      setProviderActionPendingID(null);
    }
  }, [refresh]);

  const handleTestProvider = useCallback(async (provider: ProviderSummary) => {
    setProviderActionPendingID(provider.id);
    try {
      const result = await desktopApi.testProvider(provider.id);
      setProviderFeedback({ providerID: provider.id, message: result.success ? "连接测试通过" : result.message || "连接测试失败", success: result.success });
      if (result.success) setError(null);
    } catch (reason) {
      setProviderFeedback({ providerID: provider.id, message: "连接测试失败", success: false });
      setError(safeMessage(reason, "测试服务失败"));
    } finally {
      setProviderActionPendingID(null);
    }
  }, []);

  const handleSyncProviderModels = useCallback(async (provider: ProviderSummary) => {
    setProviderActionPendingID(provider.id);
    try {
      const result = await desktopApi.syncProviderModels(provider.id);
      setProviderFeedback({ providerID: provider.id, message: "已同步 " + result.discovered + " 个模型", success: true });
      setModelsFetched(false);
      setError(null);
    } catch (reason) {
      setProviderFeedback({ providerID: provider.id, message: "模型同步失败", success: false });
      setError(safeMessage(reason, "同步模型失败"));
    } finally {
      setProviderActionPendingID(null);
    }
  }, []);

  const handleRequestProviderChange = useCallback((provider: ProviderSummary, enabled: boolean) => {
    setPendingProviderChange({ provider, enabled });
  }, []);

  const handleConfirmProviderChange = useCallback(async () => {
    if (!pendingProviderChange) return;
    const { provider, enabled } = pendingProviderChange;
    setProviderActionPendingID(provider.id);
    try {
      if (enabled) {
        await desktopApi.enableProvider(provider.id, provider.version);
      } else {
        await desktopApi.disableProvider(provider.id, provider.version);
      }
      setPendingProviderChange(null);
      setModelsFetched(false);
      await refresh();
      setError(null);
    } catch (reason) {
      setError(safeMessage(reason, enabled ? "启用服务失败" : "停用服务失败"));
    } finally {
      setProviderActionPendingID(null);
    }
  }, [pendingProviderChange, refresh]);

  const handleCancelProviderChange = useCallback(() => {
    if (providerActionPendingID === null) setPendingProviderChange(null);
  }, [providerActionPendingID]);

  const handleRequestProviderDelete = useCallback((provider: ProviderSummary) => {
    setPendingProviderDelete({ provider });
  }, []);

  const handleConfirmProviderDelete = useCallback(async () => {
    if (!pendingProviderDelete) return;
    const { provider } = pendingProviderDelete;
    setProviderActionPendingID(provider.id);
    try {
      await desktopApi.deleteProvider(provider.id, provider.version);
      setPendingProviderDelete(null);
      setModelsFetched(false);
      await refresh();
      setError(null);
    } catch (reason) {
      setError(safeMessage(reason, "删除服务失败"));
    } finally {
      setProviderActionPendingID(null);
    }
  }, [pendingProviderDelete, refresh]);

  const handleCancelProviderDelete = useCallback(() => {
    if (providerActionPendingID === null) setPendingProviderDelete(null);
  }, [providerActionPendingID]);

  const buildModelQuery = useCallback((cursor?: string): ModelListQuery => {
    const query: ModelListQuery = { page_size: 50 };
    const search = modelSearch.trim();
    if (search !== "") query.search = search;
    if (modelEnabledFilter === "enabled") query.enabled = true;
    if (modelEnabledFilter === "disabled") query.enabled = false;
    if (modelCapabilityFilter !== "") query.capability = modelCapabilityFilter;
    if (cursor) query.cursor = cursor;
    return query;
  }, [modelCapabilityFilter, modelEnabledFilter, modelSearch]);

  const handleApplyModelFilters = useCallback(() => {
    const nextQuery = buildModelQuery();
    setModelQuery(nextQuery);
    void loadModels(nextQuery);
  }, [buildModelQuery, loadModels]);

  const handleRefreshModels = useCallback(() => {
    void loadModels(modelQuery);
  }, [loadModels, modelQuery]);

  const handleNextModelPage = useCallback(() => {
    if (!modelsPage?.next_cursor) return;
    const nextQuery = buildModelQuery(modelsPage.next_cursor);
    setModelQuery(nextQuery);
    void loadModels(nextQuery);
  }, [buildModelQuery, loadModels, modelsPage?.next_cursor]);

  const handleRequestModelChange = useCallback((model: ModelSummary, enabled: boolean) => {
    setPendingModelChange({ model, enabled });
  }, []);

  const handleConfirmModelChange = useCallback(async () => {
    if (!pendingModelChange) return;
    const { model, enabled } = pendingModelChange;
    setModelActionPendingID(model.id);
    try {
      if (enabled) {
        await desktopApi.enableModel(model.id, model.version);
      } else {
        await desktopApi.disableModel(model.id, model.version);
      }
      setPendingModelChange(null);
      await loadModels(modelQuery);
      setError(null);
    } catch (reason) {
      setError(safeMessage(reason, enabled ? "启用模型失败" : "停用模型失败"));
    } finally {
      setModelActionPendingID(null);
    }
  }, [loadModels, modelQuery, pendingModelChange]);

  const handleCancelModelChange = useCallback(() => {
    if (modelActionPendingID === null) setPendingModelChange(null);
  }, [modelActionPendingID]);

  const handleUpdateModelCapabilities = useCallback(async (model: ModelSummary, capabilityOverride: ModelCapabilityOverride) => {
    setModelActionPendingID(model.id);
    try {
      await desktopApi.updateModelCapabilities(model.id, { version: model.version, capability_override: capabilityOverride });
      setEditingModel(null);
      await loadModels(modelQuery);
      setError(null);
    } catch (reason) {
      setError(safeMessage(reason, "更新模型能力失败"));
    } finally {
      setModelActionPendingID(null);
    }
  }, [loadModels, modelQuery]);

  const handleUpdateModelLimits = useCallback(async (model: ModelSummary, limitOverride: ModelLimitOverride) => {
    setModelActionPendingID(model.id);
    try {
      await desktopApi.updateModelLimits(model.id, { version: model.version, limit_override: limitOverride });
      setEditingModelLimits(null);
      await loadModels(modelQuery);
      setError(null);
    } catch (reason) {
      setError(safeMessage(reason, "更新模型参数失败"));
    } finally {
      setModelActionPendingID(null);
    }
  }, [loadModels, modelQuery]);

  const handleCreateManualModel = useCallback(async (providerID: string, input: CreateManualModelInput) => {
    setModelActionPendingID("manual-create");
    try {
      await desktopApi.createManualModel(providerID, input);
      setManualModelCreateOpen(false);
      await loadModels(modelQuery);
      setError(null);
    } catch (reason) {
      setError(safeMessage(reason, "创建手工模型失败"));
    } finally {
      setModelActionPendingID(null);
    }
  }, [loadModels, modelQuery]);

  const handleConfirmModelDelete = useCallback(async () => {
    const model = pendingModelDelete?.model;
    if (!model) return;
    setModelActionPendingID(model.id);
    try {
      await desktopApi.deleteManualModel(model.id, model.version);
      setPendingModelDelete(null);
      await loadModels(modelQuery);
      setError(null);
    } catch (reason) {
      setError(safeMessage(reason, "删除手工模型失败"));
    } finally {
      setModelActionPendingID(null);
    }
  }, [loadModels, modelQuery, pendingModelDelete]);

  const handleCancelModelDelete = useCallback(() => {
    if (modelActionPendingID === null) setPendingModelDelete(null);
  }, [modelActionPendingID]);

  const copyText = useCallback(async (value: string, updateState: (state: CopyState) => void) => {
    if (!navigator.clipboard) {
      updateState("failed");
      return;
    }
    try {
      await navigator.clipboard.writeText(value);
      updateState("copied");
    } catch {
      updateState("failed");
    }
  }, []);

  const handleCopyKey = useCallback(() => {
    if (!oneTimeKey) return;
    void copyText(oneTimeKey.key, setCopyState);
  }, [copyText, oneTimeKey]);

  const handleCopyAddress = useCallback(() => {
    if (!runtime?.data_plane_url) return;
    void copyText(runtime.data_plane_url, setAddressCopyState);
  }, [copyText, runtime?.data_plane_url]);

  const handleCopyCodexConfig = useCallback((value: string) => {
    void copyText(value, setCodexCopyState);
  }, [copyText]);

  const handleTestCodexResponses = useCallback(async (kind: "text" | "function") => {
    if (!oneTimeKey || codexModelID.trim() === "") {
      setError("请先创建 Local Key 并填写 Public Model ID");
      return;
    }
    setCodexTestPending(true);
    setCodexTestResult(null);
    try {
      setCodexTestResult(await desktopApi.testLocalResponses({ local_key: oneTimeKey.key, model: codexModelID.trim(), kind }));
    } catch (reason) {
      setError(safeMessage(reason, "本地 Responses 测试失败"));
    } finally {
      setCodexTestPending(false);
    }
  }, [codexModelID, oneTimeKey]);

  const renderPage = () => {
    if (activePage === "services") return <ServicePage dashboard={dashboard} loading={loading} actionPendingID={providerActionPendingID} feedback={providerFeedback} onCreate={() => setProviderCreateOpen(true)} onTest={handleTestProvider} onSyncModels={handleSyncProviderModels} onRequestChange={handleRequestProviderChange} onRequestEdit={setEditingProvider} onRequestDelete={handleRequestProviderDelete} />;
    if (activePage === "models") {
      return <ModelPage runtime={runtime} page={modelsPage} loading={modelLoading} actionPendingID={modelActionPendingID} search={modelSearch} enabledFilter={modelEnabledFilter} capabilityFilter={modelCapabilityFilter} onSearchChange={setModelSearch} onEnabledFilterChange={setModelEnabledFilter} onCapabilityFilterChange={setModelCapabilityFilter} onApplyFilters={handleApplyModelFilters} onNextPage={handleNextModelPage} onRefresh={handleRefreshModels} onRequestChange={handleRequestModelChange} onRequestEdit={setEditingModel} onRequestEditLimits={setEditingModelLimits} onRequestDelete={(model) => setPendingModelDelete({ model })} onCreateManual={() => setManualModelCreateOpen(true)} providers={dashboard?.providers ?? []} />;
    }
    if (activePage === "clients") {
      return <ClientConfigPage runtime={runtime} actionPending={actionPending} keyName={keyName} oneTimeKey={oneTimeKey} copyState={copyState} addressCopyState={addressCopyState} onKeyNameChange={setKeyName} onCreateKey={handleCreateKey} onCopyKey={handleCopyKey} onCopyAddress={handleCopyAddress} codexModelID={codexModelID} codexCopyState={codexCopyState} onCodexModelIDChange={setCodexModelID} onCopyCodexConfig={handleCopyCodexConfig} canTest={oneTimeKey !== null && codexModelID.trim() !== ""} testPending={codexTestPending} testResult={codexTestResult} onTest={handleTestCodexResponses} />;
    }
    if (activePage === "logs") return <DiagnosticsPage />;
    return <SettingsPage runtime={runtime} loading={loading} actionPending={actionPending} onRefresh={handleRefresh} onStartOrRestart={handleStartOrRestart} />;
  };

  const shouldStart = runtime?.state === "stopped" || runtime?.state === "failed";
  const canControlRuntime = Boolean(runtime) && !actionPending;

  return (
    <main className="tool-shell">
      <header className="top-bar">
        <a className="brand" href="#services" onClick={handleBrandClick} aria-label="Aggregation Hub，返回服务页面">
          <span className="brand-mark" aria-hidden="true">A</span>
          <span>Aggregation Hub</span>
        </a>
        <nav className="top-navigation" aria-label="主导航">
          {navigationItems.map((item) => (
            <button key={item.id} type="button" className={activePage === item.id ? "nav-button is-active" : "nav-button"} data-page={item.id} onClick={handleNavigation} aria-current={activePage === item.id ? "page" : undefined}>
              {item.label}
            </button>
          ))}
        </nav>
        <div className="status-control" ref={statusMenuRef}>
          <button type="button" className="status-button" onClick={handleStatusMenuToggle} aria-expanded={statusMenuOpen} aria-haspopup="dialog" aria-controls="runtime-menu">
            <StatusDot state={runtime?.state ?? "loading"} />
            <span>{statusLabel}</span>
          </button>
          {statusMenuOpen ? (
            <section id="runtime-menu" className="status-menu" role="dialog" aria-label="网关状态和操作">
              <div className="status-menu-heading">
                <StatusDot state={runtime?.state ?? "loading"} />
                <strong>{statusDescription}</strong>
              </div>
              <dl>
                <div><dt>地址</dt><dd>{runtime?.data_plane_url ?? "启动后显示"}</dd></div>
                <div><dt>版本</dt><dd>{runtime?.version ?? "—"}</dd></div>
              </dl>
              <div className="status-menu-actions">
                <button type="button" className="button button-secondary" onClick={handleCopyAddress} disabled={!runtime?.data_plane_url}>
                  {addressCopyState === "copied" ? "已复制" : "复制地址"}
                </button>
                <button type="button" className="button button-primary" onClick={shouldStart ? handleStartOrRestart : handleStop} disabled={!runtime || !canControlRuntime}>
                  {shouldStart ? "启动网关" : "停止网关"}
                </button>
              </div>
            </section>
          ) : null}
        </div>
      </header>

      {error ? <p className="error-banner" role="alert">{error}</p> : null}
      <div className="page-container">{renderPage()}</div>
      <CreateProviderDialog open={providerCreateOpen} pending={actionPending} onClose={() => setProviderCreateOpen(false)} onCreate={handleCreateProvider} />
      <EditProviderDialog provider={editingProvider} pending={providerActionPendingID !== null} onClose={() => setEditingProvider(null)} onUpdate={handleUpdateProvider} />
      <ProviderChangeDialog change={pendingProviderChange} pending={providerActionPendingID !== null} onConfirm={handleConfirmProviderChange} onCancel={handleCancelProviderChange} />
      <ProviderDeleteDialog provider={pendingProviderDelete?.provider ?? null} pending={providerActionPendingID !== null} onConfirm={handleConfirmProviderDelete} onCancel={handleCancelProviderDelete} />
      <EditModelCapabilitiesDialog model={editingModel} pending={modelActionPendingID === editingModel?.id} onClose={() => setEditingModel(null)} onUpdate={handleUpdateModelCapabilities} />
      <EditModelLimitsDialog model={editingModelLimits} pending={modelActionPendingID === editingModelLimits?.id} onClose={() => setEditingModelLimits(null)} onUpdate={handleUpdateModelLimits} />
      {manualModelCreateOpen ? <ManualModelDialog providers={dashboard?.providers ?? []} pending={modelActionPendingID === "manual-create"} onClose={() => setManualModelCreateOpen(false)} onCreate={handleCreateManualModel} /> : null}
      <ModelDeleteDialog change={pendingModelDelete} pending={modelActionPendingID !== null} onConfirm={handleConfirmModelDelete} onCancel={handleCancelModelDelete} />
      <ModelChangeDialog change={pendingModelChange} pending={modelActionPendingID !== null} onConfirm={handleConfirmModelChange} onCancel={handleCancelModelChange} />
    </main>
  );
}