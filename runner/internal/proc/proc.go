// Package proc launches a process tree inside an OS-level resource container
// (a Job Object on Windows) so that the whole tree — including cluster workers
// spawned by pm2, uvicorn, etc. — shares one hard memory cap and CPU affinity,
// and can be measured and killed as a unit.
package proc

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Options struct {
	Argv []string
	Dir  string
	// Env is appended to the current environment.
	Env    []string
	Stdout io.Writer
	Stderr io.Writer
	// MemoryBytes is the hard cap on committed memory for the whole tree (0 = none).
	MemoryBytes uint64
	// Affinity is a CPU bitmask (0 = all CPUs).
	Affinity uint64
}

// Stats is a point-in-time snapshot of the whole process tree.
type Stats struct {
	// CPUTime is cumulative user+kernel time of every process that ever ran in the tree.
	CPUTime time.Duration
	// Processes currently alive.
	Processes int
	// WorkingSet is the sum of resident memory (RSS) of live processes.
	WorkingSet uint64
	// Private is the sum of committed private memory of live processes; this is
	// what the memory cap is enforced against.
	Private uint64
	// PeakCommit is the high-water mark of committed memory for the tree.
	PeakCommit uint64
}

// resolveExe resolves a relative executable against dir rather than the
// runner's working directory. A bare name ("server.exe") is taken from dir if
// it exists there, otherwise looked up on PATH.
func resolveExe(exe, dir string) string {
	if filepath.IsAbs(exe) {
		return exe
	}
	if !strings.ContainsAny(exe, `/\`) {
		if p := filepath.Join(dir, exe); dir != "" && fileExists(p) {
			return p
		}
		return exe
	}
	p := filepath.Join(dir, exe)
	if _, err := os.Stat(p); err != nil {
		if _, err2 := os.Stat(p + ".exe"); err2 == nil {
			return p + ".exe"
		}
	}
	return p
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
