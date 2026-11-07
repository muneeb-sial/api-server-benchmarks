// Package docker runs a server inside a Linux container instead of on the host, so
// a machine without every runtime toolchain can still benchmark it. The container
// gets the same memory cap and CPU set a native server would, and is measured
// through its own cgroup v2 counters.
package docker

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/muneeb-sial/api-benchmarks/runner/internal/proc"
)

// label marks every image and container the runner creates.
const label = "api-bench=1"

// Image is the local image tag of a server: "rust/axum" -> "api-bench/rust-axum".
func Image(server string) string { return "api-bench/" + slug(server) }

func containerName(server string) string { return "api-bench-" + slug(server) }

func slug(server string) string { return strings.ReplaceAll(server, "/", "-") }

// Check returns the Docker engine version, or an error when the CLI is missing
// or the engine is not running.
func Check() (string, error) {
	out, err := exec.Command("docker", "version", "--format", "{{.Server.Version}}").CombinedOutput()
	if err != nil {
		msg, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("docker is not available: %s", msg)
	}
	return strings.TrimSpace(string(out)), nil
}

// Build builds the Dockerfile in dir into Image(server). Docker's layer cache
// makes a rebuild of unchanged sources take a second or two.
func Build(server, dir string, out io.Writer) error {
	cmd := exec.Command("docker", "build", "--tag", Image(server), "--label", label, dir)
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}

type Options struct {
	Server string
	// Env is passed to the container as KEY=VALUE.
	Env  []string
	Port int
	// MemoryMB is the hard cap for the container, swap included (0 = none).
	MemoryMB int
	// CPUs is a cpuset such as "0-5" ("" = all CPUs of the Docker VM).
	CPUs   string
	Stdout io.Writer
	Stderr io.Writer
}

// Container is a running server container. It satisfies the same contract as
// proc.Job: Exited, MemoryLimitHit, AbnormalExit, Stats and Close.
type Container struct {
	name   string
	exited chan struct{}
	runErr error

	mu          sync.Mutex
	probe       *probe
	oomKills    int
	inspectOnce sync.Once
	inspectOOM  bool
}

// Start runs the server's image attached, so the container's output goes to
// Stdout/Stderr and Exited fires when it stops. The port is published on
// 127.0.0.1 only.
func Start(o Options) (*Container, error) {
	name := containerName(o.Server)
	// A container left behind by an interrupted run would hold the name and port.
	_ = exec.Command("docker", "rm", "--force", name).Run()

	args := []string{"run", "--name", name, "--label", label,
		"--publish", fmt.Sprintf("127.0.0.1:%d:%d", o.Port, o.Port)}
	if o.MemoryMB > 0 {
		m := strconv.Itoa(o.MemoryMB) + "m"
		args = append(args, "--memory", m, "--memory-swap", m) // swap limit = memory limit: no swap
	}
	if o.CPUs != "" {
		args = append(args, "--cpuset-cpus", o.CPUs)
	}
	for _, e := range o.Env {
		args = append(args, "--env", e)
	}
	args = append(args, Image(o.Server))

	cmd := exec.Command("docker", args...)
	cmd.Stdout = o.Stdout
	cmd.Stderr = o.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &Container{name: name, exited: make(chan struct{})}
	go func() {
		c.runErr = cmd.Wait()
		close(c.exited)
	}()
	return c, nil
}

// Exited is closed when the container stops.
func (c *Container) Exited() <-chan struct{} { return c.exited }

// MemoryLimitHit reports whether the kernel OOM-killed any process in the
// container (a cluster worker, or the server itself).
func (c *Container) MemoryLimitHit() bool {
	c.mu.Lock()
	kills := c.oomKills
	c.mu.Unlock()
	if kills > 0 {
		return true
	}
	select {
	case <-c.exited:
		// The probe died with the container; ask the engine instead.
		c.inspectOnce.Do(func() {
			out, _ := exec.Command("docker", "inspect", "--format", "{{.State.OOMKilled}}", c.name).Output()
			c.inspectOOM = strings.TrimSpace(string(out)) == "true"
		})
		return c.inspectOOM
	default:
		return false
	}
}

// AbnormalExit reports whether the container stopped with a non-zero exit code.
func (c *Container) AbnormalExit() bool {
	select {
	case <-c.exited:
		return c.runErr != nil
	default:
		return false
	}
}

// Stats samples the container's cgroup. The first call starts the probe, so call
// it only once the server is up.
func (c *Container) Stats() (proc.Stats, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.exited:
		return proc.Stats{}, errors.New("container exited")
	default:
	}
	if c.probe == nil {
		p, err := startProbe(c.name)
		if err != nil {
			return proc.Stats{}, err
		}
		c.probe = p
	}
	s, err := c.probe.sample()
	if err != nil {
		c.probe.close()
		c.probe = nil
		return proc.Stats{}, err
	}
	c.oomKills = max(c.oomKills, s.oomKills)
	return s.stats(), nil
}

