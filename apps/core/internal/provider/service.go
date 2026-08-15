package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"aggregationhub.local/core/internal/credential"
	"aggregationhub.local/core/internal/id"
)

type ServiceOptions struct {
	Now    func() time.Time
	NewID  func(time.Time) (string, error)
	NewRef func(string, time.Time, func(time.Time) (string, error)) (credential.Ref, error)
}

type Service struct {
	repository ProviderRepository
	store      credential.Store
	now        func() time.Time
	newID      func(time.Time) (string, error)
	newRef     func(string, time.Time, func(time.Time) (string, error)) (credential.Ref, error)
}

func NewService(repository ProviderRepository, store credential.Store, options ServiceOptions) (*Service, error) {
	if repository == nil {
		return nil, errors.New("Provider 仓储不能为空")
	}
	if store == nil {
		return nil, errors.New("CredentialStore 不能为空")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.NewID == nil {
		options.NewID = id.RandomULID
	}
	if options.NewRef == nil {
		options.NewRef = defaultCredentialRef
	}
	return &Service{repository: repository, store: store, now: options.Now, newID: options.NewID, newRef: options.NewRef}, nil
}

func (service *Service) Create(ctx context.Context, input CreateProviderInput) (ProviderDTO, error) {
	if ctx == nil {
		return ProviderDTO{}, fmt.Errorf("创建 Provider: %w", ErrInvalidProvider)
	}
	normalized, err := normalizeCreateInput(input)
	if err != nil {
		return ProviderDTO{}, err
	}
	now := service.now().UTC()
	providerID, err := service.newID(now)
	if err != nil {
		return ProviderDTO{}, fmt.Errorf("生成 Provider 标识失败: %w", err)
	}
	value := Provider{ID: providerID, Slug: normalized.Slug, Name: normalized.Name, AdapterType: normalized.AdapterType, AuthType: normalized.AuthType, BaseURL: normalized.BaseURL, LifecycleStatus: ProviderStatusDraft, Enabled: false, Timeout: normalized.Timeout, AdapterConfigJSON: cloneJSON(normalized.AdapterConfigJSON), Version: 1, CreatedAt: now, UpdatedAt: now}

	var maskedHint string
	if normalized.Credential != nil {
		ref, err := service.newRef(providerID, now, service.newID)
		if err != nil {
			return ProviderDTO{}, fmt.Errorf("生成 Provider 凭据引用失败: %w", err)
		}
		secret := normalized.Credential.Clone()
		if err := service.store.Put(ctx, ref, secret); err != nil {
			return ProviderDTO{}, fmt.Errorf("保存 Provider 凭据失败: %w", err)
		}
		value.CredentialRef = &ref
		maskedHint = maskSecret(secret)
	}
	audit, err := service.newAudit(now, "provider_created", value.ID, map[string]any{"provider_slug": value.Slug})
	if err != nil {
		return ProviderDTO{}, err
	}
	if err := service.repository.Create(ctx, value, audit); err != nil {
		if value.CredentialRef != nil {
			return ProviderDTO{}, service.compensateNewCredential(ctx, *value.CredentialRef, err)
		}
		return ProviderDTO{}, err
	}
	return providerDTO(value, maskedHint), nil
}

func (service *Service) Update(ctx context.Context, id string, input UpdateProviderInput) (ProviderDTO, error) {
	if ctx == nil || strings.TrimSpace(id) == "" || input.ExpectedVersion < 1 {
		return ProviderDTO{}, ErrInvalidProvider
	}
	current, err := service.repository.FindByID(ctx, id)
	if err != nil {
		return ProviderDTO{}, err
	}
	if current.Version != input.ExpectedVersion {
		return ProviderDTO{}, ErrStaleResource
	}
	normalized, err := normalizeUpdateInput(current, input)
	if err != nil {
		return ProviderDTO{}, err
	}
	now := service.now().UTC()
	updated := current
	updated.Name = normalized.Name
	updated.BaseURL = normalized.BaseURL
	updated.Timeout = normalized.Timeout
	updated.AdapterConfigJSON = cloneJSON(normalized.AdapterConfigJSON)
	updated.UpdatedAt = now
	if normalized.Credential != nil || updated.BaseURL != current.BaseURL {
		updated.LifecycleStatus = ProviderStatusDraft
		updated.Enabled = false
	}

	var newRef *credential.Ref
	var maskedHint string
	if normalized.Credential != nil {
		ref, err := service.newRef(current.ID, now, service.newID)
		if err != nil {
			return ProviderDTO{}, fmt.Errorf("生成替换凭据引用失败: %w", err)
		}
		secret := normalized.Credential.Clone()
		if err := service.store.Put(ctx, ref, secret); err != nil {
			return ProviderDTO{}, fmt.Errorf("保存替换凭据失败: %w", err)
		}
		updated.CredentialRef = &ref
		newRef = &ref
		maskedHint = maskSecret(secret)
	}
	auditType := "provider_updated"
	if newRef != nil {
		auditType = "provider_credential_replaced"
	}
	audit, err := service.newAudit(now, auditType, current.ID, map[string]any{"provider_slug": current.Slug})
	if err != nil {
		if newRef != nil {
			return ProviderDTO{}, service.compensateNewCredential(ctx, *newRef, err)
		}
		return ProviderDTO{}, err
	}
	persisted, err := service.repository.Update(ctx, updated, input.ExpectedVersion, audit)
	if err != nil {
		if newRef != nil {
			return ProviderDTO{}, service.compensateNewCredential(ctx, *newRef, err)
		}
		return ProviderDTO{}, err
	}
	if newRef != nil && current.CredentialRef != nil {
		if err := service.store.Delete(ctx, *current.CredentialRef); err != nil {
			cleanupError := service.recordCleanupFailure(ctx, now, current.ID, "replace")
			return providerDTO(persisted, maskedHint), errors.Join(ErrCredentialCleanup, err, cleanupError)
		}
	}
	return providerDTO(persisted, maskedHint), nil
}

func (service *Service) Enable(ctx context.Context, id string, expectedVersion int64) (ProviderDTO, error) {
	return service.setEnabled(ctx, id, expectedVersion, true)
}

func (service *Service) Disable(ctx context.Context, id string, expectedVersion int64) (ProviderDTO, error) {
	return service.setEnabled(ctx, id, expectedVersion, false)
}

func (service *Service) Delete(ctx context.Context, id string, expectedVersion int64) error {
	if ctx == nil || strings.TrimSpace(id) == "" || expectedVersion < 1 {
		return ErrInvalidProvider
	}
	current, err := service.repository.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if current.Version != expectedVersion {
		return ErrStaleResource
	}
	now := service.now().UTC()
	audit, err := service.newAudit(now, "provider_deleted", current.ID, map[string]any{"provider_slug": current.Slug})
	if err != nil {
		return err
	}
	if err := service.repository.SoftDelete(ctx, current.ID, expectedVersion, audit); err != nil {
		return err
	}
	if current.CredentialRef == nil {
		return nil
	}
	if err := service.store.Delete(ctx, *current.CredentialRef); err != nil {
		cleanupError := service.recordCleanupFailure(ctx, now, current.ID, "delete")
		return errors.Join(ErrCredentialCleanup, err, cleanupError)
	}
	return nil
}

func (service *Service) setEnabled(ctx context.Context, id string, expectedVersion int64, enabled bool) (ProviderDTO, error) {
	if ctx == nil || strings.TrimSpace(id) == "" || expectedVersion < 1 {
		return ProviderDTO{}, ErrInvalidProvider
	}
	current, err := service.repository.FindByID(ctx, id)
	if err != nil {
		return ProviderDTO{}, err
	}
	if current.Version != expectedVersion {
		return ProviderDTO{}, ErrStaleResource
	}
	if enabled && current.AuthType != AuthTypeNone && current.CredentialRef == nil {
		return ProviderDTO{}, ErrInvalidProvider
	}
	now := service.now().UTC()
	auditType := "provider_disabled"
	if enabled {
		auditType = "provider_enabled"
	}
	audit, err := service.newAudit(now, auditType, current.ID, map[string]any{"provider_slug": current.Slug})
	if err != nil {
		return ProviderDTO{}, err
	}
	value, err := service.repository.SetEnabled(ctx, current.ID, expectedVersion, enabled, audit)
	if err != nil {
		return ProviderDTO{}, err
	}
	return providerDTO(value, ""), nil
}

func (service *Service) compensateNewCredential(ctx context.Context, ref credential.Ref, cause error) error {
	if err := service.store.Delete(ctx, ref); err != nil {
		return errors.Join(cause, fmt.Errorf("%w: 新凭据未能清理", ErrCredentialCleanup))
	}
	return cause
}

func (service *Service) recordCleanupFailure(ctx context.Context, now time.Time, providerID string, operation string) error {
	audit, err := service.newAudit(now, "provider_credential_cleanup_failed", providerID, map[string]any{"operation": operation})
	if err != nil {
		return err
	}
	return service.repository.AppendAudit(ctx, audit)
}

func (service *Service) newAudit(now time.Time, eventType string, entityID string, detail map[string]any) (AuditEvent, error) {
	identifier, err := service.newID(now)
	if err != nil {
		return AuditEvent{}, fmt.Errorf("生成审计标识失败: %w", err)
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return AuditEvent{}, fmt.Errorf("编码审计详情失败: %w", err)
	}
	return AuditEvent{ID: identifier, EventType: eventType, EntityType: "provider", EntityID: entityID, DetailJSON: encoded, CreatedAt: now}, nil
}

func normalizeCreateInput(input CreateProviderInput) (CreateProviderInput, error) {
	if !validSlug(input.Slug) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 128 || strings.TrimSpace(input.AdapterType) == "" || len(input.AdapterType) > 80 || input.Timeout < time.Second || input.Timeout > time.Hour {
		return CreateProviderInput{}, ErrInvalidProvider
	}
	baseURL, err := normalizeBaseURL(input.BaseURL, input.AdapterType)
	if err != nil {
		return CreateProviderInput{}, err
	}
	config, err := normalizedJSONObject(input.AdapterConfigJSON)
	if err != nil {
		return CreateProviderInput{}, err
	}
	if input.AuthType == AuthTypeOAuth {
		return CreateProviderInput{}, ErrOAuthNotConfigured
	}
	if input.AuthType != AuthTypeNone && input.AuthType != AuthTypeAPIKey && input.AuthType != AuthTypeBearerToken {
		return CreateProviderInput{}, ErrInvalidProvider
	}
	if input.AuthType == AuthTypeNone && input.Credential != nil {
		return CreateProviderInput{}, ErrInvalidProvider
	}
	if input.AuthType != AuthTypeNone && (input.Credential == nil || len(input.Credential.Bytes) == 0 || len(input.Credential.Bytes) > 5120) {
		return CreateProviderInput{}, ErrInvalidProvider
	}
	input.BaseURL = baseURL
	input.AdapterConfigJSON = config
	return input, nil
}

func normalizeUpdateInput(current Provider, input UpdateProviderInput) (UpdateProviderInput, error) {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 128 || input.Timeout < time.Second || input.Timeout > time.Hour {
		return UpdateProviderInput{}, ErrInvalidProvider
	}
	baseURL, err := normalizeBaseURL(input.BaseURL, current.AdapterType)
	if err != nil {
		return UpdateProviderInput{}, err
	}
	config, err := normalizedJSONObject(input.AdapterConfigJSON)
	if err != nil {
		return UpdateProviderInput{}, err
	}
	if current.AuthType == AuthTypeNone && input.Credential != nil {
		return UpdateProviderInput{}, ErrInvalidProvider
	}
	if input.Credential != nil && (len(input.Credential.Bytes) == 0 || len(input.Credential.Bytes) > 5120) {
		return UpdateProviderInput{}, ErrInvalidProvider
	}
	input.BaseURL = baseURL
	input.AdapterConfigJSON = config
	return input, nil
}

func normalizeBaseURL(raw string, adapterType string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || (!isLocalAdapter(adapterType) && parsed.Scheme != "https") || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return "", ErrInvalidProvider
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return parsed.String(), nil
}

func isLocalAdapter(adapterType string) bool { return adapterType == "local-openai-compatible" }

func normalizedJSONObject(value json.RawMessage) (json.RawMessage, error) {
	if len(value) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(value, &object); err != nil || containsSensitiveConfigKey(object) {
		return nil, ErrInvalidProvider
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return nil, ErrInvalidProvider
	}
	return encoded, nil
}

func containsSensitiveConfigKey(object map[string]json.RawMessage) bool {
	for key, rawValue := range object {
		normalizedKey := strings.ToLower(key)
		if strings.Contains(normalizedKey, "secret") || strings.Contains(normalizedKey, "token") || strings.Contains(normalizedKey, "password") || strings.Contains(normalizedKey, "credential") || normalizedKey == "api_key" || normalizedKey == "authorization" {
			return true
		}
		var nested map[string]json.RawMessage
		if json.Unmarshal(rawValue, &nested) == nil && containsSensitiveConfigKey(nested) {
			return true
		}
	}
	return false
}

func validSlug(value string) bool {
	if len(value) < 1 || len(value) > 48 || value != strings.ToLower(value) {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-') {
			return false
		}
	}
	return true
}

