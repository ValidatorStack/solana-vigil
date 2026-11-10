// Package alert provides persistent deduplication, flap suppression, and
// delivery without embedding secrets in Vigil's configuration.
package alert

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ValidatorStack/solana-vigil/internal/config"
	"github.com/ValidatorStack/solana-vigil/internal/monitor"
)

type Sender interface {
	Name() string
	Send(context.Context, Notification) error
}

type Notification struct {
	Check       monitor.Check `json:"check"`
	Resolved    bool          `json:"resolved"`
	VoteAccount string        `json:"vote_account"`
	Identity    string        `json:"identity,omitempty"`
	Occurrences int           `json:"occurrences"`
	Repeated    bool          `json:"repeated"`
}

type incidentState struct {
	Failures         int            `json:"failures"`
	Recoveries       int            `json:"recoveries"`
	Notified         bool           `json:"notified"`
	LastStatus       monitor.Status `json:"last_status"`
	LastNotification time.Time      `json:"last_notification,omitempty"`
	LastSummary      string         `json:"last_summary"`
}

type persistedState struct {
	Version   int                       `json:"version"`
	Incidents map[string]*incidentState `json:"incidents"`
}

type Manager struct {
	cfg     config.AlertsConfig
	senders []Sender
	logger  *slog.Logger
	mu      sync.Mutex
	state   persistedState
}

func NewManager(cfg config.AlertsConfig, logger *slog.Logger) (*Manager, error) {
	if logger == nil {
		logger = slog.Default()
	}
	senders, err := NewSenders(cfg.Webhooks)
	if err != nil {
		return nil, err
	}
	manager := &Manager{cfg: cfg, senders: senders, logger: logger}
	manager.state = persistedState{Version: 1, Incidents: make(map[string]*incidentState)}
	if cfg.StateFile != "" {
		data, err := os.ReadFile(cfg.StateFile)
		if err == nil {
			if err := json.Unmarshal(data, &manager.state); err != nil {
				return nil, fmt.Errorf("read alert state: %w", err)
			}
			if manager.state.Incidents == nil {
				manager.state.Incidents = make(map[string]*incidentState)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read alert state: %w", err)
		}
	}
	return manager, nil
}

func (m *Manager) Process(ctx context.Context, snapshot monitor.Snapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	type pendingNotification struct {
		notification Notification
		incidentID   string
	}
	var pending []pendingNotification
	for _, check := range snapshot.Checks {
		if check.Transient {
			if check.Status != monitor.OK {
				pending = append(pending, pendingNotification{notification: Notification{Check: check, VoteAccount: snapshot.VoteAccount, Identity: snapshot.Identity, Occurrences: 1}})
			}
			continue
		}
		state := m.state.Incidents[check.ID]
		if state == nil {
			state = &incidentState{}
			m.state.Incidents[check.ID] = state
		}
		if check.Status == monitor.OK {
			state.Failures = 0
			state.Recoveries++
			if state.Notified && state.Recoveries >= m.cfg.RecoveryThreshold {
				if m.cfg.SendResolved && !state.LastNotification.IsZero() {
					// Keep the incident active until resolution is delivered. A
					// subsequent healthy observation retries a failed resolution.
					pending = append(pending, pendingNotification{notification: Notification{Check: check, Resolved: true, VoteAccount: snapshot.VoteAccount, Identity: snapshot.Identity, Occurrences: state.Recoveries}, incidentID: check.ID})
				} else {
					// If the firing notification never delivered, do not send an
					// orphan recovery for an incident the receiver never saw.
					state.Notified = false
					state.Recoveries = 0
					state.LastStatus = monitor.OK
					state.LastNotification = time.Time{}
				}
			}
			continue
		}

		state.Recoveries = 0
		state.Failures++
		thresholdReached := state.Failures >= m.cfg.FailureThreshold
		immediate := check.Status == monitor.Critical && m.cfg.CriticalImmediate
		severityChanged := state.Notified && state.LastStatus != check.Status
		retryAfterDeliveryError := state.Notified && state.LastNotification.IsZero()
		repeat := state.Notified && !state.LastNotification.IsZero() && snapshot.Timestamp.Sub(state.LastNotification) >= m.cfg.RepeatInterval.Duration
		shouldNotify := (!state.Notified && (thresholdReached || immediate)) || severityChanged || retryAfterDeliveryError || repeat
		if shouldNotify {
			pending = append(pending, pendingNotification{notification: Notification{Check: check, VoteAccount: snapshot.VoteAccount, Identity: snapshot.Identity, Occurrences: state.Failures, Repeated: repeat}, incidentID: check.ID})
			state.Notified = true
			state.LastNotification = snapshot.Timestamp
		}
		state.LastStatus = check.Status
		state.LastSummary = check.Summary
	}

	var deliveryErrors []error
	for _, item := range pending {
		if err := m.dispatch(ctx, item.notification); err != nil {
			deliveryErrors = append(deliveryErrors, err)
			if item.incidentID != "" && !item.notification.Resolved {
				// A zero timestamp causes the next observation to retry instead of
				// waiting for the normal repeat interval. Resolution failures keep
				// the previous timestamp and are retried by the healthy branch.
				m.state.Incidents[item.incidentID].LastNotification = time.Time{}
			}
		} else if item.notification.Resolved {
			delete(m.state.Incidents, item.notification.Check.ID)
		}
	}
	if err := m.save(); err != nil {
		deliveryErrors = append(deliveryErrors, err)
	}
	return errors.Join(deliveryErrors...)
}

// Test sends an explicit test notification without reading or changing any
// incident state. It is safe to use during initial setup.
func (m *Manager) Test(ctx context.Context, voteAccount, identity string) error {
	return m.dispatch(ctx, Notification{
		Check: monitor.Check{
			ID: "vigil.test", Status: monitor.Warning,
			Summary:    "Vigil test notification",
			Detail:     "Alert delivery works. No validator incident is active and no incident state was changed.",
			ObservedAt: time.Now().UTC(),
		},
		VoteAccount: voteAccount,
		Identity:    identity,
		Occurrences: 1,
	})
}

func (m *Manager) dispatch(ctx context.Context, notification Notification) error {
	if len(m.senders) == 0 {
		level := m.logger.Warn
		if notification.Resolved {
			level = m.logger.Info
		}
		level("validator alert", "id", notification.Check.ID, "status", notification.Check.Status, "resolved", notification.Resolved, "summary", notification.Check.Summary, "detail", notification.Check.Detail)
		return nil
	}
	var errs []error
	for _, sender := range m.senders {
		if err := sender.Send(ctx, notification); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", sender.Name(), err))
			m.logger.Error("alert delivery failed", "sender", sender.Name(), "error", err)
		} else {
			m.logger.Info("alert delivered", "sender", sender.Name(), "id", notification.Check.ID, "resolved", notification.Resolved)
		}
	}
	return errors.Join(errs...)
}

func (m *Manager) save() error {
	if m.cfg.StateFile == "" {
		return nil
	}
	data, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	directory := filepath.Dir(m.cfg.StateFile)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create alert state directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".alert-state-*.tmp")
	if err != nil {
		return fmt.Errorf("create alert state: %w", err)
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, m.cfg.StateFile); err != nil {
		// Windows cannot atomically replace an existing file with os.Rename.
		// Linux, the production target, always takes the atomic path above.
		if removeErr := os.Remove(m.cfg.StateFile); removeErr == nil {
			if retryErr := os.Rename(name, m.cfg.StateFile); retryErr == nil {
				return nil
			}
		}
		return fmt.Errorf("replace alert state: %w", err)
	}
	return nil
}
