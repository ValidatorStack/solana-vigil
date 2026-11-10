package ops

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/ValidatorStack/solana-vigil/internal/config"
	"github.com/ValidatorStack/solana-vigil/internal/monitor"
)

// FleetEntry is one validator in fleet.json.
type FleetEntry struct {
	Name   string `json:"name"`
	Config string `json:"config"` // path to a vigil.json
}

// FleetResult is one node's outcome.
type FleetResult struct {
	Name    string  `json:"name"`
	Vote    string  `json:"vote_account"`
	Status  string  `json:"status"`
	Score   *int    `json:"score,omitempty"`
	Runway  *float64 `json:"runway_epochs,omitempty"`
	Missed  uint64  `json:"missed_slots"`
	Err     string  `json:"error,omitempty"`
}

// LoadFleet reads the fleet file (JSON keeps the binary dependency-free).
func LoadFleet(path string) ([]FleetEntry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f struct {
		Nodes []FleetEntry `json:"nodes"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	return f.Nodes, nil
}

// RunFleet checks every node in sequence and returns one row each, so a fleet
// scan degrades gracefully when a single host is down.
func RunFleet(ctx context.Context, entries []FleetEntry, runOne func(context.Context, config.Config) (monitor.Snapshot, error)) []FleetResult {
	out := make([]FleetResult, 0, len(entries))
	for _, e := range entries {
		res := FleetResult{Name: e.Name}
		cfg, err := config.Load(e.Config)
		if err != nil {
			res.Status, res.Err = "config", err.Error()
			out = append(out, res)
			continue
		}
		res.Vote = cfg.Validator.VoteAccount
		cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		snap, err := runOne(cctx, cfg)
		cancel()
		if err != nil {
			res.Status, res.Err = "unreachable", err.Error()
			out = append(out, res)
			continue
		}
		res.Missed = snap.Leader.MissedSlots
		if snap.VAT.RunwayEpochs != nil {
			v := *snap.VAT.RunwayEpochs
			res.Runway = &v
		}
		s := Build(snap, nil, nil)
		res.Score = &s.Value
		res.Status = s.Grade
		out = append(out, res)
	}
	return out
}
