// Package bench drives the build and benchmark lifecycle for each server.
package bench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/muneeb-sial/api-benchmarks/runner/internal/config"
	"github.com/muneeb-sial/api-benchmarks/runner/internal/docker"
	"github.com/muneeb-sial/api-benchmarks/runner/internal/k6"
	"github.com/muneeb-sial/api-benchmarks/runner/internal/proc"
)

const (
	StatusOK      = "ok"
	StatusOOM     = "oom"
	StatusCrashed = "crashed"
	StatusFailed  = "failed"
	StatusStalled = "stalled"
)

// StalledSeconds counts one-second samples in which VUs were active but no
// request completed — the server hung rather than erroring.
func StalledSeconds(load []k6.Point) int {
	n := 0
	for _, p := range load {
		if p.VUs > 0 && p.RPS < 1 {
			n++
		}
	}
	return n
}

// ResPoint is one resource sample of the server's process tree.
type ResPoint struct {
	T      float64 `json:"t"`    // seconds since the measured k6 run started
	CPU    float64 `json:"cpu"`  // CPU cores in use (1.0 = one core fully busy)
	MemMB  float64 `json:"mem"`  // resident memory (working set) MB
	PrivMB float64 `json:"priv"` // committed private memory MB (what the cap limits)
	Procs  int     `json:"procs"`
}

type Resources struct {
	AvgCPU       float64 `json:"avg_cpu"`
	PeakCPU      float64 `json:"peak_cpu"`
	IdleMemMB    float64 `json:"idle_mem"`
	AvgMemMB     float64 `json:"avg_mem"`
	PeakMemMB    float64 `json:"peak_mem"`
	PeakCommitMB float64 `json:"peak_commit"`
	Processes    int     `json:"processes"`
}

type Result struct {
	Server     string      `json:"server"`
	Runtime    string      `json:"runtime"`
	Framework  string      `json:"framework"`
	Scenario   string      `json:"scenario"`
	Status     string      `json:"status"`
	Error      string      `json:"error,omitempty"`
	Notes      string      `json:"notes,omitempty"`
	Workers    int         `json:"workers"`
	MemoryMB   int         `json:"memory_mb"`
	ServerCPUs string      `json:"server_cpus"`
	StartupMs  float64     `json:"startup_ms"`
	K6         *k6.Summary `json:"k6,omitempty"`
	Resources  Resources   `json:"resources"`
	Series     Series      `json:"series"`
	Started    time.Time   `json:"started"`
}

type Series struct {
	Load []k6.Point `json:"load"`
	Res  []ResPoint `json:"res"`
}

type Meta struct {
	Started  time.Time       `json:"started"`
	Host     string          `json:"host"`
	OS       string          `json:"os"`
	CPUs     int             `json:"cpus"`
	K6       config.K6       `json:"k6"`
	Defaults config.Settings `json:"defaults"`
	// Docker is the engine version when the servers ran in containers.
	Docker string `json:"docker,omitempty"`
}

type Run struct {
	Meta    Meta      `json:"meta"`
	Results []*Result `json:"results"`
}

// ---------------------------------------------------------------- build

// Build runs every manifest's build steps; it keeps going after a failure and
// returns the names that failed.
func Build(ms []*config.Manifest, out io.Writer) []string {
	var failed []string
	for _, m := range ms {
		fmt.Fprintf(out, "\n==> build %s\n", m.Name)
		for _, step := range m.Build {
			fmt.Fprintf(out, "  $ %s\n", step)
			if err := shell(step, m.Dir, out); err != nil {
				fmt.Fprintf(out, "  !! %s: %v\n", m.Name, err)
				failed = append(failed, m.Name)
				break
			}
		}
	}
	return failed
}

// BuildImages builds the Docker image of every manifest that has a Dockerfile;
// like Build it keeps going after a failure and returns the names that failed.
func BuildImages(ms []*config.Manifest, out io.Writer) []string {
	var failed []string
	for _, m := range ms {
		fmt.Fprintf(out, "\n==> docker build %s\n", m.Name)
		if m.Dockerfile() == "" {
			fmt.Fprintf(out, "  !! %s: no Dockerfile\n", m.Name)
			failed = append(failed, m.Name)
			continue
		}
		if err := docker.Build(m.Name, m.Dir, out); err != nil {
			fmt.Fprintf(out, "  !! %s: %v\n", m.Name, err)
			failed = append(failed, m.Name)
		}
	}
	return failed
}

