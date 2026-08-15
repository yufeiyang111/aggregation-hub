package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"aggregationhub.local/core/internal/routing"
	"aggregationhub.local/core/internal/security"
)

const (
	defaultConnectTimeout        = 10 * time.Second
	defaultTLSHandshakeTimeout   = 10 * time.Second
	defaultResponseHeaderTimeout = 30 * time.Second
	defaultIdleResponseTimeout   = 90 * time.Second
	defaultMaxErrorBytes         = 16 * 1024
)

var (
	ErrInvalidRequest   = errors.New("上游请求无效")
	ErrTooManyRedirects = errors.New("上游重定向次数过多")
)

type UpstreamClient interface {
	Do(*http.Request) (*http.Response, error)
}
type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type Options struct {
	Resolver               Resolver
	ConnectTimeout         time.Duration
	TLSHandshakeTimeout    time.Duration
	ResponseHeaderTimeout  time.Duration
	IdleResponseTimeout    time.Duration
	MaxRedirects           int
	MaxResponseHeaderBytes int64
}

type Factory struct {
	policy  security.NetworkPolicy
	options Options
	mutex   sync.Mutex
	clients map[string]*client
}

type client struct {
	httpClient *http.Client
	policy     security.NetworkPolicy
	scope      security.NetworkScope
	resolver   Resolver
	idle       time.Duration
}

func NewFactory(policy security.NetworkPolicy, options Options) *Factory {
	if options.Resolver == nil {
		options.Resolver = net.DefaultResolver
	}
	if options.ConnectTimeout <= 0 {
		options.ConnectTimeout = defaultConnectTimeout
	}
	if options.TLSHandshakeTimeout <= 0 {
		options.TLSHandshakeTimeout = defaultTLSHandshakeTimeout
	}
	if options.ResponseHeaderTimeout <= 0 {
		options.ResponseHeaderTimeout = defaultResponseHeaderTimeout
	}
	if options.IdleResponseTimeout <= 0 {
		options.IdleResponseTimeout = defaultIdleResponseTimeout
	}
	if options.MaxRedirects <= 0 {
		options.MaxRedirects = 5
	}
	if options.MaxResponseHeaderBytes <= 0 {
		options.MaxResponseHeaderBytes = 64 * 1024
	}
	return &Factory{policy: policy, options: options, clients: make(map[string]*client)}
}

func (factory *Factory) ForProvider(route routing.RoutePlan) (UpstreamClient, error) {
	if strings.TrimSpace(route.ProviderID) == "" || strings.TrimSpace(route.AdapterType) == "" || strings.TrimSpace(route.BaseURL) == "" {
		return nil, ErrInvalidRequest
	}
	baseURL, err := url.Parse(route.BaseURL)
	if err != nil {
		return nil, security.ErrInvalidUpstreamURL
	}
	scope := scopeForAdapter(route.AdapterType)
	if err := factory.policy.ValidateURL(baseURL, scope); err != nil {
		return nil, err
	}
	factory.mutex.Lock()
	defer factory.mutex.Unlock()
	if existing := factory.clients[route.ProviderID]; existing != nil {
		return existing, nil
	}
	created := &client{policy: factory.policy, scope: scope, resolver: factory.options.Resolver, idle: factory.options.IdleResponseTimeout}
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            created.dialContext(factory.options.ConnectTimeout),
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           32,
		MaxIdleConnsPerHost:    8,
		MaxConnsPerHost:        16,
		IdleConnTimeout:        90 * time.Second,
		TLSHandshakeTimeout:    factory.options.TLSHandshakeTimeout,
		ResponseHeaderTimeout:  factory.options.ResponseHeaderTimeout,
		ExpectContinueTimeout:  time.Second,
		MaxResponseHeaderBytes: factory.options.MaxResponseHeaderBytes,
	}
	created.httpClient = &http.Client{Transport: transport, CheckRedirect: created.checkRedirect(factory.options.MaxRedirects)}
	factory.clients[route.ProviderID] = created
	return created, nil
}

func (client *client) Do(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil {
		return nil, ErrInvalidRequest
	}
	if err := client.policy.ValidateURL(request.URL, client.scope); err != nil {
		return nil, err
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	response.Body = newIdleReadCloser(response.Body, client.idle)
	return response, nil
}

func (client *client) dialContext(timeout time.Duration) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network string, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("解析上游拨号地址失败: %w", err)
		}
		addresses, err := client.resolveAndValidate(ctx, host)
		if err != nil {
			return nil, err
		}
		dialer := net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
		return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].String(), port))
	}
}

