//go:build windows

package proc

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// The test binary re-executes itself as a child in one of these modes.
func TestMain(m *testing.M) {
	switch os.Getenv("PROC_TEST_MODE") {
	case "hog":
		var keep [][]byte
		for {
			b := make([]byte, 16<<20)
			for i := range b {
				b[i] = 1
			}
			keep = append(keep, b)
		}
	case "parent":
		// Spawns a grandchild that idles; it must land in the same job.
		c := exec.Command(os.Args[0])
		c.Env = append(os.Environ(), "PROC_TEST_MODE=idle")
		_ = c.Start()
		time.Sleep(time.Minute)
		os.Exit(0)
	case "idle":
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestMemoryCapKillsHog(t *testing.T) {
	j, err := Start(Options{
		Argv:        []string{os.Args[0]},
		Env:         []string{"PROC_TEST_MODE=hog"},
		MemoryBytes: 128 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	// A refused commit does not necessarily kill the process (Go's runtime, for
	// one, stalls), so the contract is: the cap holds and the hit is reported.
	deadline := time.Now().Add(20 * time.Second)
	for !j.MemoryLimitHit() {
		if time.Now().After(deadline) {
			s, err := j.Stats()
			t.Fatalf("memory limit never reported: %+v err=%v", s, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	s, err := j.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if s.PeakCommit > 130<<20 {
		t.Errorf("peak commit %d MB exceeds the 128 MB cap", s.PeakCommit>>20)
	}
}

func TestGrandchildrenStayInJob(t *testing.T) {
	j, err := Start(Options{
		Argv: []string{os.Args[0]},
		Env:  []string{"PROC_TEST_MODE=parent"},
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		s, err := j.Stats()
		if err != nil {
			t.Fatal(err)
		}
		if s.Processes >= 2 {
			if s.WorkingSet == 0 {
				t.Error("expected non-zero working set")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected 2 processes in job, got %d", s.Processes)
		}
		time.Sleep(50 * time.Millisecond)
	}
	j.Close()
	if err := j.Wait(); err == nil {
		t.Error("expected root to be killed")
	}
}
