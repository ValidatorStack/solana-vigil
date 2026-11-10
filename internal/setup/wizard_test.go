package setup

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ValidatorStack/solana-vigil/internal/config"
)

func TestNonInteractiveSetupWritesStrictConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vigil.json")
	var output strings.Builder
	err := Run(context.Background(), strings.NewReader(""), &output, Options{VoteAccount: "11111111111111111111111111111111", Output: path, NonInteractive: true, SkipProbe: true})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Validator.VoteAccount != "11111111111111111111111111111111" {
		t.Fatal("vote account not saved")
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("config permissions too broad: %o", info.Mode().Perm())
		}
	}
}

func TestSetupRejectsPrivateKeyMaterial(t *testing.T) {
	var output strings.Builder
	err := Run(context.Background(), strings.NewReader(""), &output, Options{VoteAccount: "[1,2,3,4]", NonInteractive: true, SkipProbe: true})
	if err == nil {
		t.Fatal("expected public address validation failure")
	}
}
