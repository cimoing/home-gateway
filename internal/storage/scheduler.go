package storage

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"home-gateway/internal/config"

	"github.com/robfig/cron/v3"
)

// ScheduleView is the API representation of one storage.sync rule.
type ScheduleView struct {
	ID             int                `json:"id"`
	Interval       string             `json:"interval"`
	Enabled        bool               `json:"enabled"`
	Src            config.StorageSyncEndpoint `json:"src"`
	Dst            config.StorageSyncEndpoint `json:"dst"`
	Running        bool               `json:"running"`
	LastStatus     string             `json:"lastStatus,omitempty"`
	LastError      string             `json:"lastError,omitempty"`
	LastScanned    int                `json:"lastScanned"`
	LastCopied     int                `json:"lastCopied"`
	LastSkipped    int                `json:"lastSkipped"`
	LastBytes      int64              `json:"lastBytes"`
	LastStartedAt  *time.Time         `json:"lastStartedAt,omitempty"`
	LastFinishedAt *time.Time         `json:"lastFinishedAt,omitempty"`
}

type scheduleEntry struct {
	rule           config.StorageSyncRule
	running        bool
	lastStatus     string
	lastError      string
	lastResult     IncrementalResult
	lastStartedAt  *time.Time
	lastFinishedAt *time.Time
}

// Scheduler runs configured storage.sync rules on cron intervals.
type Scheduler struct {
	service *Service
	mu      sync.Mutex
	cron    *cron.Cron
	entries []scheduleEntry
	cancel  context.CancelFunc
	ctx     context.Context
}

// NewScheduler creates an idle scheduler bound to a storage service.
func NewScheduler(service *Service) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		service: service,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Replace rebuilds cron entries from configuration. Safe to call on reload.
func (s *Scheduler) Replace(rules []config.StorageSyncRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cron != nil {
		ctx := s.cron.Stop()
		<-ctx.Done()
		s.cron = nil
	}

	previous := make(map[string]scheduleEntry, len(s.entries))
	for index, entry := range s.entries {
		previous[ruleFingerprint(index, entry.rule)] = entry
	}

	next := make([]scheduleEntry, 0, len(rules))
	for index, rule := range rules {
		entry := scheduleEntry{rule: rule}
		if old, ok := previous[ruleFingerprint(index, rule)]; ok {
			entry.lastStatus = old.lastStatus
			entry.lastError = old.lastError
			entry.lastResult = old.lastResult
			entry.lastStartedAt = old.lastStartedAt
			entry.lastFinishedAt = old.lastFinishedAt
		}
		next = append(next, entry)
	}
	s.entries = next

	enabledCount := 0
	for _, entry := range next {
		if entry.rule.Enabled == nil || *entry.rule.Enabled {
			enabledCount++
		}
	}
	if enabledCount == 0 {
		log.Printf("storage sync scheduler idle (no enabled rules)")
		return nil
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	scheduler := cron.New(cron.WithParser(parser), cron.WithLocation(time.Local))
	for index := range s.entries {
		index := index
		rule := s.entries[index].rule
		if rule.Enabled != nil && !*rule.Enabled {
			continue
		}
		_, err := scheduler.AddFunc(rule.Interval, func() {
			s.runRule(index, "cron")
		})
		if err != nil {
			_ = scheduler.Stop()
			return fmt.Errorf("storage.sync[%d].interval: %w", index, err)
		}
		log.Printf(
			"storage sync scheduled id=%d interval=%q src=%s:%s dst=%s:%s",
			index, rule.Interval, rule.Src.Name, rule.Src.Path, rule.Dst.Name, rule.Dst.Path,
		)
	}
	scheduler.Start()
	s.cron = scheduler
	return nil
}

// List returns configured sync schedules and last-run status.
func (s *Scheduler) List() []ScheduleView {
	s.mu.Lock()
	defer s.mu.Unlock()
	views := make([]ScheduleView, 0, len(s.entries))
	for index, entry := range s.entries {
		views = append(views, entry.view(index))
	}
	return views
}

// Trigger starts one schedule asynchronously. Returns the updated view.
func (s *Scheduler) Trigger(id int) (ScheduleView, error) {
	s.mu.Lock()
	if id < 0 || id >= len(s.entries) {
		s.mu.Unlock()
		return ScheduleView{}, ErrNotFound
	}
	if s.entries[id].running {
		view := s.entries[id].view(id)
		s.mu.Unlock()
		return view, fmt.Errorf("%w: schedule is already running", ErrConflict)
	}
	s.entries[id].running = true
	now := time.Now().UTC()
	s.entries[id].lastStatus = "running"
	s.entries[id].lastError = ""
	s.entries[id].lastStartedAt = &now
	s.entries[id].lastFinishedAt = nil
	view := s.entries[id].view(id)
	s.mu.Unlock()

	go s.runRule(id, "manual")
	return view, nil
}

// Stop cancels the scheduler and waits for cron shutdown.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancel()
	if s.cron != nil {
		ctx := s.cron.Stop()
		<-ctx.Done()
		s.cron = nil
	}
}

