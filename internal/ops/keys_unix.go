//go:build linux || darwin

package ops

import "syscall"

type syscallStat = syscall.Stat_t
