package web

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/marcelblijleven/homewizard_exporter/internal/config"
	"github.com/marcelblijleven/homewizard_exporter/internal/homewizard"
	"github.com/marcelblijleven/homewizard_exporter/internal/snapshot"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func ptr[T any](v T) *T { return &v }

func newTestServer(t *testing.T, opts Options) *Server {
	t.Helper()

	if opts.Server.Listen == "" {
		opts.Server.Listen = "127.0.0.1:0" // let the OS pick, so tests can run in parallel
	}
	if opts.Server.MetricsPath == "" {
		opts.Server.MetricsPath = "/metrics"
	}
	if opts.Registry == nil {
		opts.Registry = prometheus.NewRegistry()
	}
	opts.Logger = discardLogger()

	server, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := t.Context(), func() {}
	t.Cleanup(cancel)
	go server.Run(ctx)

	return server
}

func get(t *testing.T, server *Server, path string) (int, string) {
	t.Helper()

	resp, err := http.Get("http://" + server.Addr() + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

func TestMetricsEndpoint(t *testing.T) {
	registry := prometheus.NewRegistry()
	registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "homewizard_test_metric",
		Help: "a metric",
	}, func() float64 { return 1 }))

	server := newTestServer(t, Options{Registry: registry})

	status, body := get(t, server, "/metrics")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if !strings.Contains(body, "homewizard_test_metric 1") {
		t.Errorf("body did not carry the metric:\n%s", body)
	}
}

func TestHealthz(t *testing.T) {
	var failure error
	server := newTestServer(t, Options{Healthz: func() error { return failure }})

	if status, body := get(t, server, "/healthz"); status != http.StatusOK {
		t.Errorf("healthy check gave %d: %s", status, body)
	}

	failure = errors.New("no device has fresh readings")
	status, body := get(t, server, "/healthz")
	if status != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", status)
	}
	if !strings.Contains(body, "no device has fresh readings") {
		t.Errorf("body = %q, want the reason", body)
	}
}

func TestLandingPage(t *testing.T) {
	server := newTestServer(t, Options{})

	status, body := get(t, server, "/")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}

	for _, want := range []string{
		`href="/metrics"`,
		`href="/healthz"`,
		"<style>",              // styled without the dashboard's assets, which are not mounted
		"dashboard.enabled",    // and says how to get the dashboard
		"HOMEWIZARD_DASHBOARD", // by either route
	} {
		if !strings.Contains(body, want) {
			t.Errorf("landing page is missing %q:\n%s", want, body)
		}
	}

	if strings.Contains(body, "assets/style.css") {
		t.Error("landing page links a stylesheet that is not served when the dashboard is off")
	}
	if status, _ := get(t, server, "/assets/style.css"); status == http.StatusOK {
		t.Error("dashboard assets should not be mounted when the dashboard is off")
	}
}

func TestLandingPageNotServedWithDashboard(t *testing.T) {
	store := &snapshot.Store{}
	store.Update("p1", func(d *snapshot.Device) { d.Host = "192.168.1.10" })

	server := newTestServer(t, Options{
		Dashboard:  config.Dashboard{Enabled: true, Path: "/", Refresh: config.Duration(time.Second)},
		Store:      store,
		StaleAfter: time.Minute,
	})

	_, body := get(t, server, "/")
	if strings.Contains(body, "dashboard.enabled") {
		t.Errorf("root served the landing page while the dashboard was on:\n%s", body)
	}
	if !strings.Contains(body, "assets/app.js") {
		t.Errorf("root should be the dashboard:\n%s", body)
	}
}

func TestDashboardNeedsAStore(t *testing.T) {
	_, err := New(Options{
		Server:    config.Server{Listen: "127.0.0.1:0", MetricsPath: "/metrics"},
		Dashboard: config.Dashboard{Enabled: true, Path: "/", Refresh: config.Duration(time.Second)},
		Registry:  prometheus.NewRegistry(),
		Logger:    discardLogger(),
	})
	if err == nil {
		t.Fatal("a dashboard with nothing to show should be refused at startup")
	}
}

