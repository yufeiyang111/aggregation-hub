package controlplane_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"aggregationhub.local/core/internal/controlplane"
	"aggregationhub.local/core/internal/provider"
)

type recordingModelReader struct {
	page  provider.ModelPage
	err   error
	query provider.ModelPageQuery
}

func (reader *recordingModelReader) List(_ context.Context, query provider.ModelPageQuery) (provider.ModelPage, error) {
	reader.query = query
	return reader.page, reader.err
}

type recordingModelService struct {
	enableID       string
	enableVersion  int64
	disableID      string
	disableVersion int64
	updateID       string
	updateInput    provider.UpdateModelCapabilitiesInput
	limitsID       string
	limitsInput    provider.UpdateModelLimitsInput
	createProvider string
	createInput    provider.CreateManualModelInput
	deleteID       string
	deleteVersion  int64
	result         provider.ModelDTO
	err            error
}

func (service *recordingModelService) Enable(_ context.Context, id string, version int64) (provider.ModelDTO, error) {
	service.enableID = id
	service.enableVersion = version
	return service.result, service.err
}

func (service *recordingModelService) Disable(_ context.Context, id string, version int64) (provider.ModelDTO, error) {
	service.disableID = id
	service.disableVersion = version
	return service.result, service.err
}

func (service *recordingModelService) UpdateCapabilities(_ context.Context, id string, input provider.UpdateModelCapabilitiesInput) (provider.ModelDTO, error) {
	service.updateID = id
	service.updateInput = input
	return service.result, service.err
}

func (service *recordingModelService) UpdateLimits(_ context.Context, id string, input provider.UpdateModelLimitsInput) (provider.ModelDTO, error) {
	service.limitsID = id
	service.limitsInput = input
	return service.result, service.err
}

func (service *recordingModelService) CreateManual(_ context.Context, input provider.CreateManualModelInput) (provider.ModelDTO, error) {
	service.createProvider = input.ProviderID
	service.createInput = input
	return service.result, service.err
}

func (service *recordingModelService) DeleteManual(_ context.Context, id string, version int64) error {
	service.deleteID = id
	service.deleteVersion = version
	return service.err
}

