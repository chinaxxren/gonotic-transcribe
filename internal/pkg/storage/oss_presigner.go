package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"go.uber.org/zap"
)

// PresignedURLGenerator defines the interface for generating presigned URLs.
type PresignedURLGenerator interface {
	GeneratePresignedURL(ctx context.Context, key string) (string, error)
}

// OSSConfig contains configuration for generating presigned URLs for Aliyun OSS.
type OSSConfig struct {
	Endpoint        string
	AccessKeyID     string
	AccessKeySecret string
	Bucket          string
	PresignTTL      time.Duration
}

// OSSPresigner generates presigned URLs for Aliyun OSS objects.
type OSSPresigner struct {
	client    *oss.Client
	bucket    *oss.Bucket
	expiresIn time.Duration
	logger    *zap.Logger
}

// NewOSSPresigner creates a new OSS presigner instance.
func NewOSSPresigner(cfg OSSConfig, logger *zap.Logger) (*OSSPresigner, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("oss endpoint is required")
	}
	if cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		return nil, fmt.Errorf("oss access key id and secret are required")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("oss bucket is required")
	}

	if cfg.PresignTTL <= 0 {
		cfg.PresignTTL = 7 * 24 * time.Hour
	}

	// Create OSS client
	client, err := oss.New(cfg.Endpoint, cfg.AccessKeyID, cfg.AccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("failed to create OSS client: %w", err)
	}

	// Get bucket
	bucket, err := client.Bucket(cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("failed to get OSS bucket: %w", err)
	}

	logger.Info("✅ OSS presigner initialized",
		zap.String("endpoint", cfg.Endpoint),
		zap.String("bucket", cfg.Bucket),
		zap.Duration("ttl", cfg.PresignTTL))

	return &OSSPresigner{
		client:    client,
		bucket:    bucket,
		expiresIn: cfg.PresignTTL,
		logger:    logger,
	}, nil
}

// GeneratePresignedURL returns a presigned download URL for the given key.
func (p *OSSPresigner) GeneratePresignedURL(ctx context.Context, key string) (string, error) {
	if p == nil {
		return "", fmt.Errorf("oss presigner is not configured")
	}

	// Generate presigned URL for GET request
	urlStr, err := p.bucket.SignURL(key, oss.HTTPGet, int64(p.expiresIn.Seconds()))
	if err != nil {
		p.logger.Error("Failed to presign OSS URL",
			zap.Error(err),
			zap.String("bucket", p.bucket.BucketName),
			zap.String("key", key))
		return "", fmt.Errorf("failed to generate presigned url: %w", err)
	}

	return urlStr, nil
}

// ExtractOSSKey extracts OSS key from file path
// Handles both oss://bucket/key and direct key formats
func ExtractOSSKey(filePath string) (string, bool) {
	path := strings.TrimSpace(filePath)
	if path == "" {
		return "", false
	}

	// Handle oss://bucket/key format
	if strings.HasPrefix(path, "oss://") {
		path = strings.TrimPrefix(path, "oss://")
		idx := strings.Index(path, "/")
		if idx <= 0 || idx == len(path)-1 {
			return "", false
		}
		return path[idx+1:], true
	}

	// Handle direct key format (no bucket prefix)
	return path, true
}
