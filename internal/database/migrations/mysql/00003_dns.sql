-- +goose Up
CREATE TABLE cloudflare_credentials (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name VARCHAR(128) NOT NULL,
    token_ciphertext BLOB NOT NULL,
    token_nonce VARBINARY(32) NOT NULL,
    token_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    token_hint VARCHAR(4) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_cloudflare_credentials_name (name),
    UNIQUE KEY uq_cloudflare_credentials_fingerprint (token_fingerprint)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE dns_zones (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    credential_id BIGINT UNSIGNED NOT NULL,
    provider_zone_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    name VARCHAR(253) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT '',
    last_synced_at DATETIME(6),
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_dns_zones_provider_zone_id (provider_zone_id),
    UNIQUE KEY uq_dns_zones_name (name),
    KEY idx_dns_zones_credential_id (credential_id),
    CONSTRAINT fk_dns_zones_credential
        FOREIGN KEY (credential_id) REFERENCES cloudflare_credentials (id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE dns_records (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    zone_id BIGINT UNSIGNED NOT NULL,
    provider_record_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    type VARCHAR(16) NOT NULL,
    name VARCHAR(253) NOT NULL,
    content TEXT NOT NULL,
    ttl INT NOT NULL,
    proxied BOOLEAN,
    priority INT,
    data_json TEXT NOT NULL,
    comment VARCHAR(500) NOT NULL DEFAULT '',
    provider_created_at DATETIME(6),
    provider_modified_at DATETIME(6),
    synced_at DATETIME(6) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_dns_records_provider_record_id (provider_record_id),
    KEY idx_dns_records_zone_id (zone_id),
    KEY idx_dns_records_zone_name_type (zone_id, name, type),
    CONSTRAINT fk_dns_records_zone
        FOREIGN KEY (zone_id) REFERENCES dns_zones (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS dns_records;
DROP TABLE IF EXISTS dns_zones;
DROP TABLE IF EXISTS cloudflare_credentials;
