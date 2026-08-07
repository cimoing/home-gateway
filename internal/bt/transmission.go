package bt

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"path"
	"strings"
	"sync"
	"time"
)

// TransmissionEngine adapts transmission-daemon RPC to Engine.
type TransmissionEngine struct {
	rpc        *transmissionRPC
	rootPath   string
	listenPort int
	blocker    *Blocker
	mu         sync.Mutex
	tasks      map[string]*transmissionTask
}

type transmissionTask struct {
	engine     *TransmissionEngine
	id         int64
	hash       string
	readyOnce  sync.Once
	ready      chan struct{}
	uploadHold bool
}

// NewTransmissionEngine connects to an existing transmission-daemon.
func NewTransmissionEngine(
	rpcURL string,
	username string,
	password string,
	downloadDir string,
	listenPort int,
	downloadLimitBps int64,
	uploadLimitBps int64,
	blockConfig BlockConfig,
) (*TransmissionEngine, error) {
	blocker, err := NewBlocker(blockConfig)
	if err != nil {
		return nil, fmt.Errorf("configure BT blocklist: %w", err)
	}
	engine := &TransmissionEngine{
		rpc:        newTransmissionRPC(rpcURL, username, password),
		rootPath:   cleanRemotePath(downloadDir),
		listenPort: listenPort,
		blocker:    blocker,
		tasks:      make(map[string]*transmissionTask),
	}
	if err := engine.bootstrap(downloadLimitBps, uploadLimitBps); err != nil {
		return nil, err
	}
	if err := engine.reloadTasks(); err != nil {
		return nil, err
	}
	log.Printf(
		"BT transmission engine connected url=%s download_dir=%s torrents=%d",
		rpcURL, engine.rootPath, len(engine.tasks),
	)
	if len(blockConfig.Clients) > 0 || len(blockConfig.PeerIDs) > 0 || len(blockConfig.Ports) > 0 {
		log.Printf("BT transmission: client/peer-id/port block rules are stored but not enforced by daemon hooks")
	}
	return engine, nil
}

func (e *TransmissionEngine) bootstrap(downloadLimitBps, uploadLimitBps int64) error {
	var version struct {
		Version string `json:"version"`
	}
	if err := e.rpc.call("session-get", map[string]any{
		"fields": []string{"version"},
	}, &version); err != nil {
		// Older daemons ignore fields; retry without.
		if err := e.rpc.call("session-get", nil, &version); err != nil {
			return fmt.Errorf("connect transmission: %w", err)
		}
	}
	downLimit, downLimited := bpsToTransmissionLimit(downloadLimitBps)
	upLimit, upLimited := bpsToTransmissionLimit(uploadLimitBps)
	args := map[string]any{
		"download-dir":              e.rootPath,
		"peer-port":                 e.listenPort,
		"peer-port-random-on-start": false,
		"port-forwarding-enabled":   true,
		"dht-enabled":               true,
		"pex-enabled":               true,
		"start-added-torrents":      false,
		"speed-limit-down":          downLimit,
		"speed-limit-down-enabled":  downLimited,
		"speed-limit-up":            upLimit,
		"speed-limit-up-enabled":    upLimited,
	}
	if err := e.rpc.call("session-set", args, nil); err != nil {
		return fmt.Errorf("configure transmission session: %w", err)
	}
	return nil
}

func (e *TransmissionEngine) reloadTasks() error {
	var result struct {
		Torrents []transmissionTorrent `json:"torrents"`
	}
	if err := e.rpc.call("torrent-get", map[string]any{
		"fields": []string{"id", "hashString", "name", "downloadDir", "status"},
	}, &result); err != nil {
		return fmt.Errorf("list transmission torrents: %w", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tasks = make(map[string]*transmissionTask, len(result.Torrents))
	for _, torrent := range result.Torrents {
		hash := strings.ToLower(strings.TrimSpace(torrent.HashString))
		if hash == "" {
			continue
		}
		task := &transmissionTask{
			engine: e,
			id:     torrent.ID,
			hash:   hash,
			ready:  make(chan struct{}),
		}
		e.tasks[hash] = task
		go task.watchMetadata()
	}
	return nil
}

func (e *TransmissionEngine) AddMagnet(uri string, savePath string) (EngineTask, error) {
	return e.add(map[string]any{
		"filename":     uri,
		"download-dir": cleanRemotePath(savePath),
		"paused":       true,
	})
}

func (e *TransmissionEngine) AddTorrent(data []byte, savePath string) (EngineTask, error) {
	return e.add(map[string]any{
		"metainfo":     base64.StdEncoding.EncodeToString(data),
		"download-dir": cleanRemotePath(savePath),
		"paused":       true,
	})
}

// cleanRemotePath normalizes a path as seen by transmission-daemon (POSIX),
// independent of the OS running home-gateway.
func cleanRemotePath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" {
		return ""
	}
	cleaned := path.Clean(value)
	if strings.HasPrefix(value, "/") && cleaned != "/" && !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	return cleaned
}

