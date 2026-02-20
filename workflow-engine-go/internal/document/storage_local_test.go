package document

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLocalStorageUploadDownloadDelete(t *testing.T) {
	tests := []struct {
		name    string
		bucket  string
		key     string
		content string
		cancel  bool
		wantErr bool
	}{
		{
			name:    "happy path upload and download",
			bucket:  "loan-docs",
			key:     "case-1/doc-1.pdf",
			content: "sample-pdf-bytes",
		},
		{
			name:    "edge nested key path",
			bucket:  "loan-docs",
			key:     "nested/case-2/doc-2.png",
			content: "png-bytes",
		},
		{
			name:    "failure canceled context",
			bucket:  "loan-docs",
			key:     "case-3/doc-3.jpg",
			content: "jpg-bytes",
			cancel:  true,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			basePath := t.TempDir()
			storage := NewLocalStorage(basePath, nil)

			ctx := context.Background()
			if tt.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(context.Background())
				cancel()
			}

			path, url, err := storage.Upload(ctx, tt.bucket, tt.key, bytes.NewBufferString(tt.content), map[string]string{
				"content-type": "application/octet-stream",
			})
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Contains(t, path, tt.bucket+"/")
			assert.Contains(t, url, "file://")

			reader, err := storage.Download(context.Background(), path)
			assert.NoError(t, err)
			defer reader.Close()
			body, err := io.ReadAll(reader)
			assert.NoError(t, err)
			assert.Equal(t, tt.content, string(body))

			presigned, err := storage.GeneratePresignedURL(context.Background(), path, 5*time.Minute)
			assert.NoError(t, err)
			assert.Contains(t, presigned, "file://")

			reader.Close() // Explicitly close to release lock before delete
			assert.NoError(t, storage.Delete(context.Background(), path))
			_, err = os.Stat(filepath.Join(basePath, filepath.FromSlash(path)))
			assert.Error(t, err)
		})
	}
}
