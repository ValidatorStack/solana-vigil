// Package audit collects read-only Linux host evidence for validator operators.
package audit

import (
	"time"
)

type Status string

const (
	Pass    Status = "pass"
	Warn    Status = "warn"
	Fail    Status = "fail"
	Unknown Status = "unknown"
	Info    Status = "info"
)

type Finding struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Status         Status `json:"status"`
	Observed       string `json:"observed,omitempty"`
	Expected       string `json:"expected,omitempty"`
	Recommendation string `json:"recommendation,omitempty"`
}

type Report struct {
	Timestamp    time.Time `json:"timestamp"`
	Hostname     string    `json:"hostname,omitempty"`
	OS           string    `json:"os"`
	Architecture string    `json:"architecture"`
	Client       string    `json:"client"`
	PID          int       `json:"pid,omitempty"`
	Findings     []Finding `json:"findings"`
}

type Drift struct {
	ID     string  `json:"id"`
	Before Finding `json:"before"`
	After  Finding `json:"after"`
}

func (r Report) ExitCode() int {
	result := 0
	for _, finding := range r.Findings {
		if finding.Status == Fail {
			return 2
		}
		if (finding.Status == Warn || finding.Status == Unknown) && result == 0 {
			result = 1
		}
	}
	return result
}

func Compare(before, after Report) []Drift {
	previous := make(map[string]Finding, len(before.Findings))
	for _, finding := range before.Findings {
		previous[finding.ID] = finding
	}
	var drift []Drift
	for _, current := range after.Findings {
		old, ok := previous[current.ID]
		if ok && (old.Status != current.Status || old.Observed != current.Observed) {
			drift = append(drift, Drift{ID: current.ID, Before: old, After: current})
		}
	}
	return drift
}
