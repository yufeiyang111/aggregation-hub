package dataplane

import (
	"fmt"
	"net/http"
	"time"

	"aggregationhub.local/core/internal/config"
)

func NewServer(cfg config.Runtime, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf("%s:%d", config.LoopbackHost, cfg.ListenPort),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
}

// NewRouter 显式让 /health 绕过 Local Key；所有其余 Data Plane 路由必须通过 RequireLocalKey。
func NewRouter(healthHandler http.Handler, protectedHandler http.Handler, verifier localKeyVerifier) http.Handler {
	if healthHandler == nil || protectedHandler == nil || verifier == nil {
		panic("Data Plane router 依赖不能为空")
	}
	mux := http.NewServeMux()
	mux.Handle("/health", healthHandler)
	mux.Handle("/", RequireLocalKey(verifier)(protectedHandler))
	return mux
}
