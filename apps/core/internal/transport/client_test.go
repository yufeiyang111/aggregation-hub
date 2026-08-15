package transport_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"aggregationhub.local/core/internal/routing"
	"aggregationhub.local/core/internal/security"
	"aggregationhub.local/core/internal/transport"
)

type fakeResolver struct {
	addresses []netip.Addr
	err       error
}

func (resolver fakeResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return resolver.addresses, resolver.err
}
func localRoute(id string, baseURL string) routing.RoutePlan {
	return routing.RoutePlan{ProviderID: id, AdapterType: "local-openai-compatible", BaseURL: baseURL}
}

func TestFactoryBlocksPrivateDNSForPublicProviderAndReusesClient(t *testing.T) {
	factory := transport.NewFactory(security.NetworkPolicy{}, transport.Options{Resolver: fakeResolver{addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}})
	route := routing.RoutePlan{ProviderID: "public-provider", AdapterType: "openai-compatible", BaseURL: "https://api.example.test/v1"}
	first, err := factory.ForProvider(route)
	if err != nil {
		t.Fatalf("创建 Public Client 失败: %v", err)
	}
	second, err := factory.ForProvider(route)
	if err != nil {
		t.Fatalf("复用 Public Client 失败: %v", err)
	}
	if first != second {
		t.Fatal("同一 Provider 未复用 HTTP Client")
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.example.test/v1/models", nil)
	if err != nil {
		t.Fatalf("创建请求失败: %v", err)
	}
	if _, err := first.Do(request); !errors.Is(err, security.ErrBlockedUpstreamIP) {
		t.Fatalf("私有 DNS 地址错误=%v", err)
	}
}

func TestClientRevalidatesRedirectAndStripsSensitiveHeaders(t *testing.T) {
	received := make(chan http.Header, 1)
	target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		received <- request.Header.Clone()
		_, _ = response.Write([]byte("ok"))
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, target.URL+"/next", http.StatusFound)
	}))
	defer origin.Close()
	factory := transport.NewFactory(security.NetworkPolicy{}, transport.Options{})
	client, err := factory.ForProvider(localRoute("redirect", origin.URL))
	if err != nil {
		t.Fatalf("创建 Local Client 失败: %v", err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, origin.URL+"/start", nil)
	if err != nil {
		t.Fatalf("创建重定向请求失败: %v", err)
	}
	request.Header.Set("Authorization", "Bearer test")
	request.Header.Set("X-API-Key", "test")
	request.Header.Set("Anthropic-API-Key", "test")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("重定向请求失败: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("重定向状态=%d", response.StatusCode)
	}
	select {
	case headers := <-received:
		for _, name := range []string{"Authorization", "X-API-Key", "Anthropic-API-Key"} {
			if headers.Get(name) != "" {
				t.Fatalf("跨主机重定向携带了 %s", name)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("目标服务未收到重定向请求")
	}
}

func TestClientPropagatesCancellationToUpstream(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		close(started)
		<-request.Context().Done()
		close(canceled)
	}))
	defer server.Close()
	factory := transport.NewFactory(security.NetworkPolicy{}, transport.Options{})
	client, err := factory.ForProvider(localRoute("cancel", server.URL))
	if err != nil {
		t.Fatalf("创建 Local Client 失败: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("创建取消请求失败: %v", err)
	}
	completed := make(chan error, 1)
	go func() { _, err := client.Do(request); completed <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("上游未接收到请求")
	}
	cancel()
	select {
	case err := <-completed:
		if err == nil {
			t.Fatal("取消请求不应成功")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("客户端取消未及时返回")
	}
	select {
	case <-canceled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("上游未在 500ms 内收到取消")
	}
}

func TestClientDoesNotDisableTLSVerification(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	factory := transport.NewFactory(security.NetworkPolicy{}, transport.Options{})
	client, err := factory.ForProvider(localRoute("tls", server.URL))
	if err != nil {
		t.Fatalf("创建 TLS Client 失败: %v", err)
	}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if _, err := client.Do(request); err == nil {
		t.Fatal("自签名 TLS 证书不应被静默接受")
	}
}

func TestReadErrorSummaryBoundsBodyAndClosesIt(t *testing.T) {
	body := &trackingReadCloser{Reader: bytes.NewBufferString("0123456789")}
	summary, err := transport.ReadErrorSummary(body, "text/plain; charset=utf-8", 4)
	if err != nil {
		t.Fatalf("读取错误摘要失败: %v", err)
	}
	if summary.Text != "0123" || summary.ContentType != "text/plain" || !body.closed {
		t.Fatalf("错误摘要结果错误: %+v closed=%v", summary, body.closed)
	}
	if contentType := transport.SanitizeContentType("text/plain\r\nX-Unsafe: yes"); contentType != "application/octet-stream" {
		t.Fatalf("危险 Content-Type 未被清洗: %s", contentType)
	}
}

type trackingReadCloser struct {
	io.Reader
	closed bool
}

func (reader *trackingReadCloser) Close() error { reader.closed = true; return nil }
