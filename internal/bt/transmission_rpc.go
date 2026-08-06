package bt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type transmissionRPC struct {
	http      *http.Client
	url       string
	username  string
	password  string
	mu        sync.Mutex
	sessionID string
}

type rpcRequest struct {
	Method    string         `json:"method"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Tag       int            `json:"tag,omitempty"`
}

type rpcResponse struct {
	Result    string          `json:"result"`
	Arguments json.RawMessage `json:"arguments"`
	Tag       int             `json:"tag"`
}

func newTransmissionRPC(url, username, password string) *transmissionRPC {
	return &transmissionRPC{
		http: &http.Client{Timeout: 60 * time.Second},
		url:  strings.TrimRight(strings.TrimSpace(url), "/"),
		username: username,
		password: password,
	}
}

func (c *transmissionRPC) call(method string, args map[string]any, out any) error {
	return c.callInto(method, args, out)
}

func (c *transmissionRPC) callResult(method string, args map[string]any) (json.RawMessage, string, error) {
	payload, err := json.Marshal(rpcRequest{Method: method, Arguments: args})
	if err != nil {
		return nil, "", err
	}
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequest(http.MethodPost, c.url, bytes.NewReader(payload))
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("Content-Type", "application/json")
		c.mu.Lock()
		sessionID := c.sessionID
		c.mu.Unlock()
		if sessionID != "" {
			req.Header.Set("X-Transmission-Session-Id", sessionID)
		}
		if c.username != "" || c.password != "" {
			req.SetBasicAuth(c.username, c.password)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, "", fmt.Errorf("%w: transmission rpc: %v", ErrUnavailable, err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, "", fmt.Errorf("%w: read transmission rpc: %v", ErrUnavailable, readErr)
		}
		if resp.StatusCode == http.StatusConflict {
			next := resp.Header.Get("X-Transmission-Session-Id")
			if next == "" {
				return nil, "", fmt.Errorf("%w: transmission session id missing", ErrUnavailable)
			}
			c.mu.Lock()
			c.sessionID = next
			c.mu.Unlock()
			continue
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, "", fmt.Errorf("%w: transmission authentication failed", ErrUnavailable)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, "", fmt.Errorf("%w: transmission rpc status %d", ErrUnavailable, resp.StatusCode)
		}
		var decoded rpcResponse
		if err := json.Unmarshal(body, &decoded); err != nil {
			return nil, "", fmt.Errorf("%w: decode transmission rpc: %v", ErrUnavailable, err)
		}
		return decoded.Arguments, decoded.Result, nil
	}
	return nil, "", fmt.Errorf("%w: transmission session handshake failed", ErrUnavailable)
}

func (c *transmissionRPC) callInto(method string, args map[string]any, out any) error {
	raw, result, err := c.callResult(method, args)
	if err != nil {
		return err
	}
	if !strings.EqualFold(result, "success") {
		return fmt.Errorf("%w: %s", ErrUnavailable, result)
	}
	if out == nil || len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%w: decode transmission arguments: %v", ErrUnavailable, err)
	}
	return nil
}

type transmissionTorrent struct {
	ID                      int64              `json:"id"`
	HashString              string             `json:"hashString"`
	Name                    string             `json:"name"`
	DownloadDir             string             `json:"downloadDir"`
	TotalSize               int64              `json:"totalSize"`
	HaveValid               int64              `json:"haveValid"`
	DownloadedEver          int64              `json:"downloadedEver"`
	UploadedEver            int64              `json:"uploadedEver"`
	PeersConnected          int                `json:"peersConnected"`
	Status                  int                `json:"status"`
	MetadataPercentComplete float64            `json:"metadataPercentComplete"`
	Files                   []transmissionFile `json:"files"`
	FileStats               []transmissionFileStat `json:"fileStats"`
	Peers                   []transmissionPeer `json:"peers"`
	ErrorString             string             `json:"errorString"`
}

type transmissionFile struct {
	Name   string `json:"name"`
	Length int64  `json:"length"`
	BytesCompleted int64 `json:"bytesCompleted"`
}

type transmissionFileStat struct {
	BytesCompleted int64 `json:"bytesCompleted"`
	Wanted         bool  `json:"wanted"`
	Priority       int   `json:"priority"`
}

type transmissionPeer struct {
	Address        string  `json:"address"`
	ClientName     string  `json:"clientName"`
	FlagStr        string  `json:"flagStr"`
	IsIncoming     bool    `json:"isIncoming"`
	IsUTP          bool    `json:"isUTP"`
	PeerID         string  `json:"peerId"`
	Port           int     `json:"port"`
	RateToClient   float64 `json:"rateToClient"`
	RateToPeer     float64 `json:"rateToPeer"`
	Progress       float64 `json:"progress"`
}

type transmissionSessionStats struct {
	CumulativeSize transmissionSessionSize `json:"cumulative-stats"`
}

type transmissionSessionSize struct {
	DownloadedBytes int64 `json:"downloadedBytes"`
	UploadedBytes   int64 `json:"uploadedBytes"`
}

func bpsToTransmissionLimit(bps int64) (limit int64, limited bool) {
	if bps <= 0 {
		return 0, false
	}
	kib := bps / 1024
	if kib < 1 {
		kib = 1
	}
	return kib, true
}