func shell(step, dir string, out io.Writer) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd")
		// Pass the line verbatim; Go's argument quoting confuses cmd.exe.
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd /S /C "` + step + `"`}
	} else {
		cmd = exec.Command("sh", "-c", step)
	}
	cmd.Dir = dir
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}

// ---------------------------------------------------------------- run

type Runner struct {
	Cfg    *config.Config
	Root   string
	OutDir string
	Log    io.Writer
	// Docker runs every server from its Dockerfile in a container; k6 stays on the host.
	Docker bool
}

// server is a running server under test: a native process tree in a Job Object
// (*proc.Job) or a container (*docker.Container).
type server interface {
	Exited() <-chan struct{}
	MemoryLimitHit() bool
	AbnormalExit() bool
	Stats() (proc.Stats, error)
	Close()
}

// RunAll benchmarks every manifest × scenario sequentially and writes
// summary.json after each result, so an interrupted run keeps what it has.
func (r *Runner) RunAll(ctx context.Context, ms []*config.Manifest, scenarios []string) (*Run, error) {
	host, _ := os.Hostname()
	run := &Run{Meta: Meta{
		Started: time.Now(), Host: host, OS: runtime.GOOS + "/" + runtime.GOARCH,
		CPUs: runtime.NumCPU(), K6: r.Cfg.K6, Defaults: r.Cfg.Defaults,
	}}
	if r.Docker {
		v, err := docker.Check()
		if err != nil {
			return run, err
		}
		run.Meta.Docker = v
	}
	cooldown, _ := time.ParseDuration(r.Cfg.Cooldown)
	total := len(ms) * len(scenarios)
	i := 0
	for _, m := range ms {
		set := r.Cfg.For(m.Name)
		if set.Disabled {
			fmt.Fprintf(r.Log, "skip %s (disabled in config)\n", m.Name)
			continue
		}
		var imageErr error
		if r.Docker && ctx.Err() == nil {
			imageErr = r.buildImage(m)
		}
		for _, sc := range scenarios {
			if ctx.Err() != nil {
				return run, ctx.Err()
			}
			i++
			fmt.Fprintf(r.Log, "\n[%d/%d] %s · %s\n", i, total, m.Name, sc)
			res := r.runOne(ctx, m, set, sc, imageErr)
			printResult(r.Log, res)
			run.Results = append(run.Results, res)
			if err := WriteRun(r.OutDir, run); err != nil {
				return run, err
			}
			select {
			case <-ctx.Done():
			case <-time.After(cooldown):
			}
		}
	}
	return run, nil
}

// buildImage builds (or, from cache, refreshes) the server's image once before
// its scenarios run, logging to <out>/<runtime>_<framework>/docker-build.log.
func (r *Runner) buildImage(m *config.Manifest) error {
	if m.Dockerfile() == "" {
		return fmt.Errorf("no Dockerfile in %s", m.Dir)
	}
	runtimeName, framework, _ := strings.Cut(m.Name, "/")
	dir := filepath.Join(r.OutDir, runtimeName+"_"+framework)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	logPath := filepath.Join(dir, "docker-build.log")
	logf, err := os.Create(logPath)
	if err != nil {
		return err
	}
	defer logf.Close()
	fmt.Fprintf(r.Log, "\ndocker build %s\n", docker.Image(m.Name))
	t0 := time.Now()
	if err := docker.Build(m.Name, m.Dir, logf); err != nil {
		return fmt.Errorf("docker build: %w (see %s)", err, logPath)
	}
	fmt.Fprintf(r.Log, "  built in %.1f s\n", time.Since(t0).Seconds())
	return nil
}

func (r *Runner) runOne(ctx context.Context, m *config.Manifest, set config.Settings, scenario string, imageErr error) *Result {
	if r.Docker {
		m = m.InDocker()
	}
	runtimeName, framework, _ := strings.Cut(m.Name, "/")
	workers := m.EffectiveWorkers(set.Workers)
	res := &Result{
		Server: m.Name, Runtime: runtimeName, Framework: framework, Scenario: scenario,
		Notes: m.Notes, Workers: workers, MemoryMB: set.MemoryMB, ServerCPUs: set.ServerCPUs,
		Started: time.Now(), Status: StatusOK,
	}
	fail := func(status string, err error) *Result {
		res.Status = status
		res.Error = err.Error()
		return res
	}

	dir := filepath.Join(r.OutDir, runtimeName+"_"+framework, scenario)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fail(StatusFailed, err)
	}
	if imageErr != nil {
		return fail(StatusFailed, imageErr)
	}
	if portInUse(r.Cfg.Port) {
		return fail(StatusFailed, fmt.Errorf("port %d is already in use", r.Cfg.Port))
	}

	// Inside a container the server must listen on every interface for the
	// published port to reach it.
	host := "127.0.0.1"
	if r.Docker {
		host = "0.0.0.0"
	}
	vars := config.Vars(host, r.Cfg.Port, workers, set.MemoryMB)
	argv := make([]string, len(m.Start))
	for i, a := range m.Start {
		argv[i] = config.Expand(a, vars)
	}
	var env []string
	for k, v := range vars {
		env = append(env, k+"="+v)
	}
	for k, v := range m.Env {
		env = append(env, k+"="+config.Expand(v, vars))
	}
	serverAff, _ := config.ParseCPUs(set.ServerCPUs)
	k6Aff, _ := config.ParseCPUs(r.Cfg.K6.CPUs)

	logf, err := os.Create(filepath.Join(dir, "server.log"))
	if err != nil {
		return fail(StatusFailed, err)
	}
	defer logf.Close()

	t0 := time.Now()
	var job server
	if r.Docker {
		c, err := docker.Start(docker.Options{
			Server: m.Name, Env: env, Port: r.Cfg.Port, MemoryMB: set.MemoryMB, CPUs: set.ServerCPUs,
			Stdout: logf, Stderr: logf,
		})
		if err != nil {
			return fail(StatusFailed, fmt.Errorf("docker run %s: %w", docker.Image(m.Name), err))
		}
		job = c
	} else {
		j, err := proc.Start(proc.Options{
			Argv: argv, Dir: m.Dir, Env: env, Stdout: logf, Stderr: logf,
			MemoryBytes: uint64(set.MemoryMB) << 20, Affinity: serverAff,
		})
		if err != nil {
			return fail(StatusFailed, fmt.Errorf("start %q: %w (did you run build?)", strings.Join(argv, " "), err))
		}
		job = j
	}
	defer job.Close()

	base := fmt.Sprintf("http://127.0.0.1:%d", r.Cfg.Port)
	timeout, _ := time.ParseDuration(r.Cfg.StartupTimeout)
	if err := waitHealthy(ctx, base+"/text", job, timeout); err != nil {
		if job.MemoryLimitHit() {
			return fail(StatusOOM, err)
		}
		return fail(StatusCrashed, fmt.Errorf("%w (see %s)", err, filepath.Join(dir, "server.log")))
	}
	res.StartupMs = float64(time.Since(t0).Microseconds()) / 1000
	if s, err := job.Stats(); err == nil {
		res.Resources.IdleMemMB = mb(s.WorkingSet)
	}

	k6log, err := os.Create(filepath.Join(dir, "k6.log"))
	if err != nil {
		return fail(StatusFailed, err)
	}
	defer k6log.Close()
	opts := k6.Options{
		Script: filepath.Join(r.Root, "runner", "k6", "script.js"), BaseURL: base, Scenario: scenario,
		Executor: r.Cfg.K6.Executor, VUs: r.Cfg.K6.VUs, Rate: r.Cfg.K6.Rate,
		APIAddress: r.Cfg.K6.APIAddress, Affinity: k6Aff, Log: k6log,
	}

	fmt.Fprintf(r.Log, "  up in %.0f ms, warmup %s\n", res.StartupMs, r.Cfg.K6.Warmup)
	warm := opts
	warm.Duration = r.Cfg.K6.Warmup
	if _, _, err := k6.Run(warm); err != nil {
		return fail(r.classify(job, StatusFailed), fmt.Errorf("warmup: %w (see %s)", err, k6log.Name()))
	}

	fmt.Fprintf(r.Log, "  measuring %s @ %d VUs\n", r.Cfg.K6.Duration, r.Cfg.K6.VUs)
	measured := opts
	measured.Duration = r.Cfg.K6.Duration
	measured.Poll = true
	measured.SummaryPath = filepath.Join(dir, "k6-summary.json")
	if r.Cfg.K6.Dashboard {
		measured.DashboardExport = filepath.Join(dir, "k6-dashboard.html")
	}
	interval, _ := time.ParseDuration(r.Cfg.SampleInterval)
	stop := make(chan struct{})
	samples := make(chan []ResPoint, 1)
	go func() { samples <- sample(job, interval, stop) }()
	load, sum, k6err := k6.Run(measured)
	close(stop)
	res.Series.Res = <-samples
	res.Series.Load = load
	res.K6 = sum
	res.Resources = summarize(res.Series.Res, res.Resources.IdleMemMB)
	if s, err := job.Stats(); err == nil {
		res.Resources.PeakCommitMB = mb(s.PeakCommit)
	}

	if k6err != nil {
		return fail(r.classify(job, StatusFailed), k6err)
	}
	if st := r.classify(job, StatusOK); st != StatusOK {
		res.Status = st
		res.Error = map[string]string{
			StatusOOM:     fmt.Sprintf("hit the %d MB memory cap during the run", set.MemoryMB),
			StatusCrashed: "server process exited or crashed during the run",
		}[st]
	} else if n := StalledSeconds(load); n >= 2 {
		// Hung requests are "interrupted", not failed, so k6 reports 0 errors.
		res.Status = StatusStalled
		res.Error = fmt.Sprintf("server stopped responding for %d s while k6 was sending load", n)
	}
	writeSeriesCSV(filepath.Join(dir, "resources.csv"), res.Series.Res)
	if b, err := json.MarshalIndent(res, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "result.json"), b, 0o644)
	}
	return res
}

func (r *Runner) classify(job server, otherwise string) string {
	if job.MemoryLimitHit() {
		return StatusOOM
	}
	select {
	case <-job.Exited():
		return StatusCrashed
	default:
	}
	if job.AbnormalExit() {
		return StatusCrashed
	}
	return otherwise
}

func waitHealthy(ctx context.Context, url string, job server, timeout time.Duration) error {
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(timeout)
	for {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		select {
		case <-job.Exited():
			return errors.New("server exited before becoming healthy")
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("server not healthy after %s", timeout)
		}
	}
}

func sample(job server, interval time.Duration, stop <-chan struct{}) []ResPoint {
	var out []ResPoint
	start := time.Now()
	prevT := start
	prev, err := job.Stats()
	if err != nil {
		return nil
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return out
		case <-tick.C:
		}
		now := time.Now()
		s, err := job.Stats()
		if err != nil {
			continue
		}
		out = append(out, ResPoint{
			T:      now.Sub(start).Seconds(),
			CPU:    CPUCores(prev.CPUTime, s.CPUTime, now.Sub(prevT)),
			MemMB:  mb(s.WorkingSet),
			PrivMB: mb(s.Private),
			Procs:  s.Processes,
		})
		prev, prevT = s, now
	}
}

// CPUCores converts a CPU-time delta over a wall-time delta into "cores busy".
func CPUCores(prev, cur, wall time.Duration) float64 {
	if wall <= 0 || cur < prev {
		return 0
	}
	return float64(cur-prev) / float64(wall)
}

func summarize(pts []ResPoint, idle float64) Resources {
	r := Resources{IdleMemMB: idle}
	// Skip the first second: it straddles warmup→measure.
	var n int
	for _, p := range pts {
		if p.T < 1 {
			continue
		}
		n++
		r.AvgCPU += p.CPU
		r.AvgMemMB += p.MemMB
		r.PeakCPU = max(r.PeakCPU, p.CPU)
		r.PeakMemMB = max(r.PeakMemMB, p.MemMB)
		r.Processes = max(r.Processes, p.Procs)
	}
	if n > 0 {
		r.AvgCPU /= float64(n)
		r.AvgMemMB /= float64(n)
	}
	return r
}

func mb(b uint64) float64 { return float64(b) / (1 << 20) }

func portInUse(port int) bool {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func writeSeriesCSV(path string, pts []ResPoint) {
	var b strings.Builder
	b.WriteString("t_s,cpu_cores,working_set_mb,private_mb,processes\n")
	for _, p := range pts {
		fmt.Fprintf(&b, "%.3f,%.4f,%.2f,%.2f,%d\n", p.T, p.CPU, p.MemMB, p.PrivMB, p.Procs)
	}
	_ = os.WriteFile(path, []byte(b.String()), 0o644)
}

func printResult(w io.Writer, r *Result) {
	if r.Status != StatusOK && r.K6 == nil {
		fmt.Fprintf(w, "  %s: %s\n", strings.ToUpper(r.Status), r.Error)
		return
	}
	k := r.K6
	if k == nil {
		k = &k6.Summary{}
	}
	fmt.Fprintf(w, "  %-7s %9.0f rps  p50 %.2f  p95 %.2f  p99 %.2f  max %.2f ms  errors %.0f  cpu %.2f cores  mem %.0f MB\n",
		strings.ToUpper(r.Status), k.RPS, k.P50Ms, k.P95Ms, k.P99Ms, k.MaxMs, k.BadResponses,
		r.Resources.AvgCPU, r.Resources.PeakMemMB)
	if r.Error != "" {
		fmt.Fprintf(w, "  note: %s\n", r.Error)
	}
}

// WriteRun saves summary.json for the run directory.
func WriteRun(dir string, run *Run) error {
	b, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "summary.json"), b, 0o644)
}

// ReadRun loads summary.json from a run directory.
func ReadRun(dir string) (*Run, error) {
	b, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		return nil, err
	}
	run := &Run{}
	return run, json.Unmarshal(b, run)
}
