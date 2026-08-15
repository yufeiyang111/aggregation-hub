package controlplane_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aggregationhub.local/core/internal/controlplane"
	"aggregationhub.local/core/internal/provider"
)

type fakeProviderService struct {
	createInput provider.CreateProviderInput
	updateInput provider.UpdateProviderInput
	deleteID    string
	deleteVer   int64
	enable      bool
	actionID    string
	actionVer   int64
	createCalls int
	updateCalls int
	deleteCalls int
	actionCalls int
	err         error
}

func (service *fakeProviderService) Create(_ context.Context, input provider.CreateProviderInput) (provider.ProviderDTO, error) {
	service.createCalls++
	service.createInput = input
	return providerDTO(), service.err
}
func (service *fakeProviderService) Update(_ context.Context, _ string, input provider.UpdateProviderInput) (provider.ProviderDTO, error) {
	service.updateCalls++
	service.updateInput = input
	return providerDTO(), service.err
}
func (service *fakeProviderService) Enable(_ context.Context, id string, version int64) (provider.ProviderDTO, error) {
	service.actionCalls++
	service.enable, service.actionID, service.actionVer = true, id, version
	return providerDTO(), service.err
}
func (service *fakeProviderService) Disable(_ context.Context, id string, version int64) (provider.ProviderDTO, error) {
	service.actionCalls++
	service.enable, service.actionID, service.actionVer = false, id, version
	return providerDTO(), service.err
}
func (service *fakeProviderService) Delete(_ context.Context, id string, version int64) error {
	service.deleteCalls++
	service.deleteID, service.deleteVer = id, version
	return service.err
}

type fakeProviderReader struct {
	find  provider.Provider
	page  provider.ProviderPage
	err   error
	query provider.ProviderPageQuery
}

func (reader *fakeProviderReader) FindByID(_ context.Context, _ string) (provider.Provider, error) {
	return reader.find, reader.err
}
func (reader *fakeProviderReader) List(_ context.Context, query provider.ProviderPageQuery) (provider.ProviderPage, error) {
	reader.query = query
	return reader.page, reader.err
}

func providerDTO() provider.ProviderDTO {
	return provider.ProviderDTO{ID: "provider-1", Slug: "package-a", Name: "Package A", AdapterType: "openai-compatible", AuthType: provider.AuthTypeAPIKey, BaseURL: "https://example.test", LifecycleStatus: provider.ProviderStatusDraft, TimeoutMS: 30000, Version: 2, Credential: provider.CredentialStateDTO{Configured: true, MaskedHint: "...abcd"}}
}

func newProviderServer(t *testing.T, service *fakeProviderService, reader *fakeProviderReader) *controlplane.Server {
	t.Helper()
	server, err := controlplane.NewServer(controlplane.Options{
		ManagementToken: testManagementToken,
		Runtime:         func() controlplane.RuntimeStatus { return controlplane.RuntimeStatus{} },
		Shutdown:        func(context.Context) error { return nil },
		ProviderService: service,
		ProviderReader:  reader,
	})
	if err != nil {
		t.Fatalf("创建 Provider Control Plane 失败: %v", err)
	}
	return server
}

func providerRequest(method, target, body string, token bool) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if token {
		request.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	}
	return request
}

func TestProviderRoutesRequireManagementToken(t *testing.T) {
	server := newProviderServer(t, &fakeProviderService{}, &fakeProviderReader{})
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, providerRequest(http.MethodGet, "/internal/v1/providers", "", false))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("缺少管理令牌 status=%d", response.Code)
	}
}

func TestProviderCreateValidatesInputAndNeverEchoesCredential(t *testing.T) {
	service := &fakeProviderService{}
	server := newProviderServer(t, service, &fakeProviderReader{})
	secret := "test-only-provider-secret"
	body := `{"slug":"package-a","name":"Package A","adapter_type":"openai-compatible","auth_type":"api_key","base_url":"https://example.test","timeout_ms":30000,"adapter_config":{},"credential":"` + secret + `"}`
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, providerRequest(http.MethodPost, "/internal/v1/providers", body, true))
	if response.Code != http.StatusCreated || service.createCalls != 1 {
		t.Fatalf("创建 Provider status=%d calls=%d", response.Code, service.createCalls)
	}
	if service.createInput.Credential == nil || string(service.createInput.Credential.Bytes) != secret || service.createInput.Timeout != 30*time.Second {
		t.Fatalf("创建输入没有正确传递: %+v", service.createInput)
	}
	if strings.Contains(response.Body.String(), secret) || strings.Contains(response.Body.String(), "credential_ref") {
		t.Fatalf("响应泄露凭据: %s", response.Body.String())
	}

	invalid := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalid, providerRequest(http.MethodPost, "/internal/v1/providers", `{"unexpected":true}`, true))
	if invalid.Code != http.StatusBadRequest || service.createCalls != 1 {
		t.Fatalf("非法输入 status=%d calls=%d", invalid.Code, service.createCalls)
	}
}

