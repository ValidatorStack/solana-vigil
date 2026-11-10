// Package ops adds operator-facing summaries on top of the monitor snapshot:
// one health score, disk forecast, key hygiene, duty attribution, rewards and a
// fleet roll-up. Read-only throughout; missing evidence is "unknown", never a
// fabricated pass.
package ops

import (
	"fmt"
	"strings"

	"github.com/ValidatorStack/solana-vigil/internal/monitor"
)

type Cell struct {
	Name    string `json:"name"`
	Score   int    `json:"score"`
	Weight  int    `json:"weight"`
	Detail  string `json:"detail"`
	FixHint string `json:"fix,omitempty"`
}

type Score struct {
	Value    int      `json:"value"`
	Grade    string   `json:"grade"`
	Coverage int      `json:"coverage_pct"`
	Cells    []Cell   `json:"cells"`
	Missing  []string `json:"missing,omitempty"`
}

func mk(name string, weight int, ok bool, detail, hint string) Cell {
	s := 100
	if !ok {
		s = 0
	}
	return Cell{Name: name, Weight: weight, Score: s, Detail: detail, FixHint: hint}
}

// Build scores a snapshot. hostOK and diskDays are optional observations.
func Build(snap monitor.Snapshot, hostOK *bool, diskDays *float64) Score {
	var cells []Cell
	var missing []string

	if snap.Delinquent != nil {
		if *snap.Delinquent {
			cells = append(cells, mk("delinquency", 25, false, "vote account is delinquent",
				"check vote credits; restart if the node is skipping"))
		} else {
			cells = append(cells, mk("delinquency", 25, true, "not delinquent", ""))
		}
	} else {
		missing = append(missing, "delinquency")
	}

	if snap.VAT.RunwayEpochs != nil {
		left := *snap.VAT.RunwayEpochs
		cells = append(cells, mk("vote runway", 20, left >= 7,
			fmt.Sprintf("%.1f epochs of runway", left),
			"top up the vote account before it reaches zero"))
	} else if snap.VAT.BalanceLamports != nil {
		cells = append(cells, mk("vote runway", 20, true,
			fmt.Sprintf("%d lamports in the vote account", *snap.VAT.BalanceLamports), ""))
	} else {
		missing = append(missing, "vote runway")
	}

	switch strings.ToLower(snap.BLSState) {
	case "registered", "ok", "present", "active":
		cells = append(cells, mk("alpenglow", 10, true, "BLS key registered", ""))
	case "", "unknown":
		missing = append(missing, "alpenglow")
	default:
		cells = append(cells, mk("alpenglow", 10, false, "BLS key: "+snap.BLSState,
			"register the BLS key ahead of the Alpenglow migration"))
	}

	if snap.Leader.Available && snap.Leader.LeaderSlots > 0 {
		rate := float64(snap.Leader.MissedSlots) / float64(snap.Leader.LeaderSlots) * 100
		cells = append(cells, mk("leader duty", 15, rate <= 1.0,
			fmt.Sprintf("%d/%d leader slots missed (%.1f%%)",
				snap.Leader.MissedSlots, snap.Leader.LeaderSlots, rate),
			"missed leaders are usually catchup or an overloaded host"))
	} else {
		missing = append(missing, "leader duty")
	}

	if hostOK != nil {
		if *hostOK {
			cells = append(cells, mk("host", 15, true, "host audit clean", ""))
		} else {
			cells = append(cells, mk("host", 15, false, "host audit found warnings",
				"run `vigil audit` and apply the listed fixes"))
		}
	} else {
		missing = append(missing, "host")
	}

	if diskDays != nil {
		cells = append(cells, mk("disk", 15, *diskDays >= 14,
			fmt.Sprintf("%.1f days of headroom", *diskDays),
			"prune the ledger or grow the volume"))
	} else {
		missing = append(missing, "disk")
	}

	total, weight := 0, 0
	for _, c := range cells {
		total += c.Score * c.Weight
		weight += c.Weight
	}
	s := Score{Cells: cells, Missing: missing}
	if weight > 0 {
		s.Value = total / weight
		s.Coverage = weight * 100 / 100 // full set weighs 100
		if s.Coverage > 100 {
			s.Coverage = 100
		}
	}
	if s.Coverage < 60 {
		s.Grade = "partial"
		return s
	}
	switch {
	case s.Value >= 90:
		s.Grade = "healthy"
	case s.Value >= 70:
		s.Grade = "attention"
	case s.Value >= 40:
		s.Grade = "degraded"
	default:
		s.Grade = "critical"
	}
	return s
}
