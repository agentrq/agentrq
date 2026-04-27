package storage

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
)

type Service interface {
	Save(id string, dataBase64 string) error
	Load(id string) (string, error)
	LoadRaw(id string) ([]byte, error)
	Delete(id string) error
}

type service struct {
	baseDir string
}

func New(baseDir string) (Service, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}
	return &service{baseDir: baseDir}, nil
}

func (s *service) fullPath(id string) (string, error) {
	if id == "" || id == "." || id == ".." {
		return "", fmt.Errorf("invalid storage id: %s", id)
	}
	// Ensure the id is a simple filename and doesn't contain path separators
	if filepath.Base(id) != id {
		return "", fmt.Errorf("invalid storage id (must be a filename): %s", id)
	}
	return filepath.Join(s.baseDir, id), nil
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
	return os.Remove(path)
}
