package docker

import (
	"testing"
	"time"
)

func TestParseSample(t *testing.T) {
	// What probeScript's grep prints for one sample.
	lines := []string{
		"cpu.stat:usage_usec 2500000",
		"memory.stat:anon 104857600",
		"memory.stat:inactive_file 20971520",
		"memory.current:157286400",
		"memory.peak:209715200",
		"memory.events:oom_kill 1",
		"cgroup.procs:1",
		"cgroup.procs:7",
		"cgroup.procs:8",
		"cgroup.procs:42",
		"cgroup.procs:43",
	}
	s := parseSample(lines)
	if s.usageUsec != 2500000 || s.anon != 100<<20 || s.inactiveFile != 20<<20 ||
		s.current != 150<<20 || s.peak != 200<<20 || s.oomKills != 1 || s.procs != 5 {
		t.Fatalf("parse: %+v", s)
	}
	st := s.stats()
	if st.CPUTime != 2500*time.Millisecond || st.WorkingSet != 130<<20 || st.Private != 100<<20 ||
		st.PeakCommit != 200<<20 || st.Processes != 3 {
		t.Errorf("stats: %+v", st)
	}
}

func TestParseSampleWithoutPeak(t *testing.T) {
	// Kernels before 5.19 have no memory.peak; fall back to the current charge.
	s := parseSample([]string{"cpu.stat:usage_usec 1", "memory.current:1048576", "cgroup.procs:1"})
	if st := s.stats(); st.PeakCommit != 1<<20 || st.Processes != 0 {
		t.Errorf("stats: %+v", st)
	}
}

func TestNames(t *testing.T) {
	if Image("rust/axum") != "api-bench/rust-axum" || containerName("node/nestjs-express") != "api-bench-node-nestjs-express" {
		t.Error(Image("rust/axum"), containerName("node/nestjs-express"))
	}
}
