package report

import (
	"strings"
	"testing"
	"time"

	"github.com/ValidatorStack/solana-vigil/internal/monitor"
)

func TestHTMLRendersZeroRunwayAndEscapesValues(t *testing.T) {
	zero := float64(0)
	document := Document{Snapshot: monitor.Snapshot{
		Timestamp: time.Unix(1000, 0), VoteAccount: "vote<script>",
		BLSState: "missing", VAT: monitor.VATObservation{RunwayEpochs: &zero},
		Checks: []monitor.Check{{ID: "alpenglow.vat", Status: monitor.Critical, Summary: "VAT low"}},
	}}
	var output strings.Builder
	if err := WriteHTML(&output, document); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "0.000 epochs") {
		t.Fatal("zero runway rendered as unknown")
	}
	if strings.Contains(output.String(), "vote<script>") {
		t.Fatal("HTML was not escaped")
	}
}
