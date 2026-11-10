package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ValidatorStack/solana-vigil/internal/buildinfo"
	"github.com/ValidatorStack/solana-vigil/internal/config"
	"github.com/ValidatorStack/solana-vigil/internal/monitor"
)

type webhookSender struct {
	name       string
	kind       string
	endpoint   string
	token      string
	chatID     string
	routingKey string
	headers    map[string]string
	client     *http.Client
	timeout    time.Duration
}

func NewSenders(configs []config.WebhookConfig) ([]Sender, error) {
	senders := make([]Sender, 0, len(configs))
	for _, cfg := range configs {
		sender := &webhookSender{name: cfg.Name, kind: cfg.Type, headers: make(map[string]string), client: &http.Client{}, timeout: cfg.Timeout.Duration}
		if sender.timeout <= 0 {
			sender.timeout = 5 * time.Second
		}
		var err error
		switch cfg.Type {
		case "slack", "discord", "generic":
			sender.endpoint, err = requiredEnv(cfg.URLEnv)
			if err == nil {
				err = validateWebhookURL(sender.endpoint)
			}
		case "telegram":
			sender.token, err = requiredEnv(cfg.TokenEnv)
			sender.chatID = cfg.ChatID
			sender.endpoint = "https://api.telegram.org/bot" + sender.token + "/sendMessage"
		case "pagerduty":
			sender.routingKey, err = requiredEnv(cfg.RoutingKeyEnv)
			sender.endpoint = "https://events.pagerduty.com/v2/enqueue"
		}
		if err != nil {
			return nil, fmt.Errorf("webhook %s: %w", cfg.Name, err)
		}
		for key, value := range cfg.Headers {
			if strings.HasPrefix(value, "env:") {
				value, err = requiredEnv(strings.TrimPrefix(value, "env:"))
				if err != nil {
					return nil, fmt.Errorf("webhook %s header %s: %w", cfg.Name, key, err)
				}
			}
			sender.headers[key] = value
		}
		senders = append(senders, sender)
	}
	return senders, nil
}

func (s *webhookSender) Name() string { return s.name }

func (s *webhookSender) Send(ctx context.Context, notification Notification) error {
	text := FormatNotification(notification)
	var payload any
	switch s.kind {
	case "slack":
		payload = map[string]any{"text": text}
	case "discord":
		if len(text) > 1900 {
			text = text[:1900] + "…"
		}
		payload = map[string]any{"content": text}
	case "telegram":
		payload = map[string]any{"chat_id": s.chatID, "text": text, "disable_web_page_preview": true}
	case "pagerduty":
		action, severity := "trigger", "warning"
		if notification.Resolved {
			action = "resolve"
		}
		if notification.Check.Status == monitor.Critical {
			severity = "critical"
		}
		payload = map[string]any{
			"routing_key": s.routingKey, "event_action": action, "dedup_key": "vigil:" + notification.Check.ID,
			"payload": map[string]any{
				"summary": notification.Check.Summary, "source": "vigil", "severity": severity,
				"timestamp": notification.Check.ObservedAt.UTC().Format(time.RFC3339),
				"component": notification.Check.ID, "group": "solana-validator",
				"custom_details": map[string]any{"detail": notification.Check.Detail, "vote_account": notification.VoteAccount, "identity": notification.Identity},
			},
		}
	default:
		payload = map[string]any{"text": text, "notification": notification}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	requestContext, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestContext, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", buildinfo.UserAgent())
	for key, value := range s.headers {
		req.Header.Set(key, value)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		// url.Error includes the request URL, which contains a Telegram token or
		// webhook secret. Return only the wrapped network cause.
		var urlErr *url.Error
		if ok := errorAs(err, &urlErr); ok {
			return fmt.Errorf("delivery request failed: %w", urlErr.Err)
		}
		return fmt.Errorf("delivery request failed")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("delivery returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func FormatNotification(notification Notification) string {
	status := strings.ToUpper(string(notification.Check.Status))
	prefix := "🚨 VIGIL " + status
	if notification.Resolved {
		prefix = "✅ VIGIL RESOLVED"
	}
	var builder strings.Builder
	builder.WriteString(prefix)
	builder.WriteString("\n")
	builder.WriteString(notification.Check.Summary)
	if notification.Check.Detail != "" {
		builder.WriteString("\n")
		builder.WriteString(notification.Check.Detail)
	}
	if notification.VoteAccount != "" {
		builder.WriteString("\nVote: ")
		builder.WriteString(notification.VoteAccount)
	}
	if notification.Identity != "" {
		builder.WriteString("\nIdentity: ")
		builder.WriteString(notification.Identity)
	}
	builder.WriteString("\nObserved: ")
	builder.WriteString(notification.Check.ObservedAt.UTC().Format(time.RFC3339))
	return builder.String()
}

func requiredEnv(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("environment variable name is empty")
	}
	value := os.Getenv(name)
	if value == "" {
		return "", fmt.Errorf("environment variable %s is not set", name)
	}
	return value, nil
}

func validateWebhookURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return fmt.Errorf("webhook URL must be a valid HTTP(S) URL")
	}
	return nil
}

// Kept as a variable-sized helper to make secret-stripping behavior testable.
func errorAs(err error, target any) bool {
	switch value := target.(type) {
	case **url.Error:
		for err != nil {
			if typed, ok := err.(*url.Error); ok {
				*value = typed
				return true
			}
			unwrapper, ok := err.(interface{ Unwrap() error })
			if !ok {
				return false
			}
			err = unwrapper.Unwrap()
		}
	}
	return false
}
