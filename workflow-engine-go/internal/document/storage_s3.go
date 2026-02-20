package document

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

type s3API interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type s3PresignAPI interface {
	PresignGetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

// S3Storage stores document payloads in AWS S3.
type S3Storage struct {
	client    *s3.Client
	api       s3API
	presigner s3PresignAPI
	logger    *slog.Logger
}

// NewS3Storage constructs an S3-backed storage adapter.
func NewS3Storage(client *s3.Client, logger *slog.Logger) *S3Storage {
	if logger == nil {
		logger = slog.Default()
	}
	var api s3API
	var presigner s3PresignAPI
	if client != nil {
		api = client
		presigner = s3.NewPresignClient(client)
	}
	return &S3Storage{
		client:    client,
		api:       api,
		presigner: presigner,
		logger:    logger,
	}
}

// NewS3StorageForTest allows mocking SDK calls in unit tests.
func NewS3StorageForTest(api s3API, presigner s3PresignAPI, logger *slog.Logger) *S3Storage {
	if logger == nil {
		logger = slog.Default()
	}
	return &S3Storage{
		api:       api,
		presigner: presigner,
		logger:    logger,
	}
}

// Upload stores object bytes in S3 and validates checksum locally before PUT.
func (s *S3Storage) Upload(
	ctx context.Context,
	bucket string,
	key string,
	content io.Reader,
	metadata map[string]string,
) (string, string, error) {
	if s == nil || s.api == nil {
		return "", "", &StorageError{Operation: "upload", Path: key, Transient: false, Err: fmt.Errorf("s3 client not configured")}
	}
	bucket = strings.TrimSpace(bucket)
	key = strings.TrimSpace(strings.TrimPrefix(key, "/"))
	if bucket == "" || key == "" {
		return "", "", &StorageError{Operation: "upload", Path: key, Transient: false, Err: fmt.Errorf("bucket and key are required")}
	}

	body, err := io.ReadAll(content)
	if err != nil {
		return "", "", &StorageError{Operation: "upload", Path: key, Transient: true, Err: err}
	}
	sum := sha256.Sum256(body)
	checksumBase64 := base64.StdEncoding.EncodeToString(sum[:])

	contentLen := int64(len(body))
	_, err = s.api.PutObject(ctx, &s3.PutObjectInput{
		Bucket:         &bucket,
		Key:            &key,
		Body:           bytes.NewReader(body),
		ContentLength:  &contentLen,
		ChecksumSHA256: &checksumBase64,
		Metadata:       metadata,
	})
	if err != nil {
		return "", "", &StorageError{
			Operation: "upload",
			Path:      fmt.Sprintf("%s/%s", bucket, key),
			Transient: isRetryableS3Error(err),
			Err:       err,
		}
	}

	path := fmt.Sprintf("%s/%s", bucket, key)
	url := fmt.Sprintf("s3://%s/%s", bucket, key)
	s.logger.Info("s3 upload complete", "path", path, "bytes", len(body))
	return path, url, nil
}

// Download retrieves an object from S3.
func (s *S3Storage) Download(ctx context.Context, path string) (io.ReadCloser, error) {
	if s == nil || s.api == nil {
		return nil, &StorageError{Operation: "download", Path: path, Transient: false, Err: fmt.Errorf("s3 client not configured")}
	}
	bucket, key, err := parseStoragePath(path)
	if err != nil {
		return nil, &StorageError{Operation: "download", Path: path, Transient: false, Err: err}
	}
	output, err := s.api.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, &StorageError{Operation: "download", Path: path, Transient: isRetryableS3Error(err), Err: err}
	}
	return output.Body, nil
}

// Delete removes an object from S3.
func (s *S3Storage) Delete(ctx context.Context, path string) error {
	if s == nil || s.api == nil {
		return &StorageError{Operation: "delete", Path: path, Transient: false, Err: fmt.Errorf("s3 client not configured")}
	}
	bucket, key, err := parseStoragePath(path)
	if err != nil {
		return &StorageError{Operation: "delete", Path: path, Transient: false, Err: err}
	}
	_, err = s.api.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &bucket,
		Key:    &key,
	})
	if err != nil {
		return &StorageError{Operation: "delete", Path: path, Transient: isRetryableS3Error(err), Err: err}
	}
	s.logger.Info("s3 object deleted", "path", path)
	return nil
}

// GeneratePresignedURL creates a pre-signed GET URL for a storage path.
func (s *S3Storage) GeneratePresignedURL(ctx context.Context, path string, expiration time.Duration) (string, error) {
	if s == nil || s.presigner == nil {
		return "", &StorageError{Operation: "presign", Path: path, Transient: false, Err: fmt.Errorf("s3 presigner not configured")}
	}
	if expiration <= 0 {
		expiration = 15 * time.Minute
	}
	bucket, key, err := parseStoragePath(path)
	if err != nil {
		return "", &StorageError{Operation: "presign", Path: path, Transient: false, Err: err}
	}
	presigned, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	}, func(options *s3.PresignOptions) {
		options.Expires = expiration
	})
	if err != nil {
		return "", &StorageError{Operation: "presign", Path: path, Transient: isRetryableS3Error(err), Err: err}
	}
	return presigned.URL, nil
}

func isRetryableS3Error(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	fault := apiErr.ErrorFault()
	return fault == smithy.FaultServer || fault == smithy.FaultUnknown
}
