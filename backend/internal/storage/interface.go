package storage

import (
	"context"
	"io"
	"time"
)

type Object struct {
	Key         string
	Reader      io.Reader
	SizeBytes   int64
	ContentType string
}

type StoredObject struct {
	Key         string
	SizeBytes   int64
	ContentType string
}

type URLOptions struct {
	ExpiresIn    time.Duration
	DownloadName string
	ContentType  string
}

type Storage interface {
	Save(ctx context.Context, object Object) (StoredObject, error)
	Delete(ctx context.Context, key string) error
	GetURL(ctx context.Context, key string, opts URLOptions) (string, error)
}
