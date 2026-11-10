package rpc

import (
	"context"
	"encoding/json"
	"fmt"
)

type Context struct {
	Slot       uint64 `json:"slot"`
	APIVersion string `json:"apiVersion,omitempty"`
}

type Identity struct {
	Identity string `json:"identity"`
}

type Version struct {
	SolanaCore string `json:"solana-core"`
	FeatureSet uint64 `json:"feature-set,omitempty"`
}

type EpochInfo struct {
	AbsoluteSlot     uint64 `json:"absoluteSlot"`
	BlockHeight      uint64 `json:"blockHeight"`
	Epoch            uint64 `json:"epoch"`
	SlotIndex        uint64 `json:"slotIndex"`
	SlotsInEpoch     uint64 `json:"slotsInEpoch"`
	TransactionCount uint64 `json:"transactionCount,omitempty"`
}

func (e EpochInfo) EpochFirstSlot() uint64 {
	return e.AbsoluteSlot - e.SlotIndex
}

// OptionalString distinguishes an absent RPC field (schema unsupported) from
// an explicitly null field (not registered).
type OptionalString struct {
	Present bool
	Null    bool
	Value   string
}

func (o *OptionalString) UnmarshalJSON(data []byte) error {
	o.Present = true
	if string(data) == "null" {
		o.Null = true
		return nil
	}
	return json.Unmarshal(data, &o.Value)
}

type VoteAccount struct {
	VotePubkey       string         `json:"votePubkey"`
	NodePubkey       string         `json:"nodePubkey"`
	ActivatedStake   uint64         `json:"activatedStake"`
	Commission       uint8          `json:"commission"`
	EpochVoteAccount bool           `json:"epochVoteAccount"`
	LastVote         uint64         `json:"lastVote"`
	RootSlot         uint64         `json:"rootSlot"`
	EpochCredits     [][]uint64     `json:"epochCredits,omitempty"`
	BLSPubkey        OptionalString `json:"blsPubkeyCompressed"`
}

// MarshalJSON preserves the RPC wire schema while keeping presence metadata
// internal. Absent fields stay absent, explicit null stays null, and a
// registered key remains a string in exported reports.
func (v VoteAccount) MarshalJSON() ([]byte, error) {
	type alias VoteAccount
	var bls any
	if v.BLSPubkey.Present {
		if v.BLSPubkey.Null {
			bls = json.RawMessage("null")
		} else {
			bls = v.BLSPubkey.Value
		}
	}
	return json.Marshal(struct {
		alias
		BLSPubkey any `json:"blsPubkeyCompressed,omitempty"`
	}{alias: alias(v), BLSPubkey: bls})
}

type voteAccountsResult struct {
	Current    []VoteAccount `json:"current"`
	Delinquent []VoteAccount `json:"delinquent"`
}

type Balance struct {
	Context Context `json:"context"`
	Value   uint64  `json:"value"`
}

type accountInfoResult struct {
	Context Context `json:"context"`
	Value   *struct {
		Owner      string `json:"owner"`
		Lamports   uint64 `json:"lamports"`
		Space      uint64 `json:"space"`
		Executable bool   `json:"executable"`
		RentEpoch  uint64 `json:"rentEpoch,omitempty"`
	} `json:"value"`
}

type LeaderSchedule map[string][]uint64

type BlockProduction struct {
	Context Context `json:"context"`
	Value   struct {
		ByIdentity map[string][]uint64 `json:"byIdentity"`
		Range      struct {
			FirstSlot uint64  `json:"firstSlot"`
			LastSlot  *uint64 `json:"lastSlot,omitempty"`
		} `json:"range"`
	} `json:"value"`
}

type PerformanceSample struct {
	NumSlots         uint64 `json:"numSlots"`
	NumTransactions  uint64 `json:"numTransactions"`
	SamplePeriodSecs uint64 `json:"samplePeriodSecs"`
	Slot             uint64 `json:"slot"`
}

func (c *Client) GetIdentity(ctx context.Context) (Identity, error) {
	var out Identity
	err := c.Call(ctx, "getIdentity", nil, &out)
	return out, err
}

