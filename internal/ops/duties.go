package ops

import (
	"context"
	"fmt"

	"github.com/ValidatorStack/solana-vigil/internal/rpc"
)

// DutyRow is one epoch's leader performance for a validator.
type DutyRow struct {
	Epoch        uint64  `json:"epoch"`
	LeaderSlots  uint64  `json:"leader_slots"`
	Blocks       uint64  `json:"blocks"`
	Missed       uint64  `json:"missed"`
	SkipRatePct  float64 `json:"skip_rate_pct"`
}

// Rewards summarises inflation rewards for the vote account.
type RewardRow struct {
	Epoch      uint64  `json:"epoch"`
	Lamports   int64   `json:"lamports"`
	Commission *uint8  `json:"commission,omitempty"`
	Effective  float64 `json:"effective_slot,omitempty"`
}

type inflationReward struct {
	Epoch      uint64  `json:"epoch"`
	Lamports   int64   `json:"lamports"`
	Amount     uint64  `json:"amount"`
	Commission *uint8  `json:"commission,omitempty"`
	Effective  float64 `json:"effectiveSlot,omitempty"`
}

// Duties asks the cluster for the vote account's recent leader performance.
// It uses getBlockProduction, which reports produced blocks per leader slot.
func Duties(ctx context.Context, c *rpc.Client, voteAccount string, epochInfo *rpc.EpochInfo) (DutyRow, error) {
	if epochInfo == nil {
		var ei rpc.EpochInfo
		if err := c.Call(ctx, "getEpochInfo", []any{}, &ei); err != nil {
			return DutyRow{}, err
		}
		epochInfo = &ei
	}
	var prod struct {
		Range struct {
			FirstSlot uint64 `json:"firstSlot"`
			LastSlot  uint64 `json:"lastSlot"`
		} `json:"range"`
		ByIdentity map[string][]uint64 `json:"byIdentity"`
	}
	params := map[string]any{
		"commitment": "confirmed",
		"range":      map[string]any{"firstSlot": epochInfo.AbsoluteSlot - 432000, "lastSlot": epochInfo.AbsoluteSlot},
	}
	if err := c.Call(ctx, "getBlockProduction", []any{params}, &prod); err != nil {
		return DutyRow{}, err
	}
	for _, v := range prod.ByIdentity {
		if len(v) != 2 {
			continue
		}
		row := DutyRow{Epoch: epochInfo.Epoch, LeaderSlots: v[0], Blocks: v[1]}
		if row.LeaderSlots > row.Blocks {
			row.Missed = row.LeaderSlots - row.Blocks
		}
		if row.LeaderSlots > 0 {
			row.SkipRatePct = float64(row.Missed) / float64(row.LeaderSlots) * 100
		}
		return row, nil
	}
	return DutyRow{Epoch: epochInfo.Epoch}, fmt.Errorf("vote account not present in block production data")
}

// Rewards fetches inflation rewards for the last n epochs.
func Rewards(ctx context.Context, c *rpc.Client, voteAccount string, epochs uint64) ([]RewardRow, error) {
	var ei rpc.EpochInfo
	if err := c.Call(ctx, "getEpochInfo", []any{}, &ei); err != nil {
		return nil, err
	}
	var out []RewardRow
	for e := uint64(1); e <= epochs && e <= ei.Epoch; e++ {
		var r inflationReward
		params := []any{voteAccount, map[string]any{"epoch": ei.Epoch - e, "commitment": "confirmed"}}
		if err := c.Call(ctx, "getInflationReward", params, &r); err != nil {
			continue
		}
		out = append(out, RewardRow{
			Epoch: ei.Epoch - e, Lamports: r.Lamports, Commission: r.Commission, Effective: r.Effective,
		})
	}
	return out, nil
}
