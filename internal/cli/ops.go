// Package cli: operator summaries added in 0.2.0.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/ValidatorStack/solana-vigil/internal/config"
	"github.com/ValidatorStack/solana-vigil/internal/monitor"
	"github.com/ValidatorStack/solana-vigil/internal/ops"
	"github.com/ValidatorStack/solana-vigil/internal/rpc"
)

func judge(snap monitor.Snapshot) int {
	if snap.Delinquent != nil && *snap.Delinquent {
		return 1
	}
	return 0
}

func runScore(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("score", stderr)
	path := fs.String("config", "vigil.json", "configuration file")
	asJSON := fs.Bool("json", false, "emit JSON")
	diskDir := fs.String("disk", "", "directory to forecast (default: config directory)")
	if code := parseFlags(fs, args); code >= 0 {
		return code
	}
	src, cfg, err := loadMonitor(*path, nil, false)
	if err != nil {
		return configError(stderr, err)
	}
	_ = cfg
	cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	snap := src.Collect(cctx)

	dir := *diskDir
	if dir == "" {
		dir = "."
	}
	var days *float64
	if d, derr := ops.DiskForecast(dir); derr == nil {
		days = d.DaysLeft
	}
	s := ops.Build(snap, nil, days)

	if *asJSON {
		_ = json.NewEncoder(stdout).Encode(s)
		return judge(snap)
	}
	fmt.Fprintf(stdout, "Vigil score: %d/100 (%s) · %d%% of the checks were observable\n\n",
		s.Value, s.Grade, s.Coverage)
	for _, c := range s.Cells {
		mark := "ok  "
		if c.Score == 0 {
			mark = "fix "
		}
		fmt.Fprintf(stdout, "  %s %-14s %-52s\n", mark, c.Name, c.Detail)
		if c.FixHint != "" && c.Score == 0 {
			fmt.Fprintf(stdout, "        -> %s\n", c.FixHint)
		}
	}
	if len(s.Missing) > 0 {
		fmt.Fprintf(stdout, "\n  not observed: ")
		for i, m := range s.Missing {
			if i > 0 {
				fmt.Fprint(stdout, ", ")
			}
			fmt.Fprint(stdout, m)
		}
		fmt.Fprintln(stdout)
	}
	return judge(snap)
}

func runDisk(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("disk", stderr)
	dir := fs.String("path", ".", "directory to measure")
	asJSON := fs.Bool("json", false, "emit JSON")
	if code := parseFlags(fs, args); code >= 0 {
		return code
	}
	d, err := ops.DiskForecast(*dir)
	if err != nil {
		fmt.Fprintln(stderr, "disk:", err)
		return 2
	}
	if *asJSON {
		_ = json.NewEncoder(stdout).Encode(d)
		return 0
	}
	fmt.Fprintf(stdout, "path      %s\n", d.Path)
	fmt.Fprintf(stdout, "total     %.1f GB\n", float64(d.TotalBytes)/(1<<30))
	fmt.Fprintf(stdout, "free      %.1f GB (%.1f%% used)\n", float64(d.FreeBytes)/(1<<30), d.UsedPct)
	if d.GrowthGBDay != nil {
		fmt.Fprintf(stdout, "growth    %.2f GB/day (%d samples)\n", *d.GrowthGBDay, d.Samples)
	}
	if d.DaysLeft != nil {
		fmt.Fprintf(stdout, "forecast  %.1f days to full\n", *d.DaysLeft)
	} else {
		fmt.Fprintf(stdout, "forecast  needs a second sample, hours apart\n")
	}
	return 0
}

func runKeys(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("keys", stderr)
	identity := fs.String("identity", "", "identity keypair path")
	vote := fs.String("vote", "", "vote keypair path")
	asJSON := fs.Bool("json", false, "emit JSON")
	if code := parseFlags(fs, args); code >= 0 {
		return code
	}
	var out []ops.KeyFile
	if *identity != "" {
		out = append(out, ops.InspectKey(*identity))
	}
	if *vote != "" {
		out = append(out, ops.InspectKey(*vote))
	}
	if len(out) == 0 {
		fmt.Fprintln(stderr, "give at least one of --identity or --vote")
		return 2
	}
	if *asJSON {
		_ = json.NewEncoder(stdout).Encode(out)
		return 0
	}
	bad := 0
	for _, k := range out {
		if !k.Exists {
			fmt.Fprintf(stdout, "missing   %s\n", k.Path)
			bad = 1
			continue
		}
		fmt.Fprintf(stdout, "%-34s %s  %s  %s\n", k.Path, k.Mode, k.Modified, humanSize(k.Bytes))
		for _, p := range k.Problems {
			fmt.Fprintf(stdout, "   -> %s\n", p)
			bad = 1
		}
	}
	return bad
}

