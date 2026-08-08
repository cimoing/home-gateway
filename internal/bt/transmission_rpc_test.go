package bt

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestTransmissionRPCSessionHandshake(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "sess-1")
			w.WriteHeader(http.StatusConflict)
			return
		}
		if got := r.Header.Get("X-Transmission-Session-Id"); got != "sess-1" {
			t.Errorf("session id %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": "success",
			"arguments": map[string]any{
				"version": "4.0.0",
			},
		})
	}))
	t.Cleanup(server.Close)

	rpc := newTransmissionRPC(server.URL, "", "")
	var out struct {
		Version string `json:"version"`
	}
	if err := rpc.call("session-get", map[string]any{"fields": []string{"version"}}, &out); err != nil {
		t.Fatalf("call: %v", err)
	}
	if out.Version != "4.0.0" {
		t.Fatalf("version %q", out.Version)
	}
	if hits.Load() != 2 {
		t.Fatalf("hits=%d want 2", hits.Load())
	}
}

func TestTransmissionRPCAuthAndDuplicate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "tr" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("X-Transmission-Session-Id", "ok")
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		switch req.Method {
		case "torrent-add":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": "success",
				"arguments": map[string]any{
					"torrent-duplicate": map[string]any{
						"id":         7,
						"hashString": "ABCDEF",
						"name":       "dup",
					},
				},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"result": "success", "arguments": map[string]any{}})
		}
	}))
	t.Cleanup(server.Close)

	rpc := newTransmissionRPC(server.URL, "tr", "secret")
	raw, result, err := rpc.callResult("torrent-add", map[string]any{"filename": "magnet:?xt=urn:btih:abc"})
	if err != nil {
		t.Fatalf("callResult: %v", err)
	}
	if result != "success" {
		t.Fatalf("result %q", result)
	}
	var payload struct {
		Duplicate *transmissionTorrent `json:"torrent-duplicate"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Duplicate == nil || payload.Duplicate.ID != 7 {
		t.Fatalf("duplicate %#v", payload.Duplicate)
	}
}

func TestBpsToTransmissionLimit(t *testing.T) {
	limit, limited := bpsToTransmissionLimit(0)
	if limited || limit != 0 {
		t.Fatalf("unlimited: %d %v", limit, limited)
	}
	limit, limited = bpsToTransmissionLimit(512)
	if !limited || limit != 1 {
		t.Fatalf("sub-kib: %d %v", limit, limited)
	}
	limit, limited = bpsToTransmissionLimit(10 * 1024)
	if !limited || limit != 10 {
		t.Fatalf("10kib: %d %v", limit, limited)
	}
}

func TestSessionLimitArgs(t *testing.T) {
	args := sessionLimitArgs(10*1024, 0, 2)
	if args["speed-limit-down"] != int64(10) || args["speed-limit-down-enabled"] != true {
		t.Fatalf("download %#v", args)
	}
	if args["speed-limit-up-enabled"] != false {
		t.Fatalf("upload unlimited %#v", args)
	}
	if args["seedRatioLimit"] != float64(2) || args["seedRatioLimited"] != true {
		t.Fatalf("seed %#v", args)
	}
	disabled := sessionLimitArgs(0, 0, 0)
	if disabled["seedRatioLimited"] != false || disabled["seedRatioLimit"] != float64(0) {
		t.Fatalf("seed disabled %#v", disabled)
	}
}
