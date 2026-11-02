package main

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

type requestKey struct {
	path   string
	status int
}

type metricsRegistry struct {
	start time.Time

	mu       sync.Mutex
	requests map[requestKey]int64
}

func newMetricsRegistry() *metricsRegistry {
	return &metricsRegistry{
		start:    time.Now(),
		requests: make(map[requestKey]int64),
	}
}

func (m *metricsRegistry) observe(path string, status int) {
	m.mu.Lock()
	m.requests[requestKey{path: path, status: status}]++
	m.mu.Unlock()
}

func (m *metricsRegistry) render(w io.Writer) {
	m.mu.Lock()
	keys := make([]requestKey, 0, len(m.requests))
	counts := make(map[requestKey]int64, len(m.requests))
	for key, count := range m.requests {
		keys = append(keys, key)
		counts[key] = count
	}
	m.mu.Unlock()

	sort.Slice(keys, func(i, j int) bool {
		if keys[i].path == keys[j].path {
			return keys[i].status < keys[j].status
		}
		return keys[i].path < keys[j].path
	})

	fmt.Fprintf(w, "# HELP cmma_api_uptime_seconds Process uptime in seconds.\n")
	fmt.Fprintf(w, "# TYPE cmma_api_uptime_seconds gauge\n")
	fmt.Fprintf(w, "cmma_api_uptime_seconds %.0f\n", time.Since(m.start).Seconds())

	fmt.Fprintf(w, "# HELP cmma_api_requests_total HTTP requests by path and status.\n")
	fmt.Fprintf(w, "# TYPE cmma_api_requests_total counter\n")
	for _, key := range keys {
		fmt.Fprintf(w, "cmma_api_requests_total{path=%q,status=\"%d\"} %d\n",
			key.path, key.status, counts[key])
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(data)
}

func (s *apiServer) metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	s.metrics.render(w)
}
