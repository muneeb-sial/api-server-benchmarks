// Package k6 runs the k6 load generator and collects its results.
package k6

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/muneeb-sial/api-benchmarks/runner/internal/proc"
)

type Options struct {
	Script      string
	BaseURL     string
	Scenario    string
	Executor    string
	VUs         int
	Rate        int
	Duration    string
	SummaryPath string
	// APIAddress is k6's REST API, polled once a second for the time series.
	APIAddress string
	// Affinity pins k6 to CPUs so it does not compete with the server.
	Affinity uint64
	// DashboardExport, if set, saves k6's own HTML dashboard there.
	DashboardExport string
	Log             io.Writer
	// Poll enables time-series collection (off for warmup).
	Poll bool
}

// Point is one per-second sample of the load test.
type Point struct {
	T      float64 `json:"t"`      // seconds since k6 started
	RPS    float64 `json:"rps"`    // requests completed in this interval / interval
	AvgMs  float64 `json:"avg"`    // mean latency of requests in this interval
	P95Ms  float64 `json:"p95"`    // p95 since test start (k6 only exposes cumulative)
	Errors float64 `json:"errors"` // failed requests in this interval
	VUs    int     `json:"vus"`
}

type Summary struct {
	Requests     float64 `json:"requests"`
	RPS          float64 `json:"rps"`
	FailedReqs   float64 `json:"failed_requests"`
	FailedChecks float64 `json:"failed_checks"`
	BadResponses float64 `json:"bad_responses"` // responses failing any check
	MinMs        float64 `json:"min"`
	AvgMs        float64 `json:"avg"`
	P50Ms        float64 `json:"p50"`
	P90Ms        float64 `json:"p90"`
	P95Ms        float64 `json:"p95"`
	P99Ms        float64 `json:"p99"`
	MaxMs        float64 `json:"max"`
	DataReceived float64 `json:"data_received"`
}

// Run executes one k6 test and blocks until it finishes.
func Run(o Options) ([]Point, *Summary, error) {
	args := []string{"k6", "run", "--quiet", "--no-usage-report", "--address", o.APIAddress,
		"-e", "BASE_URL=" + o.BaseURL,
		"-e", "SCENARIO=" + o.Scenario,
		"-e", "EXECUTOR=" + o.Executor,
		"-e", "VUS=" + strconv.Itoa(o.VUs),
		"-e", "RATE=" + strconv.Itoa(o.Rate),
		"-e", "DURATION=" + o.Duration,
	}
	if o.SummaryPath != "" {
		args = append(args, "-e", "SUMMARY_PATH="+o.SummaryPath)
	}
	args = append(args, o.Script)

	var env []string
	if o.DashboardExport != "" {
		env = append(env, "K6_WEB_DASHBOARD=true", "K6_WEB_DASHBOARD_PERIOD=1s",
			"K6_WEB_DASHBOARD_EXPORT="+o.DashboardExport, "K6_WEB_DASHBOARD_OPEN=false")
	}
	log := o.Log
	if log == nil {
		log = io.Discard
	}
	job, err := proc.Start(proc.Options{Argv: args, Env: env, Stdout: log, Stderr: log, Affinity: o.Affinity})
	if err != nil {
		return nil, nil, fmt.Errorf("start k6: %w", err)
	}
	defer job.Close()

	var points []Point
	if o.Poll {
		points = poll("http://"+o.APIAddress+"/v1/metrics", job.Exited())
	}
	if err := job.Wait(); err != nil {
		return points, nil, fmt.Errorf("k6 exited: %w", err)
	}
	if o.SummaryPath == "" {
		return points, nil, nil
	}
	sum, err := ParseSummary(o.SummaryPath)
	return points, sum, err
}

type apiMetrics struct {
	Data []struct {
		ID         string `json:"id"`
		Attributes struct {
			Sample map[string]float64 `json:"sample"`
		} `json:"attributes"`
	} `json:"data"`
}

type cumulative struct {
	at       time.Time
	reqs     float64
	avg      float64
	failRate float64
}

func poll(url string, done <-chan struct{}) []Point {
	client := &http.Client{Timeout: 900 * time.Millisecond}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var points []Point
	var start time.Time
	var prev *cumulative
	for {
		select {
		case <-done:
			return points
		case <-tick.C:
		}
		cur, p95, vus, ok := fetch(client, url)
		if !ok {
			continue
		}
		if start.IsZero() {
			start = cur.at
		}
		if prev != nil && cur.reqs >= prev.reqs {
			dt := cur.at.Sub(prev.at).Seconds()
			n := cur.reqs - prev.reqs
			pt := Point{T: cur.at.Sub(start).Seconds(), P95Ms: p95, VUs: vus}
			if dt > 0 {
				pt.RPS = n / dt
			}
			if n > 0 {
				// Recover the interval mean from the running mean.
				pt.AvgMs = (cur.avg*cur.reqs - prev.avg*prev.reqs) / n
				pt.Errors = cur.failRate*cur.reqs - prev.failRate*prev.reqs
				if pt.Errors < 0 {
					pt.Errors = 0
				}
			}
			points = append(points, pt)
		}
		prev = &cur
	}
}

func fetch(client *http.Client, url string) (c cumulative, p95 float64, vus int, ok bool) {
	resp, err := client.Get(url)
	if err != nil {
		return c, 0, 0, false
	}
	defer resp.Body.Close()
	var m apiMetrics
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return c, 0, 0, false
	}
	c.at = time.Now()
	for _, d := range m.Data {
		s := d.Attributes.Sample
		switch d.ID {
		case "http_reqs":
			c.reqs = s["count"]
		case "http_req_duration":
			c.avg = s["avg"]
			p95 = s["p(95)"]
		case "http_req_failed":
			c.failRate = s["rate"]
		case "vus":
			vus = int(s["value"])
		}
	}
	return c, p95, vus, true
}

type summaryFile struct {
	Metrics map[string]struct {
		Values map[string]float64 `json:"values"`
	} `json:"metrics"`
}

// ParseSummary reads the JSON written by the script's handleSummary.
func ParseSummary(path string) (*Summary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f summaryFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	v := func(metric, key string) float64 { return f.Metrics[metric].Values[key] }
	return &Summary{
		Requests:     v("http_reqs", "count"),
		RPS:          v("http_reqs", "rate"),
		FailedReqs:   v("http_req_failed", "passes"),
		FailedChecks: v("checks", "fails"),
		BadResponses: v("bad_responses", "count"),
		MinMs:        v("http_req_duration", "min"),
		AvgMs:        v("http_req_duration", "avg"),
		P50Ms:        v("http_req_duration", "p(50)"),
		P90Ms:        v("http_req_duration", "p(90)"),
		P95Ms:        v("http_req_duration", "p(95)"),
		P99Ms:        v("http_req_duration", "p(99)"),
		MaxMs:        v("http_req_duration", "max"),
		DataReceived: v("data_received", "count"),
	}, nil
}
