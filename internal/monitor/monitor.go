// Package monitor collects validator evidence and turns it into explicit,
// actionable checks without hiding missing or inconsistent observations.
package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/ValidatorStack/solana-vigil/internal/base58"
	"github.com/ValidatorStack/solana-vigil/internal/config"
	"github.com/ValidatorStack/solana-vigil/internal/rpc"
)

type Monitor struct {
	cfg         config.Config
	local       *rpc.Client
	reference   *rpc.Client
	logger      *slog.Logger
	genesisHash string
	trackLeader bool
	leader      leaderTracker
}

func New(cfg config.Config, local, reference *rpc.Client, logger *slog.Logger, trackLeader bool) *Monitor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Monitor{cfg: cfg, local: local, reference: reference, logger: logger, trackLeader: trackLeader}
}

func (m *Monitor) ValidateCluster(ctx context.Context) error {
	hash, err := rpc.ValidateSameCluster(ctx, m.local, m.reference)
	if err != nil {
		return err
	}
	m.genesisHash = hash
	return nil
}

func (m *Monitor) Local() *rpc.Client { return m.local }

type localResults struct {
	identity    rpc.Identity
	identityErr error
	healthErr   error
	slot        uint64
	slotErr     error
	epoch       rpc.EpochInfo
	epochErr    error
	version     rpc.Version
	versionErr  error
	vote        rpc.VoteAccount
	delinquent  bool
	found       bool
	voteErr     error
	balance     uint64
	balanceErr  error
	rent        uint64
	rentErr     error
}

func (m *Monitor) Collect(ctx context.Context) Snapshot {
	now := time.Now().UTC()
	snapshot := Snapshot{
		Timestamp:   now,
		VoteAccount: m.cfg.Validator.VoteAccount,
		GenesisHash: m.genesisHash,
		BLSState:    "unknown",
		VAT: VATObservation{
			FeeLamportsPerEpoch: m.cfg.Monitor.VATLamportsPerEpoch,
		},
	}
	var results localResults
	var referenceSlot uint64
	var referenceErr error
	var wait sync.WaitGroup
	tasks := []func(){
		func() { results.identity, results.identityErr = m.local.GetIdentity(ctx) },
		func() { results.healthErr = m.local.GetHealth(ctx) },
		func() { results.slot, results.slotErr = m.local.GetSlot(ctx, m.cfg.RPC.Commitment) },
		func() { results.epoch, results.epochErr = m.local.GetEpochInfo(ctx, m.cfg.RPC.Commitment) },
		func() { results.version, results.versionErr = m.local.GetVersion(ctx) },
		func() {
			results.vote, results.delinquent, results.found, results.voteErr = m.local.GetVoteAccount(ctx, m.cfg.Validator.VoteAccount, m.cfg.RPC.Commitment)
		},
		func() {
			balance, err := m.local.GetBalance(ctx, m.cfg.Validator.VoteAccount, m.cfg.RPC.Commitment)
			results.balance, results.balanceErr = balance.Value, err
		},
		func() {
			space, err := m.local.GetAccountSpace(ctx, m.cfg.Validator.VoteAccount, m.cfg.RPC.Commitment)
			if err != nil {
				results.rentErr = err
				return
			}
			results.rent, results.rentErr = m.local.GetMinimumBalanceForRentExemption(ctx, space)
		},
	}
	if m.reference != nil {
		tasks = append(tasks, func() { referenceSlot, referenceErr = m.reference.GetSlot(ctx, m.cfg.RPC.Commitment) })
	}
	wait.Add(len(tasks))
	for _, task := range tasks {
		go func() {
			defer wait.Done()
			task()
		}()
	}
	wait.Wait()

	snapshot.Identity = firstNonEmpty(m.cfg.Validator.Identity, results.identity.Identity, results.vote.NodePubkey)
	if results.slotErr == nil {
		snapshot.Slot = uint64Pointer(results.slot)
	}
	if referenceErr == nil && m.reference != nil {
		snapshot.ReferenceSlot = uint64Pointer(referenceSlot)
	}
	if results.epochErr == nil {
		epoch := results.epoch
		snapshot.Epoch = &epoch
	}
	if results.versionErr == nil {
		snapshot.ClientVersion = results.version.SolanaCore
	}
	if results.voteErr == nil && results.found {
		vote := results.vote
		snapshot.Vote = &vote
		snapshot.Delinquent = boolPointer(results.delinquent)
	}
	if results.balanceErr == nil {
		snapshot.VAT.BalanceLamports = uint64Pointer(results.balance)
	}
	if results.rentErr == nil {
		snapshot.VAT.RentExemptLamports = uint64Pointer(results.rent)
	}

	m.addLocalChecks(&snapshot, results, now)
	m.addReferenceCheck(&snapshot, referenceErr, now)
	m.addVoteChecks(&snapshot, results, now)
	m.addVATCheck(&snapshot, results, now)
	m.addBLSCheck(&snapshot, results, now)

	if snapshot.Identity != "" {
		production, err := m.local.GetBlockProduction(ctx, snapshot.Identity, m.cfg.RPC.Commitment)
		if err != nil {
			snapshot.Checks = append(snapshot.Checks, check("leader.production", Unknown, "Leader production unavailable", err.Error(), now))
		} else {
			m.addProductionCheck(&snapshot, production, now)
		}
		if m.trackLeader && results.slotErr == nil && results.epochErr == nil {
			missed, err := m.leader.track(ctx, m.local, m.cfg, snapshot.Identity, results.slot, results.epoch)
			snapshot.MissedLeaderSlots = missed
			if err != nil {
				snapshot.Checks = append(snapshot.Checks, check("leader.tracking", Unknown, "Per-slot leader tracking unavailable", err.Error(), now))
			} else {
				snapshot.Checks = append(snapshot.Checks, check("leader.tracking", OK, "Per-slot leader tracking healthy", fmt.Sprintf("Evaluated through confirmation depth %d", m.cfg.Monitor.LeaderConfirmationDepth), now))
				if m.cfg.Monitor.AlertOnMissedLeaderSlot {
					for _, slot := range missed {
						snapshot.Checks = append(snapshot.Checks, Check{
							ID: fmt.Sprintf("leader.slot.%d", slot), Status: Critical,
							Summary:    fmt.Sprintf("Scheduled leader slot %d was missed", slot),
							Detail:     fmt.Sprintf("The slot was assigned to %s and was absent after %d confirmation slots", snapshot.Identity, m.cfg.Monitor.LeaderConfirmationDepth),
							ObservedAt: now, Transient: true,
						})
					}
				}
			}
		}
	}
	snapshot.SortChecks()
	return snapshot
}

