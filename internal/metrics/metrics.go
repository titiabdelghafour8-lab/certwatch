// Package metrics is a tiny, dependency-free Prometheus text exporter for
// counters. It covers what certwatch needs without pulling in client_golang.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type Registry struct {
	mu      sync.Mutex
	help    map[string]string
	values  map[string]map[string]uint64 // name -> labels -> value
	started time.Time
}

func New() *Registry {
	return &Registry{
		help:    map[string]string{},
		values:  map[string]map[string]uint64{},
		started: time.Now(),
	}
}

// Describe sets the HELP text for a counter.
func (r *Registry) Describe(name, help string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.help[name] = help
	if r.values[name] == nil {
		r.values[name] = map[string]uint64{}
	}
}

// Inc increments counter name. kv are alternating label names and values.
func (r *Registry) Inc(name string, kv ...string) {
	if r == nil {
		return
	}
	labels := formatLabels(kv)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.values[name] == nil {
		r.values[name] = map[string]uint64{}
	}
	r.values[name][labels]++
}

// Get returns the current value of a counter (used in tests).
func (r *Registry) Get(name string, kv ...string) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.values[name][formatLabels(kv)]
}

// Write writes all metrics in Prometheus text exposition format.
func (r *Registry) Write(w io.Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.values))
	for n := range r.values {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if h := r.help[n]; h != "" {
			fmt.Fprintf(w, "# HELP %s %s\n", n, h)
		}
		fmt.Fprintf(w, "# TYPE %s counter\n", n)
		series := make([]string, 0, len(r.values[n]))
		for l := range r.values[n] {
			series = append(series, l)
		}
		sort.Strings(series)
		for _, l := range series {
			fmt.Fprintf(w, "%s%s %d\n", n, l, r.values[n][l])
		}
	}
	fmt.Fprintf(w, "# TYPE certwatch_goroutines gauge\ncertwatch_goroutines %d\n", runtime.NumGoroutine())
	fmt.Fprintf(w, "# TYPE certwatch_uptime_seconds gauge\ncertwatch_uptime_seconds %.0f\n", time.Since(r.started).Seconds())
}

// Handler serves the metrics. If token is non-empty a matching bearer token is required.
func (r *Registry) Handler(token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if token != "" && req.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		r.Write(w)
	})
}

func formatLabels(kv []string) string {
	if len(kv) < 2 {
		return ""
	}
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i+1 < len(kv); i += 2 {
		if i > 0 {
			b.WriteByte(',')
		}
		v := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(kv[i+1])
		fmt.Fprintf(&b, `%s="%s"`, kv[i], v)
	}
	b.WriteByte('}')
	return b.String()
}
