package ingest

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"

	"promonitor/server/internal/metrics"
	"promonitor/server/internal/store"
)

// Ingestor 处理被控实时上报：HMAC 验签 + 刷新 latest_snapshot（不持久化原始数据）。
// 10 分钟历史聚合已下沉到被控端完成，通过 /api/history 上报。
type Ingestor struct {
	store  *store.Store
	secret string
}

func New(s *store.Store, secret string) *Ingestor {
	return &Ingestor{store: s, secret: secret}
}

func (ing *Ingestor) Handle(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	mac := hmac.New(sha256.New, []byte(ing.secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(r.Header.Get("X-Signature")), []byte(expected)) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	var smpl metrics.Sample
	if err := json.Unmarshal(body, &smpl); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if smpl.ServerID == "" {
		http.Error(w, "server_id required", http.StatusBadRequest)
		return
	}
	// 实时数据仅刷新 latest_snapshot，供前端展示最新状态，不落历史库。
	// 历史聚合由被控在本地完成并通过 /api/history 上传。
	cores := smpl.CPUCores
	memTotal := smpl.MemTotal
	diskTotal := smpl.DiskTotal
	snap := metrics.AggRow{
		ServerID:  smpl.ServerID,
		TS:        smpl.TS,
		CPU:       smpl.CPU,
		Mem:       smpl.Mem,
		Disk:      smpl.Disk,
		NetIn:     smpl.NetIn,
		NetOut:    smpl.NetOut,
		CPUCores:  &cores,
		MemTotal:  &memTotal,
		DiskTotal: &diskTotal,
		Pings:     smpl.Pings,
	}
	// UpsertServer 同步保证列表立即可见；UpdateSnapshot 刷新实时快照
	if err := ing.store.UpsertServer(r.Context(), smpl.ServerID, smpl.Name, smpl.IP); err != nil {
		log.Printf("upsert server failed: %v", err)
	}
	go func() {
		if err := ing.store.UpdateSnapshot(context.Background(), snap); err != nil {
			log.Printf("update snapshot failed: %v", err)
		}
	}()
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"ok":true}`))
}
