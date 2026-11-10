package alert

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ValidatorStack/solana-vigil/internal/config"
	"github.com/ValidatorStack/solana-vigil/internal/monitor"
)

type recordingSender struct{ notifications []Notification }

func (s *recordingSender) Name() string { return "test" }
func (s *recordingSender) Send(_ context.Context, notification Notification) error {
	s.notifications = append(s.notifications, notification)
	return nil
}

type flakyResolveSender struct {
	recordingSender
	failures int
}

func (s *flakyResolveSender) Send(ctx context.Context, notification Notification) error {
	if notification.Resolved && s.failures > 0 {
		s.failures--
		return errors.New("simulated delivery failure")
	}
	return s.recordingSender.Send(ctx, notification)
}

func TestWarningRequiresSustainedFailureThenResolves(t *testing.T) {
	cfg := config.Default().Alerts
	cfg.StateFile = filepath.Join(t.TempDir(), "state.json")
	sender := &recordingSender{}
	manager := &Manager{cfg: cfg, senders: []Sender{sender}, logger: testLogger(), state: persistedState{Version: 1, Incidents: make(map[string]*incidentState)}}
	base := time.Now().UTC()
	warning := monitor.Check{ID: "test", Status: monitor.Warning, Summary: "warning", ObservedAt: base}
	if err := manager.Process(context.Background(), monitor.Snapshot{Timestamp: base, Checks: []monitor.Check{warning}}); err != nil {
		t.Fatal(err)
	}
	if len(sender.notifications) != 0 {
		t.Fatal("first warning should be suppressed")
	}
	warning.ObservedAt = base.Add(10 * time.Second)
	if err := manager.Process(context.Background(), monitor.Snapshot{Timestamp: base.Add(10 * time.Second), Checks: []monitor.Check{warning}}); err != nil {
		t.Fatal(err)
	}
	if len(sender.notifications) != 1 || sender.notifications[0].Resolved {
		t.Fatalf("expected one firing alert: %+v", sender.notifications)
	}
	ok := monitor.Check{ID: "test", Status: monitor.OK, Summary: "healthy", ObservedAt: base.Add(20 * time.Second)}
	_ = manager.Process(context.Background(), monitor.Snapshot{Timestamp: base.Add(20 * time.Second), Checks: []monitor.Check{ok}})
	ok.ObservedAt = base.Add(30 * time.Second)
	_ = manager.Process(context.Background(), monitor.Snapshot{Timestamp: base.Add(30 * time.Second), Checks: []monitor.Check{ok}})
	if len(sender.notifications) != 2 || !sender.notifications[1].Resolved {
		t.Fatalf("expected resolved alert: %+v", sender.notifications)
	}
}

func TestResolvedDeliveryFailureRetriesWithoutForgettingIncident(t *testing.T) {
	cfg := config.Default().Alerts
	cfg.StateFile = ""
	sender := &flakyResolveSender{failures: 1}
	manager := &Manager{cfg: cfg, senders: []Sender{sender}, logger: testLogger(), state: persistedState{Version: 1, Incidents: make(map[string]*incidentState)}}
	base := time.Now().UTC()
	critical := monitor.Check{ID: "test", Status: monitor.Critical, Summary: "critical", ObservedAt: base}
	if err := manager.Process(context.Background(), monitor.Snapshot{Timestamp: base, Checks: []monitor.Check{critical}}); err != nil {
		t.Fatal(err)
	}
	ok := monitor.Check{ID: "test", Status: monitor.OK, Summary: "healthy"}
	if err := manager.Process(context.Background(), monitor.Snapshot{Timestamp: base.Add(time.Second), Checks: []monitor.Check{ok}}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Process(context.Background(), monitor.Snapshot{Timestamp: base.Add(2 * time.Second), Checks: []monitor.Check{ok}}); err == nil {
		t.Fatal("expected simulated resolution delivery failure")
	}
	if !manager.state.Incidents["test"].Notified {
		t.Fatal("failed resolution forgot the active incident")
	}
	if err := manager.Process(context.Background(), monitor.Snapshot{Timestamp: base.Add(3 * time.Second), Checks: []monitor.Check{ok}}); err != nil {
		t.Fatal(err)
	}
	if len(sender.notifications) != 2 || !sender.notifications[1].Resolved {
		t.Fatalf("resolution was not retried: %+v", sender.notifications)
	}
	if _, exists := manager.state.Incidents["test"]; exists {
		t.Fatal("successfully resolved incident was not cleared")
	}
}

func TestCriticalAndTransientAlertImmediately(t *testing.T) {
	cfg := config.Default().Alerts
	cfg.StateFile = ""
	sender := &recordingSender{}
	manager := &Manager{cfg: cfg, senders: []Sender{sender}, logger: testLogger(), state: persistedState{Version: 1, Incidents: make(map[string]*incidentState)}}
	checks := []monitor.Check{
		{ID: "critical", Status: monitor.Critical, ObservedAt: time.Now()},
		{ID: "leader.slot.42", Status: monitor.Critical, Transient: true, ObservedAt: time.Now()},
	}
	if err := manager.Process(context.Background(), monitor.Snapshot{Timestamp: time.Now(), Checks: checks}); err != nil {
		t.Fatal(err)
	}
	if len(sender.notifications) != 2 {
		t.Fatalf("got %d alerts", len(sender.notifications))
	}
	if len(manager.state.Incidents) != 1 {
		t.Fatal("transient event created persistent state")
	}
}

func TestNotificationFormattingContainsEvidence(t *testing.T) {
	text := FormatNotification(Notification{Check: monitor.Check{Status: monitor.Critical, Summary: "VAT low", Detail: "runway=0.5", ObservedAt: time.Now()}, VoteAccount: "Vote111"})
	if text == "" || !containsAll(text, "CRITICAL", "VAT low", "runway=0.5", "Vote111") {
		t.Fatalf("incomplete notification: %s", text)
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
