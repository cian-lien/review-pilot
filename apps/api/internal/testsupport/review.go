package testsupport

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reviewpilot.local/api/internal/config"
	"reviewpilot.local/api/internal/contracts"
	"reviewpilot.local/api/internal/github"
	"reviewpilot.local/api/internal/jobs"
	"reviewpilot.local/api/internal/persistence"
)

func SeedReview(t *testing.T, store *persistence.Store, address string) (contracts.ReviewLease, *github.Gateway) {
	t.Helper()
	if err := persistence.ApplyMigrations(context.Background(), store, os.Getenv("MIGRATION_DIRECTORY")); err != nil {
		t.Fatal(err)
	}
	fixtureID := "review-fixture-" + jobs.Hash([]byte(t.Name()))[:12]
	model := config.ReviewModelConfig{ModelConfigVersion: "fake-" + jobs.Hash([]byte(t.Name()))[:12], PriceVersion: "fake-price-v1", BaseURL: address + "/v1", Model: "fake-review", StructuredOutputMode: "json-schema",
		Currency: "TEST", InputMicrosPerMillion: "1000000", OutputMicrosPerMillion: "1000000", RunLimitMicros: "100000", RepositoryDailyLimitMicros: "1000000", GlobalMonthlyLimitMicros: "10000000", UsageSupported: true, OnlyTokenBilling: true, FixtureID: fixtureID}
	dir := t.TempDir()
	body, _ := json.Marshal(model)
	modelPath := filepath.Join(dir, "model.json")
	if err := os.WriteFile(modelPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"REVIEW_MODE": "fake", "REVIEW_TEST_ISOLATED": "1", "REVIEW_TEST_FIXTURE_ID": fixtureID, "TEST_MODEL_BASE_URL": model.BaseURL,
		"TEST_GITHUB_BASE_URL": address, "REVIEW_MODEL_CONFIG_FILE": modelPath, "GITHUB_REVIEW_PUBLISH_ENABLED": "false"} {
		t.Setenv(name, value)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "key.pem")
	if err = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0600); err != nil {
		t.Fatal(err)
	}
	gateway := github.New(config.Config{GitHubAppID: 77, GitHubPrivateKeyPath: keyPath})
	gateway.APIURL = address
	tenant := contracts.TenantRef{InstallationId: 101, RepositoryId: 202}
	db := store.DB()
	for _, statement := range []string{"UPDATE runtime_control SET mode='go',epoch=1 WHERE id=1", "INSERT INTO installations(id) VALUES(101) ON CONFLICT DO NOTHING",
		"INSERT INTO repositories(installation_id,id,full_name,public,enabled) VALUES(101,202,'acme/web',true,true) ON CONFLICT DO NOTHING"} {
		if err = db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	var generation int
	db.Raw("SELECT count(*) FROM review_runs").Scan(&generation)
	snapshot := contracts.ReviewSnapshot{Identity: contracts.ReviewIdentity{Tenant: tenant, PrNumber: 1, BaseSha: strings.Repeat("a", 40), HeadSha: strings.Repeat("b", 40), PipelineVersion: "review-v1", Generation: generation, RepositoryFullName: "acme/web", PrTitle: "Fixture review"},
		ModelConfigVersion: model.ModelConfigVersion, PriceVersion: model.PriceVersion, RepositoryConfigVersion: 1, GenerationPromptVersion: "review-generation-v1", VerificationPromptVersion: "review-verification-v1", MemorySetVersion: "none", TrustedRulesSha: "none", Policy: config.DefaultReviewPolicy(),
		Provenance: contracts.ReviewProvenance{ModelMode: "fake", GithubMode: "fake", FixtureId: fixtureID, ContentExpiresAt: time.Now().Add(30 * 24 * time.Hour)}}
	runID, taskID, token := jobs.UUID(), jobs.UUID(), jobs.UUID()
	body, _ = json.Marshal(snapshot)
	if err = db.Exec("INSERT INTO review_runs(id,installation_id,repository_id,pr_number,base_sha,head_sha,pipeline_version,generation,snapshot,status) VALUES(?,101,202,1,?,?,'review-v1',?,?::jsonb,'Indexing')", runID, snapshot.Identity.BaseSha, snapshot.Identity.HeadSha, generation, string(body)).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("INSERT INTO jobs(id,run_id,status,attempts,lease_token,lease_expires_at,execution_epoch) VALUES(?,?,'running',1,?,clock_timestamp()+interval '90 seconds',1)", taskID, runID, token).Error; err != nil {
		t.Fatal(err)
	}
	var stored persistence.Run
	if err = db.First(&stored, "id=?", runID).Error; err != nil {
		t.Fatal(err)
	}
	return contracts.ReviewLease{TaskId: taskID, RunId: runID, Tenant: tenant, HeadSha: snapshot.Identity.HeadSha, InputHash: jobs.Hash(stored.Snapshot), LeaseToken: token, LeaseExpiresAt: time.Now().Add(90 * time.Second), ExecutionEpoch: 1, Attempt: 1, Snapshot: snapshot}, gateway
}

func ReviewVerdict(t *testing.T, scenario string, facts map[string]any) {
	t.Helper()
	facts["scenario"] = scenario
	facts["passed"] = true
	facts["simulated"] = true
	data, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(os.Getenv("SCENARIO_RESULT_PATH"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
