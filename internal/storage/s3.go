package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type s3Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	Prefix          string
	AccessKeyID     string
	SecretAccessKey string
	ForcePathStyle  bool
}

type s3Backend struct {
	cfg    s3Config
	client *s3.Client
	prefix string
}

func newS3Backend(cfg s3Config) (*s3Backend, error) {
	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	cfg.Region = strings.TrimSpace(cfg.Region)
	cfg.Bucket = strings.TrimSpace(cfg.Bucket)
	cfg.Prefix = strings.Trim(strings.TrimSpace(cfg.Prefix), "/")
	cfg.AccessKeyID = strings.TrimSpace(cfg.AccessKeyID)
	if cfg.Bucket == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, fmt.Errorf("%w: s3 bucket, access key, and secret are required", ErrInvalidInput)
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	options := s3.Options{
		Region:      cfg.Region,
		Credentials: credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		UsePathStyle: cfg.ForcePathStyle,
	}
	if cfg.Endpoint != "" {
		options.BaseEndpoint = aws.String(cfg.Endpoint)
		// Custom endpoints (MinIO, etc.) typically require path-style addressing.
		if !cfg.ForcePathStyle {
			options.UsePathStyle = true
		}
	}
	return &s3Backend{cfg: cfg, client: s3.New(options), prefix: cfg.Prefix}, nil
}

func (b *s3Backend) objectKey(relative string) (string, error) {
	cleaned, err := cleanRelativePath(relative)
	if err != nil {
		return "", err
	}
	if b.prefix == "" {
		return cleaned, nil
	}
	if cleaned == "" {
		return b.prefix, nil
	}
	return b.prefix + "/" + cleaned, nil
}

func (b *s3Backend) Ping(ctx context.Context) error {
	_, err := b.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:  aws.String(b.cfg.Bucket),
		Prefix:  aws.String(prefixWithSlash(b.prefix)),
		MaxKeys: aws.Int32(1),
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return nil
}

func (b *s3Backend) List(ctx context.Context, dir string) ([]Entry, error) {
	cleaned, err := cleanRelativePath(dir)
	if err != nil {
		return nil, err
	}
	prefix, err := b.objectKey(cleaned)
	if err != nil {
		return nil, err
	}
	listPrefix := prefixWithSlash(prefix)
	output, err := b.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:    aws.String(b.cfg.Bucket),
		Prefix:    aws.String(listPrefix),
		Delimiter: aws.String("/"),
	})
	if err != nil {
		return nil, err
	}
	result := make([]Entry, 0, len(output.CommonPrefixes)+len(output.Contents))
	for _, common := range output.CommonPrefixes {
		full := strings.TrimSuffix(aws.ToString(common.Prefix), "/")
		rel := b.stripPrefix(full)
		if rel == "" {
			continue
		}
		result = append(result, Entry{
			Name:  path.Base(rel),
			Path:  rel,
			IsDir: true,
		})
	}
	for _, object := range output.Contents {
		key := aws.ToString(object.Key)
		if key == listPrefix || strings.HasSuffix(key, "/") {
			continue
		}
		rel := b.stripPrefix(key)
		if rel == "" || strings.Contains(strings.TrimPrefix(rel, cleaned+"/"), "/") {
			continue
		}
		mod := time.Time{}
		if object.LastModified != nil {
			mod = object.LastModified.UTC()
		}
		result = append(result, Entry{
			Name:    path.Base(rel),
			Path:    rel,
			IsDir:   false,
			Size:    aws.ToInt64(object.Size),
			ModTime: mod,
		})
	}
	return result, nil
}

func (b *s3Backend) Mkdir(ctx context.Context, dir string) error {
	cleaned, err := cleanRelativePath(dir)
	if err != nil || cleaned == "" {
		if err != nil {
			return err
		}
		return fmt.Errorf("%w: directory path is required", ErrInvalidInput)
	}
	key, err := b.objectKey(cleaned)
	if err != nil {
		return err
	}
	_, err = b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(b.cfg.Bucket),
		Key:    aws.String(strings.TrimSuffix(key, "/") + "/"),
		Body:   bytes.NewReader(nil),
	})
	return err
}

func (b *s3Backend) Remove(ctx context.Context, target string, recursive bool) error {
	cleaned, err := cleanRelativePath(target)
	if err != nil || cleaned == "" {
		if err != nil {
			return err
		}
		return fmt.Errorf("%w: refusing to remove bucket root", ErrInvalidInput)
	}
	key, err := b.objectKey(cleaned)
	if err != nil {
		return err
	}
	// Try delete as object first.
	_, err = b.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(b.cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return err
	}
	prefix := prefixWithSlash(key)
	listed, err := b.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(b.cfg.Bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return err
	}
	if len(listed.Contents) == 0 {
		return nil
	}
	if !recursive {
		return ErrNotEmpty
	}
	objects := make([]types.ObjectIdentifier, 0, len(listed.Contents))
	for _, object := range listed.Contents {
		objects = append(objects, types.ObjectIdentifier{Key: object.Key})
	}
	_, err = b.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(b.cfg.Bucket),
		Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
	})
	return err
}

