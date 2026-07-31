package dns

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"home-gateway/internal/cloudflare"
	"home-gateway/internal/model"
)

// ListZones returns configured DNS zones.
func (s *Service) ListZones(ctx context.Context) ([]model.DNSZone, error) {
	var zones []model.DNSZone
	err := s.db.SelectContext(ctx, &zones, `
		SELECT id, credential_id, provider_zone_id, name, status,
		       last_synced_at, created_at, updated_at
		FROM dns_zones ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("list zones: %w", err)
	}
	return zones, nil
}

// CreateZone resolves a Cloudflare zone, stores it, and initializes its cache.
func (s *Service) CreateZone(
	ctx context.Context,
	credentialID int64,
	name string,
) (model.DNSZone, error) {
	name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	if !validDomain(name) {
		return model.DNSZone{}, fmt.Errorf("%w: invalid domain name", ErrInvalidInput)
	}
	token, err := s.credentialToken(ctx, credentialID)
	if err != nil {
		return model.DNSZone{}, err
	}
	remote, err := s.providerFactory(token).FindZone(ctx, name)
	if err != nil {
		if errors.Is(err, cloudflare.ErrNotFound) {
			return model.DNSZone{}, ErrNotFound
		}
		return model.DNSZone{}, providerError(err)
	}

	var count int
	queryCount := s.db.Rebind(`SELECT COUNT(*) FROM dns_zones WHERE name = ? OR provider_zone_id = ?`)
	if err := s.db.GetContext(ctx, &count, queryCount, name, remote.ID); err != nil {
		return model.DNSZone{}, fmt.Errorf("check zone: %w", err)
	}
	if count > 0 {
		return model.DNSZone{}, ErrConflict
	}

	now := s.now().UTC()
	query := s.db.Rebind(`
		INSERT INTO dns_zones
		    (credential_id, provider_zone_id, name, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if _, err := s.db.ExecContext(
		ctx,
		query,
		credentialID,
		remote.ID,
		remote.Name,
		remote.Status,
		now,
		now,
	); err != nil {
		return model.DNSZone{}, fmt.Errorf("create zone: %w", err)
	}

	var zone model.DNSZone
	selectQuery := s.db.Rebind(`
		SELECT id, credential_id, provider_zone_id, name, status,
		       last_synced_at, created_at, updated_at
		FROM dns_zones WHERE provider_zone_id = ?
	`)
	if err := s.db.GetContext(ctx, &zone, selectQuery, remote.ID); err != nil {
		return model.DNSZone{}, fmt.Errorf("read zone: %w", err)
	}
	if _, err := s.SyncZone(ctx, zone.ID); err != nil {
		deleteQuery := s.db.Rebind(`DELETE FROM dns_zones WHERE id = ?`)
		_, _ = s.db.ExecContext(ctx, deleteQuery, zone.ID)
		return model.DNSZone{}, err
	}
	return s.zoneByID(ctx, zone.ID)
}

// DeleteZone removes a local zone and its cached records.
func (s *Service) DeleteZone(ctx context.Context, id int64) error {
	query := s.db.Rebind(`DELETE FROM dns_zones WHERE id = ?`)
	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete zone: %w", err)
	}
	if !rowsChanged(result) {
		return ErrNotFound
	}
	return nil
}
