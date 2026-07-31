-- +goose Up
CREATE TABLE cloudflare_credentials (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(128) NOT NULL UNIQUE,
    token_ciphertext BYTEA NOT NULL,
    token_nonce BYTEA NOT NULL,
    token_fingerprint CHAR(64) NOT NULL UNIQUE,
    token_hint VARCHAR(4) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE dns_zones (
    id BIGSERIAL PRIMARY KEY,
    credential_id BIGINT NOT NULL,
    provider_zone_id VARCHAR(64) NOT NULL UNIQUE,
    name VARCHAR(253) NOT NULL UNIQUE,
    status VARCHAR(32) NOT NULL DEFAULT '',
    last_synced_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_dns_zones_credential
        FOREIGN KEY (credential_id) REFERENCES cloudflare_credentials (id) ON DELETE RESTRICT
);

CREATE INDEX idx_dns_zones_credential_id ON dns_zones (credential_id);

CREATE TABLE dns_records (
    id BIGSERIAL PRIMARY KEY,
    zone_id BIGINT NOT NULL,
    provider_record_id VARCHAR(64) NOT NULL UNIQUE,
    type VARCHAR(16) NOT NULL,
    name VARCHAR(253) NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    ttl INTEGER NOT NULL,
    proxied BOOLEAN,
    priority INTEGER,
    data_json TEXT NOT NULL DEFAULT '{}',
    comment VARCHAR(500) NOT NULL DEFAULT '',
    provider_created_at TIMESTAMPTZ,
    provider_modified_at TIMESTAMPTZ,
    synced_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_dns_records_zone
        FOREIGN KEY (zone_id) REFERENCES dns_zones (id) ON DELETE CASCADE
);

CREATE INDEX idx_dns_records_zone_id ON dns_records (zone_id);
CREATE INDEX idx_dns_records_zone_name_type ON dns_records (zone_id, name, type);

-- +goose Down
DROP TABLE IF EXISTS dns_records;
DROP TABLE IF EXISTS dns_zones;
DROP TABLE IF EXISTS cloudflare_credentials;
