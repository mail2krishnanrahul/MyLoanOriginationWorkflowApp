package document

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DocumentStorage abstracts object storage (S3, GCS, local).
type DocumentStorage interface {
	// Upload stores a file and returns the storage path and URL.
	Upload(
		ctx context.Context,
		bucket string,
		key string,
		content io.Reader,
		metadata map[string]string,
	) (path string, url string, err error)

	// Download retrieves a file by path.
	Download(
		ctx context.Context,
		path string,
	) (io.ReadCloser, error)

	// Delete removes a file by path.
	Delete(
		ctx context.Context,
		path string,
	) error

	// GeneratePresignedURL creates a time-limited download URL.
	GeneratePresignedURL(
		ctx context.Context,
		path string,
		expiration time.Duration,
	) (string, error)
}

// StorageError captures transient/permanent storage failures.
type StorageError struct {
	Operation string
	Path      string
	Transient bool
	Err       error
}

func (e *StorageError) Error() string {
	if e == nil {
		return "storage error"
	}
	return fmt.Sprintf("%s(%s): %v", e.Operation, e.Path, e.Err)
}

func (e *StorageError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// LocalStorage stores files on local filesystem (dev/test).
type LocalStorage struct {
	basePath string
	logger   *slog.Logger
}

// NewLocalStorage constructs a local filesystem storage adapter.
func NewLocalStorage(basePath string, logger *slog.Logger) *LocalStorage {
	basePath = strings.TrimSpace(basePath)
	if basePath == "" {
		basePath = "."
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &LocalStorage{
		basePath: basePath,
		logger:   logger,
	}
}

// Upload writes file content into basePath/bucket/key and verifies checksum.
func (s *LocalStorage) Upload(
	ctx context.Context,
	bucket string,
	key string,
	content io.Reader,
	metadata map[string]string,
) (string, string, error) {
	_ = metadata
	select {
	case <-ctx.Done():
		return "", "", &StorageError{Operation: "upload", Path: key, Transient: true, Err: ctx.Err()}
	default:
	}

	bucket = strings.TrimSpace(bucket)
	key = strings.TrimSpace(strings.TrimPrefix(key, "/"))
	if bucket == "" || key == "" {
		return "", "", &StorageError{Operation: "upload", Path: key, Transient: false, Err: fmt.Errorf("bucket and key are required")}
	}

	fullPath := filepath.Join(s.basePath, bucket, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		return "", "", &StorageError{Operation: "upload", Path: fullPath, Transient: false, Err: err}
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(fullPath), ".upload-*")
	if err != nil {
		return "", "", &StorageError{Operation: "upload", Path: fullPath, Transient: false, Err: err}
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()    // cleaning up temp file; error ignored as it's secondary
		_ = os.Remove(tmpPath) // cleaning up temp file; error ignored as it's secondary
	}()

	writeHash := sha256.New()
	written, err := io.Copy(io.MultiWriter(tmpFile, writeHash), content)
	if err != nil {
		return "", "", &StorageError{Operation: "upload", Path: fullPath, Transient: true, Err: err}
	}
	if err := tmpFile.Sync(); err != nil {
		return "", "", &StorageError{Operation: "upload", Path: fullPath, Transient: true, Err: err}
	}
	if err := tmpFile.Close(); err != nil {
		return "", "", &StorageError{Operation: "upload", Path: fullPath, Transient: true, Err: err}
	}

	// Verify checksum by re-reading on-disk bytes.
	reopen, err := os.Open(tmpPath)
	if err != nil {
		return "", "", &StorageError{Operation: "upload", Path: fullPath, Transient: false, Err: err}
	}
	readHash := sha256.New()
	if _, err := io.Copy(readHash, reopen); err != nil {
		_ = reopen.Close()
		return "", "", &StorageError{Operation: "upload", Path: fullPath, Transient: true, Err: err}
	}
	if err := reopen.Close(); err != nil {
		return "", "", &StorageError{Operation: "upload", Path: fullPath, Transient: true, Err: err}
	}

	writeSum := hex.EncodeToString(writeHash.Sum(nil))
	readSum := hex.EncodeToString(readHash.Sum(nil))
	if writeSum != readSum {
		return "", "", &StorageError{
			Operation: "upload",
			Path:      fullPath,
			Transient: false,
			Err:       fmt.Errorf("checksum mismatch after write"),
		}
	}

	if err := os.Rename(tmpPath, fullPath); err != nil {
		return "", "", &StorageError{Operation: "upload", Path: fullPath, Transient: false, Err: err}
	}

	storagePath := filepath.ToSlash(filepath.Join(bucket, key))
	fileURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(fullPath)}).String()
	s.logger.Info("local storage upload complete", "path", storagePath, "bytes", written)
	return storagePath, fileURL, nil
}

// Download opens a local file by storage path.
func (s *LocalStorage) Download(ctx context.Context, path string) (io.ReadCloser, error) {
	select {
	case <-ctx.Done():
		return nil, &StorageError{Operation: "download", Path: path, Transient: true, Err: ctx.Err()}
	default:
	}
	fullPath := filepath.Join(s.basePath, filepath.FromSlash(strings.TrimSpace(path)))
	handle, err := os.Open(fullPath)
	if err != nil {
		transient := errors.Is(err, os.ErrDeadlineExceeded)
		return nil, &StorageError{Operation: "download", Path: fullPath, Transient: transient, Err: err}
	}
	return handle, nil
}

// Delete removes a local file by storage path.
func (s *LocalStorage) Delete(ctx context.Context, path string) error {
	select {
	case <-ctx.Done():
		return &StorageError{Operation: "delete", Path: path, Transient: true, Err: ctx.Err()}
	default:
	}
	fullPath := filepath.Join(s.basePath, filepath.FromSlash(strings.TrimSpace(path)))
	if err := os.Remove(fullPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return &StorageError{Operation: "delete", Path: fullPath, Transient: false, Err: err}
	}
	s.logger.Info("local storage object deleted", "path", path)
	return nil
}

// GeneratePresignedURL returns a local file URL.
func (s *LocalStorage) GeneratePresignedURL(ctx context.Context, path string, expiration time.Duration) (string, error) {
	_ = expiration
	select {
	case <-ctx.Done():
		return "", &StorageError{Operation: "presign", Path: path, Transient: true, Err: ctx.Err()}
	default:
	}
	fullPath := filepath.Join(s.basePath, filepath.FromSlash(strings.TrimSpace(path)))
	fileURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(fullPath)}).String()
	return fileURL, nil
}

func parseStoragePath(path string) (string, string, error) {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return "", "", fmt.Errorf("parseStoragePath: path is empty")
	}
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("parseStoragePath: invalid path %q", path)
	}
	return parts[0], parts[1], nil
}
