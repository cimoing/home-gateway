package dns

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"home-gateway/internal/cloudflare"
	"home-gateway/internal/model"
)

// ListRecords returns the local cache for a zone.
func (s *Service) ListRecords(ctx context.Context, zoneID int64) ([]model.DNSRecord, error) {
	if _, err := s.zoneByID(ctx, zoneID); err != nil {
		return nil, err
	}
	var records []model.DNSRecord
	query := s.db.Rebind(`
		SELECT id, zone_id, provider_record_id, type, name, content, ttl,
		       proxied, priority, data_json, comment, provider_created_at,
		       provider_modified_at, synced_at, created_at, updated_at
		FROM dns_records WHERE zone_id = ? ORDER BY name, type
	`)
	if err := s.db.SelectContext(ctx, &records, query, zoneID); err != nil {
		return nil, fmt.Errorf("list DNS records: %w", err)
	}
	return records, nil
}

// CreateRecord writes Cloudflare first and then caches the returned record.
func (s *Service) CreateRecord(
	ctx context.Context,
	zoneID int64,
	input cloudflare.RecordInput,
) (model.DNSRecord, error) {
	if err := validateRecord(input); err != nil {
		return model.DNSRecord{}, err
	}
	zone, token, err := s.zoneAndToken(ctx, zoneID)
	if err != nil {
		return model.DNSRecord{}, err
	}
	remote, err := s.providerFactory(token).CreateRecord(ctx, zone.ProviderZoneID, input)
	if err != nil {
		return model.DNSRecord{}, providerError(err)
	}
	return s.cacheRecord(ctx, zoneID, remote, s.now().UTC())
}

// UpdateRecord writes Cloudflare first and refreshes the cache.
func (s *Service) UpdateRecord(
	ctx context.Context,
	zoneID int64,
	recordID int64,
	input cloudflare.RecordInput,
) (model.DNSRecord, error) {
	if err := validateRecord(input); err != nil {
		return model.DNSRecord{}, err
	}
	local, err := s.recordByID(ctx, zoneID, recordID)
	if err != nil {
		return model.DNSRecord{}, err
	}
	zone, token, err := s.zoneAndToken(ctx, zoneID)
	if err != nil {
		return model.DNSRecord{}, err
	}
	remote, err := s.providerFactory(token).UpdateRecord(
		ctx,
		zone.ProviderZoneID,
		local.ProviderRecordID,
		input,
	)
	if err != nil {
		return model.DNSRecord{}, providerError(err)
	}
	return s.cacheRecord(ctx, zoneID, remote, s.now().UTC())
}

// DeleteRecord deletes remotely before removing the cache row.
func (s *Service) DeleteRecord(ctx context.Context, zoneID int64, recordID int64) error {
	local, err := s.recordByID(ctx, zoneID, recordID)
	if err != nil {
		return err
	}
	zone, token, err := s.zoneAndToken(ctx, zoneID)
	if err != nil {
		return err
	}
	if err := s.providerFactory(token).DeleteRecord(
		ctx,
		zone.ProviderZoneID,
		local.ProviderRecordID,
	); err != nil {
		return providerError(err)
	}
	query := s.db.Rebind(`DELETE FROM dns_records WHERE id = ? AND zone_id = ?`)
	if _, err := s.db.ExecContext(ctx, query, recordID, zoneID); err != nil {
		return fmt.Errorf("delete cached DNS record: %w", err)
	}
	return nil
}

