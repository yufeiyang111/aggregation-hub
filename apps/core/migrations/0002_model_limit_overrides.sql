ALTER TABLE provider_models
  ADD COLUMN context_window_override_tokens INTEGER CHECK (
    context_window_override_tokens IS NULL OR context_window_override_tokens > 0
  );

ALTER TABLE provider_models
  ADD COLUMN max_output_override_tokens INTEGER CHECK (
    max_output_override_tokens IS NULL OR max_output_override_tokens > 0
  );