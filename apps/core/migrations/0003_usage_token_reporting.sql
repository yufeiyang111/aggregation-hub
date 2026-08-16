ALTER TABLE usage_daily ADD COLUMN input_token_reported_count INTEGER NOT NULL DEFAULT 0 CHECK (input_token_reported_count >= 0);
ALTER TABLE usage_daily ADD COLUMN output_token_reported_count INTEGER NOT NULL DEFAULT 0 CHECK (output_token_reported_count >= 0);
ALTER TABLE usage_daily ADD COLUMN cached_input_token_reported_count INTEGER NOT NULL DEFAULT 0 CHECK (cached_input_token_reported_count >= 0);
ALTER TABLE usage_daily ADD COLUMN reasoning_token_reported_count INTEGER NOT NULL DEFAULT 0 CHECK (reasoning_token_reported_count >= 0);
ALTER TABLE usage_daily ADD COLUMN cache_eligible_input_tokens INTEGER NOT NULL DEFAULT 0 CHECK (cache_eligible_input_tokens >= 0);
ALTER TABLE usage_daily ADD COLUMN cache_eligible_cached_input_tokens INTEGER NOT NULL DEFAULT 0 CHECK (cache_eligible_cached_input_tokens >= 0);
