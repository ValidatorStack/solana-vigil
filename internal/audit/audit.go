package audit

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Options struct {
	Client           string
	PID              int
	Interface        string
	MinimumMemoryGiB uint64
	MinimumOpenFiles uint64
}

func Run(ctx context.Context, options Options) Report {
	hostname, _ := os.Hostname()
	report := Report{Timestamp: time.Now().UTC(), Hostname: hostname, OS: runtime.GOOS, Architecture: runtime.GOARCH, Client: options.Client, PID: options.PID}
	if options.MinimumMemoryGiB == 0 {
		options.MinimumMemoryGiB = 256
	}
	if options.MinimumOpenFiles == 0 {
		options.MinimumOpenFiles = 1_000_000
	}
	if runtime.GOOS != "linux" {
		report.Findings = append(report.Findings, Finding{ID: "host.linux", Title: "Linux host audit", Status: Unknown, Observed: runtime.GOOS, Expected: "linux", Recommendation: "Run this audit directly on the Linux validator host."})
		return report
	}
	addMemoryChecks(&report, options)
	addCPUChecks(&report, options)
	addSysctlChecks(&report)
	addTimeCheck(ctx, &report)
	addNetworkChecks(ctx, &report, options.Interface)
	addProcessChecks(&report, options)
	sort.Slice(report.Findings, func(i, j int) bool { return report.Findings[i].ID < report.Findings[j].ID })
	return report
}

func addMemoryChecks(report *Report, options Options) {
	values, err := parseKeyValueFile("/proc/meminfo", ":")
	if err != nil {
		report.Findings = append(report.Findings, Finding{ID: "memory.total", Title: "Physical memory", Status: Unknown, Observed: err.Error()})
	} else {
		totalKiB := parseLeadingUint(values["MemTotal"])
		totalGiB := totalKiB / (1024 * 1024)
		status := Pass
		if totalGiB < options.MinimumMemoryGiB {
			status = Warn
		}
		report.Findings = append(report.Findings, Finding{ID: "memory.total", Title: "Physical memory", Status: status, Observed: fmt.Sprintf("%d GiB", totalGiB), Expected: fmt.Sprintf(">= %d GiB conservative mainnet baseline", options.MinimumMemoryGiB), Recommendation: "Verify the current requirements for your exact client and account set before resizing."})
		if options.Client == "firedancer" {
			hugePages := parseLeadingUint(values["HugePages_Total"])
			status := Pass
			if hugePages == 0 {
				status = Warn
			}
			report.Findings = append(report.Findings, Finding{ID: "memory.hugepages", Title: "Firedancer huge pages", Status: status, Observed: fmt.Sprintf("%s pages", values["HugePages_Total"]), Expected: "non-zero page reservation sized for your Firedancer topology", Recommendation: "Follow the exact Firedancer deployment guide; do not copy a generic page count."})
		}
	}
	if enabled, err := os.ReadFile("/sys/kernel/mm/transparent_hugepage/enabled"); err == nil {
		report.Findings = append(report.Findings, Finding{ID: "memory.transparent_hugepages", Title: "Transparent huge pages", Status: Info, Observed: selectedToken(string(enabled)), Expected: "client- and workload-specific", Recommendation: "Benchmark before changing THP; Agave and Firedancer memory setups are not interchangeable."})
	}
	addUintRule(report, "memory.swappiness", "Swap preference", "/proc/sys/vm/swappiness", 10, "<=", "<= 10 baseline; memory sizing matters more than this single value")
}

