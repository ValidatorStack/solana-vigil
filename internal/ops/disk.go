package ops

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type Disk struct {
	Path        string   `json:"path"`
	TotalBytes  uint64   `json:"total_bytes"`
	FreeBytes   uint64   `json:"free_bytes"`
	UsedPct     float64  `json:"used_pct"`
	GrowthGBDay *float64 `json:"growth_gb_per_day,omitempty"`
	DaysLeft    *float64 `json:"days_left,omitempty"`
	Samples     int      `json:"samples"`
}

type diskSample struct {
	TS   int64  `json:"ts"`
	Free uint64 `json:"free"`
}

func stateFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".vigil", "disk.json")
}

// DiskForecast reads free space, appends a sample, and returns days-to-full
// from the slope of the samples (needs at least two, hours apart).
func DiskForecast(path string) (Disk, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return Disk{}, err
	}
	total := st.Blocks * uint64(st.Bsize)
	free := st.Bavail * uint64(st.Bsize)
	d := Disk{
		Path:       path,
		TotalBytes: total,
		FreeBytes:  free,
		UsedPct:    (1 - float64(free)/float64(total)) * 100,
	}

	var hist []diskSample
	if b, err := os.ReadFile(stateFile()); err == nil {
		_ = json.Unmarshal(b, &hist)
	}
	now := time.Now().Unix()
	if n := len(hist); n == 0 || now-hist[n-1].TS > 300 {
		hist = append(hist, diskSample{TS: now, Free: free})
		if len(hist) > 64 {
			hist = hist[len(hist)-64:]
		}
		if b, err := json.Marshal(hist); err == nil {
			_ = os.MkdirAll(filepath.Dir(stateFile()), 0o755)
			_ = os.WriteFile(stateFile(), b, 0o644)
		}
	}
	d.Samples = len(hist)
	if len(hist) >= 2 {
		first := hist[0]
		hours := float64(now-first.TS) / 3600
		if hours >= 1 {
			lost := float64(int64(first.Free) - int64(free))
			gbDay := lost / hours * 24 / (1024 * 1024 * 1024)
			d.GrowthGBDay = &gbDay
			if gbDay > 0.001 {
				days := float64(free) / (gbDay * 1024 * 1024 * 1024)
				d.DaysLeft = &days
			}
		}
	}
	return d, nil
}
