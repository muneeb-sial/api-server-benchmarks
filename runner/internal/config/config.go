// Package config loads bench.config.yaml and the per-server bench.json manifests.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Settings are the per-server knobs. Zero values mean "inherit from defaults".
type Settings struct {
	MemoryMB   int    `yaml:"memory_mb" json:"memory_mb"`
	Workers    int    `yaml:"workers" json:"workers"`
	ServerCPUs string `yaml:"server_cpus" json:"server_cpus"`
	Disabled   bool   `yaml:"disabled" json:"disabled"`
}

type K6 struct {
	VUs        int    `yaml:"vus" json:"vus"`
	Rate       int    `yaml:"rate" json:"rate"`
	Duration   string `yaml:"duration" json:"duration"`
	Warmup     string `yaml:"warmup" json:"warmup"`
	Executor   string `yaml:"executor" json:"executor"`
	CPUs       string `yaml:"cpus" json:"cpus"`
	Dashboard  bool   `yaml:"web_dashboard" json:"web_dashboard"`
	APIAddress string `yaml:"api_address" json:"api_address"`
}

type Config struct {
	Port           int                 `yaml:"port"`
	Cooldown       string              `yaml:"cooldown"`
	StartupTimeout string              `yaml:"startup_timeout"`
	SampleInterval string              `yaml:"sample_interval"`
	Scenarios      []string            `yaml:"scenarios"`
	Defaults       Settings            `yaml:"defaults"`
	K6             K6                  `yaml:"k6"`
	Servers        map[string]Settings `yaml:"servers"`
}

// Load reads the YAML config and fills in defaults for anything left out.
func Load(file string) (*Config, error) {
	c := &Config{}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	c.applyDefaults()
	return c, c.validate()
}

func (c *Config) applyDefaults() {
	if c.Port == 0 {
		c.Port = 3000
	}
	if c.Cooldown == "" {
		c.Cooldown = "5s"
	}
	if c.StartupTimeout == "" {
		c.StartupTimeout = "60s"
	}
	if c.SampleInterval == "" {
		c.SampleInterval = "250ms"
	}
	if len(c.Scenarios) == 0 {
		c.Scenarios = []string{"text", "json"}
	}
	if c.Defaults.MemoryMB == 0 {
		c.Defaults.MemoryMB = 4096
	}
	if c.Defaults.Workers == 0 {
		c.Defaults.Workers = runtime.NumCPU()
	}
	if c.K6.VUs == 0 {
		c.K6.VUs = 100
	}
	if c.K6.Duration == "" {
		c.K6.Duration = "30s"
	}
	if c.K6.Warmup == "" {
		c.K6.Warmup = "10s"
	}
	if c.K6.Executor == "" {
		c.K6.Executor = "constant-vus"
	}
	if c.K6.APIAddress == "" {
		c.K6.APIAddress = "127.0.0.1:6565"
	}
}

func (c *Config) validate() error {
	for _, d := range []string{c.Cooldown, c.StartupTimeout, c.SampleInterval, c.K6.Duration, c.K6.Warmup} {
		if _, err := time.ParseDuration(d); err != nil {
			return fmt.Errorf("invalid duration %q: %w", d, err)
		}
	}
	for _, s := range c.Scenarios {
		if s != "text" && s != "json" {
			return fmt.Errorf("unknown scenario %q (want text or json)", s)
		}
	}
	if c.K6.Executor != "constant-vus" && c.K6.Executor != "constant-arrival-rate" {
		return fmt.Errorf("unknown k6 executor %q", c.K6.Executor)
	}
	if c.K6.Executor == "constant-arrival-rate" && c.K6.Rate <= 0 {
		return fmt.Errorf("k6.rate must be > 0 for constant-arrival-rate")
	}
	if _, err := ParseCPUs(c.Defaults.ServerCPUs); err != nil {
		return fmt.Errorf("defaults.server_cpus: %w", err)
	}
	if _, err := ParseCPUs(c.K6.CPUs); err != nil {
		return fmt.Errorf("k6.cpus: %w", err)
	}
	return nil
}

