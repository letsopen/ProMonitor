package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"promonitor/server/internal/auth"
	"promonitor/server/internal/ingest"
	"promonitor/server/internal/metrics"
	"promonitor/server/internal/store"
)

// API 聚合所有 HTTP 处理器
type API struct {
	store    *store.Store
	auth     *auth.Auth
	ingest   *ingest.Ingestor
	pingType string // "icmp" | "tcp"，来自环境变量 PING_TYPE
}

func New(s *store.Store, a *auth.Auth, ing *ingest.Ingestor, pingType string) *API {
	return &API{store: s, auth: a, ingest: ing, pingType: pingType}
}

// Routes 注册全部路由
func (api *API) Routes(r *chi.Mux) {
	r.Post("/api/ingest", api.ingest.Handle) // HMAC 验签
	r.Get("/api/servers", api.listServers)   // 匿名
	r.Get("/api/servers/latest", api.latest) // 匿名（轮询）
	r.Get("/api/servers/{id}/metrics", api.history)
	r.Get("/api/ping-config", api.pingConfig) // 匿名：被控拉取探测配置

	r.Post("/api/admin/login", api.login)
	r.Post("/api/admin/logout", api.logout)

	r.Group(func(pr chi.Router) {
		pr.Use(api.auth.RequireAdmin)
		pr.Post("/api/admin/change-password", api.changePassword)
		pr.Post("/api/admin/servers", api.createServer)
		pr.Put("/api/admin/servers/{id}", api.updateServer)
		pr.Delete("/api/admin/servers/{id}", api.deleteServer)
		// Ping 节点管理（存库，主控统一维护）
		pr.Get("/api/admin/ping-nodes", api.listPingNodes)
		pr.Post("/api/admin/ping-nodes", api.createPingNode)
		pr.Put("/api/admin/ping-nodes/{id}", api.updatePingNode)
		pr.Delete("/api/admin/ping-nodes/{id}", api.deletePingNode)
	})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (api *API) listServers(w http.ResponseWriter, r *http.Request) {
	list, err := api.store.ListServers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"servers": list})
}

func (api *API) latest(w http.ResponseWriter, r *http.Request) {
	list, err := api.store.ListServers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	m := map[string]interface{}{}
	for _, s := range list {
		m[s.ID] = s
	}
	writeJSON(w, m)
}

func (api *API) history(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	from := parseTime(r.URL.Query().Get("from"), time.Now().Add(-24*time.Hour))
	to := parseTime(r.URL.Query().Get("to"), time.Now())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 4320
	}
	rows, err := api.store.GetHistory(r.Context(), id, from, to, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"metrics": rows})
}

// pingConfig 返回主控统一管理的网络探测配置，供所有被控拉取（无需鉴权，
// 因为被控在注册/HMAC 之前就需要它来决定怎么测延迟）。节点列表从库读取。
func (api *API) pingConfig(w http.ResponseWriter, r *http.Request) {
	nodes, err := api.store.ListPingNodes(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, metrics.PingConfigView{Type: api.pingType, Nodes: nodes})
}

// --- Ping 节点管理（admin） ---

func (api *API) listPingNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := api.store.ListPingNodes(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"nodes": nodes})
}

type pingNodeReq struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
	Port int    `json:"port"`
}

func (api *API) createPingNode(w http.ResponseWriter, r *http.Request) {
	var req pingNodeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.IP = strings.TrimSpace(req.IP)
	if req.Name == "" || req.IP == "" {
		http.Error(w, "name and ip required", http.StatusBadRequest)
		return
	}
	if req.Port <= 0 || req.Port > 65535 {
		req.Port = 80
	}
	id, err := api.store.CreatePingNode(r.Context(), req.Name, req.IP, req.Port)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "id": id})
}

func (api *API) updatePingNode(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	var req pingNodeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.IP = strings.TrimSpace(req.IP)
	if req.Name == "" || req.IP == "" {
		http.Error(w, "name and ip required", http.StatusBadRequest)
		return
	}
	if req.Port <= 0 || req.Port > 65535 {
		req.Port = 80
	}
	if err := api.store.UpdatePingNode(r.Context(), id, req.Name, req.IP, req.Port); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (api *API) deletePingNode(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := api.store.DeletePingNode(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func parseTime(s string, def time.Time) time.Time {
	if s == "" {
		return def
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.UnixMilli(ms)
	}
	return def
}

func (api *API) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	token, err := api.auth.Login(req.Username, req.Password)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	api.auth.WriteCookie(w, token)
	writeJSON(w, map[string]bool{"ok": true})
}

func (api *API) logout(w http.ResponseWriter, r *http.Request) {
	api.auth.ClearCookie(w)
	writeJSON(w, map[string]bool{"ok": true})
}

func (api *API) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := api.auth.ChangePassword(req.Old, req.New); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (api *API) createServer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		IP   string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := api.store.CreateServer(r.Context(), req.ID, req.Name, req.IP); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (api *API) updateServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Name string `json:"name"`
		IP   string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := api.store.UpdateServer(r.Context(), id, req.Name, req.IP); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (api *API) deleteServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := api.store.DeleteServer(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