func (s *Scheduler) runRule(id int, trigger string) {
	s.mu.Lock()
	if id < 0 || id >= len(s.entries) {
		s.mu.Unlock()
		return
	}
	// Cron ticks still need to claim the running flag; manual Trigger already set it.
	if trigger != "manual" {
		if s.entries[id].running {
			rule := s.entries[id].rule
			s.mu.Unlock()
			log.Printf("storage sync skipped overlapping run trigger=%s src=%s:%s dst=%s:%s",
				trigger, rule.Src.Name, rule.Src.Path, rule.Dst.Name, rule.Dst.Path)
			return
		}
		s.entries[id].running = true
		now := time.Now().UTC()
		s.entries[id].lastStatus = "running"
		s.entries[id].lastError = ""
		s.entries[id].lastStartedAt = &now
		s.entries[id].lastFinishedAt = nil
	} else if !s.entries[id].running {
		// Trigger should have marked running; bail if state was reset.
		s.mu.Unlock()
		return
	}
	rule := s.entries[id].rule
	parent := s.ctx
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		if id >= 0 && id < len(s.entries) {
			s.entries[id].running = false
		}
		s.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(parent, 6*time.Hour)
	defer cancel()
	started := time.Now()
	log.Printf(
		"storage sync tick trigger=%s id=%d interval=%q src=%s dst=%s",
		trigger, id, rule.Interval,
		formatEndpoint(rule.Src.Name, rule.Src.Path),
		formatEndpoint(rule.Dst.Name, rule.Dst.Path),
	)
	result, err := s.service.SyncIncremental(ctx, rule)
	finished := time.Now().UTC()
	logIncremental(rule, result, err, time.Since(started))

	s.mu.Lock()
	if id >= 0 && id < len(s.entries) {
		s.entries[id].lastResult = result
		s.entries[id].lastFinishedAt = &finished
		if err != nil {
			s.entries[id].lastStatus = "failed"
			s.entries[id].lastError = err.Error()
		} else {
			s.entries[id].lastStatus = "completed"
			s.entries[id].lastError = ""
		}
	}
	s.mu.Unlock()
}

func (e scheduleEntry) view(id int) ScheduleView {
	enabled := true
	if e.rule.Enabled != nil {
		enabled = *e.rule.Enabled
	}
	return ScheduleView{
		ID:             id,
		Interval:       e.rule.Interval,
		Enabled:        enabled,
		Src:            e.rule.Src,
		Dst:            e.rule.Dst,
		Running:        e.running,
		LastStatus:     e.lastStatus,
		LastError:      e.lastError,
		LastScanned:    e.lastResult.Scanned,
		LastCopied:     e.lastResult.Copied,
		LastSkipped:    e.lastResult.Skipped,
		LastBytes:      e.lastResult.Bytes,
		LastStartedAt:  e.lastStartedAt,
		LastFinishedAt: e.lastFinishedAt,
	}
}

func ruleFingerprint(index int, rule config.StorageSyncRule) string {
	return fmt.Sprintf("%d|%s|%s|%s|%s|%s",
		index, rule.Interval, rule.Src.Name, rule.Src.Path, rule.Dst.Name, rule.Dst.Path)
}