func (b *s3Backend) Rename(ctx context.Context, from string, to string) error {
	srcRel, err := cleanRelativePath(from)
	if err != nil || srcRel == "" {
		return fmt.Errorf("%w: invalid source path", ErrInvalidInput)
	}
	dstRel, err := cleanRelativePath(to)
	if err != nil || dstRel == "" {
		return fmt.Errorf("%w: invalid destination path", ErrInvalidInput)
	}
	srcKey, err := b.objectKey(srcRel)
	if err != nil {
		return err
	}
	dstKey, err := b.objectKey(dstRel)
	if err != nil {
		return err
	}
	_, err = b.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(b.cfg.Bucket),
		Key:    aws.String(dstKey),
	})
	if err == nil {
		return ErrConflict
	}
	_, err = b.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(b.cfg.Bucket),
		Key:        aws.String(dstKey),
		CopySource: aws.String(b.cfg.Bucket + "/" + srcKey),
	})
	if err != nil {
		return err
	}
	_, err = b.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(b.cfg.Bucket),
		Key:    aws.String(srcKey),
	})
	return err
}

func (b *s3Backend) Open(ctx context.Context, filePath string) (io.ReadCloser, error) {
	cleaned, err := cleanRelativePath(filePath)
	if err != nil || cleaned == "" {
		return nil, fmt.Errorf("%w: file path is required", ErrInvalidInput)
	}
	key, err := b.objectKey(cleaned)
	if err != nil {
		return nil, err
	}
	output, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return output.Body, nil
}

func (b *s3Backend) Create(ctx context.Context, filePath string) (io.WriteCloser, error) {
	cleaned, err := cleanRelativePath(filePath)
	if err != nil || cleaned == "" {
		return nil, fmt.Errorf("%w: file path is required", ErrInvalidInput)
	}
	key, err := b.objectKey(cleaned)
	if err != nil {
		return nil, err
	}
	return &s3DeferredWriter{backend: b, key: key}, nil
}

func (b *s3Backend) Stat(ctx context.Context, target string) (Entry, error) {
	cleaned, err := cleanRelativePath(target)
	if err != nil {
		return Entry{}, err
	}
	if cleaned == "" {
		return Entry{Name: "", Path: "", IsDir: true}, nil
	}
	key, err := b.objectKey(cleaned)
	if err != nil {
		return Entry{}, err
	}
	output, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(b.cfg.Bucket),
		Key:    aws.String(key),
	})
	if err == nil {
		mod := time.Time{}
		if output.LastModified != nil {
			mod = output.LastModified.UTC()
		}
		return Entry{
			Name:    path.Base(cleaned),
			Path:    cleaned,
			IsDir:   false,
			Size:    aws.ToInt64(output.ContentLength),
			ModTime: mod,
		}, nil
	}
	// Directory marker or prefix.
	listed, listErr := b.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:  aws.String(b.cfg.Bucket),
		Prefix:  aws.String(prefixWithSlash(key)),
		MaxKeys: aws.Int32(1),
	})
	if listErr != nil {
		return Entry{}, err
	}
	if len(listed.Contents) == 0 && len(listed.CommonPrefixes) == 0 {
		return Entry{}, ErrNotFound
	}
	return Entry{Name: path.Base(cleaned), Path: cleaned, IsDir: true}, nil
}

func (b *s3Backend) Close() error { return nil }

func (b *s3Backend) stripPrefix(key string) string {
	key = strings.TrimPrefix(key, b.prefix)
	return strings.TrimPrefix(key, "/")
}

func prefixWithSlash(prefix string) string {
	if prefix == "" {
		return ""
	}
	if strings.HasSuffix(prefix, "/") {
		return prefix
	}
	return prefix + "/"
}

type s3DeferredWriter struct {
	backend *s3Backend
	key     string
	buf     bytes.Buffer
	closed  bool
}

func (w *s3DeferredWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, errors.New("writer closed")
	}
	return w.buf.Write(p)
}

func (w *s3DeferredWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	_, err := w.backend.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(w.backend.cfg.Bucket),
		Key:    aws.String(w.key),
		Body:   bytes.NewReader(w.buf.Bytes()),
	})
	return err
}
