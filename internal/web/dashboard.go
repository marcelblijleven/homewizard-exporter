package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/marcelblijleven/homewizard_exporter/internal/snapshot"
)

//go:embed assets
var assets embed.FS

// dashboard serves the overview page, its assets and the JSON it reads.
type dashboard struct {
	page        *template.Template
	store       *snapshot.Store
	staleAfter  time.Duration
	refresh     time.Duration
	prefix      string
	metricsPath string
	logger      *slog.Logger
}

func newDashboard(opts Options) (*dashboard, error) {
	page, err := template.ParseFS(assets, "assets/dashboard.html")
	if err != nil {
		return nil, fmt.Errorf("parse dashboard template: %w", err)
	}

	prefix := opts.Dashboard.Path
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	return &dashboard{
		page:        page,
		store:       opts.Store,
		staleAfter:  opts.StaleAfter,
		refresh:     opts.Dashboard.Refresh.Duration(),
		prefix:      prefix,
		metricsPath: opts.Server.MetricsPath,
		logger:      opts.Logger,
	}, nil
}

// register mounts the dashboard routes on mux.
func (d *dashboard) register(mux *http.ServeMux) {
	mux.HandleFunc("GET "+d.prefix+"{$}", d.handlePage)
	mux.HandleFunc("GET "+d.prefix+"api/snapshot", d.handleSnapshot)
	mux.Handle("GET "+d.prefix+"assets/", http.StripPrefix(d.prefix, http.FileServer(http.FS(assets))))
}

func (d *dashboard) handlePage(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Prefix         string
		MetricsPath    string
		RefreshSeconds int
	}{
		Prefix:         d.prefix,
		MetricsPath:    d.metricsPath,
		RefreshSeconds: int(d.refresh.Seconds()),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := d.page.Execute(w, data); err != nil {
		d.logger.Error("render dashboard", "error", err)
	}
}

func (d *dashboard) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	view := buildView(d.store.Load(), d.staleAfter, time.Now())

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(view); err != nil {
		d.logger.Debug("write snapshot", "error", err)
	}
}
