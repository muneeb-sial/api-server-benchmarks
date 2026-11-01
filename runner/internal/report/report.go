// Package report renders a results directory into a self-contained report.html.
package report

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/muneeb-sial/api-benchmarks/runner/internal/bench"
)

//go:embed template.html
var tmpl string

// Generate reads <dir>/summary.json and writes <dir>/report.html.
func Generate(dir string) (string, error) {
	run, err := bench.ReadRun(dir)
	if err != nil {
		return "", err
	}
	// encoding/json escapes <, > and &, so the payload is safe inside <script>.
	data, err := json.Marshal(run)
	if err != nil {
		return "", err
	}
	html := strings.Replace(tmpl, "__BENCH_DATA__", string(data), 1)
	out := filepath.Join(dir, "report.html")
	return out, os.WriteFile(out, []byte(html), 0o644)
}
