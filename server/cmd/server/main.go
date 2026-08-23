package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"promonitor/server/internal/aggregator"
	"promonitor/server/internal/api"
	"promonitor/server/internal/auth"
	"promonitor/server/internal/config"
	"promonitor/server/internal/ingest"
	"promonitor/server/internal/store"
)

const pingNodes = 50

func main() {
	cfg := config.Load()
	if cfg.DBPath == "" {
		log.Fatal("DB_PATH is required")
	}
	if cfg.HMACSecret == "" {
		log.Fatal("HMAC_SECRET is required")
	}

	ctx := context.Background()
	st, err := store.New(ctx, cfg.DBPath)
	if err != nil {
		log.Fatalf("store init failed: %v", err)
	}
	defer st.Close()

	a := auth.New(st, cfg.AdminUser, cfg.SessionSecret)
	if cfg.AdminPass != "" {
		if err := a.Seed(cfg.AdminPass); err != nil {
			log.Fatalf("seed admin failed: %v", err)
		}
	}

	// 启动时立即执行一次 30 天保留清理，并每日定时清理 + 回收空间
	if n, err := st.PruneOld(ctx, 30); err != nil {
		log.Printf("warn: initial prune failed: %v", err)
	} else if n > 0 {
		_ = st.Vacuum(ctx)
		log.Printf("pruned %d stale rows on startup", n)
	}
	go func() {
		t := time.NewTicker(24 * time.Hour)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n, err := st.PruneOld(ctx, 30); err != nil {
					log.Printf("warn: prune failed: %v", err)
				} else if n > 0 {
					_ = st.Vacuum(ctx)
					log.Printf("pruned %d stale rows", n)
				}
			}
		}
	}()

	agg := aggregator.New(st, pingNodes)
	agg.Start(ctx)

	ing := ingest.New(agg, cfg.HMACSecret)
	ap := api.New(st, a, ing, api.PingConfigView{
		Method: cfg.PingMethod,
		Port:   cfg.PingPort,
		Nodes:  cfg.PingNodes,
	})

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)
	ap.Routes(r)
	spaFallback(r, http.Dir(cfg.FrontendDir))

	addr := ":" + cfg.Port
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}
	log.Printf("ProMonitor listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Signature")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// spaFallback 用 NotFound 处理器托管前端 dist/，未知路径回退 index.html，
// 从而支持 Vue History 路由（/detail/:id 等）。与 chi 版本的 catch-all 语法无关，最稳妥。
func spaFallback(r chi.Router, root http.Dir) {
	fs := http.FileServer(root)
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/api") {
			http.NotFound(w, req)
			return
		}
		if _, err := os.Stat(filepath.Join(string(root), req.URL.Path)); os.IsNotExist(err) {
			req.URL.Path = "/index.html"
		}
		fs.ServeHTTP(w, req)
	})
}
