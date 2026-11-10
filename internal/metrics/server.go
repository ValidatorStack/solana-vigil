package metrics

import (
	"fmt"
	"net/http"
	"time"
)

func Handler(collector *Collector, readinessAge time.Duration) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		snapshot, err := collector.Snapshot(r.Context())
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = fmt.Fprint(w, Render(snapshot, err == nil, time.Since(start)))
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if !collector.Ready(readinessAge) {
			http.Error(w, "no fresh snapshot; scrape /metrics or check validator RPC\n", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ready\n"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><title>Vigil exporter</title><h1>Vigil</h1><ul><li><a href=\"/metrics\">Prometheus metrics</a></li><li><a href=\"/healthz\">Health</a></li><li><a href=\"/readyz\">Readiness</a></li></ul>"))
	})
	return mux
}