// For merges the defaults with the overrides for one server name ("node/fastify").
func (c *Config) For(name string) Settings {
	s := c.Defaults
	o, ok := c.Servers[name]
	if !ok {
		return s
	}
	if o.MemoryMB != 0 {
		s.MemoryMB = o.MemoryMB
	}
	if o.Workers != 0 {
		s.Workers = o.Workers
	}
	if o.ServerCPUs != "" {
		s.ServerCPUs = o.ServerCPUs
	}
	s.Disabled = o.Disabled
	return s
}

// OverrideMemory sets one memory cap for every server, replacing per-server overrides.
func (c *Config) OverrideMemory(mb int) {
	c.Defaults.MemoryMB = mb
	for name, s := range c.Servers {
		s.MemoryMB = 0
		c.Servers[name] = s
	}
}

// ParseMemoryMB reads a size in MB: "4096", "512m", "4g", "1.5gb".
func ParseMemoryMB(s string) (int, error) {
	t := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), "b")
	mult := 1.0
	switch {
	case strings.HasSuffix(t, "g"):
		mult, t = 1024, strings.TrimSuffix(t, "g")
	case strings.HasSuffix(t, "m"):
		t = strings.TrimSuffix(t, "m")
	}
	n, err := strconv.ParseFloat(t, 64)
	if err != nil || n*mult < 1 {
		return 0, fmt.Errorf("bad memory size %q (want e.g. 4096, 512m or 4g)", s)
	}
	return int(n * mult), nil
}

// Manifest is a server's bench.json.
type Manifest struct {
	Name string `json:"-"`
	Dir  string `json:"-"`

	// Build steps, each run through the shell in Dir.
	Build []string `json:"build"`
	// Start is the argv of the server process; ${VARS} are expanded (see Vars).
	Start []string `json:"start"`
	// Env is added to the server's environment; ${VARS} are expanded.
	Env map[string]string `json:"env"`
	// MaxWorkers caps WORKERS for runtimes that cannot use more processes/threads
	// on this OS (0 = no cap). The effective value is recorded in the results.
	MaxWorkers int `json:"maxWorkers"`
	// Notes are shown in the report next to the result.
	Notes string `json:"notes"`
	// Docker overrides fields for `bench run -docker`, where the server runs on Linux.
	Docker *DockerOverrides `json:"docker"`
}

// DockerOverrides replace manifest fields in Docker mode; unset fields keep the native value.
type DockerOverrides struct {
	// MaxWorkers replaces the cap; 0 lifts it (e.g. cluster/SO_REUSEPORT exist on Linux).
	MaxWorkers *int   `json:"maxWorkers"`
	Notes      string `json:"notes"`
}

// InDocker returns the manifest with its Docker overrides applied.
func (m *Manifest) InDocker() *Manifest {
	if m.Docker == nil {
		return m
	}
	d := *m
	if m.Docker.MaxWorkers != nil {
		d.MaxWorkers = *m.Docker.MaxWorkers
	}
	if m.Docker.Notes != "" {
		d.Notes = m.Docker.Notes
	}
	return &d
}