func TestProviderCreateRejectsOversizeUnknownLengthBody(t *testing.T) {
	service := &fakeProviderService{}
	server := newProviderServer(t, service, &fakeProviderReader{})
	body := `{"slug":"package-a","name":"Package A","adapter_type":"openai-compatible","auth_type":"none","base_url":"https://example.test","timeout_ms":30000,"adapter_config":{}}` + strings.Repeat(" ", 64*1024)
	request := providerRequest(http.MethodPost, "/internal/v1/providers", body, true)
	request.ContentLength = -1
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || service.createCalls != 0 {
		t.Fatalf("超限请求应被拒绝 status=%d calls=%d", response.Code, service.createCalls)
	}
}
func TestProviderReadUpdateActionsAndDeleteUseSafeContracts(t *testing.T) {
	service := &fakeProviderService{}
	reader := &fakeProviderReader{find: provider.Provider{ID: "provider-1", Slug: "package-a", Name: "Package A", AdapterType: "openai-compatible", AuthType: provider.AuthTypeAPIKey, BaseURL: "https://example.test", LifecycleStatus: provider.ProviderStatusDraft, Timeout: 30 * time.Second, Version: 2}}
	server := newProviderServer(t, service, reader)

	get := httptest.NewRecorder()
	server.Handler().ServeHTTP(get, providerRequest(http.MethodGet, "/internal/v1/providers/provider-1", "", true))
	if get.Code != http.StatusOK || strings.Contains(get.Body.String(), "credential_ref") {
		t.Fatalf("读取 Provider 响应错误: %d %s", get.Code, get.Body.String())
	}

	updateBody := `{"name":"Updated","base_url":"https://example.test","timeout_ms":30000,"adapter_config":{},"version":2}`
	update := httptest.NewRecorder()
	server.Handler().ServeHTTP(update, providerRequest(http.MethodPatch, "/internal/v1/providers/provider-1", updateBody, true))
	if update.Code != http.StatusOK || service.updateCalls != 1 || service.updateInput.ExpectedVersion != 2 {
		t.Fatalf("更新 Provider 失败 status=%d input=%+v", update.Code, service.updateInput)
	}

	for _, action := range []struct {
		path    string
		enabled bool
	}{
		{path: "/internal/v1/providers/provider-1/enable", enabled: true},
		{path: "/internal/v1/providers/provider-1/disable", enabled: false},
	} {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, providerRequest(http.MethodPost, action.path, `{"version":2}`, true))
		if response.Code != http.StatusOK || service.enable != action.enabled || service.actionID != "provider-1" || service.actionVer != 2 {
			t.Fatalf("动作 %s 失败 status=%d state=%t id=%q version=%d", action.path, response.Code, service.enable, service.actionID, service.actionVer)
		}
	}

	deleteResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleteResponse, providerRequest(http.MethodDelete, "/internal/v1/providers/provider-1", `{"version":2}`, true))
	if deleteResponse.Code != http.StatusNoContent || service.deleteCalls != 1 || service.deleteID != "provider-1" || service.deleteVer != 2 {
		t.Fatalf("删除 Provider 失败 status=%d id=%q version=%d", deleteResponse.Code, service.deleteID, service.deleteVer)
	}
}

func TestProviderErrorsUseSafeStatusCodes(t *testing.T) {
	service := &fakeProviderService{err: provider.ErrStaleResource}
	server := newProviderServer(t, service, &fakeProviderReader{})
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, providerRequest(http.MethodPatch, "/internal/v1/providers/provider-1", `{"name":"Updated","base_url":"https://example.test","timeout_ms":30000,"adapter_config":{},"version":2}`, true))
	if response.Code != http.StatusConflict {
		t.Fatalf("版本冲突 status=%d", response.Code)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Error.Code != "stale_resource" {
		t.Fatalf("错误形状错误: %+v %v", body, err)
	}

	reader := &fakeProviderReader{err: errors.New("database detail must stay private")}
	server = newProviderServer(t, &fakeProviderService{}, reader)
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, providerRequest(http.MethodGet, "/internal/v1/providers", "", true))
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "database detail") {
		t.Fatalf("内部错误泄露: %d %s", response.Code, response.Body.String())
	}
}

func TestProviderListUsesBoundedPagination(t *testing.T) {
	reader := &fakeProviderReader{page: provider.ProviderPage{Items: []provider.Provider{{ID: "provider-1", Slug: "package-a", Name: "Package A", AdapterType: "openai-compatible", AuthType: provider.AuthTypeNone, BaseURL: "https://example.test", LifecycleStatus: provider.ProviderStatusDraft, Timeout: time.Second, Version: 1}}, NextCursor: "provider-1"}}
	server := newProviderServer(t, &fakeProviderService{}, reader)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, providerRequest(http.MethodGet, "/internal/v1/providers?page_size=201", "", true))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("越界分页 status=%d", response.Code)
	}
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, providerRequest(http.MethodGet, "/internal/v1/providers?page_size=50&cursor=provider-0", "", true))
	if response.Code != http.StatusOK || reader.query.PageSize != 50 || reader.query.Cursor != "provider-0" || strings.Contains(response.Body.String(), "credential_ref") {
		t.Fatalf("列表 Provider 响应错误 status=%d query=%+v body=%s", response.Code, reader.query, response.Body.String())
	}
}
