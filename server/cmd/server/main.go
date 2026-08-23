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

	"promonitor/server/internal/api"
	"promonitor/server/internal/auth"
	"promonitor/server/internal/config"
	"promonitor/server/internal/history"
	"promonitor/server/internal/ingest"
	"promonitor/server/internal/store"
)

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

	ing := ingest.New(st, cfg.HMACSecret)
	hist := history.New(st, cfg.HMACSecret)
	ap := api.New(st, a, ing, hist, cfg.PingType)

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

// spaFallback 托管前端 dist/：先放行静态资源，其余非 /api 路径回退到 index.html，
// 从而支持 Vue History 路由（/detail/:id 等）。
func spaFallback(r chi.Router, root http.Dir) {
	rootStr := string(root)
	// 显式托管静态资源，避免 http.FileServer 在 NotFound 回调里触发重定向循环。
	r.Handle("/assets/*", http.FileServer(root))
	r.Handle("/favicon.svg", http.FileServer(root))
	r.Handle("/icons.svg", http.FileServer(root))
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/api") {
			http.NotFound(w, req)
			return
		}
		// 真实文件（如 vite 构建产物）直接按文件服务；不存在的路径回退到 SPA 入口。
		if _, err := os.Stat(filepath.Join(rootStr, req.URL.Path)); err == nil {
			http.FileServer(root).ServeHTTP(w, req)
			return
		}
		serveIndexHTML(w, rootStr)
	})
}

func serveIndexHTML(w http.ResponseWriter, root string) {
	path := filepath.Join(root, "index.html")
	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, "index.html not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}
