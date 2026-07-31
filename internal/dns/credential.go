package dns

import (
	"context"
	"fmt"
	"strings"

	"home-gateway/internal/model"
)

// ListCredentials returns safe credential metadata.
func (s *Service) ListCredentials(ctx context.Context) ([]model.CloudflareCredential, error) {
	var items []model.CloudflareCredential
	err := s.db.SelectContext(ctx, &items, `
		SELECT id, name, token_hint, created_at, updated_at
		FROM cloudflare_credentials ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	return items, nil
}

// CreateCredential verifies and stores an encrypted API Token.
func (s *Service) CreateCredential(
	ctx context.Context,
	name string,
	token string,
) (model.CloudflareCredential, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 128 {
		return model.CloudflareCredential{}, fmt.Errorf(
			"%w: credential name must contain 1 to 128 characters",
			ErrInvalidInput,
		)
	}
	ciphertext, nonce, fingerprint, hint, err := s.encryptor.Encrypt(token)
	if err != nil {
		return model.CloudflareCredential{}, err
	}
	if err := s.providerFactory(token).VerifyToken(ctx); err != nil {
		return model.CloudflareCredential{}, providerError(err)
	}

	var count int
	queryCount := s.db.Rebind(`
		SELECT COUNT(*) FROM cloudflare_credentials
		WHERE name = ? OR token_fingerprint = ?
	`)
	if err := s.db.GetContext(ctx, &count, queryCount, name, fingerprint); err != nil {
		return model.CloudflareCredential{}, fmt.Errorf("check credential: %w", err)
	}
	if count > 0 {
		return model.CloudflareCredential{}, ErrConflict
	}

	now := s.now().UTC()
	query := s.db.Rebind(`
		INSERT INTO cloudflare_credentials
		    (name, token_ciphertext, token_nonce, token_fingerprint, token_hint, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if _, err := s.db.ExecContext(
		ctx,
		query,
		name,
		ciphertext,
		nonce,
		fingerprint,
		hint,
		now,
		now,
	); err != nil {
		return model.CloudflareCredential{}, fmt.Errorf("create credential: %w", err)
	}

	var item model.CloudflareCredential
	selectQuery := s.db.Rebind(`
		SELECT id, name, token_hint, created_at, updated_at
		FROM cloudflare_credentials WHERE token_fingerprint = ?
	`)
	if err := s.db.GetContext(ctx, &item, selectQuery, fingerprint); err != nil {
		return model.CloudflareCredential{}, fmt.Errorf("read credential: %w", err)
	}
	return item, nil
}

// UpdateCredential replaces and re-encrypts a stored API Token.
func (s *Service) UpdateCredential(
	ctx context.Context,
	id int64,
	token string,
) (model.CloudflareCredential, error) {
	ciphertext, nonce, fingerprint, hint, err := s.encryptor.Encrypt(token)
	if err != nil {
		return model.CloudflareCredential{}, err
	}
	if err := s.providerFactory(token).VerifyToken(ctx); err != nil {
		return model.CloudflareCredential{}, providerError(err)
	}
	now := s.now().UTC()
	query := s.db.Rebind(`
		UPDATE cloudflare_credentials
		SET token_ciphertext = ?, token_nonce = ?, token_fingerprint = ?,
		    token_hint = ?, updated_at = ?
		WHERE id = ?
	`)
	result, err := s.db.ExecContext(ctx, query, ciphertext, nonce, fingerprint, hint, now, id)
	if err != nil {
		return model.CloudflareCredential{}, fmt.Errorf("update credential: %w", err)
	}
	if !rowsChanged(result) {
		return model.CloudflareCredential{}, ErrNotFound
	}
	item, err := s.credentialByID(ctx, id)
	if err != nil {
		return model.CloudflareCredential{}, err
	}
	item.TokenCiphertext = nil
	item.TokenNonce = nil
	item.TokenFingerprint = ""
	return item, nil
}

// DeleteCredential removes an unbound credential.
func (s *Service) DeleteCredential(ctx context.Context, id int64) error {
	var zones int
	queryCount := s.db.Rebind(`SELECT COUNT(*) FROM dns_zones WHERE credential_id = ?`)
	if err := s.db.GetContext(ctx, &zones, queryCount, id); err != nil {
		return fmt.Errorf("check credential zones: %w", err)
	}
	if zones > 0 {
		return ErrConflict
	}
	query := s.db.Rebind(`DELETE FROM cloudflare_credentials WHERE id = ?`)
	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete credential: %w", err)
	}
	if !rowsChanged(result) {
		return ErrNotFound
	}
	return nil
}