// SyncZone atomically reconciles the local cache with Cloudflare.
func (s *Service) SyncZone(ctx context.Context, zoneID int64) ([]model.DNSRecord, error) {
	zone, token, err := s.zoneAndToken(ctx, zoneID)
	if err != nil {
		return nil, err
	}
	remoteRecords, err := s.providerFactory(token).ListRecords(ctx, zone.ProviderZoneID)
	if err != nil {
		return nil, providerError(err)
	}
	now := s.now().UTC()

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin DNS sync: %w", err)
	}
	defer tx.Rollback()

	var existing []struct {
		ID               int64  `db:"id"`
		ProviderRecordID string `db:"provider_record_id"`
	}
	queryExisting := tx.Rebind(`
		SELECT id, provider_record_id FROM dns_records WHERE zone_id = ?
	`)
	if err := tx.SelectContext(ctx, &existing, queryExisting, zoneID); err != nil {
		return nil, fmt.Errorf("read DNS cache: %w", err)
	}
	existingByProvider := make(map[string]int64, len(existing))
	for _, item := range existing {
		existingByProvider[item.ProviderRecordID] = item.ID
	}

	seen := make(map[string]struct{}, len(remoteRecords))
	for _, remote := range remoteRecords {
		seen[remote.ID] = struct{}{}
		dataJSON, err := cacheValues(remote)
		if err != nil {
			return nil, err
		}
		if localID, ok := existingByProvider[remote.ID]; ok {
			query := tx.Rebind(`
				UPDATE dns_records SET type = ?, name = ?, content = ?, ttl = ?,
				    proxied = ?, priority = ?, data_json = ?, comment = ?,
				    provider_created_at = ?, provider_modified_at = ?,
				    synced_at = ?, updated_at = ?
				WHERE id = ?
			`)
			if _, err := tx.ExecContext(
				ctx, query, remote.Type, remote.Name, remote.Content, remote.TTL,
				remote.Proxied, remote.Priority, dataJSON, remote.Comment,
				remote.CreatedOn, remote.ModifiedOn, now, now, localID,
			); err != nil {
				return nil, fmt.Errorf("update DNS cache: %w", err)
			}
			continue
		}
		if err := insertCachedRecord(ctx, tx, zoneID, remote, dataJSON, now); err != nil {
			return nil, err
		}
	}

	for providerID := range existingByProvider {
		if _, ok := seen[providerID]; ok {
			continue
		}
		query := tx.Rebind(`DELETE FROM dns_records WHERE zone_id = ? AND provider_record_id = ?`)
		if _, err := tx.ExecContext(ctx, query, zoneID, providerID); err != nil {
			return nil, fmt.Errorf("delete stale DNS cache: %w", err)
		}
	}

	updateZone := tx.Rebind(`
		UPDATE dns_zones SET last_synced_at = ?, updated_at = ? WHERE id = ?
	`)
	if _, err := tx.ExecContext(ctx, updateZone, now, now, zoneID); err != nil {
		return nil, fmt.Errorf("update zone sync time: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit DNS sync: %w", err)
	}
	return s.ListRecords(ctx, zoneID)
}

func (s *Service) cacheRecord(
	ctx context.Context,
	zoneID int64,
	remote cloudflare.Record,
	now time.Time,
) (model.DNSRecord, error) {
	dataJSON, err := cacheValues(remote)
	if err != nil {
		return model.DNSRecord{}, err
	}
	var localID int64
	queryID := s.db.Rebind(`SELECT id FROM dns_records WHERE provider_record_id = ?`)
	err = s.db.GetContext(ctx, &localID, queryID, remote.ID)
	if err == nil {
		query := s.db.Rebind(`
			UPDATE dns_records SET zone_id = ?, type = ?, name = ?, content = ?,
			    ttl = ?, proxied = ?, priority = ?, data_json = ?, comment = ?,
			    provider_created_at = ?, provider_modified_at = ?,
			    synced_at = ?, updated_at = ? WHERE id = ?
		`)
		if _, err := s.db.ExecContext(
			ctx, query, zoneID, remote.Type, remote.Name, remote.Content,
			remote.TTL, remote.Proxied, remote.Priority, dataJSON, remote.Comment,
			remote.CreatedOn, remote.ModifiedOn, now, now, localID,
		); err != nil {
			return model.DNSRecord{}, fmt.Errorf("update cached DNS record: %w", err)
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		if err := insertCachedRecord(ctx, s.db, zoneID, remote, dataJSON, now); err != nil {
			return model.DNSRecord{}, err
		}
	} else {
		return model.DNSRecord{}, fmt.Errorf("find cached DNS record: %w", err)
	}
	queryRecord := s.db.Rebind(`
		SELECT id, zone_id, provider_record_id, type, name, content, ttl,
		       proxied, priority, data_json, comment, provider_created_at,
		       provider_modified_at, synced_at, created_at, updated_at
		FROM dns_records WHERE provider_record_id = ?
	`)
	var record model.DNSRecord
	if err := s.db.GetContext(ctx, &record, queryRecord, remote.ID); err != nil {
		return model.DNSRecord{}, fmt.Errorf("read cached DNS record: %w", err)
	}
	return record, nil
}

type rebindExecer interface {
	Rebind(string) string
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertCachedRecord(
	ctx context.Context,
	execer rebindExecer,
	zoneID int64,
	remote cloudflare.Record,
	dataJSON string,
	now time.Time,
) error {
	query := execer.Rebind(`
		INSERT INTO dns_records
		    (zone_id, provider_record_id, type, name, content, ttl, proxied,
		     priority, data_json, comment, provider_created_at,
		     provider_modified_at, synced_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if _, err := execer.ExecContext(
		ctx, query, zoneID, remote.ID, strings.ToUpper(remote.Type), remote.Name,
		remote.Content, remote.TTL, remote.Proxied, remote.Priority, dataJSON,
		remote.Comment, remote.CreatedOn, remote.ModifiedOn, now, now, now,
	); err != nil {
		return fmt.Errorf("insert cached DNS record: %w", err)
	}
	return nil
}
