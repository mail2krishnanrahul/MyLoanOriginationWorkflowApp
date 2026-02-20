package document

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
)

type mockS3API struct {
	putFn    func(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	getFn    func(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	deleteFn func(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

func (m mockS3API) PutObject(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return m.putFn(ctx, input, opts...)
}

func (m mockS3API) GetObject(ctx context.Context, input *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return m.getFn(ctx, input, opts...)
}

func (m mockS3API) DeleteObject(ctx context.Context, input *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	return m.deleteFn(ctx, input, opts...)
}

type mockS3Presigner struct {
	presignFn func(context.Context, *s3.GetObjectInput, ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

func (m mockS3Presigner) PresignGetObject(ctx context.Context, input *s3.GetObjectInput, opts ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	return m.presignFn(ctx, input, opts...)
}

type mockAPIError struct {
	msg   string
	fault smithy.ErrorFault
}

func (e mockAPIError) Error() string {
	return e.msg
}
func (e mockAPIError) ErrorCode() string {
	return "InternalError"
}
func (e mockAPIError) ErrorMessage() string {
	return e.msg
}
func (e mockAPIError) ErrorFault() smithy.ErrorFault {
	return e.fault
}

func TestS3Storage(t *testing.T) {
	tests := []struct {
		name    string
		api     mockS3API
		presign mockS3Presigner
		run     func(t *testing.T, storage *S3Storage)
	}{
		{
			name: "happy path upload download delete presign",
			api: mockS3API{
				putFn: func(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
					if input.Bucket == nil || input.Key == nil {
						return nil, fmt.Errorf("missing bucket or key")
					}
					return &s3.PutObjectOutput{}, nil
				},
				getFn: func(ctx context.Context, input *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
					return &s3.GetObjectOutput{
						Body: io.NopCloser(bytes.NewBufferString("hello-world")),
					}, nil
				},
				deleteFn: func(ctx context.Context, input *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
					return &s3.DeleteObjectOutput{}, nil
				},
			},
			presign: mockS3Presigner{
				presignFn: func(ctx context.Context, input *s3.GetObjectInput, opts ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
					return &v4.PresignedHTTPRequest{URL: "https://signed.example.com/object"}, nil
				},
			},
			run: func(t *testing.T, storage *S3Storage) {
				path, url, err := storage.Upload(context.Background(), "bucket-a", "case-1/doc.pdf", bytes.NewBufferString("payload"), map[string]string{"x": "y"})
				assert.NoError(t, err)
				assert.Equal(t, "bucket-a/case-1/doc.pdf", path)
				assert.Equal(t, "s3://bucket-a/case-1/doc.pdf", url)

				reader, err := storage.Download(context.Background(), path)
				assert.NoError(t, err)
				defer reader.Close()
				body, err := io.ReadAll(reader)
				assert.NoError(t, err)
				assert.Equal(t, "hello-world", string(body))

				assert.NoError(t, storage.Delete(context.Background(), path))

				presigned, err := storage.GeneratePresignedURL(context.Background(), path, 2*time.Minute)
				assert.NoError(t, err)
				assert.Equal(t, "https://signed.example.com/object", presigned)
			},
		},
		{
			name: "failure upload transient error",
			api: mockS3API{
				putFn: func(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
					return nil, mockAPIError{msg: "timeout", fault: smithy.FaultServer}
				},
				getFn: func(ctx context.Context, input *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
					return nil, fmt.Errorf("not expected")
				},
				deleteFn: func(ctx context.Context, input *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
					return nil, fmt.Errorf("not expected")
				},
			},
			presign: mockS3Presigner{
				presignFn: func(ctx context.Context, input *s3.GetObjectInput, opts ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
					return nil, fmt.Errorf("not expected")
				},
			},
			run: func(t *testing.T, storage *S3Storage) {
				_, _, err := storage.Upload(context.Background(), "bucket-a", "case-1/doc.pdf", bytes.NewBufferString("payload"), nil)
				assert.Error(t, err)
				var storageErr *StorageError
				assert.ErrorAs(t, err, &storageErr)
				assert.True(t, storageErr.Transient)
			},
		},
		{
			name: "failure invalid storage path for delete",
			api: mockS3API{
				putFn: func(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
					return &s3.PutObjectOutput{}, nil
				},
				getFn: func(ctx context.Context, input *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
					return &s3.GetObjectOutput{}, nil
				},
				deleteFn: func(ctx context.Context, input *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
					return &s3.DeleteObjectOutput{}, nil
				},
			},
			presign: mockS3Presigner{
				presignFn: func(ctx context.Context, input *s3.GetObjectInput, opts ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
					return &v4.PresignedHTTPRequest{URL: "https://signed.example.com/object"}, nil
				},
			},
			run: func(t *testing.T, storage *S3Storage) {
				err := storage.Delete(context.Background(), "invalid-path-without-bucket-separator")
				assert.Error(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewS3StorageForTest(tt.api, tt.presign, nil)
			tt.run(t, storage)
		})
	}
}
