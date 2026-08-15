package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aggregationhub.local/core/internal/bootstrap"
	"aggregationhub.local/core/internal/config"
	"aggregationhub.local/core/internal/storage"
)

func TestOpenRuntimeDatabaseCreatesExpectedLayoutAndMigrates(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "AggregationHub")
	database, err := openRuntimeDatabase(dataDir)
	if err != nil {
		t.Fatalf("open runtime database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	for _, name := range []string{"backups", "logs", "diagnostics", "aggregation-hub.db"} {
		if _, err := os.Stat(filepath.Join(dataDir, name)); err != nil {
			t.Fatalf("expected runtime path %s: %v", name, err)
		}
	}
	var migrationCount int
	if err := database.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount); err != nil {
		t.Fatalf("query migrations: %v", err)
	}
	if migrationCount == 0 {
		t.Fatal("expected initial migration")
	}
}

func TestOpenRuntimeDatabaseCanReopenExistingData(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "AggregationHub")
	database, err := openRuntimeDatabase(dataDir)
	if err != nil {
		t.Fatalf("open runtime database: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	// 本测试只验证第二次启动能够复用同一数据库；请求恢复的状态边界由 observability 包单测覆盖。
	database, err = openRuntimeDatabase(dataDir)
	if err != nil {
		t.Fatalf("reopen runtime database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
}

func TestCoreStartupCreatesLocalKeyAndProtectsDataPlane(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "AggregationHub")
	managementToken := strings.Repeat("m", bootstrap.MinManagementTokenBytes)
	bootstrapBody, err := json.Marshal(map[string]string{"management_token": managementToken, "data_dir": dataDir})
	if err != nil {
		t.Fatal(err)
	}
	bootstrapBody = append(bootstrapBody, '\n')

	outputReader, outputWriter := io.Pipe()
	defer outputReader.Close()
	defer outputWriter.Close()
	var stderr bytes.Buffer
	exitCode := make(chan int, 1)
	go func() {
		exitCode <- runWithRuntime(
			[]string{"aggregation-hub-core", bootstrapStdinFlag},
			bytes.NewReader(bootstrapBody),
			outputWriter,
			&stderr,
			config.Runtime{Version: "0.1.0-rc.6", ListenPort: 0},
		)
	}()

	readyLine := make(chan []byte, 1)
	readyError := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(outputReader).ReadBytes('\n')
		if err != nil {
			readyError <- err
			return
		}
		readyLine <- line
	}()

	var ready bootstrap.ReadyEvent
	select {
	case line := <-readyLine:
		if err := json.Unmarshal(bytes.TrimSpace(line), &ready); err != nil {
			t.Fatalf("解析 Core ready 事件失败: %v", err)
		}
	case err := <-readyError:
		t.Fatalf("读取 Core ready 事件失败: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("等待 Core ready 事件超时")
	}

	client := &http.Client{Timeout: 3 * time.Second}
	assertHTTPStatus(t, client, http.MethodGet, ready.DataPlaneURL+"/health", "", "", http.StatusOK)
	assertHTTPStatus(t, client, http.MethodGet, ready.DataPlaneURL+"/v1/models", "", "", http.StatusUnauthorized)

	key := createLocalKey(t, client, ready.ControlURL, managementToken)
	assertHTTPStatus(t, client, http.MethodGet, ready.DataPlaneURL+"/v1/models", "Authorization", "Bearer "+key, http.StatusOK)
	shutdownCore(t, client, ready.ControlURL, managementToken)

	select {
	case code := <-exitCode:
		if code != 0 {
			t.Fatalf("Core 退出码=%d stderr=%s", code, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("Core 优雅关闭超时 stderr=%s", stderr.String())
	}

	database, err := storage.Open(filepath.Join(dataDir, "aggregation-hub.db"))
	if err != nil {
		t.Fatalf("打开运行数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	var tokenHash []byte
	var prefix, suffix string
	if err := database.QueryRow(`SELECT token_hash,token_prefix,token_suffix FROM local_access_keys LIMIT 1`).Scan(&tokenHash, &prefix, &suffix); err != nil {
		t.Fatalf("读取 Local Key 元数据失败: %v", err)
	}
	if len(tokenHash) != 32 || bytes.Contains(tokenHash, []byte(key)) || prefix == key || suffix == key {
		t.Fatal("SQLite 不得保存完整 Local Key")
	}
}

func createLocalKey(t *testing.T, client *http.Client, controlURL, managementToken string) string {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, controlURL+"/internal/v1/local-keys", strings.NewReader(`{"name":"Core smoke"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Aggregation-Hub-Management-Token", managementToken)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("创建 Local Key 请求失败: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("创建 Local Key status=%d", response.StatusCode)
	}
	var body struct {
		Key         string `json:"key"`
		DisplayOnce bool   `json:"display_once"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil || !strings.HasPrefix(body.Key, "ah_local_") || !body.DisplayOnce {
		t.Fatalf("Local Key 响应无效: %+v %v", body, err)
	}
	return body.Key
}

func shutdownCore(t *testing.T, client *http.Client, controlURL, managementToken string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, controlURL+"/internal/v1/runtime/shutdown", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Aggregation-Hub-Management-Token", managementToken)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("关闭 Core 请求失败: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("关闭 Core status=%d", response.StatusCode)
	}
}

func assertHTTPStatus(t *testing.T, client *http.Client, method, url, header, value string, want int) {
	t.Helper()
	request, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if header != "" {
		request.Header.Set(header, value)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("请求 %s %s 失败: %v", method, url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != want {
		t.Fatalf("请求 %s %s status=%d want=%d", method, url, response.StatusCode, want)
	}
}

func TestCoreOpenAICompatibleLocalProviderEndToEnd(t *testing.T) {
	var chatCalls int
	upstreamCancelled := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/models":
			if request.Method != http.MethodGet {
				writer.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"data":[{"id":"fake-model"}]}`))
		case "/v1/chat/completions":
			chatCalls++
			var body struct {
				Model    string            `json:"model"`
				Stream   bool              `json:"stream"`
				Tools    []json.RawMessage `json:"tools"`
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body.Model != "fake-model" {
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			if body.Stream {
				writer.Header().Set("Content-Type", "text/event-stream")
				if len(body.Messages) == 1 && body.Messages[0].Content == "cancel" {
					_, _ = writer.Write([]byte("data: {\"id\":\"upstream-cancel\",\"model\":\"fake-model\",\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"开始\"}}]}\n\n"))
					if flusher, ok := writer.(http.Flusher); ok {
						flusher.Flush()
					}
					<-request.Context().Done()
					upstreamCancelled <- struct{}{}
					return
				}
				if len(body.Tools) > 0 {
					_, _ = writer.Write([]byte("data: {\"id\":\"upstream-tool-stream\",\"model\":\"fake-model\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_stream_1\",\"type\":\"function\",\"function\":{\"name\":\"weather\",\"arguments\":\"{\\\"city\\\":\"}}]}}]}\n\n"))
					_, _ = writer.Write([]byte("data: {\"id\":\"upstream-tool-stream\",\"model\":\"fake-model\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"北京\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"))
				} else {
					_, _ = writer.Write([]byte("data: {\"id\":\"upstream-stream\",\"model\":\"fake-model\",\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"流式\"}}]}\n\n"))
					_, _ = writer.Write([]byte("data: {\"id\":\"upstream-stream\",\"model\":\"fake-model\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
				}
				_, _ = writer.Write([]byte("data: [DONE]\n\n"))
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			if len(body.Tools) > 0 {
				_, _ = writer.Write([]byte(`{"id":"upstream-tool","model":"fake-model","choices":[{"message":{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"weather","arguments":"{\"city\":\"北京\"}"}}]},"finish_reason":"tool_calls"}]}`))
				return
			}
			_, _ = writer.Write([]byte(`{"id":"upstream-1","model":"fake-model","choices":[{"message":{"content":"来自 Fake Provider"},"finish_reason":"stop"}]}`))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	dataDir := filepath.Join(t.TempDir(), "AggregationHub")
	managementToken := strings.Repeat("n", bootstrap.MinManagementTokenBytes)
	bootstrapBody, err := json.Marshal(map[string]string{"management_token": managementToken, "data_dir": dataDir})
	if err != nil {
		t.Fatal(err)
	}
	bootstrapBody = append(bootstrapBody, '\n')
	outputReader, outputWriter := io.Pipe()
	defer outputReader.Close()
	defer outputWriter.Close()
	var stderr bytes.Buffer
	exitCode := make(chan int, 1)
	go func() {
		exitCode <- runWithRuntime([]string{"aggregation-hub-core", bootstrapStdinFlag}, bytes.NewReader(bootstrapBody), outputWriter, &stderr, config.Runtime{Version: "0.1.0-rc.6", ListenPort: 0})
	}()

	var ready bootstrap.ReadyEvent
	readyLine, err := bufio.NewReader(outputReader).ReadBytes('\n')
	if err != nil {
		t.Fatalf("读取 Core ready 事件失败: %v", err)
	}
	if err := json.Unmarshal(bytes.TrimSpace(readyLine), &ready); err != nil {
		t.Fatalf("解析 Core ready 事件失败: %v", err)
	}
	client := &http.Client{Timeout: 3 * time.Second}

	providerBody := fmt.Sprintf(`{"slug":"fake-local","name":"Fake Local","adapter_type":"local-openai-compatible","auth_type":"none","base_url":%q,"timeout_ms":30000,"adapter_config":{"wire_api":"chat_completions"},"version":0}`, upstream.URL)
	providerResponse := postControlJSON(t, client, ready.ControlURL, managementToken, "/internal/v1/providers", providerBody, http.StatusCreated)
	var createdProvider struct {
		ID      string `json:"id"`
		Version int64  `json:"version"`
	}
	if err := json.Unmarshal(providerResponse, &createdProvider); err != nil || createdProvider.ID == "" || createdProvider.Version != 1 {
		t.Fatalf("创建 Fake Provider 响应错误: %+v, %v", createdProvider, err)
	}

	_ = postControlJSON(t, client, ready.ControlURL, managementToken, "/internal/v1/providers/"+createdProvider.ID+"/sync-models", "", http.StatusOK)
	modelsResponse := getControlJSON(t, client, ready.ControlURL, managementToken, "/internal/v1/models", http.StatusOK)
	var models struct {
		Data []struct {
			ID      string `json:"id"`
			Version int64  `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(modelsResponse, &models); err != nil || len(models.Data) != 1 {
		t.Fatalf("同步模型目录错误: %+v, %v", models, err)
	}
	_ = postControlJSON(t, client, ready.ControlURL, managementToken, "/internal/v1/models/"+models.Data[0].ID+"/enable", `{"version":1}`, http.StatusOK)
	_ = postControlJSON(t, client, ready.ControlURL, managementToken, "/internal/v1/providers/"+createdProvider.ID+"/enable", `{"version":1}`, http.StatusOK)

	key := createLocalKey(t, client, ready.ControlURL, managementToken)
	modelListRequest, err := http.NewRequest(http.MethodGet, ready.DataPlaneURL+"/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	modelListRequest.Header.Set("Authorization", "Bearer "+key)
	modelListResponse, err := client.Do(modelListRequest)
	if err != nil {
		t.Fatalf("读取 Data Plane 模型列表失败: %v", err)
	}
	defer modelListResponse.Body.Close()
	var publicModels struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if modelListResponse.StatusCode != http.StatusOK || json.NewDecoder(modelListResponse.Body).Decode(&publicModels) != nil || len(publicModels.Data) != 1 || publicModels.Data[0].ID != "fake-local/fake-model" {
		t.Fatalf("公开模型列表错误: status=%d body=%+v", modelListResponse.StatusCode, publicModels)
	}

	chatRequest, err := http.NewRequest(http.MethodPost, ready.DataPlaneURL+"/v1/chat/completions", strings.NewReader(`{"model":"fake-local/fake-model","messages":[{"role":"user","content":"L2 sentinel"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	chatRequest.Header.Set("Content-Type", "application/json")
	chatRequest.Header.Set("Authorization", "Bearer "+key)
	chatResponse, err := client.Do(chatRequest)
	if err != nil {
		t.Fatalf("请求 Data Plane Chat 失败: %v", err)
	}
	defer chatResponse.Body.Close()
	var chat struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if chatResponse.StatusCode != http.StatusOK || json.NewDecoder(chatResponse.Body).Decode(&chat) != nil || len(chat.Choices) != 1 || chat.Choices[0].Message.Content != "来自 Fake Provider" || chatCalls != 1 {
		t.Fatalf("Chat 代理结果错误: status=%d calls=%d body=%+v", chatResponse.StatusCode, chatCalls, chat)
	}

	streamRequest, err := http.NewRequest(http.MethodPost, ready.DataPlaneURL+"/v1/chat/completions", strings.NewReader(`{"model":"fake-local/fake-model","stream":true,"messages":[{"role":"user","content":"stream"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	streamRequest.Header.Set("Content-Type", "application/json")
	streamRequest.Header.Set("Authorization", "Bearer "+key)
	streamResponse, err := client.Do(streamRequest)
	if err != nil {
		t.Fatalf("请求流式 Chat 失败: %v", err)
	}
	streamBody, readErr := io.ReadAll(streamResponse.Body)
	_ = streamResponse.Body.Close()
	if readErr != nil || streamResponse.StatusCode != http.StatusOK || !strings.Contains(string(streamBody), "流式") || !strings.Contains(string(streamBody), "data: [DONE]") {
		t.Fatalf("流式 Chat 代理结果错误: status=%d body=%s err=%v", streamResponse.StatusCode, string(streamBody), readErr)
	}

	streamToolRequest, err := http.NewRequest(http.MethodPost, ready.DataPlaneURL+"/v1/chat/completions", strings.NewReader(`{"model":"fake-local/fake-model","stream":true,"messages":[{"role":"user","content":"stream tool"}],"tools":[{"type":"function","function":{"name":"weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	streamToolRequest.Header.Set("Content-Type", "application/json")
	streamToolRequest.Header.Set("Authorization", "Bearer "+key)
	streamToolResponse, err := client.Do(streamToolRequest)
	if err != nil {
		t.Fatalf("请求流式 Tool Chat 失败: %v", err)
	}
	streamToolBody, readErr := io.ReadAll(streamToolResponse.Body)
	_ = streamToolResponse.Body.Close()
	if readErr != nil || streamToolResponse.StatusCode != http.StatusOK || !strings.Contains(string(streamToolBody), "call_stream_1") || !strings.Contains(string(streamToolBody), "weather") || !strings.Contains(string(streamToolBody), "data: [DONE]") {
		t.Fatalf("流式 Tool Chat 代理结果错误: status=%d body=%s err=%v", streamToolResponse.StatusCode, string(streamToolBody), readErr)
	}

	cancelRequest, err := http.NewRequest(http.MethodPost, ready.DataPlaneURL+"/v1/chat/completions", strings.NewReader(`{"model":"fake-local/fake-model","stream":true,"messages":[{"role":"user","content":"cancel"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	cancelRequest.Header.Set("Content-Type", "application/json")
	cancelRequest.Header.Set("Authorization", "Bearer "+key)
	cancelResponse, err := client.Do(cancelRequest)
	if err != nil {
		t.Fatalf("请求可取消流式 Chat 失败: %v", err)
	}
	cancelReader := bufio.NewReader(cancelResponse.Body)
	var cancelLines strings.Builder
	for range 4 {
		line, readErr := cancelReader.ReadString('\n')
		if readErr != nil {
			_ = cancelResponse.Body.Close()
			t.Fatalf("可取消流读取首事件失败: body=%q err=%v", cancelLines.String(), readErr)
		}
		cancelLines.WriteString(line)
		if strings.Contains(cancelLines.String(), "开始") {
			break
		}
	}
	if !strings.Contains(cancelLines.String(), "开始") {
		_ = cancelResponse.Body.Close()
		t.Fatalf("可取消流未收到文本事件: body=%q", cancelLines.String())
	}
	_ = cancelResponse.Body.Close()
	select {
	case <-upstreamCancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("下游取消未传播到上游请求")
	}

	toolRequest, err := http.NewRequest(http.MethodPost, ready.DataPlaneURL+"/v1/chat/completions", strings.NewReader(`{"model":"fake-local/fake-model","messages":[{"role":"user","content":"tool"}],"tools":[{"type":"function","function":{"name":"weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	toolRequest.Header.Set("Content-Type", "application/json")
	toolRequest.Header.Set("Authorization", "Bearer "+key)
	toolResponse, err := client.Do(toolRequest)
	if err != nil {
		t.Fatalf("请求 Tool Chat 失败: %v", err)
	}
	defer toolResponse.Body.Close()
	var toolChat struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if toolResponse.StatusCode != http.StatusOK || json.NewDecoder(toolResponse.Body).Decode(&toolChat) != nil || len(toolChat.Choices) != 1 || len(toolChat.Choices[0].Message.ToolCalls) != 1 || toolChat.Choices[0].Message.ToolCalls[0].ID != "call_1" || toolChat.Choices[0].Message.ToolCalls[0].Function.Name != "weather" || chatCalls != 5 {
		t.Fatalf("Tool Chat 代理结果错误: status=%d calls=%d body=%+v", toolResponse.StatusCode, chatCalls, toolChat)
	}

	shutdownCore(t, client, ready.ControlURL, managementToken)
	select {
	case code := <-exitCode:
		if code != 0 {
			t.Fatalf("Core 退出码=%d stderr=%s", code, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("Core 优雅关闭超时 stderr=%s", stderr.String())
	}
}

func postControlJSON(t *testing.T, client *http.Client, controlURL, managementToken, path, body string, want int) []byte {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, controlURL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Aggregation-Hub-Management-Token", managementToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Control 请求失败: %v", err)
	}
	defer response.Body.Close()
	result, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != want {
		t.Fatalf("Control %s 状态=%d want=%d body=%s err=%v", path, response.StatusCode, want, string(result), err)
	}
	return result
}

func getControlJSON(t *testing.T, client *http.Client, controlURL, managementToken, path string, want int) []byte {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, controlURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Aggregation-Hub-Management-Token", managementToken)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Control 请求失败: %v", err)
	}
	defer response.Body.Close()
	result, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != want {
		t.Fatalf("Control %s 状态=%d want=%d body=%s err=%v", path, response.StatusCode, want, string(result), err)
	}
	return result
}
