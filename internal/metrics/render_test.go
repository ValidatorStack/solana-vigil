package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/ValidatorStack/solana-vigil/internal/monitor"
)

func TestRenderExcludesTransientSlotLabelCardinality(t *testing.T) {
	runway := 1.5
	registered := true
	snapshot := monitor.Snapshot{
		Timestamp: time.Unix(1000, 0), Identity: "id\"escaped", VoteAccount: "vote", BLSState: "registered",
		VAT: monitor.VATObservation{RunwayEpochs: &runway, SufficientNextEpoch: &registered},
		Checks: []monitor.Check{
			{ID: "alpenglow.vat", Status: monitor.Warning},
			{ID: "leader.slot.42", Status: monitor.Critical, Transient: true},
		},
	}
	output := Render(snapshot, true, 10*time.Millisecond)
	for _, expected := range []string{"solana_vat_runway_epochs 1.5", "solana_bls_registered 1", "vigil_check_status{check=\"alpenglow.vat\"} 1", "identity=\"id\\\"escaped\""} {
		if !strings.Contains(output, expected) {
			t.Errorf("missing %q in:\n%s", expected, output)
		}
	}
	if strings.Contains(output, "leader.slot.42") {
		t.Fatal("transient slot became a metric label")
	}
}
