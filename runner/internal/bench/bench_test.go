package bench

import (
	"testing"
	"time"

	"github.com/muneeb-sial/api-benchmarks/runner/internal/k6"
)

func TestCPUCores(t *testing.T) {
	if got := CPUCores(time.Second, 3*time.Second, time.Second); got != 2 {
		t.Errorf("CPUCores = %v, want 2", got)
	}
	if got := CPUCores(time.Second, 0, time.Second); got != 0 {
		t.Errorf("CPUCores with counter reset = %v, want 0", got)
	}
}

func TestStalledSeconds(t *testing.T) {
	load := []k6.Point{
		{RPS: 40000, VUs: 10}, {RPS: 4200, VUs: 10}, {RPS: 0, VUs: 10}, {RPS: 0, VUs: 10}, {RPS: 0, VUs: 0},
	}
	if got := StalledSeconds(load); got != 2 {
		t.Errorf("StalledSeconds = %d, want 2", got)
	}
}

func TestSummarizeSkipsFirstSecond(t *testing.T) {
	r := summarize([]ResPoint{
		{T: 0.5, CPU: 9, MemMB: 900},
		{T: 1.5, CPU: 1, MemMB: 100},
		{T: 2.5, CPU: 3, MemMB: 300},
	}, 50)
	if r.AvgCPU != 2 || r.PeakCPU != 3 || r.PeakMemMB != 300 || r.IdleMemMB != 50 {
		t.Errorf("summarize = %+v", r)
	}
}