func (c *Client) GetHealth(ctx context.Context) error {
	var out string
	if err := c.Call(ctx, "getHealth", nil, &out); err != nil {
		return err
	}
	if out != "ok" {
		return fmt.Errorf("unexpected health response %q", out)
	}
	return nil
}

func (c *Client) GetVersion(ctx context.Context) (Version, error) {
	var out Version
	err := c.Call(ctx, "getVersion", nil, &out)
	return out, err
}

func (c *Client) GetGenesisHash(ctx context.Context) (string, error) {
	var out string
	err := c.Call(ctx, "getGenesisHash", nil, &out)
	return out, err
}

func (c *Client) GetSlot(ctx context.Context, commitment string) (uint64, error) {
	var out uint64
	err := c.Call(ctx, "getSlot", []any{map[string]any{"commitment": commitment}}, &out)
	return out, err
}

func (c *Client) GetEpochInfo(ctx context.Context, commitment string) (EpochInfo, error) {
	var out EpochInfo
	err := c.Call(ctx, "getEpochInfo", []any{map[string]any{"commitment": commitment}}, &out)
	return out, err
}

func (c *Client) GetBalance(ctx context.Context, pubkey, commitment string) (Balance, error) {
	var out Balance
	err := c.Call(ctx, "getBalance", []any{pubkey, map[string]any{"commitment": commitment}}, &out)
	return out, err
}

func (c *Client) GetMinimumBalanceForRentExemption(ctx context.Context, space uint64) (uint64, error) {
	var out uint64
	err := c.Call(ctx, "getMinimumBalanceForRentExemption", []any{space}, &out)
	return out, err
}

func (c *Client) GetAccountSpace(ctx context.Context, pubkey, commitment string) (uint64, error) {
	var out accountInfoResult
	params := []any{pubkey, map[string]any{"encoding": "jsonParsed", "commitment": commitment}}
	if err := c.Call(ctx, "getAccountInfo", params, &out); err != nil {
		return 0, err
	}
	if out.Value == nil {
		return 0, fmt.Errorf("account %s does not exist", pubkey)
	}
	if out.Value.Space == 0 {
		return 0, fmt.Errorf("account %s returned no data size", pubkey)
	}
	return out.Value.Space, nil
}

// GetVoteAccount returns the account, whether it is delinquent, and whether it
// was present in either list.
func (c *Client) GetVoteAccount(ctx context.Context, votePubkey, commitment string) (VoteAccount, bool, bool, error) {
	var out voteAccountsResult
	params := []any{map[string]any{
		"commitment":              commitment,
		"votePubkey":              votePubkey,
		"keepUnstakedDelinquents": true,
	}}
	if err := c.Call(ctx, "getVoteAccounts", params, &out); err != nil {
		return VoteAccount{}, false, false, err
	}
	for _, account := range out.Current {
		if account.VotePubkey == votePubkey {
			return account, false, true, nil
		}
	}
	for _, account := range out.Delinquent {
		if account.VotePubkey == votePubkey {
			return account, true, true, nil
		}
	}
	return VoteAccount{}, false, false, nil
}

func (c *Client) GetLeaderSchedule(ctx context.Context, identity, commitment string) (LeaderSchedule, error) {
	var out LeaderSchedule
	params := []any{map[string]any{"identity": identity, "commitment": commitment}}
	if err := c.Call(ctx, "getLeaderSchedule", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetBlockProduction(ctx context.Context, identity, commitment string) (BlockProduction, error) {
	var out BlockProduction
	params := []any{map[string]any{"identity": identity, "commitment": commitment}}
	if err := c.Call(ctx, "getBlockProduction", params, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) GetBlocks(ctx context.Context, start, end uint64, commitment string) ([]uint64, error) {
	var out []uint64
	params := []any{start, end, map[string]any{"commitment": commitment}}
	err := c.Call(ctx, "getBlocks", params, &out)
	return out, err
}

func (c *Client) GetRecentPerformanceSamples(ctx context.Context, limit int) ([]PerformanceSample, error) {
	var out []PerformanceSample
	err := c.Call(ctx, "getRecentPerformanceSamples", []any{limit}, &out)
	return out, err
}
