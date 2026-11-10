package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ValidatorStack/solana-vigil/internal/config"
	"github.com/ValidatorStack/solana-vigil/internal/monitor"
	"github.com/ValidatorStack/solana-vigil/internal/report"
)

const testAddress = "11111111111111111111111111111111"

func TestUserFacingCommandsAgainstMockValidator(t *testing.T) {
	server := newValidatorRPC(t)
	defer server.Close()
	path := writeTestConfig(t, server.URL)

	t.Run("check JSON", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), []string{"check", "--config", path, "--json"}, strings.NewReader(""), &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit=%d stderr=%s output=%s", code, stderr.String(), stdout.String())
		}
		var document report.Document
		if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
			t.Fatal(err)
		}
		if document.Snapshot.OverallStatus() != monitor.OK {
			t.Fatalf("overall=%s checks=%+v", document.Snapshot.OverallStatus(), document.Snapshot.Checks)
		}
		if document.Snapshot.BLSState != "registered" || document.Snapshot.VAT.RunwayEpochs == nil {
			t.Fatalf("incomplete snapshot: %+v", document.Snapshot)
		}
	})

	t.Run("single exporter scrape", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), []string{"export", "--config", path, "--once"}, strings.NewReader(""), &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit=%d stderr=%s", code, stderr.String())
		}
		for _, metric := range []string{"vigil_scrape_success 1", "solana_bls_registered 1", "solana_vat_next_epoch_sufficient 1", "solana_leader_skip_ratio 0"} {
			if !strings.Contains(stdout.String(), metric) {
				t.Errorf("missing %q", metric)
			}
		}
	})

	t.Run("HTML report", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), []string{"report", "--config", path, "--format", "html"}, strings.NewReader(""), &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit=%d stderr=%s", code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "<!doctype html>") || !strings.Contains(stdout.String(), "Vigil validator report") {
			t.Fatal("HTML report not rendered")
		}
	})

	t.Run("safe alert test", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), []string{"test-alert", "--config", path}, strings.NewReader(""), &stdout, &stderr)
		if code != 0 || !strings.Contains(stdout.String(), "incident state was not changed") {
			t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
		}
	})

	t.Run("restart window", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), []string{"plan-restart", "--config", path, "--min-free-slots", "16", "--search-slots", "100", "--json"}, strings.NewReader(""), &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit=%d stderr=%s", code, stderr.String())
		}
		var window monitor.RestartWindow
		if err := json.Unmarshal(stdout.Bytes(), &window); err != nil {
			t.Fatal(err)
		}
		if window.FreeSlots != 16 || window.WindowStartSlot != 10001 {
			t.Fatalf("unexpected window: %+v", window)
		}
	})
}

func writeTestConfig(t *testing.T, endpoint string) string {
	t.Helper()
	cfg := config.Default()
	cfg.Validator.VoteAccount = testAddress
	cfg.Validator.Identity = testAddress
	cfg.RPC.LocalURL = endpoint
	cfg.Alerts.StateFile = filepath.Join(t.TempDir(), "state.json")
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "vigil.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newValidatorRPC(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     uint64 `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		var result any
		switch request.Method {
		case "getIdentity":
			result = map[string]any{"identity": testAddress}
		case "getHealth":
			result = "ok"
		case "getVersion":
			result = map[string]any{"solana-core": "4.3.2", "feature-set": 1}
		case "getGenesisHash":
			result = "Genesis111"
		case "getSlot":
			result = 10000
		case "getEpochInfo":
			result = map[string]any{"absoluteSlot": 10000, "blockHeight": 9000, "epoch": 10, "slotIndex": 1000, "slotsInEpoch": 432000}
		case "getBalance":
			result = map[string]any{"context": map[string]any{"slot": 10000}, "value": 2_600_000_000}
		case "getAccountInfo":
			result = map[string]any{"context": map[string]any{"slot": 10000}, "value": map[string]any{"owner": "Vote111", "lamports": 2_600_000_000, "space": 3731, "executable": false}}
		case "getMinimumBalanceForRentExemption":
			result = 100_000_000
		case "getVoteAccounts":
			account := map[string]any{"votePubkey": testAddress, "nodePubkey": testAddress, "activatedStake": 1000, "commission": 5, "epochVoteAccount": true, "lastVote": 10000, "rootSlot": 9990, "blsPubkeyCompressed": strings.Repeat("1", 48)}
			result = map[string]any{"current": []any{account}, "delinquent": []any{}}
		case "getBlockProduction":
			result = map[string]any{"context": map[string]any{"slot": 10000}, "value": map[string]any{"byIdentity": map[string]any{testAddress: []uint64{4, 4}}, "range": map[string]any{"firstSlot": 9000, "lastSlot": 10000}}}
		case "getLeaderSchedule":
			result = map[string]any{testAddress: []uint64{10, 20}}
		case "getRecentPerformanceSamples":
			result = []any{map[string]any{"numSlots": 100, "numTransactions": 1000, "samplePeriodSecs": 40, "slot": 10000}}
		default:
			t.Errorf("unexpected RPC method %s", request.Method)
			result = nil
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"does-not-exist"}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Fatalf("exit=%d", code)
	}
}
