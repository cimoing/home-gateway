package model

import "time"

// CloudflareCredential stores encrypted API token material.
type CloudflareCredential struct {
	ID               int64     `db:"id" json:"id"`
	Name             string    `db:"name" json:"name"`
	TokenCiphertext  []byte    `db:"token_ciphertext" json:"-"`
	TokenNonce       []byte    `db:"token_nonce" json:"-"`
	TokenFingerprint string    `db:"token_fingerprint" json:"-"`
	TokenHint        string    `db:"token_hint" json:"tokenHint"`
	CreatedAt        time.Time `db:"created_at" json:"createdAt"`
	UpdatedAt        time.Time `db:"updated_at" json:"updatedAt"`
}

// DNSZone is a Cloudflare zone bound to a stored credential.
type DNSZone struct {
	ID             int64      `db:"id" json:"id"`
	CredentialID   int64      `db:"credential_id" json:"credentialId"`
	ProviderZoneID string     `db:"provider_zone_id" json:"providerZoneId"`
	Name           string     `db:"name" json:"name"`
	Status         string     `db:"status" json:"status"`
	LastSyncedAt   *time.Time `db:"last_synced_at" json:"lastSyncedAt,omitempty"`
	CreatedAt      time.Time  `db:"created_at" json:"createdAt"`
	UpdatedAt      time.Time  `db:"updated_at" json:"updatedAt"`
}

// DNSRecord is the local cache of a Cloudflare DNS record.
type DNSRecord struct {
	ID               int64      `db:"id" json:"id"`
	ZoneID           int64      `db:"zone_id" json:"zoneId"`
	ProviderRecordID string     `db:"provider_record_id" json:"providerRecordId"`
	Type             string     `db:"type" json:"type"`
	Name             string     `db:"name" json:"name"`
	Content          string     `db:"content" json:"content"`
	TTL              int        `db:"ttl" json:"ttl"`
	Proxied          *bool      `db:"proxied" json:"proxied,omitempty"`
	Priority         *int       `db:"priority" json:"priority,omitempty"`
	DataJSON         string     `db:"data_json" json:"dataJson"`
	Comment          string     `db:"comment" json:"comment"`
	ProviderCreated  *time.Time `db:"provider_created_at" json:"providerCreatedAt,omitempty"`
	ProviderModified *time.Time `db:"provider_modified_at" json:"providerModifiedAt,omitempty"`
	SyncedAt         time.Time  `db:"synced_at" json:"syncedAt"`
	CreatedAt        time.Time  `db:"created_at" json:"createdAt"`
	UpdatedAt        time.Time  `db:"updated_at" json:"updatedAt"`
}
