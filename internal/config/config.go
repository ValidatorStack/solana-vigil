// Package config loads and validates Vigil's public, non-secret configuration.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ValidatorStack/solana-vigil/internal/base58"
)

const LamportsPerSOL uint64 = 1_000_000_000

type Config struct {
	Validator ValidatorConfig `json:"validator"`
	RPC       RPCConfig       `json:"rpc"`
	Monitor   MonitorConfig   `json:"monitor"`
	Alerts    AlertsConfig    `json:"alerts"`
}

type ValidatorConfig struct {
	VoteAccount string `json:"vote_account"`
	// Identity is optional. When empty, Vigil compares the local RPC identity
	// with the nodePubkey associated with VoteAccount.
	Identity string `json:"identity,omitempty"`
}

type RPCConfig struct {
	LocalURL     string   `json:"local_url"`
	ReferenceURL string   `json:"reference_url,omitempty"`
	Commitment   string   `json:"commitment"`
	Timeout      Duration `json:"timeout"`
	MaxRetries   int      `json:"max_retries"`
}

type MonitorConfig struct {
	Interval                  Duration `json:"interval"`
	VATLamportsPerEpoch       uint64   `json:"vat_lamports_per_epoch"`
	VATWarningEpochs          float64  `json:"vat_warning_epochs"`
	VATCriticalEpochs         float64  `json:"vat_critical_epochs"`
	RequireBLS                bool     `json:"require_bls"`
	VoteLagWarningSlots       uint64   `json:"vote_lag_warning_slots"`
	VoteLagCriticalSlots      uint64   `json:"vote_lag_critical_slots"`
	SlotLagWarning            uint64   `json:"slot_lag_warning"`
	SlotLagCritical           uint64   `json:"slot_lag_critical"`
	LeaderSkipWarningPercent  float64  `json:"leader_skip_warning_percent"`
	LeaderSkipCriticalPercent float64  `json:"leader_skip_critical_percent"`
	LeaderMinimumSample       uint64   `json:"leader_minimum_sample"`
	LeaderConfirmationDepth   uint64   `json:"leader_confirmation_depth"`
	AlertOnMissedLeaderSlot   bool     `json:"alert_on_missed_leader_slot"`
}

type AlertsConfig struct {
	StateFile         string          `json:"state_file"`
	FailureThreshold  int             `json:"failure_threshold"`
	RecoveryThreshold int             `json:"recovery_threshold"`
	CriticalImmediate bool            `json:"critical_immediate"`
	RepeatInterval    Duration        `json:"repeat_interval"`
	SendResolved      bool            `json:"send_resolved"`
	Webhooks          []WebhookConfig `json:"webhooks"`
}

