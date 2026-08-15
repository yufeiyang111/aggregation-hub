package routing

import (
	"context"
	"errors"
	"strings"

	"aggregationhub.local/core/internal/provider"
)

type providerFinder interface {
	FindByID(context.Context, string) (provider.Provider, error)
}
type modelFinder interface {
	FindByPublicID(context.Context, string) (provider.ProviderModel, error)
}

type Router struct {
	providers providerFinder
	models    modelFinder
}

func New(providers providerFinder, models modelFinder) (*Router, error) {
	if providers == nil || models == nil {
		return nil, errors.New("Router 仓储不能为空")
	}
	return &Router{providers: providers, models: models}, nil
}

func (router *Router) Resolve(ctx context.Context, publicModelID string, required provider.RequiredCapabilities) (RoutePlan, error) {
	if ctx == nil || !validPublicModelID(publicModelID) {
		return RoutePlan{}, ErrInvalidPublicModelID
	}
	model, err := router.models.FindByPublicID(ctx, publicModelID)
	if err != nil {
		return RoutePlan{}, normalizeModelLookupError(err)
	}
	if !model.Enabled || !routableModelStatus(model.LifecycleStatus) {
		return RoutePlan{}, provider.ErrModelNotFound
	}
	value, err := router.providers.FindByID(ctx, model.ProviderID)
	if err != nil {
		return RoutePlan{}, provider.ErrModelNotFound
	}
	if !value.Enabled || !routableProviderStatus(value.LifecycleStatus) {
		return RoutePlan{}, provider.ErrModelNotFound
	}
	if value.AuthType != provider.AuthTypeNone && value.CredentialRef == nil {
		return RoutePlan{}, provider.ErrModelNotFound
	}
	capabilities, err := provider.EffectiveCapabilities(model.Capabilities, model.CapabilityOverrideJSON)
	if err != nil {
		return RoutePlan{}, err
	}
	if err := capabilities.Validate(required); err != nil {
		return RoutePlan{}, err
	}
	plan := RoutePlan{ProviderID: value.ID, ProviderSlug: value.Slug, AdapterType: value.AdapterType, AuthType: value.AuthType, BaseURL: value.BaseURL, UpstreamModelID: model.UpstreamModelID, Capabilities: capabilities, AdapterConfigJSON: append([]byte(nil), value.AdapterConfigJSON...), Timeout: value.Timeout}
	if value.CredentialRef != nil {
		ref := *value.CredentialRef
		plan.CredentialRef = &ref
	}
	return plan, nil
}

func validPublicModelID(value string) bool {
	separator := strings.IndexByte(value, '/')
	return separator > 0 && separator < len(value)-1 && len(value) <= 304
}
func routableModelStatus(status provider.ModelStatus) bool {
	return status == provider.ModelStatusAvailable || status == provider.ModelStatusDegraded
}
func routableProviderStatus(status provider.ProviderStatus) bool {
	return status == provider.ProviderStatusEnabled || status == provider.ProviderStatusDegraded
}
func normalizeModelLookupError(err error) error {
	if errors.Is(err, provider.ErrModelNotFound) {
		return provider.ErrModelNotFound
	}
	return err
}