// Discover finds every <runtime>/<framework>/bench.json under root.
func Discover(root string) ([]*Manifest, error) {
	files, err := filepath.Glob(filepath.Join(root, "*", "*", "bench.json"))
	if err != nil {
		return nil, err
	}
	var out []*Manifest
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		m := &Manifest{}
		if err := json.Unmarshal(data, m); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		dir := filepath.Dir(f)
		m.Dir = dir
		m.Name = filepath.Base(filepath.Dir(dir)) + "/" + filepath.Base(dir)
		if len(m.Start) == 0 {
			return nil, fmt.Errorf("%s: start is empty", f)
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Filter keeps manifests whose name matches any of the comma-separated glob
// patterns (e.g. "node/*,rust/axum"). An empty filter keeps everything.
func Filter(ms []*Manifest, filter string) ([]*Manifest, error) {
	if strings.TrimSpace(filter) == "" {
		return ms, nil
	}
	pats := strings.Split(filter, ",")
	var out []*Manifest
	for _, m := range ms {
		for _, p := range pats {
			p = strings.TrimSpace(p)
			if !strings.Contains(p, "/") {
				p += "/*"
			}
			ok, err := path.Match(p, m.Name)
			if err != nil {
				return nil, fmt.Errorf("bad filter %q: %w", p, err)
			}
			if ok {
				out = append(out, m)
				break
			}
		}
	}
	return out, nil
}

// EffectiveWorkers applies the manifest's MaxWorkers cap.
func (m *Manifest) EffectiveWorkers(requested int) int {
	if m.MaxWorkers > 0 && requested > m.MaxWorkers {
		return m.MaxWorkers
	}
	return requested
}

// Dockerfile returns the path of the server's Dockerfile, or "" if it has none.
func (m *Manifest) Dockerfile() string {
	p := filepath.Join(m.Dir, "Dockerfile")
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		return ""
	}
	return p
}

// Vars are the substitutions available in start/env:
//
//	HOST (the address to listen on), PORT, WORKERS, MEMORY_MB, HEAP_MB (80% of the cap),
//	HEAP_BYTES, WORKER_HEAP_MB (HEAP_MB / WORKERS), WORKER_HEAP_BYTES
//
// They are also exported to the server's environment.
func Vars(host string, port, workers, memoryMB int) map[string]string {
	heap := memoryMB * 8 / 10
	perWorker := heap
	if workers > 1 {
		perWorker = heap / workers
	}
	return map[string]string{
		"HOST":              host,
		"PORT":              strconv.Itoa(port),
		"WORKERS":           strconv.Itoa(workers),
		"MEMORY_MB":         strconv.Itoa(memoryMB),
		"HEAP_MB":           strconv.Itoa(heap),
		"HEAP_BYTES":        strconv.FormatInt(int64(heap)<<20, 10),
		"WORKER_HEAP_MB":    strconv.Itoa(perWorker),
		"WORKER_HEAP_BYTES": strconv.FormatInt(int64(perWorker)<<20, 10),
	}
}

// Expand replaces ${NAME} with vars[NAME]; unknown names are left untouched.
func Expand(s string, vars map[string]string) string {
	return os.Expand(s, func(k string) string {
		if v, ok := vars[k]; ok {
			return v
		}
		return "${" + k + "}"
	})
}

// ParseCPUs turns "0-5,8,10-11" into an affinity bitmask. Empty means 0 (no pinning).
func ParseCPUs(spec string) (uint64, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, nil
	}
	var mask uint64
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		lo, hi, isRange := strings.Cut(part, "-")
		a, err := strconv.Atoi(strings.TrimSpace(lo))
		if err != nil {
			return 0, fmt.Errorf("bad cpu %q", part)
		}
		b := a
		if isRange {
			if b, err = strconv.Atoi(strings.TrimSpace(hi)); err != nil {
				return 0, fmt.Errorf("bad cpu range %q", part)
			}
		}
		if a < 0 || b < a || b > 63 {
			return 0, fmt.Errorf("cpu range %q out of bounds (0-63)", part)
		}
		for i := a; i <= b; i++ {
			mask |= 1 << uint(i)
		}
	}
	return mask, nil
}

// CountCPUs returns the number of CPUs in a mask, or NumCPU when the mask is 0.
func CountCPUs(mask uint64) int {
	if mask == 0 {
		return runtime.NumCPU()
	}
	n := 0
	for ; mask != 0; mask &= mask - 1 {
		n++
	}
	return n
}
