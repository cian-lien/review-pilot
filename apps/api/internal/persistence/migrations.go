package persistence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gorm.io/gorm"
)

var migrationName = regexp.MustCompile(`^[0-9]+.*\.sql$`)

func ApplyMigrations(ctx context.Context, store *Store, directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("migration directory unavailable")
	}
	return store.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(782914630)").Error; err != nil {
			return err
		}
		if err := tx.Exec("CREATE TABLE IF NOT EXISTS schema_migrations(name text PRIMARY KEY,hash text NOT NULL,applied_at timestamptz NOT NULL DEFAULT clock_timestamp())").Error; err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() || !migrationName.MatchString(entry.Name()) {
				continue
			}
			source, err := os.ReadFile(filepath.Join(directory, entry.Name()))
			if err != nil {
				return fmt.Errorf("migration file unavailable: %s", entry.Name())
			}
			digest := sha256.Sum256(source)
			hash := hex.EncodeToString(digest[:])
			var existing []struct{ Hash string }
			if err := tx.Raw("SELECT hash FROM schema_migrations WHERE name=?", entry.Name()).Scan(&existing).Error; err != nil {
				return err
			}
			if len(existing) > 0 {
				if existing[0].Hash != hash {
					return fmt.Errorf("applied migration changed: %s", entry.Name())
				}
				continue
			}
			if err := tx.Exec(string(source)).Error; err != nil {
				return fmt.Errorf("migration failed: %s: %w", entry.Name(), err)
			}
			if err := tx.Exec("INSERT INTO schema_migrations(name,hash) VALUES(?,?)", entry.Name(), hash).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