type WebhookConfig struct {
	Name          string            `json:"name"`
	Type          string            `json:"type"`
	URLEnv        string            `json:"url_env,omitempty"`
	TokenEnv      string            `json:"token_env,omitempty"`
	ChatID        string            `json:"chat_id,omitempty"`
	RoutingKeyEnv string            `json:"routing_key_env,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	Timeout       Duration          `json:"timeout,omitempty"`
}

func Default() Config {
	return Config{
		RPC: RPCConfig{
			LocalURL:   "http://127.0.0.1:8899",
			Commitment: "confirmed",
			Timeout:    Duration{8 * time.Second},
			MaxRetries: 2,
		},
		Monitor: MonitorConfig{
			Interval:                  Duration{10 * time.Second},
			VATLamportsPerEpoch:       800_000_000,
			VATWarningEpochs:          2,
			VATCriticalEpochs:         1,
			RequireBLS:                true,
			VoteLagWarningSlots:       256,
			VoteLagCriticalSlots:      1024,
			SlotLagWarning:            64,
			SlotLagCritical:           256,
			LeaderSkipWarningPercent:  5,
			LeaderSkipCriticalPercent: 15,
			LeaderMinimumSample:       4,
			LeaderConfirmationDepth:   32,
			AlertOnMissedLeaderSlot:   true,
		},
		Alerts: AlertsConfig{
			StateFile:         ".vigil/alert-state.json",
			FailureThreshold:  2,
			RecoveryThreshold: 2,
			CriticalImmediate: true,
			RepeatInterval:    Duration{time.Hour},
			SendResolved:      true,
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return cfg, fmt.Errorf("parse config %s: multiple JSON values", path)
		}
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return cfg, nil
}

func (c Config) Validate() error {
	var problems []string
	if strings.TrimSpace(c.Validator.VoteAccount) == "" {
		problems = append(problems, "validator.vote_account is required (a public address, never a key file)")
	} else if err := validatePublicAddress(c.Validator.VoteAccount); err != nil {
		problems = append(problems, "validator.vote_account must be a base58-encoded 32-byte public address, not key material")
	}
	if c.Validator.Identity != "" {
		if err := validatePublicAddress(c.Validator.Identity); err != nil {
			problems = append(problems, "validator.identity must be a base58-encoded 32-byte public address, not key material")
		}
	}
	if err := validateEndpoint(c.RPC.LocalURL, true); err != nil {
		problems = append(problems, "rpc.local_url: "+err.Error())
	}
	if err := validateEndpoint(c.RPC.ReferenceURL, false); err != nil {
		problems = append(problems, "rpc.reference_url: "+err.Error())
	}
	switch c.RPC.Commitment {
	case "processed", "confirmed", "finalized":
	default:
		problems = append(problems, "rpc.commitment must be processed, confirmed, or finalized")
	}
	if c.RPC.Timeout.Duration < time.Second || c.RPC.Timeout.Duration > 2*time.Minute {
		problems = append(problems, "rpc.timeout must be between 1s and 2m")
	}
	if c.RPC.MaxRetries < 0 || c.RPC.MaxRetries > 5 {
		problems = append(problems, "rpc.max_retries must be between 0 and 5")
	}
	if c.Monitor.Interval.Duration < time.Second || c.Monitor.Interval.Duration > time.Hour {
		problems = append(problems, "monitor.interval must be between 1s and 1h")
	}
	if c.Monitor.VATCriticalEpochs <= 0 || c.Monitor.VATWarningEpochs < c.Monitor.VATCriticalEpochs {
		problems = append(problems, "monitor VAT thresholds require 0 < critical <= warning")
	}
	if c.Monitor.VoteLagCriticalSlots < c.Monitor.VoteLagWarningSlots {
		problems = append(problems, "monitor.vote_lag_critical_slots must be >= warning")
	}
	if c.Monitor.SlotLagCritical < c.Monitor.SlotLagWarning {
		problems = append(problems, "monitor.slot_lag_critical must be >= warning")
	}
	if !validPercentPair(c.Monitor.LeaderSkipWarningPercent, c.Monitor.LeaderSkipCriticalPercent) {
		problems = append(problems, "leader skip thresholds require 0 <= warning <= critical <= 100")
	}
	if c.Alerts.FailureThreshold < 1 || c.Alerts.RecoveryThreshold < 1 {
		problems = append(problems, "alert failure and recovery thresholds must be at least 1")
	}
	if c.Alerts.RepeatInterval.Duration < time.Minute {
		problems = append(problems, "alerts.repeat_interval must be at least 1m")
	}
	seen := make(map[string]struct{}, len(c.Alerts.Webhooks))
	for i, hook := range c.Alerts.Webhooks {
		prefix := fmt.Sprintf("alerts.webhooks[%d]", i)
		if hook.Name == "" {
			problems = append(problems, prefix+".name is required")
		} else if _, ok := seen[hook.Name]; ok {
			problems = append(problems, prefix+".name must be unique")
		} else {
			seen[hook.Name] = struct{}{}
		}
		switch hook.Type {
		case "slack", "discord", "generic":
			if hook.URLEnv == "" {
				problems = append(problems, prefix+".url_env is required for "+hook.Type)
			}
		case "telegram":
			if hook.TokenEnv == "" || hook.ChatID == "" {
				problems = append(problems, prefix+" requires token_env and chat_id for telegram")
			}
		case "pagerduty":
			if hook.RoutingKeyEnv == "" {
				problems = append(problems, prefix+".routing_key_env is required for pagerduty")
			}
		default:
			problems = append(problems, prefix+".type must be slack, discord, telegram, pagerduty, or generic")
		}
		if hook.Timeout.Duration < 0 || hook.Timeout.Duration > time.Minute {
			problems = append(problems, prefix+".timeout must be between 0s and 1m")
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func validPercentPair(warning, critical float64) bool {
	return warning >= 0 && warning <= critical && critical <= 100
}

func validatePublicAddress(value string) error {
	decoded, err := base58.Decode(value)
	if err != nil || len(decoded) != 32 {
		return errors.New("invalid Solana public address")
	}
	return nil
}

func validateEndpoint(raw string, required bool) error {
	if raw == "" {
		if required {
			return errors.New("is required")
		}
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("must use http or https")
	}
	if parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("must be an origin or RPC path without credentials, query, or fragment")
	}
	return nil
}
