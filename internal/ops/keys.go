package ops

import (
	"os"
	"time"
)

// KeyFile reports hygiene of a keypair file without opening it.
type KeyFile struct {
	Path     string   `json:"path"`
	Exists   bool     `json:"exists"`
	Mode     string   `json:"mode,omitempty"`
	Owner    int      `json:"owner_uid,omitempty"`
	Bytes    int64    `json:"bytes,omitempty"`
	Modified string   `json:"modified,omitempty"`
	Problems []string `json:"problems,omitempty"`
}

// InspectKey stats a keypair. It never reads the content, so no secret can leak
// into a log or a report.
func InspectKey(path string) KeyFile {
	k := KeyFile{Path: path}
	fi, err := os.Stat(path)
	if err != nil {
		return k
	}
	k.Exists = true
	k.Mode = fi.Mode().Perm().String()
	k.Bytes = fi.Size()
	k.Modified = fi.ModTime().UTC().Format(time.RFC3339)
	if st, ok := fi.Sys().(*syscallStat); ok {
		k.Owner = int(st.Uid)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		k.Problems = append(k.Problems, "group/other readable (chmod 600)")
	}
	if fi.Size() < 32 || fi.Size() > 4096 {
		k.Problems = append(k.Problems, "unexpected size for a keypair file")
	}
	if age := time.Since(fi.ModTime()); age > 180*24*time.Hour {
		k.Problems = append(k.Problems, "not touched in over 180 days")
	}
	return k
}
