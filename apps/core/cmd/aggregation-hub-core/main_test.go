package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
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
	assertHTTPStatus(t, client, http.MethodGet, ready.DataPlaneURL+"/v1/models", "Authorization", "Bearer "+key, http.StatusNotFound)
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
