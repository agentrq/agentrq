// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package s3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	configmocks "github.com/agentrq/agentrq/backend/internal/service/mocks/config"
	mocks "github.com/agentrq/agentrq/backend/internal/service/mocks/s3"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("reading the upload body failed")
}

func TestS3(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockS3 := mocks.NewMockS3API(ctrl)
	mockConfig := configmocks.NewMockService(ctrl)
	s := &service{client: mockS3, bucket: "test-bucket"}

	t.Run("PutPrivate", func(t *testing.T) {
		mockS3.EXPECT().PutObject(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, in *awss3.PutObjectInput, _ ...func(*awss3.Options)) (*awss3.PutObjectOutput, error) {
			assert.Equal(t, "ns/key", *in.Key)
			// A private object carries no ACL, so the bucket's own policy applies.
			assert.Empty(t, in.ACL)
			return &awss3.PutObjectOutput{ETag: aws.String("etag-456")}, nil
		})
		etag, err := s.PutPrivate(context.Background(), "ns", "key", []byte("data"), "text/plain")
		assert.NoError(t, err)
		assert.Equal(t, "etag-456", etag)

		// nil ETag case
		mockS3.EXPECT().PutObject(gomock.Any(), gomock.Any()).Return(&awss3.PutObjectOutput{
			ETag: nil,
		}, nil)
		etag, err = s.PutPrivate(context.Background(), "ns", "key", []byte("data"), "text/plain")
		assert.NoError(t, err)
		assert.Equal(t, "", etag)

		mockS3.EXPECT().PutObject(gomock.Any(), gomock.Any()).Return(nil, errors.New("S3 unavailable"))
		_, err = s.PutPrivate(context.Background(), "ns", "key", []byte("data"), "text/plain")
		assert.Error(t, err)
	})

	t.Run("Get", func(t *testing.T) {
		mockS3.EXPECT().GetObject(gomock.Any(), gomock.Any()).Return(&awss3.GetObjectOutput{
			Body: io.NopCloser(bytes.NewReader([]byte("test-data"))),
		}, nil)
		data, err := s.Get(context.Background(), "ns", "key")
		assert.NoError(t, err)
		assert.Equal(t, []byte("test-data"), data)

		mockS3.EXPECT().GetObject(gomock.Any(), gomock.Any()).Return(nil, errors.New("S3 unavailable"))
		_, err = s.Get(context.Background(), "ns", "key")
		assert.Error(t, err)

		// A body that fails part way is an error, not a short file.
		mockS3.EXPECT().GetObject(gomock.Any(), gomock.Any()).Return(&awss3.GetObjectOutput{
			Body: io.NopCloser(failingReader{}),
		}, nil)
		_, err = s.Get(context.Background(), "ns", "key")
		assert.Error(t, err)
	})

	t.Run("Delete", func(t *testing.T) {
		mockS3.EXPECT().DeleteObject(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, in *awss3.DeleteObjectInput, _ ...func(*awss3.Options)) (*awss3.DeleteObjectOutput, error) {
			assert.Equal(t, "test-bucket", *in.Bucket)
			assert.Equal(t, "ns/key", *in.Key)
			return &awss3.DeleteObjectOutput{}, nil
		})
		assert.NoError(t, s.Delete(context.Background(), "ns", "key"))

		mockS3.EXPECT().DeleteObject(gomock.Any(), gomock.Any()).Return(nil, errors.New("S3 unavailable"))
		assert.Error(t, s.Delete(context.Background(), "ns", "key"))
	})

	t.Run("New", func(t *testing.T) {
		mockConfig.EXPECT().Populate("s3", gomock.Any()).Return(nil)
		svc, err := New(Params{Config: mockConfig})
		assert.NoError(t, err)
		assert.NotNil(t, svc)

		mockConfig.EXPECT().Populate("s3", gomock.Any()).Return(errors.New("no s3 section in the config"))
		_, err = New(Params{Config: mockConfig})
		assert.Error(t, err)

		// AWS Config error
		mockConfig.EXPECT().Populate("s3", gomock.Any()).Return(nil)
		oldLoader := loadAWSConfig
		loadAWSConfig = func(ctx context.Context, optFns ...func(*awsconfig.LoadOptions) error) (aws.Config, error) {
			return aws.Config{}, errors.New("aws config error")
		}
		defer func() { loadAWSConfig = oldLoader }()

		_, err = New(Params{Config: mockConfig})
		assert.Error(t, err)
	})

	t.Run("PublicURL", func(t *testing.T) {
		mockConfig.EXPECT().Populate("s3", gomock.Any()).DoAndReturn(func(_ string, v any) error {
			*v.(*Config) = Config{Endpoint: "https://s3.example.com/", Bucket: "my bucket"}
			return nil
		})
		svc, err := New(Params{Config: mockConfig})
		assert.NoError(t, err)
		assert.Equal(t, "https://s3.example.com/my%20bucket/attachments/a%3Fb", svc.PublicURL(context.Background(), "attachments", "a?b"))

		mockConfig.EXPECT().Populate("s3", gomock.Any()).DoAndReturn(func(_ string, v any) error {
			*v.(*Config) = Config{Endpoint: "https://s3.example.com", Bucket: "b", PublicURL: " https://cdn.example.com/files/ "}
			return nil
		})
		svc, err = New(Params{Config: mockConfig})
		assert.NoError(t, err)
		assert.Equal(t, "https://cdn.example.com/files/attachments/x/y", svc.PublicURL(context.Background(), "attachments", "x/y"))
	})

	t.Run("SignedURL", func(t *testing.T) {
		mockConfig.EXPECT().Populate("s3", gomock.Any()).DoAndReturn(func(_ string, v any) error {
			*v.(*Config) = Config{Endpoint: "https://s3.example.com", Bucket: "b", Region: "us-east-1", AccessKey: "ak", SecretAccessKey: "sk"}
			return nil
		})
		svc, err := New(Params{Config: mockConfig})
		assert.NoError(t, err)
		link, err := svc.SignedURL(context.Background(), "attachments", "x/y", time.Hour)
		assert.NoError(t, err)
		assert.True(t, strings.HasPrefix(link, "https://s3.example.com/b/attachments/x/y?"), link)
		assert.Contains(t, link, "X-Amz-Expires=3600")
		assert.Contains(t, link, "X-Amz-Signature=")

		// A public base of its own needs no signature.
		mockConfig.EXPECT().Populate("s3", gomock.Any()).DoAndReturn(func(_ string, v any) error {
			*v.(*Config) = Config{Endpoint: "https://s3.example.com", Bucket: "b", Region: "us-east-1", PublicURL: "https://cdn.example.com"}
			return nil
		})
		svc, err = New(Params{Config: mockConfig})
		assert.NoError(t, err)
		link, err = svc.SignedURL(context.Background(), "attachments", "x/y", time.Hour)
		assert.NoError(t, err)
		assert.Empty(t, link)

		presign := mocks.NewMockPresigner(ctrl)
		presign.EXPECT().PresignGetObject(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("no credentials"))
		_, err = (&service{bucket: "b", presign: presign}).SignedURL(context.Background(), "attachments", "x/y", time.Hour)
		assert.Error(t, err)
	})

	t.Run("NewCredentialsProvider", func(t *testing.T) {
		fn := newCredentialsProvider("ak", "sk")
		creds, err := fn(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, "ak", creds.AccessKeyID)
		assert.Equal(t, "sk", creds.SecretAccessKey)
	})
}
