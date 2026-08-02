package bt

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"golang.org/x/time/rate"
)

const rateLimiterBurst = 256 << 10

// AnacrolixEngine adapts github.com/anacrolix/torrent to Engine.
type AnacrolixEngine struct {
	client          *torrent.Client
	rootPath        string
	rootStorage     storage.ClientImplCloser
	downloadLimiter *rate.Limiter
	uploadLimiter   *rate.Limiter
	blocker         *Blocker
	mu              sync.RWMutex
	tasks           map[string]*anacrolixTask
	customStorages  map[string]*sharedStorage
}

type sharedStorage struct {
	impl storage.ClientImplCloser
	refs int
}

// NewAnacrolixEngine starts a process-wide BitTorrent client.
func NewAnacrolixEngine(
	downloadDir string,
	listenPort int,
	downloadLimitBps int64,
	uploadLimitBps int64,
	blockConfig BlockConfig,
) (*AnacrolixEngine, error) {
	rootPath := filepath.Clean(downloadDir)
	rootStorage := storage.NewFile(rootPath)
	downloadLimiter := rate.NewLimiter(rate.Inf, rateLimiterBurst)
	uploadLimiter := rate.NewLimiter(rate.Inf, rateLimiterBurst)
	applyRateLimit(downloadLimiter, downloadLimitBps)
	applyRateLimit(uploadLimiter, uploadLimitBps)

	blocker, err := NewBlocker(blockConfig)
	if err != nil {
		_ = rootStorage.Close()
		return nil, fmt.Errorf("configure BT blocklist: %w", err)
	}

	config := torrent.NewDefaultClientConfig()
	config.DataDir = rootPath
	config.DefaultStorage = rootStorage
	config.ListenPort = listenPort
	config.Seed = true
	config.DownloadRateLimiter = downloadLimiter
	config.UploadRateLimiter = uploadLimiter
	config.Bep20 = ClientBep20
	config.ExtendedHandshakeClientVersion = clientExtendedHandshakeVersion()
	config.HTTPUserAgent = clientHTTPUserAgent()
	config.UpnpID = clientUpnpID()
	blocker.install(config)

	client, err := torrent.NewClient(config)
	if err != nil {
		_ = rootStorage.Close()
		return nil, fmt.Errorf("start BitTorrent client: %w", err)
	}
	return &AnacrolixEngine{
		client:          client,
		rootPath:        rootPath,
		rootStorage:     rootStorage,
		downloadLimiter: downloadLimiter,
		uploadLimiter:   uploadLimiter,
		blocker:         blocker,
		tasks:           make(map[string]*anacrolixTask),
		customStorages:  make(map[string]*sharedStorage),
	}, nil
}

func (e *AnacrolixEngine) AddMagnet(uri string, savePath string) (EngineTask, error) {
	spec, err := torrent.TorrentSpecFromMagnetUri(uri)
	if err != nil {
		return nil, fmt.Errorf("%w: parse magnet URI: %v", ErrInvalidInput, err)
	}
	return e.addSpec(spec, savePath)
}

func (e *AnacrolixEngine) AddTorrent(data []byte, savePath string) (EngineTask, error) {
	meta, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: parse torrent file: %v", ErrInvalidInput, err)
	}
	spec, err := torrent.TorrentSpecFromMetaInfoErr(meta)
	if err != nil {
		return nil, fmt.Errorf("%w: parse torrent metadata: %v", ErrInvalidInput, err)
	}
	return e.addSpec(spec, savePath)
}

func (e *AnacrolixEngine) addSpec(
	spec *torrent.TorrentSpec,
	savePath string,
) (EngineTask, error) {
	storageKey := ""
	if filepath.Clean(savePath) != e.rootPath {
		storageKey = filepath.Clean(savePath)
		spec.Storage = e.acquireStorage(storageKey)
	}
	task, added, err := e.client.AddTorrentSpec(spec)
	if err != nil {
		e.releaseStorage(storageKey)
		return nil, fmt.Errorf("add torrent: %w", err)
	}
	if !added {
		e.releaseStorage(storageKey)
		return nil, ErrConflict
	}
	wrapped := &anacrolixTask{torrent: task, storageKey: storageKey}
	hash := wrapped.InfoHash()
	e.mu.Lock()
	e.tasks[hash] = wrapped
	e.mu.Unlock()
	return wrapped, nil
}

