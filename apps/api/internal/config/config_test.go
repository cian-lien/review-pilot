package config

import (
	"strings"
	"testing"
)

func TestInvalidConfigDoesNotEchoValues(t *testing.T) {
	t.Setenv("APP_URL", "http://secret-user:secret-password@example.com")
	_, err := Load()
	if err == nil {
		t.Fatal("unsafe public origin accepted")
	}
	if strings.Contains(err.Error(), "secret-password") {
		t.Fatal("secret in configuration error")
	}
}
