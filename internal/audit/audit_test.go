package audit

import "testing"

func TestParseOpenFiles(t *testing.T) {
	limits := "Limit Soft Limit Hard Limit Units\nMax open files 1000000 1048576 files\nMax processes 100 200 processes"
	soft, hard := parseOpenFiles(limits)
	if soft != 1_000_000 || hard != 1_048_576 {
		t.Fatalf("soft=%d hard=%d", soft, hard)
	}
}

func TestParseRingBuffers(t *testing.T) {
	output := "Ring parameters for eth0:\nPre-set maximums:\nRX: 4096\nTX: 4096\nCurrent hardware settings:\nRX: 1024\nTX: 1024\n"
	current, maximum := parseRingBuffers(output)
	if current != 1024 || maximum != 4096 {
		t.Fatalf("current=%d max=%d", current, maximum)
	}
}

func TestCompareDetectsMeaningfulDrift(t *testing.T) {
	before := Report{Findings: []Finding{{ID: "cpu.governor", Status: Pass, Observed: "performance", Expected: "performance"}}}
	after := Report{Findings: []Finding{{ID: "cpu.governor", Status: Warn, Observed: "powersave", Expected: "performance"}}}
	drift := Compare(before, after)
	if len(drift) != 1 || drift[0].After.Observed != "powersave" {
		t.Fatalf("unexpected drift: %+v", drift)
	}
}

func TestNonLinuxRunIsExplicitlyUnknown(t *testing.T) {
	report := Report{OS: "windows", Findings: []Finding{{ID: "host.linux", Status: Unknown}}}
	if report.ExitCode() != 1 {
		t.Fatal("unknown audit must not exit successfully")
	}
}
