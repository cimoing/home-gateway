package dns

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"home-gateway/internal/cloudflare"
	"home-gateway/internal/config"
	"home-gateway/internal/model"
)

var (
	ErrNotFound      = errors.New("resource not found")
	ErrConflict      = errors.New("resource conflicts with existing data")
	ErrInvalidInput  = errors.New("invalid input")
	ErrProvider      = errors.New("Cloudflare operation failed")
	ErrNotConfigured = errors.New("Cloudflare DNS is not configured")
)

// Provider captures the Cloudflare operations used by the DNS service.
type Provider interface {
	VerifyToken(context.Context) error
	FindZone(context.Context, string) (cloudflare.Zone, error)
	ListRecords(context.Context, string) ([]cloudflare.Record, error)
	CreateRecord(context.Context, string, cloudflare.RecordInput) (cloudflare.Record, error)
	UpdateRecord(context.Context, string, string, cloudflare.RecordInput) (cloudflare.Record, error)
	DeleteRecord(context.Context, string, string) error
}

// ProviderFactory creates an authenticated Cloudflare provider.
type ProviderFactory func(token string) Provider

type zoneMeta struct {
	ProviderZoneID string
	Name           string
	Status         string
	LastSyncedAt   *time.Time
}

// Service manages config-backed Cloudflare DNS with an in-memory record cache.
type Service struct {
	mu              sync.Mutex
	token           string
	zoneNames       []string
	zones           map[string]zoneMeta
	records         map[string][]model.DNSRecord
	providerFactory ProviderFactory
	now             func() time.Time
}

// NewService creates the DNS management service.
func NewService(cfg config.CloudflareConfig, factories ...ProviderFactory) *Service {
	factory := ProviderFactory(func(token string) Provider {
		return cloudflare.NewClient(token)
	})
	if len(factories) > 0 && factories[0] != nil {
		factory = factories[0]
	}
	service := &Service{
		providerFactory: factory,
		now:             time.Now,
		zones:           make(map[string]zoneMeta),
		records:         make(map[string][]model.DNSRecord),
	}
	service.Replace(cfg)
	return service
}

// Replace reloads Cloudflare connection settings from YAML.
func (s *Service) Replace(cfg config.CloudflareConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token = strings.TrimSpace(cfg.Token)
	s.zoneNames = append([]string(nil), cfg.Zones...)
	s.zones = make(map[string]zoneMeta)
	s.records = make(map[string][]model.DNSRecord)
}

// ListZones resolves configured zone names via Cloudflare.
func (s *Service) ListZones(ctx context.Context) ([]model.DNSZone, error) {
	s.mu.Lock()
	token := s.token
	names := append([]string(nil), s.zoneNames...)
	s.mu.Unlock()
	if token == "" {
		if len(names) == 0 {
			return []model.DNSZone{}, nil
		}
		return nil, ErrNotConfigured
	}
	provider := s.providerFactory(token)
	zones := make([]model.DNSZone, 0, len(names))
	for _, name := range names {
		meta, err := s.ensureZone(ctx, provider, name)
		if err != nil {
			return nil, err
		}
		zones = append(zones, model.DNSZone{
			ProviderZoneID: meta.ProviderZoneID,
			Name:           meta.Name,
			Status:         meta.Status,
			LastSyncedAt:   meta.LastSyncedAt,
		})
	}
	return zones, nil
}

// ListRecords returns cached records, fetching from Cloudflare on first access.
func (s *Service) ListRecords(ctx context.Context, zoneName string) ([]model.DNSRecord, error) {
	zoneName = normalizeZone(zoneName)
	s.mu.Lock()
	if records, ok := s.records[zoneName]; ok {
		out := append([]model.DNSRecord(nil), records...)
		s.mu.Unlock()
		return out, nil
	}
	s.mu.Unlock()
	return s.RefreshZone(ctx, zoneName)
}

// RefreshZone forces a full record list from Cloudflare.
func (s *Service) RefreshZone(ctx context.Context, zoneName string) ([]model.DNSRecord, error) {
	zoneName = normalizeZone(zoneName)
	provider, meta, err := s.providerForZone(ctx, zoneName)
	if err != nil {
		return nil, err
	}
	remote, err := provider.ListRecords(ctx, meta.ProviderZoneID)
	if err != nil {
		return nil, providerError(err)
	}
	now := s.now().UTC()
	records := make([]model.DNSRecord, 0, len(remote))
	for _, item := range remote {
		records = append(records, toModelRecord(zoneName, item))
	}
	s.mu.Lock()
	meta.LastSyncedAt = &now
	s.zones[zoneName] = meta
	s.records[zoneName] = records
	out := append([]model.DNSRecord(nil), records...)
	s.mu.Unlock()
	return out, nil
}

