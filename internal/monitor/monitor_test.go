package monitor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ValidatorStack/solana-vigil/internal/config"
	"github.com/ValidatorStack/solana-vigil/internal/rpc"
)

func TestVATUsesBalanceAboveRent(t *testing.T) {
	snapshot := collectFixture(t, fixture{balance: 1_600_000_000, rent: 100_000_000, bls: json.RawMessage(`"111111111111111111111111111111111111111111111111111111111111111111"`)})
	if snapshot.VAT.SpendableLamports == nil || *snapshot.VAT.SpendableLamports != 1_500_000_000 {
		t.Fatalf("wrong spendable balance: %+v", snapshot.VAT)
	}
	if snapshot.VAT.RunwayEpochs == nil || *snapshot.VAT.RunwayEpochs != 1.875 {
		t.Fatalf("wrong runway: %+v", snapshot.VAT.RunwayEpochs)
	}
	if snapshot.VAT.SufficientNextEpoch == nil || !*snapshot.VAT.SufficientNextEpoch {
		t.Fatal("1.5 SOL above rent should fund the next 0.8 SOL VAT")
	}
}

func TestVATCriticalBelowNextEpochFee(t *testing.T) {
	snapshot := collectFixture(t, fixture{balance: 800_000_000, rent: 100_000_000})
	if snapshot.VAT.SufficientNextEpoch == nil || *snapshot.VAT.SufficientNextEpoch {
		t.Fatal("0.7 SOL above rent must not fund a 0.8 SOL VAT")
	}
	if got := findCheck(snapshot, "alpenglow.vat").Status; got != Critical {
		t.Fatalf("VAT status = %s", got)
	}
}

func TestBLSEmptySchemaIsUnknownNotMissing(t *testing.T) {
	snapshot := collectFixture(t, fixture{})
	check := findCheck(snapshot, "alpenglow.bls")
	if check.Status != Unknown || snapshot.BLSState != "unknown" {
		t.Fatalf("absent BLS schema must be unknown: %+v", check)
	}
}

func TestBLSNullIsCritical(t *testing.T) {
	raw := json.RawMessage("null")
	snapshot := collectFixture(t, fixture{bls: raw})
	check := findCheck(snapshot, "alpenglow.bls")
	if check.Status != Critical || snapshot.BLSState != "missing" {
		t.Fatalf("null BLS must be critical: %+v", check)
	}
}

func TestLeaderProductionAccounting(t *testing.T) {
	snapshot := collectFixture(t, fixture{leaderSlots: 20, blocksProduced: 18})
	if snapshot.Leader.MissedSlots != 2 || snapshot.Leader.SkipRatePercent != 10 {
		t.Fatalf("wrong leader stats: %+v", snapshot.Leader)
	}
	if got := findCheck(snapshot, "leader.production").Status; got != Warning {
		t.Fatalf("10%% skip rate should warn, got %s", got)
	}
}

type fixture struct {
	balance, rent               uint64
	bls                         json.RawMessage
	leaderSlots, blocksProduced uint64
}

func collectFixture(t *testing.T, fixture fixture) Snapshot {
	t.Helper()
	identity := "Identity111"
	vote := "Vote111"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     uint64 `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		var result any
		switch request.Method {
		case "getIdentity":
			result = map[string]any{"identity": identity}
		case "getHealth":
			result = "ok"
		case "getSlot":
			result = 10000
		case "getEpochInfo":
			result = map[string]any{"absoluteSlot": 10000, "blockHeight": 9000, "epoch": 10, "slotIndex": 1000, "slotsInEpoch": 432000}
		case "getVersion":
			result = map[string]any{"solana-core": "4.3.2", "feature-set": 1}
		case "getBalance":
			result = map[string]any{"context": map[string]any{"slot": 10000}, "value": fixture.balance}
		case "getAccountInfo":
			result = map[string]any{"context": map[string]any{"slot": 10000}, "value": map[string]any{"owner": "Vote111", "lamports": fixture.balance, "space": 3731, "executable": false, "data": []any{base64.StdEncoding.EncodeToString(make([]byte, 3731)), "base64"}}}
		case "getMinimumBalanceForRentExemption":
			result = fixture.rent
		case "getVoteAccounts":
			account := map[string]any{"votePubkey": vote, "nodePubkey": identity, "activatedStake": 1000, "commission": 5, "epochVoteAccount": true, "lastVote": 9999, "rootSlot": 9990}
			if fixture.bls != nil {
				account["blsPubkeyCompressed"] = fixture.bls
			}
			result = map[string]any{"current": []any{account}, "delinquent": []any{}}
		case "getBlockProduction":
			result = map[string]any{"context": map[string]any{"slot": 10000}, "value": map[string]any{"byIdentity": map[string]any{identity: []uint64{fixture.leaderSlots, fixture.blocksProduced}}, "range": map[string]any{"firstSlot": 9000, "lastSlot": 10000}}}
		default:
			t.Errorf("unexpected RPC method %s", request.Method)
			result = nil
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.Validator.VoteAccount = vote
	cfg.RPC.LocalURL = server.URL
	client := rpc.New(server.URL, rpc.Options{Timeout: 2 * time.Second})
	monitor := New(cfg, client, nil, nil, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return monitor.Collect(ctx)
}

func findCheck(snapshot Snapshot, id string) Check {
	for _, check := range snapshot.Checks {
		if check.ID == id {
			return check
		}
	}
	return Check{Status: Unknown, Summary: "check not found: " + id}
}
