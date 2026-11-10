// Package cli wires Vigil's commands with operator-friendly exit codes.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/ValidatorStack/solana-vigil/internal/alert"
	"github.com/ValidatorStack/solana-vigil/internal/audit"
	"github.com/ValidatorStack/solana-vigil/internal/buildinfo"
	"github.com/ValidatorStack/solana-vigil/internal/config"
	"github.com/ValidatorStack/solana-vigil/internal/metrics"
	"github.com/ValidatorStack/solana-vigil/internal/monitor"
	"github.com/ValidatorStack/solana-vigil/internal/report"
	"github.com/ValidatorStack/solana-vigil/internal/rpc"
	"github.com/ValidatorStack/solana-vigil/internal/setup"
)

func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stdout)
		return 0
	}
	switch args[0] {
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	case "version", "--version":
		fmt.Fprintln(stdout, buildinfo.String())
		return 0
	case "init":
		return runInit(ctx, args[1:], stdin, stdout, stderr)
	case "check":
		return runCheck(ctx, args[1:], stdout, stderr)
	case "watch":
		return runWatch(ctx, args[1:], stdout, stderr)
	case "test-alert":
		return runTestAlert(ctx, args[1:], stdout, stderr)
	case "export":
		return runExport(ctx, args[1:], stdout, stderr)
	case "audit":
		return runAudit(ctx, args[1:], stdout, stderr)
	case "report":
		return runReport(ctx, args[1:], stdout, stderr)
	case "plan-restart":
		return runPlanRestart(ctx, args[1:], stdout, stderr)
	case "score":
		return runScore(ctx, args[1:], stdout, stderr)
	case "disk":
		return runDisk(ctx, args[1:], stdout, stderr)
	case "keys":
		return runKeys(ctx, args[1:], stdout, stderr)
	case "duties":
		return runDuties(ctx, args[1:], stdout, stderr)
	case "rewards":
		return runRewards(ctx, args[1:], stdout, stderr)
	case "fleet":
		return runFleet(ctx, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		usage(stderr)
		return 2
	}
}

func usage(writer io.Writer) {
	fmt.Fprint(writer, `Vigil — read-only, Alpenglow-native Solana validator operations

Usage: vigil <command> [options]

Get started:
  vigil init                 Guided setup (public addresses only, never keys)
  vigil check                One-shot health, VAT, BLS, and duty checks
  vigil watch                Continuous checks with deduplicated alerts
  vigil test-alert           Verify alert delivery without changing state
  vigil export               Prometheus exporter (default 127.0.0.1:9109)

Operate:
  vigil audit                Read-only Linux host and validator-process audit
  vigil report               Text, JSON, or self-contained HTML snapshot
  vigil plan-restart         Find an advisory leader-free maintenance window

Summaries:
  vigil score                One 0-100 health score with per-cell fixes
  vigil disk                 Free space and days-to-full forecast
  vigil keys                 Keypair hygiene (stat only, never reads them)
  vigil duties               Leader slots, blocks and skip rate for the epoch
  vigil rewards              Inflation rewards per epoch, in SOL
  vigil fleet                Roll-up across every node in fleet.json
  vigil version              Print version and build metadata

Exit codes for check/report: 0=ok, 1=warning, 2=critical, 3=unknown.
Run "vigil <command> --help" for command options.
`)
}

func runInit(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := newFlags("init", stderr)
	options := setup.Options{}
	fs.StringVar(&options.VoteAccount, "vote-account", "", "public vote account address")
	fs.StringVar(&options.Identity, "identity", "", "optional public validator identity")
	fs.StringVar(&options.LocalRPC, "local-rpc", "http://127.0.0.1:8899", "local validator RPC URL")
	fs.StringVar(&options.ReferenceRPC, "reference-rpc", "", "optional same-cluster reference RPC URL")
	fs.StringVar(&options.Output, "output", "vigil.json", "configuration file to create")
	fs.BoolVar(&options.NonInteractive, "non-interactive", false, "use flags instead of prompts")
	fs.BoolVar(&options.SkipProbe, "skip-probe", false, "write config without contacting RPC (not recommended)")
	fs.BoolVar(&options.Force, "force", false, "overwrite an existing config")
	if code := parseFlags(fs, args); code >= 0 {
		return code
	}
	if err := setup.Run(ctx, stdin, stdout, options); err != nil {
		fmt.Fprintln(stderr, "Setup failed:", err)
		return 2
	}
	return 0
}