func (e *TransmissionEngine) add(args map[string]any) (EngineTask, error) {
	raw, result, err := e.rpc.callResult("torrent-add", args)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(result, "success") {
		if strings.Contains(strings.ToLower(result), "duplicate") {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, result)
	}
	var payload struct {
		Added     *transmissionTorrent `json:"torrent-added"`
		Duplicate *transmissionTorrent `json:"torrent-duplicate"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("%w: decode transmission add response: %v", ErrUnavailable, err)
	}
	if payload.Duplicate != nil {
		_ = e.ensureTask(payload.Duplicate)
		return nil, ErrConflict
	}
	if payload.Added == nil {
		return nil, fmt.Errorf("%w: transmission add response missing torrent", ErrUnavailable)
	}
	hash := strings.ToLower(payload.Added.HashString)
	e.mu.Lock()
	if existing, exists := e.tasks[hash]; exists {
		e.mu.Unlock()
		if existing.id != payload.Added.ID {
			_, _, _ = e.rpc.callResult("torrent-remove", map[string]any{
				"ids":               []int64{payload.Added.ID},
				"delete-local-data": false,
			})
		}
		return nil, ErrConflict
	}
	task := &transmissionTask{
		engine: e,
		id:     payload.Added.ID,
		hash:   hash,
		ready:  make(chan struct{}),
	}
	e.tasks[hash] = task
	e.mu.Unlock()
	go task.watchMetadata()
	return task, nil
}

func (e *TransmissionEngine) ensureTask(torrent *transmissionTorrent) *transmissionTask {
	hash := strings.ToLower(strings.TrimSpace(torrent.HashString))
	if hash == "" {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if existing, ok := e.tasks[hash]; ok {
		existing.id = torrent.ID
		return existing
	}
	task := &transmissionTask{
		engine: e,
		id:     torrent.ID,
		hash:   hash,
		ready:  make(chan struct{}),
	}
	e.tasks[hash] = task
	go task.watchMetadata()
	return task
}

func (e *TransmissionEngine) Task(infoHash string) (EngineTask, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	task, ok := e.tasks[strings.ToLower(infoHash)]
	return task, ok
}

func (e *TransmissionEngine) TaskByID(id int64) (EngineTask, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, task := range e.tasks {
		if task.id == id {
			return task, true
		}
	}
	return nil, false
}

func (e *TransmissionEngine) Remove(infoHash string) error {
	hash := strings.ToLower(infoHash)
	e.mu.Lock()
	task, ok := e.tasks[hash]
	if ok {
		delete(e.tasks, hash)
	}
	e.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	return e.RemoveByID(task.id, false)
}

func (e *TransmissionEngine) RemoveByID(id int64, deleteData bool) error {
	e.mu.Lock()
	for hash, task := range e.tasks {
		if task.id == id {
			delete(e.tasks, hash)
			break
		}
	}
	e.mu.Unlock()
	_, _, err := e.rpc.callResult("torrent-remove", map[string]any{
		"ids":               []int64{id},
		"delete-local-data": deleteData,
	})
	return err
}

var remoteTorrentFields = []string{
	"id", "hashString", "name", "downloadDir", "status", "errorString",
	"totalSize", "haveValid", "downloadedEver", "uploadedEver", "peersConnected",
	"metadataPercentComplete", "addedDate", "files", "fileStats",
}

// ListRemote returns all torrents from transmission-daemon.
func (e *TransmissionEngine) ListRemote() ([]RemoteTorrent, error) {
	var result struct {
		Torrents []transmissionTorrent `json:"torrents"`
	}
	if err := e.rpc.call("torrent-get", map[string]any{"fields": remoteTorrentFields}, &result); err != nil {
		return nil, err
	}
	out := make([]RemoteTorrent, 0, len(result.Torrents))
	for index := range result.Torrents {
		torrent := &result.Torrents[index]
		_ = e.ensureTask(torrent)
		out = append(out, mapRemoteTorrent(torrent, e.rootPath))
	}
	return out, nil
}

// GetRemote returns one torrent by Transmission id.
func (e *TransmissionEngine) GetRemote(id int64) (RemoteTorrent, error) {
	var result struct {
		Torrents []transmissionTorrent `json:"torrents"`
	}
	if err := e.rpc.call("torrent-get", map[string]any{
		"ids":    []int64{id},
		"fields": remoteTorrentFields,
	}, &result); err != nil {
		return RemoteTorrent{}, err
	}
	if len(result.Torrents) == 0 {
		return RemoteTorrent{}, ErrNotFound
	}
	torrent := &result.Torrents[0]
	_ = e.ensureTask(torrent)
	return mapRemoteTorrent(torrent, e.rootPath), nil
}

func mapRemoteTorrent(torrent *transmissionTorrent, root string) RemoteTorrent {
	savePath := cleanRemotePath(torrent.DownloadDir)
	if savePath == "" {
		savePath = root
	}
	status, desired := mapTransmissionStatus(torrent)
	files := make([]RemoteFile, 0, len(torrent.Files))
	for index, file := range torrent.Files {
		wanted := true
		priority := 1
		completed := file.BytesCompleted
		if index < len(torrent.FileStats) {
			wanted = torrent.FileStats[index].Wanted
			priority = mapTransmissionFilePriority(wanted, torrent.FileStats[index].Priority)
			completed = torrent.FileStats[index].BytesCompleted
		} else if !wanted {
			priority = 0
		}
		files = append(files, RemoteFile{
			Index:          index,
			Path:           strings.ReplaceAll(file.Name, "\\", "/"),
			Length:         file.Length,
			Selected:       wanted,
			Priority:       priority,
			CompletedBytes: completed,
		})
	}
	addedAt := time.Time{}
	if torrent.AddedDate > 0 {
		addedAt = time.Unix(int64(torrent.AddedDate), 0).UTC()
	}
	return RemoteTorrent{
		ID:               torrent.ID,
		InfoHash:         strings.ToLower(strings.TrimSpace(torrent.HashString)),
		Name:             torrent.Name,
		SavePath:         savePath,
		Status:           status,
		DesiredState:     desired,
		Error:            torrent.ErrorString,
		TotalBytes:       torrent.TotalSize,
		CompletedBytes:   torrent.HaveValid,
		DownloadedBytes:  torrent.DownloadedEver,
		UploadedBytes:    torrent.UploadedEver,
		Peers:            torrent.PeersConnected,
		MetadataComplete: torrent.MetadataPercentComplete >= 1 || len(torrent.Files) > 0,
		AddedAt:          addedAt,
		Files:            files,
	}
}

func mapTransmissionStatus(torrent *transmissionTorrent) (status, desired string) {
	metaReady := torrent.MetadataPercentComplete >= 1 || len(torrent.Files) > 0
	completed := torrent.TotalSize > 0 && torrent.HaveValid >= torrent.TotalSize
	if torrent.Status == 0 {
		desired = "paused"
	} else {
		desired = "downloading"
	}
	if !metaReady {
		return "metadata", desired
	}
	if torrent.Status == 0 {
		if completed {
			return "completed", desired
		}
		return "paused", desired
	}
	if completed {
		return "completed", desired
	}
	if torrent.ErrorString != "" {
		return "error", desired
	}
	return "downloading", desired
}

func mapTransmissionFilePriority(wanted bool, priority int) int {
	if !wanted {
		return 0
	}
	if priority >= 1 {
		return 2
	}
	return 1
}

func (e *TransmissionEngine) Stats() EngineStats {
	var stats transmissionSessionStats
	if err := e.rpc.call("session-stats", nil, &stats); err != nil {
		return EngineStats{}
	}
	return EngineStats{
		DownloadedBytes: stats.CumulativeSize.DownloadedBytes,
		UploadedBytes:   stats.CumulativeSize.UploadedBytes,
	}
}

func (e *TransmissionEngine) SetRateLimits(downloadBps, uploadBps int64) {
	downLimit, downLimited := bpsToTransmissionLimit(downloadBps)
	upLimit, upLimited := bpsToTransmissionLimit(uploadBps)
	_ = e.rpc.call("session-set", map[string]any{
		"speed-limit-down":         downLimit,
		"speed-limit-down-enabled": downLimited,
		"speed-limit-up":           upLimit,
		"speed-limit-up-enabled":   upLimited,
	}, nil)
}

func (e *TransmissionEngine) SetBlockConfig(config BlockConfig) error {
	if e.blocker == nil {
		return nil
	}
	if err := e.blocker.Replace(config); err != nil {
		return err
	}
	if len(config.Clients) > 0 || len(config.PeerIDs) > 0 || len(config.Ports) > 0 {
		log.Printf("BT transmission: client/peer-id/port block rules updated but daemon cannot enforce handshake hooks")
	}
	if len(config.Networks) > 0 {
		log.Printf("BT transmission: IP/CIDR rules stored; configure transmission blocklist for wire-level IP denial")
	}
	return nil
}

func (e *TransmissionEngine) Close() error {
	e.mu.Lock()
	e.tasks = make(map[string]*transmissionTask)
	e.mu.Unlock()
	return nil
}

func (t *transmissionTask) ID() int64       { return t.id }
func (t *transmissionTask) InfoHash() string { return t.hash }

func (t *transmissionTask) MetadataReady() <-chan struct{} { return t.ready }

func (t *transmissionTask) watchMetadata() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		torrent, err := t.fetch(
			"id", "hashString", "name", "totalSize", "files", "fileStats", "metadataPercentComplete",
		)
		if err == nil && torrent != nil {
			if torrent.MetadataPercentComplete >= 1 || len(torrent.Files) > 0 {
				t.readyOnce.Do(func() { close(t.ready) })
				return
			}
		}
		select {
		case <-t.ready:
			return
		case <-ticker.C:
		}
	}
}

func (t *transmissionTask) Metadata() TaskMetadata {
	torrent, err := t.fetch("name", "totalSize", "files", "fileStats", "metadataPercentComplete")
	if err != nil || torrent == nil || len(torrent.Files) == 0 {
		return TaskMetadata{}
	}
	files := make([]TaskFile, 0, len(torrent.Files))
	for index, file := range torrent.Files {
		files = append(files, TaskFile{
			Index:  index,
			Path:   strings.ReplaceAll(file.Name, "\\", "/"),
			Length: file.Length,
		})
	}
	return TaskMetadata{
		Name:       torrent.Name,
		TotalBytes: torrent.TotalSize,
		Files:      files,
	}
}

func (t *transmissionTask) Stats() TaskStats {
	torrent, err := t.fetch(
		"haveValid", "downloadedEver", "uploadedEver", "peersConnected", "files", "fileStats",
	)
	if err != nil || torrent == nil {
		return TaskStats{FileCompleted: map[int]int64{}}
	}
	result := TaskStats{
		CompletedBytes:  torrent.HaveValid,
		DownloadedBytes: torrent.DownloadedEver,
		UploadedBytes:   torrent.UploadedEver,
		ActivePeers:     torrent.PeersConnected,
		FileCompleted:   make(map[int]int64, len(torrent.Files)),
	}
	for index, file := range torrent.Files {
		completed := file.BytesCompleted
		if index < len(torrent.FileStats) {
			completed = torrent.FileStats[index].BytesCompleted
		}
		result.FileCompleted[index] = completed
	}
	return result
}

func (t *transmissionTask) Peers() []PeerInfo {
	torrent, err := t.fetch("peers")
	if err != nil || torrent == nil {
		return nil
	}
	peers := make([]PeerInfo, 0, len(torrent.Peers))
	for _, peer := range torrent.Peers {
		address := peer.Address
		if peer.Port > 0 && peer.Address != "" && !strings.Contains(peer.Address, ":") {
			address = fmt.Sprintf("%s:%d", peer.Address, peer.Port)
		}
		network := "tcp"
		if peer.IsUTP {
			network = "udp"
		}
		source := "Tr"
		if peer.IsIncoming {
			source = "I"
		}
		peerID := strings.TrimSpace(peer.PeerID)
		var idBytes [20]byte
		copy(idBytes[:], []byte(peerID))
		client, version := identifyPeerClient(peer.ClientName, idBytes)
		if client == "" {
			client, version = splitClientVersion(peer.ClientName)
		}
		peers = append(peers, PeerInfo{
			Address:       address,
			PeerID:        peerID,
			Client:        client,
			ClientVersion: version,
			Network:       network,
			Source:        source,
			DownloadRate:  int64(peer.RateToClient),
			UploadRate:    int64(peer.RateToPeer),
		})
	}
	return peers
}

func (t *transmissionTask) Pause() {
	_ = t.engine.rpc.call("torrent-stop", map[string]any{"ids": []int64{t.id}}, nil)
}

func (t *transmissionTask) Resume() {
	_ = t.engine.rpc.call("torrent-start", map[string]any{"ids": []int64{t.id}}, nil)
	if t.uploadHold {
		t.PauseUpload()
	}
}

func (t *transmissionTask) PauseUpload() {
	t.uploadHold = true
	_ = t.engine.rpc.call("torrent-set", map[string]any{
		"ids":           []int64{t.id},
		"uploadLimited": true,
		"uploadLimit":   0,
	}, nil)
}

func (t *transmissionTask) ResumeUpload() {
	t.uploadHold = false
	_ = t.engine.rpc.call("torrent-set", map[string]any{
		"ids":           []int64{t.id},
		"uploadLimited": false,
	}, nil)
}

func (t *transmissionTask) SetFiles(selections []FileSelection) error {
	torrent, err := t.fetch("files", "fileStats")
	if err != nil {
		return err
	}
	if torrent == nil {
		return fmt.Errorf("%w: torrent metadata is not ready", ErrUnavailable)
	}
	wanted := make([]int, 0, len(selections))
	unwanted := make([]int, 0, len(selections))
	high := make([]int, 0)
	normal := make([]int, 0)
	seen := make(map[int]struct{}, len(selections))
	for _, selection := range selections {
		if selection.Index < 0 || selection.Index >= len(torrent.Files) {
			return fmt.Errorf("%w: file index %d", ErrInvalidInput, selection.Index)
		}
		if selection.Priority < 0 || selection.Priority > 2 {
			return fmt.Errorf("%w: file priority must be 0, 1, or 2", ErrInvalidInput)
		}
		seen[selection.Index] = struct{}{}
		switch selection.Priority {
		case 0:
			unwanted = append(unwanted, selection.Index)
		case 1:
			wanted = append(wanted, selection.Index)
			normal = append(normal, selection.Index)
		case 2:
			wanted = append(wanted, selection.Index)
			high = append(high, selection.Index)
		}
	}
	for index := range torrent.Files {
		if _, ok := seen[index]; ok {
			continue
		}
		unwanted = append(unwanted, index)
	}
	args := map[string]any{"ids": []int64{t.id}}
	if len(wanted) > 0 {
		args["files-wanted"] = wanted
	}
	if len(unwanted) > 0 {
		args["files-unwanted"] = unwanted
	}
	if len(high) > 0 {
		args["priority-high"] = high
	}
	if len(normal) > 0 {
		args["priority-normal"] = normal
	}
	return t.engine.rpc.call("torrent-set", args, nil)
}

func (t *transmissionTask) fetch(fields ...string) (*transmissionTorrent, error) {
	var result struct {
		Torrents []transmissionTorrent `json:"torrents"`
	}
	if err := t.engine.rpc.call("torrent-get", map[string]any{
		"ids":    []int64{t.id},
		"fields": fields,
	}, &result); err != nil {
		return nil, err
	}
	if len(result.Torrents) == 0 {
		return nil, ErrNotFound
	}
	return &result.Torrents[0], nil
}