//go:build integration

package persistence_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reviewpilot.local/api/internal/persistence"
	"reviewpilot.local/api/internal/testsupport"
)

func TestLegacySchema(t *testing.T) {
	ctx := context.Background()
	db := testsupport.NewDatabase(t)
	store, err := persistence.Open(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	directory := os.Getenv("MIGRATION_DIRECTORY")
	if directory == "" {
		t.Fatal("MIGRATION_DIRECTORY required")
	}
	if err := db.Exec("CREATE TABLE schema_migrations(name text PRIMARY KEY,hash text NOT NULL,applied_at timestamptz NOT NULL DEFAULT clock_timestamp())").Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0001-review-core.sql", "0002-sessions.sql"} {
		source, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(source)
		if err := db.Exec(string(source)).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("INSERT INTO schema_migrations(name,hash) VALUES(?,?)", name, hex.EncodeToString(hash[:])).Error; err != nil {
			t.Fatal(err)
		}
	}
	runID := "01010101-0101-4101-8101-010101010101"
	for _, sql := range []string{
		"INSERT INTO installations(id) VALUES(101)",
		"INSERT INTO repositories(installation_id,id,full_name,public,enabled) VALUES(101,202,'fixture/repo',true,true)",
		"INSERT INTO users(id,login) VALUES(42,'fixture-user')",
		"INSERT INTO sessions(token_hash,user_id,encrypted_user_token,expires_at) VALUES('legacy-hash',42,'legacy-opaque-ciphertext',clock_timestamp()+interval '1 hour')",
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("INSERT INTO review_runs(id,installation_id,repository_id,pr_number,base_sha,head_sha,pipeline_version,generation,snapshot) VALUES(?,101,202,7,?,?, 'foundation-v1',0,'{}'::jsonb)", runID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := persistence.ApplyMigrations(ctx, store, directory); err != nil {
			t.Fatal(err)
		}
	}
	var tableCount int64
	db.Raw("SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('outbox_events','job_results','publication_requests','runtime_control')").Scan(&tableCount)
	if tableCount != 4 {
		t.Fatalf("new migration missing: got %d tables", tableCount)
	}
	var gotRunID, ciphertext string
	db.Raw("SELECT id::text FROM review_runs").Scan(&gotRunID)
	db.Raw("SELECT encrypted_user_token FROM sessions").Scan(&ciphertext)
	var count int64
	db.Raw("SELECT count(*) FROM schema_migrations").Scan(&count)
	var parallel int64
	db.Raw("SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('review_run','repository','session')").Scan(&parallel)
	changed := t.TempDir()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		source, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if entry.Name() == "0001-review-core.sql" {
			source = append(source, []byte("\n-- changed\n")...)
		}
		if err := os.WriteFile(filepath.Join(changed, entry.Name()), source, 0600); err != nil {
			t.Fatal(err)
		}
	}
	rejected := persistence.ApplyMigrations(ctx, store, changed) != nil
	result := map[string]any{
		"runIdPreserved": gotRunID == runID, "sessionCiphertextPreserved": ciphertext == "legacy-opaque-ciphertext",
		"parallelTablesCreated": parallel != 0, "noDuplicateMigrationRows": count == 3, "changedMigrationRejected": rejected,
	}
	data, _ := json.Marshal(result)
	if err := os.WriteFile(os.Getenv("SCENARIO_RESULT_PATH"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
