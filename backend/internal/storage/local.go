package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type LocalStorage struct {
	root    string
	baseURL string
}

func NewLocalStorage(root, baseURL string) *LocalStorage {
	return &LocalStorage{root: root, baseURL: strings.TrimRight(baseURL, "/")}
}

func (s *LocalStorage) Save(ctx context.Context, object Object) (StoredObject, error) {
	select {
	case <-ctx.Done():
		return StoredObject{}, ctx.Err()
	default:
	}

	cleanKey := filepath.Clean(object.Key)
	if strings.HasPrefix(cleanKey, "..") || filepath.IsAbs(cleanKey) {
		return StoredObject{}, fmt.Errorf("invalid storage key")
	}

	path := filepath.Join(s.root, cleanKey)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return StoredObject{}, err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return StoredObject{}, err
	}
	defer file.Close()

	written, err := io.Copy(file, object.Reader)
	if err != nil {
		return StoredObject{}, err
	}

	return StoredObject{Key: cleanKey, SizeBytes: written, ContentType: object.ContentType}, nil
}

func (s *LocalStorage) Delete(ctx context.Context, key string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	cleanKey := filepath.Clean(key)
	if strings.HasPrefix(cleanKey, "..") || filepath.IsAbs(cleanKey) {
		return fmt.Errorf("invalid storage key")
	}
	return os.Remove(filepath.Join(s.root, cleanKey))
}

func (s *LocalStorage) GetURL(ctx context.Context, key string, opts URLOptions) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	cleanKey := strings.TrimLeft(filepath.ToSlash(filepath.Clean(key)), "/")
	if strings.HasPrefix(cleanKey, "../") {
		return "", fmt.Errorf("invalid storage key")
	}
	return s.baseURL + "/api/v1/files/" + cleanKey, nil
}