func (e *AnacrolixEngine) Task(infoHash string) (EngineTask, bool) {
	e.mu.RLock()
	task, ok := e.tasks[strings.ToLower(infoHash)]
	e.mu.RUnlock()
	return task, ok
}

func (e *AnacrolixEngine) Remove(infoHash string) error {
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
	task.torrent.Drop()
	e.releaseStorage(task.storageKey)
	return nil
}

func (e *AnacrolixEngine) Stats() EngineStats {
	// Prefer PeerConns+WebSeeds payload counters over raw BytesRead/BytesWritten,
	// which include handshake, extension, keepalive, and encryption framing.
	clientStats := e.client.Stats()
	downloaded, uploaded := payloadBytes(clientStats.PeerConns)
	webDown, webUp := payloadBytes(clientStats.WebSeeds)
	result := EngineStats{
		DownloadedBytes: downloaded + webDown,
		UploadedBytes:   uploaded + webUp,
	}
	for _, server := range e.client.DhtServers() {
		stats, ok := server.Stats().(dht.ServerStats)
		if !ok {
			continue
		}
		result.DHTNodes += stats.Nodes
		result.DHTGoodNodes += stats.GoodNodes
	}
	return result
}

func (e *AnacrolixEngine) SetRateLimits(downloadBps, uploadBps int64) {
	applyRateLimit(e.downloadLimiter, downloadBps)
	applyRateLimit(e.uploadLimiter, uploadBps)
}

func (e *AnacrolixEngine) SetBlockConfig(config BlockConfig) error {
	if e.blocker == nil {
		return nil
	}
	if err := e.blocker.Replace(config); err != nil {
		return err
	}
	e.dropBlockedPeers()
	return nil
}

func (e *AnacrolixEngine) dropBlockedPeers() {
	if e.client == nil || e.blocker == nil {
		return
	}
	for _, task := range e.client.Torrents() {
		for _, conn := range task.PeerConns() {
			reason, blocked := e.blocker.shouldBlock(
				peerExtendedClientName(conn),
				conn.PeerID,
				conn.RemoteAddr,
				conn.PeerListenPort,
			)
			if !blocked {
				continue
			}
			log.Printf("BT dropped peer %s (%s)", conn.RemoteAddr, reason)
			dropPeerConn(conn)
		}
	}
}

