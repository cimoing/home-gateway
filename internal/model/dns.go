package model

import "time"

// DNSZone is a Cloudflare zone from configuration.
type DNSZone struct {
	ProviderZoneID string     `json:"providerZoneId"`
	Name           string     `json:"name"`
	Status         string     `json:"status"`
	LastSyncedAt   *time.Time `json:"lastSyncedAt,omitempty"`
}

// DNSRecord is a Cloudflare DNS record (memory-cached).
type DNSRecord struct {
	ID               string         `json:"id"`
	ZoneName         string         `json:"zoneName"`
	ProviderRecordID string         `json:"providerRecordId"`
	Type             string         `json:"type"`
	Name             string         `json:"name"`
	Content          string         `json:"content"`
	TTL              int            `json:"ttl"`
	Proxied          *bool          `json:"proxied,omitempty"`
	Priority         *int           `json:"priority,omitempty"`
	Data             map[string]any `json:"data,omitempty"`
	Comment          string         `json:"comment"`
	ProviderCreated  *time.Time     `json:"providerCreatedAt,omitempty"`
	ProviderModified *time.Time     `json:"providerModifiedAt,omitempty"`
}
