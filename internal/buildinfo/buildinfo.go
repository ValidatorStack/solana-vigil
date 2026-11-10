// Package buildinfo contains release metadata injected at link time.
package buildinfo

import "fmt"

var (
	Version = "0.2.0-dev"
	Commit  = "unknown"
	Date    = "unknown"
)

func String() string {
	return fmt.Sprintf("vigil %s (commit %s, built %s)", Version, Commit, Date)
}

func UserAgent() string {
	return "vigil/" + Version
}
