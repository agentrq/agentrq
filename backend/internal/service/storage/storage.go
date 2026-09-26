// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package storage

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Service interface {
	Save(id string, dataBase64 string) error
	Load(id string) (string, error)
	LoadRaw(id string) ([]byte, error)
	Delete(id string) error
}

// Publisher is a Service whose blobs anyone can also read, at a public URL.
type Publisher interface {
	// SavePublic saves a blob served with contentType and returns its URL.
	SavePublic(id, dataBase64, contentType string) (string, error)
}

// SaveAttachment saves an attachment and returns its public URL, or "" when
// svc keeps blobs to be read through the server only.
func SaveAttachment(svc Service, id, dataBase64, contentType string) (string, error) {
	if p, ok := svc.(Publisher); ok {
		return p.SavePublic(id, dataBase64, contentType)
	}
	return "", svc.Save(id, dataBase64)
}

type service struct {
	baseDir string
	nested  bool
}

func New(baseDir string) (Service, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}
	return &service{baseDir: baseDir}, nil
}

// NewNested is New for ids that are slash-separated paths, such as a skill
// file's w-<workspace>/skill-<skill>/<file>. Each segment is still checked
// like a flat id, so an id cannot traverse.
func NewNested(baseDir string) (Service, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}
	return &service{baseDir: baseDir, nested: true}, nil
}

func (s *service) Save(id string, dataBase64 string) error {
	data, err := base64.StdEncoding.DecodeString(dataBase64)
	if err != nil {
		return fmt.Errorf("decode base64: %w", err)
	}

	path, err := s.fullPath(id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (s *service) Load(id string) (string, error) {
	path, err := s.fullPath(id)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func (s *service) LoadRaw(id string) ([]byte, error) {
	path, err := s.fullPath(id)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func (s *service) Delete(id string) error {
	path, err := s.fullPath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	// Drop the directories the id left empty; os.Remove refuses a full one.
	for dir := filepath.Dir(path); dir != filepath.Clean(s.baseDir); dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil {
			break
		}
	}
	return nil
}

// fullPath validates the ID and returns the absolute path within the base directory.
// It prevents path traversal by ensuring the ID is a simple filename.
func (s *service) fullPath(id string) (string, error) {
	check := validID
	if s.nested {
		check = validKey
	}
	if err := check(id); err != nil {
		return "", err
	}
	return filepath.Join(s.baseDir, filepath.FromSlash(id)), nil
}

// validID accepts a flat name only, on every store, so an id cannot traverse.
func validID(id string) error {
	if id == "" || id == "." || id == ".." {
		return fmt.Errorf("invalid storage id")
	}
	if filepath.Base(id) != id {
		return fmt.Errorf("invalid storage id: path traversal detected or invalid format")
	}
	return nil
}

// validKey accepts a slash-separated path of ids that validID accepts.
func validKey(key string) error {
	for _, seg := range strings.Split(key, "/") {
		if err := validID(seg); err != nil {
			return err
		}
	}
	return nil
}