func (m *Monitor) addLocalChecks(snapshot *Snapshot, results localResults, now time.Time) {
	if results.identityErr != nil {
		snapshot.Checks = append(snapshot.Checks, check("rpc.local.identity", Critical, "Local validator identity unavailable", results.identityErr.Error(), now))
	} else {
		snapshot.Checks = append(snapshot.Checks, check("rpc.local.identity", OK, "Local validator identity observed", results.identity.Identity, now))
	}
	if results.healthErr != nil {
		snapshot.Checks = append(snapshot.Checks, check("validator.health", Critical, "Local validator RPC is unhealthy", results.healthErr.Error(), now))
	} else {
		snapshot.Checks = append(snapshot.Checks, check("validator.health", OK, "Local validator RPC reports healthy", "getHealth returned ok", now))
	}
	if results.slotErr != nil {
		snapshot.Checks = append(snapshot.Checks, check("rpc.local.slot", Critical, "Local slot unavailable", results.slotErr.Error(), now))
	}
	if results.epochErr != nil {
		snapshot.Checks = append(snapshot.Checks, check("rpc.local.epoch", Warning, "Epoch information unavailable", results.epochErr.Error(), now))
	}
	if results.versionErr != nil {
		snapshot.Checks = append(snapshot.Checks, check("validator.version", Unknown, "Client version unavailable", results.versionErr.Error(), now))
	}
}

func (m *Monitor) addReferenceCheck(snapshot *Snapshot, referenceErr error, now time.Time) {
	if m.reference == nil {
		return
	}
	if referenceErr != nil {
		snapshot.Checks = append(snapshot.Checks, check("rpc.reference", Warning, "Reference RPC unavailable", referenceErr.Error(), now))
		return
	}
	snapshot.Checks = append(snapshot.Checks, check("rpc.reference", OK, "Reference RPC reachable", "Genesis hash verified at startup", now))
	if snapshot.Slot == nil || snapshot.ReferenceSlot == nil {
		return
	}
	local, reference := *snapshot.Slot, *snapshot.ReferenceSlot
	if reference < local {
		return
	}
	lag := reference - local
	status, summary := OK, "Local RPC is within the slot-lag threshold"
	if lag >= m.cfg.Monitor.SlotLagCritical {
		status, summary = Critical, "Local RPC is critically behind the reference"
	} else if lag >= m.cfg.Monitor.SlotLagWarning {
		status, summary = Warning, "Local RPC is behind the reference"
	}
	snapshot.Checks = append(snapshot.Checks, check("rpc.slot_lag", status, summary, fmt.Sprintf("lag=%d slots, warning=%d, critical=%d", lag, m.cfg.Monitor.SlotLagWarning, m.cfg.Monitor.SlotLagCritical), now))
}