func (e *AnacrolixEngine) Close() error {
	e.mu.Lock()
	e.tasks = make(map[string]*anacrolixTask)
	e.mu.Unlock()
	var errs []error
	errs = append(errs, e.client.Close()...)
	e.mu.Lock()
	for key, entry := range e.customStorages {
		if err := entry.impl.Close(); err != nil {
			errs = append(errs, err)
		}
		delete(e.customStorages, key)
	}
	e.mu.Unlock()
	if err := e.rootStorage.Close(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (e *AnacrolixEngine) acquireStorage(path string) storage.ClientImplCloser {
	e.mu.Lock()
	defer e.mu.Unlock()
	if entry, ok := e.customStorages[path]; ok {
		entry.refs++
		return entry.impl
	}
	impl := storage.NewFile(path)
	e.customStorages[path] = &sharedStorage{impl: impl, refs: 1}
	return impl
}

func (e *AnacrolixEngine) releaseStorage(path string) {
	if path == "" {
		return
	}
	e.mu.Lock()
	entry, ok := e.customStorages[path]
	if !ok {
		e.mu.Unlock()
		return
	}
	entry.refs--
	if entry.refs > 0 {
		e.mu.Unlock()
		return
	}
	delete(e.customStorages, path)
	e.mu.Unlock()
	_ = entry.impl.Close()
}

func applyRateLimit(limiter *rate.Limiter, bps int64) {
	if bps <= 0 {
		limiter.SetLimit(rate.Inf)
		limiter.SetBurst(rateLimiterBurst)
		return
	}
	burst := int(bps)
	if burst < rateLimiterBurst {
		burst = rateLimiterBurst
	}
	limiter.SetLimit(rate.Limit(bps))
	limiter.SetBurst(burst)
}

type anacrolixTask struct {
	torrent    *torrent.Torrent
	storageKey string
}

func (t *anacrolixTask) InfoHash() string {
	return strings.ToLower(t.torrent.InfoHash().HexString())
}

func (t *anacrolixTask) MetadataReady() <-chan struct{} {
	return t.torrent.GotInfo()
}

func (t *anacrolixTask) Metadata() TaskMetadata {
	info := t.torrent.Info()
	if info == nil {
		return TaskMetadata{}
	}
	files := t.torrent.Files()
	result := TaskMetadata{
		Name:       info.BestName(),
		TotalBytes: t.torrent.Length(),
		Files:      make([]TaskFile, 0, len(files)),
	}
	for index, file := range files {
		result.Files = append(result.Files, TaskFile{
			Index: index, Path: file.Path(), Length: file.Length(),
		})
	}
	return result
}

func (t *anacrolixTask) Stats() TaskStats {
	stats := t.torrent.Stats()
	downloaded, uploaded := payloadBytes(stats.ConnStats)
	result := TaskStats{
		DownloadedBytes: downloaded,
		UploadedBytes:   uploaded,
		ActivePeers:     stats.ActivePeers,
		FileCompleted:   make(map[int]int64),
	}
	if t.torrent.Info() == nil {
		return result
	}
	files := t.torrent.Files()
	result.CompletedBytes = t.torrent.BytesCompleted()
	for index, file := range files {
		result.FileCompleted[index] = file.BytesCompleted()
	}
	return result
}

func (t *anacrolixTask) Peers() []PeerInfo {
	conns := t.torrent.PeerConns()
	peers := make([]PeerInfo, 0, len(conns))
	for _, conn := range conns {
		address := ""
		if conn.RemoteAddr != nil {
			address = conn.RemoteAddr.String()
		}
		stats := conn.Stats()
		downloaded, uploaded := payloadBytes(stats.ConnStats)
		client, version := identifyPeerClient(peerExtendedClientName(conn), conn.PeerID)
		peers = append(peers, PeerInfo{
			Address:       address,
			PeerID:        formatPeerID(conn.PeerID),
			Client:        client,
			ClientVersion: version,
			Network:       conn.Network,
			Source:        string(conn.Discovery),
			Downloaded:    downloaded,
			Uploaded:      uploaded,
		})
	}
	return peers
}

func peerExtendedClientName(conn *torrent.PeerConn) string {
	value := conn.PeerClientName.Load()
	if value == nil {
		return ""
	}
	name, _ := value.(string)
	return name
}

// payloadBytes returns torrent file piece payload totals.
// BytesRead/BytesWritten are intentionally ignored: those include protocol chatter.
func payloadBytes(stats torrent.ConnStats) (downloaded, uploaded int64) {
	return stats.BytesReadData.Int64(), stats.BytesWrittenData.Int64()
}

func (t *anacrolixTask) Pause() {
	t.torrent.DisallowDataDownload()
	t.torrent.DisallowDataUpload()
}

func (t *anacrolixTask) Resume() {
	t.torrent.AllowDataDownload()
	t.torrent.AllowDataUpload()
}

func (t *anacrolixTask) PauseUpload() {
	t.torrent.DisallowDataUpload()
}

func (t *anacrolixTask) ResumeUpload() {
	t.torrent.AllowDataUpload()
}

func (t *anacrolixTask) SetFiles(selections []FileSelection) error {
	files := t.torrent.Files()
	priorities := make(map[int]int, len(selections))
	for _, selection := range selections {
		if selection.Index < 0 || selection.Index >= len(files) {
			return fmt.Errorf("%w: file index %d", ErrInvalidInput, selection.Index)
		}
		if selection.Priority < 0 || selection.Priority > 2 {
			return fmt.Errorf("%w: file priority must be 0, 1, or 2", ErrInvalidInput)
		}
		priorities[selection.Index] = selection.Priority
	}
	for index, file := range files {
		priority := priorities[index]
		switch priority {
		case 0:
			file.SetPriority(torrent.PiecePriorityNone)
		case 1:
			file.SetPriority(torrent.PiecePriorityNormal)
		case 2:
			file.SetPriority(torrent.PiecePriorityHigh)
		}
	}
	return nil
}

func formatPeerID(id torrent.PeerID) string {
	printable := make([]byte, 0, len(id))
	for _, b := range id {
		if b >= 32 && b < 127 {
			printable = append(printable, b)
		} else {
			printable = append(printable, '.')
		}
	}
	return string(printable)
}