func defaultCredentialRef(providerID string, now time.Time, newID func(time.Time) (string, error)) (credential.Ref, error) {
	nonce, err := newID(now)
	if err != nil {
		return "", err
	}
	ref := credential.Ref("providers/" + providerID + "/auth/" + nonce)
	if err := ref.Validate(); err != nil {
		return "", err
	}
	return ref, nil
}

func providerDTO(value Provider, maskedHint string) ProviderDTO {
	credentialState := CredentialStateDTO{Configured: value.CredentialRef != nil}
	if credentialState.Configured {
		credentialState.MaskedHint = maskedHint
		if credentialState.MaskedHint == "" {
			credentialState.MaskedHint = "已配置"
		}
	}
	return ProviderDTO{ID: value.ID, Slug: value.Slug, Name: value.Name, AdapterType: value.AdapterType, AuthType: value.AuthType, BaseURL: value.BaseURL, LifecycleStatus: value.LifecycleStatus, Enabled: value.Enabled, TimeoutMS: value.Timeout.Milliseconds(), AdapterConfig: SanitizeAdapterConfig(value.AdapterConfigJSON), Version: value.Version, Credential: credentialState}
}

func maskSecret(value credential.SecretValue) string {
	if len(value.Bytes) <= 7 {
		return "已配置"
	}
	return string(value.Bytes[:3]) + "…" + string(value.Bytes[len(value.Bytes)-4:])
}

func cloneJSON(value json.RawMessage) json.RawMessage { return append(json.RawMessage(nil), value...) }
