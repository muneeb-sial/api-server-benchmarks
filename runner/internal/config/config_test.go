package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseCPUs(t *testing.T) {
	cases := map[string]uint64{
		"":         0,
		"0":        1,
		"0-3":      0b1111,
		"1,3":      0b1010,
		"0-1, 6-7": 0b11000011,
	}
	for in, want := range cases {
		got, err := ParseCPUs(in)
		if err != nil || got != want {
			t.Errorf("ParseCPUs(%q) = %b, %v; want %b", in, got, err, want)
		}
	}
	for _, bad := range []string{"a", "3-1", "64", "-1"} {
		if _, err := ParseCPUs(bad); err == nil {
			t.Errorf("ParseCPUs(%q) should fail", bad)
		}
	}
	if CountCPUs(0b1011) != 3 {
		t.Error("CountCPUs")
	}
}

func TestForMergesOverrides(t *testing.T) {
	c := &Config{
		Defaults: Settings{MemoryMB: 4096, Workers: 6, ServerCPUs: "0-5"},
		Servers:  map[string]Settings{"java/springboot": {MemoryMB: 2048}, "bun/hono": {Disabled: true}},
	}
	got := c.For("java/springboot")
	if got.MemoryMB != 2048 || got.Workers != 6 || got.ServerCPUs != "0-5" {
		t.Errorf("override merge: %+v", got)
	}
	if !c.For("bun/hono").Disabled {
		t.Error("disabled not applied")
	}
	if c.For("go/gin") != c.Defaults {
		t.Error("defaults not applied")
	}
}

func TestVarsAndExpand(t *testing.T) {
	v := Vars("127.0.0.1", 3000, 4, 4000)
	if v["HEAP_MB"] != "3200" || v["WORKER_HEAP_MB"] != "800" {
		t.Errorf("vars: %v", v)
	}
	if got := Expand("--max-old-space-size=${WORKER_HEAP_MB} ${UNKNOWN}", v); got != "--max-old-space-size=800 ${UNKNOWN}" {
		t.Errorf("expand: %q", got)
	}
}

func TestDiscoverAndFilter(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"node/express", "node/fastify", "rust/axum"} {
		d := filepath.Join(root, filepath.FromSlash(n))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "bench.json"), []byte(`{"start":["x"],"maxWorkers":1}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ms, err := Discover(root)
	if err != nil || len(ms) != 3 || ms[0].Name != "node/express" {
		t.Fatalf("discover: %v %v", ms, err)
	}
	if ms[0].EffectiveWorkers(8) != 1 {
		t.Error("maxWorkers cap")
	}
	for filter, want := range map[string]int{"": 3, "node": 2, "rust/axum": 1, "node/f*,rust/*": 2, "go": 0} {
		got, err := Filter(ms, filter)
		if err != nil || len(got) != want {
			t.Errorf("Filter(%q) = %d, %v; want %d", filter, len(got), err, want)
		}
	}
}

func TestParseMemoryMB(t *testing.T) {
	cases := map[string]int{"4096": 4096, "512m": 512, "512MB": 512, "4g": 4096, "4GB": 4096, " 1.5g ": 1536}
	for in, want := range cases {
		if got, err := ParseMemoryMB(in); err != nil || got != want {
			t.Errorf("ParseMemoryMB(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "g", "-1g", "0", "lots"} {
		if _, err := ParseMemoryMB(bad); err == nil {
			t.Errorf("ParseMemoryMB(%q) should fail", bad)
		}
	}
	c := &Config{Defaults: Settings{MemoryMB: 4096}, Servers: map[string]Settings{"java/springboot": {MemoryMB: 2048, Workers: 2}}}
	c.OverrideMemory(1024)
	if got := c.For("java/springboot"); got.MemoryMB != 1024 || got.Workers != 2 {
		t.Errorf("OverrideMemory: %+v", got)
	}
}

func TestInDocker(t *testing.T) {
	var m Manifest
	data := `{"start": ["bun", "main.js"], "maxWorkers": 1, "notes": "single process",
		"docker": {"maxWorkers": 0, "notes": "node:cluster"}}`
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		t.Fatal(err)
	}
	d := m.InDocker()
	if d.EffectiveWorkers(6) != 6 || d.Notes != "node:cluster" {
		t.Errorf("docker overrides not applied: %+v", d)
	}
	if m.EffectiveWorkers(6) != 1 || m.Notes != "single process" {
		t.Errorf("native manifest changed: %+v", m)
	}
	plain := &Manifest{MaxWorkers: 2, Notes: "n"}
	if plain.InDocker() != plain {
		t.Error("manifest without overrides should be returned as is")
	}
	if v := Vars("0.0.0.0", 3000, 4, 4000); v["WORKER_HEAP_BYTES"] != "838860800" {
		t.Errorf("WORKER_HEAP_BYTES = %s", v["WORKER_HEAP_BYTES"])
	}
}
