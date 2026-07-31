package dns

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"home-gateway/internal/cloudflare"
	"home-gateway/internal/credential"
	"home-gateway/internal/model"

	"github.com/jmoiron/sqlx"
)

var (
	ErrNotFound      = errors.New("resource not found")
	ErrConflict      = errors.New("resource conflicts with existing data")
	ErrInvalidInput  = errors.New("invalid input")
	ErrProvider      = errors.New("Cloudflare operation failed")
	ErrNotConfigured = credential.ErrNotConfigured
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

// Service manages encrypted credentials, zones, and the local DNS cache.
type Service struct {
	db              *sqlx.DB
	encryptor       *credential.Encryptor
	providerFactory ProviderFactory
	now             func() time.Time
}

// NewService creates the DNS management service.
func NewService(
	db *sqlx.DB,
	encryptor *credential.Encryptor,
	factories ...ProviderFactory,
) *Service {
	factory := ProviderFactory(func(token string) Provider {
		return cloudflare.NewClient(token)
	})
	if len(factories) > 0 && factories[0] != nil {
		factory = factories[0]
	}
	return &Service{db: db, encryptor: encryptor, providerFactory: factory, now: time.Now}
}

func (s *Service) credentialByID(ctx context.Context, id int64) (model.CloudflareCredential, error) {
	var item model.CloudflareCredential
	query := s.db.Rebind(`
		SELECT id, name, token_ciphertext, token_nonce, token_fingerprint,
		       token_hint, created_at, updated_at
		FROM cloudflare_credentials WHERE id = ?
	`)
	if err := s.db.GetContext(ctx, &item, query, id); err != nil {
		return model.CloudflareCredential{}, mapNotFound(err, "credential")
	}
	return item, nil
}

func (s *Service) credentialToken(ctx context.Context, id int64) (string, error) {
	item, err := s.credentialByID(ctx, id)
	if err != nil {
		return "", err
	}
	return s.encryptor.Decrypt(item.TokenCiphertext, item.TokenNonce)
}

func (s *Service) zoneByID(ctx context.Context, id int64) (model.DNSZone, error) {
	var zone model.DNSZone
	query := s.db.Rebind(`
		SELECT id, credential_id, provider_zone_id, name, status,
		       last_synced_at, created_at, updated_at
		FROM dns_zones WHERE id = ?
	`)
	if err := s.db.GetContext(ctx, &zone, query, id); err != nil {
		return model.DNSZone{}, mapNotFound(err, "zone")
	}
	return zone, nil
}

func (s *Service) zoneAndToken(
	ctx context.Context,
	zoneID int64,
) (model.DNSZone, string, error) {
	zone, err := s.zoneByID(ctx, zoneID)
	if err != nil {
		return model.DNSZone{}, "", err
	}
	token, err := s.credentialToken(ctx, zone.CredentialID)
	if err != nil {
		return model.DNSZone{}, "", err
	}
	return zone, token, nil
}

func (s *Service) recordByID(
	ctx context.Context,
	zoneID int64,
	recordID int64,
) (model.DNSRecord, error) {
	var record model.DNSRecord
	query := s.db.Rebind(`
		SELECT id, zone_id, provider_record_id, type, name, content, ttl,
		       proxied, priority, data_json, comment, provider_created_at,
		       provider_modified_at, synced_at, created_at, updated_at
		FROM dns_records WHERE id = ? AND zone_id = ?
	`)
	if err := s.db.GetContext(ctx, &record, query, recordID, zoneID); err != nil {
		return model.DNSRecord{}, mapNotFound(err, "DNS record")
	}
	return record, nil
}

func cacheValues(record cloudflare.Record) (string, error) {
	if record.Data == nil {
		return "{}", nil
	}
	data, err := json.Marshal(record.Data)
	if err != nil {
		return "", fmt.Errorf("encode DNS record data: %w", err)
	}
	return string(data), nil
}

func providerError(err error) error {
	return fmt.Errorf("%w: %w", ErrProvider, err)
}

func mapNotFound(err error, resource string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrNotFound, resource)
	}
	return err
}

func rowsChanged(result sql.Result) bool {
	count, err := result.RowsAffected()
	return err == nil && count > 0
}
