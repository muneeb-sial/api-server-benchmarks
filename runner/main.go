// Command runner builds and benchmarks the hello-world servers in this repo.
//
//	bench doctor
//	bench list   [-filter node/*]
//	bench build  [-filter node/*]
//	bench run    [-filter rust/*,go/gin] [-scenario text,json] [-duration 30s] [-vus 100]
//	bench report results/<run>
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/muneeb-sial/api-benchmarks/runner/internal/bench"
	"github.com/muneeb-sial/api-benchmarks/runner/internal/config"
	"github.com/muneeb-sial/api-benchmarks/runner/internal/report"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	root := fs.String("root", ".", "repo root (contains bench.config.yaml and the runtime folders)")
	cfgPath := fs.String("config", "", "config file (default <root>/bench.config.yaml)")
	filter := fs.String("filter", "", "comma-separated server globs, e.g. node/*,rust/axum or just bun")
	scenarios := fs.String("scenario", "", "override scenarios: text,json")
	duration := fs.String("duration", "", "override k6 duration")
	warmup := fs.String("warmup", "", "override k6 warmup")
	vus := fs.Int("vus", 0, "override k6 VUs")
	out := fs.String("out", "", "results directory (default <root>/results/<timestamp>)")
	_ = fs.Parse(args)

	switch cmd {
	case "report":
		dir := fs.Arg(0)
		if dir == "" {
			fail(fmt.Errorf("usage: bench report results/<run>"))
		}
		path, err := report.Generate(dir)
		check(err)
		fmt.Println("report:", path)
		return
	case "doctor":
		doctor()
		return
	case "list", "build", "run":
	default:
		usage()
	}

	absRoot, err := filepath.Abs(*root)
	check(err)
	if *cfgPath == "" {
		*cfgPath = filepath.Join(absRoot, "bench.config.yaml")
	}
	cfg, err := config.Load(*cfgPath)
	check(err)
	all, err := config.Discover(absRoot)
	check(err)
	ms, err := config.Filter(all, *filter)
	check(err)
	if len(ms) == 0 {
		fail(fmt.Errorf("no servers match %q", *filter))
	}

	switch cmd {
	case "list":
		for _, m := range ms {
			s := cfg.For(m.Name)
			state := ""
			if s.Disabled {
				state = "  (disabled)"
			}
			fmt.Printf("%-26s mem %5d MB  workers %2d  cpus %-6s%s\n",
				m.Name, s.MemoryMB, m.EffectiveWorkers(s.Workers), orAll(s.ServerCPUs), state)
		}
	case "build":
		if failed := bench.Build(ms, os.Stdout); len(failed) > 0 {
			fail(fmt.Errorf("build failed for: %s", strings.Join(failed, ", ")))
		}
		fmt.Println("\nall builds ok")
	case "run":
		if *duration != "" {
			cfg.K6.Duration = *duration
		}
		if *warmup != "" {
			cfg.K6.Warmup = *warmup
		}
		if *vus > 0 {
			cfg.K6.VUs = *vus
		}
		sc := cfg.Scenarios
		if *scenarios != "" {
			sc = strings.Split(*scenarios, ",")
		}
		for _, d := range []string{cfg.K6.Duration, cfg.K6.Warmup} {
			if _, err := time.ParseDuration(d); err != nil {
				fail(fmt.Errorf("bad duration %q", d))
			}
		}
		dir := *out
		if dir == "" {
			dir = filepath.Join(absRoot, "results", time.Now().Format("2006-01-02_15-04-05"))
		}
		check(os.MkdirAll(dir, 0o755))

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		r := &bench.Runner{Cfg: cfg, Root: absRoot, OutDir: dir, Log: os.Stdout}
		_, runErr := r.RunAll(ctx, ms, sc)
		path, err := report.Generate(dir)
		check(err)
		fmt.Println("\nresults:", dir)
		fmt.Println("report: ", path)
		if runErr != nil {
			fail(runErr)
		}
	}
}

// doctor reports which toolchains are on PATH and their versions.
func doctor() {
	tools := [][]string{
		{"k6", "version"}, {"go", "version"}, {"cargo", "--version"}, {"node", "--version"},
		{"bun", "--version"}, {"deno", "--version"}, {"python", "--version"}, {"java", "-version"},
	}
	missing := 0
	for _, t := range tools {
		out, err := exec.Command(t[0], t[1:]...).CombinedOutput()
		if err != nil {
			fmt.Printf("  %-7s MISSING (%v)\n", t[0], err)
			missing++
			continue
		}
		first, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		fmt.Printf("  %-7s %s\n", t[0], strings.TrimSpace(first))
	}
	if missing > 0 {
		fmt.Printf("\n%d tool(s) missing; servers for those runtimes will fail to build.\n", missing)
	}
}

func orAll(s string) string {
	if s == "" {
		return "all"
	}
	return s
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: bench <doctor|list|build|run|report> [flags]

  doctor  check that k6 and every runtime toolchain is installed
  list    show discovered servers and their effective settings
  build   build every (or -filter'd) server in release mode
  run     benchmark servers, write results/<timestamp>/ and report.html
  report  regenerate report.html for a results directory

run "bench <command> -h" for flags`)
	os.Exit(2)
}

func check(err error) {
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
