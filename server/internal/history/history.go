package history

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"net/http"

	"promonitor/server/internal/metrics"
	"promonitor/server/internal/store"
)

// Handler 处理被控端按整数 10 分钟边界聚合后的批量上报。
type Handler struct {
	store  *store.Store
	secret string
}

func New(s *store.Store, secret string) *Handler {
	return &Handler{store: s, secret: secret}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(r.Header.Get("X-Signature")), []byte(expected)) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	var payload metrics.HistoryPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if payload.ServerID == "" {
		http.Error(w, "server_id required", http.StatusBadRequest)
		return
	}
	// 同步更新被控元数据，保证列表可见
	if err := h.store.UpsertServer(r.Context(), payload.ServerID, payload.Name, payload.IP); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(payload.Rows) > 0 {
		if err := h.store.UpsertAggBatch(r.Context(), payload.ServerID, payload.Rows); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"ok":true}`))
}
