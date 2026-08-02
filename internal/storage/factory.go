package storage

import (
	"fmt"
	"path/filepath"
	"strings"

	"home-gateway/internal/config"
	"home-gateway/internal/model"
)

// LocalConfig describes a local filesystem backend.
type LocalConfig struct {
	Root string `json:"root"`
}

// SMBConfigJSON describes an SMB backend.
type SMBConfigJSON struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Share    string `json:"share"`
	Username string `json:"username"`
	Domain   string `json:"domain"`
}

// S3ConfigJSON describes an S3-compatible backend.
type S3ConfigJSON struct {
	Endpoint       string `json:"endpoint"`
	Region         string `json:"region"`
	Bucket         string `json:"bucket"`
	Prefix         string `json:"prefix"`
	AccessKeyID    string `json:"accessKeyId"`
	ForcePathStyle bool   `json:"forcePathStyle"`
}

// OpenFromConfig builds a live backend from YAML settings.
func OpenFromConfig(backend config.StorageBackendConfig) (Backend, error) {
	secret := backend.Secret
	switch strings.ToLower(strings.TrimSpace(backend.Type)) {
	case model.StorageTypeLocal:
		root := filepath.Clean(stringConfig(backend.Config, "root"))
		if root == "" || !filepath.IsAbs(root) {
			return nil, fmt.Errorf("%w: local root must be an absolute path", ErrInvalidInput)
		}
		return newLocalBackend(root)
	case model.StorageTypeSMB:
		cfg := SMBConfigJSON{
			Host:     stringConfig(backend.Config, "host"),
			Share:    stringConfig(backend.Config, "share"),
			Username: stringConfig(backend.Config, "username"),
			Domain:   stringConfig(backend.Config, "domain"),
			Port:     intConfig(backend.Config, "port", 445),
		}
		if cfg.Host == "" || cfg.Share == "" || cfg.Username == "" {
			return nil, fmt.Errorf("%w: smb host, share, and username are required", ErrInvalidInput)
		}
		if strings.TrimSpace(secret) == "" {
			return nil, fmt.Errorf("%w: smb password is required", ErrInvalidInput)
		}
		return newSMBBackend(smbConfig{
			Host: cfg.Host, Port: cfg.Port, Share: cfg.Share,
			Username: cfg.Username, Domain: cfg.Domain, Password: secret,
		})
	case model.StorageTypeS3:
		cfg := S3ConfigJSON{
			Endpoint:       stringConfig(backend.Config, "endpoint"),
			Region:         stringConfig(backend.Config, "region"),
			Bucket:         stringConfig(backend.Config, "bucket"),
			Prefix:         stringConfig(backend.Config, "prefix"),
			AccessKeyID:    firstString(backend.Config, "accessKeyId", "access_key_id"),
			ForcePathStyle: boolConfig(backend.Config, "forcePathStyle") || boolConfig(backend.Config, "force_path_style"),
		}
		if cfg.Bucket == "" || cfg.AccessKeyID == "" {
			return nil, fmt.Errorf("%w: s3 bucket and access_key_id are required", ErrInvalidInput)
		}
		if strings.TrimSpace(secret) == "" {
			return nil, fmt.Errorf("%w: s3 secret is required", ErrInvalidInput)
		}
		return newS3Backend(s3Config{
			Endpoint: cfg.Endpoint, Region: cfg.Region, Bucket: cfg.Bucket,
			Prefix: cfg.Prefix, AccessKeyID: cfg.AccessKeyID,
			SecretAccessKey: secret, ForcePathStyle: cfg.ForcePathStyle,
		})
	default:
		return nil, fmt.Errorf("%w: unsupported storage type %q", ErrInvalidInput, backend.Type)
	}
}

// PublicConfig returns non-secret config for API responses.
func PublicConfig(backend config.StorageBackendConfig) map[string]any {
	switch strings.ToLower(backend.Type) {
	case model.StorageTypeLocal:
		return map[string]any{"root": stringConfig(backend.Config, "root")}
	case model.StorageTypeSMB:
		return map[string]any{
			"host": stringConfig(backend.Config, "host"),
			"port": intConfig(backend.Config, "port", 445),
			"share": stringConfig(backend.Config, "share"),
			"username": stringConfig(backend.Config, "username"),
			"domain": stringConfig(backend.Config, "domain"),
		}
	case model.StorageTypeS3:
		return map[string]any{
			"endpoint": stringConfig(backend.Config, "endpoint"),
			"region": stringConfig(backend.Config, "region"),
			"bucket": stringConfig(backend.Config, "bucket"),
			"prefix": stringConfig(backend.Config, "prefix"),
			"accessKeyId": firstString(backend.Config, "accessKeyId", "access_key_id"),
			"forcePathStyle": boolConfig(backend.Config, "forcePathStyle") || boolConfig(backend.Config, "force_path_style"),
		}
	default:
		return map[string]any{}
	}
}

func stringConfig(config map[string]any, key string) string {
	if config == nil {
		return ""
	}
	value, _ := config[key].(string)
	return strings.TrimSpace(value)
}

func firstString(config map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringConfig(config, key); value != "" {
			return value
		}
	}
	return ""
}

func intConfig(config map[string]any, key string, fallback int) int {
	if config == nil {
		return fallback
	}
	switch value := config[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case int64:
		return int(value)
	default:
		return fallback
	}
}

func boolConfig(config map[string]any, key string) bool {
	if config == nil {
		return false
	}
	value, _ := config[key].(bool)
	return value
}
