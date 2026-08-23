package ingest

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"net/http"

	"promonitor/server/internal/aggregator"
	"promonitor/server/internal/metrics"
)

// Ingestor 处理被控上报：HMAC 验签 + 写入聚合缓冲（不持久化原始数据）
type Ingestor struct {
	agg    *aggregator.Aggregator
	secret string
}

func New(agg *aggregator.Aggregator, secret string) *Ingestor {
	return &Ingestor{agg: agg, secret: secret}
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
	ing.agg.Add(smpl)
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"ok":true}`))
}
