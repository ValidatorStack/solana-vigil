package alert

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ValidatorStack/solana-vigil/internal/config"
	"github.com/ValidatorStack/solana-vigil/internal/monitor"
)

func TestSlackWebhookDelivery(t *testing.T) {
	var text string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		text, _ = payload["text"].(string)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	t.Setenv("VIGIL_TEST_WEBHOOK", server.URL)
	senders, err := NewSenders([]config.WebhookConfig{{Name: "ops", Type: "slack", URLEnv: "VIGIL_TEST_WEBHOOK"}})
	if err != nil {
		t.Fatal(err)
	}
	notification := Notification{Check: monitor.Check{Status: monitor.Warning, Summary: "test alert", ObservedAt: time.Now()}}
	if err := senders[0].Send(context.Background(), notification); err != nil {
		t.Fatal(err)
	}
	if text == "" || !containsAll(text, "WARNING", "test alert") {
		t.Fatalf("unexpected payload %q", text)
	}
}

func TestMissingWebhookSecretFailsAtStartup(t *testing.T) {
	_, err := NewSenders([]config.WebhookConfig{{Name: "ops", Type: "slack", URLEnv: "VIGIL_DEFINITELY_NOT_SET"}})
	if err == nil {
		t.Fatal("expected missing environment error")
	}
}