func humanSize(n int64) string {
	switch {
	case n > 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n > 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func runDuties(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("duties", stderr)
	path := fs.String("config", "vigil.json", "configuration file")
	asJSON := fs.Bool("json", false, "emit JSON")
	if code := parseFlags(fs, args); code >= 0 {
		return code
	}
	_, cfg, err := loadMonitor(*path, nil, false)
	if err != nil {
		return configError(stderr, err)
	}
	client := rpc.New(cfg.RPC.LocalURL, rpc.Options{Timeout: cfg.RPC.Timeout.Duration, MaxRetries: cfg.RPC.MaxRetries})
	row, err := ops.Duties(ctx, client, cfg.Validator.VoteAccount, nil)
	if err != nil {
		fmt.Fprintln(stderr, "duties:", err)
		return 2
	}
	if *asJSON {
		_ = json.NewEncoder(stdout).Encode(row)
		return 0
	}
	fmt.Fprintf(stdout, "epoch %d  leader slots %d  blocks %d  missed %d  skip %.2f%%\n",
		row.Epoch, row.LeaderSlots, row.Blocks, row.Missed, row.SkipRatePct)
	return 0
}

func runRewards(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("rewards", stderr)
	path := fs.String("config", "vigil.json", "configuration file")
	epochs := fs.Uint64("epochs", 5, "how many past epochs")
	asJSON := fs.Bool("json", false, "emit JSON")
	if code := parseFlags(fs, args); code >= 0 {
		return code
	}
	_, cfg, err := loadMonitor(*path, nil, false)
	if err != nil {
		return configError(stderr, err)
	}
	client := rpc.New(cfg.RPC.LocalURL, rpc.Options{Timeout: cfg.RPC.Timeout.Duration, MaxRetries: cfg.RPC.MaxRetries})
	rows, err := ops.Rewards(ctx, client, cfg.Validator.VoteAccount, *epochs)
	if err != nil {
		fmt.Fprintln(stderr, "rewards:", err)
		return 2
	}
	if *asJSON {
		_ = json.NewEncoder(stdout).Encode(rows)
		return 0
	}
	var total int64
	fmt.Fprintf(stdout, "%-8s %-14s %s\n", "epoch", "SOL", "commission")
	for _, r := range rows {
		total += r.Lamports
		comm := "-"
		if r.Commission != nil {
			comm = fmt.Sprintf("%d%%", *r.Commission)
		}
		fmt.Fprintf(stdout, "%-8d %-14.6f %s\n", r.Epoch, float64(r.Lamports)/1e9, comm)
	}
	fmt.Fprintf(stdout, "total    %.6f SOL over %d epochs\n", float64(total)/1e9, len(rows))
	return 0
}

func runFleet(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlags("fleet", stderr)
	file := fs.String("file", "fleet.json", "fleet definition")
	asJSON := fs.Bool("json", false, "emit JSON")
	if code := parseFlags(fs, args); code >= 0 {
		return code
	}
	entries, err := ops.LoadFleet(*file)
	if err != nil {
		fmt.Fprintln(stderr, "fleet:", err)
		return 2
	}
	rows := ops.RunFleet(ctx, entries, func(c context.Context, cfg config.Config) (monitor.Snapshot, error) {
		options := rpc.Options{Timeout: cfg.RPC.Timeout.Duration, MaxRetries: cfg.RPC.MaxRetries, Concurrency: 6}
		local := rpc.New(cfg.RPC.LocalURL, options)
		m := monitor.New(cfg, local, nil, nil, false)
		snap := m.Collect(c)
		return snap, nil
	})
	if *asJSON {
		_ = json.NewEncoder(stdout).Encode(rows)
		return 0
	}
	fmt.Fprintf(stdout, "%-18s %-14s %-10s %-6s %s\n", "node", "status", "score", "missed", "runway")
	worst := 0
	for _, r := range rows {
		score := "-"
		if r.Score != nil {
			score = fmt.Sprintf("%d", *r.Score)
		}
		runway := "-"
		if r.Runway != nil {
			runway = fmt.Sprintf("%.1f ep", *r.Runway)
		}
		if r.Err != "" {
			runway = r.Err
		}
		fmt.Fprintf(stdout, "%-18s %-14s %-10s %-6d %s\n", r.Name, r.Status, score, r.Missed, runway)
		if r.Score != nil && *r.Score < 70 {
			worst = 1
		}
	}
	return worst
}
