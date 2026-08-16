package gateway

import (
	"context"
	"errors"
	"net/http"
	"time"

	"aggregationhub.local/core/internal/adapter"
	"aggregationhub.local/core/internal/credential"
	"aggregationhub.local/core/internal/normalize"
	"aggregationhub.local/core/internal/provider"
	"aggregationhub.local/core/internal/routing"
)

var ErrGatewayDependency = errors.New("Gateway 依赖无效")

const defaultRequestTimeout = 60 * time.Second

type Options struct {
	RequestTimeout time.Duration
}

type routeResolver interface {
	Resolve(context.Context, string, provider.RequiredCapabilities) (routing.RoutePlan, error)
}
type credentialReader interface {
	Get(context.Context, credential.Ref) (credential.SecretValue, error)
}
type adapterFactory interface {
	Create(string) (adapter.Adapter, error)
}
type clientFactory interface {
	ForProvider(routing.RoutePlan) (adapter.UpstreamClient, error)
}

// Service 串联路由、凭据、受控 Transport 与 Adapter；不记录或持久化请求正文及上游凭据。
type Service struct {
	router         routeResolver
	credentials    credentialReader
	adapters       adapterFactory
	clients        clientFactory
	requestTimeout time.Duration
}

func New(router routeResolver, credentials credentialReader, adapters adapterFactory, clients clientFactory) (*Service, error) {
	return NewWithOptions(router, credentials, adapters, clients, Options{})
}

// NewWithOptions 允许 Core 在启动时把受控全局请求超时应用到入口请求；更新后需重启 Core 才会生效。
func NewWithOptions(router routeResolver, credentials credentialReader, adapters adapterFactory, clients clientFactory, options Options) (*Service, error) {
	if router == nil || credentials == nil || adapters == nil || clients == nil {
		return nil, ErrGatewayDependency
	}
	if options.RequestTimeout == 0 {
		options.RequestTimeout = defaultRequestTimeout
	}
	if options.RequestTimeout < time.Second || options.RequestTimeout > time.Hour {
		return nil, ErrGatewayDependency
	}
	return &Service{router: router, credentials: credentials, adapters: adapters, clients: clients, requestTimeout: options.RequestTimeout}, nil
}

func (service *Service) Complete(ctx context.Context, request normalize.NormalizedRequest) (normalize.NormalizedResponse, error) {
	ctx, cancel := service.requestContext(ctx)
	defer cancel()
	value, route, credentialValue, client, err := service.prepare(ctx, request)
	if err != nil {
		return normalize.NormalizedResponse{}, err
	}
	defer clearCredential(credentialValue)
	upstream, err := value.BuildRequest(ctx, route, request, credentialValue)
	if err != nil {
		return normalize.NormalizedResponse{}, err
	}
	response, err := client.Do(upstream)
	if err != nil {
		return normalize.NormalizedResponse{}, upstreamUnavailable(route.ProviderID, err)
	}
	return value.ParseResponse(ctx, route, response)
}

func (service *Service) Stream(ctx context.Context, request normalize.NormalizedRequest, emitter normalize.StreamEmitter) error {
	ctx, cancel := service.requestContext(ctx)
	defer cancel()
	value, route, credentialValue, client, err := service.prepare(ctx, request)
	if err != nil {
		return err
	}
	defer clearCredential(credentialValue)
	upstream, err := value.BuildRequest(ctx, route, request, credentialValue)
	if err != nil {
		return err
	}
	response, err := client.Do(upstream)
	if err != nil {
		return upstreamUnavailable(route.ProviderID, err)
	}
	return value.ParseStream(ctx, route, response, emitter)
}

func (service *Service) prepare(ctx context.Context, request normalize.NormalizedRequest) (adapter.Adapter, routing.RoutePlan, adapter.Credential, adapter.UpstreamClient, error) {
	required, err := normalize.ValidateRequest(request, normalize.DefaultValidationLimits())
	if err != nil {
		return nil, routing.RoutePlan{}, adapter.Credential{}, nil, err
	}
	route, err := service.router.Resolve(ctx, request.Model, required)
	if err != nil {
		return nil, routing.RoutePlan{}, adapter.Credential{}, nil, err
	}
	value, err := service.adapters.Create(route.AdapterType)
	if err != nil {
		return nil, routing.RoutePlan{}, adapter.Credential{}, nil, err
	}
	credentialValue := adapter.Credential{AuthType: route.AuthType}
	if route.AuthType != provider.AuthTypeNone {
		if route.CredentialRef == nil {
			return nil, routing.RoutePlan{}, adapter.Credential{}, nil, &adapter.GatewayError{Code: "credential_unavailable", SafeMessage: "服务凭据不可用", HTTPStatus: http.StatusBadGateway, ProviderID: route.ProviderID}
		}
		secret, err := service.credentials.Get(ctx, *route.CredentialRef)
		if err != nil {
			return nil, routing.RoutePlan{}, adapter.Credential{}, nil, &adapter.GatewayError{Code: "credential_unavailable", SafeMessage: "服务凭据不可用", HTTPStatus: http.StatusBadGateway, ProviderID: route.ProviderID, Cause: err}
		}
		credentialValue.Secret = secret
	}
	client, err := service.clients.ForProvider(route)
	if err != nil {
		return nil, routing.RoutePlan{}, adapter.Credential{}, nil, err
	}
	return value, route, credentialValue, client, nil
}

func upstreamUnavailable(providerID string, cause error) *adapter.GatewayError {
	return &adapter.GatewayError{Code: "upstream_unavailable", SafeMessage: "无法连接上游服务", HTTPStatus: http.StatusBadGateway, Retryable: true, ProviderID: providerID, Cause: cause}
}
func clearCredential(value adapter.Credential) {
	for index := range value.Secret.Bytes {
		value.Secret.Bytes[index] = 0
	}
}

func (service *Service) requestContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, service.requestTimeout)
}
