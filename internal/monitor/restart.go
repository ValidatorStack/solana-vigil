package monitor

import (
	"context"
	"fmt"
	"sort"
	"time"
)

type RestartWindow struct {
	Identity              string         `json:"identity"`
	Epoch                 uint64         `json:"epoch"`
	ObservedSlot          uint64         `json:"observed_slot"`
	WindowStartSlot       uint64         `json:"window_start_slot"`
	WindowEndSlot         uint64         `json:"window_end_slot"`
	FreeSlots             uint64         `json:"free_slots"`
	SlotsUntilWindow      uint64         `json:"slots_until_window"`
	NextLeaderSlot        *uint64        `json:"next_leader_slot,omitempty"`
	EstimatedStartIn      *time.Duration `json:"estimated_start_in,omitempty"`
	AverageSlotsPerSecond *float64       `json:"average_slots_per_second,omitempty"`
	Warning               string         `json:"warning"`
}

// PlanRestart finds a leader-free window. It never restarts a process and the
// result is advisory: schedules and cluster conditions can change.
func PlanRestart(ctx context.Context, monitor *Monitor, identity string, minimumFreeSlots, searchSlots uint64) (RestartWindow, error) {
	if minimumFreeSlots == 0 || searchSlots < minimumFreeSlots {
		return RestartWindow{}, fmt.Errorf("require 1 <= minimum free slots <= search slots")
	}
	client := monitor.Local()
	commitment := monitor.cfg.RPC.Commitment
	if identity == "" {
		observed, err := client.GetIdentity(ctx)
		if err != nil {
			return RestartWindow{}, err
		}
		identity = observed.Identity
	}
	slot, err := client.GetSlot(ctx, commitment)
	if err != nil {
		return RestartWindow{}, err
	}
	epoch, err := client.GetEpochInfo(ctx, commitment)
	if err != nil {
		return RestartWindow{}, err
	}
	schedule, err := client.GetLeaderSchedule(ctx, identity, commitment)
	if err != nil {
		return RestartWindow{}, err
	}
	firstSlot := epoch.EpochFirstSlot()
	epochEnd := firstSlot + epoch.SlotsInEpoch - 1
	searchEnd := slot + searchSlots
	if searchEnd > epochEnd {
		searchEnd = epochEnd
	}
	var leaders []uint64
	for _, index := range schedule[identity] {
		absolute := firstSlot + index
		if absolute >= slot+1 && absolute <= searchEnd {
			leaders = append(leaders, absolute)
		}
	}
	sort.Slice(leaders, func(i, j int) bool { return leaders[i] < leaders[j] })
	candidate := slot + 1
	found := false
	for _, leader := range leaders {
		if leader >= candidate && leader-candidate >= minimumFreeSlots {
			found = true
			break
		}
		candidate = leader + 1
	}
	if !found && candidate+minimumFreeSlots-1 <= searchEnd {
		found = true
	}
	if !found {
		return RestartWindow{}, fmt.Errorf("no %d-slot leader-free window in the next %d slots of epoch %d", minimumFreeSlots, searchSlots, epoch.Epoch)
	}
	window := RestartWindow{
		Identity: identity, Epoch: epoch.Epoch, ObservedSlot: slot,
		WindowStartSlot: candidate, WindowEndSlot: candidate + minimumFreeSlots - 1,
		FreeSlots: minimumFreeSlots, SlotsUntilWindow: candidate - slot,
		Warning: "Advisory only. Re-check immediately before maintenance; Vigil never restarts or switches a validator.",
	}
	if len(leaders) > 0 {
		window.NextLeaderSlot = uint64Pointer(leaders[0])
	}
	samples, sampleErr := client.GetRecentPerformanceSamples(ctx, 12)
	if sampleErr == nil {
		var slots, seconds uint64
		for _, sample := range samples {
			slots += sample.NumSlots
			seconds += sample.SamplePeriodSecs
		}
		if slots > 0 && seconds > 0 {
			rate := float64(slots) / float64(seconds)
			estimate := time.Duration(float64(window.SlotsUntilWindow) / rate * float64(time.Second))
			window.AverageSlotsPerSecond = &rate
			window.EstimatedStartIn = &estimate
		}
	}
	return window, nil
}