func addCPUChecks(report *Report, options Options) {
	cores := runtime.NumCPU()
	status := Pass
	if cores < 12 {
		status = Warn
	}
	report.Findings = append(report.Findings, Finding{ID: "cpu.cores", Title: "Logical CPU count", Status: status, Observed: strconv.Itoa(cores), Expected: ">= 12 logical CPUs baseline", Recommendation: "Validate clock speed, NUMA placement, and client topology as well as core count."})
	governors, _ := filepath.Glob("/sys/devices/system/cpu/cpu[0-9]*/cpufreq/scaling_governor")
	if len(governors) == 0 {
		report.Findings = append(report.Findings, Finding{ID: "cpu.governor", Title: "CPU frequency governor", Status: Unknown, Observed: "cpufreq governor not exposed", Recommendation: "Check VM/bare-metal capabilities and BIOS power settings."})
	} else {
		unique := make(map[string]struct{})
		for _, path := range governors {
			if data, err := os.ReadFile(path); err == nil {
				unique[strings.TrimSpace(string(data))] = struct{}{}
			}
		}
		var observed []string
		for governor := range unique {
			observed = append(observed, governor)
		}
		sort.Strings(observed)
		status := Pass
		if len(unique) != 1 {
			status = Warn
		} else if _, ok := unique["performance"]; !ok {
			status = Warn
		}
		report.Findings = append(report.Findings, Finding{ID: "cpu.governor", Title: "CPU frequency governor", Status: status, Observed: strings.Join(observed, ","), Expected: "performance on every exposed CPU", Recommendation: "Use your client-specific performance profile and verify thermal headroom."})
	}
	cmdline, _ := os.ReadFile("/proc/cmdline")
	isolated := strings.Contains(string(cmdline), "isolcpus=")
	status = Info
	if options.Client == "firedancer" && !isolated {
		status = Warn
	}
	report.Findings = append(report.Findings, Finding{ID: "cpu.isolation", Title: "Kernel CPU isolation", Status: status, Observed: fmt.Sprintf("isolcpus=%t", isolated), Expected: "Firedancer: align isolcpus/nohz_full with the configured tile topology", Recommendation: "Do not isolate CPUs without reserving enough cores for the OS and validator services."})
}

func addSysctlChecks(report *Report) {
	addUintRule(report, "network.core.rmem_max", "Maximum socket receive buffer", "/proc/sys/net/core/rmem_max", 134_217_728, ">=", ">= 128 MiB baseline")
	addUintRule(report, "network.core.wmem_max", "Maximum socket send buffer", "/proc/sys/net/core/wmem_max", 134_217_728, ">=", ">= 128 MiB baseline")
	addUintRule(report, "network.core.netdev_max_backlog", "Network receive backlog", "/proc/sys/net/core/netdev_max_backlog", 10_000, ">=", ">= 10000 baseline")
	addUintRule(report, "fs.file_max", "System file descriptor limit", "/proc/sys/fs/file-max", 1_000_000, ">=", ">= 1000000 baseline")
	addUintRule(report, "memory.max_map_count", "Maximum memory map count", "/proc/sys/vm/max_map_count", 1_000_000, ">=", ">= 1000000 baseline; verify against client requirements")
	if data, err := os.ReadFile("/proc/sys/net/ipv4/tcp_congestion_control"); err == nil {
		report.Findings = append(report.Findings, Finding{ID: "network.tcp_congestion_control", Title: "TCP congestion control", Status: Info, Observed: strings.TrimSpace(string(data)), Expected: "no universal validator value", Recommendation: "Solana ingest is predominantly UDP; benchmark RPC/TCP changes separately instead of copying sysctl gists."})
	}
}

func addUintRule(report *Report, id, title, path string, threshold uint64, operator, expected string) {
	data, err := os.ReadFile(path)
	if err != nil {
		report.Findings = append(report.Findings, Finding{ID: id, Title: title, Status: Unknown, Observed: err.Error(), Expected: expected})
		return
	}
	observed := parseLeadingUint(strings.TrimSpace(string(data)))
	passed := observed >= threshold
	if operator == "<=" {
		passed = observed <= threshold
	}
	status := Pass
	if !passed {
		status = Warn
	}
	report.Findings = append(report.Findings, Finding{ID: id, Title: title, Status: status, Observed: strconv.FormatUint(observed, 10), Expected: expected, Recommendation: "Test changes under load and persist them through your configuration-management system."})
}

