package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"echorift/backend/internal/config"
	"echorift/backend/internal/database"

	"github.com/jackc/pgx/v5"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	db, err := database.Connect(ctx, cfg.PostgresDSN)
	if err != nil {
		slog.Error("database connect failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if _, err := db.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		checksum TEXT NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		slog.Error("schema_migrations create failed", "error", err)
		os.Exit(1)
	}

	files, err := filepath.Glob(filepath.Join("migrations", "*.sql"))
	if err != nil {
		slog.Error("migration scan failed", "error", err)
		os.Exit(1)
	}
	sort.Strings(files)

	for _, path := range files {
		version := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		content, err := os.ReadFile(path)
		if err != nil {
			slog.Error("migration read failed", "path", path, "error", err)
			os.Exit(1)
		}
		checksumBytes := sha256.Sum256(content)
		checksum := hex.EncodeToString(checksumBytes[:])

		var existingChecksum string
		err = db.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE version = $1", version).Scan(&existingChecksum)
		if err == nil {
			if existingChecksum != checksum {
				slog.Error("migration checksum mismatch", "version", version)
				os.Exit(1)
			}
			slog.Info("migration already applied", "version", version)
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("migration lookup failed", "version", version, "error", err)
			os.Exit(1)
		}

		tx, err := db.Begin(ctx)
		if err != nil {
			slog.Error("migration tx begin failed", "version", version, "error", err)
			os.Exit(1)
		}

		if _, err := tx.Exec(ctx, string(content)); err != nil {
			_ = tx.Rollback(ctx)
			slog.Error("migration exec failed", "version", version, "error", err)
			os.Exit(1)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, checksum) VALUES ($1, $2)", version, checksum); err != nil {
			_ = tx.Rollback(ctx)
			slog.Error("migration record failed", "version", version, "error", err)
			os.Exit(1)
		}
		if err := tx.Commit(ctx); err != nil {
			slog.Error("migration commit failed", "version", version, "error", err)
			os.Exit(1)
		}
		fmt.Printf("applied %s\n", version)
	}
}
