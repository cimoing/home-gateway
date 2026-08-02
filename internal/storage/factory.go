package storage

import (
	"encoding/json"
	"fmt"

	"home-gateway/internal/model"
)

// LocalConfig is persisted in config_json for local backends.
type LocalConfig struct {
	Root string `json:"root"`
}

// SMBConfigJSON is persisted in config_json for smb backends.
type SMBConfigJSON struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Share    string `json:"share"`
	Username string `json:"username"`
	Domain   string `json:"domain"`
}

// S3ConfigJSON is persisted in config_json for s3 backends.
type S3ConfigJSON struct {
	Endpoint       string `json:"endpoint"`
	Region         string `json:"region"`
	Bucket         string `json:"bucket"`
	Prefix         string `json:"prefix"`
	AccessKeyID    string `json:"accessKeyId"`
	ForcePathStyle bool   `json:"forcePathStyle"`
}

// OpenBackend builds a live backend from a persisted row and decrypted secret.
func OpenBackend(backend model.StorageBackend, secret string) (Backend, error) {
	switch backend.Type {
	case model.StorageTypeLocal:
		var cfg LocalConfig
		if err := json.Unmarshal([]byte(backend.ConfigJSON), &cfg); err != nil {
			return nil, fmt.Errorf("%w: invalid local config", ErrInvalidInput)
		}
		return newLocalBackend(cfg.Root)
	case model.StorageTypeSMB:
		var cfg SMBConfigJSON
		if err := json.Unmarshal([]byte(backend.ConfigJSON), &cfg); err != nil {
			return nil, fmt.Errorf("%w: invalid smb config", ErrInvalidInput)
		}
		return newSMBBackend(smbConfig{
			Host:     cfg.Host,
			Port:     cfg.Port,
			Share:    cfg.Share,
			Username: cfg.Username,
			Domain:   cfg.Domain,
			Password: secret,
		})
	case model.StorageTypeS3:
		var cfg S3ConfigJSON
		if err := json.Unmarshal([]byte(backend.ConfigJSON), &cfg); err != nil {
			return nil, fmt.Errorf("%w: invalid s3 config", ErrInvalidInput)
		}
		return newS3Backend(s3Config{
			Endpoint:        cfg.Endpoint,
			Region:          cfg.Region,
			Bucket:          cfg.Bucket,
			Prefix:          cfg.Prefix,
			AccessKeyID:     cfg.AccessKeyID,
			SecretAccessKey: secret,
			ForcePathStyle:  cfg.ForcePathStyle,
		})
	default:
		return nil, fmt.Errorf("%w: unsupported storage type %q", ErrInvalidInput, backend.Type)
	}
}

// PublicConfig returns non-secret config for API responses.
func PublicConfig(backend model.StorageBackend) (map[string]any, error) {
	switch backend.Type {
	case model.StorageTypeLocal:
		var cfg LocalConfig
		if err := json.Unmarshal([]byte(backend.ConfigJSON), &cfg); err != nil {
			return nil, err
		}
		return map[string]any{"root": cfg.Root}, nil
	case model.StorageTypeSMB:
		var cfg SMBConfigJSON
		if err := json.Unmarshal([]byte(backend.ConfigJSON), &cfg); err != nil {
			return nil, err
		}
		return map[string]any{
			"host": cfg.Host, "port": cfg.Port, "share": cfg.Share,
			"username": cfg.Username, "domain": cfg.Domain,
		}, nil
	case model.StorageTypeS3:
		var cfg S3ConfigJSON
		if err := json.Unmarshal([]byte(backend.ConfigJSON), &cfg); err != nil {
			return nil, err
		}
		return map[string]any{
			"endpoint": cfg.Endpoint, "region": cfg.Region, "bucket": cfg.Bucket,
			"prefix": cfg.Prefix, "accessKeyId": cfg.AccessKeyID,
			"forcePathStyle": cfg.ForcePathStyle,
		}, nil
	default:
		return map[string]any{}, nil
	}
}