func addTimeCheck(ctx context.Context, report *Report) {
	commandCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(commandCtx, "timedatectl", "show", "--property=NTPSynchronized", "--value").CombinedOutput()
	if err == nil {
		synchronized := strings.TrimSpace(string(output)) == "yes"
		status := Fail
		if synchronized {
			status = Pass
		}
		report.Findings = append(report.Findings, Finding{ID: "time.synchronization", Title: "System clock synchronization", Status: status, Observed: strings.TrimSpace(string(output)), Expected: "yes", Recommendation: "Use multiple reliable NTP sources and alert on offset, not only service uptime."})
		return
	}
	commandCtx2, cancel2 := context.WithTimeout(ctx, 3*time.Second)
	defer cancel2()
	output, err = exec.CommandContext(commandCtx2, "chronyc", "-c", "tracking").CombinedOutput()
	if err == nil {
		fields := strings.Split(strings.TrimSpace(string(output)), ",")
		status := Fail
		if len(fields) > 3 && fields[3] == "Normal" {
			status = Pass
		}
		report.Findings = append(report.Findings, Finding{ID: "time.synchronization", Title: "System clock synchronization", Status: status, Observed: strings.TrimSpace(string(output)), Expected: "chrony leap status Normal", Recommendation: "Repair chrony/NTP before evaluating validator latency."})
		return
	}
	report.Findings = append(report.Findings, Finding{ID: "time.synchronization", Title: "System clock synchronization", Status: Unknown, Observed: "timedatectl and chronyc unavailable or denied", Expected: "synchronized clock", Recommendation: "Verify NTP with the tools available on your host."})
}

func addNetworkChecks(ctx context.Context, report *Report, iface string) {
	if iface == "" {
		iface = defaultInterface()
	}
	if iface == "" {
		report.Findings = append(report.Findings, Finding{ID: "network.interface", Title: "Primary network interface", Status: Unknown, Observed: "no default route found", Recommendation: "Pass --interface explicitly."})
		return
	}
	state, err := os.ReadFile("/sys/class/net/" + iface + "/operstate")
	if err == nil {
		status := Fail
		if strings.TrimSpace(string(state)) == "up" {
			status = Pass
		}
		report.Findings = append(report.Findings, Finding{ID: "network.interface", Title: "Primary network interface", Status: status, Observed: iface + " " + strings.TrimSpace(string(state)), Expected: "up"})
	}
	if data, err := os.ReadFile("/sys/class/net/" + iface + "/speed"); err == nil {
		mbps := parseLeadingUint(strings.TrimSpace(string(data)))
		status := Pass
		if mbps < 10_000 {
			status = Warn
		}
		report.Findings = append(report.Findings, Finding{ID: "network.link_speed", Title: "NIC link speed", Status: status, Observed: fmt.Sprintf("%d Mb/s", mbps), Expected: ">= 10000 Mb/s baseline", Recommendation: "Check negotiated speed, errors, drops, PCIe bandwidth, and your actual upstream capacity."})
	}
	commandCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(commandCtx, "ethtool", "-g", iface).CombinedOutput()
	if err != nil {
		report.Findings = append(report.Findings, Finding{ID: "network.rings", Title: "NIC ring buffers", Status: Unknown, Observed: strings.TrimSpace(string(output)), Expected: "current RX/TX near hardware-supported values", Recommendation: "Install ethtool or run the audit with permission to query the NIC."})
		return
	}
	current, maximum := parseRingBuffers(string(output))
	status := Unknown
	observed := strings.TrimSpace(string(output))
	if current > 0 && maximum > 0 {
		status = Pass
		if current < maximum {
			status = Warn
		}
		observed = fmt.Sprintf("RX current=%d max=%d", current, maximum)
	}
	report.Findings = append(report.Findings, Finding{ID: "network.rings", Title: "NIC ring buffers", Status: status, Observed: observed, Expected: "current RX at or deliberately near the stable driver maximum", Recommendation: "Increase only after checking driver limits, memory pressure, and packet-drop counters."})
}

