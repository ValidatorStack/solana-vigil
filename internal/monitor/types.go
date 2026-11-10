package monitor

import (
	"sort"
	"time"

	"github.com/ValidatorStack/solana-vigil/internal/rpc"
)

type Status string

const (
	OK       Status = "ok"
	Warning  Status = "warning"
	Critical Status = "critical"
	Unknown  Status = "unknown"
)

func (s Status) ExitCode() int {
	switch s {
	case Warning:
		return 1
	case Critical:
		return 2
	case Unknown:
		return 3
	default:
		return 0
	}
}

type Check struct {
	ID         string    `json:"id"`
	Status     Status    `json:"status"`
	Summary    string    `json:"summary"`
	Detail     string    `json:"detail,omitempty"`
	ObservedAt time.Time `json:"observed_at"`
	// Transient checks are one-shot duty events. They alert immediately and do
	// not create persistent flap state.
	Transient bool `json:"transient,omitempty"`
}

type VATObservation struct {
	BalanceLamports     *uint64  `json:"balance_lamports,omitempty"`
	RentExemptLamports  *uint64  `json:"rent_exempt_lamports,omitempty"`
	SpendableLamports   *uint64  `json:"spendable_lamports,omitempty"`
	FeeLamportsPerEpoch uint64   `json:"fee_lamports_per_epoch"`
	RunwayEpochs        *float64 `json:"runway_epochs,omitempty"`
	SufficientNextEpoch *bool    `json:"sufficient_next_epoch,omitempty"`
}

type LeaderStats struct {
	Available       bool    `json:"available"`
	LeaderSlots     uint64  `json:"leader_slots"`
	BlocksProduced  uint64  `json:"blocks_produced"`
	MissedSlots     uint64  `json:"missed_slots"`
	SkipRatePercent float64 `json:"skip_rate_percent"`
	RangeFirstSlot  uint64  `json:"range_first_slot"`
	RangeLastSlot   *uint64 `json:"range_last_slot,omitempty"`
}

type Snapshot struct {
	Timestamp         time.Time        `json:"timestamp"`
	VoteAccount       string           `json:"vote_account"`
	Identity          string           `json:"identity,omitempty"`
	GenesisHash       string           `json:"genesis_hash,omitempty"`
	ClientVersion     string           `json:"client_version,omitempty"`
	Slot              *uint64          `json:"slot,omitempty"`
	ReferenceSlot     *uint64          `json:"reference_slot,omitempty"`
	Epoch             *rpc.EpochInfo   `json:"epoch,omitempty"`
	Vote              *rpc.VoteAccount `json:"vote,omitempty"`
	Delinquent        *bool            `json:"delinquent,omitempty"`
	VAT               VATObservation   `json:"vat"`
	BLSState          string           `json:"bls_state"`
	Leader            LeaderStats      `json:"leader"`
	MissedLeaderSlots []uint64         `json:"missed_leader_slots,omitempty"`
	Checks            []Check          `json:"checks"`
}

func (s Snapshot) OverallStatus() Status {
	overall := OK
	for _, check := range s.Checks {
		switch check.Status {
		case Critical:
			return Critical
		case Warning:
			overall = Warning
		case Unknown:
			if overall == OK {
				overall = Unknown
			}
		}
	}
	return overall
}

func (s *Snapshot) SortChecks() {
	sort.Slice(s.Checks, func(i, j int) bool { return s.Checks[i].ID < s.Checks[j].ID })
}