// Close removes the container and waits for `docker run` to return.
func (c *Container) Close() {
	c.mu.Lock()
	if c.probe != nil {
		c.probe.close()
		c.probe = nil
	}
	c.mu.Unlock()
	_ = exec.Command("docker", "rm", "--force", c.name).Run()
	select {
	case <-c.exited:
	case <-time.After(15 * time.Second):
	}
}

// ---------------------------------------------------------------- cgroup probe

// probeScript runs inside the container and prints its cgroup v2 counters each
// time it reads a line, so a sample is taken exactly when the runner asks for
// one. Its cost (one grep per sample) is charged to the container.
const probeScript = `cd /sys/fs/cgroup || exit 1
while read -r _; do
  grep -s -E '^(usage_usec|anon|inactive_file|oom_kill) |^[0-9]+$' cpu.stat memory.stat memory.current memory.peak memory.events cgroup.procs
  echo .
done`

// probeProcs is the number of probe processes alive while grep reads
// cgroup.procs (the shell and grep), which are not part of the server.
const probeProcs = 2

type probe struct {
	cmd   *exec.Cmd
	in    io.WriteCloser
	snaps chan cgroupSample
}

func startProbe(container string) (*probe, error) {
	cmd := exec.Command("docker", "exec", "--interactive", container, "sh", "-c", probeScript)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("docker exec: %w", err)
	}
	p := &probe{cmd: cmd, in: in, snaps: make(chan cgroupSample, 1)}
	go p.read(out)
	return p, nil
}

func (p *probe) read(out io.Reader) {
	defer close(p.snaps)
	sc := bufio.NewScanner(out)
	var lines []string
	for sc.Scan() {
		if line := sc.Text(); line != "." {
			lines = append(lines, line)
			continue
		}
		s := parseSample(lines)
		lines = lines[:0]
		select {
		case p.snaps <- s:
		default: // nobody waiting (a timed-out request); drop it
		}
	}
}

func (p *probe) sample() (cgroupSample, error) {
	// Discard a late answer to an earlier, timed-out request.
	select {
	case _, ok := <-p.snaps:
		if !ok {
			return cgroupSample{}, errors.New("cgroup probe exited")
		}
	default:
	}
	if _, err := io.WriteString(p.in, "\n"); err != nil {
		return cgroupSample{}, fmt.Errorf("cgroup probe: %w", err)
	}
	select {
	case s, ok := <-p.snaps:
		if !ok {
			return cgroupSample{}, errors.New("cgroup probe exited")
		}
		if s.usageUsec == 0 {
			return s, errors.New("cgroup probe: no cgroup v2 counters in the container")
		}
		return s, nil
	case <-time.After(2 * time.Second):
		return cgroupSample{}, errors.New("cgroup probe timed out")
	}
}

func (p *probe) close() {
	_ = p.in.Close()
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	_ = p.cmd.Wait()
}

type cgroupSample struct {
	usageUsec    uint64
	current      uint64
	peak         uint64
	anon         uint64
	inactiveFile uint64
	oomKills     int
	procs        int
}

// parseSample reads grep's "file:line" output from probeScript.
func parseSample(lines []string) cgroupSample {
	var s cgroupSample
	for _, line := range lines {
		file, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, val, hasKey := strings.Cut(rest, " ")
		if !hasKey {
			key, val = "", rest
		}
		n, err := strconv.ParseUint(strings.TrimSpace(val), 10, 64)
		if err != nil {
			continue
		}
		switch file + ":" + key {
		case "cpu.stat:usage_usec":
			s.usageUsec = n
		case "memory.current:":
			s.current = n
		case "memory.peak:":
			s.peak = n
		case "memory.stat:anon":
			s.anon = n
		case "memory.stat:inactive_file":
			s.inactiveFile = n
		case "memory.events:oom_kill":
			s.oomKills = int(n)
		case "cgroup.procs:":
			s.procs++
		}
	}
	return s
}

func (s cgroupSample) stats() proc.Stats {
	st := proc.Stats{
		CPUTime:   time.Duration(s.usageUsec) * time.Microsecond,
		Processes: max(s.procs-probeProcs, 0),
		// Like `docker stats`: charged memory minus page cache the kernel can drop.
		WorkingSet: s.current,
		Private:    s.anon,
		// memory.peak is what the --memory cap is enforced against (needs Linux 5.19+).
		PeakCommit: max(s.peak, s.current),
	}
	if s.inactiveFile < s.current {
		st.WorkingSet = s.current - s.inactiveFile
	}
	return st
}