func addProcessChecks(report *Report, options Options) {
	pid := options.PID
	if pid == 0 {
		pid = discoverValidator()
	}
	if pid == 0 {
		report.Findings = append(report.Findings, Finding{ID: "process.validator", Title: "Validator process", Status: Unknown, Observed: "not auto-detected", Recommendation: "Pass --pid so process limits and flags can be audited."})
		return
	}
	report.PID = pid
	if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err != nil {
		report.Findings = append(report.Findings, Finding{ID: "process.validator", Title: "Validator process", Status: Fail, Observed: fmt.Sprintf("PID %d unavailable", pid), Recommendation: "Check the service and provide its current PID."})
		return
	}
	report.Findings = append(report.Findings, Finding{ID: "process.validator", Title: "Validator process", Status: Pass, Observed: fmt.Sprintf("PID %d", pid)})
	limits, err := os.ReadFile(fmt.Sprintf("/proc/%d/limits", pid))
	if err == nil {
		soft, hard := parseOpenFiles(string(limits))
		status := Unknown
		observed := "unable to parse limits"
		if soft > 0 {
			status = Pass
			if soft < options.MinimumOpenFiles {
				status = Warn
			}
			observed = fmt.Sprintf("soft=%d hard=%d", soft, hard)
		}
		report.Findings = append(report.Findings, Finding{ID: "process.open_files", Title: "Validator file descriptor limit", Status: status, Observed: observed, Expected: fmt.Sprintf("soft >= %d", options.MinimumOpenFiles), Recommendation: "Set LimitNOFILE in the validator's actual systemd unit and verify after restart."})
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err == nil {
		arguments := splitNull(string(cmdline))
		noVoting := contains(arguments, "--no-voting")
		status := Pass
		if noVoting {
			status = Fail
		}
		report.Findings = append(report.Findings, Finding{ID: "process.voting", Title: "Validator voting mode", Status: status, Observed: fmt.Sprintf("--no-voting=%t", noVoting), Expected: "voting enabled for a validator", Recommendation: "If this is intentionally an RPC node, do not run the validator audit profile against it."})
	}
}

func discoverValidator() int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	var matches []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
		if err != nil {
			continue
		}
		name := strings.TrimSpace(string(comm))
		if name == "agave-validator" || name == "solana-validator" || name == "firedancer" {
			matches = append(matches, pid)
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return 0
}

func defaultInterface() string {
	file, err := os.Open("/proc/net/route")
	if err != nil {
		return ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) > 2 && fields[1] == "00000000" {
			return fields[0]
		}
	}
	return ""
}

func parseKeyValueFile(path, separator string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), separator, 2)
		if len(parts) == 2 {
			values[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return values, scanner.Err()
}

func parseLeadingUint(value string) uint64 {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0
	}
	parsed, _ := strconv.ParseUint(fields[0], 10, 64)
	return parsed
}

func selectedToken(value string) string {
	start, end := strings.Index(value, "["), strings.Index(value, "]")
	if start >= 0 && end > start {
		return value[start+1 : end]
	}
	return strings.TrimSpace(value)
}

func parseOpenFiles(limits string) (uint64, uint64) {
	for _, line := range strings.Split(limits, "\n") {
		if strings.HasPrefix(line, "Max open files") {
			fields := strings.Fields(strings.TrimPrefix(line, "Max open files"))
			if len(fields) >= 2 {
				soft, _ := strconv.ParseUint(fields[0], 10, 64)
				hard, _ := strconv.ParseUint(fields[1], 10, 64)
				return soft, hard
			}
		}
	}
	return 0, 0
}

func parseRingBuffers(output string) (uint64, uint64) {
	section := ""
	var current, maximum uint64
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(line, "Pre-set maximums:") {
			section = "max"
			continue
		}
		if strings.HasPrefix(line, "Current hardware settings:") {
			section = "current"
			continue
		}
		if !strings.HasPrefix(trimmed, "RX:") {
			continue
		}
		value := parseLeadingUint(strings.TrimSpace(strings.TrimPrefix(trimmed, "RX:")))
		if section == "max" {
			maximum = value
		} else if section == "current" {
			current = value
		}
	}
	return current, maximum
}

func splitNull(value string) []string {
	raw := strings.Split(value, "\x00")
	result := raw[:0]
	for _, item := range raw {
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
