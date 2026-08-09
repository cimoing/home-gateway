package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"path"
	"strings"
	"sync"
	"time"
)

var errTransferInFlight = errors.New("transfer already in progress")

// SyncItem is one file or directory to copy between backends.
type SyncItem struct {
	SourcePath string `json:"sourcePath"`
	DestPath   string `json:"destPath"`
}

// SyncJobRequest starts a background cross-backend copy.
type SyncJobRequest struct {
	SourceBackend string     `json:"sourceBackend"`
	DestBackend   string     `json:"destBackend"`
	Items         []SyncItem `json:"items"`
	Overwrite     bool       `json:"overwrite"`
}

// SyncJobStatus is the public progress snapshot.
type SyncJobStatus struct {
	ID            string    `json:"id"`
	Status        string    `json:"status"`
	Error         string    `json:"error,omitempty"`
	SourceBackend string    `json:"sourceBackend"`
	DestBackend   string    `json:"destBackend"`
	TotalFiles    int       `json:"totalFiles"`
	CopiedFiles   int       `json:"copiedFiles"`
	SkippedFiles  int       `json:"skippedFiles"`
	FailedFiles   int       `json:"failedFiles"`
	TotalBytes     int64     `json:"totalBytes"`
	CopiedBytes    int64     `json:"copiedBytes"`
	CopyRateBps    int64     `json:"copyRateBps"`
	CurrentPath    string    `json:"currentPath,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type syncJob struct {
	mu             sync.Mutex
	id             string
	status         string
	errMessage     string
	sourceBackend  string
	destBackend    string
	totalFiles     int
	copiedFiles    int
	skippedFiles   int
	failedFiles    int
	totalBytes     int64
	copiedBytes    int64
	copyRateBps    int64
	rateSampleAt   time.Time
	rateSampleBytes int64
	currentPath    string
	createdAt      time.Time
	updatedAt      time.Time
	cancel         context.CancelFunc
}

const (
	syncJobQueued    = "queued"
	syncJobRunning   = "running"
	syncJobCompleted = "completed"
	syncJobFailed    = "failed"
	syncJobCanceled  = "canceled"
)

// SyncJobs tracks in-memory cross-storage copy jobs.
type SyncJobs struct {
	mu   sync.Mutex
	jobs map[string]*syncJob
}

func NewSyncJobs() *SyncJobs {
	return &SyncJobs{jobs: make(map[string]*syncJob)}
}

func (m *SyncJobs) Start(ctx context.Context, service *Service, request SyncJobRequest) (SyncJobStatus, error) {
	source := strings.TrimSpace(request.SourceBackend)
	dest := strings.TrimSpace(request.DestBackend)
	if source == "" || dest == "" {
		return SyncJobStatus{}, fmt.Errorf("%w: source and dest backends are required", ErrInvalidInput)
	}
	if len(request.Items) == 0 {
		return SyncJobStatus{}, fmt.Errorf("%w: at least one item is required", ErrInvalidInput)
	}
	for _, item := range request.Items {
		if strings.TrimSpace(item.SourcePath) == "" || strings.TrimSpace(item.DestPath) == "" {
			return SyncJobStatus{}, fmt.Errorf("%w: sourcePath and destPath are required", ErrInvalidInput)
		}
		if _, err := cleanRelativePath(item.SourcePath); err != nil {
			return SyncJobStatus{}, fmt.Errorf("%w: invalid sourcePath", ErrInvalidInput)
		}
		if _, err := cleanRelativePath(item.DestPath); err != nil {
			return SyncJobStatus{}, fmt.Errorf("%w: invalid destPath", ErrInvalidInput)
		}
	}
	if _, err := service.getConfig(source); err != nil {
		return SyncJobStatus{}, err
	}
	if _, err := service.getConfig(dest); err != nil {
		return SyncJobStatus{}, err
	}

	jobCtx, cancel := context.WithCancel(context.Background())
	now := time.Now().UTC()
	job := &syncJob{
		id:            newSyncJobID(),
		status:        syncJobQueued,
		sourceBackend: source,
		destBackend:   dest,
		createdAt:     now,
		updatedAt:     now,
		cancel:        cancel,
	}
	m.mu.Lock()
	m.jobs[job.id] = job
	m.mu.Unlock()

	go m.run(jobCtx, service, job, request)
	return job.snapshot(), nil
}

func (m *SyncJobs) Get(id string) (SyncJobStatus, error) {
	m.mu.Lock()
	job, ok := m.jobs[id]
	m.mu.Unlock()
	if !ok {
		return SyncJobStatus{}, ErrNotFound
	}
	return job.snapshot(), nil
}

func (m *SyncJobs) Cancel(id string) (SyncJobStatus, error) {
	m.mu.Lock()
	job, ok := m.jobs[id]
	m.mu.Unlock()
	if !ok {
		return SyncJobStatus{}, ErrNotFound
	}
	job.mu.Lock()
	if job.status == syncJobQueued || job.status == syncJobRunning {
		job.cancel()
	}
	job.mu.Unlock()
	return job.snapshot(), nil
}

func (m *SyncJobs) run(ctx context.Context, service *Service, job *syncJob, request SyncJobRequest) {
	started := time.Now()
	log.Printf(
		"storage copy job=%s start src=%s dst=%s items=%d overwrite=%v",
		job.id, request.SourceBackend, request.DestBackend, len(request.Items), request.Overwrite,
	)
	job.setStatus(syncJobRunning, "")
	src, err := service.open(request.SourceBackend)
	if err != nil {
		log.Printf("storage copy job=%s failed open src=%s err=%v", job.id, request.SourceBackend, err)
		job.setStatus(syncJobFailed, err.Error())
		return
	}
	defer src.Close()
	dst, err := service.open(request.DestBackend)
	if err != nil {
		log.Printf("storage copy job=%s failed open dst=%s err=%v", job.id, request.DestBackend, err)
		job.setStatus(syncJobFailed, err.Error())
		return
	}
	defer dst.Close()

	plan, err := buildCopyPlan(ctx, src, request.Items)
	if err != nil {
		log.Printf("storage copy job=%s failed plan err=%v", job.id, err)
		job.setStatus(syncJobFailed, err.Error())
		return
	}
	plan = dedupeCopySteps(plan)
	job.mu.Lock()
	job.totalFiles = len(plan)
	var totalBytes int64
	for _, step := range plan {
		totalBytes += step.size
	}
	job.totalBytes = totalBytes
	job.updatedAt = time.Now().UTC()
	job.mu.Unlock()
	log.Printf(
		"storage copy job=%s planned files=%d bytes=%s",
		job.id, len(plan), formatByteSize(totalBytes),
	)

	for index, step := range plan {
		if ctx.Err() != nil {
			log.Printf("storage copy job=%s canceled after %d/%d", job.id, index, len(plan))
			job.setStatus(syncJobCanceled, "canceled")
			return
		}
		job.setCurrent(step.destPath)
		log.Printf(
			"storage copy job=%s %d/%d src=%s:%s dst=%s:%s size=%s",
			job.id, index+1, len(plan),
			request.SourceBackend, step.sourcePath,
			request.DestBackend, step.destPath,
			formatByteSize(step.size),
		)
		fileStarted := time.Now()
		err := copyOneFile(
			ctx, service.transfers, src, dst,
			request.SourceBackend, request.DestBackend,
			step.sourcePath, step.destPath, step.size, request.Overwrite, job,
		)
		if err != nil {
			if errors.Is(err, errTransferInFlight) {
				job.mu.Lock()
				job.skippedFiles++
				job.updatedAt = time.Now().UTC()
				job.mu.Unlock()
				log.Printf(
					"storage copy job=%s skip in-flight src=%s:%s dst=%s:%s",
					job.id, request.SourceBackend, step.sourcePath, request.DestBackend, step.destPath,
				)
				continue
			}
			job.mu.Lock()
			job.failedFiles++
			job.updatedAt = time.Now().UTC()
			job.mu.Unlock()
			log.Printf(
				"storage copy job=%s failed src=%s:%s dst=%s:%s err=%v",
				job.id, request.SourceBackend, step.sourcePath, request.DestBackend, step.destPath, err,
			)
			job.setStatus(syncJobFailed, fmt.Sprintf("%s: %v", step.sourcePath, err))
			return
		}
		job.mu.Lock()
		job.copiedFiles++
		job.updatedAt = time.Now().UTC()
		job.mu.Unlock()
		log.Printf(
			"storage copy job=%s copied src=%s:%s dst=%s:%s size=%s elapsed=%s",
			job.id, request.SourceBackend, step.sourcePath, request.DestBackend, step.destPath,
			formatByteSize(step.size), time.Since(fileStarted).Round(time.Millisecond),
		)
	}
	log.Printf(
		"storage copy job=%s done files=%d bytes=%s elapsed=%s",
		job.id, len(plan), formatByteSize(totalBytes), time.Since(started).Round(time.Millisecond),
	)
	job.setStatus(syncJobCompleted, "")
}

type copyStep struct {
	sourcePath string
	destPath   string
	size       int64
}

func buildCopyPlan(ctx context.Context, src Backend, items []SyncItem) ([]copyStep, error) {
	steps := make([]copyStep, 0)
	for _, item := range items {
		sourcePath, err := cleanRelativePath(item.SourcePath)
		if err != nil {
			return nil, err
		}
		destPath, err := cleanRelativePath(item.DestPath)
		if err != nil {
			return nil, err
		}
		entry, err := src.Stat(ctx, sourcePath)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", sourcePath, err)
		}
		if !entry.IsDir {
			steps = append(steps, copyStep{sourcePath: sourcePath, destPath: destPath, size: entry.Size})
			continue
		}
		files, err := listFilesRecursive(ctx, src, sourcePath)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			rel := relativeUnder(sourcePath, file.path)
			target := destPath
			if rel != "" {
				target = path.Join(destPath, rel)
			}
			steps = append(steps, copyStep{sourcePath: file.path, destPath: target, size: file.size})
		}
	}
	return steps, nil
}

func dedupeCopySteps(steps []copyStep) []copyStep {
	if len(steps) < 2 {
		return steps
	}
	seen := make(map[string]struct{}, len(steps))
	out := make([]copyStep, 0, len(steps))
	for _, step := range steps {
		key := step.sourcePath + "\x00" + step.destPath
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, step)
	}
	return out
}

func copyOneFile(
	ctx context.Context,
	transfers *transferLock,
	src Backend,
	dst Backend,
	srcBackend string,
	dstBackend string,
	sourcePath string,
	destPath string,
	size int64,
	overwrite bool,
	job *syncJob,
) error {
	release, ok := transfers.TryBegin(srcBackend, sourcePath, dstBackend, destPath)
	if !ok {
		return errTransferInFlight
	}
	defer release()

	if parent := path.Dir(destPath); parent != "." && parent != "" {
		if err := ensureDir(ctx, dst, parent); err != nil {
			return err
		}
	}
	if !overwrite {
		if _, err := dst.Stat(ctx, destPath); err == nil {
			return fmt.Errorf("%w: destination exists", ErrConflict)
		} else if !isNotFound(err) {
			return err
		}
	}

	// Large files: multi-connection range copy (SMB3 share access / local ReadAt+WriteAt).
	if size >= parallelCopyMinSize && canParallelCopy(src, dst) {
		if err := copyParallel(ctx, src, dst, sourcePath, destPath, size, job); err != nil {
			if !errors.Is(err, errParallelUnsupported) {
				return err
			}
		} else {
			return nil
		}
	}

	reader, err := src.Open(ctx, sourcePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	writer, err := dst.Create(ctx, destPath)
	if err != nil {
		return err
	}
	defer writer.Close()

	buf := make([]byte, copyBufferSize)
	var written int64
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, readErr := reader.Read(buf)
		if n > 0 {
			if _, writeErr := writer.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			written += int64(n)
			if job != nil {
				job.addBytes(int64(n))
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	_ = size
	_ = written
	return writer.Close()
}

func ensureDir(ctx context.Context, backend Backend, dir string) error {
	dir, err := cleanRelativePath(dir)
	if err != nil {
		return err
	}
	if dir == "" {
		return nil
	}
	parts := strings.Split(dir, "/")
	current := ""
	for _, part := range parts {
		if current == "" {
			current = part
		} else {
			current = path.Join(current, part)
		}
		entry, err := backend.Stat(ctx, current)
		if err == nil {
			if !entry.IsDir {
				return fmt.Errorf("%w: %s is not a directory", ErrInvalidInput, current)
			}
			continue
		}
		if !isNotFound(err) {
			return err
		}
		if err := backend.Mkdir(ctx, current); err != nil && !isConflict(err) {
			return err
		}
	}
	return nil
}

func isNotFound(err error) bool {
	return err != nil && (err == ErrNotFound || strings.Contains(strings.ToLower(err.Error()), "not found"))
}

func isConflict(err error) bool {
	return err != nil && (err == ErrConflict || strings.Contains(strings.ToLower(err.Error()), "exist"))
}

func (j *syncJob) snapshot() SyncJobStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	return SyncJobStatus{
		ID:            j.id,
		Status:        j.status,
		Error:         j.errMessage,
		SourceBackend: j.sourceBackend,
		DestBackend:   j.destBackend,
		TotalFiles:    j.totalFiles,
		CopiedFiles:   j.copiedFiles,
		SkippedFiles:  j.skippedFiles,
		FailedFiles:   j.failedFiles,
		TotalBytes:    j.totalBytes,
		CopiedBytes:   j.copiedBytes,
		CopyRateBps:   j.copyRateBps,
		CurrentPath:   j.currentPath,
		CreatedAt:     j.createdAt,
		UpdatedAt:     j.updatedAt,
	}
}

func (j *syncJob) setStatus(status, message string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.status = status
	j.errMessage = message
	j.updatedAt = time.Now().UTC()
	if status == syncJobCompleted || status == syncJobFailed || status == syncJobCanceled {
		j.currentPath = ""
	}
}

func (j *syncJob) setCurrent(path string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.currentPath = path
	j.updatedAt = time.Now().UTC()
}

func (j *syncJob) addBytes(n int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.copiedBytes += n
	now := time.Now()
	j.updatedAt = now.UTC()
	if j.rateSampleAt.IsZero() {
		j.rateSampleAt = now
		j.rateSampleBytes = j.copiedBytes
		return
	}
	elapsed := now.Sub(j.rateSampleAt).Seconds()
	if elapsed < 0.8 {
		return
	}
	delta := j.copiedBytes - j.rateSampleBytes
	j.copyRateBps = int64(float64(delta) / elapsed)
	j.rateSampleAt = now
	j.rateSampleBytes = j.copiedBytes
}

func newSyncJobID() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), hex.EncodeToString(buf[:]))
}
