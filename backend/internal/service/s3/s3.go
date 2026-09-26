// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package s3

import (
	"bytes"
	"context"
	"net/url"
	"strings"

	"github.com/agentrq/agentrq/backend/internal/service/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	zlog "github.com/rs/zerolog/log"
)

type (
	Params struct {
		Config config.Service
	}
	Service interface {
		PutPrivate(ctx context.Context, namespace, key string, data []byte, contentType string) (string, error)
		Get(ctx context.Context, namespace, key string) ([]byte, error)
		Delete(ctx context.Context, namespace, key string) error
		// PublicURL is where anyone can read an object, if the bucket lets them.
		PublicURL(ctx context.Context, namespace, key string) string
	}
	S3API interface {
		PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
		GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
		DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	}
	service struct {
		client    S3API
		bucket    string
		publicURL string
	}
	s3Config struct {
		Enabled         bool   `yaml:"enabled"`
		Endpoint        string `yaml:"endpoint"`
		AccessKey       string `yaml:"accessKey"`
		SecretAccessKey string `yaml:"secretAccessKey"`
		Region          string `yaml:"region"`
		Bucket          string `yaml:"bucket"`
		// PublicURL is the base public links are made from, such as a CDN.
		// Unset, it is the endpoint and bucket, path-style.
		PublicURL string `yaml:"publicUrl"`
	}
)

const cfgKey = "s3"

var loadAWSConfig = awsconfig.LoadDefaultConfig

// New init a new s3 service
func New(p Params) (Service, error) {
	var cfg s3Config
	err := p.Config.Populate(cfgKey, &cfg)
	if err != nil {
		return nil, err
	}
	fn := newCredentialsProvider(cfg.AccessKey, cfg.SecretAccessKey)
	ctx := context.Background()
	acfg, err := loadAWSConfig(
		ctx,
		awsconfig.WithCredentialsProvider(fn),
		awsconfig.WithRegion(cfg.Region),
	)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(acfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.Endpoint)
		o.UsePathStyle = true
		// The SDK's default flexible checksums are refused by some S3-compatible stores.
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})
	return &service{
		client:    client,
		bucket:    cfg.Bucket,
		publicURL: publicBase(cfg),
	}, nil
}

// PutPrivate stores the data in s3
func (s *service) PutPrivate(ctx context.Context, namespace, key string, data []byte, contentType string) (string, error) {
	actualKey := namespace + "/" + key
	zlog.Info().Str("bucket", s.bucket).Str("key", actualKey).Int("size", len(data)).Msg("S3 PutPrivate initiated")
	o, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(actualKey),
		Body:        bytes.NewReader(data),
		ContentType: &contentType,
	})
	if err != nil {
		zlog.Error().Err(err).Str("bucket", s.bucket).Str("key", actualKey).Msg("S3 PutPrivate failed")
		return "", err
	}
	etag := ""
	if o.ETag != nil {
		etag = *o.ETag
	}
	zlog.Info().Str("etag", etag).Msg("S3 PutPrivate succeeded")
	return etag, nil
}

// Get retrieves the data from s3
func (s *service) Get(ctx context.Context, namespace, key string) ([]byte, error) {
	actualKey := namespace + "/" + key
	o, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(actualKey),
	})
	if err != nil {
		return nil, err
	}
	defer o.Body.Close()
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(o.Body); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Delete removes the data from s3
func (s *service) Delete(ctx context.Context, namespace, key string) error {
	actualKey := namespace + "/" + key
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(actualKey),
	})
	if err != nil {
		zlog.Error().Err(err).Str("bucket", s.bucket).Str("key", actualKey).Msg("S3 Delete failed")
	}
	return err
}

// PublicURL is the object's address under the public base, each path
// segment escaped.
func (s *service) PublicURL(_ context.Context, namespace, key string) string {
	segs := strings.Split(namespace+"/"+key, "/")
	for i, seg := range segs {
		segs[i] = url.PathEscape(seg)
	}
	return s.publicURL + "/" + strings.Join(segs, "/")
}

// publicBase is publicUrl, or the endpoint and bucket, path-style, without a
// trailing slash.
func publicBase(cfg s3Config) string {
	if base := strings.TrimSpace(cfg.PublicURL); base != "" {
		return strings.TrimRight(base, "/")
	}
	return strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/") + "/" + url.PathEscape(cfg.Bucket)
}

func newCredentialsProvider(accessKey, secretKey string) aws.CredentialsProviderFunc {
	return func(ctx context.Context) (aws.Credentials, error) {
		return aws.Credentials{
			AccessKeyID:     accessKey,
			SecretAccessKey: secretKey,
		}, nil
	}
}