func (client *client) resolveAndValidate(ctx context.Context, host string) ([]netip.Addr, error) {
	if parsed, err := netip.ParseAddr(host); err == nil {
		if err := client.policy.ValidateResolvedIP(parsed, client.scope); err != nil {
			return nil, err
		}
		return []netip.Addr{parsed.Unmap()}, nil
	}
	addresses, err := client.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("解析上游主机失败: %w", err)
	}
	if len(addresses) == 0 {
		return nil, errors.New("上游主机未解析到地址")
	}
	for _, address := range addresses {
		if err := client.policy.ValidateResolvedIP(address, client.scope); err != nil {
			return nil, err
		}
	}
	return addresses, nil
}

func (client *client) checkRedirect(maxRedirects int) func(*http.Request, []*http.Request) error {
	return func(next *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return ErrTooManyRedirects
		}
		if err := client.policy.ValidateURL(next.URL, client.scope); err != nil {
			return err
		}
		if len(via) == 0 {
			return nil
		}
		previous := via[len(via)-1]
		if !sameAuthority(previous.URL, next.URL) {
			for _, header := range []string{"Authorization", "Proxy-Authorization", "X-API-Key", "Anthropic-API-Key"} {
				next.Header.Del(header)
			}
		}
		return nil
	}
}

func scopeForAdapter(adapterType string) security.NetworkScope {
	if adapterType == "local-openai-compatible" {
		return security.NetworkScopeLocal
	}
	return security.NetworkScopePublic
}
func sameAuthority(first *url.URL, second *url.URL) bool {
	return strings.EqualFold(first.Host, second.Host)
}

type ErrorSummary struct {
	ContentType string
	Text        string
}

func ReadErrorSummary(body io.ReadCloser, contentType string, maxBytes int64) (ErrorSummary, error) {
	if body == nil || maxBytes < 1 {
		return ErrorSummary{}, ErrInvalidRequest
	}
	defer body.Close()
	contents, err := io.ReadAll(io.LimitReader(body, maxBytes))
	if err != nil {
		return ErrorSummary{}, fmt.Errorf("读取上游错误摘要失败: %w", err)
	}
	return ErrorSummary{ContentType: SanitizeContentType(contentType), Text: sanitizeErrorText(contents)}, nil
}

func SanitizeContentType(value string) string {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return "application/octet-stream"
	}
	mediaType = strings.ToLower(mediaType)
	if mediaType == "application/json" || mediaType == "text/plain" || mediaType == "text/event-stream" {
		return mediaType
	}
	return "application/octet-stream"
}

func sanitizeErrorText(contents []byte) string {
	return strings.TrimSpace(strings.Map(func(character rune) rune {
		if character < 32 && character != '\n' && character != '\r' && character != '\t' {
			return ' '
		}
		return character
	}, string(bytes.ToValidUTF8(contents, []byte("�")))))
}

type idleReadCloser struct {
	body    io.ReadCloser
	timer   *time.Timer
	timeout time.Duration
	mutex   sync.Mutex
	closed  bool
}

func newIdleReadCloser(body io.ReadCloser, timeout time.Duration) io.ReadCloser {
	if body == nil || timeout <= 0 {
		return body
	}
	value := &idleReadCloser{body: body, timeout: timeout}
	value.timer = time.AfterFunc(timeout, func() { _ = value.Close() })
	return value
}
func (value *idleReadCloser) Read(buffer []byte) (int, error) {
	value.resetTimer()
	count, err := value.body.Read(buffer)
	if err == io.EOF {
		_ = value.Close()
	} else {
		value.resetTimer()
	}
	return count, err
}
func (value *idleReadCloser) Close() error {
	value.mutex.Lock()
	defer value.mutex.Unlock()
	if value.closed {
		return nil
	}
	value.closed = true
	if value.timer != nil {
		value.timer.Stop()
	}
	return value.body.Close()
}
func (value *idleReadCloser) resetTimer() {
	value.mutex.Lock()
	defer value.mutex.Unlock()
	if !value.closed && value.timer != nil {
		value.timer.Stop()
		value.timer.Reset(value.timeout)
	}
}