func (m *Monitor) addVoteChecks(snapshot *Snapshot, results localResults, now time.Time) {
	if results.voteErr != nil {
		snapshot.Checks = append(snapshot.Checks, check("validator.vote_account", Critical, "Vote account state unavailable", results.voteErr.Error(), now))
		return
	}
	if !results.found {
		snapshot.Checks = append(snapshot.Checks, check("validator.vote_account", Critical, "Vote account not found", "Verify the public vote address and RPC commitment", now))
		return
	}
	snapshot.Checks = append(snapshot.Checks, check("validator.vote_account", OK, "Vote account observed", results.vote.VotePubkey, now))
	if results.delinquent {
		snapshot.Checks = append(snapshot.Checks, check("validator.delinquency", Critical, "Validator is delinquent", "The local RPC placed the vote account in its delinquent set", now))
	} else {
		snapshot.Checks = append(snapshot.Checks, check("validator.delinquency", OK, "Validator is not delinquent", fmt.Sprintf("last vote slot %d", results.vote.LastVote), now))
	}
	if m.cfg.Validator.Identity != "" && results.vote.NodePubkey != m.cfg.Validator.Identity {
		snapshot.Checks = append(snapshot.Checks, check("validator.identity", Critical, "Configured identity does not own the vote account", fmt.Sprintf("configured=%s vote_account_node=%s", m.cfg.Validator.Identity, results.vote.NodePubkey), now))
	} else if results.identityErr == nil && results.identity.Identity != results.vote.NodePubkey {
		snapshot.Checks = append(snapshot.Checks, check("validator.identity", Critical, "Local RPC identity does not own the vote account", fmt.Sprintf("local_identity=%s vote_account_node=%s", results.identity.Identity, results.vote.NodePubkey), now))
	} else {
		snapshot.Checks = append(snapshot.Checks, check("validator.identity", OK, "Identity and vote account match", results.vote.NodePubkey, now))
	}
	if results.vote.ActivatedStake == 0 || !results.vote.EpochVoteAccount {
		snapshot.Checks = append(snapshot.Checks, check("validator.stake", Warning, "Vote account has no active stake this epoch", fmt.Sprintf("activated_stake=%d epoch_vote_account=%t", results.vote.ActivatedStake, results.vote.EpochVoteAccount), now))
	} else {
		snapshot.Checks = append(snapshot.Checks, check("validator.stake", OK, "Vote account is staked", fmt.Sprintf("activated_stake=%d lamports", results.vote.ActivatedStake), now))
	}
	if snapshot.Slot != nil {
		if *snapshot.Slot < results.vote.LastVote {
			snapshot.Checks = append(snapshot.Checks, check("validator.vote_lag", Unknown, "Vote and slot observations are inconsistent", "Last vote is ahead of the observed local slot", now))
		} else {
			lag := *snapshot.Slot - results.vote.LastVote
			status := OK
			if lag >= m.cfg.Monitor.VoteLagCriticalSlots {
				status = Critical
			} else if lag >= m.cfg.Monitor.VoteLagWarningSlots {
				status = Warning
			}
			snapshot.Checks = append(snapshot.Checks, check("validator.vote_lag", status, fmt.Sprintf("Vote lag is %d slots", lag), fmt.Sprintf("warning=%d critical=%d", m.cfg.Monitor.VoteLagWarningSlots, m.cfg.Monitor.VoteLagCriticalSlots), now))
		}
	}
}

func (m *Monitor) addVATCheck(snapshot *Snapshot, results localResults, now time.Time) {
	if m.cfg.Monitor.VATLamportsPerEpoch == 0 {
		snapshot.Checks = append(snapshot.Checks, check("alpenglow.vat", OK, "VAT monitoring is disabled", "Set vat_lamports_per_epoch to enable it", now))
		return
	}
	if results.balanceErr != nil || results.rentErr != nil {
		detail := ""
		if results.balanceErr != nil {
			detail = "balance: " + results.balanceErr.Error()
		}
		if results.rentErr != nil {
			detail += " rent: " + results.rentErr.Error()
		}
		snapshot.Checks = append(snapshot.Checks, check("alpenglow.vat", Unknown, "VAT runway cannot be calculated", detail, now))
		return
	}
	spendable := uint64(0)
	if results.balance > results.rent {
		spendable = results.balance - results.rent
	}
	runway := float64(spendable) / float64(m.cfg.Monitor.VATLamportsPerEpoch)
	sufficient := spendable >= m.cfg.Monitor.VATLamportsPerEpoch
	snapshot.VAT.SpendableLamports = uint64Pointer(spendable)
	snapshot.VAT.RunwayEpochs = floatPointer(runway)
	snapshot.VAT.SufficientNextEpoch = boolPointer(sufficient)
	status, summary := OK, "Vote account can fund upcoming VAT payments"
	if !sufficient || runway < m.cfg.Monitor.VATCriticalEpochs {
		status, summary = Critical, "Vote account cannot safely fund the next epoch VAT"
	} else if runway < m.cfg.Monitor.VATWarningEpochs {
		status, summary = Warning, "Vote account VAT runway is low"
	}
	detail := fmt.Sprintf("balance=%.9f SOL rent=%.9f SOL spendable=%.9f SOL runway=%.3f epochs fee=%.9f SOL/epoch", sol(results.balance), sol(results.rent), sol(spendable), runway, sol(m.cfg.Monitor.VATLamportsPerEpoch))
	snapshot.Checks = append(snapshot.Checks, check("alpenglow.vat", status, summary, detail, now))
}