func TestControlPlaneModelRoutesValidateAuthQueryAndVersion(t *testing.T) {
	model := provider.ProviderModel{ID: "01H00000000000000000000301", ProviderID: "01H00000000000000000000300", UpstreamModelID: "gpt-test", PublicModelID: "bundle/gpt-test", DisplayName: "测试模型", Source: provider.ModelSourceUpstream, LifecycleStatus: provider.ModelStatusAvailable, Enabled: false, Capabilities: provider.Capabilities{Streaming: true, Tools: true}, CapabilitySource: "upstream", CapabilityOverrideJSON: json.RawMessage(`{}`), Version: 3}
	reader := &recordingModelReader{page: provider.ModelPage{Items: []provider.ProviderModel{model}}}
	service := &recordingModelService{result: provider.ModelDTO{ID: model.ID, PublicModelID: model.PublicModelID, Enabled: true, Version: 4}}
	server, err := controlplane.NewServer(controlplane.Options{ManagementToken: testManagementToken, Runtime: func() controlplane.RuntimeStatus { return controlplane.RuntimeStatus{} }, Shutdown: func(context.Context) error { return nil }, ModelReader: reader, ModelService: service})
	if err != nil {
		t.Fatalf("创建 Control Plane 失败: %v", err)
	}

	unauthorized := httptest.NewRequest(http.MethodGet, "/internal/v1/models", nil)
	unauthorizedResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("未授权模型列表状态=%d", unauthorizedResponse.Code)
	}

	invalidQuery := httptest.NewRequest(http.MethodGet, "/internal/v1/models?enabled=1", nil)
	invalidQuery.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	invalidQueryResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalidQueryResponse, invalidQuery)
	if invalidQueryResponse.Code != http.StatusBadRequest {
		t.Fatalf("非法筛选状态=%d body=%s", invalidQueryResponse.Code, invalidQueryResponse.Body.String())
	}

	list := httptest.NewRequest(http.MethodGet, "/internal/v1/models?page_size=25&provider_id=01H00000000000000000000300&enabled=false&capability=tools&search=%E6%B5%8B%E8%AF%95", nil)
	list.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	listResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusOK || reader.query.PageSize != 25 || reader.query.ProviderID != model.ProviderID || reader.query.Enabled == nil || *reader.query.Enabled || reader.query.Capability != "tools" || reader.query.Search != "测试" {
		t.Fatalf("模型列表错误: status=%d query=%+v body=%s", listResponse.Code, reader.query, listResponse.Body.String())
	}
	var listed struct {
		Data []provider.ModelDTO `json:"data"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listed); err != nil || len(listed.Data) != 1 || listed.Data[0].PublicModelID != model.PublicModelID || !listed.Data[0].Capabilities.Tools {
		t.Fatalf("模型列表响应错误: %+v, %v", listed, err)
	}

	invalidBody := httptest.NewRequest(http.MethodPost, "/internal/v1/models/"+model.ID+"/enable", bytes.NewBufferString(`{"version":3,"extra":true}`))
	invalidBody.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	invalidBodyResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalidBodyResponse, invalidBody)
	if invalidBodyResponse.Code != http.StatusBadRequest || service.enableID != "" {
		t.Fatalf("未知字段不应调用服务: status=%d id=%q", invalidBodyResponse.Code, service.enableID)
	}

	enable := httptest.NewRequest(http.MethodPost, "/internal/v1/models/"+model.ID+"/enable", bytes.NewBufferString(`{"version":3}`))
	enable.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	enableResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(enableResponse, enable)
	if enableResponse.Code != http.StatusOK || service.enableID != model.ID || service.enableVersion != 3 {
		t.Fatalf("启用模型路由错误: status=%d id=%q version=%d", enableResponse.Code, service.enableID, service.enableVersion)
	}

	invalidOverride := httptest.NewRequest(http.MethodPatch, "/internal/v1/models/"+model.ID, bytes.NewBufferString(`{"version":3,"capability_override":{"unsupported":true}}`))
	invalidOverride.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	invalidOverrideResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalidOverrideResponse, invalidOverride)
	if invalidOverrideResponse.Code != http.StatusBadRequest || service.updateID != "" {
		t.Fatalf("未知能力字段不应调用服务: status=%d id=%q", invalidOverrideResponse.Code, service.updateID)
	}

	update := httptest.NewRequest(http.MethodPatch, "/internal/v1/models/"+model.ID, bytes.NewBufferString(`{"version":3,"capability_override":{"supports_tools":false}}`))
	update.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	updateResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(updateResponse, update)
	if updateResponse.Code != http.StatusOK || service.updateID != model.ID || service.updateInput.ExpectedVersion != 3 || service.updateInput.CapabilityOverride.Tools == nil || *service.updateInput.CapabilityOverride.Tools {
		t.Fatalf("更新模型能力路由错误: status=%d id=%q input=%+v", updateResponse.Code, service.updateID, service.updateInput)
	}
}

func TestControlPlaneModelRoutesReturnSafeMappedErrors(t *testing.T) {
	reader := &recordingModelReader{err: errors.New("数据库故障")}
	service := &recordingModelService{err: provider.ErrModelNotFound}
	server, err := controlplane.NewServer(controlplane.Options{ManagementToken: testManagementToken, Runtime: func() controlplane.RuntimeStatus { return controlplane.RuntimeStatus{} }, Shutdown: func(context.Context) error { return nil }, ModelReader: reader, ModelService: service})
	if err != nil {
		t.Fatalf("创建 Control Plane 失败: %v", err)
	}
	list := httptest.NewRequest(http.MethodGet, "/internal/v1/models", nil)
	list.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	listResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusInternalServerError || bytes.Contains(listResponse.Body.Bytes(), []byte("数据库故障")) {
		t.Fatalf("内部错误泄露或状态错误: status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	enable := httptest.NewRequest(http.MethodPost, "/internal/v1/models/missing/enable", bytes.NewBufferString(`{"version":1}`))
	enable.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	enableResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(enableResponse, enable)
	if enableResponse.Code != http.StatusNotFound || !bytes.Contains(enableResponse.Body.Bytes(), []byte("model_not_found")) {
		t.Fatalf("模型不存在错误映射错误: status=%d body=%s", enableResponse.Code, enableResponse.Body.String())
	}
}

func TestControlPlaneManualModelAndLimitRoutes(t *testing.T) {
	service := &recordingModelService{result: provider.ModelDTO{ID: "01H00000000000000000000401", Version: 4}}
	server, err := controlplane.NewServer(controlplane.Options{ManagementToken: testManagementToken, Runtime: func() controlplane.RuntimeStatus { return controlplane.RuntimeStatus{} }, Shutdown: func(context.Context) error { return nil }, ModelReader: &recordingModelReader{}, ModelService: service})
	if err != nil {
		t.Fatalf("创建 Control Plane 失败: %v", err)
	}
	create := httptest.NewRequest(http.MethodPost, "/internal/v1/providers/01H00000000000000000000400/models", bytes.NewBufferString(`{"upstream_model_id":"manual","display_name":"手工","capabilities":{"streaming":true,"tools":false,"parallel_tools":false,"reasoning":false,"thinking":false,"vision":false},"context_window_tokens":128000}`))
	create.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	createResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(createResponse, create)
	if createResponse.Code != http.StatusCreated || service.createProvider != "01H00000000000000000000400" || service.createInput.UpstreamModelID != "manual" || !service.createInput.Capabilities.Streaming {
		t.Fatalf("手工模型创建路由错误: status=%d provider=%q input=%+v", createResponse.Code, service.createProvider, service.createInput)
	}
	invalidCreate := httptest.NewRequest(http.MethodPost, "/internal/v1/providers/provider/models", bytes.NewBufferString(`{"upstream_model_id":"manual","display_name":"手工","capabilities":{"streaming":true}}`))
	invalidCreate.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	invalidCreateResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalidCreateResponse, invalidCreate)
	if invalidCreateResponse.Code != http.StatusBadRequest {
		t.Fatalf("缺少能力字段不应创建: status=%d", invalidCreateResponse.Code)
	}
	limits := httptest.NewRequest(http.MethodPatch, "/internal/v1/models/01H00000000000000000000401/limits", bytes.NewBufferString(`{"version":3,"limit_override":{"context_window_tokens":100000,"max_output_tokens":4096}}`))
	limits.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	limitsResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(limitsResponse, limits)
	if limitsResponse.Code != http.StatusOK || service.limitsID == "" || service.limitsInput.ExpectedVersion != 3 || service.limitsInput.LimitOverride.ContextWindowTokens == nil || *service.limitsInput.LimitOverride.ContextWindowTokens != 100000 {
		t.Fatalf("模型参数路由错误: status=%d input=%+v", limitsResponse.Code, service.limitsInput)
	}
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/internal/v1/models/01H00000000000000000000401", bytes.NewBufferString(`{"version":4}`))
	deleteRequest.Header.Set(controlplane.ManagementTokenHeader, testManagementToken)
	deleteResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusNoContent || service.deleteID == "" || service.deleteVersion != 4 {
		t.Fatalf("手工模型删除路由错误: status=%d id=%q version=%d", deleteResponse.Code, service.deleteID, service.deleteVersion)
	}
}
