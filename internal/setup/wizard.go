// Package setup implements Vigil's safe, public-data-only guided setup.
package setup

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ValidatorStack/solana-vigil/internal/base58"
	"github.com/ValidatorStack/solana-vigil/internal/config"
	"github.com/ValidatorStack/solana-vigil/internal/rpc"
)

type Options struct {
	VoteAccount    string
	Identity       string
	LocalRPC       string
	ReferenceRPC   string
	Output         string
	NonInteractive bool
	SkipProbe      bool
	Force          bool
}

func Run(ctx context.Context, input io.Reader, output io.Writer, options Options) error {
	reader := bufio.NewReader(input)
	cfg := config.Default()
	cfg.Validator.VoteAccount = options.VoteAccount
	cfg.Validator.Identity = options.Identity
	if options.LocalRPC != "" {
		cfg.RPC.LocalURL = options.LocalRPC
	}
	cfg.RPC.ReferenceURL = options.ReferenceRPC
	if options.Output == "" {
		options.Output = "vigil.json"
	}
	fmt.Fprintln(output, "Vigil guided setup — read-only Solana validator monitoring")
	fmt.Fprintln(output, "Vigil will NEVER ask for a seed phrase, private key, or keypair file. Only public addresses are used.")
	fmt.Fprintln(output)
	if !options.NonInteractive {
		var err error
		cfg.Validator.VoteAccount, err = prompt(reader, output, "Public vote account address (required)", cfg.Validator.VoteAccount, true)
		if err != nil {
			return err
		}
		cfg.Validator.Identity, err = prompt(reader, output, "Validator identity (optional; verified/auto-filled)", cfg.Validator.Identity, false)
		if err != nil {
			return err
		}
		cfg.RPC.LocalURL, err = prompt(reader, output, "Local validator RPC URL", cfg.RPC.LocalURL, true)
		if err != nil {
			return err
		}
		cfg.RPC.ReferenceURL, err = prompt(reader, output, "Optional same-cluster reference RPC URL", cfg.RPC.ReferenceURL, false)
		if err != nil {
			return err
		}
	}
	if err := validatePublicKey("vote account", cfg.Validator.VoteAccount, true); err != nil {
		return err
	}
	if err := validatePublicKey("identity", cfg.Validator.Identity, false); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if !options.SkipProbe {
		client := rpc.New(cfg.RPC.LocalURL, rpc.Options{Timeout: cfg.RPC.Timeout.Duration, MaxRetries: cfg.RPC.MaxRetries})
		probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		identity, err := client.GetIdentity(probeCtx)
		if err != nil {
			return fmt.Errorf("local RPC probe failed: %w", err)
		}
		if cfg.Validator.Identity == "" {
			cfg.Validator.Identity = identity.Identity
		}
		if identity.Identity != cfg.Validator.Identity {
			return fmt.Errorf("local RPC identity %s does not match configured identity %s", identity.Identity, cfg.Validator.Identity)
		}
		account, _, found, err := client.GetVoteAccount(probeCtx, cfg.Validator.VoteAccount, cfg.RPC.Commitment)
		if err != nil {
			return fmt.Errorf("vote account probe failed: %w", err)
		}
		if !found {
			return fmt.Errorf("vote account was not found by the local RPC")
		}
		if account.NodePubkey != cfg.Validator.Identity {
			return fmt.Errorf("vote account belongs to %s, not local identity %s", account.NodePubkey, cfg.Validator.Identity)
		}
		if cfg.RPC.ReferenceURL != "" {
			reference := rpc.New(cfg.RPC.ReferenceURL, rpc.Options{Timeout: cfg.RPC.Timeout.Duration, MaxRetries: cfg.RPC.MaxRetries})
			if _, err := rpc.ValidateSameCluster(probeCtx, client, reference); err != nil {
				return err
			}
		}
		fmt.Fprintf(output, "✓ Local RPC, identity, vote account%s verified.\n", map[bool]string{true: ", and cluster", false: ""}[cfg.RPC.ReferenceURL != ""])
	}
	if !options.NonInteractive {
		configure, err := confirm(reader, output, "Add one alert destination now? Secrets stay in environment variables.", false)
		if err != nil {
			return err
		}
		if configure {
			hook, err := promptWebhook(reader, output)
			if err != nil {
				return err
			}
			cfg.Alerts.Webhooks = []config.WebhookConfig{hook}
		}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if options.Force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	file, err := os.OpenFile(options.Output, flags, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w (choose another --output or use --force)", options.Output, err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	fmt.Fprintf(output, "\n✓ Wrote %s (mode 0600).\n", options.Output)
	fmt.Fprintf(output, "Next: vigil check --config %s\n", options.Output)
	fmt.Fprintf(output, "Then: vigil watch --config %s\n", options.Output)
	for _, hook := range cfg.Alerts.Webhooks {
		switch hook.Type {
		case "telegram":
			fmt.Fprintf(output, "Before watch: export %s='<telegram-bot-token>'\n", hook.TokenEnv)
		case "pagerduty":
			fmt.Fprintf(output, "Before watch: export %s='<pagerduty-routing-key>'\n", hook.RoutingKeyEnv)
		default:
			fmt.Fprintf(output, "Before watch: export %s='<webhook-url>'\n", hook.URLEnv)
		}
	}
	return nil
}

func prompt(reader *bufio.Reader, output io.Writer, label, current string, required bool) (string, error) {
	for {
		if current != "" {
			fmt.Fprintf(output, "%s [%s]: ", label, current)
		} else {
			fmt.Fprintf(output, "%s: ", label)
		}
		value, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		value = strings.TrimSpace(value)
		if value == "" {
			value = current
		}
		if value != "" || !required {
			return value, nil
		}
		if err == io.EOF {
			return "", fmt.Errorf("%s is required", label)
		}
		fmt.Fprintln(output, "A value is required.")
	}
}

func confirm(reader *bufio.Reader, output io.Writer, label string, defaultYes bool) (bool, error) {
	suffix := " [y/N]: "
	if defaultYes {
		suffix = " [Y/n]: "
	}
	fmt.Fprint(output, label+suffix)
	value, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return defaultYes, nil
	}
	return value == "y" || value == "yes", nil
}

func promptWebhook(reader *bufio.Reader, output io.Writer) (config.WebhookConfig, error) {
	kind, err := prompt(reader, output, "Alert type (slack, discord, telegram, pagerduty, generic)", "slack", true)
	if err != nil {
		return config.WebhookConfig{}, err
	}
	hook := config.WebhookConfig{Name: "primary", Type: strings.ToLower(kind)}
	switch hook.Type {
	case "slack", "discord", "generic":
		hook.URLEnv, err = prompt(reader, output, "Environment variable that will contain the webhook URL", "VIGIL_WEBHOOK_URL", true)
	case "telegram":
		hook.TokenEnv, err = prompt(reader, output, "Environment variable for the Telegram bot token", "VIGIL_TELEGRAM_BOT_TOKEN", true)
		if err == nil {
			hook.ChatID, err = prompt(reader, output, "Telegram chat ID (not secret)", "", true)
		}
	case "pagerduty":
		hook.RoutingKeyEnv, err = prompt(reader, output, "Environment variable for the PagerDuty routing key", "VIGIL_PAGERDUTY_ROUTING_KEY", true)
	default:
		return hook, fmt.Errorf("unsupported alert type %q", kind)
	}
	return hook, err
}

func validatePublicKey(label, value string, required bool) error {
	if value == "" {
		if required {
			return fmt.Errorf("%s is required", label)
		}
		return nil
	}
	decoded, err := base58.Decode(value)
	if err != nil || len(decoded) != 32 {
		return fmt.Errorf("%s must be a base58-encoded 32-byte Solana public address", label)
	}
	return nil
}