func (m *Monitor) addBLSCheck(snapshot *Snapshot, results localResults, now time.Time) {
	if !m.cfg.Monitor.RequireBLS {
		return
	}
	if results.voteErr != nil || !results.found {
		snapshot.Checks = append(snapshot.Checks, check("alpenglow.bls", Unknown, "BLS registration cannot be determined", "Vote account observation unavailable", now))
		return
	}
	bls := results.vote.BLSPubkey
	switch {
	case !bls.Present:
		snapshot.Checks = append(snapshot.Checks, check("alpenglow.bls", Unknown, "RPC did not expose BLS registration", "blsPubkeyCompressed was absent; do not treat an old RPC schema as proof of a missing key", now))
	case bls.Null || bls.Value == "":
		snapshot.BLSState = "missing"
		snapshot.Checks = append(snapshot.Checks, check("alpenglow.bls", Critical, "Vote account has no registered BLS key", "blsPubkeyCompressed is null or empty", now))
	default:
		decoded, err := base58.Decode(bls.Value)
		if err != nil || len(decoded) != 48 {
			snapshot.BLSState = "invalid"
			snapshot.Checks = append(snapshot.Checks, check("alpenglow.bls", Critical, "Vote account BLS key is malformed", fmt.Sprintf("compressed BLS public key must decode to 48 bytes; got %d bytes", len(decoded)), now))
			return
		}
		snapshot.BLSState = "registered"
		snapshot.Checks = append(snapshot.Checks, check("alpenglow.bls", OK, "BLS key is registered", "blsPubkeyCompressed decodes to 48 bytes", now))
	}
}

func (m *Monitor) addProductionCheck(snapshot *Snapshot, production rpc.BlockProduction, now time.Time) {
	stats := LeaderStats{Available: true, RangeFirstSlot: production.Value.Range.FirstSlot, RangeLastSlot: production.Value.Range.LastSlot}
	values := production.Value.ByIdentity[snapshot.Identity]
	if len(values) >= 2 {
		stats.LeaderSlots, stats.BlocksProduced = values[0], values[1]
	}
	if stats.BlocksProduced > stats.LeaderSlots {
		snapshot.Leader = stats
		snapshot.Checks = append(snapshot.Checks, check("leader.production", Unknown, "Leader production data is inconsistent", "Produced blocks exceed reported leader slots", now))
		return
	}
	stats.MissedSlots = stats.LeaderSlots - stats.BlocksProduced
	if stats.LeaderSlots > 0 {
		stats.SkipRatePercent = 100 * float64(stats.MissedSlots) / float64(stats.LeaderSlots)
	}
	snapshot.Leader = stats
	status := OK
	summary := "Leader production is within configured thresholds"
	if stats.LeaderSlots == 0 {
		summary = "No leader slots reported in the current production range"
	} else if stats.LeaderSlots < m.cfg.Monitor.LeaderMinimumSample {
		status = Unknown
		summary = "Leader sample is too small for a skip-rate decision"
	} else if stats.SkipRatePercent >= m.cfg.Monitor.LeaderSkipCriticalPercent {
		status = Critical
		summary = "Leader skip rate is critical"
	} else if stats.SkipRatePercent >= m.cfg.Monitor.LeaderSkipWarningPercent {
		status = Warning
		summary = "Leader skip rate is elevated"
	}
	detail := fmt.Sprintf("leader_slots=%d produced=%d missed=%d skip_rate=%.3f%%", stats.LeaderSlots, stats.BlocksProduced, stats.MissedSlots, stats.SkipRatePercent)
	snapshot.Checks = append(snapshot.Checks, check("leader.production", status, summary, detail, now))
}

func check(id string, status Status, summary, detail string, now time.Time) Check {
	return Check{ID: id, Status: status, Summary: summary, Detail: detail, ObservedAt: now}
}

func sol(lamports uint64) float64        { return float64(lamports) / float64(config.LamportsPerSOL) }
func uint64Pointer(value uint64) *uint64 { return &value }
func boolPointer(value bool) *bool       { return &value }
func floatPointer(value float64) *float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	return &value
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
