package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"aggregationhub.local/core/internal/adapter"
	openaiadapter "aggregationhub.local/core/internal/adapter/openai"
	"aggregationhub.local/core/internal/bootstrap"
	"aggregationhub.local/core/internal/config"
	"aggregationhub.local/core/internal/controlplane"
	"aggregationhub.local/core/internal/credential"
	"aggregationhub.local/core/internal/dataplane"
	"aggregationhub.local/core/internal/gateway"
	"aggregationhub.local/core/internal/health"
	openaiingress "aggregationhub.local/core/internal/ingress/openai_chat"
	"aggregationhub.local/core/internal/management"
	"aggregationhub.local/core/internal/observability"
	"aggregationhub.local/core/internal/provider"
	"aggregationhub.local/core/internal/routing"
	"aggregationhub.local/core/internal/security"
	"aggregationhub.local/core/internal/storage"
	"aggregationhub.local/core/internal/transport"
	"aggregationhub.local/core/migrations"
)

const bootstrapStdinFlag = "--bootstrap-stdin"

func main() { os.Exit(run(os.Args, os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	return runWithRuntime(args, stdin, stdout, stderr, config.Runtime{Version: "0.1.0-rc.6", ListenPort: 18443})
}

func runWithRuntime(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer, cfg config.Runtime) int {
	logger := log.New(stderr, "aggregation-hub-core: ", log.LstdFlags)
	if len(args) != 2 || args[1] != bootstrapStdinFlag {
		logger.Print("必须通过受限 stdin bootstrap 启动")
		return 2
	}
	secrets, err := bootstrap.ReadBootstrapSecrets(stdin)
	if err != nil {
		logger.Print("bootstrap 输入无效")
		return 2
	}
	defer secrets.Clear()
	database, err := openRuntimeDatabase(secrets.DataDir)
	if err != nil {
		logger.Print("运行时数据库初始化失败")
		return 1
	}
	defer database.Close()
	localKeyRepository, err := storage.NewLocalKeyRepository(database)
	if err != nil {
		logger.Print("Local Access Key 仓储初始化失败")
		return 1
	}
	localKeyService, err := security.NewLocalKeyService(localKeyRepository, security.LocalKeyServiceOptions{})
	if err != nil {
		logger.Print("Local Access Key 服务初始化失败")
		return 1
	}
	providerRepository, err := storage.NewProviderRepository(database)
	if err != nil {
		logger.Print("Provider 仓储初始化失败")
		return 1
	}
	credentialStore := credential.NewWindowsStore()
	providerService, err := provider.NewService(providerRepository, credentialStore, provider.ServiceOptions{})
	if err != nil {
		logger.Print("Provider 服务初始化失败")
		return 1
	}
	modelRepository, err := storage.NewModelRepository(database)
	if err != nil {
		logger.Print("模型仓储初始化失败")
		return 1
	}
	modelService, err := provider.NewModelService(modelRepository, provider.ModelServiceOptions{})
	if err != nil {
		logger.Print("模型服务初始化失败")
		return 1
	}
	router, err := routing.New(providerRepository, modelRepository)
	if err != nil {
		logger.Print("模型路由初始化失败")
		return 1
	}
	registry := adapter.NewRegistry()
	if err := openaiadapter.Register(registry); err != nil {
		logger.Print("OpenAI Adapter 注册失败")
		return 1
	}
	gate, err := gateway.New(router, credentialStore, registry, transport.NewFactory(security.NetworkPolicy{}, transport.Options{}))
	if err != nil {
		logger.Print("Gateway 初始化失败")
		return 1
	}
	providerOperations, err := management.NewProviderOperations(providerRepository, modelRepository, credentialStore, registry, transport.NewFactory(security.NetworkPolicy{}, transport.Options{}))
	if err != nil {
		logger.Print("Provider 操作服务初始化失败")
		return 1
	}
	chatHandler, err := openaiingress.NewHandler(gate)
	if err != nil {
		logger.Print("Chat 入口初始化失败")
		return 1
	}
	dataListener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", config.LoopbackHost, cfg.ListenPort))
	if err != nil {
		logger.Print("Data Plane 监听失败")
		return 1
	}
	defer dataListener.Close()
	controlListener, err := net.Listen("tcp", fmt.Sprintf("%s:0", config.LoopbackHost))
	if err != nil {
		logger.Print("Control Plane 监听失败")
		return 1
	}
	defer controlListener.Close()
	startedAt := time.Now().UTC()
	controlPort := controlListener.Addr().(*net.TCPAddr).Port
	ready := bootstrap.ReadyEvent{Event: "ready", ControlURL: fmt.Sprintf("http://%s:%d", config.LoopbackHost, controlPort), DataPlaneURL: fmt.Sprintf("http://%s:%d", config.LoopbackHost, dataListener.Addr().(*net.TCPAddr).Port), PID: os.Getpid()}
	protectedDataPlane := http.NewServeMux()
	protectedDataPlane.Handle("POST /v1/chat/completions", chatHandler)
	protectedDataPlane.Handle("GET /v1/models", dataplane.NewModelsHandler(modelRepository))
	dataRouter := dataplane.NewRouter(health.NewHandler(cfg.Version), protectedDataPlane, localKeyService)
	dataServer := dataplane.NewServer(cfg, dataRouter)
	var shutdownRequested atomic.Bool
	var controlServer *http.Server
	control, err := controlplane.NewServer(controlplane.Options{
		ManagementToken: secrets.ManagementToken,
		Runtime: func() controlplane.RuntimeStatus {
			return controlplane.RuntimeStatus{State: "running", DataPlaneURL: ready.DataPlaneURL, StartedAt: startedAt.Format(time.RFC3339Nano), Version: cfg.Version, LastError: nil}
		},
		ProviderService:    providerService,
		ProviderReader:     providerRepository,
		ProviderOperations: providerOperations,
		ModelService:       modelService,
		ModelReader:        modelRepository,
		LocalKeyService:    localKeyService,
		Shutdown: func(ctx context.Context) error {
			shutdownRequested.Store(true)
			go func() {
				_ = dataServer.Shutdown(ctx)
				if controlServer != nil {
					_ = controlServer.Shutdown(ctx)
				}
			}()
			return nil
		},
	})
	if err != nil {
		logger.Print("Control Plane 初始化失败")
		return 1
	}
	controlServer = &http.Server{Handler: control.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 90 * time.Second}
	defer dataServer.Close()
	defer controlServer.Close()
	serveErrors := make(chan string, 2)
	go serve("Data Plane", dataServer, dataListener, serveErrors)
	go serve("Control Plane", controlServer, controlListener, serveErrors)
	if err := bootstrap.WriteReadyEvent(stdout, ready); err != nil {
		logger.Print("ready 事件写入失败")
		return 1
	}
	logger.Printf("Data Plane 已监听 %s", ready.DataPlaneURL)
	logger.Printf("Control Plane 已监听 %s", ready.ControlURL)
	message := <-serveErrors
	if shutdownRequested.Load() {
		logger.Print("Core 已正常停止")
		return 0
	}
	logger.Print(message)
	return 1
}

func openRuntimeDatabase(dataDir string) (*sql.DB, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	for _, name := range []string{"backups", "logs", "diagnostics"} {
		if err := os.MkdirAll(filepath.Join(dataDir, name), 0o700); err != nil {
			return nil, fmt.Errorf("创建运行目录失败: %w", err)
		}
	}
	database, err := storage.Open(filepath.Join(dataDir, "aggregation-hub.db"))
	if err != nil {
		return nil, err
	}
	if err := storage.Migrate(context.Background(), database, migrations.FS); err != nil {
		_ = database.Close()
		return nil, err
	}
	if _, err := observability.RecoverInFlightRequests(context.Background(), database, time.Now()); err != nil {
		_ = database.Close()
		return nil, err
	}
	return database, nil
}

func serve(name string, server *http.Server, listener net.Listener, failures chan<- string) {
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		failures <- name + " 意外停止"
		return
	}
	failures <- name + " 已停止"
}
