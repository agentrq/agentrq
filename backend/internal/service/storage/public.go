// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package storage

import (
	"net/url"
	"strings"
	"time"
)

// Publisher is a Service whose blobs anyone can also read, at a public URL.
type Publisher interface {
	// SavePublic saves a blob served with contentType and returns its URL.
	SavePublic(id, dataBase64, contentType string) (string, error)
	// PublicURL is where the blob kept under id can be read.
	PublicURL(id string) string
}

// PublicURL is where anyone can read the blob svc keeps under id, or "" when
// svc keeps it private.
func PublicURL(svc Service, id string) string {
	p, ok := svc.(Publisher)
	if !ok {
		return ""
	}
	return p.PublicURL(id)
}

// Signer is a Publisher whose public link is only a guess, such as a bucket
// with no public base configured: it signs a link that reads a blob for ttl,
// or returns "" when its public link is good as it is.
type Signer interface {
	SignedURL(id string, ttl time.Duration) (string, error)
}

// ReadURL is a link to read the blob svc keeps under id: signed for ttl when
// svc signs, publicURL otherwise, and publicURL too when signing fails, since
// a guess still beats no link.
func ReadURL(svc Service, id, publicURL string, ttl time.Duration) string {
	s, ok := svc.(Signer)
	if !ok {
		return publicURL
	}
	if signed, err := s.SignedURL(id, ttl); err == nil && signed != "" {
		return signed
	}
	return publicURL
}

// WithFallback is primary, except that a blob primary does not have is read
// or deleted from fallback: where it was kept before primary took over. New
// blobs only ever go to primary.
func WithFallback(primary, fallback Service) Service {
	return &fallbackService{primary: primary, fallback: fallback}
}

type fallbackService struct{ primary, fallback Service }

func (s *fallbackService) Save(id, dataBase64 string) error { return s.primary.Save(id, dataBase64) }

// SavePublic keeps the primary's links, so wrapping never hides them.
func (s *fallbackService) SavePublic(id, dataBase64, contentType string) (string, error) {
	if p, ok := s.primary.(Publisher); ok {
		return p.SavePublic(id, dataBase64, contentType)
	}
	return "", s.primary.Save(id, dataBase64)
}

func (s *fallbackService) PublicURL(id string) string { return PublicURL(s.primary, id) }

func (s *fallbackService) SignedURL(id string, ttl time.Duration) (string, error) {
	if p, ok := s.primary.(Signer); ok {
		return p.SignedURL(id, ttl)
	}
	return "", nil
}

// SaveBlob saves a blob served with contentType and returns its public URL,
// or "" when svc keeps blobs to be read through the server only.
func SaveBlob(svc Service, id, dataBase64, contentType string) (string, error) {
	if p, ok := svc.(Publisher); ok {
		return p.SavePublic(id, dataBase64, contentType)
	}
	return "", svc.Save(id, dataBase64)
}

// inlineTypes are the content types a public file may be shown as in a
// browser. None of them can run script on the origin serving it.
var inlineTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/bmp": true,
	"application/pdf": true, "text/plain": true, "text/plain; charset=utf-8": true,
}

// SafeContentType is contentType when a browser may show it, and
// application/octet-stream otherwise, so that HTML, SVG and the like
// uploaded as an attachment download rather than render.
func SafeContentType(contentType string) string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if inlineTypes[ct] || strings.HasPrefix(ct, "video/") || strings.HasPrefix(ct, "audio/") {
		return ct
	}
	return octetStream
}

const octetStream = "application/octet-stream"

// NewNestedPublic is NewNested for blobs anyone can read, at urlBase followed
// by the blob's key. Something must serve them there; see the public file
// routes of the API handler.
func NewNestedPublic(baseDir, urlBase string) (Service, error) {
	s, err := NewNested(baseDir)
	if err != nil {
		return nil, err
	}
	return &localPublic{Service: s, urlBase: strings.TrimRight(urlBase, "/")}, nil
}

type localPublic struct {
	Service
	urlBase string
}

func (s *localPublic) SavePublic(id, dataBase64, _ string) (string, error) {
	if err := s.Save(id, dataBase64); err != nil {
		return "", err
	}
	return PublicURL(s, id), nil
}

func (s *localPublic) PublicURL(id string) string { return s.urlBase + "/" + escapeKey(id) }

// escapeKey escapes each segment of a slash-separated key for a URL path.
func escapeKey(key string) string {
	segs := strings.Split(key, "/")
	for i, seg := range segs {
		segs[i] = url.PathEscape(seg)
	}
	return strings.Join(segs, "/")
}
