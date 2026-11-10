package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadStrictAndDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vigil.json")
	data := `{"validator":{"vote_account":"11111111111111111111111111111111"},"rpc":{"timeout":"3s"}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RPC.LocalURL != "http://127.0.0.1:8899" || cfg.RPC.Timeout.Duration != 3*time.Second {
		t.Fatalf("defaults/overrides not applied: %+v", cfg.RPC)
	}
	if cfg.Monitor.VATLamportsPerEpoch != 800_000_000 {
		t.Fatalf("VAT default lost: %d", cfg.Monitor.VATLamportsPerEpoch)
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vigil.json")
	_ = os.WriteFile(path, []byte(`{"validator":{"vote_account":"11111111111111111111111111111111"},"typo":true}`), 0o600)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected strict unknown-field error, got %v", err)
	}
}

func TestValidateRejectsDangerousEndpoints(t *testing.T) {
	cfg := Default()
	cfg.Validator.VoteAccount = "11111111111111111111111111111111"
	cfg.RPC.LocalURL = "https://user:password@example.com"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("expected credentials rejection, got %v", err)
	}
}

func TestValidateWebhookSecretConfiguration(t *testing.T) {
	cfg := Default()
	cfg.Validator.VoteAccount = "11111111111111111111111111111111"
	cfg.Alerts.Webhooks = []WebhookConfig{{Name: "ops", Type: "slack"}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "url_env") {
		t.Fatalf("expected url_env requirement, got %v", err)
	}
}