func runCheck(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("check", stderr)
	path := fs.String("config", "vigil.json", "configuration file")
	jsonOutput := fs.Bool("json", false, "emit JSON")
	timeout := fs.Duration("timeout", 45*time.Second, "overall check timeout")
	if code := parseFlags(fs, args); code >= 0 {
		return code
	}
	source, _, err := loadMonitor(*path, nil, false)
	if err != nil {
		return configError(stderr, err)
	}
	checkCtx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	if err := source.ValidateCluster(checkCtx); err != nil {
		fmt.Fprintln(stderr, "Cluster validation failed:", err)
		return 2
	}
	snapshot := source.Collect(checkCtx)
	if *jsonOutput {
		_ = report.WriteJSON(stdout, report.Document{Snapshot: snapshot})
	} else {
		report.WriteText(stdout, report.Document{Snapshot: snapshot})
	}
	return snapshot.OverallStatus().ExitCode()
}

func runWatch(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("watch", stderr)
	path := fs.String("config", "vigil.json", "configuration file")
	logFormat := fs.String("log-format", "text", "text or json")
	logLevel := fs.String("log-level", "info", "debug, info, warn, or error")
	if code := parseFlags(fs, args); code >= 0 {
		return code
	}
	logger, err := newLogger(*logFormat, *logLevel, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	source, cfg, err := loadMonitor(*path, logger, true)
	if err != nil {
		return configError(stderr, err)
	}
	manager, err := alert.NewManager(cfg.Alerts, logger)
	if err != nil {
		fmt.Fprintln(stderr, "Alert setup failed:", err)
		return 2
	}
	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := source.ValidateCluster(startCtx); err != nil {
		fmt.Fprintln(stderr, "Cluster validation failed:", err)
		return 2
	}
	fmt.Fprintf(stdout, "Vigil is watching %s every %s. Alerts: %d destination(s). Press Ctrl+C to stop.\n", cfg.Validator.VoteAccount, cfg.Monitor.Interval.String(), len(cfg.Alerts.Webhooks))
	ticker := time.NewTicker(cfg.Monitor.Interval.Duration)
	defer ticker.Stop()
	for {
		pollTimeout := 5 * cfg.RPC.Timeout.Duration
		if pollTimeout < 30*time.Second {
			pollTimeout = 30 * time.Second
		}
		pollCtx, pollCancel := context.WithTimeout(ctx, pollTimeout)
		snapshot := source.Collect(pollCtx)
		if err := manager.Process(pollCtx, snapshot); err != nil {
			logger.Error("alert processing failed", "error", err)
		}
		pollCancel()
		logger.Info("validator observation", "status", snapshot.OverallStatus(), "checks", len(snapshot.Checks), "missed_leader_slots", len(snapshot.MissedLeaderSlots))
		select {
		case <-ctx.Done():
			fmt.Fprintln(stdout, "Vigil stopped cleanly.")
			return 0
		case <-ticker.C:
		}
	}
}

func runTestAlert(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("test-alert", stderr)
	path := fs.String("config", "vigil.json", "configuration file")
	if code := parseFlags(fs, args); code >= 0 {
		return code
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return configError(stderr, err)
	}
	manager, err := alert.NewManager(cfg.Alerts, slog.Default())
	if err != nil {
		fmt.Fprintln(stderr, "Alert setup failed:", err)
		return 2
	}
	testCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := manager.Test(testCtx, cfg.Validator.VoteAccount, cfg.Validator.Identity); err != nil {
		fmt.Fprintln(stderr, "Test alert failed:", err)
		return 2
	}
	fmt.Fprintf(stdout, "Test notification sent to %d destination(s); incident state was not changed.\n", len(cfg.Alerts.Webhooks))
	return 0
}

func runExport(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("export", stderr)
	path := fs.String("config", "vigil.json", "configuration file")
	listen := fs.String("listen", "127.0.0.1:9109", "Prometheus HTTP listen address")
	cache := fs.Duration("cache", 5*time.Second, "snapshot cache duration")
	once := fs.Bool("once", false, "print one metrics scrape and exit")
	logFormat := fs.String("log-format", "text", "text or json")
	if code := parseFlags(fs, args); code >= 0 {
		return code
	}
	logger, err := newLogger(*logFormat, "info", stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	source, _, err := loadMonitor(*path, logger, false)
	if err != nil {
		return configError(stderr, err)
	}
	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := source.ValidateCluster(startCtx); err != nil {
		fmt.Fprintln(stderr, "Cluster validation failed:", err)
		return 2
	}
	collector := metrics.NewCollector(source, *cache)
	if *once {
		scrapeCtx, scrapeCancel := context.WithTimeout(ctx, 45*time.Second)
		defer scrapeCancel()
		start := time.Now()
		snapshot, err := collector.Snapshot(scrapeCtx)
		fmt.Fprint(stdout, metrics.Render(snapshot, err == nil, time.Since(start)))
		if err != nil {
			return 3
		}
		return 0
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintln(stderr, "Listen failed:", err)
		return 2
	}
	server := &http.Server{Handler: metrics.Handler(collector, 3**cache), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	errChannel := make(chan error, 1)
	go func() { errChannel <- server.Serve(listener) }()
	logger.Info("Prometheus exporter listening", "address", listener.Addr().String(), "os", runtime.GOOS)
	select {
	case err := <-errChannel:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(stderr, err)
			return 2
		}
		return 0
	case <-ctx.Done():
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownCtx)
		return 0
	}
}

func runAudit(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("audit", stderr)
	client := fs.String("client", "auto", "auto, agave, or firedancer")
	pid := fs.Int("pid", 0, "validator PID (auto-detection is single-match only)")
	iface := fs.String("interface", "", "network interface (default route if omitted)")
	format := fs.String("format", "text", "text or json")
	output := fs.String("output", "-", "output file or - for stdout")
	baseline := fs.String("baseline", "", "previous audit JSON for host-drift comparison")
	minMemory := fs.Uint64("min-memory-gib", 256, "memory warning baseline in GiB")
	minFiles := fs.Uint64("min-open-files", 1_000_000, "validator soft nofile warning baseline")
	if code := parseFlags(fs, args); code >= 0 {
		return code
	}
	if *client != "auto" && *client != "agave" && *client != "firedancer" {
		fmt.Fprintln(stderr, "--client must be auto, agave, or firedancer")
		return 2
	}
	result := audit.Run(ctx, audit.Options{Client: *client, PID: *pid, Interface: *iface, MinimumMemoryGiB: *minMemory, MinimumOpenFiles: *minFiles})
	writer, closeWriter, err := outputWriter(*output, stdout)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	defer closeWriter()
	if *format == "json" {
		err = json.NewEncoder(writer).Encode(result)
	} else if *format == "text" {
		audit.WriteText(writer, result)
	} else {
		fmt.Fprintln(stderr, "--format must be text or json")
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	code := result.ExitCode()
	if *baseline != "" {
		data, err := os.ReadFile(*baseline)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		var previous audit.Report
		if err := json.Unmarshal(data, &previous); err != nil {
			fmt.Fprintln(stderr, "Invalid baseline:", err)
			return 2
		}
		drift := audit.Compare(previous, result)
		audit.WriteDrift(stderr, drift)
		if len(drift) > 0 && code == 0 {
			code = 1
		}
	}
	return code
}

func runReport(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("report", stderr)
	path := fs.String("config", "vigil.json", "configuration file")
	format := fs.String("format", "html", "html, json, or text")
	output := fs.String("output", "-", "output file or - for stdout")
	includeAudit := fs.Bool("include-audit", false, "include a read-only Linux host audit")
	client := fs.String("audit-client", "auto", "auto, agave, or firedancer")
	if code := parseFlags(fs, args); code >= 0 {
		return code
	}
	source, _, err := loadMonitor(*path, nil, false)
	if err != nil {
		return configError(stderr, err)
	}
	reportCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if err := source.ValidateCluster(reportCtx); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	document := report.Document{Snapshot: source.Collect(reportCtx)}
	if *includeAudit {
		hostAudit := audit.Run(reportCtx, audit.Options{Client: *client})
		document.Audit = &hostAudit
	}
	writer, closeWriter, err := outputWriter(*output, stdout)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	defer closeWriter()
	switch *format {
	case "json":
		err = report.WriteJSON(writer, document)
	case "html":
		err = report.WriteHTML(writer, document)
	case "text":
		report.WriteText(writer, document)
	default:
		fmt.Fprintln(stderr, "--format must be html, json, or text")
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "Render report:", err)
		return 2
	}
	return document.Snapshot.OverallStatus().ExitCode()
}

func runPlanRestart(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("plan-restart", stderr)
	path := fs.String("config", "vigil.json", "configuration file")
	identity := fs.String("identity", "", "identity override (default: config or local RPC)")
	minimum := fs.Uint64("min-free-slots", 16, "required consecutive non-leader slots")
	search := fs.Uint64("search-slots", 5000, "slots to search within the current epoch")
	jsonOutput := fs.Bool("json", false, "emit JSON")
	if code := parseFlags(fs, args); code >= 0 {
		return code
	}
	source, cfg, err := loadMonitor(*path, nil, false)
	if err != nil {
		return configError(stderr, err)
	}
	planCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	selectedIdentity := *identity
	if selectedIdentity == "" {
		selectedIdentity = cfg.Validator.Identity
	}
	window, err := monitor.PlanRestart(planCtx, source, selectedIdentity, *minimum, *search)
	if err != nil {
		fmt.Fprintln(stderr, "Restart planning failed:", err)
		return 2
	}
	if *jsonOutput {
		_ = json.NewEncoder(stdout).Encode(window)
	} else {
		fmt.Fprintf(stdout, "Advisory leader-free window for %s\nEpoch: %d | observed slot: %d\nWindow: %d–%d (%d slots, starts in %d slots)\n", window.Identity, window.Epoch, window.ObservedSlot, window.WindowStartSlot, window.WindowEndSlot, window.FreeSlots, window.SlotsUntilWindow)
		if window.EstimatedStartIn != nil {
			fmt.Fprintf(stdout, "Estimated start: %s (%.2f slots/s)\n", window.EstimatedStartIn.Round(time.Second), *window.AverageSlotsPerSecond)
		}
		if window.NextLeaderSlot != nil {
			fmt.Fprintf(stdout, "Next scheduled leader slot: %d\n", *window.NextLeaderSlot)
		}
		fmt.Fprintln(stdout, "Warning:", window.Warning)
	}
	return 0
}

func loadMonitor(path string, logger *slog.Logger, trackLeader bool) (*monitor.Monitor, config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, cfg, err
	}
	options := rpc.Options{Timeout: cfg.RPC.Timeout.Duration, MaxRetries: cfg.RPC.MaxRetries, Concurrency: 6, Logger: logger}
	local := rpc.New(cfg.RPC.LocalURL, options)
	var reference *rpc.Client
	if cfg.RPC.ReferenceURL != "" {
		reference = rpc.New(cfg.RPC.ReferenceURL, options)
	}
	return monitor.New(cfg, local, reference, logger, trackLeader), cfg, nil
}

func newLogger(format, level string, writer io.Writer) (*slog.Logger, error) {
	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("invalid log level %q", level)
	}
	options := &slog.HandlerOptions{Level: parsed}
	switch format {
	case "text":
		return slog.New(slog.NewTextHandler(writer, options)), nil
	case "json":
		return slog.New(slog.NewJSONHandler(writer, options)), nil
	default:
		return nil, fmt.Errorf("log format must be text or json")
	}
}

func newFlags(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}
func parseFlags(fs *flag.FlagSet, args []string) int {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(fs.Output(), "unexpected arguments: %s\n", strings.Join(fs.Args(), " "))
		return 2
	}
	return -1
}
func configError(stderr io.Writer, err error) int {
	fmt.Fprintln(stderr, err)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(stderr, "Run `vigil init` for guided setup, or pass --config /path/to/vigil.json.")
	}
	return 2
}
func outputWriter(path string, stdout io.Writer) (io.Writer, func(), error) {
	if path == "-" || path == "" {
		return stdout, func() {}, nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, func() {}, err
	}
	return file, func() { _ = file.Close() }, nil
}
