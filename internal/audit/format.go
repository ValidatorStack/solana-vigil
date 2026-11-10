package audit

import (
	"fmt"
	"io"
	"strings"
)

func WriteText(writer io.Writer, report Report) {
	fmt.Fprintf(writer, "Vigil Linux audit — %s\nHost: %s | OS: %s/%s | Client: %s", report.Timestamp.Format("2006-01-02 15:04:05 UTC"), report.Hostname, report.OS, report.Architecture, report.Client)
	if report.PID > 0 {
		fmt.Fprintf(writer, " | PID: %d", report.PID)
	}
	fmt.Fprint(writer, "\n\n")
	for _, finding := range report.Findings {
		fmt.Fprintf(writer, "%-7s %-34s %s\n", strings.ToUpper(string(finding.Status)), finding.ID, finding.Title)
		if finding.Observed != "" {
			fmt.Fprintf(writer, "        observed: %s\n", finding.Observed)
		}
		if finding.Expected != "" {
			fmt.Fprintf(writer, "        expected: %s\n", finding.Expected)
		}
		if finding.Status != Pass && finding.Recommendation != "" {
			fmt.Fprintf(writer, "        action:   %s\n", finding.Recommendation)
		}
	}
	fmt.Fprintln(writer, "\nRead-only audit: no host, network, service, or validator settings were changed.")
}

func WriteDrift(writer io.Writer, drift []Drift) {
	if len(drift) == 0 {
		fmt.Fprintln(writer, "No host drift against the supplied baseline.")
		return
	}
	fmt.Fprintf(writer, "%d audit finding(s) changed:\n", len(drift))
	for _, change := range drift {
		fmt.Fprintf(writer, "- %s: %s/%s -> %s/%s\n", change.ID, change.Before.Status, change.Before.Observed, change.After.Status, change.After.Observed)
	}
}
