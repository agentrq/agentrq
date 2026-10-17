// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package storage

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mustafaturan/monoflake"
)

type Service interface {
	Save(id string, dataBase64 string) error
	Load(id string) (string, error)
	LoadRaw(id string) ([]byte, error)
	Delete(id string) error
}

func (s *fallbackService) Load(id string) (string, error) {
	if data, err := s.primary.Load(id); err == nil {
		return data, nil
	}
	return s.fallback.Load(id)
}

func (s *fallbackService) LoadRaw(id string) ([]byte, error) {
	if data, err := s.primary.LoadRaw(id); err == nil {
		return data, nil
	}
	return s.fallback.LoadRaw(id)
}

func (s *fallbackService) Delete(id string) error {
	if s.primary.Delete(id) == nil {
		return nil
	}
	return s.fallback.Delete(id)
}

// AttachmentKey is where an attachment is kept, w-<workspace>/<task>/<id>,
// every id in base62. svc must accept nested keys (NewNested or S3).
func AttachmentKey(workspaceID, taskID int64, id string) string {
	return AttachmentPrefix(workspaceID) + monoflake.ID(taskID).String() + "/" + id
}

// AttachmentPrefix starts the key of every attachment kept under a workspace.
func AttachmentPrefix(workspaceID int64) string {
	return "w-" + monoflake.ID(workspaceID).String() + "/"
}

// SaveAttachment saves an attachment of a task and returns its public URL, or
// "" when svc keeps blobs to be read through the server only.
func SaveAttachment(svc Service, workspaceID, taskID int64, id, dataBase64, contentType string) (string, error) {
	return SaveBlob(svc, AttachmentKey(workspaceID, taskID, id), dataBase64, contentType)
}

// LoadAttachment reads an attachment of a task. One saved before keys named
// the workspace and task is looked for under its bare id.
func LoadAttachment(svc Service, workspaceID, taskID int64, id string) ([]byte, error) {
	data, err := svc.LoadRaw(AttachmentKey(workspaceID, taskID, id))
	if err != nil {
		return svc.LoadRaw(id)
	}
	return data, nil
}

// DeleteAttachment removes an attachment of a task, wherever LoadAttachment
// would find it.
func DeleteAttachment(svc Service, workspaceID, taskID int64, id string) {
	if svc.Delete(AttachmentKey(workspaceID, taskID, id)) != nil {
		_ = svc.Delete(id)
	}
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
	data, err := s.LoadRaw(id)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// LoadRaw reads through an os.Root on the base directory, so that on top of
// the id checks, no path and no symlink can reach a file outside it: some of
// these files are served to anyone holding a link.
func (s *service) LoadRaw(id string) ([]byte, error) {
	if _, err := s.fullPath(id); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(s.baseDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.ReadFile(filepath.FromSlash(id))
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
