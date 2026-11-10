package metrics

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ValidatorStack/solana-vigil/internal/buildinfo"
	"github.com/ValidatorStack/solana-vigil/internal/monitor"
)

type renderer struct{ strings.Builder }

func (r *renderer) help(name, help, kind string) {
	fmt.Fprintf(r, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
}
func (r *renderer) value(name string, labels map[string]string, value any) {
	r.WriteString(name)
	if len(labels) > 0 {
		keys := make([]string, 0, len(labels))
		for key := range labels {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		r.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				r.WriteByte(',')
			}
			fmt.Fprintf(r, "%s=%s", key, strconv.Quote(labels[key]))
		}
		r.WriteByte('}')
	}
	fmt.Fprintf(r, " %v\n", value)
}

func Render(snapshot monitor.Snapshot, scrapeSuccess bool, scrapeDuration time.Duration) string {
	r := &renderer{}
	r.help("vigil_build_info", "Vigil release information.", "gauge")
	r.value("vigil_build_info", map[string]string{"version": buildinfo.Version, "commit": buildinfo.Commit}, 1)
	r.help("vigil_scrape_success", "Whether this snapshot completed before its context was cancelled.", "gauge")
	r.value("vigil_scrape_success", nil, boolNumber(scrapeSuccess))
	r.help("vigil_scrape_duration_seconds", "Time spent rendering and collecting this snapshot.", "gauge")
	r.value("vigil_scrape_duration_seconds", nil, scrapeDuration.Seconds())
	r.help("vigil_snapshot_timestamp_seconds", "UTC timestamp of the latest validator observation.", "gauge")
	r.value("vigil_snapshot_timestamp_seconds", nil, snapshot.Timestamp.Unix())
	r.help("vigil_validator_info", "Static validator and client labels; value is always 1.", "gauge")
	r.value("vigil_validator_info", map[string]string{"identity": snapshot.Identity, "vote_account": snapshot.VoteAccount, "client_version": snapshot.ClientVersion, "genesis_hash": snapshot.GenesisHash}, 1)

	r.help("vigil_check_status", "Current check status: 0=ok, 1=warning, 2=critical, 3=unknown.", "gauge")
	counts := map[monitor.Status]int{monitor.OK: 0, monitor.Warning: 0, monitor.Critical: 0, monitor.Unknown: 0}
	for _, check := range snapshot.Checks {
		if check.Transient {
			continue
		}
		counts[check.Status]++
		r.value("vigil_check_status", map[string]string{"check": check.ID}, statusNumber(check.Status))
	}
	r.help("vigil_checks", "Number of current non-transient checks by status.", "gauge")
	for _, status := range []monitor.Status{monitor.OK, monitor.Warning, monitor.Critical, monitor.Unknown} {
		r.value("vigil_checks", map[string]string{"status": string(status)}, counts[status])
	}

	r.help("solana_slot", "Latest local RPC slot.", "gauge")
	if snapshot.Slot != nil {
		r.value("solana_slot", nil, *snapshot.Slot)
	}
	r.help("solana_reference_slot", "Latest reference RPC slot.", "gauge")
	if snapshot.ReferenceSlot != nil {
		r.value("solana_reference_slot", nil, *snapshot.ReferenceSlot)
	}
	r.help("solana_slot_lag", "Non-negative reference-minus-local slot lag.", "gauge")
	if snapshot.Slot != nil && snapshot.ReferenceSlot != nil {
		lag := int64(0)
		if *snapshot.ReferenceSlot > *snapshot.Slot {
			lag = int64(*snapshot.ReferenceSlot - *snapshot.Slot)
		}
		r.value("solana_slot_lag", nil, lag)
	}
	if snapshot.Epoch != nil {
		r.help("solana_epoch", "Current Solana epoch.", "gauge")
		r.value("solana_epoch", nil, snapshot.Epoch.Epoch)
		r.help("solana_epoch_slot_index", "Current slot index within the epoch.", "gauge")
		r.value("solana_epoch_slot_index", nil, snapshot.Epoch.SlotIndex)
		r.help("solana_epoch_progress_ratio", "Epoch progress from 0 to 1.", "gauge")
		if snapshot.Epoch.SlotsInEpoch > 0 {
			r.value("solana_epoch_progress_ratio", nil, float64(snapshot.Epoch.SlotIndex)/float64(snapshot.Epoch.SlotsInEpoch))
		}
	}
	if snapshot.Vote != nil {
		r.help("solana_activated_stake_sol", "Activated delegated stake in SOL.", "gauge")
		r.value("solana_activated_stake_sol", nil, float64(snapshot.Vote.ActivatedStake)/1e9)
		r.help("solana_commission_ratio", "Vote account commission from 0 to 1.", "gauge")
		r.value("solana_commission_ratio", nil, float64(snapshot.Vote.Commission)/100)
		r.help("solana_last_vote_slot", "Last vote slot reported by getVoteAccounts.", "gauge")
		r.value("solana_last_vote_slot", nil, snapshot.Vote.LastVote)
		r.help("solana_root_slot", "Vote account root slot.", "gauge")
		r.value("solana_root_slot", nil, snapshot.Vote.RootSlot)
	}
	r.help("solana_delinquent", "Whether the vote account is delinquent (1=yes, 0=no).", "gauge")
	if snapshot.Delinquent != nil {
		r.value("solana_delinquent", nil, boolNumber(*snapshot.Delinquent))
	}

	r.help("solana_vote_account_balance_sol", "Total vote account balance in SOL.", "gauge")
	if snapshot.VAT.BalanceLamports != nil {
		r.value("solana_vote_account_balance_sol", nil, float64(*snapshot.VAT.BalanceLamports)/1e9)
	}
	r.help("solana_vote_account_rent_exempt_minimum_sol", "Rent-exempt minimum for the vote account in SOL.", "gauge")
	if snapshot.VAT.RentExemptLamports != nil {
		r.value("solana_vote_account_rent_exempt_minimum_sol", nil, float64(*snapshot.VAT.RentExemptLamports)/1e9)
	}
	r.help("solana_vat_spendable_sol", "Vote account balance above rent in SOL.", "gauge")
	if snapshot.VAT.SpendableLamports != nil {
		r.value("solana_vat_spendable_sol", nil, float64(*snapshot.VAT.SpendableLamports)/1e9)
	}
	r.help("solana_vat_lamports_per_epoch", "Configured Validator Admission Ticket fee in lamports.", "gauge")
	r.value("solana_vat_lamports_per_epoch", nil, snapshot.VAT.FeeLamportsPerEpoch)
	r.help("solana_vat_runway_epochs", "Continuous VAT runway above rent, in epochs.", "gauge")
	if snapshot.VAT.RunwayEpochs != nil {
		r.value("solana_vat_runway_epochs", nil, *snapshot.VAT.RunwayEpochs)
	}
	r.help("solana_vat_next_epoch_sufficient", "Whether above-rent balance covers the next epoch VAT.", "gauge")
	if snapshot.VAT.SufficientNextEpoch != nil {
		r.value("solana_vat_next_epoch_sufficient", nil, boolNumber(*snapshot.VAT.SufficientNextEpoch))
	}
	r.help("solana_bls_registered", "BLS registration: 1=registered, 0=missing or malformed; omitted if unknown.", "gauge")
	if snapshot.BLSState != "unknown" && snapshot.BLSState != "" {
		r.value("solana_bls_registered", nil, boolNumber(snapshot.BLSState == "registered"))
	}
	r.help("vigil_observation_available", "Whether a specific observation is available (1=yes, 0=unknown).", "gauge")
	r.value("vigil_observation_available", map[string]string{"observation": "bls"}, boolNumber(snapshot.BLSState != "unknown" && snapshot.BLSState != ""))

	if snapshot.Leader.Available {
		r.help("solana_leader_slots", "Leader slots in the current block-production range.", "gauge")
		r.value("solana_leader_slots", nil, snapshot.Leader.LeaderSlots)
		r.help("solana_blocks_produced", "Blocks produced in the current block-production range.", "gauge")
		r.value("solana_blocks_produced", nil, snapshot.Leader.BlocksProduced)
		r.help("solana_leader_missed_slots", "Leader-minus-produced slots in the current range.", "gauge")
		r.value("solana_leader_missed_slots", nil, snapshot.Leader.MissedSlots)
		r.help("solana_leader_skip_ratio", "Leader skip ratio in the current range.", "gauge")
		r.value("solana_leader_skip_ratio", nil, snapshot.Leader.SkipRatePercent/100)
	}
	return r.String()
}

func boolNumber(value bool) int {
	if value {
		return 1
	}
	return 0
}
func statusNumber(status monitor.Status) int {
	switch status {
	case monitor.Warning:
		return 1
	case monitor.Critical:
		return 2
	case monitor.Unknown:
		return 3
	default:
		return 0
	}
}
