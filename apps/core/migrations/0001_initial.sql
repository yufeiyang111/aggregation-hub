CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  checksum TEXT NOT NULL,
  applied_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS app_settings (
  key TEXT PRIMARY KEY,
  value_json TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS local_access_keys (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  token_hash BLOB NOT NULL UNIQUE,
  token_prefix TEXT NOT NULL,
  token_suffix TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('active','revoked','expired')),
  created_at INTEGER NOT NULL,
  last_used_at INTEGER,
  expires_at INTEGER,
  revoked_at INTEGER
);

CREATE TABLE IF NOT EXISTS providers (
  id TEXT PRIMARY KEY,
  slug TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  adapter_type TEXT NOT NULL,
  auth_type TEXT NOT NULL CHECK (auth_type IN ('api_key','bearer_token','oauth','none')),
  base_url TEXT NOT NULL,
  credential_ref TEXT,
  lifecycle_status TEXT NOT NULL CHECK (
    lifecycle_status IN ('draft','enabled','degraded','auth_required','disabled','deleted')
  ),
  enabled INTEGER NOT NULL CHECK (enabled IN (0,1)),
  timeout_ms INTEGER NOT NULL CHECK (timeout_ms BETWEEN 1000 AND 3600000),
  adapter_config_json TEXT NOT NULL DEFAULT '{}',
  version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  deleted_at INTEGER,
  CHECK (slug = lower(slug)),
  CHECK (length(slug) BETWEEN 1 AND 48),
  CHECK ((auth_type='none' AND credential_ref IS NULL) OR
         (auth_type<>'none' AND credential_ref IS NOT NULL))
);

CREATE TABLE IF NOT EXISTS provider_headers (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL REFERENCES providers(id),
  name TEXT NOT NULL,
  value_plaintext TEXT,
  credential_ref TEXT,
  is_secret INTEGER NOT NULL CHECK (is_secret IN (0,1)),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE(provider_id, name),
  CHECK ((is_secret = 0 AND value_plaintext IS NOT NULL AND credential_ref IS NULL) OR
         (is_secret = 1 AND value_plaintext IS NULL AND credential_ref IS NOT NULL))
);

CREATE TABLE IF NOT EXISTS provider_models (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL REFERENCES providers(id),
  upstream_model_id TEXT NOT NULL,
  public_model_id TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL,
  source TEXT NOT NULL CHECK (source IN ('upstream','adapter_default','manual','oauth')),
  lifecycle_status TEXT NOT NULL CHECK (
    lifecycle_status IN ('available','degraded','missing_upstream','disabled','deleted')
  ),
  enabled INTEGER NOT NULL CHECK (enabled IN (0,1)),
  supports_streaming INTEGER NOT NULL CHECK (supports_streaming IN (0,1)),
  supports_tools INTEGER NOT NULL CHECK (supports_tools IN (0,1)),
  supports_parallel_tools INTEGER NOT NULL CHECK (supports_parallel_tools IN (0,1)),
  supports_reasoning INTEGER NOT NULL CHECK (supports_reasoning IN (0,1)),
  supports_thinking INTEGER NOT NULL CHECK (supports_thinking IN (0,1)),
  supports_vision INTEGER NOT NULL CHECK (supports_vision IN (0,1)),
  context_window_tokens INTEGER CHECK (context_window_tokens IS NULL OR context_window_tokens > 0),
  max_output_tokens INTEGER CHECK (max_output_tokens IS NULL OR max_output_tokens > 0),
  capability_source TEXT NOT NULL,
  capability_override_json TEXT NOT NULL DEFAULT '{}',
  version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  deleted_at INTEGER,
  UNIQUE(provider_id, upstream_model_id)
);

CREATE TABLE IF NOT EXISTS model_prices (
  id TEXT PRIMARY KEY,
  provider_model_id TEXT NOT NULL REFERENCES provider_models(id),
  currency TEXT NOT NULL CHECK (currency = 'USD'),
  input_microusd_per_million INTEGER CHECK (input_microusd_per_million IS NULL OR input_microusd_per_million >= 0),
  output_microusd_per_million INTEGER CHECK (output_microusd_per_million IS NULL OR output_microusd_per_million >= 0),
  cached_input_microusd_per_million INTEGER CHECK (cached_input_microusd_per_million IS NULL OR cached_input_microusd_per_million >= 0),
  cache_write_microusd_per_million INTEGER CHECK (cache_write_microusd_per_million IS NULL OR cache_write_microusd_per_million >= 0),
  reasoning_microusd_per_million INTEGER CHECK (reasoning_microusd_per_million IS NULL OR reasoning_microusd_per_million >= 0),
  source TEXT NOT NULL CHECK (source IN ('provider','manual','estimated')),
  effective_from INTEGER NOT NULL,
  effective_to INTEGER,
  created_at INTEGER NOT NULL,
  CHECK (effective_to IS NULL OR effective_to > effective_from)
);

