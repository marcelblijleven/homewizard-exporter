// Package web serves the Prometheus endpoint, a health check and (optionally)
// the built-in dashboard.
package web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/marcelblijleven/homewizard_exporter/internal/buildinfo"
	"github.com/marcelblijleven/homewizard_exporter/internal/config"
	"github.com/marcelblijleven/homewizard_exporter/internal/snapshot"
)

// Options configures a Server.
type Options struct {
	Server    config.Server
	Dashboard config.Dashboard
	Registry  *prometheus.Registry
	Logger    *slog.Logger

	Store      *snapshot.Store
	StaleAfter time.Duration

	Healthz func() error
}

// Server is the HTTP frontend. The listener is bound during New so that a port
// clash is reported before any polling starts.
type Server struct {
	srv      *http.Server
	listener net.Listener
	log      *slog.Logger
}

// New binds the configured address and builds the request router.
func New(opts Options) (*Server, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Healthz == nil {
		opts.Healthz = func() error { return nil }
	}

	listener, err := net.Listen("tcp", opts.Server.Listen)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", opts.Server.Listen, err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET "+opts.Server.MetricsPath, promhttp.HandlerFor(opts.Registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
		Registry:      opts.Registry,
	}))
	mux.HandleFunc("GET /healthz", healthz(opts.Healthz))

	switch {
	case opts.Dashboard.Enabled:
		if opts.Store == nil {
			_ = listener.Close()
			return nil, fmt.Errorf("dashboard is enabled but no snapshot store was supplied")
		}
		dash, err := newDashboard(opts)
		if err != nil {
			_ = listener.Close()
			return nil, err
		}
		dash.register(mux)

	case opts.Server.MetricsPath != "/":
		// Without the dashboard, root points at /metrics and says how to turn the
		// convention every other exporter follows.
		mux.HandleFunc("GET /{$}", landing(opts.Server.MetricsPath))
	}

	s := &Server{
		listener: listener,
		log:      opts.Logger,
		srv: &http.Server{
			Handler:           logRequests(opts.Logger, mux),
			ReadHeaderTimeout: opts.Server.ReadTimeout.Duration(),
			ErrorLog:          slog.NewLogLogger(opts.Logger.Handler(), slog.LevelWarn),
		},
	}
	return s, nil
}

// Addr reports the bound address
func (s *Server) Addr() string { return s.listener.Addr().String() }

// Run serves until ctx is cancelled, then drains in-flight requests.
func (s *Server) Run(ctx context.Context) error {
	errc := make(chan error, 1)
	go func() {
		s.log.Info("http server listening", "addr", s.Addr())
		errc <- s.srv.Serve(s.listener)
	}()

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	s.log.Info("shutting down http server")
	if err := s.srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

func healthz(check func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if err := check(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintln(w, err.Error())
			return
		}
		fmt.Fprintln(w, "ok")
	}
}

// landingPage is served at root when the dashboard is switched off.
const landingPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>homewizard_exporter</title>
<style>
:root { color-scheme: light dark; --bg: #fbfbfa; --fg: #1c1b1a; --muted: #6b6764; --line: #e3e0dc; }
@media (prefers-color-scheme: dark) {
  :root { --bg: #17161a; --fg: #eceae6; --muted: #97918c; --line: #2e2c31; }
}
body {
  margin: 0; padding: 3rem 1.5rem; background: var(--bg); color: var(--fg);
  font: 15px/1.6 ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
}
main { max-width: 34rem; margin: 0 auto; }
h1 { margin: 0 0 0.25rem; font-size: 1.25rem; letter-spacing: -0.01em; }
.version { color: var(--muted); font-size: 0.8rem; margin: 0 0 2rem; }
ul { list-style: none; padding: 0; margin: 0 0 2rem; }
li { padding: 0.6rem 0; border-top: 1px solid var(--line); }
li:last-child { border-bottom: 1px solid var(--line); }
a { color: inherit; }
.hint { color: var(--muted); font-size: 0.85rem; border-left: 2px solid var(--line); padding-left: 0.9rem; }
code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 0.85em; }
</style>
</head>
<body>
<main>
  <h1>homewizard_exporter</h1>
  <p class="version">{{ .Version }}</p>
  <ul>
    <li><a href="{{ .MetricsPath }}">{{ .MetricsPath }}</a>: Prometheus metrics</li>
    <li><a href="/healthz">/healthz</a>: readiness</li>
  </ul>
  <p class="hint">
    There is a built-in dashboard showing every device at a glance. It is off by
    default; set <code>dashboard.enabled: true</code> in the config, or
    <code>HOMEWIZARD_DASHBOARD=true</code>, and it appears here.
  </p>
</main>
</body>
</html>
`

func landing(metricsPath string) http.HandlerFunc {
	page, err := template.New("landing").Parse(landingPage)
	if err != nil {
		panic("landing page template: " + err.Error())
	}

	var rendered bytes.Buffer
	data := struct{ Version, MetricsPath string }{buildinfo.Get().String(), metricsPath}
	if err := page.Execute(&rendered, data); err != nil {
		panic("landing page render: " + err.Error())
	}
	body := rendered.Bytes()

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(body)
	}
}

func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Debug(
			"request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