// CreateRecord writes Cloudflare first, then updates the memory cache.
func (s *Service) CreateRecord(
	ctx context.Context,
	zoneName string,
	input cloudflare.RecordInput,
) (model.DNSRecord, error) {
	if err := validateRecord(input); err != nil {
		return model.DNSRecord{}, err
	}
	provider, meta, err := s.providerForZone(ctx, zoneName)
	if err != nil {
		return model.DNSRecord{}, err
	}
	remote, err := provider.CreateRecord(ctx, meta.ProviderZoneID, input)
	if err != nil {
		return model.DNSRecord{}, providerError(err)
	}
	record := toModelRecord(meta.Name, remote)
	s.mu.Lock()
	s.records[meta.Name] = append(s.records[meta.Name], record)
	s.mu.Unlock()
	return record, nil
}

// UpdateRecord writes Cloudflare first, then updates the memory cache.
func (s *Service) UpdateRecord(
	ctx context.Context,
	zoneName string,
	recordID string,
	input cloudflare.RecordInput,
) (model.DNSRecord, error) {
	if err := validateRecord(input); err != nil {
		return model.DNSRecord{}, err
	}
	provider, meta, err := s.providerForZone(ctx, zoneName)
	if err != nil {
		return model.DNSRecord{}, err
	}
	remote, err := provider.UpdateRecord(ctx, meta.ProviderZoneID, recordID, input)
	if err != nil {
		return model.DNSRecord{}, providerError(err)
	}
	record := toModelRecord(meta.Name, remote)
	s.mu.Lock()
	records := s.records[meta.Name]
	found := false
	for index := range records {
		if records[index].ProviderRecordID == recordID || records[index].ID == recordID {
			records[index] = record
			found = true
			break
		}
	}
	if !found {
		records = append(records, record)
	}
	s.records[meta.Name] = records
	s.mu.Unlock()
	return record, nil
}

// DeleteRecord deletes on Cloudflare and removes from the memory cache.
func (s *Service) DeleteRecord(ctx context.Context, zoneName string, recordID string) error {
	provider, meta, err := s.providerForZone(ctx, zoneName)
	if err != nil {
		return err
	}
	if err := provider.DeleteRecord(ctx, meta.ProviderZoneID, recordID); err != nil {
		return providerError(err)
	}
	s.mu.Lock()
	records := s.records[meta.Name]
	next := records[:0]
	for _, record := range records {
		if record.ProviderRecordID == recordID || record.ID == recordID {
			continue
		}
		next = append(next, record)
	}
	s.records[meta.Name] = next
	s.mu.Unlock()
	return nil
}

func (s *Service) providerForZone(ctx context.Context, zoneName string) (Provider, zoneMeta, error) {
	s.mu.Lock()
	token := s.token
	s.mu.Unlock()
	if token == "" {
		return nil, zoneMeta{}, ErrNotConfigured
	}
	provider := s.providerFactory(token)
	meta, err := s.ensureZone(ctx, provider, zoneName)
	if err != nil {
		return nil, zoneMeta{}, err
	}
	return provider, meta, nil
}

func (s *Service) ensureZone(ctx context.Context, provider Provider, zoneName string) (zoneMeta, error) {
	zoneName = normalizeZone(zoneName)
	s.mu.Lock()
	if meta, ok := s.zones[zoneName]; ok {
		s.mu.Unlock()
		return meta, nil
	}
	allowed := false
	for _, name := range s.zoneNames {
		if name == zoneName {
			allowed = true
			break
		}
	}
	s.mu.Unlock()
	if !allowed {
		return zoneMeta{}, fmt.Errorf("%w: zone", ErrNotFound)
	}
	remote, err := provider.FindZone(ctx, zoneName)
	if err != nil {
		if errors.Is(err, cloudflare.ErrNotFound) {
			return zoneMeta{}, fmt.Errorf("%w: zone", ErrNotFound)
		}
		return zoneMeta{}, providerError(err)
	}
	meta := zoneMeta{
		ProviderZoneID: remote.ID,
		Name:           strings.ToLower(remote.Name),
		Status:         remote.Status,
	}
	s.mu.Lock()
	s.zones[meta.Name] = meta
	s.mu.Unlock()
	return meta, nil
}

func toModelRecord(zoneName string, remote cloudflare.Record) model.DNSRecord {
	return model.DNSRecord{
		ID:               remote.ID,
		ZoneName:         zoneName,
		ProviderRecordID: remote.ID,
		Type:             remote.Type,
		Name:             remote.Name,
		Content:          remote.Content,
		TTL:              remote.TTL,
		Proxied:          remote.Proxied,
		Priority:         remote.Priority,
		Data:             remote.Data,
		Comment:          remote.Comment,
		ProviderCreated:  remote.CreatedOn,
		ProviderModified: remote.ModifiedOn,
	}
}

func normalizeZone(name string) string {
	return strings.TrimSpace(strings.ToLower(name))
}

func providerError(err error) error {
	return fmt.Errorf("%w: %w", ErrProvider, err)
}