CREATE TABLE IF NOT EXISTS oauth_accounts (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL REFERENCES providers(id),
  account_label TEXT NOT NULL,
  subject_hash TEXT,
  credential_ref TEXT NOT NULL,
  scopes_json TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL CHECK (status IN ('connected','refreshing','auth_required','revoked')),
  expires_at INTEGER,
  last_refreshed_at INTEGER,
  last_error_code TEXT,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS provider_health_checks (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL REFERENCES providers(id),
  provider_model_id TEXT REFERENCES provider_models(id),
  check_type TEXT NOT NULL CHECK (check_type IN ('connection','models','completion','oauth_refresh')),
  status TEXT NOT NULL CHECK (status IN ('succeeded','failed','skipped')),
  latency_ms INTEGER CHECK (latency_ms IS NULL OR latency_ms >= 0),
  error_code TEXT,
  checked_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS requests (
  id TEXT PRIMARY KEY,
  client_request_id TEXT,
  local_access_key_id TEXT REFERENCES local_access_keys(id),
  provider_id TEXT REFERENCES providers(id),
  provider_model_id TEXT REFERENCES provider_models(id),
  model_price_id TEXT REFERENCES model_prices(id),
  provider_slug_snapshot TEXT NOT NULL,
  public_model_snapshot TEXT NOT NULL,
  upstream_model_snapshot TEXT NOT NULL,
  source_protocol TEXT NOT NULL CHECK (
    source_protocol IN ('anthropic_messages','openai_responses','openai_chat')
  ),
  endpoint TEXT NOT NULL,
  streaming INTEGER NOT NULL CHECK (streaming IN (0,1)),
  status TEXT NOT NULL CHECK (
    status IN ('pending','streaming','succeeded','failed','cancelled','aborted_by_restart')
  ),
  http_status INTEGER,
  error_code TEXT,
  retryable INTEGER NOT NULL DEFAULT 0 CHECK (retryable IN (0,1)),
  usage_source TEXT CHECK (usage_source IN ('upstream_reported','locally_estimated','unknown')),
  input_tokens INTEGER CHECK (input_tokens IS NULL OR input_tokens >= 0),
  output_tokens INTEGER CHECK (output_tokens IS NULL OR output_tokens >= 0),
  cached_input_tokens INTEGER CHECK (cached_input_tokens IS NULL OR cached_input_tokens >= 0),
  cache_write_tokens INTEGER CHECK (cache_write_tokens IS NULL OR cache_write_tokens >= 0),
  reasoning_tokens INTEGER CHECK (reasoning_tokens IS NULL OR reasoning_tokens >= 0),
  estimated_cost_microusd INTEGER,
  request_bytes INTEGER CHECK (request_bytes IS NULL OR request_bytes >= 0),
  response_bytes INTEGER CHECK (response_bytes IS NULL OR response_bytes >= 0),
  duration_ms INTEGER CHECK (duration_ms IS NULL OR duration_ms >= 0),
  first_token_ms INTEGER CHECK (first_token_ms IS NULL OR first_token_ms >= 0),
  created_at INTEGER NOT NULL,
  started_stream_at INTEGER,
  completed_at INTEGER
);

CREATE TABLE IF NOT EXISTS usage_daily (
  date_utc TEXT NOT NULL,
  provider_slug_snapshot TEXT NOT NULL,
  public_model_snapshot TEXT NOT NULL,
  request_count INTEGER NOT NULL DEFAULT 0 CHECK (request_count >= 0),
  succeeded_count INTEGER NOT NULL DEFAULT 0 CHECK (succeeded_count >= 0),
  failed_count INTEGER NOT NULL DEFAULT 0 CHECK (failed_count >= 0),
  cancelled_count INTEGER NOT NULL DEFAULT 0 CHECK (cancelled_count >= 0),
  input_tokens INTEGER NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
  output_tokens INTEGER NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
  cached_input_tokens INTEGER NOT NULL DEFAULT 0 CHECK (cached_input_tokens >= 0),
  cache_write_tokens INTEGER NOT NULL DEFAULT 0 CHECK (cache_write_tokens >= 0),
  reasoning_tokens INTEGER NOT NULL DEFAULT 0 CHECK (reasoning_tokens >= 0),
  estimated_cost_microusd INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (date_utc, provider_slug_snapshot, public_model_snapshot)
);

CREATE TABLE IF NOT EXISTS audit_events (
  id TEXT PRIMARY KEY,
  event_type TEXT NOT NULL,
  entity_type TEXT NOT NULL,
  entity_id TEXT,
  detail_json TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_providers_enabled_status ON providers(enabled, lifecycle_status);
CREATE INDEX IF NOT EXISTS idx_providers_adapter_type ON providers(adapter_type);
CREATE INDEX IF NOT EXISTS idx_provider_headers_provider ON provider_headers(provider_id);
CREATE INDEX IF NOT EXISTS idx_models_provider_enabled ON provider_models(provider_id, enabled, lifecycle_status);
CREATE INDEX IF NOT EXISTS idx_model_prices_model_effective ON model_prices(provider_model_id, effective_from DESC);
CREATE INDEX IF NOT EXISTS idx_oauth_accounts_provider_status ON oauth_accounts(provider_id, status);
CREATE INDEX IF NOT EXISTS idx_health_checks_provider_checked ON provider_health_checks(provider_id, checked_at DESC);
CREATE INDEX IF NOT EXISTS idx_requests_created ON requests(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_requests_provider_created ON requests(provider_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_requests_model_created ON requests(provider_model_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_requests_status_created ON requests(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_requests_protocol_created ON requests(source_protocol, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_created ON audit_events(created_at DESC);