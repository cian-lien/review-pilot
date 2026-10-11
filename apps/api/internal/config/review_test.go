package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReviewDefaultsClosed(t *testing.T) {
	for _, name := range []string{"REVIEW_MODE", "REVIEW_REAL_ALLOWED", "GITHUB_REVIEW_PUBLISH_ENABLED", "REVIEW_MODEL_CONFIG_FILE", "REVIEW_RETAIN_CONTENT"} {
		t.Setenv(name, "")
	}
	cfg, err := LoadReview()
	if err != nil || cfg.Mode != "disabled" || cfg.PublishEnabled {
		t.Fatalf("default review behavior must be closed: %+v %v", cfg, err)
	}
	for _, body := range []string{`{"apiKey":"secret"}`, `{"runLimitMicros":"9223372036854775808"}`} {
		file := filepath.Join(t.TempDir(), "model.json")
		if err := os.WriteFile(file, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("REVIEW_MODEL_CONFIG_FILE", file)
		if _, err := LoadReview(); err == nil {
			t.Fatal("invalid configuration was accepted")
		}
	}
	t.Setenv("REVIEW_MODEL_CONFIG_FILE", "")
	t.Setenv("REVIEW_RETAIN_CONTENT", "false")
	if cfg, err := LoadReview(); err != nil || cfg.Policy.RetainContent {
		t.Fatal("metadata-only policy cannot be selected", err)
	}
	t.Setenv("REVIEW_RETAIN_CONTENT", "invalid")
	if _, err := LoadReview(); err == nil {
		t.Fatal("invalid content policy accepted")
	}
}
