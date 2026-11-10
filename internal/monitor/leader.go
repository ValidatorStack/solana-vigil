package monitor

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/ValidatorStack/solana-vigil/internal/config"
	"github.com/ValidatorStack/solana-vigil/internal/rpc"
)

type leaderTracker struct {
	mu            sync.Mutex
	epoch         uint64
	identity      string
	absoluteSlots []uint64
	initialized   bool
	lastEvaluated uint64
}

func (t *leaderTracker) track(ctx context.Context, client *rpc.Client, cfg config.Config, identity string, observedSlot uint64, epoch rpc.EpochInfo) ([]uint64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	depth := cfg.Monitor.LeaderConfirmationDepth
	if observedSlot <= depth {
		return nil, nil
	}
	target := observedSlot - depth
	if t.epoch != epoch.Epoch || t.identity != identity {
		schedule, err := client.GetLeaderSchedule(ctx, identity, cfg.RPC.Commitment)
		if err != nil {
			return nil, fmt.Errorf("leader schedule: %w", err)
		}
		indices := schedule[identity]
		firstSlot := epoch.EpochFirstSlot()
		t.absoluteSlots = t.absoluteSlots[:0]
		for _, index := range indices {
			if index < epoch.SlotsInEpoch {
				t.absoluteSlots = append(t.absoluteSlots, firstSlot+index)
			}
		}
		sort.Slice(t.absoluteSlots, func(i, j int) bool { return t.absoluteSlots[i] < t.absoluteSlots[j] })
		t.epoch, t.identity = epoch.Epoch, identity
		t.initialized = false
	}
	if !t.initialized {
		t.lastEvaluated = target
		t.initialized = true
		return nil, nil
	}
	if target <= t.lastEvaluated {
		return nil, nil
	}
	start := t.lastEvaluated + 1
	// getBlocks ranges are bounded. On a long stall, evaluate the newest window
	// and explicitly skip older history instead of emitting an alert storm.
	if target-start >= 5000 {
		start = target - 4999
	}
	produced, err := client.GetBlocks(ctx, start, target, cfg.RPC.Commitment)
	if err != nil {
		return nil, fmt.Errorf("confirmed blocks %d-%d: %w", start, target, err)
	}
	present := make(map[uint64]struct{}, len(produced))
	for _, slot := range produced {
		present[slot] = struct{}{}
	}
	var missed []uint64
	for _, slot := range t.absoluteSlots {
		if slot < start {
			continue
		}
		if slot > target {
			break
		}
		if _, ok := present[slot]; !ok {
			missed = append(missed, slot)
		}
	}
	t.lastEvaluated = target
	return missed, nil
}