func TestDashboardServesPageAndSnapshot(t *testing.T) {
	store := &snapshot.Store{}
	store.Update("p1", func(d *snapshot.Device) {
		d.Host = "192.168.1.10"
		d.APIVersion = config.APIv2
		d.Info = homewizard.Info{
			ProductType: homewizard.ProductP1,
			ProductName: "P1 Meter",
			Serial:      "aabbccddeeff",
		}
		d.MeasuredAt = time.Now()
		d.Measurement = homewizard.Measurement{
			PowerW:          ptr(-543.0),
			EnergyImportKWh: ptr(13779.338),
			WifiSSID:        "My Wi-Fi",
		}
	})

	server := newTestServer(t, Options{
		Dashboard:  config.Dashboard{Enabled: true, Path: "/", Refresh: config.Duration(10 * time.Second)},
		Store:      store,
		StaleAfter: time.Minute,
	})

	status, body := get(t, server, "/")
	if status != http.StatusOK {
		t.Fatalf("page status = %d", status)
	}
	if !strings.Contains(body, "assets/app.js") {
		t.Errorf("page did not reference its script:\n%s", body)
	}

	status, body = get(t, server, "/api/snapshot")
	if status != http.StatusOK {
		t.Fatalf("snapshot status = %d", status)
	}

	var view View
	if err := json.Unmarshal([]byte(body), &view); err != nil {
		t.Fatalf("snapshot is not JSON: %v\n%s", err, body)
	}
	if len(view.Devices) != 1 {
		t.Fatalf("devices = %d, want 1", len(view.Devices))
	}
	if view.Devices[0].Watts == nil || *view.Devices[0].Watts != -543 {
		t.Errorf("watts = %v", view.Devices[0].Watts)
	}
	if !view.Up {
		t.Error("a freshly read device should be up")
	}

	if status, _ := get(t, server, "/assets/style.css"); status != http.StatusOK {
		t.Errorf("stylesheet status = %d", status)
	}

	// Metrics still work with the dashboard mounted at root.
	if status, _ := get(t, server, "/metrics"); status != http.StatusOK {
		t.Errorf("metrics status = %d with the dashboard at root", status)
	}
}

func TestSnapshotCarriesNoSecrets(t *testing.T) {
	store := &snapshot.Store{}
	store.Update("p1", func(d *snapshot.Device) {
		d.MeasuredAt = time.Now()
		d.Measurement = homewizard.Measurement{PowerW: ptr(1.0)}
	})

	server := newTestServer(t, Options{
		Dashboard:  config.Dashboard{Enabled: true, Path: "/", Refresh: config.Duration(time.Second)},
		Store:      store,
		StaleAfter: time.Minute,
	})

	_, body := get(t, server, "/api/snapshot")
	for _, forbidden := range []string{"token", "Token", "password", "Authorization"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("snapshot mentions %q:\n%s", forbidden, body)
		}
	}
}

func TestBuildView(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	store := &snapshot.Store{}

	// A grid meter, a socket measuring one appliance, and a dead device.
	store.Update("p1", func(d *snapshot.Device) {
		d.Info = homewizard.Info{ProductType: homewizard.ProductP1}
		d.MeasuredAt = now
		d.Measurement = homewizard.Measurement{
			PowerW:      ptr(-543.0),
			PowerPhaseW: [3]*float64{ptr(-676.0), ptr(133.0), nil},
		}
	})
	store.Update("socket", func(d *snapshot.Device) {
		d.Info = homewizard.Info{ProductType: homewizard.ProductSocket}
		d.MeasuredAt = now
		d.Measurement = homewizard.Measurement{PowerW: ptr(2000.0)}
		d.StateAt = now
		d.State = homewizard.State{PowerOn: ptr(true)}
	})
	store.Update("dead", func(d *snapshot.Device) { d.Host = "192.168.1.99" })

	view := buildView(store.Load(), time.Minute, now)

	if view.Totals.Devices != 3 || view.Totals.Online != 2 {
		t.Errorf("totals = %+v, want 3 devices with 2 online", view.Totals)
	}

	if view.Totals.Watts != -543 {
		t.Errorf("household total = %v, want only the grid meter's -543", view.Totals.Watts)
	}

	if view.Status != "some devices are not reporting" {
		t.Errorf("status = %q", view.Status)
	}

	byName := map[string]DeviceView{}
	for _, device := range view.Devices {
		byName[device.Name] = device
	}

	if got := len(byName["p1"].Phases); got != 2 {
		t.Errorf("phases = %d, want 2", got)
	}
	if byName["socket"].SocketOn == nil || !*byName["socket"].SocketOn {
		t.Error("the socket's relay state should reach the page")
	}
	if byName["dead"].Status != "never reported" {
		t.Errorf("dead device status = %q", byName["dead"].Status)
	}
	if byName["dead"].Up {
		t.Error("a device that never answered is not up")
	}
}

func TestBuildViewBeforeFirstPoll(t *testing.T) {
	view := buildView(nil, time.Minute, time.Now())
	if view.Up || view.Status != "waiting for the first poll" {
		t.Errorf("view = %+v", view)
	}
}
